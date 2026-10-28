package observabilidad

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	capacidadColaLogs = 512
	timeoutEscritura  = 2 * time.Second
	insertarAppLog    = `INSERT INTO app_logs
		(fecha, nivel, servicio, mensaje, correlacion_id, trace_id, metadatos)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
)

type ejecutorSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type handlerCombinado struct {
	handlers []slog.Handler
}

func (h *handlerCombinado) Enabled(ctx context.Context, nivel slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, nivel) {
			return true
		}
	}
	return false
}

func (h *handlerCombinado) Handle(ctx context.Context, registro slog.Record) error {
	var errores []error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, registro.Level) {
			if err := handler.Handle(ctx, registro); err != nil {
				errores = append(errores, err)
			}
		}
	}
	return errors.Join(errores...)
}

func (h *handlerCombinado) WithAttrs(attrs []slog.Attr) slog.Handler {
	copia := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		copia[i] = handler.WithAttrs(attrs)
	}
	return &handlerCombinado{handlers: copia}
}

func (h *handlerCombinado) WithGroup(nombre string) slog.Handler {
	copia := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		copia[i] = handler.WithGroup(nombre)
	}
	return &handlerCombinado{handlers: copia}
}

type entradaLog struct {
	json []byte
}

type colaLogs struct {
	db        ejecutorSQL
	cola      chan entradaLog
	avisos    chan struct{}
	detener   chan struct{}
	terminado chan struct{}
	salida    io.Writer

	mu          sync.Mutex
	cerrada     bool
	descartados atomic.Uint64
}

type operacionHandler struct {
	attrs []slog.Attr
	group string
}

type handlerMySQL struct {
	cola        *colaLogs
	operaciones []operacionHandler
}

func nuevoLoggerConMySQL(servicio string, db ejecutorSQL, stdout, stderr io.Writer) (slog.Handler, func()) {
	hJSON := slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	q := &colaLogs{
		db:        db,
		cola:      make(chan entradaLog, capacidadColaLogs),
		avisos:    make(chan struct{}, 1),
		detener:   make(chan struct{}),
		terminado: make(chan struct{}),
		salida:    stderr,
	}
	go q.procesar()

	hMySQL := &handlerMySQL{cola: q}
	h := &handlerCombinado{handlers: []slog.Handler{hJSON, hMySQL}}
	return h.WithAttrs([]slog.Attr{slog.String("servicio", servicio)}), q.cerrar
}

func (h *handlerMySQL) Enabled(_ context.Context, nivel slog.Level) bool {
	return nivel >= slog.LevelInfo
}

func (h *handlerMySQL) Handle(ctx context.Context, registro slog.Record) error {
	var buffer bytes.Buffer
	var jsonHandler slog.Handler = slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelInfo})
	for _, operacion := range h.operaciones {
		if operacion.group != "" {
			jsonHandler = jsonHandler.WithGroup(operacion.group)
		} else {
			jsonHandler = jsonHandler.WithAttrs(operacion.attrs)
		}
	}
	if err := jsonHandler.Handle(ctx, registro); err != nil {
		return err
	}
	h.cola.encolar(entradaLog{json: bytes.Clone(buffer.Bytes())})
	return nil
}

func (h *handlerMySQL) WithAttrs(attrs []slog.Attr) slog.Handler {
	copia := h.copiar()
	copia.operaciones = append(copia.operaciones, operacionHandler{attrs: append([]slog.Attr(nil), attrs...)})
	return copia
}

func (h *handlerMySQL) WithGroup(nombre string) slog.Handler {
	if nombre == "" {
		return h
	}
	copia := h.copiar()
	copia.operaciones = append(copia.operaciones, operacionHandler{group: nombre})
	return copia
}

func (h *handlerMySQL) copiar() *handlerMySQL {
	return &handlerMySQL{
		cola:        h.cola,
		operaciones: append([]operacionHandler(nil), h.operaciones...),
	}
}

func (q *colaLogs) encolar(entrada entradaLog) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cerrada {
		q.registrarDescarte()
		return
	}
	select {
	case q.cola <- entrada:
	default:
		q.registrarDescarte()
	}
}

func (q *colaLogs) registrarDescarte() {
	q.descartados.Add(1)
	select {
	case q.avisos <- struct{}{}:
	default:
	}
}

func (q *colaLogs) cerrar() {
	q.mu.Lock()
	if !q.cerrada {
		q.cerrada = true
		close(q.detener)
	}
	q.mu.Unlock()
	<-q.terminado
}

func (q *colaLogs) procesar() {
	defer close(q.terminado)

	persistir := func(entrada entradaLog) error {
		var registro struct {
			Fecha       time.Time `json:"time"`
			Nivel       string    `json:"level"`
			Mensaje     string    `json:"msg"`
			Servicio    string    `json:"servicio"`
			Correlacion string    `json:"correlacion_id"`
			TraceID     string    `json:"trace_id"`
		}
		var campos map[string]json.RawMessage
		if err := json.Unmarshal(entrada.json, &campos); err != nil {
			return fmt.Errorf("decodificar log estructurado: %w", err)
		}
		if err := json.Unmarshal(entrada.json, &registro); err != nil {
			return fmt.Errorf("decodificar campos del log: %w", err)
		}

		for _, campo := range []string{"time", "level", "msg", "servicio", "correlacion_id", "trace_id"} {
			delete(campos, campo)
		}
		metadatos, err := json.Marshal(campos)
		if err != nil {
			return fmt.Errorf("codificar metadatos del log: %w", err)
		}

		ctx, cancelar := context.WithTimeout(context.Background(), timeoutEscritura)
		defer cancelar()
		_, err = q.db.ExecContext(ctx, insertarAppLog,
			registro.Fecha.UTC(), registro.Nivel, registro.Servicio, registro.Mensaje,
			valorNulo(registro.Correlacion), valorNulo(registro.TraceID), string(metadatos),
		)
		if err != nil {
			return fmt.Errorf("insertar log en app_logs: %w", err)
		}
		return nil
	}

	baseDeDatosCaida := false
	for {
		select {
		case entrada := <-q.cola:
			if err := persistir(entrada); err != nil {
				if !baseDeDatosCaida {
					fmt.Fprintf(q.salida, "observabilidad: no se pudo persistir en MySQL; los logs siguen en stdout: %v\n", err)
					baseDeDatosCaida = true
				}
				continue
			}
			if baseDeDatosCaida {
				fmt.Fprintln(q.salida, "observabilidad: se reanudó la persistencia de logs en MySQL")
				baseDeDatosCaida = false
			}
		case <-q.avisos:
			q.informarDescartes()
		case <-q.detener:
			for {
				select {
				case entrada := <-q.cola:
					if err := persistir(entrada); err != nil && !baseDeDatosCaida {
						fmt.Fprintf(q.salida, "observabilidad: no se pudo persistir en MySQL durante el cierre; los logs siguen en stdout: %v\n", err)
						baseDeDatosCaida = true
					}
				default:
					q.informarDescartes()
					return
				}
			}
		}
	}
}

func (q *colaLogs) informarDescartes() {
	if cantidad := q.descartados.Swap(0); cantidad > 0 {
		fmt.Fprintf(q.salida, "observabilidad: se descartaron %d logs sin persistir por saturación de la cola o durante el cierre\n", cantidad)
	}
}

func valorNulo(valor string) any {
	if valor == "" {
		return nil
	}
	return valor
}

var _ slog.Handler = (*handlerCombinado)(nil)
var _ slog.Handler = (*handlerMySQL)(nil)
