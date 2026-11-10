package mensajeria

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/observabilidad"
)

var logDescartado = slog.New(slog.NewTextHandler(io.Discard, nil))

var colaTest = ColaConReintentos{Nombre: "prueba.cola", DLX: "prueba.dlx", Esperas: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}

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

func entrega(headers amqp.Table) (amqp.Delivery, *acuse) {
	a := &acuse{}
	return amqp.Delivery{Acknowledger: a, DeliveryTag: 1, Body: []byte(`{}`), Headers: headers}, a
}

type publicacion struct {
	exchange, routingKey string
	msg                  amqp.Publishing
}

type publicadorFalso struct {
	mu   sync.Mutex
	err  error
	hist []publicacion
}

func (p *publicadorFalso) Publicar(_ context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.hist = append(p.hist, publicacion{exchange, routingKey, msg})
	return nil
}

func TestConfirmarHaceAck(t *testing.T) {
	d, a := entrega(nil)
	Resolver(context.Background(), logDescartado, &publicadorFalso{}, colaTest, d, Confirmar)
	if a.acks != 1 || a.nacks != 0 {
		t.Fatalf("acks=%d nacks=%d", a.acks, a.nacks)
	}
}

func TestDescartarVaALaDLQ(t *testing.T) {
	d, a := entrega(nil)
	Resolver(context.Background(), logDescartado, &publicadorFalso{}, colaTest, d, Descartar)
	if a.nacks != 1 || a.requeue {
		t.Fatalf("nacks=%d requeue=%v; se esperaba nack sin requeue", a.nacks, a.requeue)
	}
}

func TestReintentarUsaEsperaCrecienteYCuentaIntentos(t *testing.T) {
	for n := range MaxIntentos {
		pub := &publicadorFalso{}
		headers := amqp.Table{observabilidad.HeaderCorrelacion: "corr-1"}
		if n > 0 {
			headers[HeaderIntentos] = int32(n)
		}
		d, a := entrega(headers)
		Resolver(context.Background(), logDescartado, pub, colaTest, d, Reintentar)

		if a.acks != 1 {
			t.Fatalf("intento %d: el original se confirma después de programar el reintento", n+1)
		}
		p := pub.hist[0]
		if p.exchange != "" || p.routingKey != colaTest.ColaReintento(colaTest.Esperas[n]) {
			t.Errorf("intento %d: publicado en %q/%q", n+1, p.exchange, p.routingKey)
		}
		if Intentos(p.msg.Headers) != n+1 || p.msg.Headers[observabilidad.HeaderCorrelacion] != "corr-1" {
			t.Errorf("intento %d: headers %v", n+1, p.msg.Headers)
		}
		if p.msg.DeliveryMode != amqp.Persistent {
			t.Error("el reintento no es persistente")
		}
	}
}

func TestSiNoSePuedeProgramarElReintentoVuelveALaCola(t *testing.T) {
	d, a := entrega(nil)
	Resolver(context.Background(), logDescartado, &publicadorFalso{err: errors.New("canal cerrado")}, colaTest, d, Reintentar)
	if a.nacks != 1 || !a.requeue || a.acks != 0 {
		t.Fatalf("acks=%d nacks=%d requeue=%v", a.acks, a.nacks, a.requeue)
	}
}

func TestProcesarEntregaPasaLaCorrelacionAlManejador(t *testing.T) {
	d, a := entrega(amqp.Table{observabilidad.HeaderCorrelacion: "corr-9"})
	var vista string
	ProcesarEntrega(context.Background(), logDescartado, &publicadorFalso{}, colaTest, "prueba", d,
		func(ctx context.Context, _ amqp.Delivery) (Resultado, error) {
			vista = observabilidad.Correlacion(ctx)
			return Confirmar, nil
		})
	if vista != "corr-9" || a.acks != 1 {
		t.Fatalf("correlación=%q acks=%d", vista, a.acks)
	}
}

type outboxFalso struct {
	eventos    []EventoOutbox
	errLeer    error
	publicados []uint64
}

func (o *outboxFalso) Pendientes(context.Context, int) ([]EventoOutbox, error) {
	return o.eventos, o.errLeer
}
func (o *outboxFalso) MarcarPublicado(_ context.Context, id uint64) error {
	o.publicados = append(o.publicados, id)
	return nil
}

func TestOutboxPublicaEnSuDestinoYMarcaDespuesDeConfirmar(t *testing.T) {
	ob := &outboxFalso{eventos: []EventoOutbox{
		{ID: 7, Tipo: "CompraConfirmada", Payload: []byte(`{"compraId":"c"}`), Headers: map[string]any{observabilidad.HeaderCorrelacion: "corr"}},
		{ID: 8, Tipo: "Desconocido", Payload: []byte(`{}`)},
	}}
	pub := &publicadorFalso{}
	p := NuevoPublicadorOutbox(ob, logDescartado, map[string]Destino{"CompraConfirmada": {RoutingKey: "comprobantes.compras"}})

	if err := p.PublicarPendientes(context.Background(), pub); err != nil {
		t.Fatal(err)
	}
	if len(pub.hist) != 1 || pub.hist[0].routingKey != "comprobantes.compras" || pub.hist[0].msg.Type != "CompraConfirmada" {
		t.Fatalf("publicaciones = %+v", pub.hist)
	}
	if pub.hist[0].msg.Headers[observabilidad.HeaderCorrelacion] != "corr" || pub.hist[0].msg.DeliveryMode != amqp.Persistent {
		t.Errorf("mensaje = %+v", pub.hist[0].msg)
	}
	if len(ob.publicados) != 1 || ob.publicados[0] != 7 {
		t.Errorf("marcados = %v; el tipo desconocido queda pendiente", ob.publicados)
	}
}

func TestOutboxNoMarcaSiElBrokerNoConfirma(t *testing.T) {
	ob := &outboxFalso{eventos: []EventoOutbox{{ID: 1, Tipo: "X", Payload: []byte(`{}`)}}}
	p := NuevoPublicadorOutbox(ob, logDescartado, map[string]Destino{"X": {RoutingKey: "x"}})
	if err := p.PublicarPendientes(context.Background(), &publicadorFalso{err: errors.New("nack")}); err == nil {
		t.Fatal("se esperaba error para reconectar")
	}
	if len(ob.publicados) != 0 {
		t.Error("no debía marcar un evento sin confirmar")
	}
}

func TestOutboxConLaBaseCaidaNoCortaLaConexion(t *testing.T) {
	p := NuevoPublicadorOutbox(&outboxFalso{errLeer: errors.New("base caída")}, logDescartado, nil)
	if err := p.PublicarPendientes(context.Background(), &publicadorFalso{}); err != nil {
		t.Fatal(err)
	}
}

func TestSinBrokerMantenerConexionNoDevuelveError(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancelar()
	err := MantenerConexion(ctx, "amqp://guest:guest@127.0.0.1:1/", logDescartado, "prueba",
		func(context.Context, *amqp.Connection) error { return nil })
	if err != nil {
		t.Fatalf("devolvió %v; debía reintentar hasta que se cancele el contexto", err)
	}
}
