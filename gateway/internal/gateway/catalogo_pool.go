package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	intervaloChequeoCatalogo = 5 * time.Second
	timeoutChequeoCatalogo   = time.Second
	fallasParaRetirar        = 3
	exitosParaReincorporar   = 2
)

type instanciaCatalogo struct {
	nombre  string
	baseURL string
	proxy   *httputil.ReverseProxy
	sana    bool
	fallas  int
	exitos  int
}

type poolCatalogo struct {
	log        *slog.Logger
	cliente    *http.Client
	instancias []*instanciaCatalogo
	siguiente  atomic.Uint64
	mu         sync.RWMutex
	cancelar   context.CancelFunc
	terminado  chan struct{}
}

func nuevoPoolCatalogo(urls []string, log *slog.Logger) (*poolCatalogo, error) {
	p := &poolCatalogo{
		log: log, cliente: &http.Client{Timeout: timeoutChequeoCatalogo},
		terminado: make(chan struct{}),
	}
	for i, direccion := range urls {
		direccion = strings.TrimRight(direccion, "/")
		objetivo, err := url.Parse(direccion)
		if err != nil {
			return nil, fmt.Errorf("parsear URL de Catálogo %d: %w", i+1, err)
		}
		nombre := objetivo.Host
		if len(urls) > 1 {
			nombre = fmt.Sprintf("catalogo-%d", i+1)
		}
		proxy, err := nuevoProxy(nombre, direccion, log)
		if err != nil {
			return nil, err
		}
		p.instancias = append(p.instancias, &instanciaCatalogo{
			nombre: nombre, baseURL: direccion, proxy: proxy, sana: true,
		})
		saludInstanciaCatalogo.WithLabelValues(nombre).Set(1)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	p.cancelar = cancelar
	go p.verificarPeriodicamente(ctx)
	return p, nil
}

func nuevoProxy(servicio, direccion string, log *slog.Logger) (*httputil.ReverseProxy, error) {
	objetivo, err := url.Parse(direccion)
	if err != nil {
		return nil, fmt.Errorf("parsear URL de %s: %w", servicio, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(objetivo)
	proxy.Transport = otelTransport()
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// El pool de Catálogo puede reintentar en otra instancia: entonces solo
		// anota la falla y no responde (ver poolCatalogo.ServeHTTP).
		if intento, ok := r.Context().Value(claveIntento{}).(*resultadoIntento); ok {
			intento.err = err
			return
		}
		log.ErrorContext(r.Context(), "falló la solicitud al servicio", "servicio", servicio, "error", err)
		metricasGateway.WithLabelValues(servicio, rutaMetrica(r), "502").Inc()
		responderProblemaHTTP(w, http.StatusBadGateway, "servicio-no-disponible", "Servicio no disponible",
			"No se pudo completar la solicitud al servicio.")
	}
	proxy.ModifyResponse = func(res *http.Response) error {
		metricasGateway.WithLabelValues(servicio, rutaMetrica(res.Request), strconv.Itoa(res.StatusCode)).Inc()
		return nil
	}
	return proxy, nil
}

func otelTransport() http.RoundTripper {
	return otelhttp.NewTransport(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	})
}

// claveIntento marca en el contexto una solicitud que el pool puede repetir en
// otra instancia; resultadoIntento recibe el error de transporte, si lo hubo.
type claveIntento struct{}

type resultadoIntento struct{ err error }

// ServeHTTP elige una instancia sana por round robin. Una lectura (GET o HEAD)
// que no llega a la instancia (conexión rechazada, timeout) se repite una vez en
// otra instancia sana, y la falla cuenta para retirarla del pool sin esperar a
// los chequeos de salud (detección pasiva). Las escrituras no se repiten: el
// error de transporte no dice si la instancia llegó a aplicar el cambio.
func (p *poolCatalogo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lectura := r.Method == http.MethodGet || r.Method == http.MethodHead
	var descartada *instanciaCatalogo
	if lectura {
		seleccionada := p.seleccionar(nil)
		if seleccionada == nil {
			p.responderSinInstancias(w, r)
			return
		}
		intento := &resultadoIntento{}
		seleccionada.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveIntento{}, intento)))
		if intento.err == nil {
			return
		}
		if r.Context().Err() != nil {
			return // el cliente se fue: no hay a quién responder
		}
		p.log.WarnContext(r.Context(), "la instancia de Catálogo no respondió; se reintenta en otra",
			"instancia", seleccionada.nombre, "error", intento.err)
		p.registrarResultado(seleccionada, false, intento.err)
		descartada = seleccionada
	}
	seleccionada := p.seleccionar(descartada)
	if seleccionada == nil {
		p.responderSinInstancias(w, r)
		return
	}
	seleccionada.proxy.ServeHTTP(w, r)
}

