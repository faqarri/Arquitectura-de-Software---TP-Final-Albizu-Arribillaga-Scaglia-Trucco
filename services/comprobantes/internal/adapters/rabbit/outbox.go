package rabbit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

const tamanioLoteOutbox = 50

// colaPorTipo indica en qué cola se publica cada tipo de evento del outbox.
var colaPorTipo = map[string]string{
	ports.EventoEnvioSolicitado: ColaEnvios,
}

func iniciarPublicadorOutbox(ctx context.Context, cfg Config, log *slog.Logger, outbox ports.Outbox) error {
	cfg = cfg.conDefectos()
	log = log.With("componente", "publicador de outbox")
	p := &publicadorOutbox{outbox: outbox, log: log}
	return mantenerConexion(ctx, cfg, log, "publicador de outbox", func(ctx context.Context, conn *amqp.Connection) error {
		if err := declararTopologia(conn, cfg); err != nil {
			return err
		}
		pub, err := nuevoCanalConfirmado(conn)
		if err != nil {
			return err
		}
		defer pub.cerrar()
		cierre := conn.NotifyClose(make(chan *amqp.Error, 1))

		tic := time.NewTicker(cfg.IntervaloOutbox)
		defer tic.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case e := <-cierre:
				return fmt.Errorf("conexión cerrada: %v", e)
			case <-tic.C:
				if err := p.publicarPendientes(ctx, pub); err != nil {
					return err
				}
			}
		}
	})
}

type publicadorOutbox struct {
	outbox ports.Outbox
	log    *slog.Logger
	// ultimoError evita repetir el mismo log cada segundo mientras la base no responde.
	ultimoError string
}

// publicarPendientes publica un lote del outbox. Devuelve error solo si falló
// la publicación (para reconectar); los errores de la base se registran y se
// reintenta en el próximo ciclo.
func (p *publicadorOutbox) publicarPendientes(ctx context.Context, pub publicador) error {
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
		cola, ok := colaPorTipo[ev.Tipo]
		if !ok {
			log.Error("tipo de evento sin cola asignada: queda en el outbox")
			continue
		}
		err := pub.publicar(ctx, cola, amqp.Publishing{
			Headers:      tablaHeaders(ev.Headers),
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
			// Se volverá a publicar: el worker es idempotente.
			log.Warn("evento publicado pero no se pudo marcar en el outbox", "error", err)
			return nil
		}
		log.Info("evento del outbox publicado", "cola", cola)
	}
	return nil
}

// tablaHeaders convierte los headers del outbox (decodificados de JSON) en una
// tabla AMQP. Un valor que AMQP no admite se convierte en texto, para que un
// header raro no trabe la publicación del evento.
func tablaHeaders(h map[string]any) amqp.Table {
	t := amqp.Table{}
	for k, v := range h {
		if (amqp.Table{k: v}).Validate() != nil {
			v = fmt.Sprint(v)
		}
		t[k] = v
	}
	return t
}
