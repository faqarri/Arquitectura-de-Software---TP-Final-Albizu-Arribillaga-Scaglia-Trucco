package mysql

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

var errNoImplementado = errors.New("no implementado")

// Repositorio implementa ports.RepositorioComprobantes.
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

// Guardar: TODO(api-mysql). En una sola transacción (db.Transaction):
//   - insertar comprobanteModelo con sus Items;
//   - insertar en outbox un evento Tipo "EnvioSolicitado" con payload {"comprobanteId": ...}
//     y en Headers el JSON de observabilidad.InyectarEnMensaje(ctx, headers);
//   - completar c.Nro con el valor asignado por la base.
//
// Si MySQL responde clave duplicada (error 1062 sobre ux_consumidor_clave),
// devolver ports.ErrClaveDuplicada.
func (r *Repositorio) Guardar(ctx context.Context, c *domain.Comprobante) error {
	return errNoImplementado
}

// BuscarPorClave: TODO(api-mysql). Preload("Items"), ordenados por Orden.
func (r *Repositorio) BuscarPorClave(ctx context.Context, consumidor, clave string) (*domain.Comprobante, error) {
	return nil, errNoImplementado
}

// BuscarPorID: TODO(api-mysql). Filtrar por id Y consumidor.
func (r *Repositorio) BuscarPorID(ctx context.Context, consumidor, id string) (*domain.Comprobante, error) {
	return nil, errNoImplementado
}

// Obtener: TODO(api-mysql). Igual que BuscarPorID pero sin filtrar por consumidor.
func (r *Repositorio) Obtener(ctx context.Context, id string) (*domain.Comprobante, error) {
	return nil, errNoImplementado
}

// ActualizarEnvio: TODO(api-mysql). Actualizar estado_envio e intentos_envio.
func (r *Repositorio) ActualizarEnvio(ctx context.Context, c *domain.Comprobante) error {
	return errNoImplementado
}

// Pendientes: TODO(api-mysql). Hasta limite eventos con publicado_en NULL, por
// orden de id, con Headers decodificado. Lo usa el publicador de outbox (adapters/rabbit).
func (r *Repositorio) Pendientes(ctx context.Context, limite int) ([]ports.EventoOutbox, error) {
	return nil, errNoImplementado
}

// MarcarPublicado: TODO(api-mysql). Completar publicado_en.
func (r *Repositorio) MarcarPublicado(ctx context.Context, id uint64) error {
	return errNoImplementado
}
