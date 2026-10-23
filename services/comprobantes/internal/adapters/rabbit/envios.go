package rabbit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

// casoEnvio es lo que el worker usa de application.EnviarComprobante.
type casoEnvio interface {
	Ejecutar(ctx context.Context, id string) error
	MarcarFallido(ctx context.Context, id string) error
}

var _ casoEnvio = (*application.EnviarComprobante)(nil)

// mensajeEnvio es el payload de EnvioSolicitado, escrito en el outbox al emitir.
type mensajeEnvio struct {
	ComprobanteID string `json:"comprobanteId"`
}

var errMensajeMalFormado = errors.New("mensaje mal formado")

func iniciarWorkerEnvios(ctx context.Context, cfg Config, log *slog.Logger, uc casoEnvio) error {
	cfg = cfg.conDefectos()
	log = log.With("componente", "worker de envíos")
	return mantenerConexion(ctx, cfg, log, "worker de envíos", func(ctx context.Context, conn *amqp.Connection) error {
		return consumir(ctx, conn, cfg, ColaEnvios, func(ctx context.Context, d amqp.Delivery, pub publicador) {
			manejarEnvio(ctx, log, cfg, uc, pub, d)
		})
	})
}

func manejarEnvio(ctx context.Context, log *slog.Logger, cfg Config, uc casoEnvio, pub publicador, d amqp.Delivery) {
	var r resultado
	_ = observabilidad.ProcesarMensaje(ctx, "enviar comprobante", d.Headers, func(ctx context.Context) error {
		var err error
		r, err = procesarEnvio(ctx, observabilidad.Logger(ctx, log), cfg, uc, d)
		return err
	})
	// La correlación se recupera de nuevo para los logs de resolver.
	resolver(ctx, observabilidad.Logger(observabilidad.ExtraerDeMensaje(ctx, d.Headers), log), cfg, pub, ColaEnvios, d, r)
}

// procesarEnvio envía el correo y decide qué hacer con el mensaje. El error
// devuelto es solo para la traza; la decisión es el resultado.
func procesarEnvio(ctx context.Context, log *slog.Logger, cfg Config, uc casoEnvio, d amqp.Delivery) (resultado, error) {
	var m mensajeEnvio
	if err := json.Unmarshal(d.Body, &m); err != nil || m.ComprobanteID == "" {
		log.Error("mensaje de envío mal formado: va a la DLQ", "error", err, "cuerpo", string(d.Body))
		return descartar, errMensajeMalFormado
	}
	n := intentos(d.Headers)
	log = log.With("comprobante_id", m.ComprobanteID, "intento", n+1)

	err := uc.Ejecutar(ctx, m.ComprobanteID)
	switch {
	case err == nil:
		log.Info("envío de comprobante procesado")
		return confirmar, nil
	case errors.Is(err, ports.ErrNoEncontrado):
		log.Error("el comprobante no existe: el mensaje va a la DLQ")
		return descartar, err
	case n < MaxIntentos:
		log.Warn("falló el envío del comprobante, se reintenta", "error", err, "espera", esperaReintento(cfg, n).String())
		return reintentar, err
	}

	if errFallido := uc.MarcarFallido(ctx, m.ComprobanteID); errFallido != nil {
		// Sin el estado FALLIDO guardado, el mensaje no puede ir a la DLQ: el
		// comprobante quedaría PENDIENTE para siempre. Se vuelve a intentar más tarde.
		log.Error("se agotaron los reintentos pero no se pudo marcar FALLIDO, se reintenta", "error", errFallido)
		return reintentar, errFallido
	}
	log.Error("se agotaron los reintentos de envío: comprobante FALLIDO, el mensaje va a la DLQ", "error", err)
	return descartar, err
}
