package observabilidad

import "context"

// IniciarTrazas configura OpenTelemetry para el servicio y devuelve la función
// que vacía y cierra el exportador al apagar.
//
// TODO(observabilidad): exportar las trazas a Jaeger por OTLP, instrumentar Gin
// (otelgin) y propagar el contexto de la traza en los headers de RabbitMQ, para
// que el flujo API → outbox → cola → worker quede en una sola traza distribuida.
func IniciarTrazas(ctx context.Context, servicio string) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}
