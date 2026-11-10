package hub

import "testing"

func TestEntregaSoloALasConexionesQueMiranEsaOferta(t *testing.T) {
	h := Nuevo()
	a := h.SuscribirOfertas([]string{"of-1", "of-2"})
	b := h.SuscribirOfertas([]string{"of-2"})
	defer h.Cancelar(a)
	defer h.Cancelar(b)

	if n := h.PublicarOferta("of-1", Evento{Nombre: "disponibilidad"}); n != 1 {
		t.Fatalf("of-1 entregó a %d; solo la mira a", n)
	}
	if n := h.PublicarOferta("of-2", Evento{Nombre: "disponibilidad"}); n != 2 {
		t.Fatalf("of-2 entregó a %d; la miran las dos", n)
	}
	if len(a.Eventos) != 2 || len(b.Eventos) != 1 {
		t.Fatalf("a=%d b=%d", len(a.Eventos), len(b.Eventos))
	}
}

func TestLosEventosDeUnUsuarioNoLleganAOtro(t *testing.T) {
	h := Nuevo()
	ana := h.SuscribirUsuario("u-ana")
	beto := h.SuscribirUsuario("u-beto")
	defer h.Cancelar(ana)
	defer h.Cancelar(beto)
	h.PublicarUsuario("u-ana", Evento{Nombre: "retencion-vencida"})
	if len(ana.Eventos) != 1 || len(beto.Eventos) != 0 {
		t.Fatalf("ana=%d beto=%d", len(ana.Eventos), len(beto.Eventos))
	}
}

func TestDespuesDeCancelarNoRecibeNada(t *testing.T) {
	h := Nuevo()
	s := h.SuscribirOfertas([]string{"of-1"})
	h.Cancelar(s)
	if n := h.PublicarOferta("of-1", Evento{}); n != 0 {
		t.Fatalf("entregó a %d conexiones canceladas", n)
	}
	if len(h.porOferta) != 0 {
		t.Error("el índice debía quedar vacío")
	}
}

func TestUnaConexionLentaNoFrenaALasDemas(t *testing.T) {
	h := Nuevo()
	lenta := h.SuscribirOfertas([]string{"of-1"})
	rapida := h.SuscribirOfertas([]string{"of-1"})
	defer h.Cancelar(lenta)
	defer h.Cancelar(rapida)
	for i := 0; i < TamanioBuffer+5; i++ {
		h.PublicarOferta("of-1", Evento{})
		<-rapida.Eventos // la rápida lee todo; la lenta nunca
	}
	if len(lenta.Eventos) != TamanioBuffer {
		t.Fatalf("la lenta tiene %d pendientes; se esperaba el buffer lleno y el resto descartado", len(lenta.Eventos))
	}
}
