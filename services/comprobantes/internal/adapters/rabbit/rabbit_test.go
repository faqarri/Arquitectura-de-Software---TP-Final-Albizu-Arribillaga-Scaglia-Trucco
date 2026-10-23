package rabbit

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

// --- Conexión ---

func TestSinRabbitMQReintentaLaConexionSinDevolverError(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancelar()
	var b strings.Builder
	log := slog.New(slog.NewTextHandler(&b, nil))

	// Puerto 1: nadie escucha, la conexión se rechaza.
	err := iniciarWorkerEnvios(ctx, Config{URL: "amqp://guest:guest@127.0.0.1:1/"}, log, &envioFalso{})

	if err != nil {
		t.Fatalf("devolvió %v; sin broker debía seguir reintentando hasta que se cancele el contexto", err)
	}
	if n := strings.Count(b.String(), "no se pudo conectar a RabbitMQ"); n < 2 {
		t.Errorf("se registraron %d intentos de conexión; se esperaban al menos 2", n)
	}
}

// --- Worker de envíos ---

func TestEnvioExitosoConfirmaElMensaje(t *testing.T) {
	uc := &envioFalso{}
	pub := &publicadorFalso{}
	d, a := entrega(`{"comprobanteId":"c-1"}`, amqp.Table{observabilidad.HeaderCorrelacion: "corr-1"})

	manejarEnvio(context.Background(), logDescartado, cfgTest, uc, pub, d)

	if a.acks != 1 || a.nacks != 0 || len(pub.colas) != 0 {
		t.Fatalf("acks=%d nacks=%d publicados=%d; se esperaba solo un ack", a.acks, a.nacks, len(pub.colas))
	}
	if uc.ejecutados[0] != "c-1" {
		t.Errorf("se envió %q", uc.ejecutados[0])
	}
	if uc.correlaciones[0] != "corr-1" {
		t.Errorf("el caso de uso recibió la correlación %q; se esperaba corr-1", uc.correlaciones[0])
	}
}

func TestEnvioFallidoSeReintentaConEsperaCreciente(t *testing.T) {
	uc := &envioFalso{err: errTemporal}
	for n := range MaxIntentos {
		pub := &publicadorFalso{}
		headers := amqp.Table{observabilidad.HeaderCorrelacion: "corr-1"}
		if n > 0 {
			headers[HeaderIntentos] = int32(n)
		}
		d, a := entrega(`{"comprobanteId":"c-1"}`, headers)

		manejarEnvio(context.Background(), logDescartado, cfgTest, uc, pub, d)

		if a.acks != 1 || a.nacks != 0 {
			t.Fatalf("intento %d: acks=%d nacks=%d; el original se confirma después de programar el reintento", n+1, a.acks, a.nacks)
		}
		esperada := colaReintento(ColaEnvios, cfgTest.EsperasReintento[n])
		if len(pub.colas) != 1 || pub.colas[0] != esperada {
			t.Fatalf("intento %d: publicado en %v; se esperaba %s", n+1, pub.colas, esperada)
		}
		msg := pub.mensajes[0]
		if got := intentos(msg.Headers); got != n+1 {
			t.Errorf("intento %d: %s = %d; se esperaba %d", n+1, HeaderIntentos, got, n+1)
		}
		if msg.Headers[observabilidad.HeaderCorrelacion] != "corr-1" {
			t.Errorf("el reintento perdió la correlación")
		}
		if msg.DeliveryMode != amqp.Persistent {
			t.Errorf("el reintento no es persistente")
		}
	}
	if _, fallidos := uc.cantidades(); fallidos != 0 {
		t.Errorf("se marcó FALLIDO antes de agotar los reintentos")
	}
}

func TestEnvioAgotadoMarcaFallidoYVaALaDLQ(t *testing.T) {
	uc := &envioFalso{err: errTemporal}
	pub := &publicadorFalso{}
	d, a := entrega(`{"comprobanteId":"c-1"}`, amqp.Table{HeaderIntentos: int32(MaxIntentos)})

	manejarEnvio(context.Background(), logDescartado, cfgTest, uc, pub, d)

	if a.nacks != 1 || a.requeue || a.acks != 0 {
		t.Fatalf("acks=%d nacks=%d requeue=%v; se esperaba nack sin requeue (DLQ)", a.acks, a.nacks, a.requeue)
	}
	if len(uc.fallidos) != 1 || uc.fallidos[0] != "c-1" {
		t.Errorf("fallidos = %v; se esperaba c-1", uc.fallidos)
	}
	if len(pub.colas) != 0 {
		t.Errorf("no debía programar otro reintento")
	}
}

