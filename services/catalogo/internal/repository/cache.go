package repository

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"agencia/catalogo/internal/models"
)

// Métricas de la caché (ADR-007): con ellas se calcula la tasa de aciertos y se
// ve el impacto en el tablero.
var (
	consultasCache = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_consultas_total",
		Help: "Consultas a la caché de Catálogo por tipo de dato (oferta o guias) y resultado (acierto o fallo).",
	}, []string{"tipo", "resultado"})
	erroresCache = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cache_errores_total",
		Help: "Operaciones sobre Redis que fallaron o superaron el timeout.",
	})
	circuitoCache = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cache_circuito_abierto",
		Help: "1 si Catálogo dejó de consultar Redis por errores seguidos.",
	})
)

func init() {
	prometheus.MustRegister(consultasCache, erroresCache, circuitoCache)
}

// ConfigCache. Los valores por defecto son los de ADR-007.
type ConfigCache struct {
	URL              string
	Habilitada       bool
	Timeout          time.Duration // 50 ms
	TTLOferta        time.Duration // 10 min (+ hasta 60 s al azar)
	TTLGuias         time.Duration // 5 min
	ErroresParaCorte int           // 5
	DuracionCorte    time.Duration // 30 s
}

// Cache es opcional: si Redis falla o tarda, se informa un fallo y quien llama
// lee la fuente de verdad. Después de varios errores seguidos deja de consultar
// Redis un tiempo, para no sumar el timeout a cada solicitud.
type Cache struct {
	rdb *redis.Client
	cfg ConfigCache
	log *slog.Logger

	mu              sync.Mutex
	erroresSeguidos int
	cortadaHasta    time.Time
}

func NuevaCache(cfg ConfigCache, log *slog.Logger) (*Cache, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 50 * time.Millisecond
	}
	if cfg.TTLOferta <= 0 {
		cfg.TTLOferta = 10 * time.Minute
	}
	if cfg.TTLGuias <= 0 {
		cfg.TTLGuias = 5 * time.Minute
	}
	if cfg.ErroresParaCorte <= 0 {
		cfg.ErroresParaCorte = 5
	}
	if cfg.DuracionCorte <= 0 {
		cfg.DuracionCorte = 30 * time.Second
	}
	c := &Cache{cfg: cfg, log: log.With("componente", "caché")}
	if !cfg.Habilitada || cfg.URL == "" {
		c.cfg.Habilitada = false
		return c, nil
	}
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	opts.DialTimeout = 200 * time.Millisecond
	opts.ReadTimeout = cfg.Timeout
	opts.WriteTimeout = cfg.Timeout
	opts.MaxRetries = 0 // la caché no reintenta: si falla, se lee la fuente
	c.rdb = redis.NewClient(opts)
	return c, nil
}

func (c *Cache) Cerrar() {
	if c.rdb != nil {
		_ = c.rdb.Close()
	}
}

func claveOferta(id string) string     { return "oferta:v1:" + id }
func claveGuias(destino string) string { return "guias:v1:" + destino }

// ObtenerOferta devuelve la oferta cacheada y true si hubo acierto.
func (c *Cache) ObtenerOferta(ctx context.Context, id string) (models.Oferta, bool) {
	var o models.Oferta
	return o, c.obtener(ctx, "oferta", claveOferta(id), &o)
}

func (c *Cache) GuardarOferta(ctx context.Context, o models.Oferta) {
	// Desvío al azar de hasta 60 s para que no venzan muchas claves juntas.
	ttl := c.cfg.TTLOferta + time.Duration(rand.Int64N(int64(time.Minute)))
	c.guardar(ctx, claveOferta(o.ID), o, ttl)
}

// BorrarOferta invalida el detalle. Si falla no hace fallar la escritura: el
// borrado por evento y el TTL lo cubren (ADR-007).
func (c *Cache) BorrarOferta(ctx context.Context, id string) {
	if !c.disponible() {
		return
	}
	ctx, cancelar := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancelar()
	c.registrar(c.rdb.Del(ctx, claveOferta(id)).Err())
}

// ObtenerGuias guarda las guías por destino para no gastar el cupo de 60
// consultas por minuto de Estudiario.
func (c *Cache) ObtenerGuias(ctx context.Context, destino string, valor any) bool {
	return c.obtener(ctx, "guias", claveGuias(destino), valor)
}

func (c *Cache) GuardarGuias(ctx context.Context, destino string, valor any) {
	c.guardar(ctx, claveGuias(destino), valor, c.cfg.TTLGuias)
}

func (c *Cache) obtener(ctx context.Context, tipo, clave string, destino any) bool {
	if !c.disponible() {
		consultasCache.WithLabelValues(tipo, "fallo").Inc()
		return false
	}
	ctx, cancelar := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancelar()
	crudo, err := c.rdb.Get(ctx, clave).Bytes()
	if errors.Is(err, redis.Nil) {
		c.registrar(nil)
		consultasCache.WithLabelValues(tipo, "fallo").Inc()
		return false
	}
	c.registrar(err)
	if err != nil || json.Unmarshal(crudo, destino) != nil {
		consultasCache.WithLabelValues(tipo, "fallo").Inc()
		return false
	}
	consultasCache.WithLabelValues(tipo, "acierto").Inc()
	return true
}

func (c *Cache) guardar(ctx context.Context, clave string, valor any, ttl time.Duration) {
	if !c.disponible() {
		return
	}
	crudo, err := json.Marshal(valor)
	if err != nil {
		return
	}
	ctx, cancelar := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancelar()
	c.registrar(c.rdb.Set(ctx, clave, crudo, ttl).Err())
}

// disponible dice si hay que consultar Redis: está habilitada y no está en corte.
func (c *Cache) disponible() bool {
	if !c.cfg.Habilitada {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cortadaHasta.IsZero() {
		return true
	}
	if time.Now().Before(c.cortadaHasta) {
		return false
	}
	// Pasó la pausa: se prueba de nuevo. Un error más vuelve a cortar.
	c.cortadaHasta = time.Time{}
	c.erroresSeguidos = c.cfg.ErroresParaCorte - 1
	circuitoCache.Set(0)
	return true
}

func (c *Cache) registrar(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		if c.erroresSeguidos > 0 {
			c.erroresSeguidos = 0
		}
		return
	}
	erroresCache.Inc()
	c.erroresSeguidos++
	if c.erroresSeguidos >= c.cfg.ErroresParaCorte && c.cortadaHasta.IsZero() {
		c.cortadaHasta = time.Now().Add(c.cfg.DuracionCorte)
		circuitoCache.Set(1)
		c.log.Warn("Redis falla seguido: se deja de usar la caché por un tiempo",
			"errores_seguidos", c.erroresSeguidos, "pausa", c.cfg.DuracionCorte.String(), "error", err)
	}
}
