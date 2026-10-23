// Package domain contiene las entidades y reglas de Comprobantes. No depende de
// HTTP, RabbitMQ, MySQL ni SMTP (ADR-002).
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EstadoEnvio es el estado del envío por correo de un comprobante.
type EstadoEnvio string

const (
	EnvioPendiente EstadoEnvio = "PENDIENTE"
	EnvioEnviado   EstadoEnvio = "ENVIADO"
	EnvioFallido   EstadoEnvio = "FALLIDO"
)

// ConsumidorInterno identifica a los comprobantes de las compras propias
// (evento CompraConfirmada). Los grupos externos se identifican por su API Key.
const ConsumidorInterno = "interno"

// ImporteMaximo es el mayor precio o total que admite un comprobante: el máximo
// de DECIMAL(12,2), la precisión con la que se guardan los importes.
var ImporteMaximo = decimal.RequireFromString("9999999999.99")

var (
	ErrSinItems            = errors.New("el comprobante debe tener al menos un ítem")
	ErrPrecioNegativo      = errors.New("el precio de un ítem no puede ser negativo")
	ErrTotalCero           = errors.New("el total del comprobante debe ser mayor a cero")
	ErrImporteFueraDeRango = errors.New("el precio de cada ítem y el total no pueden superar 9999999999.99")
	ErrTransicionInvalida  = errors.New("transición de estado de envío inválida")
)

// Item es una línea del comprobante.
type Item struct {
	Descripcion string
	Precio      decimal.Decimal
}

// Comprobante es una factura emitida. Siempre se envía por correo a Email.
type Comprobante struct {
	ID             string
	Nro            int64 // lo asigna la base al guardar; ver Numero
	Consumidor     string
	IdempotencyKey string
	HashSolicitud  string
	Email          string
	Items          []Item
	Total          decimal.Decimal
	EstadoEnvio    EstadoEnvio
	IntentosEnvio  int
	FechaEmision   time.Time
}

// NuevoComprobante crea un comprobante PENDIENTE de envío. Redondea cada precio
// a 2 decimales (el 5 hacia arriba) y calcula el total como la suma de los
// precios ya redondeados, así el total coincide con los ítems devueltos.
// No modifica el slice recibido.
func NuevoComprobante(consumidor, clave, hash, email string, items []Item, ahora time.Time) (*Comprobante, error) {
	if len(items) == 0 {
		return nil, ErrSinItems
	}
	redondeados := make([]Item, len(items))
	total := decimal.Zero
	for i, it := range items {
		if it.Precio.IsNegative() {
			return nil, ErrPrecioNegativo
		}
		precio := it.Precio.Round(2)
		if precio.GreaterThan(ImporteMaximo) {
			return nil, ErrImporteFueraDeRango
		}
		redondeados[i] = Item{Descripcion: it.Descripcion, Precio: precio}
		total = total.Add(precio)
	}
	if !total.IsPositive() {
		return nil, ErrTotalCero
	}
	if total.GreaterThan(ImporteMaximo) {
		return nil, ErrImporteFueraDeRango
	}
	return &Comprobante{
		ID:             uuid.NewString(),
		Consumidor:     consumidor,
		IdempotencyKey: clave,
		HashSolicitud:  hash,
		Email:          email,
		Items:          redondeados,
		Total:          total,
		EstadoEnvio:    EnvioPendiente,
		// Milisegundos: es la precisión con la que se guarda, así la fecha que
		// devuelve la emisión es la misma que se lee después.
		FechaEmision: ahora.UTC().Truncate(time.Millisecond),
	}, nil
}

// Numero es el número visible del comprobante, por ejemplo B-0001-00000042.
func (c *Comprobante) Numero() string {
	return fmt.Sprintf("B-0001-%08d", c.Nro)
}

// MarcarEnviado registra que el servidor de correo aceptó el mensaje.
func (c *Comprobante) MarcarEnviado() error {
	if c.EstadoEnvio != EnvioPendiente {
		return ErrTransicionInvalida
	}
	c.EstadoEnvio = EnvioEnviado
	c.IntentosEnvio++
	return nil
}

// MarcarFallido registra que se agotaron los reintentos de envío. El
// comprobante sigue emitido.
func (c *Comprobante) MarcarFallido() error {
	if c.EstadoEnvio != EnvioPendiente {
		return ErrTransicionInvalida
	}
	c.EstadoEnvio = EnvioFallido
	return nil
}
