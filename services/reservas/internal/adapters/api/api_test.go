package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"agencia/reservas/internal/adapters/api"
	"agencia/reservas/internal/application"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// ---------- dobles mínimos ----------

type repo struct {
	mu      sync.Mutex
	items   []domain.ItemCarrito
	compras []*domain.Compra
	guias   []*domain.Guia
}

func (r *repo) Obtener(_ context.Context, u string) (domain.Carrito, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := domain.Carrito{UsuarioID: u}
	for _, it := range r.items {
		if it.UsuarioID == u {
			c.Items = append(c.Items, it)
		}
	}
	return c, nil
}
func (r *repo) Agregar(_ context.Context, it domain.ItemCarrito) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, it)
	return nil
}
func (r *repo) BuscarPorClave(_ context.Context, u, k string) (domain.ItemCarrito, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range r.items {
		if it.UsuarioID == u && it.IdempotencyKey == k {
			return it, nil
		}
	}
	return domain.ItemCarrito{}, ports.ErrNoEncontrado
}
func (r *repo) Quitar(_ context.Context, u, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = slices.DeleteFunc(r.items, func(it domain.ItemCarrito) bool { return it.UsuarioID == u && it.ID == id })
	return nil
}
func (r *repo) MarcarVencida(context.Context, string) error { return nil }

type compras struct{ *repo }

