package estudiario

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agencia/pkg/observabilidad"
)

const paginaValida = `{"items":[{"id":"abc12345","title":"Guía","category":"Salta","tags":[],"status":"aprobado",
"source":"externo","author":{"name":"Ana"},"rating":{"average":null,"count":0},
"file":{"contentType":"application/pdf","sizeBytes":10,"downloadPath":"/v1/documents/abc12345/file"},
"createdAt":"2026-11-02T18:30:00-03:00","campoNuevo":"se ignora"}],"page":1,"size":20,"total":1}`

// servidor responde con las respuestas en orden (la última se repite).
func servidor(t *testing.T, respuestas ...func(http.ResponseWriter)) (*httptest.Server, *int32, *sync.Map) {
	t.Helper()
	var llamadas int32
	var headers sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&llamadas, 1)
		headers.Store(n, r.Header.Clone())
		respuestas[min(int(n), len(respuestas))-1](w)
	}))
	t.Cleanup(srv.Close)
	return srv, &llamadas, &headers
}

func ok(cuerpo string, status int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(cuerpo))
	}
}

func problema(status int, code string) func(http.ResponseWriter) {
	return ok(`{"type":"about:blank","title":"x","status":`+strconv.Itoa(status)+`,"code":"`+code+`","detail":"d"}`, status)
}

func clienteRapido(url string) *Cliente {
	return NuevoCliente(Config{
		URL: url, APIKey: "k",
		EsperasLectura:  []time.Duration{time.Millisecond, time.Millisecond},
		EsperasRegistro: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		TimeoutLectura:  200 * time.Millisecond,
		PausaCircuito:   100 * time.Millisecond,
	})
}

func TestReintentaUn503YDespuesResponde(t *testing.T) {
	srv, llamadas, _ := servidor(t, problema(503, "SERVICE_UNAVAILABLE"), ok(paginaValida, 200))
	p, err := clienteRapido(srv.URL).Buscar(context.Background(), Busqueda{Destino: "Salta"})
	if err != nil || len(p.Guias) != 1 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if *llamadas != 2 {
		t.Errorf("llamadas = %d; se esperaba 1 reintento", *llamadas)
	}
}

func TestNoReintentaUnRechazoDeNegocio(t *testing.T) {
	srv, llamadas, _ := servidor(t, problema(422, "VALIDATION_ERROR"))
	_, err := clienteRapido(srv.URL).Registrar(context.Background(), Registro{IdempotencyKey: "clave-123", PDF: []byte("%PDF")})
	var r *ErrorRechazo
	if !errors.As(err, &r) || r.Codigo != "VALIDATION_ERROR" {
		t.Fatalf("err = %v", err)
	}
	if *llamadas != 1 {
		t.Errorf("llamadas = %d; un 422 no se reintenta", *llamadas)
	}
}

func TestLosReintentosDelRegistroUsanLaMismaClave(t *testing.T) {
	srv, llamadas, headers := servidor(t, problema(409, "REQUEST_IN_PROGRESS"), problema(503, "SERVICE_UNAVAILABLE"),
		ok(`{"id":"abc12345","title":"t","category":"c","tags":[],"status":"pendiente","source":"externo","author":{"name":"a"},"rating":{"average":null,"count":0},"file":{"contentType":"application/pdf","sizeBytes":1,"downloadPath":"/x"},"createdAt":"2026-11-02T18:30:00Z"}`, 202))
	g, err := clienteRapido(srv.URL).Registrar(context.Background(), Registro{IdempotencyKey: "clave-fija-1", PDF: []byte("%PDF")})
	if err != nil || g.Estado != EstadoPendiente {
		t.Fatalf("g=%+v err=%v", g, err)
	}
	for i := int32(1); i <= *llamadas; i++ {
		h, _ := headers.Load(i)
		if got := h.(http.Header).Get("Idempotency-Key"); got != "clave-fija-1" {
			t.Errorf("intento %d con Idempotency-Key %q", i, got)
		}
	}
}

