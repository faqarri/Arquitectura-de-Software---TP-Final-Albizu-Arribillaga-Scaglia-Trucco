// Package services es la capa de lógica de aplicación de Catálogo: coordina
// MongoDB, MySQL y la caché a través de repository y aplica las reglas de
// models. No conoce HTTP ni RabbitMQ (ADR-002).
package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"agencia/catalogo/internal/models"
	"agencia/catalogo/internal/repository"
)

// Latencia del detalle según de dónde salió (ADR-007): la comparación entre
// "cache" y "mongo" es la evidencia del impacto de la caché.
var duracionDetalle = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "catalogo_detalle_oferta_segundos",
	Help:    "Duración de la lectura del detalle de una oferta, por origen (cache o mongo).",
	Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1},
}, []string{"origen"})

func init() { prometheus.MustRegister(duracionDetalle) }

const (
	OrigenCache = "cache"
	OrigenMongo = "mongo"
)

// Ofertas administra las ofertas: descripción en MongoDB, cupo en MySQL y
// detalle cacheado en Redis.
type Ofertas struct {
	ofertas *repository.Ofertas
	inv     *repository.Inventario
	cache   *repository.Cache
	log     *slog.Logger
}

func NuevoOfertas(ofertas *repository.Ofertas, inv *repository.Inventario, cache *repository.Cache, log *slog.Logger) *Ofertas {
	return &Ofertas{ofertas: ofertas, inv: inv, cache: cache, log: log}
}

// Crear da de alta una oferta (HU-09). Escribe primero el inventario en MySQL y
// después la descripción en MongoDB: no hay transacción común (ADR-014), y en
// este orden una falla deja a lo sumo un inventario sin oferta, que no se puede
// retener porque retener lee la oferta de MongoDB.
func (s *Ofertas) Crear(ctx context.Context, n models.OfertaNueva) (models.Oferta, error) {
	if err := n.Validar(); err != nil {
		return models.Oferta{}, err
	}
	o := models.Oferta{ID: "of-" + uuid.NewString()}
	o.Aplicar(n)
	if err := s.inv.CrearInventario(ctx, o.ID, n.CupoTotal); err != nil {
		return models.Oferta{}, err
	}
	creada, err := s.ofertas.Crear(ctx, o)
	if err != nil {
		return models.Oferta{}, err
	}
	cero := 0
	creada.CupoTotal, creada.Vendidos = &n.CupoTotal, &cero
	return creada, nil
}

// Actualizar edita una oferta. Regla: el cupo no puede quedar por debajo de lo
// ya vendido o retenido (RN-01). Borra el detalle de la caché en la misma
// solicitud para que el administrador vea su cambio al instante (ADR-007).
func (s *Ofertas) Actualizar(ctx context.Context, id string, n models.OfertaNueva) (models.Oferta, error) {
	if err := n.Validar(); err != nil {
		return models.Oferta{}, err
	}
	existente, err := s.ofertas.Obtener(ctx, id)
	if err != nil {
		return models.Oferta{}, err
	}
	if err := s.inv.ActualizarCupo(ctx, id, n.CupoTotal); err != nil {
		return models.Oferta{}, err
	}
	existente.Aplicar(n)
	actualizada, err := s.ofertas.Actualizar(ctx, existente)
	if err != nil {
		return models.Oferta{}, err
	}
	s.cache.BorrarOferta(ctx, id)
	s.completarCupo(ctx, &actualizada)
	return actualizada, nil
}

// Despublicar es la baja lógica (HU-09): la oferta deja de verse y de poder
// retenerse, pero las retenciones y compras existentes no cambian.
func (s *Ofertas) Despublicar(ctx context.Context, id string) error {
	if _, err := s.ofertas.Despublicar(ctx, id); err != nil {
		return err
	}
	s.cache.BorrarOferta(ctx, id)
	return nil
}

// Detalle devuelve una oferta publicada, con caché cache-aside (ADR-007). El
// administrador lee siempre de MongoDB, ve también las despublicadas y recibe
// el cupo y los vendidos, que cambian con cada venta y por eso no se cachean.
func (s *Ofertas) Detalle(ctx context.Context, id string, admin bool) (models.Oferta, string, error) {
	inicio := time.Now()
	if !admin {
		if o, ok := s.cache.ObtenerOferta(ctx, id); ok {
			duracionDetalle.WithLabelValues(OrigenCache).Observe(time.Since(inicio).Seconds())
			return o, OrigenCache, nil
		}
	}
	o, err := s.ofertas.Obtener(ctx, id)
	if err != nil {
		return models.Oferta{}, OrigenMongo, err
	}
	if !o.Publicada && !admin {
		return models.Oferta{}, OrigenMongo, models.ErrNoEncontrado // no se cachean las inexistentes
	}
	if o.Publicada {
		s.cache.GuardarOferta(ctx, o)
	}
	duracionDetalle.WithLabelValues(OrigenMongo).Observe(time.Since(inicio).Seconds())
	if admin {
		s.completarCupo(ctx, &o)
	}
	return o, OrigenMongo, nil
}

// Disponibilidad se calcula siempre en MySQL, nunca se cachea (ADR-007).
func (s *Ofertas) Disponibilidad(ctx context.Context, id string) (models.Disponibilidad, error) {
	return s.inv.Disponibilidad(ctx, id)
}

// InvalidarCache borra el detalle al consumir OfertaActualizada. Cubre el caso
// en que el borrado al escribir falló o perdió una carrera (ADR-007).
func (s *Ofertas) InvalidarCache(ctx context.Context, id string) {
	s.cache.BorrarOferta(ctx, id)
}

func (s *Ofertas) completarCupo(ctx context.Context, o *models.Oferta) {
	total, vendidos, err := s.inv.Cupo(ctx, o.ID)
	if err != nil {
		if !errors.Is(err, models.ErrNoEncontrado) {
			s.log.WarnContext(ctx, "no se pudo leer el cupo de la oferta", "oferta_id", o.ID, "error", err)
		}
		return
	}
	o.CupoTotal, o.Vendidos = &total, &vendidos
}
