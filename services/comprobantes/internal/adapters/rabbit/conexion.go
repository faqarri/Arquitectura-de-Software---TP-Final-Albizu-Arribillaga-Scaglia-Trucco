package rabbit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	esperaInicialReconexion = time.Second
	esperaMaximaReconexion  = 30 * time.Second
	tiempoMaxConfirmacion   = 5 * time.Second
	// tiempoMaxProcesamiento acota lo que tarda un mensaje. Al apagar, los
	// mensajes en curso terminan dentro de este plazo en lugar de cortarse.
	tiempoMaxProcesamiento = 30 * time.Second
)

// mantenerConexion conecta a RabbitMQ y ejecuta sesion con la conexión. Si no
// puede conectar, o si sesion termina porque se cayó la conexión, lo registra y
// vuelve a intentar con backoff exponencial (con jitter). Solo termina cuando
// ctx se cancela, así una caída del broker no tira abajo el servicio.
func mantenerConexion(ctx context.Context, cfg Config, log *slog.Logger, nombre string, sesion func(context.Context, *amqp.Connection) error) error {
	espera := esperaInicialReconexion
	for {
		conn, err := amqp.DialConfig(cfg.URL, amqp.Config{
			Properties: amqp.Table{"connection_name": "comprobantes: " + nombre},
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
			// arranca el backoff de cero. Si la sesión falla enseguida (por
			// ejemplo, por una cola declarada con otros argumentos), el backoff sigue creciendo.
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

// colaReintento es la cola donde espera un mensaje de cola antes de volver a
// ella. El nombre incluye la espera porque es el TTL de la cola: si cambia la
// espera se declara otra cola, en lugar de chocar con los argumentos de la anterior.
func colaReintento(cola string, espera time.Duration) string {
	return cola + ".reintento." + espera.String()
}

// declararTopologia declara exchanges, colas y bindings. Es idempotente: se
// llama en cada conexión, desde cada componente. Ver README del servicio.
func declararTopologia(conn *amqp.Connection, cfg Config) error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(ExchangeDLX, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarar %s: %w", ExchangeDLX, err)
	}
	for _, cola := range []string{ColaCompras, ColaEnvios} {
		dlq := cola + SufijoDLQ
		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declarar %s: %w", dlq, err)
		}
		if err := ch.QueueBind(dlq, dlq, ExchangeDLX, false, nil); err != nil {
			return fmt.Errorf("enlazar %s: %w", dlq, err)
		}
		// Un Nack sin requeue manda el mensaje a la DLQ a través del DLX.
		if _, err := ch.QueueDeclare(cola, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    ExchangeDLX,
			"x-dead-letter-routing-key": dlq,
		}); err != nil {
			return fmt.Errorf("declarar %s: %w", cola, err)
		}
		// Colas de reintento: nadie las consume. Al vencer el TTL, el mensaje
		// vuelve a la cola original por el exchange por defecto.
		for _, espera := range cfg.EsperasReintento {
			nombre := colaReintento(cola, espera)
			if _, err := ch.QueueDeclare(nombre, true, false, false, false, amqp.Table{
				"x-message-ttl":             espera.Milliseconds(),
				"x-dead-letter-exchange":    "",
				"x-dead-letter-routing-key": cola,
			}); err != nil {
				return fmt.Errorf("declarar %s: %w", nombre, err)
			}
		}
	}
	return nil
}

// publicador publica un mensaje en una cola (por el exchange por defecto) y
// devuelve nil solo si el broker lo confirmó.
type publicador interface {
	publicar(ctx context.Context, cola string, msg amqp.Publishing) error
}

// canalConfirmado es un canal en modo publisher confirms.
type canalConfirmado struct {
	ch *amqp.Channel
}

func nuevoCanalConfirmado(conn *amqp.Connection) (*canalConfirmado, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("abrir canal: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("activar publisher confirms: %w", err)
	}
	return &canalConfirmado{ch: ch}, nil
}

func (c *canalConfirmado) publicar(ctx context.Context, cola string, msg amqp.Publishing) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaxConfirmacion)
	defer cancelar()
	confirmacion, err := c.ch.PublishWithDeferredConfirmWithContext(ctx, "", cola, false, false, msg)
	if err != nil {
		return fmt.Errorf("publicar en %s: %w", cola, err)
	}
	ok, err := confirmacion.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("esperar confirmación de %s: %w", cola, err)
	}
	if !ok {
		return fmt.Errorf("el broker rechazó el mensaje para %s (nack)", cola)
	}
	return nil
}

func (c *canalConfirmado) cerrar() { _ = c.ch.Close() }

// consumir declara la topología y procesa los mensajes de cola con
// cfg.Prefetch goroutines, llamando a manejar con cada entrega. manejar hace
// el ack, el nack o el reintento. Vuelve cuando ctx se cancela (después de
// terminar los mensajes en curso) o cuando se cierra el canal.
func consumir(ctx context.Context, conn *amqp.Connection, cfg Config, cola string, manejar func(context.Context, amqp.Delivery, publicador)) error {
	if err := declararTopologia(conn, cfg); err != nil {
		return err
	}
	pub, err := nuevoCanalConfirmado(conn)
	if err != nil {
		return err
	}
	defer pub.cerrar()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	defer ch.Close()
	if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("configurar prefetch: %w", err)
	}
	const etiqueta = "comprobantes"
	entregas, err := ch.Consume(cola, etiqueta, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumir %s: %w", cola, err)
	}

	var wg sync.WaitGroup
	for range cfg.Prefetch {
		wg.Go(func() {
			for d := range entregas {
				// El mensaje en curso no se corta al apagar: termina dentro de tiempoMaxProcesamiento.
				ctxMsg, cancelar := context.WithTimeout(context.WithoutCancel(ctx), tiempoMaxProcesamiento)
				manejar(ctxMsg, d, pub)
				cancelar()
			}
		})
	}
	terminado := make(chan struct{})
	go func() { wg.Wait(); close(terminado) }()

	select {
	case <-ctx.Done():
		// Dejar de recibir; los mensajes ya entregados y sin ack vuelven a la cola.
		_ = ch.Cancel(etiqueta, false)
		<-terminado
		return nil
	case <-terminado:
		return errors.New("se cerró el canal de consumo de " + cola)
	}
}
