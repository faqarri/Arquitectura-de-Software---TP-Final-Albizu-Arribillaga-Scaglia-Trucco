package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

// codigoClaveDuplicada es el error de MySQL ER_DUP_ENTRY.
const codigoClaveDuplicada = 1062

// Repositorio implementa ports.RepositorioComprobantes y ports.Outbox.
type Repositorio struct {
	db *gorm.DB
}

func NuevoRepositorio(db *gorm.DB) *Repositorio {
	return &Repositorio{db: db}
}

var (
	_ ports.RepositorioComprobantes = (*Repositorio)(nil)
	_ ports.Outbox                  = (*Repositorio)(nil)
)

// Guardar inserta el comprobante, sus ítems y el evento EnvioSolicitado del
// outbox en una sola transacción: o queda todo, o nada. Así un comprobante
// emitido siempre tiene su envío pedido (ADR-005).
func (r *Repositorio) Guardar(ctx context.Context, c *domain.Comprobante) error {
	modelo := aModelo(c)

	payload, err := json.Marshal(map[string]string{"comprobanteId": c.ID})
	if err != nil {
		return err
	}
	headers := map[string]any{}
	observabilidad.InyectarEnMensaje(ctx, headers)
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&modelo).Error; err != nil {
			return err
		}
		// MySQL no devuelve columnas AUTO_INCREMENT que no son la clave primaria:
		// se leen dentro de la misma transacción.
		if err := tx.Model(&comprobanteModelo{}).Where("id = ?", c.ID).Pluck("nro", &modelo.Nro).Error; err != nil {
			return err
		}
		return tx.Create(&outboxModelo{
			Tipo:    ports.EventoEnvioSolicitado,
			Payload: string(payload),
			Headers: string(headersJSON),
		}).Error
	})
	if esClaveDuplicada(err) {
		return ports.ErrClaveDuplicada
	}
	if err != nil {
		return fmt.Errorf("guardar comprobante: %w", err)
	}
	c.Nro = modelo.Nro
	return nil
}

func (r *Repositorio) BuscarPorClave(ctx context.Context, consumidor, clave string) (*domain.Comprobante, error) {
	return r.buscar(ctx, "consumidor = ? AND idempotency_key = ?", consumidor, clave)
}

func (r *Repositorio) BuscarPorID(ctx context.Context, consumidor, id string) (*domain.Comprobante, error) {
	return r.buscar(ctx, "id = ? AND consumidor = ?", id, consumidor)
}

func (r *Repositorio) Obtener(ctx context.Context, id string) (*domain.Comprobante, error) {
	return r.buscar(ctx, "id = ?", id)
}

func (r *Repositorio) buscar(ctx context.Context, condicion string, args ...any) (*domain.Comprobante, error) {
	var m comprobanteModelo
	err := r.db.WithContext(ctx).
		Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("orden") }).
		Where(condicion, args...).
		Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("buscar comprobante: %w", err)
	}
	return aDominio(m), nil
}

func (r *Repositorio) ActualizarEnvio(ctx context.Context, c *domain.Comprobante) error {
	err := r.db.WithContext(ctx).Model(&comprobanteModelo{}).
		Where("id = ?", c.ID).
		Updates(map[string]any{
			"estado_envio":   string(c.EstadoEnvio),
			"intentos_envio": c.IntentosEnvio,
		}).Error
	if err != nil {
		return fmt.Errorf("actualizar envío: %w", err)
	}
	return nil
}

// Pendientes devuelve los eventos del outbox que todavía no se publicaron, del
// más viejo al más nuevo.
func (r *Repositorio) Pendientes(ctx context.Context, limite int) ([]ports.EventoOutbox, error) {
	var filas []outboxModelo
	err := r.db.WithContext(ctx).
		Where("publicado_en IS NULL").
		Order("id").
		Limit(limite).
		Find(&filas).Error
	if err != nil {
		return nil, fmt.Errorf("leer outbox: %w", err)
	}

	eventos := make([]ports.EventoOutbox, len(filas))
	for i, f := range filas {
		headers := map[string]any{}
		if err := json.Unmarshal([]byte(f.Headers), &headers); err != nil {
			return nil, fmt.Errorf("evento %d del outbox con headers inválidos: %w", f.ID, err)
		}
		eventos[i] = ports.EventoOutbox{ID: f.ID, Tipo: f.Tipo, Payload: []byte(f.Payload), Headers: headers}
	}
	return eventos, nil
}

func (r *Repositorio) MarcarPublicado(ctx context.Context, id uint64) error {
	err := r.db.WithContext(ctx).Model(&outboxModelo{}).
		Where("id = ?", id).
		Update("publicado_en", time.Now().UTC()).Error
	if err != nil {
		return fmt.Errorf("marcar evento %d como publicado: %w", id, err)
	}
	return nil
}

// esClaveDuplicada reconoce el choque con la clave única (consumidor, idempotency_key).
func esClaveDuplicada(err error) bool {
	var e *drivermysql.MySQLError
	return errors.As(err, &e) && e.Number == codigoClaveDuplicada &&
		strings.Contains(e.Message, "ux_consumidor_clave")
}

func aModelo(c *domain.Comprobante) comprobanteModelo {
	items := make([]itemModelo, len(c.Items))
	for i, it := range c.Items {
		items[i] = itemModelo{ComprobanteID: c.ID, Orden: i, Descripcion: it.Descripcion, Precio: it.Precio}
	}
	return comprobanteModelo{
		ID:             c.ID,
		Consumidor:     c.Consumidor,
		IdempotencyKey: c.IdempotencyKey,
		HashSolicitud:  c.HashSolicitud,
		Email:          c.Email,
		Total:          c.Total,
		EstadoEnvio:    string(c.EstadoEnvio),
		IntentosEnvio:  c.IntentosEnvio,
		FechaEmision:   c.FechaEmision,
		Items:          items,
	}
}

func aDominio(m comprobanteModelo) *domain.Comprobante {
	items := make([]domain.Item, len(m.Items))
	for i, it := range m.Items {
		items[i] = domain.Item{Descripcion: it.Descripcion, Precio: it.Precio}
	}
	return &domain.Comprobante{
		ID:             m.ID,
		Nro:            m.Nro,
		Consumidor:     m.Consumidor,
		IdempotencyKey: m.IdempotencyKey,
		HashSolicitud:  m.HashSolicitud,
		Email:          m.Email,
		Items:          items,
		Total:          m.Total,
		EstadoEnvio:    domain.EstadoEnvio(m.EstadoEnvio),
		IntentosEnvio:  m.IntentosEnvio,
		FechaEmision:   m.FechaEmision.UTC(),
	}
}
