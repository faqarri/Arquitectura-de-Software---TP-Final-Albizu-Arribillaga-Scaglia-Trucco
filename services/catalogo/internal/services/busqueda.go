package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"agencia/catalogo/internal/models"
	"agencia/catalogo/internal/repository"
)

// Métricas de la búsqueda (ADR-006). El p95 del retraso es el objetivo de
// servicio: 5 s o menos entre la escritura en MongoDB y la aparición en la búsqueda.
var (
	retrasoIndexacion = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "busqueda_retraso_indexacion_segundos",
		Help:    "Tiempo entre la escritura de una oferta en MongoDB y su visibilidad en la búsqueda (ahora + refresh_interval - actualizadaEn).",
		Buckets: []float64{.25, .5, 1, 1.5, 2, 3, 5, 10, 30, 60},
	})
	indexaciones = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "busqueda_indexaciones_total",
		Help: "Ofertas procesadas por el indexador, por resultado (indexada, repetida o inexistente).",
	}, []string{"resultado"})
)

func init() { prometheus.MustRegister(retrasoIndexacion, indexaciones) }

const (
	loteReconstruccion = 500
	// refreshIntervalo es el refresh_interval del índice: lo que tarda en ser
	// visible un documento ya escrito.
	refreshIntervalo = time.Second
)

// Busqueda es la búsqueda de ofertas (HU-01) y su sincronización con MongoDB.
type Busqueda struct {
	indice  *repository.IndiceBusqueda
	ofertas *repository.Ofertas
	log     *slog.Logger

	reconstruyendo sync.Mutex
}

func NuevoBusqueda(indice *repository.IndiceBusqueda, ofertas *repository.Ofertas, log *slog.Logger) *Busqueda {
	return &Busqueda{indice: indice, ofertas: ofertas, log: log.With("componente", "búsqueda")}
}

// Buscar valida los filtros y consulta el índice.
func (s *Busqueda) Buscar(ctx context.Context, f models.FiltrosBusqueda) (models.PaginaOfertas, error) {
	if err := f.Validar(); err != nil {
		return models.PaginaOfertas{}, err
	}
	return s.indice.Buscar(ctx, f)
}

// Indexar lleva al índice el estado actual de la oferta en MongoDB. El evento
// solo dice qué oferta cambió: se lee siempre la versión vigente, y la versión
// externa de OpenSearch descarta lo que llegue repetido o fuera de orden.
func (s *Busqueda) Indexar(ctx context.Context, ofertaID string) error {
	o, err := s.ofertas.Obtener(ctx, ofertaID)
	if errors.Is(err, models.ErrNoEncontrado) {
		// No hay nada que indexar (no debería pasar: las ofertas no se borran).
		indexaciones.WithLabelValues("inexistente").Inc()
		s.log.WarnContext(ctx, "OfertaActualizada de una oferta que no está en MongoDB", "oferta_id", ofertaID)
		return nil
	}
	if err != nil {
		return err
	}
	nueva, err := s.indice.Indexar(ctx, o)
	if err != nil {
		return err
	}
	if !nueva {
		indexaciones.WithLabelValues("repetida").Inc()
		return nil
	}
	indexaciones.WithLabelValues("indexada").Inc()
	if !o.ActualizadaEn.IsZero() {
		retrasoIndexacion.Observe(time.Since(o.ActualizadaEn).Seconds() + refreshIntervalo.Seconds())
	}
	return nil
}

