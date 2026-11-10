package handlers_test

// Tests de la API pública de Catálogo con el router real y MySQL y MongoDB
// reales: administración de ofertas (HU-09), detalle y disponibilidad (HU-05),
// packs (HU-03, HU-10) y guías del destino con el proveedor caído (HU-14, ADR-009).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"agencia/pkg/estudiario"
)

// estudiarioDePrueba devuelve una guía, o falla como el proveedor caído.
type estudiarioDePrueba struct{ caido atomic.Bool }

func (s *estudiarioDePrueba) Buscar(_ context.Context, b estudiario.Busqueda) (estudiario.Pagina, error) {
	if s.caido.Load() {
		return estudiario.Pagina{}, estudiario.ErrCircuitoAbierto
	}
	cal := 4.6
	return estudiario.Pagina{Guias: []estudiario.Guia{{
		ID: "g1", Titulo: "Trekking en " + b.Destino, Autor: "Lucía", Calificacion: &cal, CantidadCalificaciones: 12,
	}}, Total: 1}, nil
}

func TestAPIPublica(t *testing.T) {
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
	prov := &estudiarioDePrueba{}
	e := nuevoEntorno(t, prov)
	admin := []string{"X-Usuario-Rol", "admin"}
	const get, post, put, del = http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete

	// Solo un admin escribe, y los datos se validan campo por campo.
	estado, res := e.llamar(post, "/api/ofertas", "", map[string]any{"tipo": "EXCURSION"})
	esperar(t, "crear sin ser admin", estado, 403, res, "prohibido")
	estado, res = e.llamar(post, "/api/ofertas", "", map[string]any{"tipo": "CRUCERO", "desde": "2026-12-07T11:30:00Z", "hasta": "2026-12-07T13:45:00Z"}, admin...)
	esperar(t, "oferta inválida", estado, 400, res, "solicitud-invalida")
	if errs, _ := res["errores"].([]any); len(errs) == 0 {
		t.Fatalf("la validación debería decir qué campo está mal: %v", res)
	}

	vuelo := e.crearOferta("TRANSPORTE", "2026-12-07T11:30:00Z", "2026-12-07T13:45:00Z", 5)

	// Detalle público: sin cupo ni vendidos. El admin sí los ve.
	rec := httptest.NewRecorder()
	e.api.ServeHTTP(rec, httptest.NewRequest(get, "/api/ofertas/"+vuelo, nil))
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" || rec.Header().Get("X-Instancia") != "prueba" {
		t.Fatalf("detalle: %d %v", rec.Code, rec.Header())
	}
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo, "", nil)
	if estado != 200 || res["cupoTotal"] != nil {
		t.Fatalf("el detalle público no muestra el cupo: %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo, "", nil, admin...)
	if estado != 200 || res["cupoTotal"] != 5.0 || res["vendidos"] != 0.0 {
		t.Fatalf("el admin ve cupo y vendidos: %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo+"/disponibilidad", "", nil)
	if estado != 200 || res["estado"] != "DISPONIBLE" || res["libres"] != 5.0 {
		t.Fatalf("disponibilidad: %d %v", estado, res)
	}

	// Editar: el cupo no puede bajar de lo ocupado (RN-01).
	estado, _ = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "ana", "ofertaId": vuelo, "cantidad": 3})
	esperar(t, "retener 3", estado, 201, nil, "")
	edicion := map[string]any{"tipo": "TRANSPORTE", "subtipoTransporte": "AVION", "origen": "Córdoba", "titulo": "Vuelo editado",
		"descripcion": "", "destino": "Bariloche", "desde": "2026-12-07T11:30:00Z", "hasta": "2026-12-07T13:45:00Z", "precio": 150000, "cupoTotal": 2}
	estado, res = e.llamar(put, "/api/ofertas/"+vuelo, "", edicion, admin...)
	esperar(t, "bajar el cupo por debajo de lo retenido", estado, 422, res, "CUPO_MENOR_AL_OCUPADO")
	edicion["cupoTotal"] = 4
	estado, res = e.llamar(put, "/api/ofertas/"+vuelo, "", edicion, admin...)
	if estado != 200 || res["titulo"] != "Vuelo editado" || res["precio"] != 150000.0 || res["version"] != 2.0 {
		t.Fatalf("editar: %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo+"/disponibilidad", "", nil)
	if res["libres"] != 1.0 {
		t.Fatalf("con cupo 4 y 3 retenidos queda 1 libre: %v", res)
	}

	// Guías del destino: con el proveedor caído, el detalle igual responde (HU-14).
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo+"/guias", "", nil)
	if estado != 200 || res["disponible"] != true || len(res["guias"].([]any)) != 1 {
		t.Fatalf("guías con el proveedor sano: %d %v", estado, res)
	}
	prov.caido.Store(true)
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo+"/guias", "", nil)
	if estado != 200 || res["disponible"] != false || len(res["guias"].([]any)) != 0 {
		t.Fatalf("con Estudiario caído: 200 y disponible = false; obtuve %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/ofertas/of-no-existe/guias", "", nil)
	esperar(t, "guías de una oferta inexistente", estado, 404, res, "no-encontrado")

	// Baja lógica: deja de verse y de poder retenerse, pero el admin la sigue viendo.
	estado, _ = e.llamar(del, "/api/ofertas/"+vuelo, "", nil, admin...)
	esperar(t, "despublicar", estado, 204, nil, "")
	estado, res = e.llamar(get, "/api/ofertas/"+vuelo, "", nil)
	esperar(t, "detalle de una despublicada", estado, 404, res, "no-encontrado")
	estado, res = e.llamar(post, "/internal/retenciones", uuid.NewString(), map[string]any{"titularId": "beto", "ofertaId": vuelo, "cantidad": 1})
	esperar(t, "retener una despublicada", estado, 404, res, "OFERTA_INEXISTENTE")
	estado, _ = e.llamar(get, "/api/ofertas/"+vuelo, "", nil, admin...)
	esperar(t, "el admin ve la despublicada", estado, 200, nil, "")

	// Packs: alta, detalle con sus opciones y listado.
	ida := e.crearOferta("TRANSPORTE", "2026-12-07T11:30:00Z", "2026-12-07T13:45:00Z", 5)
	hotel := e.crearOferta("ESTADIA", "2026-12-07T17:00:00Z", "2026-12-12T13:00:00Z", 5)
	paseo := e.crearOferta("EXCURSION", "2026-12-09T12:00:00Z", "2026-12-09T21:00:00Z", 5)
	estado, res = e.llamar(post, "/api/packs", "", map[string]any{"titulo": "Sin excursión", "destino": "Bariloche", "descuentoPorcentaje": 10, "opciones": []string{ida, hotel}}, admin...)
	esperar(t, "pack sin los tres tipos", estado, 400, res, "solicitud-invalida")
	estado, pack := e.llamar(post, "/api/packs", "", map[string]any{"titulo": "Bariloche", "destino": "Bariloche", "descuentoPorcentaje": 10, "opciones": []string{ida, hotel, paseo}}, admin...)
	esperar(t, "crear pack", estado, 201, pack, "")
	estado, res = e.llamar(get, "/api/packs/"+pack["id"].(string), "", nil)
	if estado != 200 || len(res["opciones"].([]any)) != 3 || res["descuentoPorcentaje"] != 10.0 {
		t.Fatalf("detalle del pack: %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/packs", "", nil)
	if estado != 200 || len(res["packs"].([]any)) != 1 {
		t.Fatalf("listado de packs: %d %v", estado, res)
	}
	estado, res = e.llamar(get, "/api/packs/pk-no-existe", "", nil)
	esperar(t, "pack inexistente", estado, 404, res, "no-encontrado")

	// Salud y métricas.
	estado, res = e.llamar(get, "/health", "", nil)
	if estado != 200 || res["estado"] != "ok" {
		t.Fatalf("health: %d %v", estado, res)
	}
}
