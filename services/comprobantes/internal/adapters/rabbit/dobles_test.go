package rabbit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

var logDescartado = slog.New(slog.NewTextHandler(io.Discard, nil))

var cfgTest = Config{EsperasReintento: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}.conDefectos()

// acuse registra qué se hizo con una entrega (implementa amqp.Acknowledger).
type acuse struct {
	acks, nacks int
	requeue     bool
}

func (a *acuse) Ack(uint64, bool) error { a.acks++; return nil }
func (a *acuse) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacks++
	a.requeue = requeue
	return nil
}
func (a *acuse) Reject(_ uint64, requeue bool) error { return a.Nack(0, false, requeue) }

func entrega(cuerpo string, headers amqp.Table) (amqp.Delivery, *acuse) {
	a := &acuse{}
	return amqp.Delivery{Acknowledger: a, DeliveryTag: 1, Body: []byte(cuerpo), Headers: headers}, a
}

// publicadorFalso registra lo publicado.
type publicadorFalso struct {
	mu       sync.Mutex
	err      error
	colas    []string
	mensajes []amqp.Publishing
}

func (p *publicadorFalso) publicar(_ context.Context, cola string, msg amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.colas = append(p.colas, cola)
	p.mensajes = append(p.mensajes, msg)
	return nil
}

// envioFalso es un doble de application.EnviarComprobante.
type envioFalso struct {
	mu            sync.Mutex
	err           error
	errFallido    error
	ejecutados    []string
	correlaciones []string
	fallidos      []string
}

func (e *envioFalso) Ejecutar(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ejecutados = append(e.ejecutados, id)
	e.correlaciones = append(e.correlaciones, observabilidad.Correlacion(ctx))
	return e.err
}

func (e *envioFalso) MarcarFallido(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errFallido != nil {
		return e.errFallido
	}
	e.fallidos = append(e.fallidos, id)
	return nil
}

func (e *envioFalso) cantidades() (ejecutados, fallidos int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ejecutados), len(e.fallidos)
}

// emisionFalsa es un doble de application.EmitirComprobante.
type emisionFalsa struct {
	mu          sync.Mutex
	err         error
	yaExistia   bool
	solicitudes []application.SolicitudEmision
}

func (e *emisionFalsa) Ejecutar(_ context.Context, s application.SolicitudEmision) (application.ResultadoEmision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.solicitudes = append(e.solicitudes, s)
	if e.err != nil {
		return application.ResultadoEmision{}, e.err
	}
	return application.ResultadoEmision{Comprobante: &domain.Comprobante{ID: "c-1"}, YaExistia: e.yaExistia}, nil
}

func (e *emisionFalsa) llamadas() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.solicitudes)
}

// outboxFalso es un doble de ports.Outbox.
type outboxFalso struct {
	mu         sync.Mutex
	eventos    []ports.EventoOutbox
	errLeer    error
	errMarcar  error
	publicados []uint64
}

func (o *outboxFalso) Pendientes(_ context.Context, limite int) ([]ports.EventoOutbox, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.errLeer != nil {
		return nil, o.errLeer
	}
	var pendientes []ports.EventoOutbox
	for _, ev := range o.eventos {
		if !o.publicado(ev.ID) && len(pendientes) < limite {
			pendientes = append(pendientes, ev)
		}
	}
	return pendientes, nil
}

func (o *outboxFalso) publicado(id uint64) bool {
	for _, p := range o.publicados {
		if p == id {
			return true
		}
	}
	return false
}

func (o *outboxFalso) MarcarPublicado(_ context.Context, id uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.errMarcar != nil {
		return o.errMarcar
	}
	o.publicados = append(o.publicados, id)
	return nil
}

func (o *outboxFalso) cantidadPublicados() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.publicados)
}

var errTemporal = errors.New("falla temporal")