func TestPropagaLaCorrelacion(t *testing.T) {
	srv, _, headers := servidor(t, ok(paginaValida, 200))
	ctx := observabilidad.ConCorrelacion(context.Background(), "corr-77")
	if _, err := clienteRapido(srv.URL).Buscar(ctx, Busqueda{}); err != nil {
		t.Fatal(err)
	}
	h, _ := headers.Load(int32(1))
	if h.(http.Header).Get("X-Correlation-Id") != "corr-77" || h.(http.Header).Get("X-API-Key") != "k" {
		t.Errorf("headers = %v", h)
	}
}

func TestEnlacePublicoAlPDF(t *testing.T) {
	srv, _, _ := servidor(t, ok(paginaValida, 200))
	p, err := clienteRapido(srv.URL).Buscar(context.Background(), Busqueda{})
	if err != nil || p.Guias[0].URLDescarga != "" {
		t.Fatalf("sin URLPublica no debería haber enlace: %+v err=%v", p, err)
	}
	cli := NuevoCliente(Config{URL: srv.URL, APIKey: "k", URLPublica: "https://estudiario.example/"})
	p, err = cli.Buscar(context.Background(), Busqueda{})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Guias[0].URLDescarga; got != "https://estudiario.example/v1/public/documents/abc12345/file" {
		t.Errorf("URLDescarga = %q", got)
	}
}

func TestLectorTolerante(t *testing.T) {
	cuerpo := strings.Replace(paginaValida, `"aprobado"`, `"archivado"`, 1)
	srv, _, _ := servidor(t, ok(cuerpo, 200))
	p, err := clienteRapido(srv.URL).Buscar(context.Background(), Busqueda{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Guias[0].Estado != EstadoDesconocido || p.Guias[0].Calificacion != nil {
		t.Errorf("guía = %+v", p.Guias[0])
	}
}

func TestRespuestaInvalida(t *testing.T) {
	srv, _, _ := servidor(t, ok(`no es json`, 200))
	_, err := clienteRapido(srv.URL).Buscar(context.Background(), Busqueda{})
	if !errors.Is(err, ErrRespuestaInvalida) {
		t.Fatalf("err = %v", err)
	}
}

func TestTimeoutEsNoDisponible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	inicio := time.Now()
	_, err := clienteRapido(srv.URL).Buscar(context.Background(), Busqueda{})
	if !errors.Is(err, ErrNoDisponible) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("tardó %v: 3 intentos de 200 ms", d)
	}
}

func TestCircuitoSeAbreDespuesDeFallosSeguidosYSeCierraConUnaPruebaExitosa(t *testing.T) {
	var caido atomic.Bool
	caido.Store(true)
	var llamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamadas.Add(1)
		if caido.Load() {
			problema(503, "SERVICE_UNAVAILABLE")(w)
			return
		}
		ok(paginaValida, 200)(w)
	}))
	defer srv.Close()
	cli := clienteRapido(srv.URL)

	for range 5 {
		_, _ = cli.Buscar(context.Background(), Busqueda{})
	}
	if !cli.CircuitoAbierto() {
		t.Fatal("el circuito debía abrirse después de 5 llamadas fallidas")
	}
	antes := llamadas.Load()
	if _, err := cli.Buscar(context.Background(), Busqueda{}); !errors.Is(err, ErrCircuitoAbierto) {
		t.Fatalf("err = %v; se esperaba ErrCircuitoAbierto", err)
	}
	if llamadas.Load() != antes {
		t.Error("con el circuito abierto no se debía llamar al proveedor")
	}

	caido.Store(false)
	time.Sleep(150 * time.Millisecond) // pasa la pausa: semiabierto
	if _, err := cli.Buscar(context.Background(), Busqueda{}); err != nil {
		t.Fatalf("la llamada de prueba debía pasar: %v", err)
	}
	if cli.CircuitoAbierto() {
		t.Error("el circuito debía cerrarse después de la prueba exitosa")
	}
}

func TestUnRechazoNoAbreElCircuito(t *testing.T) {
	srv, _, _ := servidor(t, problema(422, "VALIDATION_ERROR"))
	cli := clienteRapido(srv.URL)
	for range 10 {
		_, _ = cli.Registrar(context.Background(), Registro{IdempotencyKey: "clave-123", PDF: []byte("%PDF")})
	}
	if cli.CircuitoAbierto() {
		t.Error("los rechazos de negocio no indican que el proveedor esté caído")
	}
}
