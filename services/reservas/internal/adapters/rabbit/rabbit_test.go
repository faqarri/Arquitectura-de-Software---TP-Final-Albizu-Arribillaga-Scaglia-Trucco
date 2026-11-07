package rabbit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"agencia/pkg/mensajeria"
	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/adapters/mysql"
)

var logDescartado = slog.New(slog.NewTextHandler(io.Discard, nil))

type vencidas struct {
	mu            sync.Mutex
	err           error
	ids           []string
	correlaciones []string
}

func (v *vencidas) MarcarRetencionVencida(ctx context.Context, id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ids = append(v.ids, id)
	v.correlaciones = append(v.correlaciones, observabilidad.Correlacion(ctx))
	return v.err
}

func (v *vencidas) cantidad() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.ids)
}

func entrega(tipo, cuerpo string, intentos int) amqp.Delivery {
	h := amqp.Table{}
	if intentos > 0 {
		h[mensajeria.HeaderIntentos] = int32(intentos)
	}
	return amqp.Delivery{Type: tipo, Body: []byte(cuerpo), Headers: h}
}

func TestManejarRetencionVencida(t *testing.T) {
	uc := &vencidas{}
	r, err := manejarRetencion(logDescartado, uc)(context.Background(), entrega(TipoRetencionVencida, `{"retencionId":"r1"}`, 0))
	if r != mensajeria.Confirmar || err != nil || uc.ids[0] != "r1" {
		t.Fatalf("r=%v err=%v ids=%v", r, err, uc.ids)
	}
}

func TestIgnoraOtrosEventosDelFanout(t *testing.T) {
	uc := &vencidas{}
	r, _ := manejarRetencion(logDescartado, uc)(context.Background(), entrega("DisponibilidadCambiada", `{"ofertaId":"o1"}`, 0))
	if r != mensajeria.Confirmar || uc.cantidad() != 0 {
		t.Fatalf("r=%v llamadas=%d", r, uc.cantidad())
	}
}

func TestTipoEnElCuerpoSiNoVieneEnLaPropiedad(t *testing.T) {
	uc := &vencidas{}
	r, _ := manejarRetencion(logDescartado, uc)(context.Background(), entrega("", `{"tipo":"RetencionVencida","retencionId":"r9"}`, 0))
	if r != mensajeria.Confirmar || uc.cantidad() != 1 {
		t.Fatalf("r=%v", r)
	}
}

func TestMalFormadaVaALaDLQ(t *testing.T) {
	for _, cuerpo := range []string{`no es json`, `{}`} {
		r, _ := manejarRetencion(logDescartado, &vencidas{})(context.Background(), entrega(TipoRetencionVencida, cuerpo, 0))
		if r != mensajeria.Descartar {
			t.Errorf("%q: r=%v", cuerpo, r)
		}
	}
}

func TestErrorSeReintentaYDespuesVaALaDLQ(t *testing.T) {
	uc := &vencidas{err: errors.New("base caída")}
	m := manejarRetencion(logDescartado, uc)
	if r, _ := m(context.Background(), entrega(TipoRetencionVencida, `{"retencionId":"r1"}`, 0)); r != mensajeria.Reintentar {
		t.Errorf("primer fallo: %v", r)
	}
	if r, _ := m(context.Background(), entrega(TipoRetencionVencida, `{"retencionId":"r1"}`, mensajeria.MaxIntentos)); r != mensajeria.Descartar {
		t.Errorf("agotado: %v", r)
	}
}

// ---------- Integración contra RabbitMQ real ----------

type outboxUnEvento struct {
	mu         sync.Mutex
	publicados []uint64
}

func (o *outboxUnEvento) Pendientes(context.Context, int) ([]mensajeria.EventoOutbox, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.publicados) > 0 {
		return nil, nil
	}
	return []mensajeria.EventoOutbox{{ID: 1, Tipo: mysql.TipoCompraConfirmada,
		Payload: []byte(`{"eventId":"e1","compraId":"c1","email":"a@b.example","items":[{"descripcion":"Vuelo","precio":100.00}]}`),
		Headers: map[string]any{observabilidad.HeaderCorrelacion: "corr-reservas"}}}, nil
}

func (o *outboxUnEvento) MarcarPublicado(_ context.Context, id uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.publicados = append(o.publicados, id)
	return nil
}

func TestIntegracionRabbitMQ(t *testing.T) {
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
	ctr, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	url, _ := ctr.AmqpURL(ctx)

	ctxServicio, apagar := context.WithCancel(ctx)
	defer apagar()
	ob := &outboxUnEvento{}
	uc := &vencidas{}
	go func() { _ = IniciarPublicadorOutbox(ctxServicio, url, logDescartado, ob) }()
	go func() { _ = IniciarConsumidorRetenciones(ctxServicio, url, logDescartado, uc) }()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 1. CompraConfirmada llega a la cola de Comprobantes, con su correlación.
	// Un canal nuevo por intento: consultar una cola que todavía no existe cierra el canal.
	var msg amqp.Delivery
	esperar(t, func() bool {
		c, err := conn.Channel()
		if err != nil {
			return false
		}
		defer c.Close()
		var ok bool
		msg, ok, _ = c.Get(ColaComprobantes, true)
		return ok
	})
	if msg.Type != mysql.TipoCompraConfirmada || msg.Headers[observabilidad.HeaderCorrelacion] != "corr-reservas" || msg.DeliveryMode != amqp.Persistent {
		t.Errorf("mensaje = %+v", msg)
	}

	// 2. RetencionVencida publicada en el fanout de Catálogo llega al caso de uso.
	esperar(t, func() bool {
		c, err := conn.Channel()
		if err != nil {
			return false
		}
		defer c.Close()
		_, err = c.QueueDeclarePassive(ColaRetenciones, true, false, false, false, nil)
		return err == nil
	})
	ch, _ := conn.Channel()
	for _, tipo := range []string{"DisponibilidadCambiada", TipoRetencionVencida} {
		if err := ch.PublishWithContext(ctx, ExchangeDisponibilidad, "", false, false, amqp.Publishing{
			Type: tipo, ContentType: "application/json", Body: []byte(`{"retencionId":"r-vencida","ofertaId":"o1"}`),
			Headers: amqp.Table{observabilidad.HeaderCorrelacion: "corr-catalogo"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	esperar(t, func() bool { return uc.cantidad() == 1 })
	if uc.ids[0] != "r-vencida" || uc.correlaciones[0] != "corr-catalogo" {
		t.Errorf("ids=%v correlaciones=%v", uc.ids, uc.correlaciones)
	}
}

func esperar(t *testing.T, condicion func() bool) {
	t.Helper()
	limite := time.Now().Add(30 * time.Second)
	for !condicion() {
		if time.Now().After(limite) {
			t.Fatal("la condición no se cumplió en 30 s")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
