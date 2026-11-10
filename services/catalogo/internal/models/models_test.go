package models

import (
	"errors"
	"testing"
	"time"
)

func fecha(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCalcularDisponibilidad(t *testing.T) {
	prox := fecha("2026-11-09T15:00")
	casos := []struct {
		nombre                     string
		total, vendidos, retenidos int
		estado                     string
		libres                     int
		conProxima                 bool
	}{
		{"con lugar", 10, 3, 2, Disponible, 5, false},
		{"todo retenido", 10, 6, 4, Retenida, 0, true},
		{"todo vendido", 10, 10, 0, Agotada, 0, false},
		{"nunca libres negativos", 5, 5, 3, Retenida, 0, true},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			d := CalcularDisponibilidad("of-1", c.total, c.vendidos, c.retenidos, &prox, 7)
			if d.Estado != c.estado || d.Libres != c.libres || d.Version != 7 {
				t.Fatalf("obtuve %+v", d)
			}
			if (d.ProximaLiberacion != nil) != c.conProxima {
				t.Fatalf("proximaLiberacion = %v", d.ProximaLiberacion)
			}
		})
	}
}

func TestVerificarCupo(t *testing.T) {
	prox := fecha("2026-11-09T15:00")

	if err := VerificarCupo("of-1", 2, 10, 5, 3, 0, 6, nil); err != nil {
		t.Fatalf("quedan 2 libres y pide 2: %v", err)
	}

	var sinCupo *ErrSinCupo
	err := VerificarCupo("of-1", 1, 10, 7, 3, 0, 6, &prox)
	if !errors.As(err, &sinCupo) || sinCupo.Codigo != CodigoRetenida || sinCupo.ProximaLiberacion == nil {
		t.Fatalf("todo retenido debería dar RETENIDA con próxima liberación: %v", err)
	}

	err = VerificarCupo("of-1", 1, 10, 10, 0, 0, 6, nil)
	if !errors.As(err, &sinCupo) || sinCupo.Codigo != CodigoAgotada {
		t.Fatalf("todo vendido debería dar AGOTADA: %v", err)
	}

	// RN-12: ya tiene 5 y pide 2 más, se pasa del máximo de 6.
	var regla *ErrRegla
	err = VerificarCupo("of-1", 2, 100, 0, 5, 5, 6, nil)
	if !errors.As(err, &regla) || regla.Codigo != CodigoLimitePorCarrito {
		t.Fatalf("debería superar el límite por carrito: %v", err)
	}
	if err := VerificarCupo("of-1", 1, 100, 0, 5, 5, 6, nil); err != nil {
		t.Fatalf("5 + 1 = 6 está permitido: %v", err)
	}
}

func ofertaValida() OfertaNueva {
	return OfertaNueva{
		Tipo: Transporte, SubtipoTransporte: Avion, Titulo: " Vuelo Córdoba - Bariloche ",
		Destino: "Bariloche", Origen: "Córdoba",
		Desde: fecha("2026-07-12T08:00"), Hasta: fecha("2026-07-12T10:30"),
		Precio: 185000.499, CupoTotal: 50,
	}
}

func TestValidarOferta(t *testing.T) {
	n := ofertaValida()
	if err := n.Validar(); err != nil {
		t.Fatalf("oferta válida rechazada: %v", err)
	}
	if n.Titulo != "Vuelo Córdoba - Bariloche" || n.Precio != 185000.5 {
		t.Fatalf("no normalizó título o precio: %q %v", n.Titulo, n.Precio)
	}

	invalidas := map[string]func(*OfertaNueva){
		"tipo":              func(n *OfertaNueva) { n.Tipo = "CRUCERO" },
		"subtipoTransporte": func(n *OfertaNueva) { n.SubtipoTransporte = "" },
		"origen":            func(n *OfertaNueva) { n.Origen = "" },
		"titulo":            func(n *OfertaNueva) { n.Titulo = "  " },
		"hasta":             func(n *OfertaNueva) { n.Hasta = n.Desde.Add(-time.Hour) },
		"precio":            func(n *OfertaNueva) { n.Precio = 0 },
		"cupoTotal":         func(n *OfertaNueva) { n.CupoTotal = 0 },
	}
	for campo, romper := range invalidas {
		t.Run(campo, func(t *testing.T) {
			n := ofertaValida()
			romper(&n)
			var v *ErrValidacion
			if err := n.Validar(); !errors.As(err, &v) || v.Errores[0].Campo != campo {
				t.Fatalf("esperaba error en %s, obtuve %v", campo, err)
			}
		})
	}

	estadia := OfertaNueva{Tipo: Estadia, SubtipoTransporte: Avion, Titulo: "Hotel", Destino: "Bariloche",
		Desde: fecha("2026-07-12T14:00"), Hasta: fecha("2026-07-15T10:00"), Precio: 1, CupoTotal: 1}
	if err := estadia.Validar(); err != nil || estadia.SubtipoTransporte != "" {
		t.Fatalf("una estadía no lleva subtipo ni origen: %v %q", err, estadia.SubtipoTransporte)
	}
}

