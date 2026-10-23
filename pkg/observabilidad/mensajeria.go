package observabilidad

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Propagación de la correlación (y de la traza) en los headers de los mensajes
// de RabbitMQ y del outbox. Un amqp.Table se puede pasar directamente como headers.

// InyectarEnMensaje copia en headers el id de correlación del contexto.
// headers no puede ser nil.
func InyectarEnMensaje(ctx context.Context, headers map[string]any) {
	if id := Correlacion(ctx); id != "" {
		headers[HeaderCorrelacion] = id
	}
	otel.GetTextMapPropagator().Inject(ctx, carrierHeaders(headers))
}

// ExtraerDeMensaje devuelve un contexto con el id de correlación que traen los
// headers del mensaje.
func ExtraerDeMensaje(ctx context.Context, headers map[string]any) context.Context {
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrierHeaders(headers))
	if id, ok := headers[HeaderCorrelacion].(string); ok && id != "" {
		return ConCorrelacion(ctx, id)
	}
	return ctx
}

// ProcesarMensaje ejecuta fn con el contexto (correlación y traza) que traen los
// headers del mensaje. Los consumidores de RabbitMQ procesan cada mensaje con esto.
func ProcesarMensaje(ctx context.Context, nombre string, headers map[string]any, fn func(context.Context) error) error {
	ctx, span := otel.Tracer("agencia/pkg/observabilidad").Start(
		ExtraerDeMensaje(ctx, headers),
		nombre,
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

type carrierHeaders map[string]any

func (h carrierHeaders) Get(key string) string {
	for k, value := range h {
		if strings.EqualFold(k, key) {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

func (h carrierHeaders) Set(key, value string) {
	for existing := range h {
		if strings.EqualFold(existing, key) {
			delete(h, existing)
		}
	}
	h[key] = value
}

func (h carrierHeaders) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}
