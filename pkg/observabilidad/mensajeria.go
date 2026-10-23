package observabilidad

import "context"

// Propagación de la correlación (y de la traza) en los headers de los mensajes
// de RabbitMQ y del outbox. Un amqp.Table se puede pasar directamente como headers.

// InyectarEnMensaje copia en headers el id de correlación del contexto.
// headers no puede ser nil.
//
// TODO(observabilidad): inyectar también el contexto de la traza (traceparent)
// con el propagador de OpenTelemetry.
func InyectarEnMensaje(ctx context.Context, headers map[string]any) {
	if id := Correlacion(ctx); id != "" {
		headers[HeaderCorrelacion] = id
	}
}

// ExtraerDeMensaje devuelve un contexto con el id de correlación que traen los
// headers del mensaje.
//
// TODO(observabilidad): extraer también el contexto de la traza, para que el
// consumidor continúe la misma traza distribuida.
func ExtraerDeMensaje(ctx context.Context, headers map[string]any) context.Context {
	if id, ok := headers[HeaderCorrelacion].(string); ok && id != "" {
		return ConCorrelacion(ctx, id)
	}
	return ctx
}

// ProcesarMensaje ejecuta fn con el contexto (correlación y traza) que traen los
// headers del mensaje. Los consumidores de RabbitMQ procesan cada mensaje con esto.
//
// TODO(observabilidad): abrir un span de consumidor llamado nombre, registrar en
// él el error de fn y cerrarlo.
func ProcesarMensaje(ctx context.Context, nombre string, headers map[string]any, fn func(context.Context) error) error {
	return fn(ExtraerDeMensaje(ctx, headers))
}
