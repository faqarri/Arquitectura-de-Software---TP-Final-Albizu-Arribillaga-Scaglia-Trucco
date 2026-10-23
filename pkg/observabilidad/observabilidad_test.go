package observabilidad

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestInyectarYExtraerDeMensajePropagaCorrelacionYTraza(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := ConCorrelacion(trace.ContextWithSpanContext(context.Background(), spanContext), "correlacion-123")
	headers := map[string]any{}

	InyectarEnMensaje(ctx, headers)
	extraido := ExtraerDeMensaje(context.Background(), headers)

	if headers[HeaderCorrelacion] != "correlacion-123" {
		t.Fatalf("header de correlación = %v, se esperaba correlacion-123", headers[HeaderCorrelacion])
	}
	if headers["traceparent"] == nil {
		t.Fatal("no se inyectó el header traceparent")
	}
	if got := Correlacion(extraido); got != "correlacion-123" {
		t.Fatalf("correlación extraída = %q, se esperaba correlacion-123", got)
	}
	gotSpanContext := trace.SpanContextFromContext(extraido)
	if gotSpanContext.TraceID() != spanContext.TraceID() || gotSpanContext.SpanID() != spanContext.SpanID() {
		t.Fatalf("contexto de traza extraído = %v/%v, se esperaba %v/%v",
			gotSpanContext.TraceID(), gotSpanContext.SpanID(), spanContext.TraceID(), spanContext.SpanID())
	}
}

func TestLoggerAgregaTraceID(t *testing.T) {
	var salida bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&salida, nil))
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	Logger(ctx, base).InfoContext(ctx, "prueba")

	var registro map[string]any
	if err := json.Unmarshal(salida.Bytes(), &registro); err != nil {
		t.Fatalf("no se pudo leer el log JSON: %v", err)
	}
	if got := registro["trace_id"]; got != spanContext.TraceID().String() {
		t.Fatalf("trace_id = %v, se esperaba %s", got, spanContext.TraceID())
	}
	if got := registro["span_id"]; got != spanContext.SpanID().String() {
		t.Fatalf("span_id = %v, se esperaba %s", got, spanContext.SpanID())
	}
}
