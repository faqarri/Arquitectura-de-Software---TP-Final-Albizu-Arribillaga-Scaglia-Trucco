// Package consumidor recibe los eventos de Catálogo del exchange fanout
// catalogo.disponibilidad y se los pasa al hub (ADR-015).
//
// Cada instancia declara su propia cola exclusiva, sin nombre y que se borra al
// desconectarse: así todas reciben todos los eventos y el servicio escala sin
// coordinación. No hay DLQ: los eventos son avisos y un mensaje mal formado se
// descarta y se registra.
package consumidor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/mensajeria"
	"agencia/tiempo-real/internal/hub"
)

const (
	// Exchange es el fanout donde Catálogo publica los cambios de disponibilidad.
	Exchange = "catalogo.disponibilidad"

	tipoDisponibilidadCambiada = "DisponibilidadCambiada"
	tipoRetencionVencida       = "RetencionVencida"
)

var eventosRecibidos = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "tiempo_real_eventos_total",
	Help: "Eventos de Catálogo recibidos, por tipo y resultado (entregado, sin_suscriptores, invalido, ignorado).",
}, []string{"tipo", "resultado"})

func init() { prometheus.MustRegister(eventosRecibidos) }

// Iniciar consume hasta que ctx se cancela, reconectando si se cae RabbitMQ.
func Iniciar(ctx context.Context, url string, log *slog.Logger, h *hub.Hub) error {
	log = log.With("componente", "consumidor de disponibilidad")
	return mensajeria.MantenerConexion(ctx, url, log, "tiempo-real", func(ctx context.Context, conn *amqp.Connection) error {
		ch, err := conn.Channel()
		if err != nil {
			return err
		}
		defer ch.Close()
		if err := ch.ExchangeDeclare(Exchange, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
			return err
		}
		cola, err := ch.QueueDeclare("", false, true, true, false, nil)
		if err != nil {
			return err
		}
		if err := ch.QueueBind(cola.Name, "", Exchange, false, nil); err != nil {
			return err
		}
		entregas, err := ch.Consume(cola.Name, "", true, true, false, false, nil)
		if err != nil {
			return err
		}
		cierre := conn.NotifyClose(make(chan *amqp.Error, 1))
		for {
			select {
			case <-ctx.Done():
				return nil
			case e := <-cierre:
				return fmt.Errorf("conexión cerrada: %v", e)
			case d, ok := <-entregas:
				if !ok {
					return errors.New("el canal de entregas se cerró")
				}
				Despachar(log, h, d.Body)
			}
		}
	})
}

type encabezado struct {
	Tipo string `json:"tipo"`
}

// disponibilidad es lo que recibe el navegador en el evento "disponibilidad",
// con los mismos campos que GET /api/ofertas/{id}/disponibilidad.
type disponibilidad struct {
	OfertaID          string     `json:"ofertaId"`
	Estado            string     `json:"estado"`
	Libres            int        `json:"libres"`
	ProximaLiberacion *time.Time `json:"proximaLiberacion"`
	Version           int64      `json:"version"`
}

type retencionVencida struct {
	RetencionID string    `json:"retencionId"`
	OfertaID    string    `json:"ofertaId"`
	TitularID   string    `json:"titularId,omitempty"`
	ExpiraEn    time.Time `json:"expiraEn"`
}

// Despachar interpreta un mensaje de Catálogo y lo reparte. Lo que no entiende
// lo descarta (lector tolerante: un tipo nuevo no rompe el servicio).
func Despachar(log *slog.Logger, h *hub.Hub, cuerpo []byte) {
	var enc encabezado
	if err := json.Unmarshal(cuerpo, &enc); err != nil {
		eventosRecibidos.WithLabelValues("desconocido", "invalido").Inc()
		log.Warn("mensaje de Catálogo mal formado, se descarta", "error", err)
		return
	}
	switch enc.Tipo {
	case tipoDisponibilidadCambiada:
		var d disponibilidad
		if err := json.Unmarshal(cuerpo, &d); err != nil || d.OfertaID == "" {
			eventosRecibidos.WithLabelValues(enc.Tipo, "invalido").Inc()
			log.Warn("DisponibilidadCambiada mal formado, se descarta", "error", err)
			return
		}
		datos, _ := json.Marshal(d)
		contar(enc.Tipo, h.PublicarOferta(d.OfertaID, hub.Evento{Nombre: "disponibilidad", Datos: datos}))
	case tipoRetencionVencida:
		var r retencionVencida
		if err := json.Unmarshal(cuerpo, &r); err != nil || r.TitularID == "" {
			eventosRecibidos.WithLabelValues(enc.Tipo, "invalido").Inc()
			log.Warn("RetencionVencida mal formado, se descarta", "error", err)
			return
		}
		titular := r.TitularID
		r.TitularID = "" // el navegador no lo necesita
		datos, _ := json.Marshal(r)
		contar(enc.Tipo, h.PublicarUsuario(titular, hub.Evento{Nombre: "retencion-vencida", Datos: datos}))
	default:
		eventosRecibidos.WithLabelValues(enc.Tipo, "ignorado").Inc()
	}
}

func contar(tipo string, entregados int) {
	if entregados == 0 {
		eventosRecibidos.WithLabelValues(tipo, "sin_suscriptores").Inc()
		return
	}
	eventosRecibidos.WithLabelValues(tipo, "entregado").Add(float64(entregados))
}
