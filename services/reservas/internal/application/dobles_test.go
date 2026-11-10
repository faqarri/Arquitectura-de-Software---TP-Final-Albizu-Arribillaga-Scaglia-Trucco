package application

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// memoria implementa los tres repositorios, con la semántica que documentan los puertos.
type memoria struct {
	mu       sync.Mutex
	items    []domain.ItemCarrito
	compras  []*domain.Compra
	outbox   []string // ids de compras con CompraConfirmada en el outbox
	guias    []*domain.Guia
	archivos map[string][]byte
	fallar   error
}

func nuevaMemoria() *memoria { return &memoria{archivos: map[string][]byte{}} }

func (m *memoria) Obtener(_ context.Context, usuarioID string) (domain.Carrito, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := domain.Carrito{UsuarioID: usuarioID}
	for _, it := range m.items {
		if it.UsuarioID == usuarioID {
			c.Items = append(c.Items, it)
		}
	}
	return c, nil
}

func (m *memoria) Agregar(_ context.Context, it domain.ItemCarrito) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fallar != nil {
		return m.fallar
	}
	for _, e := range m.items {
		if e.UsuarioID == it.UsuarioID && e.IdempotencyKey == it.IdempotencyKey {
			return ports.ErrClaveDuplicada
		}
	}
	m.items = append(m.items, it)
	return nil
}

func (m *memoria) BuscarPorClave(_ context.Context, usuarioID, clave string) (domain.ItemCarrito, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.items {
		if e.UsuarioID == usuarioID && e.IdempotencyKey == clave {
			return e, nil
		}
	}
	return domain.ItemCarrito{}, ports.ErrNoEncontrado
}

func (m *memoria) Quitar(_ context.Context, usuarioID, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = slices.DeleteFunc(m.items, func(it domain.ItemCarrito) bool { return it.ID == itemID && it.UsuarioID == usuarioID })
	return nil
}

func (m *memoria) MarcarVencida(_ context.Context, retencionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.items {
		if slices.Contains(m.items[i].Retenciones, retencionID) {
			m.items[i].Estado = domain.ItemVencido
		}
	}
	return nil
}

// comprasMem es la vista de memoria como RepositorioCompras (BuscarPorClave choca de nombre).
type comprasMem struct{ *memoria }

func (r comprasMem) Crear(_ context.Context, c *domain.Compra) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.compras {
		if e.UsuarioID == c.UsuarioID && e.IdempotencyKey == c.IdempotencyKey {
			return ports.ErrClaveDuplicada
		}
	}
	copia := *c
	r.compras = append(r.compras, &copia)
	return nil
}

func (r comprasMem) BuscarPorClave(_ context.Context, usuarioID, clave string) (*domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.compras {
		if e.UsuarioID == usuarioID && e.IdempotencyKey == clave {
			copia := *e
			return &copia, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}

func (r comprasMem) Obtener(_ context.Context, usuarioID, id string) (*domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.compras {
		if e.UsuarioID == usuarioID && e.ID == id {
			copia := *e
			return &copia, nil
		}
	}
	return nil, ports.ErrNoEncontrado
}

func (r comprasMem) Listar(_ context.Context, usuarioID string) ([]domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Compra
	for _, e := range r.compras {
		if e.UsuarioID == usuarioID {
			res = append(res, *e)
		}
	}
	return res, nil
}

func (r comprasMem) Resolver(_ context.Context, c *domain.Compra) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.compras {
		if e.ID != c.ID {
			continue
		}
		if e.Estado != domain.CompraPendiente {
			return false, nil
		}
		*e = *c
		switch c.Estado {
		case domain.CompraConfirmada:
			r.outbox = append(r.outbox, c.ID)
			r.items = slices.DeleteFunc(r.items, func(it domain.ItemCarrito) bool {
				return it.UsuarioID == c.UsuarioID && slices.Contains(c.Retenciones, it.Retenciones[0])
			})
		case domain.CompraRechazada:
			for i := range r.items {
				for _, v := range c.RetencionesVencidas {
					if slices.Contains(r.items[i].Retenciones, v) {
						r.items[i].Estado = domain.ItemVencido
					}
				}
			}
		}
		return true, nil
	}
	return false, ports.ErrNoEncontrado
}

func (r comprasMem) Pendientes(_ context.Context, antesDe time.Time, limite int) ([]domain.Compra, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Compra
	for _, e := range r.compras {
		if e.Estado == domain.CompraPendiente && e.CreadaEn.Before(antesDe) && len(res) < limite {
			res = append(res, *e)
		}
	}
	return res, nil
}

type guiasMem struct{ *memoria }

func (r guiasMem) Crear(_ context.Context, g *domain.Guia, pdf []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copia := *g
	r.guias = append(r.guias, &copia)
	r.archivos[g.ID] = pdf
	return nil
}

func (r guiasMem) Actualizar(_ context.Context, g *domain.Guia) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.guias {
		if e.ID == g.ID {
			*e = *g
			if g.Estado != domain.GuiaPorEnviar {
				delete(r.archivos, g.ID)
			}
		}
	}
	return nil
}

