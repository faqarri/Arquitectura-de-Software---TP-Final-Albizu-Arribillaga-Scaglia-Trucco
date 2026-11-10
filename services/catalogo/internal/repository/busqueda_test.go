package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"agencia/catalogo/internal/models"
)

// Tests unitarios de la consulta (sin OpenSearch). Verifican las reglas de
// ADR-006: filtros en contexto filter, solo publicadas, desempate por id.

func consultaJSON(t *testing.T, f models.FiltrosBusqueda) string {
	t.Helper()
	if err := f.Validar(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(ConsultaBusqueda(f))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConsultaSoloPublicadasYOrdenPorFecha(t *testing.T) {
	q := consultaJSON(t, models.FiltrosBusqueda{})
	for _, parte := range []string{
		`"filter":[{"term":{"publicada":true}}]`,
		`"sort":[{"fechaInicio":"asc"},{"id":"asc"}]`,
		`"from":0`, `"size":20`, `"track_total_hits":1000`,
	} {
		if !strings.Contains(q, parte) {
			t.Errorf("falta %s en %s", parte, q)
		}
	}
	if strings.Contains(q, `"must"`) {
		t.Errorf("sin texto libre no hay consulta que puntúe: %s", q)
	}
}

func TestConsultaConTodosLosFiltros(t *testing.T) {
	q := consultaJSON(t, models.FiltrosBusqueda{
		Destino: "Bariloche ", Tipo: models.Transporte, SubtipoTransporte: models.Avion,
		Desde: "2026-07-01", Hasta: "2026-07-31", PrecioMin: ptr(100), PrecioMax: ptr(250000.5),
		Orden: models.OrdenPrecioDesc, Pagina: 3, Tamanio: 10,
	})
	for _, parte := range []string{
		`{"term":{"destino":"bariloche"}}`,
		`{"term":{"tipo":"TRANSPORTE"}}`,
		`{"term":{"subtipoTransporte":"AVION"}}`,
		`{"range":{"fechaInicio":{"gte":"2026-07-01","lte":"2026-07-31||/d"}}}`,
		`{"range":{"precio":{"gte":100,"lte":250000.5}}}`,
		`"sort":[{"precio":"desc"},{"id":"asc"}]`,
		`"from":20`, `"size":10`,
	} {
		if !strings.Contains(q, parte) {
			t.Errorf("falta %s en %s", parte, q)
		}
	}
}

func TestConsultaTextoLibreOrdenaPorRelevancia(t *testing.T) {
	q := consultaJSON(t, models.FiltrosBusqueda{Q: "cerro catedral"})
	if !strings.Contains(q, `"multi_match"`) || !strings.Contains(q, `"sort":["_score",{"id":"asc"}]`) {
		t.Fatalf("con q y sin orden se ordena por relevancia: %s", q)
	}
	q = consultaJSON(t, models.FiltrosBusqueda{Q: "cerro", Orden: models.OrdenPrecio})
	if !strings.Contains(q, `"sort":[{"precio":"asc"},{"id":"asc"}]`) {
		t.Fatalf("un orden pedido gana sobre la relevancia: %s", q)
	}
}

func ptr(f float64) *float64 { return &f }

func TestMapeoEsJSONValido(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(mapeoOfertas), &m); err != nil {
		t.Fatal(err)
	}
	cuerpo, err := cuerpoIndice("ofertas")
	if err != nil {
		t.Fatal(err)
	}
	var conAlias map[string]any
	if err := json.NewDecoder(cuerpo).Decode(&conAlias); err != nil || conAlias["aliases"] == nil {
		t.Fatalf("el índice inicial se crea con el alias en la misma operación: %v %v", conAlias, err)
	}
}
