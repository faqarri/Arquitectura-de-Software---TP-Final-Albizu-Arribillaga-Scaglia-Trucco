package rabbit

import (
	"context"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// resultado es lo que hay que hacer con un mensaje después de procesarlo.
type resultado int

const (
	confirmar  resultado = iota // ack
	reintentar                  // copia a la cola de reintento y ack del original
	descartar                   // nack sin requeue: el DLX lo lleva a la DLQ
)

// intentos devuelve cuántos reintentos tuvo ya el mensaje (0 en la primera entrega).
func intentos(h amqp.Table) int {
	switch v := h[HeaderIntentos].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	case int16:
		return int(v)
	case int8:
		return int(v)
	case uint8:
		return int(v)
	}
	return 0
}

// esperaReintento es la espera antes del próximo reintento. Si el mensaje ya
// agotó los reintentos (por ejemplo, porque falló marcar FALLIDO), usa la última.
func esperaReintento(cfg Config, n int) time.Duration {
	return cfg.EsperasReintento[min(n, len(cfg.EsperasReintento)-1)]
}

// resolver aplica r a la entrega d, que llegó por cola.
func resolver(ctx context.Context, log *slog.Logger, cfg Config, pub publicador, cola string, d amqp.Delivery, r resultado) {
	switch r {
	case confirmar:
		if err := d.Ack(false); err != nil {
			log.Warn("no se pudo confirmar el mensaje; se volverá a entregar", "error", err)
		}

	case descartar:
		if err := d.Nack(false, false); err != nil {
			log.Warn("no se pudo mandar el mensaje a la DLQ; se volverá a entregar", "error", err)
		}

	case reintentar:
		n := intentos(d.Headers)
		headers := amqp.Table{}
		for k, v := range d.Headers {
			headers[k] = v
		}
		headers[HeaderIntentos] = int32(n + 1)
		copia := amqp.Publishing{
			Headers:       headers,
			ContentType:   d.ContentType,
			DeliveryMode:  amqp.Persistent,
			CorrelationId: d.CorrelationId,
			MessageId:     d.MessageId,
			Timestamp:     d.Timestamp,
			Type:          d.Type,
			Body:          d.Body,
		}
		// Primero se publica la copia (confirmada) y después se confirma el
		// original. Si el proceso cae entre ambos pasos, el mensaje queda
		// duplicado, nunca perdido; los consumidores son idempotentes.
		if err := pub.publicar(ctx, colaReintento(cola, esperaReintento(cfg, n)), copia); err != nil {
			log.Error("no se pudo programar el reintento; el mensaje vuelve a la cola", "error", err)
			_ = d.Nack(false, true)
			return
		}
		if err := d.Ack(false); err != nil {
			log.Warn("no se pudo confirmar el mensaje original; puede procesarse dos veces", "error", err)
		}
	}
}
