package mensajeria

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/observabilidad"
)

const tamanioLoteOutbox = 50

// EventoOutbox es un evento guardado en el outbox, pendiente de publicar.
// Headers trae la correlación y la traza de la operación que lo originó.
type EventoOutbox struct {
	ID      uint64
	Tipo    string
	Payload []byte
	Headers map[string]any
}

// Outbox es lo que el publicador necesita del almacenamiento del servicio.
type Outbox interface {
	Pendientes(ctx context.Context, limite int) ([]EventoOutbox, error)
	MarcarPublicado(ctx context.Context, id uint64) error
}

// Destino indica dónde se publica un tipo de evento. Para publicar directo en
// una cola: Exchange "" y RoutingKey = nombre de la cola.
type Destino struct {
	Exchange   string
	RoutingKey string
}

// ConfigOutbox configura el publicador.
type ConfigOutbox struct {
	Destinos  map[string]Destino // por tipo de evento
	Intervalo time.Duration      // por defecto, 1 s
	// Topologia declara los exchanges o colas de destino (de forma idempotente).
	Topologia func(*amqp.Channel) error
}

// IniciarPublicadorOutbox lee cada cfg.Intervalo los eventos pendientes, los
// publica como mensajes persistentes con publisher confirms y recién después
// los marca publicados. Bloquea hasta que ctx se cancela; si RabbitMQ no
// responde, reconecta con backoff.
func IniciarPublicadorOutbox(ctx context.Context, url string, log *slog.Logger, outbox Outbox, cfg ConfigOutbox) error {
	if cfg.Intervalo <= 0 {
		cfg.Intervalo = time.Second
	}
	log = log.With("componente", "publicador de outbox")
	p := &PublicadorOutbox{outbox: outbox, log: log, destinos: cfg.Destinos}
	return MantenerConexion(ctx, url, log, "publicador de outbox", func(ctx context.Context, conn *amqp.Connection) error {
		if err := Declarar(conn, cfg.Topologia); err != nil {
			return err
		}
		pub, err := NuevoCanalConfirmado(conn)
		if err != nil {
			return err
		}
		defer pub.Cerrar()
		cierre := conn.NotifyClose(make(chan *amqp.Error, 1))

		tic := time.NewTicker(cfg.Intervalo)
		defer tic.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case e := <-cierre:
				return fmt.Errorf("conexión cerrada: %v", e)
			case <-tic.C:
				if err := p.PublicarPendientes(ctx, pub); err != nil {
					return err
				}
			}
		}
	})
}

// PublicadorOutbox publica lotes del outbox. Exportado para los tests.
type PublicadorOutbox struct {
	outbox   Outbox
	log      *slog.Logger
	destinos map[string]Destino
	// ultimoError evita repetir el mismo log cada segundo mientras la base no responde.
	ultimoError string
}

func NuevoPublicadorOutbox(outbox Outbox, log *slog.Logger, destinos map[string]Destino) *PublicadorOutbox {
	return &PublicadorOutbox{outbox: outbox, log: log, destinos: destinos}
}

// PublicarPendientes publica un lote. Devuelve error solo si falló la
// publicación (para reconectar); los errores de la base se registran y se
// reintenta en el próximo ciclo.
func (p *PublicadorOutbox) PublicarPendientes(ctx context.Context, pub Publicador) error {
	eventos, err := p.outbox.Pendientes(ctx, tamanioLoteOutbox)
	if err != nil {
		if err.Error() != p.ultimoError {
			p.log.Warn("no se pudieron leer los eventos pendientes del outbox", "error", err)
			p.ultimoError = err.Error()
		}
		return nil
	}
	if p.ultimoError != "" {
		p.log.Info("se recuperó la lectura del outbox")
		p.ultimoError = ""
	}

	for _, ev := range eventos {
		log := observabilidad.Logger(observabilidad.ExtraerDeMensaje(ctx, ev.Headers), p.log).
			With("evento_id", ev.ID, "tipo", ev.Tipo)
		destino, ok := p.destinos[ev.Tipo]
		if !ok {
			log.Error("tipo de evento sin destino asignado: queda en el outbox")
			continue
		}
		err := pub.Publicar(ctx, destino.Exchange, destino.RoutingKey, amqp.Publishing{
			Headers:      TablaHeaders(ev.Headers),
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    fmt.Sprintf("outbox-%d", ev.ID),
			Timestamp:    time.Now().UTC(),
			Type:         ev.Tipo,
			Body:         ev.Payload,
		})
		if err != nil {
			return fmt.Errorf("publicar evento %d del outbox: %w", ev.ID, err)
		}

		// Ya está confirmado por el broker: se marca aunque el servicio se esté
		// apagando, para no publicarlo dos veces.
		ctxMarcar, cancelar := context.WithTimeout(context.WithoutCancel(ctx), tiempoMaxConfirmacion)
		err = p.outbox.MarcarPublicado(ctxMarcar, ev.ID)
		cancelar()
		if err != nil {
			log.Warn("evento publicado pero no se pudo marcar en el outbox; se publicará de nuevo", "error", err)
			return nil
		}
		log.Info("evento del outbox publicado", "exchange", destino.Exchange, "routing_key", destino.RoutingKey)
	}
	return nil
}

// TablaHeaders convierte headers decodificados de JSON en una tabla AMQP. Un
// valor que AMQP no admite se convierte en texto, para que un header raro no
// trabe la publicación.
func TablaHeaders(h map[string]any) amqp.Table {
	t := amqp.Table{}
	for k, v := range h {
		if (amqp.Table{k: v}).Validate() != nil {
			v = fmt.Sprint(v)
		}
		t[k] = v
	}
	return t
}