// Pack de ejemplo: vuelo que llega el 12 a las 10:30, hotel del 12 al 15 y dos excursiones.
func packDeEjemplo() (Pack, map[string]Oferta) {
	o := map[string]Oferta{
		"vuelo":    {ID: "vuelo", Tipo: Transporte, Titulo: "Vuelo", Desde: fecha("2026-07-12T08:00"), Hasta: fecha("2026-07-12T10:30"), Precio: 100000},
		"hotel":    {ID: "hotel", Tipo: Estadia, Titulo: "Hotel", Desde: fecha("2026-07-12T14:00"), Hasta: fecha("2026-07-15T10:00"), Precio: 60000},
		"cerro":    {ID: "cerro", Tipo: Excursion, Titulo: "Cerro Catedral", Desde: fecha("2026-07-13T09:00"), Hasta: fecha("2026-07-13T17:00"), Precio: 20000},
		"tarde":    {ID: "tarde", Tipo: Excursion, Titulo: "Excursión tardía", Desde: fecha("2026-07-16T09:00"), Hasta: fecha("2026-07-16T12:00"), Precio: 15000},
		"temprano": {ID: "temprano", Tipo: Estadia, Titulo: "Hostel", Desde: fecha("2026-07-12T09:00"), Hasta: fecha("2026-07-14T10:00"), Precio: 30000},
	}
	p := Pack{ID: "pack", OpcionIDs: []string{"vuelo", "hotel", "cerro", "tarde", "temprano"}, DescuentoPorcentaje: 10}
	return p, o
}

func TestValidarEleccionDePack(t *testing.T) {
	p, o := packDeEjemplo()
	elegir := func(ids ...string) []Oferta {
		var r []Oferta
		for _, id := range ids {
			r = append(r, o[id])
		}
		return r
	}

	if err := ValidarEleccion(p, elegir("vuelo", "hotel", "cerro")); err != nil {
		t.Fatalf("elección coherente rechazada: %v", err)
	}

	casos := map[string][]Oferta{
		"RN-03: falta la excursión":                 elegir("vuelo", "hotel"),
		"RN-04: la estadía empieza antes de llegar": elegir("vuelo", "temprano", "cerro"),
		"RN-04: excursión fuera de la estadía":      elegir("vuelo", "hotel", "tarde"),
		"opción ajena al pack":                      append(elegir("vuelo", "hotel", "cerro"), Oferta{ID: "otra", Tipo: Excursion}),
	}
	for nombre, elegidas := range casos {
		t.Run(nombre, func(t *testing.T) {
			var regla *ErrRegla
			if err := ValidarEleccion(p, elegidas); !errors.As(err, &regla) || regla.Codigo != CodigoPackInvalido {
				t.Fatalf("esperaba PACK_INVALIDO, obtuve %v", err)
			}
		})
	}
}

func TestValidarOpcionesDelPack(t *testing.T) {
	_, o := packDeEjemplo()
	if err := ValidarOpcionesDelPack([]string{"vuelo", "hotel", "cerro"}, o); err != nil {
		t.Fatalf("opciones válidas rechazadas: %v", err)
	}
	var v *ErrValidacion
	if err := ValidarOpcionesDelPack([]string{"vuelo", "hotel"}, o); !errors.As(err, &v) {
		t.Fatalf("sin excursión debería fallar: %v", err)
	}
	if err := ValidarOpcionesDelPack([]string{"vuelo", "hotel", "cerro", "no-existe"}, o); !errors.As(err, &v) {
		t.Fatalf("una opción inexistente debería fallar: %v", err)
	}
}

func TestPrecioLinea(t *testing.T) {
	casos := []struct {
		unitario  float64
		cantidad  int
		descuento float64
		esperado  float64
	}{
		{185000.50, 2, 0, 370001.00}, // ejemplo del contrato interno (corregido)
		{100000, 1, 10, 90000},       // RN-05
		{0.1, 3, 0, 0.3},             // sin errores de coma flotante
		{33333.33, 1, 15, 28333.33},  // se redondea al centavo
	}
	for _, c := range casos {
		if got := PrecioLinea(c.unitario, c.cantidad, c.descuento); got != c.esperado {
			t.Errorf("PrecioLinea(%v, %d, %v) = %v, esperaba %v", c.unitario, c.cantidad, c.descuento, got, c.esperado)
		}
	}
}

func TestValidarPackNuevo(t *testing.T) {
	n := PackNuevo{Titulo: "Bariloche completo", Destino: "Bariloche", DescuentoPorcentaje: 10,
		Opciones: []string{"vuelo", "hotel", "cerro", "vuelo", " "}}
	if err := n.Validar(); err != nil {
		t.Fatalf("pack válido rechazado: %v", err)
	}
	if len(n.Opciones) != 3 {
		t.Fatalf("no quitó repetidos ni vacíos: %v", n.Opciones)
	}
	n.DescuentoPorcentaje = 95
	var v *ErrValidacion
	if err := n.Validar(); !errors.As(err, &v) {
		t.Fatal("un descuento de 95 % debería rechazarse")
	}
}
