// Package events conecta Catálogo con RabbitMQ usando pkg/mensajeria (ADR-005):
//
//   - publica DisponibilidadCambiada y RetencionVencida desde el outbox de MySQL
//     en el exchange fanout catalogo.disponibilidad (ADR-015; lo consumen
//     Reservas y Tiempo Real);
//   - publica OfertaActualizada desde el "outbox en el documento" de MongoDB en
//     el exchange fanout catalogo.ofertas (ADR-006);
//   - consume OfertaActualizada para invalidar la caché del detalle (ADR-007).
//
// Recibe lo que necesita como interfaces: no importa repository ni handlers.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/catalogo/internal/models"
	"agencia/pkg/mensajeria"
)

const (
	ExchangeDisponibilidad = "catalogo.disponibilidad"
	ExchangeOfertas        = "catalogo.ofertas"
	DLX                    = "catalogo.dlx"
	// ColaIndexacion la consume el indexador de la búsqueda (ADR-006). Se
	// declara acá para que ningún cambio se pierda aunque el indexador no esté corriendo.
	ColaIndexacion = "catalogo.indexacion"
	// ColaCache la consumen las instancias de Catálogo para invalidar Redis.
	// Redis es compartido, así que alcanza con que una instancia procese cada evento.
	ColaCache = "catalogo.cache"

	TipoOfertaActualizada      = "OfertaActualizada"
	TipoDisponibilidadCambiada = "DisponibilidadCambiada"
	TipoRetencionVencida       = "RetencionVencida"
)

func declararDisponibilidad(ch *amqp.Channel) error {
	return ch.ExchangeDeclare(ExchangeDisponibilidad, amqp.ExchangeFanout, true, false, false, false, nil)
}

var colaIndexacion = mensajeria.ColaConReintentos{Nombre: ColaIndexacion, DLX: DLX}

func declararOfertas(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeOfertas, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return err
	}
	if err := colaIndexacion.Declarar(ch); err != nil {
		return err
	}
	return ch.QueueBind(ColaIndexacion, "", ExchangeOfertas, false, nil)
}

// IniciarPublicadorOutbox publica los eventos del outbox de MySQL. Bloquea
// hasta que ctx se cancela.
func IniciarPublicadorOutbox(ctx context.Context, url string, log *slog.Logger, outbox mensajeria.Outbox) error {
	destino := mensajeria.Destino{Exchange: ExchangeDisponibilidad}
	return mensajeria.IniciarPublicadorOutbox(ctx, url, log, outbox, mensajeria.ConfigOutbox{
		Destinos: map[string]mensajeria.Destino{
			TipoDisponibilidadCambiada: destino,
			TipoRetencionVencida:       destino,
		},
		Topologia: declararDisponibilidad,
	})
}

// OfertasPendientes es lo que necesita el publicador de OfertaActualizada.
type OfertasPendientes interface {
	PendientesDePublicar(ctx context.Context, limite int) ([]models.CambioPendiente, error)
	MarcarPublicada(ctx context.Context, c models.CambioPendiente) error
}

type eventoOfertaActualizada struct {
	EventID  string `json:"eventId"`
	Tipo     string `json:"tipo"`
	OfertaID string `json:"ofertaId"`
	Version  int64  `json:"version"`
}

// IniciarPublicadorOfertas busca cada segundo las ofertas marcadas
// pendientePublicar, publica OfertaActualizada {ofertaId, version} con
// publisher confirms y recién después las desmarca, solo si la versión no
// cambió (ADR-006). Si dos instancias publican el mismo cambio, el evento llega
// duplicado; los consumidores son idempotentes por versión.
func IniciarPublicadorOfertas(ctx context.Context, url string, log *slog.Logger, ofertas OfertasPendientes) error {
	log = log.With("componente", "publicador de OfertaActualizada")
	return mensajeria.MantenerConexion(ctx, url, log, "publicador de ofertas", func(ctx context.Context, conn *amqp.Connection) error {
		if err := mensajeria.Declarar(conn, declararOfertas); err != nil {
			return err
		}
		pub, err := mensajeria.NuevoCanalConfirmado(conn)
		if err != nil {
			return err
		}
		defer pub.Cerrar()
		cierre := conn.NotifyClose(make(chan *amqp.Error, 1))
		tic := time.NewTicker(time.Second)
		defer tic.Stop()
		ultimoError := ""
		for {
			select {
			case <-ctx.Done():
				return nil
			case e := <-cierre:
				return fmt.Errorf("conexión cerrada: %v", e)
			case <-tic.C:
				err := publicarOfertas(ctx, log, pub, ofertas)
				if err != nil && err.Error() != ultimoError {
					log.Warn("no se pudieron publicar los cambios de ofertas; se reintenta", "error", err)
				}
				if err == nil {
					ultimoError = ""
				} else {
					ultimoError = err.Error()
				}
			}
		}
	})
}

