// Package ports define las interfaces que los casos de uso necesitan del mundo
// exterior. Las implementan los paquetes de internal/adapters (ADR-002).
package ports

import (
	"context"
	"errors"

	"agencia/comprobantes/internal/domain"
)

var (
	ErrNoEncontrado   = errors.New("comprobante no encontrado")
	ErrClaveDuplicada = errors.New("ya existe un comprobante con esa clave de idempotencia para el consumidor")
)

// RepositorioComprobantes persiste los comprobantes.
type RepositorioComprobantes interface {
	// Guardar inserta el comprobante con sus ítems y, en la MISMA transacción, el
	// evento EnvioSolicitado en el outbox. Completa c.Nro con el número asignado
	// por la base. Devuelve ErrClaveDuplicada si ya existe un comprobante con el
	// mismo (consumidor, idempotency_key).
	Guardar(ctx context.Context, c *domain.Comprobante) error

	// BuscarPorClave devuelve el comprobante de ese consumidor con esa clave de
	// idempotencia, o ErrNoEncontrado.
	BuscarPorClave(ctx context.Context, consumidor, clave string) (*domain.Comprobante, error)

	// BuscarPorID solo encuentra comprobantes del consumidor indicado: los de otro
	// consumidor devuelven ErrNoEncontrado, igual que si no existieran.
	BuscarPorID(ctx context.Context, consumidor, id string) (*domain.Comprobante, error)

	// Obtener busca por id sin filtrar por consumidor. Solo lo usa el envío de correo.
	Obtener(ctx context.Context, id string) (*domain.Comprobante, error)

	// ActualizarEnvio guarda EstadoEnvio e IntentosEnvio.
	ActualizarEnvio(ctx context.Context, c *domain.Comprobante) error
}

// GeneradorPDF genera la factura en PDF.
type GeneradorPDF interface {
	Generar(ctx context.Context, c *domain.Comprobante) ([]byte, error)
}

// Adjunto es un archivo adjunto de un correo.
type Adjunto struct {
	Nombre    string
	Contenido []byte
}

// Correo es un mensaje a enviar.
type Correo struct {
	Para    string
	Asunto  string
	Cuerpo  string
	Adjunto Adjunto
}

// Mailer envía correos. Devuelve error si el servidor no aceptó el mensaje.
type Mailer interface {
	Enviar(ctx context.Context, correo Correo) error
}

// EventoEnvioSolicitado es el tipo del evento que Guardar deja en el outbox para
// que el worker envíe el correo. Payload: {"comprobanteId": "..."}.
const EventoEnvioSolicitado = "EnvioSolicitado"

// EventoOutbox es un evento guardado en el outbox, pendiente de publicar.
// Headers trae la correlación y la traza de la solicitud que lo originó; el
// publicador los copia en los headers del mensaje.
type EventoOutbox struct {
	ID      uint64
	Tipo    string
	Payload []byte
	Headers map[string]any
}

// Outbox lo usa el publicador de outbox para leer los eventos pendientes. No lo
// usan los casos de uso: es el contrato entre los adaptadores de MySQL y RabbitMQ.
type Outbox interface {
	Pendientes(ctx context.Context, limite int) ([]EventoOutbox, error)
	MarcarPublicado(ctx context.Context, id uint64) error
}
