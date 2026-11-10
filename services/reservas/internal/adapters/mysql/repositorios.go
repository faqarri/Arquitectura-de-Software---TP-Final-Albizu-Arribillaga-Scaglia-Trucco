package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agencia/pkg/mensajeria"
	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// TipoCompraConfirmada es el evento que consume Comprobantes (formato en el
// README de Comprobantes, sección Mensajes).
const TipoCompraConfirmada = "CompraConfirmada"

const codigoClaveDuplicada = 1062

func esDuplicada(err error, indice string) bool {
	var e *drivermysql.MySQLError
	return errors.As(err, &e) && e.Number == codigoClaveDuplicada && strings.Contains(e.Message, indice)
}

// ---------- Carritos ----------

type Carritos struct{ db *gorm.DB }

var _ ports.RepositorioCarritos = (*Carritos)(nil)

func NuevoCarritos(db *gorm.DB) *Carritos { return &Carritos{db: db} }

func (r *Carritos) Obtener(ctx context.Context, usuarioID string) (domain.Carrito, error) {
	var ms []itemModelo
	err := r.db.WithContext(ctx).
		Preload("Retenciones", func(db *gorm.DB) *gorm.DB { return db.Order("orden") }).
		Where("usuario_id = ?", usuarioID).Order("creado_en").Find(&ms).Error
	if err != nil {
		return domain.Carrito{}, fmt.Errorf("leer carrito: %w", err)
	}
	c := domain.Carrito{UsuarioID: usuarioID}
	for _, m := range ms {
		c.Items = append(c.Items, itemADominio(m))
	}
	return c, nil
}

func (r *Carritos) Agregar(ctx context.Context, it domain.ItemCarrito) error {
	m := itemAModelo(it)
	err := r.db.WithContext(ctx).Create(&m).Error // GORM crea las retenciones en la misma transacción
	if esDuplicada(err, "ux_item_usuario_clave") {
		return ports.ErrClaveDuplicada
	}
	if err != nil {
		return fmt.Errorf("guardar ítem: %w", err)
	}
	return nil
}

func (r *Carritos) BuscarPorClave(ctx context.Context, usuarioID, clave string) (domain.ItemCarrito, error) {
	var m itemModelo
	err := r.db.WithContext(ctx).
		Preload("Retenciones", func(db *gorm.DB) *gorm.DB { return db.Order("orden") }).
		Where("usuario_id = ? AND idempotency_key = ?", usuarioID, clave).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ItemCarrito{}, ports.ErrNoEncontrado
	}
	if err != nil {
		return domain.ItemCarrito{}, fmt.Errorf("buscar ítem: %w", err)
	}
	return itemADominio(m), nil
}

func (r *Carritos) Quitar(ctx context.Context, usuarioID, itemID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND usuario_id = ?", itemID, usuarioID).Delete(&itemModelo{})
		if res.Error != nil {
			return fmt.Errorf("quitar ítem: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
		return tx.Where("item_id = ?", itemID).Delete(&itemRetencionModelo{}).Error
	})
}

func (r *Carritos) MarcarVencida(ctx context.Context, retencionID string) error {
	err := r.db.WithContext(ctx).Model(&itemModelo{}).
		Where("id IN (?)", r.db.Model(&itemRetencionModelo{}).Select("item_id").Where("retencion_id = ?", retencionID)).
		Update("estado", string(domain.ItemVencido)).Error
	if err != nil {
		return fmt.Errorf("marcar retención vencida: %w", err)
	}
	return nil
}

// ---------- Compras ----------

type Compras struct{ db *gorm.DB }

var _ ports.RepositorioCompras = (*Compras)(nil)

func NuevoCompras(db *gorm.DB) *Compras { return &Compras{db: db} }

func (r *Compras) Crear(ctx context.Context, c *domain.Compra) error {
	m := compraAModelo(c)
	err := r.db.WithContext(ctx).Omit("Lineas").Create(&m).Error
	if esDuplicada(err, "ux_compra_usuario_clave") {
		return ports.ErrClaveDuplicada
	}
	if err != nil {
		return fmt.Errorf("crear compra: %w", err)
	}
	return nil
}

func (r *Compras) buscar(ctx context.Context, condicion string, args ...any) (*domain.Compra, error) {
	var m compraModelo
	err := r.db.WithContext(ctx).
		Preload("Lineas", func(db *gorm.DB) *gorm.DB { return db.Order("orden") }).
		Where(condicion, args...).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("buscar compra: %w", err)
	}
	c := compraADominio(m)
	return &c, nil
}

func (r *Compras) BuscarPorClave(ctx context.Context, usuarioID, clave string) (*domain.Compra, error) {
	return r.buscar(ctx, "usuario_id = ? AND idempotency_key = ?", usuarioID, clave)
}

