// Package models reúne las entidades de Catálogo y las reglas de negocio que
// no dependen de la infraestructura (RN-03, RN-04, RN-05, RN-12, RN-13). Es la
// base de las tres capas: handlers, services y repository lo usan, y no importa
// a ninguna de ellas.
package models

import (
	"math"
	"strings"
	"time"
)

type TipoOferta string

const (
	Transporte TipoOferta = "TRANSPORTE"
	Estadia    TipoOferta = "ESTADIA"
	Excursion  TipoOferta = "EXCURSION"
)

// TiposDeOferta en el orden en que se informan los faltantes de un pack.
var TiposDeOferta = []TipoOferta{Transporte, Estadia, Excursion}

type SubtipoTransporte string

const (
	Avion     SubtipoTransporte = "AVION"
	Tren      SubtipoTransporte = "TREN"
	Colectivo SubtipoTransporte = "COLECTIVO"
)

// Oferta es lo que se vende: un transporte, una estadía o una excursión. La
// descripción vive en MongoDB; el cupo, en MySQL (inventario).
type Oferta struct {
	ID                string            `json:"id"`
	Tipo              TipoOferta        `json:"tipo"`
	SubtipoTransporte SubtipoTransporte `json:"subtipoTransporte,omitempty"`
	Titulo            string            `json:"titulo"`
	Descripcion       string            `json:"descripcion"`
	Destino           string            `json:"destino"`
	Origen            string            `json:"origen,omitempty"`
	Desde             time.Time         `json:"desde"`
	Hasta             time.Time         `json:"hasta"`
	Precio            float64           `json:"precio"`
	Publicada         bool              `json:"-"`
	Version           int64             `json:"version"`
	// ActualizadaEn es el instante de la última escritura: con él se mide el
	// retraso de la búsqueda (ADR-006).
	ActualizadaEn time.Time `json:"-"`
	// Solo para administradores: cambian con cada venta, por eso no se cachean.
	CupoTotal *int `json:"cupoTotal,omitempty"`
	Vendidos  *int `json:"vendidos,omitempty"`
}

// OfertaNueva son los datos que manda el administrador al crear o editar.
type OfertaNueva struct {
	Tipo              TipoOferta        `json:"tipo"`
	SubtipoTransporte SubtipoTransporte `json:"subtipoTransporte"`
	Titulo            string            `json:"titulo"`
	Descripcion       string            `json:"descripcion"`
	Destino           string            `json:"destino"`
	Origen            string            `json:"origen"`
	Desde             time.Time         `json:"desde"`
	Hasta             time.Time         `json:"hasta"`
	Precio            float64           `json:"precio"`
	CupoTotal         int               `json:"cupoTotal"`
}

// MaxCupoTotal acota el cupo de una oferta a un valor razonable.
const MaxCupoTotal = 100000

// Validar revisa los datos de una oferta y devuelve un error de validación con
// un mensaje por campo, o nil.
func (n *OfertaNueva) Validar() error {
	n.Titulo = strings.TrimSpace(n.Titulo)
	n.Destino = strings.TrimSpace(n.Destino)
	n.Origen = strings.TrimSpace(n.Origen)
	n.Descripcion = strings.TrimSpace(n.Descripcion)

	var errs []ErrorCampo
	agregar := func(campo, mensaje string) { errs = append(errs, ErrorCampo{Campo: campo, Mensaje: mensaje}) }

	switch n.Tipo {
	case Transporte:
		switch n.SubtipoTransporte {
		case Avion, Tren, Colectivo:
		default:
			agregar("subtipoTransporte", "Elegí avión, tren o colectivo.")
		}
		if n.Origen == "" {
			agregar("origen", "Indicá desde dónde sale el transporte.")
		}
	case Estadia, Excursion:
		n.SubtipoTransporte = ""
	default:
		agregar("tipo", "El tipo debe ser TRANSPORTE, ESTADIA o EXCURSION.")
	}
	if n.Titulo == "" || len(n.Titulo) > 200 {
		agregar("titulo", "El título es obligatorio y tiene hasta 200 caracteres.")
	}
	if n.Destino == "" || len(n.Destino) > 120 {
		agregar("destino", "El destino es obligatorio y tiene hasta 120 caracteres.")
	}
	if len(n.Descripcion) > 5000 {
		agregar("descripcion", "La descripción tiene hasta 5000 caracteres.")
	}
	if n.Desde.IsZero() || n.Hasta.IsZero() {
		agregar("desde", "Las fechas de inicio y fin son obligatorias.")
	} else if n.Hasta.Before(n.Desde) {
		agregar("hasta", "La fecha de fin no puede ser anterior a la de inicio.")
	}
	if n.Precio <= 0 || math.IsNaN(n.Precio) || math.IsInf(n.Precio, 0) {
		agregar("precio", "El precio debe ser mayor que cero.")
	}
	if n.CupoTotal < 1 || n.CupoTotal > MaxCupoTotal {
		agregar("cupoTotal", "El cupo debe estar entre 1 y 100000.")
	}
	if len(errs) > 0 {
		return &ErrValidacion{Errores: errs}
	}
	n.Precio = Redondear(n.Precio)
	n.Desde = n.Desde.UTC()
	n.Hasta = n.Hasta.UTC()
	return nil
}

// Aplicar copia los datos de la solicitud en la oferta.
func (o *Oferta) Aplicar(n OfertaNueva) {
	o.Tipo = n.Tipo
	o.SubtipoTransporte = n.SubtipoTransporte
	o.Titulo = n.Titulo
	o.Descripcion = n.Descripcion
	o.Destino = n.Destino
	o.Origen = n.Origen
	o.Desde = n.Desde
	o.Hasta = n.Hasta
	o.Precio = n.Precio
}

// Centavos convierte un importe en centavos para operar sin errores de coma flotante.
func Centavos(importe float64) int64 { return int64(math.Round(importe * 100)) }

// DesdeCentavos convierte centavos en un importe con 2 decimales.
func DesdeCentavos(c int64) float64 { return float64(c) / 100 }

// Redondear deja un importe con 2 decimales.
func Redondear(importe float64) float64 { return DesdeCentavos(Centavos(importe)) }

// CambioPendiente es una oferta modificada cuyo OfertaActualizada falta
// publicar (outbox dentro del documento, ADR-006).
type CambioPendiente struct {
	OfertaID string
	Version  int64
}
