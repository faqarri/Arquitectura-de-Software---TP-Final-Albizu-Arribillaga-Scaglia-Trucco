package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EstadoCompra es el estado de una compra (SPEC HU-06).
type EstadoCompra string

const (
	CompraPendiente  EstadoCompra = "PENDIENTE"  // esperando una respuesta concluyente de Catálogo
	CompraConfirmada EstadoCompra = "CONFIRMADA" // cupos vendidos
	CompraRechazada  EstadoCompra = "RECHAZADA"  // alguna retención venció; no se vendió nada
)

var (
	ErrTransicionInvalida = errors.New("la compra ya está resuelta")
	ErrSinLineas          = errors.New("una compra confirmada necesita al menos una línea")
)

// LineaCompra es una línea con el precio que se cobró (lo fija Catálogo al confirmar, RN-06).
type LineaCompra struct {
	RetencionID string
	OfertaID    string
	Descripcion string
	Destino     string
	Precio      decimal.Decimal
}

// Compra es la confirmación de un carrito. Se crea PENDIENTE y termina
// CONFIRMADA o RECHAZADA; nunca vuelve atrás (ADR-014).
type Compra struct {
	ID             string
	UsuarioID      string
	Email          string
	IdempotencyKey string
	Estado         EstadoCompra
	Retenciones    []string
	Lineas         []LineaCompra
	Total          decimal.Decimal
	// RetencionesVencidas son las que Catálogo informó como no vigentes al rechazar.
	RetencionesVencidas []string
	CreadaEn            time.Time
	ActualizadaEn       time.Time
}

// NuevaCompra crea una compra PENDIENTE con todas las retenciones del carrito.
// El id de la compra es también la Idempotency-Key de la confirmación en Catálogo.
func NuevaCompra(usuarioID, email, clave string, carrito Carrito, ahora time.Time) (*Compra, error) {
	if carrito.Vacio() {
		return nil, ErrCarritoVacio
	}
	if email == "" {
		return nil, ErrSinEmail
	}
	return &Compra{
		ID:             uuid.NewString(),
		UsuarioID:      usuarioID,
		Email:          email,
		IdempotencyKey: clave,
		Estado:         CompraPendiente,
		Retenciones:    carrito.Retenciones(),
		Total:          decimal.Zero,
		CreadaEn:       ahora.UTC(),
		ActualizadaEn:  ahora.UTC(),
	}, nil
}

// Confirmar registra las líneas cobradas. El total es la suma de las líneas.
func (c *Compra) Confirmar(lineas []LineaCompra, ahora time.Time) error {
	if c.Estado != CompraPendiente {
		return ErrTransicionInvalida
	}
	if len(lineas) == 0 {
		return ErrSinLineas
	}
	total := decimal.Zero
	for _, l := range lineas {
		total = total.Add(l.Precio)
	}
	c.Estado = CompraConfirmada
	c.Lineas = lineas
	c.Total = total
	c.ActualizadaEn = ahora.UTC()
	return nil
}

// Rechazar registra qué retenciones no estaban vigentes. No se vendió nada.
func (c *Compra) Rechazar(vencidas []string, ahora time.Time) error {
	if c.Estado != CompraPendiente {
		return ErrTransicionInvalida
	}
	c.Estado = CompraRechazada
	c.RetencionesVencidas = vencidas
	c.ActualizadaEn = ahora.UTC()
	return nil
}

// Destinos devuelve los destinos comprados (solo si está confirmada).
func (c Compra) Destinos() []string {
	if c.Estado != CompraConfirmada {
		return nil
	}
	var destinos []string
	for _, l := range c.Lineas {
		if l.Destino != "" {
			destinos = append(destinos, l.Destino)
		}
	}
	return destinos
}