func (r guiasMem) PorEnviar(_ context.Context, limite int) ([]ports.GuiaConArchivo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []ports.GuiaConArchivo
	for _, g := range r.guias {
		if g.Estado == domain.GuiaPorEnviar && len(res) < limite {
			res = append(res, ports.GuiaConArchivo{Guia: *g, PDF: r.archivos[g.ID]})
		}
	}
	return res, nil
}

func (r guiasMem) PendientesDeModeracion(context.Context) ([]domain.Guia, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Guia
	for _, g := range r.guias {
		if g.Estado == domain.GuiaPendiente {
			res = append(res, *g)
		}
	}
	return res, nil
}

func (r guiasMem) Listar(_ context.Context, usuarioID string) ([]domain.Guia, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []domain.Guia
	for _, g := range r.guias {
		if g.UsuarioID == usuarioID {
			res = append(res, *g)
		}
	}
	return res, nil
}

// catalogoFalso simula la API interna de retenciones.
type catalogoFalso struct {
	mu          sync.Mutex
	errRetener  error
	errConfirm  error
	vencidas    []string          // si no está vacío, Confirmar responde 409 con estas
	retenidas   map[string]string // idempotency key → retención (para reintentos)
	liberadas   []string
	confirmadas []string // compraIds
}

func nuevoCatalogo() *catalogoFalso { return &catalogoFalso{retenidas: map[string]string{}} }

func (c *catalogoFalso) Retener(_ context.Context, s ports.SolicitudRetencion) (ports.GrupoRetenido, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errRetener != nil {
		return ports.GrupoRetenido{}, c.errRetener
	}
	id, ok := c.retenidas[s.IdempotencyKey]
	if !ok {
		id = uuid.NewString()
		c.retenidas[s.IdempotencyKey] = id
	}
	return ports.GrupoRetenido{
		ExpiraEn: time.Now().Add(15 * time.Minute),
		Retenciones: []ports.Retencion{{
			ID: id, OfertaID: s.OfertaID, Cantidad: s.Cantidad, Descripcion: "Vuelo a Bariloche",
			Destino: "Bariloche", PrecioReferencia: decimal.NewFromInt(1000).Mul(decimal.NewFromInt(int64(s.Cantidad))),
		}},
	}, nil
}

func (c *catalogoFalso) Liberar(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errRetener != nil {
		return c.errRetener
	}
	c.liberadas = append(c.liberadas, id)
	return nil
}

func (c *catalogoFalso) Confirmar(_ context.Context, compraID, _ string, retenciones []string) ([]domain.LineaCompra, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errConfirm != nil {
		return nil, c.errConfirm
	}
	if len(c.vencidas) > 0 {
		return nil, &ports.ErrRetencionesNoVigentes{Retenciones: c.vencidas}
	}
	c.confirmadas = append(c.confirmadas, compraID)
	lineas := make([]domain.LineaCompra, len(retenciones))
	for i, r := range retenciones {
		lineas[i] = domain.LineaCompra{RetencionID: r, Descripcion: "Vuelo a Bariloche", Destino: "Bariloche", Precio: decimal.NewFromInt(1000)}
	}
	return lineas, nil
}

// externasFalsas simula Estudiario.
type externasFalsas struct {
	mu         sync.Mutex
	err        error
	colgado    bool     // simula un Estudiario que no responde: espera a que venza el contexto
	registros  []string // idempotency keys recibidas
	moderacion []ports.EstadoModeracion
}

func (e *externasFalsas) Registrar(ctx context.Context, g domain.Guia, _ []byte) (string, error) {
	e.mu.Lock()
	colgado := e.colgado
	e.mu.Unlock()
	if colgado {
		<-ctx.Done()
		return "", ports.ErrGuiasNoDisponible
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return "", e.err
	}
	e.registros = append(e.registros, g.IdempotencyKey)
	return "doc" + g.IdempotencyKey[:8], nil
}

func (e *externasFalsas) Moderacion(context.Context) ([]ports.EstadoModeracion, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.moderacion, e.err
}
