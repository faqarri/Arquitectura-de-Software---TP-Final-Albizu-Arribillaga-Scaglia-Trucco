package observabilidad

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type ejecutorSQLFuncion func(context.Context, string, ...any) (sql.Result, error)

func (f ejecutorSQLFuncion) ExecContext(ctx context.Context, consulta string, argumentos ...any) (sql.Result, error) {
	return f(ctx, consulta, argumentos...)
}

func TestLoggerConMySQLPersisteCamposYConservaSalidaJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var consulta string
	var argumentos []any
	handler, cerrar := nuevoLoggerConMySQL("comprobantes", ejecutorSQLFuncion(
		func(_ context.Context, query string, args ...any) (sql.Result, error) {
			consulta = query
			argumentos = args
			return nil, nil
		},
	), &stdout, &stderr)

	ctx := ConCorrelacion(context.Background(), "correlacion-123")
	Logger(ctx, slog.New(handler)).WarnContext(ctx, "solicitud procesada",
		"ruta", "/v1/comprobantes",
		"estado", 201,
	)
	cerrar()

	if !strings.Contains(stdout.String(), `"servicio":"comprobantes"`) ||
		!strings.Contains(stdout.String(), `"correlacion_id":"correlacion-123"`) {
		t.Fatalf("stdout no conservó los campos JSON: %s", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("stderr inesperado: %s", stderr.String())
	}
	if !strings.Contains(consulta, "INSERT INTO app_logs") || len(argumentos) != 7 {
		t.Fatalf("consulta o cantidad de argumentos inesperados: %q, %d", consulta, len(argumentos))
	}
	if nivel, ok := argumentos[1].(string); !ok || nivel != "WARN" {
		t.Errorf("nivel persistido = %v, se esperaba WARN", argumentos[1])
	}
	if servicio, ok := argumentos[2].(string); !ok || servicio != "comprobantes" {
		t.Errorf("servicio persistido = %v, se esperaba comprobantes", argumentos[2])
	}
	if correlacion, ok := argumentos[4].(string); !ok || correlacion != "correlacion-123" {
		t.Errorf("correlación persistida = %v", argumentos[4])
	}

	var metadatos map[string]any
	if err := json.Unmarshal([]byte(argumentos[6].(string)), &metadatos); err != nil {
		t.Fatalf("metadatos no son JSON válido: %v", err)
	}
	if metadatos["ruta"] != "/v1/comprobantes" || metadatos["estado"] != float64(201) {
		t.Errorf("metadatos inesperados: %#v", metadatos)
	}
	if fecha, ok := argumentos[0].(time.Time); !ok || fecha.Location() != time.UTC {
		t.Errorf("fecha persistida debe estar en UTC, valor: %#v", argumentos[0])
	}
}

func TestLoggerMySQLNoFallaSiLaBaseDeDatosNoEstaDisponible(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handler, cerrar := nuevoLoggerConMySQL("comprobantes", ejecutorSQLFuncion(
		func(context.Context, string, ...any) (sql.Result, error) {
			return nil, errors.New("mysql no disponible")
		},
	), &stdout, &stderr)

	slog.New(handler).Error("fallo de ejemplo")
	cerrar()

	if !strings.Contains(stdout.String(), `"msg":"fallo de ejemplo"`) {
		t.Fatalf("el log no se escribió en stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no se pudo persistir en MySQL") {
		t.Fatalf("la falla de persistencia no se informó: %s", stderr.String())
	}
}

func TestLoggerMySQLNoBloqueaSiLaColaSeLlenaYReportaDescartes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	inicioEscritura := make(chan struct{})
	liberarEscritura := make(chan struct{})
	var unaVez sync.Once
	handler, cerrar := nuevoLoggerConMySQL("comprobantes", ejecutorSQLFuncion(
		func(context.Context, string, ...any) (sql.Result, error) {
			unaVez.Do(func() { close(inicioEscritura) })
			<-liberarEscritura
			return nil, nil
		},
	), &stdout, &stderr)

	log := slog.New(handler)
	log.Info("registro que ocupa al worker")
	<-inicioEscritura
	for i := 0; i < capacidadColaLogs+2; i++ {
		log.Info("registro en cola")
	}
	close(liberarEscritura)
	cerrar()

	if !strings.Contains(stderr.String(), "se descartaron") {
		t.Fatalf("no se informó la saturación de la cola: %s", stderr.String())
	}
}
