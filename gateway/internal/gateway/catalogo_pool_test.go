package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolCatalogoDistribuyeRoundRobinEntreInstanciasSanas(t *testing.T) {
	var recibidas1, recibidas2 atomic.Int32
	crearInstancia := func(recibidas *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				return
			}
			recibidas.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	instancia1 := crearInstancia(&recibidas1)
	defer instancia1.Close()
	instancia2 := crearInstancia(&recibidas2)
	defer instancia2.Close()

	pool, err := nuevoPoolCatalogo([]string{instancia1.URL, instancia2.URL}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.cerrar()
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		pool.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/ofertas", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("solicitud %d: status = %d", i, w.Code)
		}
	}
	if recibidas1.Load() != 1 || recibidas2.Load() != 1 {
		t.Fatalf("distribución inesperada: instancia1=%d instancia2=%d", recibidas1.Load(), recibidas2.Load())
	}
}

func TestPoolRetiraTrasTresFallasYReincorporaTrasDosExitos(t *testing.T) {
	instancia := &instanciaCatalogo{nombre: "catalogo-prueba", sana: true}
	pool := &poolCatalogo{
		log: slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)),
	}
	for i := 0; i < fallasParaRetirar-1; i++ {
		pool.registrarResultado(instancia, false, context.DeadlineExceeded)
		if !instancia.sana {
			t.Fatal("la instancia se retiró antes de tres fallas")
		}
	}
	pool.registrarResultado(instancia, false, context.DeadlineExceeded)
	if instancia.sana {
		t.Fatal("la instancia debería retirarse luego de tres fallas")
	}
	pool.registrarResultado(instancia, true, nil)
	if instancia.sana {
		t.Fatal("la instancia volvió al pool antes de dos éxitos")
	}
	pool.registrarResultado(instancia, true, nil)
	if !instancia.sana {
		t.Fatal("la instancia debería reincorporarse luego de dos éxitos")
	}
}

func TestPoolChequeaConTimeoutDeUnSegundo(t *testing.T) {
	if timeoutChequeoCatalogo != time.Second || intervaloChequeoCatalogo != 5*time.Second {
		t.Fatalf("intervalo/timeout cambiaron: intervalo=%s timeout=%s", intervaloChequeoCatalogo, timeoutChequeoCatalogo)
	}
}

func TestRutasCatalogoResponden503CuandoNoHayInstanciasConfiguradas(t *testing.T) {
	cfg := Config{
		SecretoJWT: secretoPrueba, UsuariosURL: "http://127.0.0.1:1", ReservasURL: "http://127.0.0.1:1",
		LimiteSolicitudesMin: 120,
	}
	handler, cerrar, err := NuevoRouter(cfg, slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cerrar()

	respuesta := nuevoRecorder()
	handler.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, "/api/busqueda", nil))
	if respuesta.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, se esperaba 503: %s", respuesta.Code, respuesta.Body.String())
	}
}
