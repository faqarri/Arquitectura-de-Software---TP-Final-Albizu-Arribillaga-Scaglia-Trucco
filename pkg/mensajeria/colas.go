package mensajeria

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// SufijoDLQ es el sufijo de las colas de mensajes fallidos.
const SufijoDLQ = ".dlq"

// MaxIntentos es la cantidad de reintentos después del primer intento; al
// agotarlos, el mensaje va a la DLQ (ADR-005).
const MaxIntentos = 3

// EsperasPorDefecto son las esperas de los reintentos 1, 2 y 3.
var EsperasPorDefecto = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}

// ColaConReintentos es una cola durable con su DLQ y una cola de reintento por
// espera. Un mensaje que falla se copia a la cola de reintento, que no tiene
// consumidores: al vencer su TTL vuelve a la cola original. Al agotar los
// reintentos, un nack sin requeue lo lleva a la DLQ por el exchange DLX.
// Mecanismo explicado en el README de Comprobantes, sección Colas.
type ColaConReintentos struct {
	Nombre  string
	DLX     string          // exchange direct de dead-letter, por ejemplo "reservas.dlx"
	Esperas []time.Duration // si está vacío, EsperasPorDefecto
}

func (c ColaConReintentos) DLQ() string { return c.Nombre + SufijoDLQ }

func (c ColaConReintentos) esperas() []time.Duration {
	if len(c.Esperas) == 0 {
		return EsperasPorDefecto
	}
	return c.Esperas
}

// ColaReintento es la cola donde espera un mensaje antes de volver. El nombre
// incluye la espera porque es el TTL de la cola: si cambia la espera se declara
// otra cola, en lugar de chocar con los argumentos de la anterior.
func (c ColaConReintentos) ColaReintento(espera time.Duration) string {
	return c.Nombre + ".reintento." + espera.String()
}

// EsperaReintento es la espera antes del reintento n+1. Si ya se agotaron los
// reintentos, usa la última.
func (c ColaConReintentos) EsperaReintento(n int) time.Duration {
	e := c.esperas()
	return e[min(n, len(e)-1)]
}

// Declarar declara (de forma idempotente) el DLX, la DLQ, la cola y sus colas de reintento.
func (c ColaConReintentos) Declarar(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(c.DLX, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarar %s: %w", c.DLX, err)
	}
	if _, err := ch.QueueDeclare(c.DLQ(), true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarar %s: %w", c.DLQ(), err)
	}
	if err := ch.QueueBind(c.DLQ(), c.DLQ(), c.DLX, false, nil); err != nil {
		return fmt.Errorf("enlazar %s: %w", c.DLQ(), err)
	}
	if _, err := ch.QueueDeclare(c.Nombre, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    c.DLX,
		"x-dead-letter-routing-key": c.DLQ(),
	}); err != nil {
		return fmt.Errorf("declarar %s: %w", c.Nombre, err)
	}
	for _, espera := range c.esperas() {
		nombre := c.ColaReintento(espera)
		if _, err := ch.QueueDeclare(nombre, true, false, false, false, amqp.Table{
			"x-message-ttl":             espera.Milliseconds(),
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": c.Nombre,
		}); err != nil {
			return fmt.Errorf("declarar %s: %w", nombre, err)
		}
	}
	return nil
}