func TestEnvioAgotadoSinPoderMarcarFallidoSeVuelveAIntentar(t *testing.T) {
	uc := &envioFalso{err: errTemporal, errFallido: errors.New("base caída")}
	pub := &publicadorFalso{}
	d, a := entrega(`{"comprobanteId":"c-1"}`, amqp.Table{HeaderIntentos: int32(MaxIntentos)})

	manejarEnvio(context.Background(), logDescartado, cfgTest, uc, pub, d)

	if a.nacks != 0 || a.acks != 1 {
		t.Fatalf("acks=%d nacks=%d; no debía ir a la DLQ sin marcar FALLIDO", a.acks, a.nacks)
	}
	ultima := cfgTest.EsperasReintento[len(cfgTest.EsperasReintento)-1]
	if len(pub.colas) != 1 || pub.colas[0] != colaReintento(ColaEnvios, ultima) {
		t.Errorf("publicado en %v; se esperaba la última cola de reintento", pub.colas)
	}
}

func TestEnvioMalFormadoVaALaDLQSinLlamarAlCasoDeUso(t *testing.T) {
	for _, cuerpo := range []string{`no es json`, `{}`, `{"comprobanteId":""}`} {
		uc := &envioFalso{}
		d, a := entrega(cuerpo, nil)
		manejarEnvio(context.Background(), logDescartado, cfgTest, uc, &publicadorFalso{}, d)
		if a.nacks != 1 || a.requeue {
			t.Errorf("%q: nacks=%d requeue=%v; se esperaba DLQ", cuerpo, a.nacks, a.requeue)
		}
		if len(uc.ejecutados) != 0 {
			t.Errorf("%q: no debía llamar al caso de uso", cuerpo)
		}
	}
}

func TestEnvioDeComprobanteInexistenteVaALaDLQ(t *testing.T) {
	uc := &envioFalso{err: ports.ErrNoEncontrado}
	d, a := entrega(`{"comprobanteId":"c-x"}`, nil)
	manejarEnvio(context.Background(), logDescartado, cfgTest, uc, &publicadorFalso{}, d)
	if a.nacks != 1 || a.requeue {
		t.Fatalf("nacks=%d requeue=%v; se esperaba DLQ sin reintentos", a.nacks, a.requeue)
	}
}

func TestSiNoSePuedeProgramarElReintentoElMensajeVuelveALaCola(t *testing.T) {
	uc := &envioFalso{err: errTemporal}
	pub := &publicadorFalso{err: errors.New("canal cerrado")}
	d, a := entrega(`{"comprobanteId":"c-1"}`, nil)
	manejarEnvio(context.Background(), logDescartado, cfgTest, uc, pub, d)
	if a.nacks != 1 || !a.requeue || a.acks != 0 {
		t.Fatalf("acks=%d nacks=%d requeue=%v; se esperaba nack con requeue", a.acks, a.nacks, a.requeue)
	}
}

// --- Consumidor de CompraConfirmada ---

const compraValida = `{
	"eventId": "0d8c3d64-5f0e-4d5e-9a57-7a0f2b3c4d5e",
	"compraId": "6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3",
	"email": "ana.perez@example.com",
	"items": [
		{"descripcion": "Pasaje Córdoba - Bariloche", "precio": 1234.5},
		{"descripcion": "Cargo por servicio", "precio": 0.1}
	]
}`

func TestCompraValidaEmiteComprobanteInterno(t *testing.T) {
	uc := &emisionFalsa{}
	d, a := entrega(compraValida, nil)

	manejarCompra(context.Background(), logDescartado, cfgTest, uc, &publicadorFalso{}, d)

	if a.acks != 1 || a.nacks != 0 {
		t.Fatalf("acks=%d nacks=%d; se esperaba ack", a.acks, a.nacks)
	}
	s := uc.solicitudes[0]
	if s.Consumidor != domain.ConsumidorInterno || s.IdempotencyKey != "6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3" {
		t.Errorf("consumidor=%q clave=%q", s.Consumidor, s.IdempotencyKey)
	}
	if s.Email != "ana.perez@example.com" || len(s.Items) != 2 {
		t.Errorf("solicitud = %+v", s)
	}
	// Los precios llegan exactos, sin pasar por float64.
	if !s.Items[0].Precio.Equal(decimal.RequireFromString("1234.5")) || !s.Items[1].Precio.Equal(decimal.RequireFromString("0.1")) {
		t.Errorf("precios = %v, %v", s.Items[0].Precio, s.Items[1].Precio)
	}
}

