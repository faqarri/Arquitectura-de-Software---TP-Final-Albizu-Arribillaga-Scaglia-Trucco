package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/comprobantes/internal/adapters/api"
	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// Tests de la API contra el contrato 1.0.1, con un repositorio en memoria.

const (
	claveA = "clave-del-grupo-a"
	claveB = "clave-del-grupo-b"
)

type repoEnMemoria struct {
	mu     sync.Mutex
	porID  map[string]*domain.Comprobante
	fallar error // si no es nil, todas las operaciones fallan con este error
}

func (r *repoEnMemoria) Guardar(_ context.Context, c *domain.Comprobante) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallar != nil {
		return r.fallar
	}
	for _, e := range r.porID {
		if e.Consumidor == c.Consumidor && e.IdempotencyKey == c.IdempotencyKey {
			return ports.ErrClaveDuplicada
		}
	}
	c.Nro = int64(len(r.porID) + 1)
	r.porID[c.ID] = c
	return nil
}

func (r *repoEnMemoria) BuscarPorClave(_ context.Context, consumidor, clave string) (*domain.Comprobante, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.porID {
		if e.Consumidor == consumidor && e.IdempotencyKey == clave {
			return e, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}

func (r *repoEnMemoria) BuscarPorID(_ context.Context, consumidor, id string) (*domain.Comprobante, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallar != nil {
		return nil, r.fallar
	}
	c, ok := r.porID[id]
	if !ok || c.Consumidor != consumidor {
		return nil, ports.ErrNoEncontrado
	}
	return c, nil
}

func (r *repoEnMemoria) Obtener(ctx context.Context, id string) (*domain.Comprobante, error) {
	return nil, errors.New("no usado en estos tests")
}

func (r *repoEnMemoria) ActualizarEnvio(context.Context, *domain.Comprobante) error { return nil }

func nuevoServidor(t *testing.T) (*httptest.Server, *repoEnMemoria) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &repoEnMemoria{porID: map[string]*domain.Comprobante{}}
	router := api.NuevoRouter(api.Dependencias{
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Emitir:         application.NuevoEmitirComprobante(repo),
		Obtener:        application.NuevoObtenerComprobante(repo),
		APIKeys:        map[string]string{claveA: "grupo-a", claveB: "grupo-b"},
		VerificarSalud: func(context.Context) error { return nil },
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, repo
}

type respuesta struct {
	estado  int
	headers http.Header
	cuerpo  map[string]any
	crudo   string
}

func pedir(t *testing.T, srv *httptest.Server, metodo, ruta, apiKey, clave, cuerpo string) respuesta {
	t.Helper()
	req, err := http.NewRequest(metodo, srv.URL+ruta, strings.NewReader(cuerpo))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	r := respuesta{estado: res.StatusCode, headers: res.Header, crudo: string(b)}
	_ = json.Unmarshal(b, &r.cuerpo)
	return r
}

func TestMetricsExponeMetricasPrometheusSinAutenticacion(t *testing.T) {
	srv, _ := nuevoServidor(t)

	if res := pedir(t, srv, http.MethodGet, "/health", "", "", ""); res.estado != http.StatusOK {
		t.Fatalf("GET /health: estado = %d, se esperaba %d", res.estado, http.StatusOK)
	}
	res := pedir(t, srv, http.MethodGet, "/metrics", "", "", "")

	if res.estado != http.StatusOK {
		t.Fatalf("GET /metrics: estado = %d, se esperaba %d", res.estado, http.StatusOK)
	}
	if !strings.Contains(res.crudo, "http_requests_total") {
		t.Fatal("la respuesta no contiene el contador HTTP")
	}
	if !strings.Contains(res.crudo, "http_request_duration_seconds") {
		t.Fatal("la respuesta no contiene el histograma de duración HTTP")
	}
}

const cuerpoValido = `{
  "email": "ana.perez@example.com",
  "items": [
    { "descripcion": "Entrada general x2", "precio": 30000 },
    { "descripcion": "Cargo por servicio", "precio": 1500 }
  ]
}`

func esperarEstado(t *testing.T, r respuesta, estado int) {
	t.Helper()
	if r.estado != estado {
		t.Fatalf("estado %d, se esperaba %d. Cuerpo: %s", r.estado, estado, r.crudo)
	}
}

func esperarProblema(t *testing.T, r respuesta, estado int, tipo string) {
	t.Helper()
	esperarEstado(t, r, estado)
	if ct := r.headers.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type %q, se esperaba application/problem+json", ct)
	}
	if got := r.cuerpo["type"]; got != "https://agencia-viajes.example/errores/"+tipo {
		t.Errorf("type %v, se esperaba el error %s", got, tipo)
	}
}

func TestSinAPIKeyOConClaveInvalida(t *testing.T) {
	srv, _ := nuevoServidor(t)
	esperarProblema(t, pedir(t, srv, "POST", "/v1/comprobantes", "", "clave-123456", cuerpoValido), 401, "no-autenticado")
	esperarProblema(t, pedir(t, srv, "GET", "/v1/comprobantes/x", "otra-clave", "", ""), 401, "no-autenticado")
}

func TestEmitirYReintentar(t *testing.T) {
	srv, _ := nuevoServidor(t)

	r := pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", cuerpoValido)
	esperarEstado(t, r, 201)
	id, _ := r.cuerpo["id"].(string)
	if r.headers.Get("Location") != "/v1/comprobantes/"+id {
		t.Errorf("Location %q no apunta al comprobante %s", r.headers.Get("Location"), id)
	}
	if r.cuerpo["estadoEnvio"] != "PENDIENTE" || r.cuerpo["numero"] != "B-0001-00000001" || r.cuerpo["email"] != "ana.perez@example.com" {
		t.Errorf("cuerpo inesperado: %s", r.crudo)
	}
	if !strings.Contains(r.crudo, `"total":31500.00`) || !strings.Contains(r.crudo, `"precio":30000.00`) {
		t.Errorf("los importes deberían ir como números con 2 decimales: %s", r.crudo)
	}

	// Mismo cuerpo con otro formato (espacios distintos): es el mismo pedido.
	reintento := pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", strings.Join(strings.Fields(cuerpoValido), " "))
	esperarEstado(t, reintento, 200)
	if reintento.cuerpo["id"] != id {
		t.Errorf("el reintento devolvió otro comprobante: %v", reintento.cuerpo["id"])
	}
}

func TestMismaClaveConOtroCuerpo(t *testing.T) {
	srv, _ := nuevoServidor(t)
	esperarEstado(t, pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", cuerpoValido), 201)

	otro := strings.Replace(cuerpoValido, "30000", "31000", 1)
	esperarProblema(t, pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", otro), 409, "conflicto-idempotencia")
}

func TestRedondeaPreciosConMasDeDosDecimales(t *testing.T) {
	srv, _ := nuevoServidor(t)
	cuerpo := `{"email": "ana@example.com", "items": [{"descripcion": "A", "precio": 10.005}, {"descripcion": "B", "precio": 0.004}]}`

	r := pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-redondeo", cuerpo)
	esperarEstado(t, r, 201)
	if !strings.Contains(r.crudo, `"total":10.01`) {
		t.Errorf("total esperado 10.01: %s", r.crudo)
	}
}

func TestTotalCero(t *testing.T) {
	srv, _ := nuevoServidor(t)
	cuerpo := `{"email": "ana@example.com", "items": [{"descripcion": "Gratis", "precio": 0}]}`
	esperarProblema(t, pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-gratis", cuerpo), 422, "regla-incumplida")
}

func TestImporteFueraDeRango(t *testing.T) {
	srv, _ := nuevoServidor(t)
	cuerpo := `{"email": "ana@example.com", "items": [{"descripcion": "Caro", "precio": 1000000000000}]}`
	esperarProblema(t, pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-carisima", cuerpo), 422, "regla-incumplida")
}

func TestRutaInexistente(t *testing.T) {
	srv, _ := nuevoServidor(t)
	esperarProblema(t, pedir(t, srv, "GET", "/v1/otra-cosa", claveA, "", ""), 404, "no-encontrado")
}

func TestSolicitudesInvalidas(t *testing.T) {
	srv, _ := nuevoServidor(t)
	casos := []struct {
		nombre string
		clave  string
		cuerpo string
		campo  string
	}{
		{"sin Idempotency-Key", "", cuerpoValido, "Idempotency-Key"},
		{"Idempotency-Key corta", "corta", cuerpoValido, "Idempotency-Key"},
		{"campo no definido", "orden-invalida-1", `{"email": "a@b.com", "moneda": "ARS", "items": [{"descripcion": "A", "precio": 1}]}`, "moneda"},
		{"campo no definido en un ítem", "orden-invalida-2", `{"email": "a@b.com", "items": [{"descripcion": "A", "precio": 1, "cantidad": 2}]}`, "cantidad"},
		{"precio como texto", "orden-invalida-3", `{"email": "a@b.com", "items": [{"descripcion": "A", "precio": "100"}]}`, "items[0].precio"},
		{"precio negativo", "orden-invalida-4", `{"email": "a@b.com", "items": [{"descripcion": "A", "precio": -1}]}`, "items[0].precio"},
		{"sin precio", "orden-invalida-5", `{"email": "a@b.com", "items": [{"descripcion": "A"}]}`, "items[0].precio"},
		{"descripción vacía", "orden-invalida-6", `{"email": "a@b.com", "items": [{"descripcion": "", "precio": 1}]}`, "items[0].descripcion"},
		{"sin ítems", "orden-invalida-7", `{"email": "a@b.com", "items": []}`, "items"},
		{"email inválido", "orden-invalida-8", `{"email": "no-es-un-email", "items": [{"descripcion": "A", "precio": 1}]}`, "email"},
		{"email con nombre", "orden-invalida-9", `{"email": "Ana <a@b.com>", "items": [{"descripcion": "A", "precio": 1}]}`, "email"},
		{"sin email", "orden-invalida-10", `{"items": [{"descripcion": "A", "precio": 1}]}`, "email"},
		{"JSON roto", "orden-invalida-11", `{"email": `, ""},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			r := pedir(t, srv, "POST", "/v1/comprobantes", claveA, tc.clave, tc.cuerpo)
			esperarProblema(t, r, 400, "solicitud-invalida")
			errores, _ := r.cuerpo["errores"].([]any)
			if len(errores) == 0 {
				t.Fatalf("falta la lista de errores: %s", r.crudo)
			}
			if campo := errores[0].(map[string]any)["campo"]; campo != tc.campo {
				t.Errorf("campo %v, se esperaba %q", campo, tc.campo)
			}
		})
	}
}

func TestObtener(t *testing.T) {
	srv, _ := nuevoServidor(t)
	r := pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", cuerpoValido)
	esperarEstado(t, r, 201)
	id := r.cuerpo["id"].(string)

	propio := pedir(t, srv, "GET", "/v1/comprobantes/"+id, claveA, "", "")
	esperarEstado(t, propio, 200)
	if propio.cuerpo["id"] != id {
		t.Errorf("GET devolvió otro comprobante: %s", propio.crudo)
	}
	esperarProblema(t, pedir(t, srv, "GET", "/v1/comprobantes/"+id, claveB, "", ""), 404, "no-encontrado")
	esperarProblema(t, pedir(t, srv, "GET", "/v1/comprobantes/no-existe", claveA, "", ""), 404, "no-encontrado")
}

func TestDependenciaCaidaResponde503(t *testing.T) {
	srv, repo := nuevoServidor(t)
	repo.fallar = errors.New("MySQL no responde")

	r := pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-000123", cuerpoValido)
	esperarProblema(t, r, 503, "no-disponible")
	if r.headers.Get("Retry-After") == "" {
		t.Error("falta el header Retry-After")
	}
}

// repoColgado simula un MySQL que acepta la conexión pero nunca responde.
type repoColgado struct{ repoEnMemoria }

func (*repoColgado) Guardar(ctx context.Context, _ *domain.Comprobante) error {
	<-ctx.Done()
	return ctx.Err()
}

func (*repoColgado) BuscarPorID(ctx context.Context, _, _ string) (*domain.Comprobante, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDependenciaLentaCortaPorTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repoColgado{}
	srv := httptest.NewServer(api.NuevoRouter(api.Dependencias{
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		Emitir:              application.NuevoEmitirComprobante(repo),
		Obtener:             application.NuevoObtenerComprobante(repo),
		APIKeys:             map[string]string{claveA: "grupo-a"},
		VerificarSalud:      func(context.Context) error { return nil },
		TimeoutDependencias: 100 * time.Millisecond,
	}))
	t.Cleanup(srv.Close)

	inicio := time.Now()
	esperarProblema(t, pedir(t, srv, "POST", "/v1/comprobantes", claveA, "orden-lenta", cuerpoValido), 503, "no-disponible")
	esperarProblema(t, pedir(t, srv, "GET", "/v1/comprobantes/x", claveA, "", ""), 503, "no-disponible")
	if demora := time.Since(inicio); demora > 2*time.Second {
		t.Errorf("las solicitudes tardaron %v: el timeout no cortó la espera", demora)
	}
}

func TestLimiteDeSolicitudes(t *testing.T) {
	srv, _ := nuevoServidor(t)
	for i := 0; i < api.SolicitudesPorMinuto; i++ {
		esperarEstado(t, pedir(t, srv, "GET", "/v1/comprobantes/x", claveA, "", ""), 404)
	}

	r := pedir(t, srv, "GET", "/v1/comprobantes/x", claveA, "", "")
	esperarProblema(t, r, 429, "demasiadas-solicitudes")
	if r.headers.Get("Retry-After") == "" {
		t.Error("falta el header Retry-After")
	}
	// El límite es por consumidor: el grupo B no se ve afectado.
	esperarEstado(t, pedir(t, srv, "GET", "/v1/comprobantes/x", claveB, "", ""), 404)
}

func TestSaludSinAutenticacion(t *testing.T) {
	srv, _ := nuevoServidor(t)
	r := pedir(t, srv, "GET", "/health", "", "", "")
	esperarEstado(t, r, 200)
	if r.cuerpo["estado"] != "ok" {
		t.Errorf("cuerpo inesperado: %s", r.crudo)
	}
}

func TestEndpointErrorPruebaRegistraErrorYMetrica500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var salida bytes.Buffer
	srv := httptest.NewServer(api.NuevoRouter(api.Dependencias{
		Log:                  slog.New(slog.NewJSONHandler(&salida, nil)),
		APIKeys:              map[string]string{claveA: "grupo-a"},
		VerificarSalud:       func(context.Context) error { return nil },
		HabilitarErrorPrueba: true,
	}))
	t.Cleanup(srv.Close)

	r := pedir(t, srv, "GET", "/error", "", "", "")
	esperarProblema(t, r, http.StatusInternalServerError, "error-simulado")
	if !strings.Contains(salida.String(), `"level":"ERROR"`) ||
		!strings.Contains(salida.String(), `"msg":"error de prueba simulado"`) {
		t.Errorf("no se registró el evento de error estructurado: %s", salida.String())
	}

	respuestaMetricas, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("consultar métricas: %v", err)
	}
	defer respuestaMetricas.Body.Close()
	cuerpoMetricas, err := io.ReadAll(respuestaMetricas.Body)
	if err != nil {
		t.Fatalf("leer métricas: %v", err)
	}
	if !strings.Contains(string(cuerpoMetricas), `route="/error"`) ||
		!strings.Contains(string(cuerpoMetricas), `status_code="500"`) {
		t.Errorf("no se encontró el contador de /error con estado 500")
	}
}

func TestEndpointErrorPruebaDeshabilitadoPorDefecto(t *testing.T) {
	srv, _ := nuevoServidor(t)
	r := pedir(t, srv, "GET", "/error", "", "", "")
	esperarProblema(t, r, http.StatusNotFound, "no-encontrado")
}
