// Package observabilidad reúne lo que comparten todos los servicios para logs
// estructurados, correlación de solicitudes y trazas (D11).
package observabilidad

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// NuevoLogger devuelve un logger que escribe JSON por stdout e incluye el nombre
// del servicio en cada línea.
func NuevoLogger(servicio string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h).With("servicio", servicio)
}

// NuevoLoggerConMySQL escribe cada log en JSON por stdout y, de forma
// asíncrona, intenta persistirlo en app_logs. La persistencia es best effort:
// una falla de MySQL no afecta la operación del servicio.
func NuevoLoggerConMySQL(servicio string, db *sql.DB) (*slog.Logger, func()) {
	handler, cerrar := nuevoLoggerConMySQL(servicio, db, os.Stdout, os.Stderr)
	return slog.New(handler), cerrar
}

// Logger devuelve el logger base con el id de correlación del contexto, si lo hay.
// Usarlo en todo log que pertenezca a una solicitud o a un mensaje.
func Logger(ctx context.Context, base *slog.Logger) *slog.Logger {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		base = base.With(
			"trace_id", spanContext.TraceID().String(),
			"span_id", spanContext.SpanID().String(),
		)
	}
	if id := Correlacion(ctx); id != "" {
		return base.With("correlacion_id", id)
	}
	return base
}