func (r compras) Crear(_ context.Context, c *domain.Compra) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copia := *c
	r.compras = append(r.compras, &copia)
	return nil
}
func (r compras) BuscarPorClave(_ context.Context, u, k string) (*domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.compras {
		if c.UsuarioID == u && c.IdempotencyKey == k {
			copia := *c
			return &copia, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}
func (r compras) Obtener(_ context.Context, u, id string) (*domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.compras {
		if c.UsuarioID == u && c.ID == id {
			copia := *c
			return &copia, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}
func (r compras) Listar(_ context.Context, u string) ([]domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Compra
	for _, c := range r.compras {
		if c.UsuarioID == u {
			res = append(res, *c)
		}
	}
	return res, nil
}
func (r compras) Resolver(_ context.Context, c *domain.Compra) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.compras {
		if e.ID == c.ID {
			*e = *c
		}
	}
	return true, nil
}
func (r compras) Pendientes(context.Context, time.Time, int) ([]domain.Compra, error) {
	return nil, nil
}

type guias struct{ *repo }

func (r guias) Crear(_ context.Context, g *domain.Guia, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copia := *g
	r.guias = append(r.guias, &copia)
	return nil
}
func (r guias) Actualizar(context.Context, *domain.Guia) error                 { return nil }
func (r guias) PorEnviar(context.Context, int) ([]ports.GuiaConArchivo, error) { return nil, nil }
func (r guias) PendientesDeModeracion(context.Context) ([]domain.Guia, error)  { return nil, nil }
func (r guias) Listar(_ context.Context, u string) ([]domain.Guia, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Guia
	for _, g := range r.guias {
		if g.UsuarioID == u {
			res = append(res, *g)
		}
	}
	return res, nil
}

type catalogo struct {
	errRetener error
	errConfirm error
}

func (c *catalogo) Retener(_ context.Context, s ports.SolicitudRetencion) (ports.GrupoRetenido, error) {
	if c.errRetener != nil {
		return ports.GrupoRetenido{}, c.errRetener
	}
	return ports.GrupoRetenido{ExpiraEn: time.Now().Add(15 * time.Minute), Retenciones: []ports.Retencion{{
		ID: uuid.NewString(), OfertaID: s.OfertaID, Cantidad: s.Cantidad, Descripcion: "Vuelo", Destino: "Bariloche",
		PrecioReferencia: decimal.RequireFromString("1500.5"),
	}}}, nil
}
func (c *catalogo) Liberar(context.Context, string) error { return nil }
func (c *catalogo) Confirmar(_ context.Context, _, _ string, rs []string) ([]domain.LineaCompra, error) {
	if c.errConfirm != nil {
		return nil, c.errConfirm
	}
	return []domain.LineaCompra{{RetencionID: rs[0], Descripcion: "Vuelo", Destino: "Bariloche", Precio: decimal.RequireFromString("1500.5")}}, nil
}

type externas struct{}

func (externas) Registrar(context.Context, domain.Guia, []byte) (string, error) {
	return "", ports.ErrGuiasNoDisponible
}
func (externas) Moderacion(context.Context) ([]ports.EstadoModeracion, error) { return nil, nil }

// ---------- entorno ----------

type entorno struct {
	srv *httptest.Server
	cat *catalogo
}

func nuevoEntorno(t *testing.T) entorno {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &repo{}
	cat := &catalogo{}
	srv := httptest.NewServer(api.NuevoRouter(api.Dependencias{
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Carritos:       application.NuevoCarritos(r, cat),
		Compras:        application.NuevoCompras(compras{r}, r, cat),
		Guias:          application.NuevoGuias(guias{r}, compras{r}, externas{}),
		VerificarSalud: func(context.Context) error { return nil },
	}))
	t.Cleanup(srv.Close)
	return entorno{srv: srv, cat: cat}
}

type resp struct {
	estado int
	cuerpo map[string]any
	header http.Header
}

func (e entorno) pedir(t *testing.T, metodo, ruta, usuario string, cuerpo io.Reader, headers map[string]string) resp {
	t.Helper()
	req, _ := http.NewRequest(metodo, e.srv.URL+ruta, cuerpo)
	req.Header.Set("Content-Type", "application/json")
	if usuario != "" {
		req.Header.Set("X-Usuario-Id", usuario)
		req.Header.Set("X-Usuario-Email", usuario+"@example.com")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	r := resp{estado: res.StatusCode, header: res.Header}
	_ = json.NewDecoder(res.Body).Decode(&r.cuerpo)
	return r
}

func esperar(t *testing.T, r resp, estado int, code string) {
	t.Helper()
	if r.estado != estado {
		t.Fatalf("estado %d, se esperaba %d: %v", r.estado, estado, r.cuerpo)
	}
	if code != "" && r.cuerpo["code"] != code {
		t.Fatalf("code %v, se esperaba %s", r.cuerpo["code"], code)
	}
}

func json1(s string) io.Reader { return strings.NewReader(s) }

// ---------- tests ----------

func TestSinIdentidadResponde401(t *testing.T) {
	e := nuevoEntorno(t)
	esperar(t, e.pedir(t, "GET", "/api/carrito", "", nil, nil), 401, "no-autenticado")
}

func TestSaludSinIdentidad(t *testing.T) {
	e := nuevoEntorno(t)
	esperar(t, e.pedir(t, "GET", "/health", "", nil, nil), 200, "")
}

func TestAgregarVerYQuitar(t *testing.T) {
	e := nuevoEntorno(t)
	r := e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1","cantidad":2}`), map[string]string{"Idempotency-Key": "agregar-0001"})
	esperar(t, r, 201, "")
	if r.cuerpo["precioReferencia"] != 1500.5 || r.cuerpo["estado"] != "ACTIVO" || r.cuerpo["destino"] != "Bariloche" {
		t.Errorf("ítem = %v", r.cuerpo)
	}
	id := r.cuerpo["id"].(string)

	// Reintento con la misma clave: 200 y el mismo ítem.
	r2 := e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1","cantidad":2}`), map[string]string{"Idempotency-Key": "agregar-0001"})
	esperar(t, r2, 200, "")
	if r2.cuerpo["id"] != id {
		t.Errorf("el reintento creó otro ítem")
	}

	carrito := e.pedir(t, "GET", "/api/carrito", "u1", nil, nil)
	if len(carrito.cuerpo["items"].([]any)) != 1 || carrito.cuerpo["totalReferencia"] != 1500.5 {
		t.Errorf("carrito = %v", carrito.cuerpo)
	}
	esperar(t, e.pedir(t, "DELETE", "/api/carrito/items/"+id, "u2", nil, nil), 404, "no-encontrado")
	esperar(t, e.pedir(t, "DELETE", "/api/carrito/items/"+id, "u1", nil, nil), 204, "")
}

func TestAgregarErrores(t *testing.T) {
	e := nuevoEntorno(t)
	esperar(t, e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"cantidad":1}`), nil), 400, "solicitud-invalida")
	esperar(t, e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"o","extra":1}`), nil), 400, "solicitud-invalida")
	esperar(t, e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"o"}`), map[string]string{"Idempotency-Key": "corta"}), 400, "solicitud-invalida")

	proxima := time.Now().Add(5 * time.Minute)
	e.cat.errRetener = &ports.ErrSinCupo{Estado: "RETENIDA", OfertaID: "o", ProximaLiberacion: &proxima}
	r := e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"o"}`), nil)
	esperar(t, r, 409, "sin-cupo")
	if r.cuerpo["estado"] != "RETENIDA" || r.cuerpo["proximaLiberacion"] == nil {
		t.Errorf("cuerpo = %v", r.cuerpo)
	}

	e.cat.errRetener = &ports.ErrReglaCatalogo{Codigo: "LIMITE_POR_CARRITO", Detalle: "máximo 6"}
	esperar(t, e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"o"}`), nil), 422, "regla-incumplida")

	e.cat.errRetener = ports.ErrCatalogoNoDisponible
	r = e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"o"}`), nil)
	esperar(t, r, 503, "no-disponible")
	if r.header.Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
}

