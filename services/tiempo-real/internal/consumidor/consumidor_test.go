package consumidor

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"agencia/tiempo-real/internal/hub"
)

var logMudo = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestDisponibilidadCambiadaLlegaComoEventoDisponibilidad(t *testing.T) {
	h := hub.Nuevo()
	s := h.SuscribirOfertas([]string{"of-1"})
	defer h.Cancelar(s)

	Despachar(logMudo, h, []byte(`{"eventId":"e1","tipo":"DisponibilidadCambiada","ofertaId":"of-1",
		"estado":"RETENIDA","libres":0,"proximaLiberacion":"2026-12-01T10:15:00Z","version":7}`))

	e := <-s.Eventos
	if e.Nombre != "disponibilidad" {
		t.Fatalf("evento %q", e.Nombre)
	}
	var d map[string]any
	_ = json.Unmarshal(e.Datos, &d)
	if d["estado"] != "RETENIDA" || d["version"] != float64(7) || d["eventId"] != nil {
		t.Errorf("datos = %s", e.Datos)
	}
}

func TestRetencionVencidaLeLlegaSoloAlTitularSinSuID(t *testing.T) {
	h := hub.Nuevo()
	titular := h.SuscribirUsuario("u-1")
	otro := h.SuscribirUsuario("u-2")
	defer h.Cancelar(titular)
	defer h.Cancelar(otro)

	Despachar(logMudo, h, []byte(`{"eventId":"e2","tipo":"RetencionVencida","retencionId":"r-9",
		"ofertaId":"of-1","titularId":"u-1","expiraEn":"2026-12-01T10:15:00Z"}`))

	if len(otro.Eventos) != 0 {
		t.Fatal("el vencimiento le llegó a otro usuario")
	}
	e := <-titular.Eventos
	var r map[string]any
	_ = json.Unmarshal(e.Datos, &r)
	if e.Nombre != "retencion-vencida" || r["retencionId"] != "r-9" || r["titularId"] != nil {
		t.Errorf("evento %q datos %s", e.Nombre, e.Datos)
	}
}

func TestMensajesInvalidosODesconocidosSeDescartanSinRomper(t *testing.T) {
	h := hub.Nuevo()
	s := h.SuscribirOfertas([]string{"of-1"})
	defer h.Cancelar(s)
	for _, cuerpo := range []string{`no es json`, `{"tipo":"OtroEvento","ofertaId":"of-1"}`, `{"tipo":"DisponibilidadCambiada"}`} {
		Despachar(logMudo, h, []byte(cuerpo))
	}
	if len(s.Eventos) != 0 {
		t.Fatalf("se entregaron %d eventos inválidos", len(s.Eventos))
	}
}
