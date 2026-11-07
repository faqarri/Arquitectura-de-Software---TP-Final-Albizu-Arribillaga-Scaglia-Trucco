// Package ports define las interfaces que los casos de uso necesitan del mundo
// exterior. Las implementan los paquetes de internal/adapters (ADR-002).
package ports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"agencia/reservas/internal/domain"
)

var (
	ErrNoEncontrado   = errors.New("no encontrado")
	ErrClaveDuplicada = errors.New("ya existe un registro con esa clave de idempotencia")
)

// ---------- Catálogo (API interna de retenciones, ADR-014) ----------

// SolicitudRetencion: una oferta suelta (OfertaID) o un pack (PackID + Opciones).
type SolicitudRetencion struct {
	IdempotencyKey string
	TitularID      string
	OfertaID       string
	Cantidad       int
	PackID         string
	Opciones       []string
}

// Retencion es una retención creada por Catálogo.
type Retencion struct {
	ID               string
	OfertaID         string
	Cantidad         int
	Descripcion      string
	Destino          string
	PrecioReferencia decimal.Decimal
}

// GrupoRetenido son las retenciones de un ítem del carrito.
type GrupoRetenido struct {
	ExpiraEn    time.Time
	Retenciones []Retencion
}

// RetencionesCatalogo es la API interna de Catálogo. Las tres operaciones son
// idempotentes; el adaptador reintenta con timeout y backoff (ADR-005).
type RetencionesCatalogo interface {
	Retener(ctx context.Context, s SolicitudRetencion) (GrupoRetenido, error)
	Liberar(ctx context.Context, retencionID string) error
	// Confirmar devuelve las líneas con el precio cobrado, o *ErrRetencionesNoVigentes.
	Confirmar(ctx context.Context, compraID, titularID string, retenciones []string) ([]domain.LineaCompra, error)
}

// ErrCatalogoNoDisponible: Catálogo no dio una respuesta concluyente (timeout o
// 5xx después de los reintentos). Al confirmar, la compra queda PENDIENTE.
var ErrCatalogoNoDisponible = errors.New("catálogo no disponible")

// ErrSinCupo: la oferta está AGOTADA o RETENIDA por otros carritos (409).
type ErrSinCupo struct {
	Estado            string // AGOTADA o RETENIDA
	OfertaID          string
	ProximaLiberacion *time.Time
}

func (e *ErrSinCupo) Error() string { return fmt.Sprintf("sin cupo: %s", e.Estado) }

// ErrReglaCatalogo: Catálogo rechazó la retención por una regla (422 o 404).
type ErrReglaCatalogo struct {
	Codigo  string
	Detalle string
}

func (e *ErrReglaCatalogo) Error() string { return e.Codigo + ": " + e.Detalle }

// ErrRetencionesNoVigentes: al confirmar, alguna retención ya no estaba vigente (409).
type ErrRetencionesNoVigentes struct {
	Retenciones []string
}

func (e *ErrRetencionesNoVigentes) Error() string {
	return fmt.Sprintf("retenciones no vigentes: %v", e.Retenciones)
}

// ---------- Persistencia ----------

// RepositorioCarritos guarda los ítems del carrito.
type RepositorioCarritos interface {
	Obtener(ctx context.Context, usuarioID string) (domain.Carrito, error)
	// Agregar guarda el ítem. ErrClaveDuplicada si ya existe uno del usuario con esa IdempotencyKey.
	Agregar(ctx context.Context, item domain.ItemCarrito) error
	BuscarPorClave(ctx context.Context, usuarioID, clave string) (domain.ItemCarrito, error)
	Quitar(ctx context.Context, usuarioID, itemID string) error
	// MarcarVencida marca VENCIDO el ítem que contiene la retención. Si no hay
	// ninguno (ya se quitó o se compró), no hace nada: es idempotente.
	MarcarVencida(ctx context.Context, retencionID string) error
}

// RepositorioCompras guarda las compras.
type RepositorioCompras interface {
	// Crear guarda una compra PENDIENTE. ErrClaveDuplicada si el usuario ya
	// tiene una con esa IdempotencyKey.
	Crear(ctx context.Context, c *domain.Compra) error
	BuscarPorClave(ctx context.Context, usuarioID, clave string) (*domain.Compra, error)
	Obtener(ctx context.Context, usuarioID, compraID string) (*domain.Compra, error)
	Listar(ctx context.Context, usuarioID string) ([]domain.Compra, error)
	// Resolver guarda el estado final de una compra que estaba PENDIENTE, en una
	// sola transacción: si quedó CONFIRMADA, inserta CompraConfirmada en el outbox
	// y quita del carrito los ítems comprados; si quedó RECHAZADA, marca vencidos
	// los ítems de las retenciones no vigentes. Devuelve false si la compra ya no
	// estaba PENDIENTE (otro proceso la resolvió antes): en ese caso no hace nada.
	Resolver(ctx context.Context, c *domain.Compra) (bool, error)
	// Pendientes devuelve compras PENDIENTE creadas antes de antesDe, para el proceso de recuperación.
	Pendientes(ctx context.Context, antesDe time.Time, limite int) ([]domain.Compra, error)
}

// RepositorioGuias guarda las guías compartidas y su PDF hasta que Estudiario las acepta.
type RepositorioGuias interface {
	Crear(ctx context.Context, g *domain.Guia, pdf []byte) error
	Actualizar(ctx context.Context, g *domain.Guia) error
	// PorEnviar devuelve guías POR_ENVIAR con su PDF.
	PorEnviar(ctx context.Context, limite int) ([]GuiaConArchivo, error)
	// PendientesDeModeracion devuelve las guías PENDIENTE (ya registradas en Estudiario).
	PendientesDeModeracion(ctx context.Context) ([]domain.Guia, error)
	Listar(ctx context.Context, usuarioID string) ([]domain.Guia, error)
}

// GuiaConArchivo es una guía con el PDF que todavía hay que enviar.
type GuiaConArchivo struct {
	Guia domain.Guia
	PDF  []byte
}

// ---------- Guías externas (Estudiario, ADR-009) ----------

// EstadoModeracion de una guía en el sistema externo.
type EstadoModeracion struct {
	DocumentoID string
	Publicada   bool
	Rechazada   bool
	Motivo      string
}

// GuiasExternas es el servicio que publica y modera las guías.
type GuiasExternas interface {
	// Registrar devuelve el id externo. ErrGuiasNoDisponible si se puede
	// reintentar más tarde; *ErrGuiaRechazada si los datos no son aceptados.
	Registrar(ctx context.Context, g domain.Guia, pdf []byte) (string, error)
	// Moderacion devuelve el estado de todas las guías que registramos.
	Moderacion(ctx context.Context) ([]EstadoModeracion, error)
}

var ErrGuiasNoDisponible = errors.New("el servicio de guías no está disponible")

// ErrGuiaRechazada: el servicio externo rechazó los datos de la guía.
type ErrGuiaRechazada struct {
	Motivo string
}

func (e *ErrGuiaRechazada) Error() string { return "guía rechazada: " + e.Motivo }
