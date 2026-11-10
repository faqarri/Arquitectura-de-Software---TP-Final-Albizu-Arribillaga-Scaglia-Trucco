package handlers_test

// Test de contrato del lado del proveedor: cada respuesta de la API interna de
// Catálogo se valida contra docs/contracts/internos/catalogo-retenciones.yaml,
// el mismo archivo contra el que Reservas valida sus solicitudes. Si alguien
// cambia una respuesta sin actualizar el contrato, este test falla.
//
// Corre el router real con MySQL 8.4 y MongoDB 7 reales (testcontainers).
// Necesita Docker; con go test -short se saltea.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"agencia/catalogo/internal/handlers"
	"agencia/catalogo/internal/repository"
	"agencia/catalogo/internal/services"
	"agencia/pkg/estudiario"
)

const rutaContrato = "../../../../docs/contracts/internos/catalogo-retenciones.yaml"

// sinGuias reemplaza a Estudiario: este test no lo usa.
type sinGuias struct{}

func (sinGuias) Buscar(context.Context, estudiario.Busqueda) (estudiario.Pagina, error) {
	return estudiario.Pagina{}, estudiario.ErrNoDisponible
}

type entorno struct {
	t       *testing.T
	api     http.Handler
	routerC routers.Router
}

func nuevoEntorno(t *testing.T, guias ...buscadorGuias) *entorno {
	t.Helper()
	ctx := context.Background()
	cmy, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("catalogo"), tcmysql.WithUsername("catalogo"), tcmysql.WithPassword("prueba"))
	if err != nil {
		t.Fatalf("no se pudo levantar MySQL (¿Docker está corriendo?): %v", err)
	}
	t.Cleanup(func() { _ = cmy.Terminate(ctx) })
	cmo, err := tcmongo.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("no se pudo levantar MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = cmo.Terminate(ctx) })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	host, _ := cmy.Host(ctx)
	puerto, _ := cmy.MappedPort(ctx, "3306/tcp")
	sqlDB, err := repository.ConectarMySQL(ctx, repository.ConfigMySQL{
		Host: host, Puerto: puerto.Port(), Usuario: "catalogo", Password: "prueba", Nombre: "catalogo"}, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MigrarMySQL(ctx, sqlDB); err != nil {
		t.Fatal(err)
	}
	uri, err := cmo.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mongoDB, err := repository.ConectarMongo(ctx, uri, "catalogo", log)
	if err != nil {
		t.Fatal(err)
	}
	cache, _ := repository.NuevaCache(repository.ConfigCache{Habilitada: false}, log)

	ofertasRepo, packsRepo, inv := repository.NuevoOfertas(mongoDB), repository.NuevoPacks(mongoDB), repository.NuevoInventario(sqlDB)
	ofertas := services.NuevoOfertas(ofertasRepo, inv, cache, log)
	api := handlers.NuevoRouter(handlers.Dependencias{
		Log: log, Instancia: "prueba", Ofertas: ofertas,
		Packs:          services.NuevoPacks(packsRepo, ofertasRepo),
		Retenciones:    services.NuevoRetenciones(ofertasRepo, packsRepo, inv, services.ConfigRetenciones{Minutos: 15, MaxPorOferta: 6}),
		Guias:          services.NuevoGuias(ofertas, buscador(guias), cache, log),
		VerificarSalud: func(context.Context) error { return nil },
	})

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(rutaContrato)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("el contrato no es válido: %v", err)
	}
	routerC, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &entorno{t: t, api: api, routerC: routerC}
}

