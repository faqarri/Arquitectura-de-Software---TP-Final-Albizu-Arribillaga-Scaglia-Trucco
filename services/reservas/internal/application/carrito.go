// Package application contiene los casos de uso de Reservas. Depende solo de
// domain y de ports (ADR-002).
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// MaxCantidad es el máximo de cupos de una oferta por carrito (RN-12). Lo
// controla Catálogo; acá se valida antes para no hacer una llamada inútil.
const MaxCantidad = 6

// ErrSolicitudInvalida indica un dato mal formado de la solicitud.
type ErrSolicitudInvalida struct {
	Campo   string
	Mensaje string
}

func (e *ErrSolicitudInvalida) Error() string { return e.Campo + ": " + e.Mensaje }

// SolicitudAgregar agrega una oferta suelta (OfertaID) o un pack (PackID + Opciones).
type SolicitudAgregar struct {
	UsuarioID      string
	IdempotencyKey string
	OfertaID       string
	Cantidad       int
	PackID         string
	Opciones       []string
}

// Carritos agrupa los casos de uso del carrito (HU-02, HU-03, HU-04).
type Carritos struct {
	repo     ports.RepositorioCarritos
	catalogo ports.RetencionesCatalogo
	ahora    func() time.Time
}

func NuevoCarritos(repo ports.RepositorioCarritos, catalogo ports.RetencionesCatalogo) *Carritos {
	return &Carritos{repo: repo, catalogo: catalogo, ahora: time.Now}
}

// Agregar retiene el cupo en Catálogo y guarda el ítem. Con la misma
// IdempotencyKey devuelve el ítem ya agregado (existia = true), sin retener otra vez.
func (uc *Carritos) Agregar(ctx context.Context, s SolicitudAgregar) (item domain.ItemCarrito, existia bool, err error) {
	if err := validarAgregar(&s); err != nil {
		return domain.ItemCarrito{}, false, err
	}
	if s.IdempotencyKey == "" {
		s.IdempotencyKey = uuid.NewString()
	} else if previo, err := uc.repo.BuscarPorClave(ctx, s.UsuarioID, s.IdempotencyKey); err == nil {
		return previo, true, nil
	} else if !errors.Is(err, ports.ErrNoEncontrado) {
		return domain.ItemCarrito{}, false, err
	}

	grupo, err := uc.catalogo.Retener(ctx, ports.SolicitudRetencion{
		IdempotencyKey: s.IdempotencyKey,
		TitularID:      s.UsuarioID,
		OfertaID:       s.OfertaID,
		Cantidad:       s.Cantidad,
		PackID:         s.PackID,
		Opciones:       s.Opciones,
	})
	if err != nil {
		return domain.ItemCarrito{}, false, err
	}
	if len(grupo.Retenciones) == 0 {
		return domain.ItemCarrito{}, false, domain.ErrSinRetenciones
	}
	item = itemDesde(s, grupo, uc.ahora())

	switch err := uc.repo.Agregar(ctx, item); {
	case err == nil:
		return item, false, nil
	case errors.Is(err, ports.ErrClaveDuplicada):
		// Dos solicitudes con la misma clave en paralelo: Catálogo devolvió las
		// mismas retenciones a las dos y la otra ya guardó el ítem.
		previo, err := uc.repo.BuscarPorClave(ctx, s.UsuarioID, s.IdempotencyKey)
		return previo, true, err
	default:
		// No se pudo guardar: se intenta liberar el cupo. Si tampoco se puede,
		// la retención vence sola (ADR-014).
		for _, r := range item.Retenciones {
			_ = uc.catalogo.Liberar(context.WithoutCancel(ctx), r)
		}
		return domain.ItemCarrito{}, false, fmt.Errorf("guardar ítem del carrito: %w", err)
	}
}

func validarAgregar(s *SolicitudAgregar) error {
	esOferta, esPack := s.OfertaID != "", s.PackID != ""
	switch {
	case esOferta == esPack:
		return &ErrSolicitudInvalida{"ofertaId", "indicá ofertaId o packId (uno de los dos)"}
	case esPack && len(s.Opciones) == 0:
		return &ErrSolicitudInvalida{"opciones", "un pack necesita las opciones elegidas"}
	}
	if s.Cantidad == 0 {
		s.Cantidad = 1
	}
	if s.Cantidad < 1 || s.Cantidad > MaxCantidad {
		return &ErrSolicitudInvalida{"cantidad", fmt.Sprintf("debe estar entre 1 y %d", MaxCantidad)}
	}
	return nil
}

func itemDesde(s SolicitudAgregar, g ports.GrupoRetenido, ahora time.Time) domain.ItemCarrito {
	descripciones := make([]string, len(g.Retenciones))
	ids := make([]string, len(g.Retenciones))
	precio := decimal.Zero
	for i, r := range g.Retenciones {
		descripciones[i] = r.Descripcion
		ids[i] = r.ID
		precio = precio.Add(r.PrecioReferencia)
	}
	return domain.ItemCarrito{
		ID:               uuid.NewString(),
		UsuarioID:        s.UsuarioID,
		IdempotencyKey:   s.IdempotencyKey,
		OfertaID:         s.OfertaID,
		PackID:           s.PackID,
		Cantidad:         s.Cantidad,
		Descripcion:      strings.Join(descripciones, " + "),
		Destino:          g.Retenciones[0].Destino,
		PrecioReferencia: precio,
		Retenciones:      ids,
		ExpiraEn:         g.ExpiraEn.UTC(),
		Estado:           domain.ItemActivo,
		CreadoEn:         ahora.UTC(),
	}
}

// Quitar saca el ítem del carrito y libera su cupo. Si Catálogo no responde,
// el ítem se quita igual: la retención vence sola (README de Reservas).
// liberado indica si Catálogo confirmó la liberación.
func (uc *Carritos) Quitar(ctx context.Context, usuarioID, itemID string) (liberado bool, err error) {
	carrito, err := uc.repo.Obtener(ctx, usuarioID)
	if err != nil {
		return false, err
	}
	var item *domain.ItemCarrito
	for i := range carrito.Items {
		if carrito.Items[i].ID == itemID {
			item = &carrito.Items[i]
		}
	}
	if item == nil {
		return false, domain.ErrItemNoEncontrado
	}
	liberado = true
	for _, r := range item.Retenciones {
		if err := uc.catalogo.Liberar(ctx, r); err != nil {
			liberado = false
		}
	}
	return liberado, uc.repo.Quitar(ctx, usuarioID, itemID)
}

// Ver devuelve el carrito con el estado visible de cada ítem.
func (uc *Carritos) Ver(ctx context.Context, usuarioID string) (domain.Carrito, error) {
	c, err := uc.repo.Obtener(ctx, usuarioID)
	if err != nil {
		return domain.Carrito{}, err
	}
	ahora := uc.ahora()
	for i := range c.Items {
		c.Items[i].Estado = c.Items[i].EstadoVisible(ahora)
	}
	return c, nil
}

// MarcarRetencionVencida procesa el evento RetencionVencida de Catálogo. Es
// idempotente: un evento repetido o de un ítem que ya no está no hace nada.
func (uc *Carritos) MarcarRetencionVencida(ctx context.Context, retencionID string) error {
	return uc.repo.MarcarVencida(ctx, retencionID)
}
