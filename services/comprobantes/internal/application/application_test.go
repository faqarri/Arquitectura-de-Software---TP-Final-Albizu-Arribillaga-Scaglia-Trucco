package application

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// repoEnMemoria es un doble de RepositorioComprobantes para los tests.
type repoEnMemoria struct {
	porID       map[string]*domain.Comprobante
	actualizado int
}

func nuevoRepo() *repoEnMemoria {
	return &repoEnMemoria{porID: map[string]*domain.Comprobante{}}
}

func (r *repoEnMemoria) Guardar(_ context.Context, c *domain.Comprobante) error {
	for _, e := range r.porID {
		if e.Consumidor == c.Consumidor && e.IdempotencyKey == c.IdempotencyKey {
			return ports.ErrClaveDuplicada
		}
	}
	c.Nro = int64(len(r.porID) + 1)
	r.porID[c.ID] = c
	return nil
}

func (r *repoEnMemoria) BuscarPorClave(_ context.Context, consumidor, clave string) (*domain.Comprobante, error) {
	for _, e := range r.porID {
		if e.Consumidor == consumidor && e.IdempotencyKey == clave {
			return e, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}

func (r *repoEnMemoria) BuscarPorID(_ context.Context, consumidor, id string) (*domain.Comprobante, error) {
	c, ok := r.porID[id]
	if !ok || c.Consumidor != consumidor {
		return nil, ports.ErrNoEncontrado
	}
	return c, nil
}

func (r *repoEnMemoria) Obtener(_ context.Context, id string) (*domain.Comprobante, error) {
	c, ok := r.porID[id]
	if !ok {
		return nil, ports.ErrNoEncontrado
	}
	return c, nil
}

func (r *repoEnMemoria) ActualizarEnvio(context.Context, *domain.Comprobante) error {
	r.actualizado++
	return nil
}

type pdfFalso struct{}

func (pdfFalso) Generar(context.Context, *domain.Comprobante) ([]byte, error) {
	return []byte("%PDF"), nil
}

type mailerFalso struct {
	enviados int
	err      error
}

func (m *mailerFalso) Enviar(context.Context, ports.Correo) error {
	if m.err != nil {
		return m.err
	}
	m.enviados++
	return nil
}

func solicitud(clave, email, precio string) SolicitudEmision {
	return SolicitudEmision{
		Consumidor:     "grupo-1",
		IdempotencyKey: clave,
		Email:          email,
		Items:          []domain.Item{{Descripcion: "Entrada", Precio: decimal.RequireFromString(precio)}},
	}
}

func TestEmitirReintentoConMismoCuerpoDevuelveElExistente(t *testing.T) {
	uc := NuevoEmitirComprobante(nuevoRepo())
	ctx := context.Background()

	primero, err := uc.Ejecutar(ctx, solicitud("k1", "a@b.com", "100"))
	if err != nil || primero.YaExistia {
		t.Fatalf("primera emisión: resultado %+v, error %v", primero, err)
	}
	segundo, err := uc.Ejecutar(ctx, solicitud("k1", "a@b.com", "100.00"))
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if !segundo.YaExistia || segundo.Comprobante.ID != primero.Comprobante.ID {
		t.Errorf("el reintento debería devolver el mismo comprobante (YaExistia=%v)", segundo.YaExistia)
	}
}

func TestEmitirMismaClaveConOtroCuerpoEsConflicto(t *testing.T) {
	uc := NuevoEmitirComprobante(nuevoRepo())
	ctx := context.Background()

	if _, err := uc.Ejecutar(ctx, solicitud("k1", "a@b.com", "100")); err != nil {
		t.Fatal(err)
	}
	_, err := uc.Ejecutar(ctx, solicitud("k1", "a@b.com", "200"))
	if !errors.Is(err, ErrConflictoIdempotencia) {
		t.Errorf("error %v, se esperaba ErrConflictoIdempotencia", err)
	}
}

func TestEmitirMismaClaveDeOtroConsumidorNoChoca(t *testing.T) {
	uc := NuevoEmitirComprobante(nuevoRepo())
	ctx := context.Background()

	a := solicitud("k1", "a@b.com", "100")
	b := solicitud("k1", "a@b.com", "100")
	b.Consumidor = domain.ConsumidorInterno

	ra, errA := uc.Ejecutar(ctx, a)
	rb, errB := uc.Ejecutar(ctx, b)
	if errA != nil || errB != nil {
		t.Fatalf("errores: %v, %v", errA, errB)
	}
	if rb.YaExistia || ra.Comprobante.ID == rb.Comprobante.ID {
		t.Error("dos consumidores con la misma clave deberían tener comprobantes distintos")
	}
}

func TestEnviarNoReenviaUnComprobanteYaEnviado(t *testing.T) {
	repo := nuevoRepo()
	mailer := &mailerFalso{}
	emitir := NuevoEmitirComprobante(repo)
	enviar := NuevoEnviarComprobante(repo, pdfFalso{}, mailer)
	ctx := context.Background()

	r, err := emitir.Ejecutar(ctx, solicitud("k1", "a@b.com", "100"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // la segunda simula una entrega repetida del mensaje
		if err := enviar.Ejecutar(ctx, r.Comprobante.ID); err != nil {
			t.Fatal(err)
		}
	}
	if mailer.enviados != 1 {
		t.Errorf("se enviaron %d correos, se esperaba 1", mailer.enviados)
	}
	if r.Comprobante.EstadoEnvio != domain.EnvioEnviado {
		t.Errorf("estado %s, se esperaba ENVIADO", r.Comprobante.EstadoEnvio)
	}
}

func TestEnviarConErrorDejaElComprobantePendiente(t *testing.T) {
	repo := nuevoRepo()
	enviar := NuevoEnviarComprobante(repo, pdfFalso{}, &mailerFalso{err: errors.New("smtp caído")})
	ctx := context.Background()

	r, _ := NuevoEmitirComprobante(repo).Ejecutar(ctx, solicitud("k1", "a@b.com", "100"))
	if err := enviar.Ejecutar(ctx, r.Comprobante.ID); err == nil {
		t.Fatal("se esperaba un error para que el worker reintente")
	}
	if r.Comprobante.EstadoEnvio != domain.EnvioPendiente {
		t.Errorf("estado %s, se esperaba PENDIENTE", r.Comprobante.EstadoEnvio)
	}

	if err := enviar.MarcarFallido(ctx, r.Comprobante.ID); err != nil {
		t.Fatal(err)
	}
	if r.Comprobante.EstadoEnvio != domain.EnvioFallido {
		t.Errorf("estado %s, se esperaba FALLIDO", r.Comprobante.EstadoEnvio)
	}
}
