// Package rabbit agrupa los adaptadores de RabbitMQ de Comprobantes: el
// consumidor de CompraConfirmada (entrada), el worker de envíos (entrada) y el
// publicador de outbox (salida). Diseño en ADR-005 y en el README del servicio.
package rabbit

import (
	"context"
	"log/slog"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/ports"
)

// Nombres de colas y exchanges compartidos con los demás servicios.
const (
	ColaCompras = "comprobantes.compras" // CompraConfirmada, publicada por Reservas
	ColaEnvios  = "comprobantes.envios"  // EnvioSolicitado, publicada desde nuestro outbox
	SufijoDLQ   = ".dlq"
	MaxIntentos = 3
)

// Config de conexión a RabbitMQ.
type Config struct {
	URL string
}

// IniciarConsumidorCompras: TODO(rabbit). Consumir CompraConfirmada
// ({compraId, email, items}) y llamar a emitir.Ejecutar con
// Consumidor = domain.ConsumidorInterno e IdempotencyKey = compraId.
//   - ack solo después de procesar; un duplicado devuelve YaExistia y se confirma igual.
//   - mensaje mal formado o regla incumplida → directo a la DLQ, sin reintentar.
//   - otros errores → reintento con espera creciente, hasta MaxIntentos, luego DLQ.
//
// Bloquea hasta que ctx se cancela.
func IniciarConsumidorCompras(ctx context.Context, cfg Config, log *slog.Logger, emitir *application.EmitirComprobante) error {
	log.Warn("consumidor de CompraConfirmada sin implementar")
	<-ctx.Done()
	return nil
}

// IniciarWorkerEnvios: TODO(rabbit). Consumir EnvioSolicitado ({comprobanteId}) y
// llamar a enviar.Ejecutar. Si falla, reintentar hasta MaxIntentos con espera
// creciente; al agotarlos, enviar.MarcarFallido y mandar el mensaje a la DLQ.
//
// Bloquea hasta que ctx se cancela.
func IniciarWorkerEnvios(ctx context.Context, cfg Config, log *slog.Logger, enviar *application.EnviarComprobante) error {
	log.Warn("worker de envíos sin implementar")
	<-ctx.Done()
	return nil
}

// IniciarPublicadorOutbox: TODO(rabbit). Cada pocos segundos, leer
// outbox.Pendientes, publicar cada evento en su cola (EnvioSolicitado → ColaEnvios)
// con evento.Headers como headers del mensaje, esperar la confirmación del broker
// (publisher confirms) y recién después MarcarPublicado.
//
// Los consumidores recuperan la correlación con observabilidad.ExtraerDeMensaje.
//
// Bloquea hasta que ctx se cancela.
func IniciarPublicadorOutbox(ctx context.Context, cfg Config, log *slog.Logger, outbox ports.Outbox) error {
	log.Warn("publicador de outbox sin implementar")
	<-ctx.Done()
	return nil
}