func publicarOfertas(ctx context.Context, log *slog.Logger, pub mensajeria.Publicador, ofertas OfertasPendientes) error {
	pendientes, err := ofertas.PendientesDePublicar(ctx, 50)
	if err != nil {
		return err
	}
	for _, c := range pendientes {
		cuerpo, _ := json.Marshal(eventoOfertaActualizada{
			EventID: uuid.NewString(), Tipo: TipoOfertaActualizada, OfertaID: c.OfertaID, Version: c.Version,
		})
		if err := pub.Publicar(ctx, ExchangeOfertas, "", amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    fmt.Sprintf("oferta-%s-v%d", c.OfertaID, c.Version),
			Timestamp:    time.Now().UTC(),
			Type:         TipoOfertaActualizada,
			Body:         cuerpo,
		}); err != nil {
			return err
		}
		if err := ofertas.MarcarPublicada(ctx, c); err != nil {
			return fmt.Errorf("evento publicado pero no se pudo desmarcar la oferta (se publicará de nuevo): %w", err)
		}
		log.Info("OfertaActualizada publicada", "oferta_id", c.OfertaID, "version", c.Version)
	}
	return nil
}

// Indexador es lo que se usa de services.Busqueda.
type Indexador interface {
	Indexar(ctx context.Context, ofertaID string) error
}

// IniciarIndexador consume catalogo.indexacion y lleva cada oferta modificada
// al índice de búsqueda (ADR-006). Las dos instancias consumen la misma cola
// (consumidores en competencia): cada cambio lo indexa una sola. Si OpenSearch
// o MongoDB fallan, el mensaje se reintenta a los 5, 15 y 30 s y después va a
// catalogo.indexacion.dlq; esa oferta puede quedar desactualizada hasta
// reprocesar la DLQ o reconstruir el índice.
func IniciarIndexador(ctx context.Context, url string, log *slog.Logger, idx Indexador) error {
	log = log.With("componente", "indexador de búsqueda")
	cfg := mensajeria.ConfigConsumo{
		Cola:       colaIndexacion,
		NombreSpan: "indexar oferta",
		Topologia:  declararOfertas,
	}
	return mensajeria.MantenerConexion(ctx, url, log, "indexador de búsqueda", func(ctx context.Context, conn *amqp.Connection) error {
		return mensajeria.Consumir(ctx, conn, cfg, log, ManejadorIndexacion(log, idx))
	})
}

// ManejadorIndexacion procesa un OfertaActualizada. Exportado para probarlo sin broker.
func ManejadorIndexacion(log *slog.Logger, idx Indexador) mensajeria.Manejador {
	return func(ctx context.Context, d amqp.Delivery) (mensajeria.Resultado, error) {
		var ev eventoOfertaActualizada
		if err := json.Unmarshal(d.Body, &ev); err != nil || ev.OfertaID == "" {
			log.Error("OfertaActualizada mal formada: va a la DLQ", "cuerpo", string(d.Body))
			return mensajeria.Descartar, fmt.Errorf("OfertaActualizada mal formada")
		}
		err := idx.Indexar(ctx, ev.OfertaID)
		switch n := mensajeria.Intentos(d.Headers); {
		case err == nil:
			log.Debug("oferta indexada", "oferta_id", ev.OfertaID, "version", ev.Version)
			return mensajeria.Confirmar, nil
		case n < mensajeria.MaxIntentos:
			log.Warn("no se pudo indexar la oferta, se reintenta", "oferta_id", ev.OfertaID, "error", err, "intento", n+1)
			return mensajeria.Reintentar, err
		default:
			log.Error("se agotaron los reintentos: la oferta va a la DLQ y puede quedar desactualizada en la búsqueda",
				"oferta_id", ev.OfertaID, "error", err)
			return mensajeria.Descartar, err
		}
	}
}

// InvalidadorCache es lo que se usa de services.Ofertas.
type InvalidadorCache interface {
	InvalidarCache(ctx context.Context, ofertaID string)
}

var colaCache = mensajeria.ColaConReintentos{Nombre: ColaCache, DLX: DLX}

// IniciarConsumidorCache borra el detalle cacheado de cada oferta modificada.
// Borrar es idempotente: un evento repetido o desordenado no hace daño.
func IniciarConsumidorCache(ctx context.Context, url string, log *slog.Logger, cache InvalidadorCache) error {
	log = log.With("componente", "invalidación de caché")
	cfg := mensajeria.ConfigConsumo{
		Cola:       colaCache,
		NombreSpan: "invalidar caché de oferta",
		Topologia: func(ch *amqp.Channel) error {
			if err := declararOfertas(ch); err != nil {
				return err
			}
			return ch.QueueBind(ColaCache, "", ExchangeOfertas, false, nil)
		},
	}
	return mensajeria.MantenerConexion(ctx, url, log, "consumidor de caché", func(ctx context.Context, conn *amqp.Connection) error {
		return mensajeria.Consumir(ctx, conn, cfg, log, func(ctx context.Context, d amqp.Delivery) (mensajeria.Resultado, error) {
			var ev eventoOfertaActualizada
			if err := json.Unmarshal(d.Body, &ev); err != nil || ev.OfertaID == "" {
				log.Error("OfertaActualizada mal formada: va a la DLQ", "cuerpo", string(d.Body))
				return mensajeria.Descartar, fmt.Errorf("OfertaActualizada mal formada")
			}
			cache.InvalidarCache(ctx, ev.OfertaID)
			return mensajeria.Confirmar, nil
		})
	})
}
