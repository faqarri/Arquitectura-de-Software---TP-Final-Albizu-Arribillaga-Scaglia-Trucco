package estudiario

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agencia/pkg/observabilidad"
)

// Config del cliente. Solo URL y APIKey son obligatorias.
type Config struct {
	URL    string // base, sin /v1, por ejemplo https://estudiario.example
	APIKey string
	// URLPublica es la base desde la que el navegador descarga el PDF de una
	// guía publicada, sin API Key (/v1/public/documents/{id}/file, ruta que el
	// proveedor agregó al desplegar y no figura en el contrato 1.0.0). Vacía:
	// las guías no traen enlace (el mock no sirve esa ruta).
	URLPublica string

	// Lecturas (buscar, listar): las usa un usuario esperando una pantalla, así
	// que van con timeout corto y pocos reintentos (ADR-009: 2 s y 2 reintentos).
	TimeoutLectura time.Duration
	EsperasLectura []time.Duration
	// Registro: lo hace un proceso en segundo plano, con los valores que
	// recomienda el proveedor (20 s y reintentos de 1, 2 y 4 s).
	TimeoutRegistro time.Duration
	EsperasRegistro []time.Duration

	FallosParaAbrir int           // circuit breaker; por defecto 5
	PausaCircuito   time.Duration // por defecto 30 s

	HTTP *http.Client
}

func (c Config) conDefectos() Config {
	if c.TimeoutLectura <= 0 {
		c.TimeoutLectura = 2 * time.Second
	}
	if c.EsperasLectura == nil {
		c.EsperasLectura = []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	}
	if c.TimeoutRegistro <= 0 {
		c.TimeoutRegistro = 20 * time.Second
	}
	if c.EsperasRegistro == nil {
		c.EsperasRegistro = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	}
	if c.FallosParaAbrir <= 0 {
		c.FallosParaAbrir = 5
	}
	if c.PausaCircuito <= 0 {
		c.PausaCircuito = 30 * time.Second
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{}
	}
	c.URL = strings.TrimRight(c.URL, "/")
	c.URLPublica = strings.TrimRight(c.URLPublica, "/")
	return c
}

// Cliente de Estudiario. Es seguro para usar desde varias goroutines.
type Cliente struct {
	cfg      Config
	circuito *circuito
}

func NuevoCliente(cfg Config) *Cliente {
	cfg = cfg.conDefectos()
	return &Cliente{cfg: cfg, circuito: nuevoCircuito(cfg.FallosParaAbrir, cfg.PausaCircuito)}
}

// CircuitoAbierto informa si el circuit breaker está abierto (para /health o métricas).
func (c *Cliente) CircuitoAbierto() bool { return c.circuito.abiertoAhora() }

// Buscar busca guías publicadas.
func (c *Cliente) Buscar(ctx context.Context, b Busqueda) (Pagina, error) {
	q := url.Values{}
	if b.Texto != "" {
		q.Set("q", b.Texto)
	}
	if b.Destino != "" {
		q.Set("category", b.Destino)
	}
	for _, e := range b.Etiquetas {
		q.Add("tag", e)
	}
	if b.CalificacionMin > 0 {
		q.Set("minRating", strconv.FormatFloat(b.CalificacionMin, 'f', -1, 64))
	}
	if b.Orden != "" {
		q.Set("sort", string(b.Orden))
	}
	agregarPaginacion(q, b.Pagina, b.Tamanio)

	var p paginaDTO
	err := c.llamar(ctx, "buscar", c.cfg.TimeoutLectura, c.cfg.EsperasLectura, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL+"/v1/documents?"+q.Encode(), nil)
	}, http.StatusOK, &p)
	if err != nil {
		return Pagina{}, err
	}
	pagina, err := p.aPagina()
	if err != nil || c.cfg.URLPublica == "" {
		return pagina, err
	}
	for i := range pagina.Guias {
		pagina.Guias[i].URLDescarga = c.cfg.URLPublica + "/v1/public/documents/" + url.PathEscape(pagina.Guias[i].ID) + "/file"
	}
	return pagina, nil
}

// Registradas lista las guías que registramos, en cualquier estado
// (filtro opcional por estado). Sirve para seguir la moderación.
func (c *Cliente) Registradas(ctx context.Context, estado EstadoGuia, pagina, tamanio int) (Pagina, error) {
	q := url.Values{}
	if s, ok := estadoAProveedor[estado]; ok {
		q.Set("status", s)
	}
	agregarPaginacion(q, pagina, tamanio)

	var p paginaDTO
	err := c.llamar(ctx, "registradas", c.cfg.TimeoutLectura, c.cfg.EsperasLectura, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL+"/v1/submissions?"+q.Encode(), nil)
	}, http.StatusOK, &p)
	if err != nil {
		return Pagina{}, err
	}
	return p.aPagina()
}

// Registrar registra una guía. Queda PENDIENTE hasta que Estudiario la modera.
// Los reintentos usan siempre la misma IdempotencyKey, así nunca se registra dos veces.
func (c *Cliente) Registrar(ctx context.Context, r Registro) (Guia, error) {
	if r.IdempotencyKey == "" {
		return Guia{}, errors.New("estudiario: Registrar necesita IdempotencyKey")
	}
	cuerpo, tipo, err := formularioRegistro(r)
	if err != nil {
		return Guia{}, err
	}
	var d documentoDTO
	err = c.llamar(ctx, "registrar", c.cfg.TimeoutRegistro, c.cfg.EsperasRegistro, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/v1/documents", bytes.NewReader(cuerpo))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", tipo)
		req.Header.Set("Idempotency-Key", r.IdempotencyKey)
		return req, nil
	}, http.StatusAccepted, &d)
	if err != nil {
		return Guia{}, err
	}
	return d.aGuia()
}

