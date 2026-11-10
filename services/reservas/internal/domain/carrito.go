// Package domain contiene las entidades y reglas de Reservas: carrito, compra
// y guías de viaje. No depende de HTTP, MySQL, RabbitMQ ni de otros servicios (ADR-002).
package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// EstadoItem es el estado de un ítem del carrito.
type EstadoItem string

const (
	ItemActivo  EstadoItem = "ACTIVO"
	ItemVencido EstadoItem = "VENCIDO"
)

var (
	ErrCarritoVacio     = errors.New("el carrito está vacío")
	ErrItemNoEncontrado = errors.New("el ítem no está en tu carrito")
	ErrSinEmail         = errors.New("la compra necesita el email del cliente")
	ErrSinRetenciones   = errors.New("Catálogo no devolvió retenciones")
)

// ItemCarrito es una oferta (o un pack) agregada al carrito, con el cupo
// retenido en Catálogo hasta ExpiraEn (ADR-014). Un pack tiene una retención
// por opción elegida, todas con el mismo vencimiento.
type ItemCarrito struct {
	ID               string
	UsuarioID        string
	IdempotencyKey   string
	OfertaID         string // oferta suelta; vacío si es un pack
	PackID           string
	Cantidad         int
	Descripcion      string
	Destino          string
	PrecioReferencia decimal.Decimal
	Retenciones      []string
	ExpiraEn         time.Time
	Estado           EstadoItem
	CreadoEn         time.Time
}

// Vigente indica si la retención del ítem todavía cuenta según nuestro reloj.
// La fuente de verdad es Catálogo: al confirmar puede responder que venció.
func (i ItemCarrito) Vigente(ahora time.Time) bool {
	return i.Estado == ItemActivo && ahora.Before(i.ExpiraEn)
}

// EstadoVisible es el estado que se muestra: un ítem activo cuyo vencimiento ya
// pasó se muestra VENCIDO aunque todavía no haya llegado RetencionVencida.
func (i ItemCarrito) EstadoVisible(ahora time.Time) EstadoItem {
	if i.Estado == ItemActivo && !i.Vigente(ahora) {
		return ItemVencido
	}
	return i.Estado
}

// Carrito es el carrito de un usuario (uno por usuario).
type Carrito struct {
	UsuarioID string
	Items     []ItemCarrito
}

func (c Carrito) Vacio() bool { return len(c.Items) == 0 }

// Retenciones devuelve las retenciones de todos los ítems, en orden.
func (c Carrito) Retenciones() []string {
	var ids []string
	for _, it := range c.Items {
		ids = append(ids, it.Retenciones...)
	}
	return ids
}

// ItemsDe devuelve los ítems que contienen alguna de las retenciones indicadas.
func (c Carrito) ItemsDe(retenciones []string) []ItemCarrito {
	buscadas := map[string]bool{}
	for _, r := range retenciones {
		buscadas[r] = true
	}
	var items []ItemCarrito
	for _, it := range c.Items {
		for _, r := range it.Retenciones {
			if buscadas[r] {
				items = append(items, it)
				break
			}
		}
	}
	return items
}

// TotalReferencia es la suma de los precios de referencia.
func (c Carrito) TotalReferencia() decimal.Decimal {
	total := decimal.Zero
	for _, it := range c.Items {
		total = total.Add(it.PrecioReferencia)
	}
	return total
}