func (r *Compras) Obtener(ctx context.Context, usuarioID, compraID string) (*domain.Compra, error) {
	return r.buscar(ctx, "id = ? AND usuario_id = ?", compraID, usuarioID)
}

func (r *Compras) Listar(ctx context.Context, usuarioID string) ([]domain.Compra, error) {
	var ms []compraModelo
	err := r.db.WithContext(ctx).
		Preload("Lineas", func(db *gorm.DB) *gorm.DB { return db.Order("orden") }).
		Where("usuario_id = ?", usuarioID).Order("creada_en DESC").Find(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("listar compras: %w", err)
	}
	res := make([]domain.Compra, len(ms))
	for i, m := range ms {
		res[i] = compraADominio(m)
	}
	return res, nil
}

func (r *Compras) Pendientes(ctx context.Context, antesDe time.Time, limite int) ([]domain.Compra, error) {
	var ms []compraModelo
	err := r.db.WithContext(ctx).
		Where("estado = ? AND creada_en < ?", string(domain.CompraPendiente), antesDe).
		Order("creada_en").Limit(limite).Find(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("leer compras pendientes: %w", err)
	}
	res := make([]domain.Compra, len(ms))
	for i, m := range ms {
		res[i] = compraADominio(m)
	}
	return res, nil
}

// itemComprobante y eventoCompraConfirmada son el formato de CompraConfirmada.
// El precio va como número JSON con 2 decimales, nunca como float.
type itemComprobante struct {
	Descripcion string      `json:"descripcion"`
	Precio      json.Number `json:"precio"`
}

type eventoCompraConfirmada struct {
	EventID  string            `json:"eventId"`
	CompraID string            `json:"compraId"`
	Email    string            `json:"email"`
	Items    []itemComprobante `json:"items"`
}

// Resolver guarda el estado final en una sola transacción (ver ports.RepositorioCompras).
// La actualización condicionada a estado = PENDIENTE hace que, si la solicitud y
// el proceso de recuperación resuelven la misma compra a la vez, solo uno
// escriba el outbox: nunca se publican dos CompraConfirmada.
func (r *Compras) Resolver(ctx context.Context, c *domain.Compra) (bool, error) {
	vencidas, err := json.Marshal(c.RetencionesVencidas)
	if err != nil {
		return false, err
	}
	var outbox *outboxModelo
	if c.Estado == domain.CompraConfirmada {
		if outbox, err = eventoOutbox(ctx, c); err != nil {
			return false, err
		}
	}

	resuelta := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&compraModelo{}).
			Where("id = ? AND estado = ?", c.ID, string(domain.CompraPendiente)).
			Updates(map[string]any{
				"estado":               string(c.Estado),
				"total":                c.Total,
				"retenciones_vencidas": string(vencidas),
				"actualizada_en":       c.ActualizadaEn,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // ya la resolvió otro
		}
		resuelta = true

		retenidos := tx.Model(&itemRetencionModelo{}).Select("item_id")
		switch c.Estado {
		case domain.CompraConfirmada:
			lineas := make([]lineaModelo, len(c.Lineas))
			for i, l := range c.Lineas {
				lineas[i] = lineaModelo{CompraID: c.ID, Orden: i, RetencionID: l.RetencionID, OfertaID: l.OfertaID, Descripcion: l.Descripcion, Destino: l.Destino, Precio: l.Precio}
			}
			if err := tx.Create(&lineas).Error; err != nil {
				return err
			}
			if err := tx.Create(outbox).Error; err != nil {
				return err
			}
			// Los ítems comprados salen del carrito.
			ids := tx.Model(&itemRetencionModelo{}).Select("item_id").Where("retencion_id IN ?", c.Retenciones)
			if err := tx.Where("usuario_id = ? AND id IN (?)", c.UsuarioID, ids).Delete(&itemModelo{}).Error; err != nil {
				return err
			}
			return tx.Where("retencion_id IN ?", c.Retenciones).Delete(&itemRetencionModelo{}).Error
		case domain.CompraRechazada:
			if len(c.RetencionesVencidas) == 0 {
				return nil
			}
			return tx.Model(&itemModelo{}).
				Where("usuario_id = ? AND id IN (?)", c.UsuarioID, retenidos.Where("retencion_id IN ?", c.RetencionesVencidas)).
				Update("estado", string(domain.ItemVencido)).Error
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("resolver compra: %w", err)
	}
	return resuelta, nil
}