func TestCompraDuplicadaSeConfirma(t *testing.T) {
	uc := &emisionFalsa{yaExistia: true}
	d, a := entrega(compraValida, nil)
	manejarCompra(context.Background(), logDescartado, cfgTest, uc, &publicadorFalso{}, d)
	if a.acks != 1 || a.nacks != 0 {
		t.Fatalf("acks=%d nacks=%d; un duplicado se confirma igual", a.acks, a.nacks)
	}
}

func TestCompraInvalidaVaALaDLQSinReintentos(t *testing.T) {
	casos := map[string]string{
		"json roto":           `{"compraId":`,
		"sin compraId":        `{"email":"a@b.example","items":[{"descripcion":"x","precio":1}]}`,
		"compraId no es uuid": `{"compraId":"123","email":"a@b.example","items":[{"descripcion":"x","precio":1}]}`,
		"email inválido":      `{"compraId":"6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3","email":"nada","items":[{"descripcion":"x","precio":1}]}`,
		"sin precio":          `{"compraId":"6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3","email":"a@b.example","items":[{"descripcion":"x"}]}`,
		"sin descripción":     `{"compraId":"6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3","email":"a@b.example","items":[{"descripcion":"","precio":1}]}`,
		"precio no numérico":  `{"compraId":"6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3","email":"a@b.example","items":[{"descripcion":"x","precio":"abc"}]}`,
	}
	for nombre, cuerpo := range casos {
		uc := &emisionFalsa{}
		pub := &publicadorFalso{}
		d, a := entrega(cuerpo, nil)
		manejarCompra(context.Background(), logDescartado, cfgTest, uc, pub, d)
		if a.nacks != 1 || a.requeue || len(pub.colas) != 0 {
			t.Errorf("%s: nacks=%d requeue=%v reintentos=%d; se esperaba DLQ directa", nombre, a.nacks, a.requeue, len(pub.colas))
		}
		if uc.llamadas() != 0 {
			t.Errorf("%s: no debía llamar al caso de uso", nombre)
		}
	}
}

func TestCompraQueIncumpleReglasVaALaDLQSinReintentos(t *testing.T) {
	for _, err := range []error{domain.ErrTotalCero, domain.ErrSinItems, domain.ErrPrecioNegativo, domain.ErrImporteFueraDeRango, application.ErrConflictoIdempotencia} {
		pub := &publicadorFalso{}
		d, a := entrega(compraValida, nil)
		manejarCompra(context.Background(), logDescartado, cfgTest, &emisionFalsa{err: err}, pub, d)
		if a.nacks != 1 || a.requeue || len(pub.colas) != 0 {
			t.Errorf("%v: nacks=%d requeue=%v reintentos=%d; se esperaba DLQ directa", err, a.nacks, a.requeue, len(pub.colas))
		}
	}
}

func TestCompraConErrorTemporalSeReintentaYDespuesVaALaDLQ(t *testing.T) {
	uc := &emisionFalsa{err: errTemporal}

	pub := &publicadorFalso{}
	d, a := entrega(compraValida, nil)
	manejarCompra(context.Background(), logDescartado, cfgTest, uc, pub, d)
	if a.acks != 1 || len(pub.colas) != 1 || pub.colas[0] != colaReintento(ColaCompras, cfgTest.EsperasReintento[0]) {
		t.Fatalf("primer fallo: acks=%d publicado en %v", a.acks, pub.colas)
	}

	pub = &publicadorFalso{}
	d, a = entrega(compraValida, amqp.Table{HeaderIntentos: int32(MaxIntentos)})
	manejarCompra(context.Background(), logDescartado, cfgTest, uc, pub, d)
	if a.nacks != 1 || a.requeue || len(pub.colas) != 0 {
		t.Fatalf("agotado: nacks=%d requeue=%v reintentos=%d; se esperaba DLQ", a.nacks, a.requeue, len(pub.colas))
	}
}

