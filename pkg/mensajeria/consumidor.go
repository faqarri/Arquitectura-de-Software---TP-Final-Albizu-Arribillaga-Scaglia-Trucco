package mensajeria

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/observabilidad"
)

// HeaderIntentos cuenta los reintentos que ya tuvo un mensaje.
const HeaderIntentos = "x-intentos"

// tiempoMaxProcesamiento acota lo que tarda un mensaje. Al apagar, los
// mensajes en curso terminan dentro de este plazo en lugar de cortarse.
const tiempoMaxProcesamiento = 30 * time.Second

// Resultado es lo que hay que hacer con un mensaje después de procesarlo.
type Resultado int

const (
	Confirmar  Resultado = iota // ack
	Reintentar                  // copia a la cola de reintento y ack del original
	Descartar                   // nack sin requeue: el DLX lo lleva a la DLQ
)

// Intentos devuelve cuántos reintentos tuvo ya el mensaje (0 en la primera entrega).
func Intentos(h amqp.Table) int {
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

// Manejador procesa un mensaje y decide qué hacer con él. El ctx trae la
// correlación y la traza del mensaje. El error es solo para la traza.
type Manejador func(ctx context.Context, d amqp.Delivery) (Resultado, error)

// ConfigConsumo describe una cola a consumir.
type ConfigConsumo struct {
	Cola ColaConReintentos
	// Topologia declara lo demás que necesita la cola, por ejemplo el binding a
	// un exchange fanout. Se ejecuta después de declarar la cola.
	Topologia func(*amqp.Channel) error
	// Prefetch es la cantidad de mensajes sin confirmar y también la cantidad
	// que se procesan en paralelo. Por defecto, 5.
	Prefetch int
	// NombreSpan nombra el span de cada mensaje (por ejemplo, "marcar retención vencida").
	NombreSpan string
}

// Consumir declara la cola y procesa sus mensajes hasta que ctx se cancela
// (después de terminar los mensajes en curso) o hasta que se cierra el canal.
// Pensado para usarse dentro de MantenerConexion.
func Consumir(ctx context.Context, conn *amqp.Connection, cfg ConfigConsumo, log *slog.Logger, manejar Manejador) error {
	prefetch := cfg.Prefetch
	if prefetch <= 0 {
		prefetch = 5
	}
	if err := Declarar(conn, cfg.Cola.Declarar, cfg.Topologia); err != nil {
		return err
	}
	pub, err := NuevoCanalConfirmado(conn)
	if err != nil {
		return err
	}
	defer pub.Cerrar()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	defer ch.Close()
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return fmt.Errorf("configurar prefetch: %w", err)
	}
	const etiqueta = "consumidor"
	entregas, err := ch.Consume(cfg.Cola.Nombre, etiqueta, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumir %s: %w", cfg.Cola.Nombre, err)
	}

	var wg sync.WaitGroup
	for range prefetch {
		wg.Go(func() {
			for d := range entregas {
				ctxMsg, cancelar := context.WithTimeout(context.WithoutCancel(ctx), tiempoMaxProcesamiento)
				ProcesarEntrega(ctxMsg, log, pub, cfg.Cola, cfg.NombreSpan, d, manejar)
				cancelar()
			}
		})
	}
	terminado := make(chan struct{})
	go func() { wg.Wait(); close(terminado) }()

	select {
	case <-ctx.Done():
		_ = ch.Cancel(etiqueta, false)
		<-terminado
		return nil
	case <-terminado:
		return errors.New("se cerró el canal de consumo de " + cfg.Cola.Nombre)
	}
}

// ProcesarEntrega ejecuta manejar con la correlación y la traza del mensaje y
// aplica el resultado. Es exportada para poder probar los manejadores sin broker.
func ProcesarEntrega(ctx context.Context, log *slog.Logger, pub Publicador, cola ColaConReintentos, nombreSpan string, d amqp.Delivery, manejar Manejador) {
	var r Resultado
	_ = observabilidad.ProcesarMensaje(ctx, nombreSpan, d.Headers, func(ctx context.Context) error {
		var err error
		r, err = manejar(ctx, d)
		return err
	})
	Resolver(ctx, observabilidad.Logger(observabilidad.ExtraerDeMensaje(ctx, d.Headers), log), pub, cola, d, r)
}

// Resolver aplica r a la entrega d, que llegó por cola.
func Resolver(ctx context.Context, log *slog.Logger, pub Publicador, cola ColaConReintentos, d amqp.Delivery, r Resultado) {
	switch r {
	case Confirmar:
		if err := d.Ack(false); err != nil {
			log.Warn("no se pudo confirmar el mensaje; se volverá a entregar", "error", err)
		}

	case Descartar:
		if err := d.Nack(false, false); err != nil {
			log.Warn("no se pudo mandar el mensaje a la DLQ; se volverá a entregar", "error", err)
		}

	case Reintentar:
		n := Intentos(d.Headers)
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
		// Primero la copia (confirmada) y después el ack del original: si el
		// proceso cae entre ambos, el mensaje queda duplicado, nunca perdido.
		if err := pub.Publicar(ctx, "", cola.ColaReintento(cola.EsperaReintento(n)), copia); err != nil {
			log.Error("no se pudo programar el reintento; el mensaje vuelve a la cola", "error", err)
			_ = d.Nack(false, true)
			return
		}
		if err := d.Ack(false); err != nil {
			log.Warn("no se pudo confirmar el mensaje original; puede procesarse dos veces", "error", err)
		}
	}
}
