package estudiario

// Test de contrato de la capacidad de Estudiario (exigido por el enunciado para
// la capacidad consumida). Un mock construido a partir del YAML fijado:
//   - valida contra el contrato cada solicitud que arma nuestro cliente, y
//   - responde con los ejemplos del contrato (éxitos y cada error), que nuestro
//     cliente tiene que interpretar bien.
// Si el proveedor cambia el contrato de forma incompatible (y actualizamos la
// copia fijada), o si nuestro cliente manda algo fuera del contrato, falla.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

const (
	rutaContrato = "../../docs/contracts/consumidos/estudiario/1.0.0/documents-v1.yaml"
	rutaPDF      = "../../docs/contracts/consumidos/estudiario/1.0.0/guia-ejemplo.pdf"
	urlPublicado = "https://raw.githubusercontent.com/MichiSil/ProyectoEstudiario/main/docs/contracts/documents-v1.yaml"
	versionMayor = "1"
)

// Operaciones del contrato que usamos. Si alguna desaparece, la integración se rompe.
var operacionesUsadas = []string{"registerDocument", "searchDocuments", "listSubmissions"}

// Campos del documento que leemos y que el contrato garantiza (required).
var camposUsados = []string{"id", "title", "category", "tags", "status", "author", "rating", "file"}

func cargarContrato(t *testing.T, datos []byte) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(datos)
	if err != nil {
		t.Fatalf("el contrato no se puede leer: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("el contrato no es un OpenAPI válido: %v", err)
	}
	return doc
}

type mockContrato struct {
	t      *testing.T
	doc    *openapi3.T
	router routers.Router

	mu          sync.Mutex
	forzar      int // si no es 0, responde ese status
	violaciones []string
	recibidas   []*http.Request
}

func nuevoMockContrato(t *testing.T) (*mockContrato, *Cliente) {
	t.Helper()
	datos, err := os.ReadFile(rutaContrato)
	if err != nil {
		t.Fatal(err)
	}
	doc := cargarContrato(t, datos)
	doc.Servers = nil // que el router acepte la URL del servidor de prueba
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	m := &mockContrato{t: t, doc: doc, router: router}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		for _, v := range m.violaciones {
			t.Errorf("violación del contrato: %s", v)
		}
	})
	cli := NuevoCliente(Config{
		URL: srv.URL, APIKey: "clave-de-prueba",
		EsperasLectura: []time.Duration{}, EsperasRegistro: []time.Duration{}, // sin reintentos
		FallosParaAbrir: 1000,
	})
	return m, cli
}

func (m *mockContrato) violacion(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.violaciones = append(m.violaciones, fmt.Sprintf(format, args...))
}

