// Package rabbit agrupa los adaptadores de RabbitMQ de Comprobantes: el
// consumidor de CompraConfirmada (entrada), el worker de envíos (entrada) y el
// publicador de outbox (salida). Diseño en ADR-005 y en el README del servicio.
package rabbit

import (
	"context"
	"log/slog"
	"time"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/ports"
)

// Nombres de colas y exchanges compartidos con los demás servicios.
const (
	ColaCompras = "comprobantes.compras" // CompraConfirmada, publicada por Reservas
	ColaEnvios  = "comprobantes.envios"  // EnvioSolicitado, publicada desde nuestro outbox
	SufijoDLQ   = ".dlq"
	MaxIntentos = 3 // reintentos después del primer intento; al agotarlos, el mensaje va a la DLQ

	// ExchangeDLX recibe los mensajes rechazados de ColaCompras y ColaEnvios y
	// los enruta a su DLQ (routing key = nombre de la DLQ).
	ExchangeDLX = "comprobantes.dlx"

	// HeaderIntentos cuenta los reintentos que ya tuvo un mensaje. Lo agrega el
	// consumidor al mandarlo a una cola de reintento.
	HeaderIntentos = "x-intentos"
)

// Valores por defecto de Config.
var (
	EsperasReintento = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}
	IntervaloOutbox  = time.Second
	Prefetch         = 5
)

// Config de conexión a RabbitMQ. Solo URL es obligatoria; el resto toma los
// valores por defecto de arriba (los tests de integración los acortan).
type Config struct {
	URL string

	// EsperasReintento[i] es la espera antes del reintento i+1. Debe tener
	// MaxIntentos elementos. Cada espera es una cola de reintento con ese TTL.
	EsperasReintento []time.Duration
	IntervaloOutbox  time.Duration
	// Prefetch es la cantidad de mensajes sin confirmar por consumidor, y
	// también la cantidad de mensajes que se procesan en paralelo.
	Prefetch int
}

func (c Config) conDefectos() Config {
	if len(c.EsperasReintento) == 0 {
		c.EsperasReintento = EsperasReintento
	}
	if c.IntervaloOutbox <= 0 {
		c.IntervaloOutbox = IntervaloOutbox
	}
	if c.Prefetch <= 0 {
		c.Prefetch = Prefetch
	}
	return c
}

// IniciarConsumidorCompras consume CompraConfirmada de ColaCompras y emite el
// comprobante con Consumidor = domain.ConsumidorInterno e IdempotencyKey =
// compraId. Formato del evento y política de reintentos en el README.
//
// Bloquea hasta que ctx se cancela. Si RabbitMQ no está disponible, reintenta
// la conexión con backoff sin devolver error.
func IniciarConsumidorCompras(ctx context.Context, cfg Config, log *slog.Logger, emitir *application.EmitirComprobante) error {
	return iniciarConsumidorCompras(ctx, cfg, log, emitir)
}

// IniciarWorkerEnvios consume EnvioSolicitado ({comprobanteId}) de ColaEnvios
// y envía el correo. Si falla, reintenta hasta MaxIntentos con espera
// creciente; al agotarlos, marca el comprobante FALLIDO y el mensaje pasa a la DLQ.
//
// Bloquea hasta que ctx se cancela.
func IniciarWorkerEnvios(ctx context.Context, cfg Config, log *slog.Logger, enviar *application.EnviarComprobante) error {
	return iniciarWorkerEnvios(ctx, cfg, log, enviar)
}

// IniciarPublicadorOutbox lee cada cfg.IntervaloOutbox los eventos pendientes
// del outbox, los publica en su cola con evento.Headers como headers del
// mensaje, espera la confirmación del broker (publisher confirms) y recién
// después los marca publicados.
//
// Bloquea hasta que ctx se cancela.
func IniciarPublicadorOutbox(ctx context.Context, cfg Config, log *slog.Logger, outbox ports.Outbox) error {
	return iniciarPublicadorOutbox(ctx, cfg, log, outbox)
}
