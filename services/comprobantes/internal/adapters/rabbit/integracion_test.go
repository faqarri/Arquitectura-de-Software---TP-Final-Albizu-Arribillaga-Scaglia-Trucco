package rabbit

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

// TestIntegracionRabbitMQ levanta un RabbitMQ real con testcontainers y prueba
// la topología, el outbox, los reintentos y las DLQ de punta a punta. Necesita
// Docker; se saltea con go test -short o si Docker no está disponible.
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
	url, err := ctr.AmqpURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		URL:              url,
		EsperasReintento: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond},
		IntervaloOutbox:  100 * time.Millisecond,
		Prefetch:         2,
	}
	log := slog.New(slog.NewTextHandler(t.Output(), nil))

	ob := &outboxFalso{eventos: []ports.EventoOutbox{{
		ID: 1, Tipo: ports.EventoEnvioSolicitado, Payload: []byte(`{"comprobanteId":"c-1"}`),
		Headers: map[string]any{observabilidad.HeaderCorrelacion: "corr-integracion"},
	}}}
	envio := &envioFalso{err: errTemporal} // simula Mailpit apagado
	emision := &emisionFalsa{}

	ctxServicio, apagar := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, iniciar := range []func() error{
		func() error { return iniciarPublicadorOutbox(ctxServicio, cfg, log, ob) },
		func() error { return iniciarWorkerEnvios(ctxServicio, cfg, log, envio) },
		func() error { return iniciarConsumidorCompras(ctxServicio, cfg, log, emision) },
	} {
		wg.Go(func() {
			if err := iniciar(); err != nil {
				t.Errorf("un componente terminó con error: %v", err)
			}
		})
	}

	t.Run("envío que siempre falla: 1 intento + 3 reintentos, FALLIDO y DLQ", func(t *testing.T) {
		esperar(t, func() bool { _, fallidos := envio.cantidades(); return fallidos == 1 })
		esperar(t, func() bool { return mensajesEn(t, url, ColaEnvios+SufijoDLQ) == 1 })

		if ob.cantidadPublicados() != 1 {
			t.Error("el evento del outbox no quedó marcado como publicado")
		}
		envio.mu.Lock()
		defer envio.mu.Unlock()
		if len(envio.ejecutados) != 1+MaxIntentos {
			t.Errorf("intentos de envío = %d; se esperaban %d", len(envio.ejecutados), 1+MaxIntentos)
		}
		for i, c := range envio.correlaciones {
			if c != "corr-integracion" {
				t.Errorf("intento %d con correlación %q; se esperaba la de la solicitud original", i+1, c)
			}
		}
	})

	t.Run("CompraConfirmada mal formada va directo a la DLQ", func(t *testing.T) {
		publicarEn(t, url, ColaCompras, `{"compraId": "no-es-uuid"}`)
		esperar(t, func() bool { return mensajesEn(t, url, ColaCompras+SufijoDLQ) == 1 })
		if emision.llamadas() != 0 {
			t.Error("no debía llamar al caso de uso")
		}
	})

	t.Run("CompraConfirmada válida emite el comprobante", func(t *testing.T) {
		publicarEn(t, url, ColaCompras, compraValida)
		esperar(t, func() bool { return emision.llamadas() == 1 })
		esperar(t, func() bool { return mensajesEn(t, url, ColaCompras) == 0 })
	})

	apagar()
	wg.Wait()
}

func esperar(t *testing.T, condicion func() bool) {
	t.Helper()
	limite := time.Now().Add(30 * time.Second)
	for !condicion() {
		if time.Now().After(limite) {
			t.Fatal("la condición no se cumplió en 30 s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func mensajesEn(t *testing.T, url, cola string) int {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	q, err := ch.QueueDeclarePassive(cola, true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return q.Messages
}

func publicarEn(t *testing.T, url, cola, cuerpo string) {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pub, err := nuevoCanalConfirmado(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.cerrar()
	if err := pub.publicar(context.Background(), cola, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: []byte(cuerpo),
	}); err != nil {
		t.Fatal(err)
	}
}
