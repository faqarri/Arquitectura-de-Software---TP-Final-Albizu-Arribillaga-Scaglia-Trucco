package consumidor

// Test de integración contra RabbitMQ real (contenedor rabbitmq:3.13, la misma
// versión del docker-compose). Verifica lo que los tests unitarios no pueden:
// que el consumidor declara su cola exclusiva, la enlaza al fanout de Catálogo
// y recibe los eventos tal como los publica Catálogo, y que cada instancia
// recibe todos los eventos (escala sin coordinación, ADR-015).
// Necesita Docker; con go test -short se saltea.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"agencia/tiempo-real/internal/hub"
)

func TestConsumidorContraRabbitMQReal(t *testing.T) {
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()

	ctr, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
	if err != nil {
		t.Fatalf("no se pudo levantar RabbitMQ (¿Docker está corriendo?): %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })
	url, err := ctr.AmqpURL(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Dos instancias de Tiempo Real, cada una con su hub, como dos contenedores.
	instancias := []*hub.Hub{hub.Nuevo(), hub.Nuevo()}
	var ofertas, usuarios []*hub.Suscripcion
	for _, h := range instancias {
		ofertas = append(ofertas, h.SuscribirOfertas([]string{"of-1"}))
		usuarios = append(usuarios, h.SuscribirUsuario("u-1"))
		go func() { _ = Iniciar(ctx, url, logMudo, h) }()
	}

	// Catálogo publica en el fanout, con persistencia, como su publicador de outbox.
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.ExchangeDeclare(Exchange, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	publicar := func(cuerpo string) {
		t.Helper()
		if err := ch.PublishWithContext(ctx, Exchange, "", false, false, amqp.Publishing{
			ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: []byte(cuerpo),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Un fanout solo entrega a las colas ya enlazadas: se republica hasta que
	// las dos instancias terminaron de conectarse y recibieron el aviso.
	const disponibilidad = `{"eventId":"e1","tipo":"DisponibilidadCambiada","ofertaId":"of-1","estado":"RETENIDA","libres":0,"proximaLiberacion":"2026-12-01T10:15:00Z","version":7}`
	for i, s := range ofertas {
		e := esperarEvento(t, s, func() { publicar(disponibilidad) })
		var d map[string]any
		_ = json.Unmarshal(e.Datos, &d)
		if e.Nombre != "disponibilidad" || d["estado"] != "RETENIDA" || d["version"] != float64(7) {
			t.Fatalf("instancia %d: evento %q con datos %s", i+1, e.Nombre, e.Datos)
		}
	}

	// Un mensaje mal formado no tira abajo el consumidor: el siguiente evento llega igual.
	publicar(`esto no es JSON`)
	publicar(`{"eventId":"e2","tipo":"RetencionVencida","retencionId":"r-9","ofertaId":"of-1","titularId":"u-1","expiraEn":"2026-12-01T10:15:00Z"}`)
	for i, s := range usuarios {
		e := esperarEvento(t, s, nil)
		var r map[string]any
		_ = json.Unmarshal(e.Datos, &r)
		if e.Nombre != "retencion-vencida" || r["retencionId"] != "r-9" || r["titularId"] != nil {
			t.Fatalf("instancia %d: evento %q con datos %s", i+1, e.Nombre, e.Datos)
		}
	}
}

// esperarEvento espera hasta 20 s un evento en la suscripción. Si reintentar no
// es nil, lo ejecuta cada medio segundo mientras espera.
func esperarEvento(t *testing.T, s *hub.Suscripcion, reintentar func()) hub.Evento {
	t.Helper()
	limite := time.After(20 * time.Second)
	tic := time.NewTicker(500 * time.Millisecond)
	defer tic.Stop()
	if reintentar != nil {
		reintentar()
	}
	for {
		select {
		case e := <-s.Eventos:
			return e
		case <-tic.C:
			if reintentar != nil {
				reintentar()
			}
		case <-limite:
			t.Fatal("el evento no llegó en 20 s")
		}
	}
}