func eventoOutbox(ctx context.Context, c *domain.Compra) (*outboxModelo, error) {
	ev := eventoCompraConfirmada{EventID: uuid.NewString(), CompraID: c.ID, Email: c.Email}
	for _, l := range c.Lineas {
		ev.Items = append(ev.Items, itemComprobante{Descripcion: l.Descripcion, Precio: json.Number(l.Precio.StringFixed(2))})
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	headers := map[string]any{}
	observabilidad.InyectarEnMensaje(ctx, headers)
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return nil, err
	}
	return &outboxModelo{Tipo: TipoCompraConfirmada, Payload: string(payload), Headers: string(headersJSON)}, nil
}

// ---------- Outbox (lo lee el publicador de pkg/mensajeria) ----------

type Outbox struct{ db *gorm.DB }

var _ mensajeria.Outbox = (*Outbox)(nil)

func NuevoOutbox(db *gorm.DB) *Outbox { return &Outbox{db: db} }

func (r *Outbox) Pendientes(ctx context.Context, limite int) ([]mensajeria.EventoOutbox, error) {
	var filas []outboxModelo
	err := r.db.WithContext(ctx).Where("publicado_en IS NULL").Order("id").Limit(limite).Find(&filas).Error
	if err != nil {
		return nil, fmt.Errorf("leer outbox: %w", err)
	}
	eventos := make([]mensajeria.EventoOutbox, len(filas))
	for i, f := range filas {
		headers := map[string]any{}
		if err := json.Unmarshal([]byte(f.Headers), &headers); err != nil {
			return nil, fmt.Errorf("evento %d del outbox con headers inválidos: %w", f.ID, err)
		}
		eventos[i] = mensajeria.EventoOutbox{ID: f.ID, Tipo: f.Tipo, Payload: []byte(f.Payload), Headers: headers}
	}
	return eventos, nil
}

func (r *Outbox) MarcarPublicado(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&outboxModelo{}).Where("id = ?", id).Update("publicado_en", time.Now().UTC()).Error
}

// ---------- Guías ----------

type Guias struct{ db *gorm.DB }

var _ ports.RepositorioGuias = (*Guias)(nil)

func NuevoGuias(db *gorm.DB) *Guias { return &Guias{db: db} }

func (r *Guias) Crear(ctx context.Context, g *domain.Guia, pdf []byte) error {
	m := guiaAModelo(g)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return fmt.Errorf("crear guía: %w", err)
		}
		return tx.Create(&archivoGuiaModelo{GuiaID: g.ID, PDF: pdf}).Error
	})
}

// Actualizar guarda el estado. Cuando Estudiario ya aceptó (o rechazó) la guía,
// el PDF no hace falta más y se borra.
func (r *Guias) Actualizar(ctx context.Context, g *domain.Guia) error {
	m := guiaAModelo(g)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&guiaModelo{}).Where("id = ?", g.ID).
			Select("documento_id", "estado", "motivo_rechazo", "intentos", "actualizada_en").Updates(&m).Error; err != nil {
			return fmt.Errorf("actualizar guía: %w", err)
		}
		if g.Estado != domain.GuiaPorEnviar {
			return tx.Where("guia_id = ?", g.ID).Delete(&archivoGuiaModelo{}).Error
		}
		return nil
	})
}

func (r *Guias) PorEnviar(ctx context.Context, limite int) ([]ports.GuiaConArchivo, error) {
	var ms []guiaModelo
	err := r.db.WithContext(ctx).Where("estado = ?", string(domain.GuiaPorEnviar)).
		Order("actualizada_en").Limit(limite).Find(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("leer guías por enviar: %w", err)
	}
	res := make([]ports.GuiaConArchivo, 0, len(ms))
	for _, m := range ms {
		var a archivoGuiaModelo
		if err := r.db.WithContext(ctx).Where("guia_id = ?", m.ID).Take(&a).Error; err != nil {
			return nil, fmt.Errorf("leer PDF de la guía %s: %w", m.ID, err)
		}
		res = append(res, ports.GuiaConArchivo{Guia: guiaADominio(m), PDF: a.PDF})
	}
	return res, nil
}

func (r *Guias) PendientesDeModeracion(ctx context.Context) ([]domain.Guia, error) {
	return r.listar(ctx, "estado = ?", string(domain.GuiaPendiente))
}

func (r *Guias) Listar(ctx context.Context, usuarioID string) ([]domain.Guia, error) {
	return r.listar(ctx, "usuario_id = ?", usuarioID)
}

func (r *Guias) listar(ctx context.Context, condicion string, args ...any) ([]domain.Guia, error) {
	var ms []guiaModelo
	if err := r.db.WithContext(ctx).Where(condicion, args...).Order(clause.OrderByColumn{Column: clause.Column{Name: "creada_en"}, Desc: true}).Find(&ms).Error; err != nil {
		return nil, fmt.Errorf("listar guías: %w", err)
	}
	res := make([]domain.Guia, len(ms))
	for i, m := range ms {
		res[i] = guiaADominio(m)
	}
	return res, nil
}