// llamar ejecuta la solicitud en el router real. Si la ruta es del contrato,
// valida la solicitud y la respuesta (incluido que el status esté documentado).
func (e *entorno) llamar(metodo, ruta, clave string, cuerpo any, headers ...string) (int, map[string]any) {
	e.t.Helper()
	var crudo []byte
	if cuerpo != nil {
		crudo, _ = json.Marshal(cuerpo)
	}
	nueva := func() *http.Request {
		r := httptest.NewRequest(metodo, "http://catalogo:8080"+ruta, bytes.NewReader(crudo))
		if cuerpo != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if clave != "" {
			r.Header.Set("Idempotency-Key", clave)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		return r
	}
	rec := httptest.NewRecorder()
	e.api.ServeHTTP(rec, nueva())

	if ruta, params, err := e.routerC.FindRoute(nueva()); err == nil {
		ctx := context.Background()
		entrada := &openapi3filter.RequestValidationInput{Request: nueva(), PathParams: params, Route: ruta}
		if err := openapi3filter.ValidateRequest(ctx, entrada); err != nil {
			e.t.Fatalf("%s %s: la solicitud del test no cumple el contrato: %v", metodo, ruta.Path, err)
		}
		salida := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: entrada, Status: rec.Code, Header: rec.Header(),
			Body:    io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
			Options: &openapi3filter.Options{IncludeResponseStatus: true},
		}
		if err := openapi3filter.ValidateResponse(ctx, salida); err != nil {
			e.t.Fatalf("%s %s → %d: la respuesta no cumple el contrato: %v\n%s", metodo, ruta.Path, rec.Code, err, rec.Body.String())
		}
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, res
}

func (e *entorno) crearOferta(tipo, desde, hasta string, cupo int) string {
	e.t.Helper()
	o := map[string]any{"tipo": tipo, "titulo": "Oferta " + tipo, "descripcion": "", "destino": "Bariloche",
		"desde": desde, "hasta": hasta, "precio": 100000.5, "cupoTotal": cupo}
	if tipo == "TRANSPORTE" {
		o["subtipoTransporte"], o["origen"] = "AVION", "Córdoba"
	}
	estado, res := e.llamar(http.MethodPost, "/api/ofertas", "", o, "X-Usuario-Rol", "admin")
	if estado != http.StatusCreated {
		e.t.Fatalf("crear oferta: %d %v", estado, res)
	}
	return res["id"].(string)
}

func esperar(t *testing.T, caso string, estado, esperado int, res map[string]any, code string) {
	t.Helper()
	if estado != esperado || (code != "" && res["code"] != code) {
		t.Fatalf("%s: obtuve %d %v, esperaba %d %s", caso, estado, res, esperado, code)
	}
}

func TestRespuestasCumplenElContratoInterno(t *testing.T) {
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
	e := nuevoEntorno(t)
	const post, del = http.MethodPost, http.MethodDelete
	vuelo := e.crearOferta("TRANSPORTE", "2026-12-07T11:30:00Z", "2026-12-07T13:45:00Z", 2)

	// Retener: 201, y el reintento devuelve el mismo grupo.
	clave := uuid.NewString()
	estado, g := e.llamar(post, "/internal/retenciones", clave, map[string]any{"titularId": "ana", "ofertaId": vuelo, "cantidad": 2})
	esperar(t, "retener", estado, 201, g, "")
	estado, g2 := e.llamar(post, "/internal/retenciones", clave, map[string]any{"titularId": "ana", "ofertaId": vuelo, "cantidad": 2})
	esperar(t, "reintento de retener", estado, 201, g2, "")
	if g["grupoId"] != g2["grupoId"] {
		t.Fatal("el reintento devolvió otro grupo")
	}
	retencion := g["retenciones"].([]any)[0].(map[string]any)["id"].(string)

	// Errores de retener.
	estado, res := e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": vuelo, "cantidad": 1})
	esperar(t, "sin cupo libre", estado, 409, res, "RETENIDA")
	grande := e.crearOferta("EXCURSION", "2026-12-09T12:00:00Z", "2026-12-09T21:00:00Z", 50)
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": grande, "cantidad": 5})
	esperar(t, "5 cupos", estado, 201, res, "")
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": grande, "cantidad": 2})
	esperar(t, "5 + 2 supera los 6 por carrito", estado, 422, res, "LIMITE_POR_CARRITO")
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": "of-no-existe", "cantidad": 1})
	esperar(t, "oferta inexistente", estado, 404, res, "OFERTA_INEXISTENTE")

	// Confirmar: 200, el reintento devuelve lo mismo, y después la oferta queda AGOTADA.
	compra := uuid.NewString()
	conf := map[string]any{"compraId": compra, "titularId": "ana", "retencionIds": []string{retencion}}
	estado, c1 := e.llamar(post, "/internal/retenciones/confirmacion", compra, conf)
	esperar(t, "confirmar", estado, 200, c1, "")
	estado, c2 := e.llamar(post, "/internal/retenciones/confirmacion", compra, conf)
	esperar(t, "reintento de confirmar", estado, 200, c2, "")
	if c1["total"] != c2["total"] || c1["total"] != 200001.0 {
		t.Fatalf("totales: %v y %v, esperaba 200001", c1["total"], c2["total"])
	}
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": vuelo, "cantidad": 1})
	esperar(t, "todo vendido", estado, 409, res, "AGOTADA")

	otra := uuid.NewString()
	estado, res = e.llamar(post, "/internal/retenciones/confirmacion", otra,
		map[string]any{"compraId": otra, "titularId": "ana", "retencionIds": []string{"no-existe"}})
	esperar(t, "confirmar una retención inexistente", estado, 409, res, "RETENCION_NO_VIGENTE")

	// Liberar: 204 siempre.
	estado, _ = e.llamar(del, "/internal/retenciones/"+retencion, "", nil)
	esperar(t, "liberar una confirmada", estado, 204, nil, "")
	estado, _ = e.llamar(del, "/internal/retenciones/no-existe", "", nil)
	esperar(t, "liberar una inexistente", estado, 204, nil, "")

	// Pack: todo o nada, y PACK_INVALIDO si falta un tipo.
	ida := e.crearOferta("TRANSPORTE", "2026-12-07T11:30:00Z", "2026-12-07T13:45:00Z", 5)
	hotel := e.crearOferta("ESTADIA", "2026-12-07T17:00:00Z", "2026-12-12T13:00:00Z", 5)
	paseo := e.crearOferta("EXCURSION", "2026-12-09T12:00:00Z", "2026-12-09T21:00:00Z", 5)
	estado, pack := e.llamar(post, "/api/packs", "",
		map[string]any{"titulo": "Bariloche", "destino": "Bariloche", "descuentoPorcentaje": 10, "opciones": []string{ida, hotel, paseo}},
		"X-Usuario-Rol", "admin")
	esperar(t, "crear pack", estado, 201, pack, "")
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(),
		map[string]any{"titularId": "carla", "packId": pack["id"], "opciones": []string{ida, hotel, paseo}, "cantidad": 1})
	esperar(t, "retener pack", estado, 201, res, "")
	if n := len(res["retenciones"].([]any)); n != 3 {
		t.Fatalf("el pack debería retener 3 opciones, retuvo %d", n)
	}
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(),
		map[string]any{"titularId": "carla", "packId": pack["id"], "opciones": []string{ida, hotel}, "cantidad": 1})
	esperar(t, "pack sin excursión", estado, 422, res, "PACK_INVALIDO")
}

// buscadorGuias es lo que services.NuevoGuias necesita de Estudiario.
type buscadorGuias interface {
	Buscar(context.Context, estudiario.Busqueda) (estudiario.Pagina, error)
}

func buscador(b []buscadorGuias) buscadorGuias {
	if len(b) > 0 {
		return b[0]
	}
	return sinGuias{}
}
