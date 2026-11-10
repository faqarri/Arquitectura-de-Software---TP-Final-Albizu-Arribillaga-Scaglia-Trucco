// Package rabbit conecta Reservas con RabbitMQ usando pkg/mensajeria:
//   - publica CompraConfirmada (outbox) en la cola de Comprobantes;
//   - consume RetencionVencida de Catálogo para marcar ítems vencidos (ADR-005, ADR-014).
package rabbit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/mensajeria"
	"agencia/reservas/internal/adapters/mysql"
)

const (
	// ColaComprobantes la consume Comprobantes (formato en su README, sección Mensajes).
	ColaComprobantes = "comprobantes.compras"
	// ExchangeDisponibilidad es el fanout donde Catálogo publica DisponibilidadCambiada
	// y RetencionVencida (ADR-015). A esta cola llegan los dos; se usa solo el segundo.
	ExchangeDisponibilidad = "catalogo.disponibilidad"
	ColaRetenciones        = "reservas.retenciones"
	DLX                    = "reservas.dlx"
	TipoRetencionVencida   = "RetencionVencida"
)

// declararColaComprobantes declara comprobantes.compras con los mismos
// argumentos que usa Comprobantes. Si Reservas arranca antes, el evento no se
// pierde: queda en la cola hasta que Comprobantes la consuma.
func declararColaComprobantes(ch *amqp.Channel) error {
	_, err := ch.QueueDeclare(ColaComprobantes, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "comprobantes.dlx",
		"x-dead-letter-routing-key": ColaComprobantes + mensajeria.SufijoDLQ,
	})
	return err
}

// IniciarPublicadorOutbox publica los eventos del outbox. Bloquea hasta que ctx se cancela.
func IniciarPublicadorOutbox(ctx context.Context, url string, log *slog.Logger, outbox mensajeria.Outbox) error {
	return mensajeria.IniciarPublicadorOutbox(ctx, url, log, outbox, mensajeria.ConfigOutbox{
		Destinos:  map[string]mensajeria.Destino{mysql.TipoCompraConfirmada: {RoutingKey: ColaComprobantes}},
		Topologia: declararColaComprobantes,
	})
}

// casoVencida es lo que se usa de application.Carritos.
type casoVencida interface {
	MarcarRetencionVencida(ctx context.Context, retencionID string) error
}

// Cola de RetencionVencida: durable, con reintentos y DLQ.
var colaRetenciones = mensajeria.ColaConReintentos{Nombre: ColaRetenciones, DLX: DLX}

func topologiaRetenciones(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeDisponibilidad, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return err
	}
	return ch.QueueBind(ColaRetenciones, "", ExchangeDisponibilidad, false, nil)
}

// IniciarConsumidorRetenciones consume RetencionVencida. Bloquea hasta que ctx se cancela.
func IniciarConsumidorRetenciones(ctx context.Context, url string, log *slog.Logger, uc casoVencida) error {
	log = log.With("componente", "consumidor de retenciones")
	cfg := mensajeria.ConfigConsumo{Cola: colaRetenciones, Topologia: topologiaRetenciones, NombreSpan: "marcar retención vencida"}
	return mensajeria.MantenerConexion(ctx, url, log, "consumidor de retenciones", func(ctx context.Context, conn *amqp.Connection) error {
		return mensajeria.Consumir(ctx, conn, cfg, log, manejarRetencion(log, uc))
	})
}

type eventoRetencion struct {
	Tipo        string `json:"tipo"`
	RetencionID string `json:"retencionId"`
}

var errMalFormado = errors.New("RetencionVencida mal formada")

// manejarRetencion marca el ítem vencido. Otros eventos del fanout se ignoran.
func manejarRetencion(log *slog.Logger, uc casoVencida) mensajeria.Manejador {
	return func(ctx context.Context, d amqp.Delivery) (mensajeria.Resultado, error) {
		var ev eventoRetencion
		errJSON := json.Unmarshal(d.Body, &ev)
		tipo := d.Type
		if tipo == "" {
			tipo = ev.Tipo
		}
		if tipo != TipoRetencionVencida {
			return mensajeria.Confirmar, nil // DisponibilidadCambiada u otro: no es para Reservas
		}
		if errJSON != nil || ev.RetencionID == "" {
			log.Error("RetencionVencida mal formada: va a la DLQ", "cuerpo", string(d.Body))
			return mensajeria.Descartar, errMalFormado
		}
		err := uc.MarcarRetencionVencida(ctx, ev.RetencionID)
		switch n := mensajeria.Intentos(d.Headers); {
		case err == nil:
			log.Info("ítem del carrito marcado vencido", "retencion_id", ev.RetencionID)
			return mensajeria.Confirmar, nil
		case n < mensajeria.MaxIntentos:
			log.Warn("no se pudo marcar la retención vencida, se reintenta", "error", err, "intento", n+1)
			return mensajeria.Reintentar, err
		default:
			log.Error("se agotaron los reintentos: RetencionVencida va a la DLQ", "error", err)
			return mensajeria.Descartar, err
		}
	}
}