// --- Publicador de outbox ---

func TestOutboxPublicaYMarcaDespuesDeLaConfirmacion(t *testing.T) {
	ob := &outboxFalso{eventos: []ports.EventoOutbox{
		{ID: 7, Tipo: ports.EventoEnvioSolicitado, Payload: []byte(`{"comprobanteId":"c-1"}`), Headers: map[string]any{observabilidad.HeaderCorrelacion: "corr-1"}},
	}}
	pub := &publicadorFalso{}
	p := &publicadorOutbox{outbox: ob, log: logDescartado}

	if err := p.publicarPendientes(context.Background(), pub); err != nil {
		t.Fatal(err)
	}
	if len(pub.colas) != 1 || pub.colas[0] != ColaEnvios {
		t.Fatalf("publicado en %v; se esperaba %s", pub.colas, ColaEnvios)
	}
	msg := pub.mensajes[0]
	if msg.DeliveryMode != amqp.Persistent || msg.Headers[observabilidad.HeaderCorrelacion] != "corr-1" || string(msg.Body) != `{"comprobanteId":"c-1"}` {
		t.Errorf("mensaje = %+v", msg)
	}
	if len(ob.publicados) != 1 || ob.publicados[0] != 7 {
		t.Errorf("marcados = %v; se esperaba 7", ob.publicados)
	}
}

func TestOutboxNoMarcaSiElBrokerNoConfirma(t *testing.T) {
	ob := &outboxFalso{eventos: []ports.EventoOutbox{{ID: 1, Tipo: ports.EventoEnvioSolicitado, Payload: []byte(`{}`)}}}
	p := &publicadorOutbox{outbox: ob, log: logDescartado}
	if err := p.publicarPendientes(context.Background(), &publicadorFalso{err: errors.New("nack")}); err == nil {
		t.Fatal("se esperaba error para reconectar")
	}
	if len(ob.publicados) != 0 {
		t.Errorf("no debía marcar publicado un evento sin confirmar")
	}
}

func TestOutboxConLaBaseCaidaNoCortaElPublicador(t *testing.T) {
	ob := &outboxFalso{errLeer: errors.New("base caída")}
	p := &publicadorOutbox{outbox: ob, log: logDescartado}
	if err := p.publicarPendientes(context.Background(), &publicadorFalso{}); err != nil {
		t.Fatalf("un error de la base no debía cortar la conexión con RabbitMQ: %v", err)
	}
}

func TestOutboxDejaPendienteUnTipoDesconocido(t *testing.T) {
	ob := &outboxFalso{eventos: []ports.EventoOutbox{
		{ID: 1, Tipo: "Desconocido", Payload: []byte(`{}`)},
		{ID: 2, Tipo: ports.EventoEnvioSolicitado, Payload: []byte(`{}`)},
	}}
	pub := &publicadorFalso{}
	p := &publicadorOutbox{outbox: ob, log: logDescartado}
	if err := p.publicarPendientes(context.Background(), pub); err != nil {
		t.Fatal(err)
	}
	if len(ob.publicados) != 1 || ob.publicados[0] != 2 {
		t.Errorf("marcados = %v; se esperaba solo el 2", ob.publicados)
	}
}

func TestTablaHeadersConvierteValoresNoSoportados(t *testing.T) {
	tabla := tablaHeaders(map[string]any{
		"texto":   "a",
		"numero":  float64(3),
		"anidado": map[string]any{"x": 1},
	})
	if err := tabla.Validate(); err != nil {
		t.Fatalf("la tabla no es válida para AMQP: %v", err)
	}
	if tabla["texto"] != "a" || tabla["numero"] != float64(3) {
		t.Errorf("tabla = %v", tabla)
	}
}

func TestIntentosLeeDistintosTiposEnteros(t *testing.T) {
	for _, v := range []any{int32(2), int64(2), int(2), int16(2), int8(2), uint8(2)} {
		if got := intentos(amqp.Table{HeaderIntentos: v}); got != 2 {
			t.Errorf("intentos(%T) = %d", v, got)
		}
	}
	if intentos(nil) != 0 {
		t.Error("sin header debía ser 0")
	}
}