// Preparar deja el índice listo para usar. La primera vez crea el índice y el
// alias y lo carga con todas las ofertas de MongoDB (por ejemplo, si ya había
// ofertas cuando se agregó la búsqueda). Si ya existe, no hace nada.
func (s *Busqueda) Preparar(ctx context.Context) error {
	creado, err := s.indice.AsegurarIndice(ctx)
	if err != nil || !creado {
		return err
	}
	nombre := s.indice.NombreIndiceInicial()
	n, err := s.cargar(ctx, nombre, time.Time{})
	if err != nil {
		// Sin la carga completa el índice quedaría incompleto para siempre:
		// se borra para que el próximo intento lo cree de nuevo.
		if errBorrar := s.indice.BorrarIndices(context.WithoutCancel(ctx), []string{nombre}); errBorrar != nil {
			s.log.ErrorContext(ctx, "no se pudo borrar el índice a medio cargar: hay que reconstruirlo", "indice", nombre, "error", errBorrar)
		}
		return fmt.Errorf("carga inicial del índice: %w", err)
	}
	s.log.InfoContext(ctx, "índice de búsqueda creado y cargado", "indice", nombre, "ofertas", n)
	return nil
}

// Reconstruir arma un índice nuevo con todo MongoDB y le pasa el alias sin
// cortar la búsqueda (ADR-006, "Reconstrucción completa").
func (s *Busqueda) Reconstruir(ctx context.Context) (models.ResultadoReconstruccion, error) {
	if !s.reconstruyendo.TryLock() {
		return models.ResultadoReconstruccion{}, models.ErrReconstruccionEnCurso
	}
	defer s.reconstruyendo.Unlock()

	// 1. T0 e índice nuevo con el mapping vigente.
	t0 := time.Now().UTC()
	nuevo := s.indice.NombreIndiceNuevo(t0)
	res := models.ResultadoReconstruccion{Indice: nuevo, IndicesBorrados: []string{}}
	if err := s.indice.CrearIndice(ctx, nuevo); err != nil {
		return res, err
	}
	// 2. Todas las ofertas, en lotes de 500 con versiones externas.
	n, err := s.cargar(ctx, nuevo, time.Time{})
	if err != nil {
		_ = s.indice.BorrarIndices(context.WithoutCancel(ctx), []string{nuevo})
		return res, fmt.Errorf("cargar el índice nuevo: %w", err)
	}
	res.Documentos = n
	// 3. Cambio atómico del alias.
	viejos, err := s.indice.IndicesDelAlias(ctx)
	if err != nil {
		return res, err
	}
	if err := s.indice.MoverAlias(ctx, nuevo, viejos); err != nil {
		return res, err
	}
	// 4. Lo que cambió durante la carga (el indexador lo escribió en el índice viejo).
	if res.Recuperados, err = s.cargar(ctx, nuevo, t0); err != nil {
		return res, fmt.Errorf("recuperar los cambios hechos durante la carga: %w", err)
	}
	// 5. Se borra el índice anterior solo si el nuevo tiene todo.
	if err := s.indice.Refrescar(ctx, nuevo); err != nil {
		return res, err
	}
	total, err := s.indice.Contar(ctx, nuevo)
	if err != nil {
		return res, err
	}
	var aBorrar []string
	for _, v := range viejos {
		if v != nuevo {
			aBorrar = append(aBorrar, v)
		}
	}
	if total < n {
		return res, fmt.Errorf("el índice nuevo tiene %d documentos y se cargaron %d: se conservan los anteriores %v", total, n, aBorrar)
	}
	if err := s.indice.BorrarIndices(ctx, aBorrar); err != nil {
		return res, err
	}
	res.IndicesBorrados = append(res.IndicesBorrados, aBorrar...)
	res.DuracionSegundos = time.Since(t0).Seconds()
	s.log.InfoContext(ctx, "índice de búsqueda reconstruido", "indice", nuevo, "ofertas", n,
		"recuperadas", res.Recuperados, "borrados", aBorrar, "duracion_s", res.DuracionSegundos)
	return res, nil
}

// cargar copia a indice las ofertas modificadas desde "desde" (todas si es cero).
func (s *Busqueda) cargar(ctx context.Context, indice string, desde time.Time) (int, error) {
	n := 0
	err := s.ofertas.Recorrer(ctx, desde, loteReconstruccion, func(lote []models.Oferta) error {
		if err := s.indice.CargarLote(ctx, indice, lote); err != nil {
			return err
		}
		n += len(lote)
		return nil
	})
	return n, err
}
