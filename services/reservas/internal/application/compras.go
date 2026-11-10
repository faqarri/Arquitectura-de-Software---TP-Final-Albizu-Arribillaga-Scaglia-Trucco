package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// EsperaRecuperacion es cuánto tiempo tiene que tener una compra PENDIENTE para
// que la tome el proceso de recuperación (así no compite con la solicitud que
// la está confirmando en ese momento).
const EsperaRecuperacion = 10 * time.Second

// Compras agrupa los casos de uso de la compra (HU-06, HU-08).
type Compras struct {
	compras  ports.RepositorioCompras
	carritos ports.RepositorioCarritos
	catalogo ports.RetencionesCatalogo
	ahora    func() time.Time
}

func NuevoCompras(compras ports.RepositorioCompras, carritos ports.RepositorioCarritos, catalogo ports.RetencionesCatalogo) *Compras {
	return &Compras{compras: compras, carritos: carritos, catalogo: catalogo, ahora: time.Now}
}

// Comprar confirma el carrito (ADR-014):
//   - se crea la compra PENDIENTE con todas las retenciones del carrito;
//   - Catálogo confirma todo (CONFIRMADA, y CompraConfirmada sale por outbox) o
//     informa qué venció (RECHAZADA, no se vende nada);
//   - si Catálogo no da una respuesta concluyente, queda PENDIENTE y la
//     resuelve el proceso de recuperación con la misma clave (el id de la compra).
//
// La misma IdempotencyKey devuelve la compra ya creada (yaExistia = true): el
// cliente puede reintentar sin generar otra compra.
func (uc *Compras) Comprar(ctx context.Context, usuarioID, email, clave string) (compra *domain.Compra, yaExistia bool, err error) {
	if clave == "" {
		clave = uuid.NewString()
	}
	if previa, err := uc.compras.BuscarPorClave(ctx, usuarioID, clave); err == nil {
		return previa, true, nil
	} else if !errors.Is(err, ports.ErrNoEncontrado) {
		return nil, false, err
	}

	carrito, err := uc.carritos.Obtener(ctx, usuarioID)
	if err != nil {
		return nil, false, err
	}
	compra, err = domain.NuevaCompra(usuarioID, email, clave, carrito, uc.ahora())
	if err != nil {
		return nil, false, err
	}
	if err := uc.compras.Crear(ctx, compra); errors.Is(err, ports.ErrClaveDuplicada) {
		previa, err := uc.compras.BuscarPorClave(ctx, usuarioID, clave)
		return previa, true, err
	} else if err != nil {
		return nil, false, err
	}
	compra, err = uc.resolver(ctx, compra)
	return compra, false, err
}

// resolver pide a Catálogo confirmar las retenciones y guarda el resultado.
// Una falta de respuesta deja la compra PENDIENTE sin devolver error.
func (uc *Compras) resolver(ctx context.Context, c *domain.Compra) (*domain.Compra, error) {
	lineas, err := uc.catalogo.Confirmar(ctx, c.ID, c.UsuarioID, c.Retenciones)
	var noVigentes *ports.ErrRetencionesNoVigentes
	switch {
	case err == nil:
		if err := c.Confirmar(lineas, uc.ahora()); err != nil {
			return c, err
		}
	case errors.As(err, &noVigentes):
		if err := c.Rechazar(noVigentes.Retenciones, uc.ahora()); err != nil {
			return c, err
		}
	default:
		// Sin respuesta concluyente: queda PENDIENTE para el proceso de recuperación.
		return c, nil
	}

	resuelta, err := uc.compras.Resolver(ctx, c)
	if err != nil {
		return c, err
	}
	if !resuelta {
		// Otro proceso la resolvió antes: se devuelve lo que quedó guardado.
		return uc.compras.Obtener(ctx, c.UsuarioID, c.ID)
	}
	return c, nil
}

// RecuperarPendientes reintenta la confirmación de las compras que quedaron
// PENDIENTE. Siempre terminan: a lo sumo cuando vencen sus retenciones,
// Catálogo responde 409 y quedan RECHAZADA (ADR-014).
func (uc *Compras) RecuperarPendientes(ctx context.Context, limite int) (resueltas int, err error) {
	pendientes, err := uc.compras.Pendientes(ctx, uc.ahora().Add(-EsperaRecuperacion), limite)
	if err != nil {
		return 0, err
	}
	for i := range pendientes {
		c, err := uc.resolver(ctx, &pendientes[i])
		if err != nil {
			return resueltas, err
		}
		if c.Estado != domain.CompraPendiente {
			resueltas++
		}
	}
	return resueltas, nil
}

func (uc *Compras) Listar(ctx context.Context, usuarioID string) ([]domain.Compra, error) {
	return uc.compras.Listar(ctx, usuarioID)
}

func (uc *Compras) Obtener(ctx context.Context, usuarioID, compraID string) (*domain.Compra, error) {
	return uc.compras.Obtener(ctx, usuarioID, compraID)
}
