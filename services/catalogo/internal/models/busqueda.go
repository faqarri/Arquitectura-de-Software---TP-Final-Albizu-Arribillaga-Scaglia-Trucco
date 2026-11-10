package models

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Búsqueda de ofertas (HU-01, ADR-006). Las reglas de paginación y los filtros
// válidos viven acá; repository los traduce a una consulta de OpenSearch.

// OrdenBusqueda. Sin orden: por relevancia si hay texto libre, si no por fecha.
type OrdenBusqueda string

const (
	OrdenRelevancia OrdenBusqueda = ""
	OrdenFecha      OrdenBusqueda = "fecha"
	OrdenFechaDesc  OrdenBusqueda = "-fecha"
	OrdenPrecio     OrdenBusqueda = "precio"
	OrdenPrecioDesc OrdenBusqueda = "-precio"
)

const (
	TamanioPorDefecto = 20
	MaxTamanio        = 50
	MaxPaginas        = 50
	// MaxResultados es el límite de from + size (ADR-006): más allá se pide refinar.
	MaxResultados = 1000
	maxTextoLibre = 200
)

// ErrBusquedaNoDisponible: OpenSearch no responde o el índice todavía no
// existe. La búsqueda responde 503; el detalle, el carrito y la compra siguen.
var ErrBusquedaNoDisponible = errors.New("búsqueda no disponible")

// ErrReconstruccionEnCurso: ya hay una reconstrucción del índice corriendo en esta instancia.
var ErrReconstruccionEnCurso = errors.New("ya hay una reconstrucción en curso")

// FiltrosBusqueda son los parámetros de GET /api/busqueda. Desde y Hasta son
// fechas YYYY-MM-DD y filtran por el inicio de la oferta.
type FiltrosBusqueda struct {
	Q                 string
	Destino           string
	Tipo              TipoOferta
	SubtipoTransporte SubtipoTransporte
	Desde             string
	Hasta             string
	PrecioMin         *float64
	PrecioMax         *float64
	Orden             OrdenBusqueda
	Pagina            int
	Tamanio           int
}

// Validar completa los valores por defecto y revisa los filtros. Devuelve un
// *ErrValidacion con un mensaje por campo, o nil.
func (f *FiltrosBusqueda) Validar() error {
	f.Q = strings.TrimSpace(f.Q)
	f.Destino = strings.TrimSpace(f.Destino)
	f.Tipo = TipoOferta(strings.ToUpper(strings.TrimSpace(string(f.Tipo))))
	f.SubtipoTransporte = SubtipoTransporte(strings.ToUpper(strings.TrimSpace(string(f.SubtipoTransporte))))
	if f.Pagina == 0 {
		f.Pagina = 1
	}
	if f.Tamanio == 0 {
		f.Tamanio = TamanioPorDefecto
	}

	var errs []ErrorCampo
	agregar := func(campo, mensaje string) { errs = append(errs, ErrorCampo{Campo: campo, Mensaje: mensaje}) }

	if utf8.RuneCountInString(f.Q) > maxTextoLibre {
		agregar("q", "El texto a buscar tiene hasta 200 caracteres.")
	}
	switch f.Tipo {
	case "", Transporte, Estadia, Excursion:
	default:
		agregar("tipo", "El tipo debe ser TRANSPORTE, ESTADIA o EXCURSION.")
	}
	switch f.SubtipoTransporte {
	case "", Avion, Tren, Colectivo:
	default:
		agregar("subtipoTransporte", "El subtipo debe ser AVION, TREN o COLECTIVO.")
	}
	var desde, hasta time.Time
	if f.Desde != "" {
		var err error
		if desde, err = time.Parse(time.DateOnly, f.Desde); err != nil {
			agregar("desde", "La fecha va en formato AAAA-MM-DD.")
		}
	}
	if f.Hasta != "" {
		var err error
		if hasta, err = time.Parse(time.DateOnly, f.Hasta); err != nil {
			agregar("hasta", "La fecha va en formato AAAA-MM-DD.")
		}
	}
	if !desde.IsZero() && !hasta.IsZero() && hasta.Before(desde) {
		agregar("hasta", "La fecha hasta no puede ser anterior a la fecha desde.")
	}
	for campo, p := range map[string]*float64{"precioMin": f.PrecioMin, "precioMax": f.PrecioMax} {
		if p != nil && (*p < 0 || math.IsNaN(*p) || math.IsInf(*p, 0)) {
			agregar(campo, "El precio debe ser un número mayor o igual a cero.")
		}
	}
	if f.PrecioMin != nil && f.PrecioMax != nil && *f.PrecioMax < *f.PrecioMin {
		agregar("precioMax", "El precio máximo no puede ser menor que el mínimo.")
	}
	switch f.Orden {
	case OrdenRelevancia, OrdenFecha, OrdenFechaDesc, OrdenPrecio, OrdenPrecioDesc:
	default:
		agregar("orden", "El orden debe ser fecha, -fecha, precio o -precio.")
	}
	if f.Tamanio < 1 || f.Tamanio > MaxTamanio {
		agregar("tamanio", "El tamaño de página va de 1 a 50.")
	}
	switch {
	case f.Pagina < 1:
		agregar("pagina", "La página empieza en 1.")
	case f.Tamanio >= 1 && f.Tamanio <= MaxTamanio && (f.Pagina > MaxPaginas || f.Pagina*f.Tamanio > MaxResultados):
		agregar("pagina", "Solo se pueden recorrer los primeros 1000 resultados: refiná los filtros para encontrar lo que buscás.")
	}
	if len(errs) > 0 {
		return &ErrValidacion{Errores: errs}
	}
	return nil
}

// Desplazamiento es el from de la consulta.
func (f FiltrosBusqueda) Desplazamiento() int { return (f.Pagina - 1) * f.Tamanio }

// PaginaOfertas es la respuesta de la búsqueda. Total llega como máximo a
// MaxResultados (track_total_hits acotado).
type PaginaOfertas struct {
	Items   []Oferta `json:"items"`
	Pagina  int      `json:"pagina"`
	Tamanio int      `json:"tamanio"`
	Total   int      `json:"total"`
}

// ResultadoReconstruccion resume una reconstrucción completa del índice.
type ResultadoReconstruccion struct {
	Indice           string   `json:"indice"`
	Documentos       int      `json:"documentos"`
	Recuperados      int      `json:"recuperados"`
	IndicesBorrados  []string `json:"indicesBorrados"`
	DuracionSegundos float64  `json:"duracionSegundos"`
}

var sinAcentos = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "â", "a", "ê", "e", "ô", "o", "ç", "c",
)

// NormalizarDestino deja el destino en minúsculas, sin acentos y sin espacios
// repetidos: "  San Carlos de BARILOCHE " y "san carlos de bariloche" son el
// mismo destino. El índice aplica la misma normalización (lowercase + asciifolding).
func NormalizarDestino(s string) string {
	return sinAcentos.Replace(strings.Join(strings.Fields(strings.ToLower(s)), " "))
}