func (p *poolCatalogo) responderSinInstancias(w http.ResponseWriter, r *http.Request) {
	responderProblemaHTTP(w, http.StatusServiceUnavailable, "catalogo-no-disponible", "Catálogo no disponible",
		"No hay instancias de Catálogo saludables.")
	metricasGateway.WithLabelValues("ninguna", rutaMetrica(r), "503").Inc()
}

// seleccionar devuelve la próxima instancia sana, salteando excluida (la que
// acaba de fallar), o nil si no queda ninguna.
func (p *poolCatalogo) seleccionar(excluida *instanciaCatalogo) *instanciaCatalogo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sanas := make([]*instanciaCatalogo, 0, len(p.instancias))
	for _, instancia := range p.instancias {
		if instancia.sana && instancia != excluida {
			sanas = append(sanas, instancia)
		}
	}
	if len(sanas) == 0 {
		return nil
	}
	posicion := p.siguiente.Add(1) - 1
	return sanas[posicion%uint64(len(sanas))]
}

func (p *poolCatalogo) verificarPeriodicamente(ctx context.Context) {
	defer close(p.terminado)
	p.verificar(ctx)
	ticker := time.NewTicker(intervaloChequeoCatalogo)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.verificar(ctx)
		}
	}
}

func (p *poolCatalogo) verificar(ctx context.Context) {
	var grupo sync.WaitGroup
	for _, instancia := range p.instancias {
		grupo.Add(1)
		go func(instancia *instanciaCatalogo) {
			defer grupo.Done()
			chequeo, cancelar := context.WithTimeout(ctx, timeoutChequeoCatalogo)
			defer cancelar()
			req, err := http.NewRequestWithContext(chequeo, http.MethodGet, instancia.baseURL+"/health", nil)
			if err == nil {
				resp, requestErr := p.cliente.Do(req)
				err = requestErr
				if resp != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						err = fmt.Errorf("GET /health devolvió %d", resp.StatusCode)
					}
				}
			}
			if errors.Is(err, context.Canceled) {
				return
			}
			p.registrarResultado(instancia, err == nil, err)
		}(instancia)
	}
	grupo.Wait()
}

func (p *poolCatalogo) registrarResultado(instancia *instanciaCatalogo, exito bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	antes := instancia.sana
	if exito {
		instancia.fallas = 0
		instancia.exitos++
		if !instancia.sana && instancia.exitos >= exitosParaReincorporar {
			instancia.sana = true
			instancia.exitos = 0
		}
	} else {
		instancia.exitos = 0
		instancia.fallas++
		if instancia.sana && instancia.fallas >= fallasParaRetirar {
			instancia.sana = false
			instancia.fallas = 0
		}
	}
	if antes != instancia.sana {
		valor := 0.0
		if instancia.sana {
			valor = 1
		}
		saludInstanciaCatalogo.WithLabelValues(instancia.nombre).Set(valor)
		p.log.Warn("cambió el estado de una instancia de Catálogo", "instancia", instancia.nombre, "sana", instancia.sana, "error", errorTexto(err))
	}
}

func errorTexto(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (p *poolCatalogo) cerrar() {
	p.cancelar()
	<-p.terminado
}