func agregarPaginacion(q url.Values, pagina, tamanio int) {
	if pagina > 0 {
		q.Set("page", strconv.Itoa(pagina))
	}
	if tamanio > 0 {
		q.Set("size", strconv.Itoa(min(tamanio, 50)))
	}
}

func formularioRegistro(r Registro) ([]byte, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	campos := [][2]string{
		{"title", r.Titulo},
		{"category", r.Destino},
		{"authorName", r.Autor},
	}
	if r.Descripcion != "" {
		campos = append(campos, [2]string{"description", r.Descripcion})
	}
	if len(r.Etiquetas) > 0 {
		campos = append(campos, [2]string{"tags", strings.Join(r.Etiquetas, ",")})
	}
	if r.AutorExternoID != "" {
		campos = append(campos, [2]string{"authorExternalId", r.AutorExternoID})
	}
	for _, c := range campos {
		if err := w.WriteField(c[0], c[1]); err != nil {
			return nil, "", err
		}
	}
	nombre := r.NombreArchivo
	if nombre == "" {
		nombre = "guia.pdf"
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, nombre))
	h.Set("Content-Type", "application/pdf")
	parte, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := parte.Write(r.PDF); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return b.Bytes(), w.FormDataContentType(), nil
}

// maxRetryAfter acota la espera que pide el proveedor en Retry-After, para no
// colgar una operación por un valor exagerado.
const maxRetryAfter = 10 * time.Second

// llamar ejecuta la solicitud con timeout por intento, reintentos con espera
// exponencial y jitter (solo ante error de red, timeout, 409, 429 y 503, como
// pide el proveedor) y circuit breaker. Decodifica una respuesta esperado en destino.
func (c *Cliente) llamar(ctx context.Context, operacion string, timeout time.Duration, esperas []time.Duration,
	nuevaSolicitud func(context.Context) (*http.Request, error), esperado int, destino any) error {

	if !c.circuito.permitir() {
		metricaLlamadas.WithLabelValues(operacion, "circuito_abierto").Inc()
		return ErrCircuitoAbierto
	}

	var err error
	for intento := 0; ; intento++ {
		var reintentable bool
		var retryAfter time.Duration
		reintentable, retryAfter, err = c.intentar(ctx, timeout, nuevaSolicitud, esperado, destino)
		if !reintentable || intento >= len(esperas) || ctx.Err() != nil {
			break
		}
		espera := conJitter(esperas[intento])
		if retryAfter > 0 {
			espera = min(retryAfter, maxRetryAfter)
		}
		select {
		case <-ctx.Done():
			err = fmt.Errorf("%w: %v", ErrNoDisponible, ctx.Err())
		case <-time.After(espera):
			continue
		}
		break
	}

	// Solo la falta de disponibilidad cuenta como fallo para el circuito.
	c.circuito.registrar(!errors.Is(err, ErrNoDisponible))
	metricaLlamadas.WithLabelValues(operacion, resultadoMetrica(err)).Inc()
	return err
}

func resultadoMetrica(err error) string {
	var rechazo *ErrorRechazo
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &rechazo), errors.Is(err, ErrNoEncontrada):
		return "rechazo"
	case errors.Is(err, ErrRespuestaInvalida):
		return "invalida"
	default:
		return "no_disponible"
	}
}

func (c *Cliente) intentar(ctx context.Context, timeout time.Duration, nuevaSolicitud func(context.Context) (*http.Request, error),
	esperado int, destino any) (reintentable bool, retryAfter time.Duration, err error) {

	ctx, cancelar := context.WithTimeout(ctx, timeout)
	defer cancelar()
	req, err := nuevaSolicitud(ctx)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json, application/problem+json")
	if id := observabilidad.Correlacion(ctx); id != "" {
		req.Header.Set("X-Correlation-Id", id)
	}

	res, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return true, 0, fmt.Errorf("%w: %v", ErrNoDisponible, err)
	}
	defer res.Body.Close()
	cuerpo, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return true, 0, fmt.Errorf("%w: leer respuesta: %v", ErrNoDisponible, err)
	}

	if res.StatusCode == esperado {
		if err := json.Unmarshal(cuerpo, destino); err != nil {
			return false, 0, fmt.Errorf("%w: %v", ErrRespuestaInvalida, err)
		}
		return false, 0, nil
	}

	p := leerProblema(cuerpo)
	switch {
	case res.StatusCode == http.StatusConflict, res.StatusCode == http.StatusTooManyRequests,
		res.StatusCode == http.StatusServiceUnavailable:
		return true, leerRetryAfter(res.Header.Get("Retry-After")),
			fmt.Errorf("%w: %d %s", ErrNoDisponible, res.StatusCode, p.Code)
	case res.StatusCode >= 500:
		// Otros 5xx: el proveedor no los recomienda reintentar, pero sí cuentan para el circuito.
		return false, 0, fmt.Errorf("%w: %d %s", ErrNoDisponible, res.StatusCode, p.Code)
	case res.StatusCode == http.StatusUnauthorized:
		// Nuestra API Key falta o es inválida: es un problema de configuración
		// nuestro, no de los datos. No se reintenta ahora, pero tampoco se
		// rechaza la guía: queda para cuando se corrija la clave.
		return false, 0, ErrNoAutorizado
	case res.StatusCode == http.StatusNotFound:
		return false, 0, ErrNoEncontrada
	case res.StatusCode >= 400:
		return false, 0, p.aRechazo(res.StatusCode)
	default:
		return false, 0, fmt.Errorf("%w: estado %d inesperado", ErrRespuestaInvalida, res.StatusCode)
	}
}

func leerRetryAfter(v string) time.Duration {
	s, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

func conJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}
