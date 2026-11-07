// Package mensajeria reúne lo que comparten los servicios que usan RabbitMQ:
// reconexión con backoff, publicación con publisher confirms, colas con
// reintentos de espera creciente y DLQ, consumo con ack después de procesar y
// el publicador del outbox (ADR-005).
//
// Es la versión genérica del paquete rabbit de Comprobantes, que se escribió
// primero y todavía tiene su propia copia.
package mensajeria

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	esperaInicialReconexion = time.Second
	esperaMaximaReconexion  = 30 * time.Second
	tiempoMaxConfirmacion   = 5 * time.Second
)

// MantenerConexion conecta a RabbitMQ y ejecuta sesion con la conexión. Si no
// puede conectar, o si sesion termina porque se cayó la conexión, lo registra y
// vuelve a intentar con backoff exponencial (con jitter). Solo termina cuando
// ctx se cancela, así una caída del broker no tira abajo el servicio.
func MantenerConexion(ctx context.Context, url string, log *slog.Logger, nombre string, sesion func(context.Context, *amqp.Connection) error) error {
	espera := esperaInicialReconexion
	for {
		conn, err := amqp.DialConfig(url, amqp.Config{
			Properties: amqp.Table{"connection_name": nombre},
		})
		if err != nil {
			log.Warn("no se pudo conectar a RabbitMQ, se reintenta", "error", err, "espera", espera.String())
		} else {
			log.Info("conectado a RabbitMQ")
			inicio := time.Now()
			err = sesion(ctx, conn)
			_ = conn.Close()
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("se perdió la conexión con RabbitMQ, se reconecta", "error", err, "espera", espera.String())
			// Una sesión larga indica que el broker estaba sano: la próxima caída
			// arranca el backoff de cero.
			if time.Since(inicio) > esperaMaximaReconexion {
				espera = esperaInicialReconexion
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(conJitter(espera)):
		}
		espera = min(espera*2, esperaMaximaReconexion)
	}
}

// conJitter devuelve d ± 20 %, para que varias instancias no reconecten a la vez.
func conJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

// Publicador publica un mensaje y devuelve nil solo si el broker lo confirmó.
type Publicador interface {
	Publicar(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error
}

// CanalConfirmado es un canal en modo publisher confirms.
type CanalConfirmado struct {
	ch *amqp.Channel
}

func NuevoCanalConfirmado(conn *amqp.Connection) (*CanalConfirmado, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("abrir canal: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("activar publisher confirms: %w", err)
	}
	return &CanalConfirmado{ch: ch}, nil
}

func (c *CanalConfirmado) Publicar(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaxConfirmacion)
	defer cancelar()
	confirmacion, err := c.ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, msg)
	if err != nil {
		return fmt.Errorf("publicar en %q/%q: %w", exchange, routingKey, err)
	}
	ok, err := confirmacion.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("esperar confirmación de %q/%q: %w", exchange, routingKey, err)
	}
	if !ok {
		return fmt.Errorf("el broker rechazó el mensaje para %q/%q (nack)", exchange, routingKey)
	}
	return nil
}

func (c *CanalConfirmado) Cerrar() { _ = c.ch.Close() }

// Declarar abre un canal temporal y ejecuta las declaraciones de topología.
func Declarar(conn *amqp.Connection, declaraciones ...func(*amqp.Channel) error) error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	defer ch.Close()
	for _, d := range declaraciones {
		if d == nil {
			continue
		}
		if err := d(ch); err != nil {
			return err
		}
	}
	return nil
}
