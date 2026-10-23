package rabbit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"unicode/utf8"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/pkg/observabilidad"
)

// casoEmision es lo que el consumidor usa de application.EmitirComprobante.
type casoEmision interface {
	Ejecutar(ctx context.Context, s application.SolicitudEmision) (application.ResultadoEmision, error)
}

var _ casoEmision = (*application.EmitirComprobante)(nil)

// eventoCompraConfirmada es el evento que publica Reservas en ColaCompras.
// Formato documentado en el README del servicio y en ADR-005.
type eventoCompraConfirmada struct {
	EventID  string `json:"eventId"`
	CompraID string `json:"compraId"`
	Email    string `json:"email"`
	Items    []struct {
		Descripcion string `json:"descripcion"`
		// Puntero para distinguir un precio ausente de un precio 0. decimal lee
		// el número JSON sin pasar por float64.
		Precio *decimal.Decimal `json:"precio"`
	} `json:"items"`
}

// solicitud valida el evento y lo convierte en la solicitud del caso de uso.
// Las reglas de importes (ítems, precios negativos, total) las valida el dominio.
func (e eventoCompraConfirmada) solicitud() (application.SolicitudEmision, error) {
	if _, err := uuid.Parse(e.CompraID); err != nil {
		return application.SolicitudEmision{}, fmt.Errorf("compraId inválido: %q", e.CompraID)
	}
	if _, err := mail.ParseAddress(e.Email); err != nil || len(e.Email) > 254 {
		return application.SolicitudEmision{}, fmt.Errorf("email inválido: %q", e.Email)
	}
	items := make([]domain.Item, len(e.Items))
	for i, it := range e.Items {
		if n := utf8.RuneCountInString(it.Descripcion); n == 0 || n > 200 {
			return application.SolicitudEmision{}, fmt.Errorf("items[%d].descripcion debe tener entre 1 y 200 caracteres", i)
		}
		if it.Precio == nil {
			return application.SolicitudEmision{}, fmt.Errorf("items[%d].precio es obligatorio", i)
		}
		items[i] = domain.Item{Descripcion: it.Descripcion, Precio: *it.Precio}
	}
	return application.SolicitudEmision{
		Consumidor:     domain.ConsumidorInterno,
		IdempotencyKey: e.CompraID,
		Email:          e.Email,
		Items:          items,
	}, nil
}

// errorPermanente indica que reintentar no cambiaría el resultado.
func errorPermanente(err error) bool {
	return errors.Is(err, domain.ErrSinItems) ||
		errors.Is(err, domain.ErrPrecioNegativo) ||
		errors.Is(err, domain.ErrTotalCero) ||
		errors.Is(err, domain.ErrImporteFueraDeRango) ||
		// El mismo compraId con otro contenido: es un error del productor.
		errors.Is(err, application.ErrConflictoIdempotencia)
}

func iniciarConsumidorCompras(ctx context.Context, cfg Config, log *slog.Logger, uc casoEmision) error {
	cfg = cfg.conDefectos()
	log = log.With("componente", "consumidor de compras")
	return mantenerConexion(ctx, cfg, log, "consumidor de compras", func(ctx context.Context, conn *amqp.Connection) error {
		return consumir(ctx, conn, cfg, ColaCompras, func(ctx context.Context, d amqp.Delivery, pub publicador) {
			manejarCompra(ctx, log, cfg, uc, pub, d)
		})
	})
}

func manejarCompra(ctx context.Context, log *slog.Logger, cfg Config, uc casoEmision, pub publicador, d amqp.Delivery) {
	var r resultado
	_ = observabilidad.ProcesarMensaje(ctx, "emitir comprobante de compra", d.Headers, func(ctx context.Context) error {
		var err error
		r, err = procesarCompra(ctx, observabilidad.Logger(ctx, log), cfg, uc, d)
		return err
	})
	resolver(ctx, observabilidad.Logger(observabilidad.ExtraerDeMensaje(ctx, d.Headers), log), cfg, pub, ColaCompras, d, r)
}

// procesarCompra emite el comprobante y decide qué hacer con el mensaje. El
// error devuelto es solo para la traza; la decisión es el resultado.
func procesarCompra(ctx context.Context, log *slog.Logger, cfg Config, uc casoEmision, d amqp.Delivery) (resultado, error) {
	var e eventoCompraConfirmada
	if err := json.Unmarshal(d.Body, &e); err != nil {
		log.Error("CompraConfirmada mal formada: va a la DLQ", "error", err, "cuerpo", string(d.Body))
		return descartar, fmt.Errorf("%w: %v", errMensajeMalFormado, err)
	}
	log = log.With("event_id", e.EventID, "compra_id", e.CompraID)
	s, err := e.solicitud()
	if err != nil {
		log.Error("CompraConfirmada inválida: va a la DLQ", "error", err)
		return descartar, fmt.Errorf("%w: %v", errMensajeMalFormado, err)
	}

	n := intentos(d.Headers)
	res, err := uc.Ejecutar(ctx, s)
	switch {
	case err == nil:
		log.Info("comprobante de compra emitido", "comprobante_id", res.Comprobante.ID, "ya_existia", res.YaExistia)
		return confirmar, nil
	case errorPermanente(err):
		log.Error("CompraConfirmada rechazada por una regla de negocio: va a la DLQ", "error", err)
		return descartar, err
	case n < MaxIntentos:
		log.Warn("falló la emisión del comprobante, se reintenta", "error", err, "intento", n+1, "espera", esperaReintento(cfg, n).String())
		return reintentar, err
	}
	log.Error("se agotaron los reintentos de emisión: el mensaje va a la DLQ", "error", err, "intento", n+1)
	return descartar, err
}
