package catalogo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"agencia/reservas/internal/ports"
)

const rutaContrato = "../../../../../docs/contracts/internos/catalogo-retenciones.yaml"

// servidor valida cada solicitud contra el contrato interno y responde con las
// respuestas dadas, en orden (la última se repite).
func servidor(t *testing.T, respuestas ...func(http.ResponseWriter)) (*Cliente, *int32) {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(rutaContrato)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("contrato interno inválido: %v", err)
	}
	doc.Servers = nil
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	var llamadas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ruta, params, err := router.FindRoute(r)
		if err == nil {
			err = openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: ruta})
		}
		if err != nil {
			t.Errorf("solicitud fuera del contrato interno: %v", err)
		}
		n := atomic.AddInt32(&llamadas, 1)
		respuestas[min(int(n), len(respuestas))-1](w)
	}))
	t.Cleanup(srv.Close)
	return NuevoCliente(Config{URL: srv.URL, Timeout: 200 * time.Millisecond, Esperas: []time.Duration{time.Millisecond, time.Millisecond}}), &llamadas
}

func responder(status int, cuerpo string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		tipo := "application/json"
		if status >= 400 {
			tipo = "application/problem+json"
		}
		w.Header().Set("Content-Type", tipo)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(cuerpo))
	}
}

const grupoCreado = `{"grupoId":"g1","expiraEn":"2026-11-09T15:45:00Z","retenciones":[
 {"id":"r1","ofertaId":"of-1","cantidad":2,"descripcion":"Vuelo","destino":"Bariloche","precioReferencia":185000.50}]}`

func TestRetenerOfertaSuelta(t *testing.T) {
	cli, _ := servidor(t, responder(201, grupoCreado))
	g, err := cli.Retener(context.Background(), ports.SolicitudRetencion{IdempotencyKey: "clave-123", TitularID: "u1", OfertaID: "of-1", Cantidad: 2})
	if err != nil {
		t.Fatal(err)
	}
	r := g.Retenciones[0]
	if r.ID != "r1" || r.Destino != "Bariloche" || r.PrecioReferencia.String() != "185000.5" || g.ExpiraEn.IsZero() {
		t.Errorf("g = %+v", g)
	}
}

func TestRetenerPack(t *testing.T) {
	cli, _ := servidor(t, responder(201, grupoCreado))
	if _, err := cli.Retener(context.Background(), ports.SolicitudRetencion{IdempotencyKey: "clave-123", TitularID: "u1", PackID: "p1", Opciones: []string{"a", "b", "c"}, Cantidad: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestRetenerSinCupoNoSeReintenta(t *testing.T) {
	cli, llamadas := servidor(t, responder(409, `{"title":"Sin cupo","status":409,"code":"RETENIDA","ofertaId":"of-1","proximaLiberacion":"2026-11-09T15:41:00Z"}`))
	_, err := cli.Retener(context.Background(), ports.SolicitudRetencion{IdempotencyKey: "clave-123", TitularID: "u1", OfertaID: "of-1", Cantidad: 1})
	var sinCupo *ports.ErrSinCupo
	if !errors.As(err, &sinCupo) || sinCupo.Estado != "RETENIDA" || sinCupo.ProximaLiberacion == nil {
		t.Fatalf("err = %v", err)
	}
	if *llamadas != 1 {
		t.Errorf("llamadas = %d; un 409 no se reintenta", *llamadas)
	}
}

func TestRetenerReglaIncumplida(t *testing.T) {
	cli, _ := servidor(t, responder(422, `{"title":"Regla","status":422,"code":"LIMITE_POR_CARRITO","detail":"máximo 6"}`))
	_, err := cli.Retener(context.Background(), ports.SolicitudRetencion{IdempotencyKey: "clave-123", TitularID: "u1", OfertaID: "of-1", Cantidad: 6})
	var regla *ports.ErrReglaCatalogo
	if !errors.As(err, &regla) || regla.Codigo != "LIMITE_POR_CARRITO" {
		t.Fatalf("err = %v", err)
	}
}

func TestReintentaUn5xxConLaMismaClave(t *testing.T) {
	cli, llamadas := servidor(t, responder(503, `{}`), responder(201, grupoCreado))
	if _, err := cli.Retener(context.Background(), ports.SolicitudRetencion{IdempotencyKey: "clave-123", TitularID: "u1", OfertaID: "of-1", Cantidad: 1}); err != nil {
		t.Fatal(err)
	}
	if *llamadas != 2 {
		t.Errorf("llamadas = %d", *llamadas)
	}
}

func TestSinRespuestaDespuesDeLosReintentosEsNoDisponible(t *testing.T) {
	cli, llamadas := servidor(t, func(w http.ResponseWriter) { time.Sleep(300 * time.Millisecond) })
	_, err := cli.Confirmar(context.Background(), "compra-123", "u1", []string{"r1"})
	if !errors.Is(err, ports.ErrCatalogoNoDisponible) {
		t.Fatalf("err = %v", err)
	}
	if *llamadas != 3 {
		t.Errorf("llamadas = %d; 1 intento + 2 reintentos", *llamadas)
	}
}

func TestConfirmar(t *testing.T) {
	cli, _ := servidor(t, responder(200, `{"compraId":"compra-123","lineas":[{"retencionId":"r1","ofertaId":"of-1","descripcion":"Vuelo x2","destino":"Bariloche","precio":371001.00}],"total":371001.00}`))
	lineas, err := cli.Confirmar(context.Background(), "compra-123", "u1", []string{"r1"})
	if err != nil || len(lineas) != 1 || lineas[0].Precio.String() != "371001" || lineas[0].Destino != "Bariloche" {
		t.Fatalf("lineas=%+v err=%v", lineas, err)
	}
}

func TestConfirmarConRetencionesVencidas(t *testing.T) {
	cli, _ := servidor(t, responder(409, `{"title":"No vigentes","status":409,"code":"RETENCION_NO_VIGENTE","retencionIds":["r1"]}`))
	_, err := cli.Confirmar(context.Background(), "compra-123", "u1", []string{"r1", "r2"})
	var nv *ports.ErrRetencionesNoVigentes
	if !errors.As(err, &nv) || len(nv.Retenciones) != 1 || nv.Retenciones[0] != "r1" {
		t.Fatalf("err = %v", err)
	}
}

func TestLiberar(t *testing.T) {
	cli, _ := servidor(t, func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
	if err := cli.Liberar(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
}
