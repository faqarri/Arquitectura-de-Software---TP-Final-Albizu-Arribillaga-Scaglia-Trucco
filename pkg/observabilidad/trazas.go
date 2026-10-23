package observabilidad

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// IniciarTrazas configura OpenTelemetry para el servicio y devuelve la función
// que vacía y cierra el exportador al apagar.
func IniciarTrazas(ctx context.Context, servicio string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		slog.WarnContext(ctx, "exportación de trazas deshabilitada: OTEL_EXPORTER_OTLP_ENDPOINT no está definida",
			"servicio", servicio,
		)
		return func(context.Context) error { return nil }, nil
	}

	exportador, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	recurso := resource.NewWithAttributes(
		"",
		attribute.String("service.name", servicio),
	)
	proveedor := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exportador),
		sdktrace.WithResource(recurso),
	)
	otel.SetTracerProvider(proveedor)

	return proveedor.Shutdown, nil
}