func (m *mockContrato) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cuerpo, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(cuerpo))
	m.mu.Lock()
	m.recibidas = append(m.recibidas, r.Clone(ctx))
	forzar := m.forzar
	m.mu.Unlock()

	ruta, params, err := m.router.FindRoute(r)
	if err != nil {
		m.violacion("%s %s no existe en el contrato: %v", r.Method, r.URL.Path, err)
		http.Error(w, "fuera de contrato", http.StatusNotFound)
		return
	}
	entrada := &openapi3filter.RequestValidationInput{
		Request: r, PathParams: params, Route: ruta,
		Options: &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, a *openapi3filter.AuthenticationInput) error {
			if a.RequestValidationInput.Request.Header.Get("X-API-Key") == "" {
				return errors.New("falta X-API-Key")
			}
			return nil
		}},
	}
	if err := openapi3filter.ValidateRequest(ctx, entrada); err != nil {
		m.violacion("solicitud %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, "solicitud inválida", http.StatusBadRequest)
		return
	}

	status := forzar
	if status == 0 {
		status = estadoExitoso(ruta.Operation)
	}
	tipo, ejemplo := ejemploDe(ruta.Operation, status)
	if ejemplo == nil {
		m.violacion("%s no define la respuesta %d", ruta.Operation.OperationID, status)
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", tipo)
	if status == 409 || status == 429 || status == 503 {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_, _ = w.Write(ejemplo)

	// Los ejemplos del contrato tienen que cumplir su propio esquema.
	salida := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: entrada, Status: status, Header: w.Header(),
		Body: io.NopCloser(bytes.NewReader(ejemplo)),
	}
	if err := openapi3filter.ValidateResponse(ctx, salida); err != nil {
		m.violacion("el ejemplo %d de %s no cumple el contrato: %v", status, ruta.Operation.OperationID, err)
	}
}

func estadoExitoso(op *openapi3.Operation) int {
	for _, s := range []int{200, 201, 202} {
		if op.Responses.Status(s) != nil {
			return s
		}
	}
	return 200
}

// ejemploDe devuelve el tipo y el primer ejemplo (por nombre) de la respuesta status.
func ejemploDe(op *openapi3.Operation, status int) (string, []byte) {
	ref := op.Responses.Status(status)
	if ref == nil || ref.Value == nil {
		return "", nil
	}
	for _, tipo := range []string{"application/json", "application/problem+json"} {
		mt := ref.Value.Content.Get(tipo)
		if mt == nil {
			continue
		}
		valor := mt.Example
		if valor == nil && len(mt.Examples) > 0 {
			nombres := make([]string, 0, len(mt.Examples))
			for n := range mt.Examples {
				nombres = append(nombres, n)
			}
			sort.Strings(nombres)
			valor = mt.Examples[nombres[0]].Value.Value
		}
		b, _ := json.Marshal(valor)
		return tipo, b
	}
	return "", nil
}

func (m *mockContrato) responderCon(status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forzar = status
}

func TestContratoBuscarGuiasDeUnDestino(t *testing.T) {
	_, cli := nuevoMockContrato(t)
	p, err := cli.Buscar(context.Background(), Busqueda{
		Destino: "Patagonia", Etiquetas: []string{"trekking"}, CalificacionMin: 4,
		Orden: OrdenCalificacion, Pagina: 1, Tamanio: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Guias) == 0 || p.Total == 0 {
		t.Fatalf("página vacía: %+v", p)
	}
	g := p.Guias[0]
	if g.ID == "" || g.Titulo == "" || g.Destino == "" || g.Estado != EstadoPublicada || g.RutaDescarga == "" {
		t.Errorf("guía mal traducida: %+v", g)
	}
}

func TestContratoRegistrarUnaGuia(t *testing.T) {
	m, cli := nuevoMockContrato(t)
	pdf, err := os.ReadFile(rutaPDF)
	if err != nil {
		t.Fatal(err)
	}
	g, err := cli.Registrar(context.Background(), Registro{
		IdempotencyKey: "3f8a9c1e-5b7d-4e8a-9c21-7d4e5f6a8b90",
		Titulo:         "Guía de trekking en los glaciares",
		Descripcion:    "Recorridos y refugios.",
		Destino:        "Patagonia",
		Etiquetas:      []string{"trekking", "invierno"},
		Autor:          "Lucía Fernández",
		AutorExternoID: "viajero-1842",
		NombreArchivo:  "guia.pdf",
		PDF:            pdf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Estado != EstadoPendiente || g.ID == "" {
		t.Errorf("guía registrada = %+v; se esperaba PENDIENTE con id", g)
	}
	if r := m.recibidas[0]; r.Header.Get("Idempotency-Key") == "" {
		t.Error("falta Idempotency-Key")
	}
}

func TestContratoListarRegistradas(t *testing.T) {
	_, cli := nuevoMockContrato(t)
	p, err := cli.Registradas(context.Background(), EstadoRechazada, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range p.Guias {
		if g.Estado == EstadoRechazada && g.MotivoRechazo == "" {
			t.Errorf("guía rechazada sin motivo: %+v", g)
		}
	}
}

// Cada error documentado se interpreta como corresponde: los reintentables
// como ErrNoDisponible y los de negocio como *ErrorRechazo.
func TestContratoErroresDocumentados(t *testing.T) {
	pdf, _ := os.ReadFile(rutaPDF)
	registro := Registro{IdempotencyKey: "clave-12345678", Titulo: "Guía", Destino: "Salta", Autor: "Ana", PDF: pdf}

	casos := []struct {
		operacion string
		status    int
		esperado  func(error) bool
	}{
		{"registrar", 401, esNoDisponible}, // es nuestra clave, no los datos de la guía
		{"registrar", 409, esNoDisponible},
		{"registrar", 413, esRechazo},
		{"registrar", 415, esRechazo},
		{"registrar", 422, esRechazo},
		{"registrar", 429, esNoDisponible},
		{"registrar", 503, esNoDisponible},
		{"buscar", 429, esNoDisponible},
		{"buscar", 503, esNoDisponible},
		{"registradas", 503, esNoDisponible},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("%s %d", c.operacion, c.status), func(t *testing.T) {
			m, cli := nuevoMockContrato(t)
			m.responderCon(c.status)
			var err error
			switch c.operacion {
			case "registrar":
				_, err = cli.Registrar(context.Background(), registro)
			case "buscar":
				_, err = cli.Buscar(context.Background(), Busqueda{Destino: "Salta"})
			case "registradas":
				_, err = cli.Registradas(context.Background(), "", 0, 0)
			}
			if !c.esperado(err) {
				t.Errorf("error %v mal interpretado", err)
			}
			var r *ErrorRechazo
			if errors.As(err, &r) && r.Codigo == "" {
				t.Errorf("rechazo sin code del proveedor: %+v", r)
			}
		})
	}
}

func esRechazo(err error) bool {
	var r *ErrorRechazo
	return errors.As(err, &r)
}

func esNoDisponible(err error) bool { return errors.Is(err, ErrNoDisponible) }

// La copia fijada sigue en la versión mayor con la que integramos y conserva lo que usamos.
func TestContratoFijadoEnVersionMayor(t *testing.T) {
	datos, err := os.ReadFile(rutaContrato)
	if err != nil {
		t.Fatal(err)
	}
	verificarCompatibilidad(t, cargarContrato(t, datos))
}

// Compara contra el contrato que el proveedor publica hoy. Detecta un cambio
// incompatible antes de que llegue a producción. Necesita internet: se saltea
// con -short o sin conexión.
func TestContratoPublicadoSigueCompatible(t *testing.T) {
	if testing.Short() {
		t.Skip("necesita internet: se saltea con -short")
	}
	cli := &http.Client{Timeout: 5 * time.Second}
	res, err := cli.Get(urlPublicado)
	if err != nil {
		t.Skipf("no se pudo descargar el contrato publicado: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Skipf("el contrato publicado respondió %d", res.StatusCode)
	}
	datos, _ := io.ReadAll(res.Body)
	verificarCompatibilidad(t, cargarContrato(t, datos))
}

func verificarCompatibilidad(t *testing.T, doc *openapi3.T) {
	t.Helper()
	if mayor, _, _ := strings.Cut(doc.Info.Version, "."); mayor != versionMayor {
		t.Errorf("el contrato está en la versión %s; integramos la %s.x: revisar la integración antes de actualizar", doc.Info.Version, versionMayor)
	}
	ids := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			ids[op.OperationID] = true
		}
	}
	for _, id := range operacionesUsadas {
		if !ids[id] {
			t.Errorf("la operación %s ya no está en el contrato", id)
		}
	}
	documento := doc.Components.Schemas["Document"]
	if documento == nil || documento.Value == nil {
		t.Fatal("el contrato ya no define el esquema Document")
	}
	requeridos := map[string]bool{}
	for _, r := range documento.Value.Required {
		requeridos[r] = true
	}
	for _, c := range camposUsados {
		if !requeridos[c] {
			t.Errorf("el campo %s del documento dejó de ser obligatorio", c)
		}
	}
}