func TestComprar(t *testing.T) {
	e := nuevoEntorno(t)
	esperar(t, e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-0001"}), 422, "carrito-vacio")
	e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1"}`), nil)

	esperar(t, e.pedir(t, "POST", "/api/compras", "u1", nil, nil), 400, "solicitud-invalida")
	r := e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-0002"})
	esperar(t, r, 201, "")
	if r.cuerpo["estado"] != "CONFIRMADA" || r.cuerpo["total"] != 1500.5 {
		t.Errorf("compra = %v", r.cuerpo)
	}
	id := r.cuerpo["id"].(string)

	esperar(t, e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-0002"}), 200, "")
	esperar(t, e.pedir(t, "GET", "/api/compras/"+id, "u1", nil, nil), 200, "")
	esperar(t, e.pedir(t, "GET", "/api/compras/"+id, "u2", nil, nil), 404, "no-encontrado")
	if l := e.pedir(t, "GET", "/api/compras", "u1", nil, nil); len(l.cuerpo["compras"].([]any)) != 1 {
		t.Errorf("compras = %v", l.cuerpo)
	}
}

func TestComprarConCatalogoCaidoResponde202Pendiente(t *testing.T) {
	e := nuevoEntorno(t)
	e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1"}`), nil)
	e.cat.errConfirm = ports.ErrCatalogoNoDisponible
	r := e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-0003"})
	esperar(t, r, 202, "")
	if r.cuerpo["estado"] != "PENDIENTE" || r.header.Get("Location") == "" {
		t.Errorf("r = %v %v", r.cuerpo, r.header)
	}
}

func TestComprarRechazadaResponde409(t *testing.T) {
	e := nuevoEntorno(t)
	e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1"}`), nil)
	e.cat.errConfirm = &ports.ErrRetencionesNoVigentes{Retenciones: []string{"r1"}}
	r := e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-0004"})
	esperar(t, r, 409, "")
	if r.cuerpo["estado"] != "RECHAZADA" || len(r.cuerpo["retencionesVencidas"].([]any)) != 1 {
		t.Errorf("r = %v", r.cuerpo)
	}
}

func formularioGuia(t *testing.T, destino string, contenido []byte) (io.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("titulo", "Trekking en el Catedral")
	_ = w.WriteField("destino", destino)
	_ = w.WriteField("autor", "Ana")
	_ = w.WriteField("etiquetas", "trekking,invierno")
	f, _ := w.CreateFormFile("archivo", "guia.pdf")
	_, _ = f.Write(contenido)
	_ = w.Close()
	return &b, w.FormDataContentType()
}

func TestCompartirGuia(t *testing.T) {
	e := nuevoEntorno(t)
	cuerpo, tipo := formularioGuia(t, "Bariloche", []byte("%PDF-1.4"))
	esperar(t, e.pedir(t, "POST", "/api/guias", "u1", cuerpo, map[string]string{"Content-Type": tipo}), 403, "destino-no-comprado")

	e.pedir(t, "POST", "/api/carrito/items", "u1", json1(`{"ofertaId":"of-1"}`), nil)
	e.pedir(t, "POST", "/api/compras", "u1", nil, map[string]string{"Idempotency-Key": "compra-guia-1"})

	cuerpo, tipo = formularioGuia(t, "Bariloche\xe9", []byte("%PDF-1.4")) // Windows-1252, no UTF-8
	esperar(t, e.pedir(t, "POST", "/api/guias", "u1", cuerpo, map[string]string{"Content-Type": tipo}), 400, "solicitud-invalida")

	cuerpo, tipo = formularioGuia(t, "bariloche", []byte("no es pdf"))
	esperar(t, e.pedir(t, "POST", "/api/guias", "u1", cuerpo, map[string]string{"Content-Type": tipo}), 415, "archivo-invalido")

	// Estudiario no responde (doble): la guía queda POR_ENVIAR y el cliente recibe 202 igual.
	cuerpo, tipo = formularioGuia(t, "bariloche", []byte("%PDF-1.4"))
	r := e.pedir(t, "POST", "/api/guias", "u1", cuerpo, map[string]string{"Content-Type": tipo})
	esperar(t, r, 202, "")
	if r.cuerpo["estado"] != "POR_ENVIAR" || len(r.cuerpo["etiquetas"].([]any)) != 2 {
		t.Errorf("guía = %v", r.cuerpo)
	}
	if l := e.pedir(t, "GET", "/api/guias", "u1", nil, nil); len(l.cuerpo["guias"].([]any)) != 1 {
		t.Errorf("guías = %v", l.cuerpo)
	}
}
