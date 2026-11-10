package models

import (
	"slices"
	"strings"
)

// Pack agrupa opciones de transporte, estadía y excursión de un destino con un
// descuento. El cliente elige al menos una de cada tipo (RN-03).
type Pack struct {
	ID                  string   `json:"id"`
	Titulo              string   `json:"titulo"`
	Destino             string   `json:"destino"`
	Descripcion         string   `json:"descripcion,omitempty"`
	DescuentoPorcentaje float64  `json:"descuentoPorcentaje"`
	OpcionIDs           []string `json:"-"`
	// Opciones se completa al leer el pack para la API pública.
	Opciones []Oferta `json:"opciones"`
}

type PackNuevo struct {
	Titulo              string   `json:"titulo"`
	Destino             string   `json:"destino"`
	Descripcion         string   `json:"descripcion"`
	DescuentoPorcentaje float64  `json:"descuentoPorcentaje"`
	Opciones            []string `json:"opciones"`
}

// MaxDescuentoPack evita un pack regalado por error de carga.
const MaxDescuentoPack = 90

// Validar revisa los datos propios del pack. Que las opciones existan y cubran
// los tres tipos se verifica con ValidarOpcionesDelPack, una vez leídas.
func (n *PackNuevo) Validar() error {
	n.Titulo = strings.TrimSpace(n.Titulo)
	n.Destino = strings.TrimSpace(n.Destino)
	n.Descripcion = strings.TrimSpace(n.Descripcion)
	var errs []ErrorCampo
	if n.Titulo == "" || len(n.Titulo) > 200 {
		errs = append(errs, ErrorCampo{"titulo", "El título es obligatorio y tiene hasta 200 caracteres."})
	}
	if n.Destino == "" {
		errs = append(errs, ErrorCampo{"destino", "El destino es obligatorio."})
	}
	if n.DescuentoPorcentaje < 0 || n.DescuentoPorcentaje > MaxDescuentoPack {
		errs = append(errs, ErrorCampo{"descuentoPorcentaje", "El descuento debe estar entre 0 y 90 %."})
	}
	n.Opciones = sinRepetidos(n.Opciones)
	if len(n.Opciones) < len(TiposDeOferta) {
		errs = append(errs, ErrorCampo{"opciones", "Elegí al menos una opción de cada tipo."})
	}
	if len(errs) > 0 {
		return &ErrValidacion{Errores: errs}
	}
	return nil
}

// ValidarOpcionesDelPack verifica que las opciones ofrecidas por un pack existan
// y cubran los tres tipos, para que el cliente siempre pueda armar uno válido.
func ValidarOpcionesDelPack(ids []string, encontradas map[string]Oferta) error {
	var faltantes []string
	for _, id := range ids {
		if _, ok := encontradas[id]; !ok {
			faltantes = append(faltantes, id)
		}
	}
	if len(faltantes) > 0 {
		return &ErrValidacion{Errores: []ErrorCampo{{"opciones", "No existen las ofertas: " + strings.Join(faltantes, ", ") + "."}}}
	}
	ofertas := make([]Oferta, 0, len(ids))
	for _, id := range ids {
		ofertas = append(ofertas, encontradas[id])
	}
	if faltan := tiposFaltantes(ofertas); len(faltan) > 0 {
		return &ErrValidacion{Errores: []ErrorCampo{{"opciones", "Falta al menos una opción de: " + nombres(faltan) + "."}}}
	}
	return nil
}

// ValidarEleccion aplica RN-03 y RN-04 a las opciones que eligió el cliente:
// todas deben ser del pack, al menos una de cada tipo, la estadía empieza en o
// después de la llegada del transporte y cada excursión cae dentro de una estadía.
func ValidarEleccion(p Pack, elegidas []Oferta) error {
	invalido := func(detalle string) error { return &ErrRegla{Codigo: CodigoPackInvalido, Detalle: detalle} }
	for _, o := range elegidas {
		if !slices.Contains(p.OpcionIDs, o.ID) {
			return invalido("La opción " + o.ID + " no pertenece al pack.")
		}
	}
	if faltan := tiposFaltantes(elegidas); len(faltan) > 0 {
		return invalido("Elegí al menos una opción de " + nombres(faltan) + ".")
	}
	de := func(t TipoOferta) []Oferta {
		var r []Oferta
		for _, o := range elegidas {
			if o.Tipo == t {
				r = append(r, o)
			}
		}
		return r
	}
	// RN-04: la llegada es la del último transporte elegido.
	llegada := de(Transporte)[0].Hasta
	for _, t := range de(Transporte) {
		if t.Hasta.After(llegada) {
			llegada = t.Hasta
		}
	}
	for _, e := range de(Estadia) {
		if e.Desde.Before(llegada) {
			return invalido("La estadía \"" + e.Titulo + "\" empieza antes de que llegue el transporte.")
		}
	}
	for _, x := range de(Excursion) {
		dentro := false
		for _, e := range de(Estadia) {
			if !x.Desde.Before(e.Desde) && !x.Hasta.After(e.Hasta) {
				dentro = true
				break
			}
		}
		if !dentro {
			return invalido("La excursión \"" + x.Titulo + "\" queda fuera de las fechas de la estadía.")
		}
	}
	return nil
}

// PrecioLinea es el precio de una línea: precio unitario por cantidad, menos el
// descuento del pack si corresponde (RN-05). Se redondea al centavo.
func PrecioLinea(precioUnitario float64, cantidad int, descuentoPorcentaje float64) float64 {
	bruto := Centavos(precioUnitario) * int64(cantidad)
	if descuentoPorcentaje <= 0 {
		return DesdeCentavos(bruto)
	}
	neto := float64(bruto) * (100 - descuentoPorcentaje) / 100
	return DesdeCentavos(int64(neto + 0.5))
}

func tiposFaltantes(ofertas []Oferta) []TipoOferta {
	var faltan []TipoOferta
	for _, t := range TiposDeOferta {
		if !slices.ContainsFunc(ofertas, func(o Oferta) bool { return o.Tipo == t }) {
			faltan = append(faltan, t)
		}
	}
	return faltan
}

func nombres(tipos []TipoOferta) string {
	nombre := map[TipoOferta]string{Transporte: "transporte", Estadia: "estadía", Excursion: "excursión"}
	partes := make([]string, len(tipos))
	for i, t := range tipos {
		partes[i] = nombre[t]
	}
	return strings.Join(partes, ", ")
}

func sinRepetidos(ids []string) []string {
	vistos := map[string]bool{}
	var r []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !vistos[id] {
			vistos[id] = true
			r = append(r, id)
		}
	}
	return r
}
