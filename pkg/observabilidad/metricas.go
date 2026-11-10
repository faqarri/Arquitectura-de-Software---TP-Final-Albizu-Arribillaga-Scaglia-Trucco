package observabilidad

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	solicitudesHTTP = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Cantidad total de solicitudes HTTP recibidas.",
		},
		[]string{"method", "route", "status_code"},
	)
	duracionSolicitudesHTTP = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duración de las solicitudes HTTP en segundos.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status_code"},
	)
)

func init() {
	prometheus.MustRegister(solicitudesHTTP, duracionSolicitudesHTTP)
}

// MiddlewareMetricas cuenta solicitudes y observa su duración usando la ruta
// registrada por Gin, evitando etiquetas de alta cardinalidad.
func MiddlewareMetricas() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		inicio := time.Now()
		c.Next()

		ruta := c.FullPath()
		if ruta == "" {
			ruta = "unmatched"
		}
		etiquetas := prometheus.Labels{
			"method":      c.Request.Method,
			"route":       ruta,
			"status_code": strconv.Itoa(c.Writer.Status()),
		}
		solicitudesHTTP.With(etiquetas).Inc()
		duracionSolicitudesHTTP.With(etiquetas).Observe(time.Since(inicio).Seconds())
	}
}

// HandlerMetricas devuelve el handler HTTP de Prometheus para exponer las
// métricas registradas por el proceso.
func HandlerMetricas() http.Handler {
	return promhttp.Handler()
}
