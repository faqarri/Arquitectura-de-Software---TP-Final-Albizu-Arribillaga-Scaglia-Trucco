package observabilidad

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMiddlewareMetricasCuentaConRutaNormalizada(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(MiddlewareMetricas())
	router.GET("/items/:itemID", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	etiquetas := prometheus.Labels{
		"method":      http.MethodGet,
		"route":       "/items/:itemID",
		"status_code": strconv.Itoa(http.StatusCreated),
	}
	contadorAntes := valorContador(t, solicitudesHTTP.With(etiquetas))
	observacionesAntes := cantidadObservaciones(t, duracionSolicitudesHTTP.With(etiquetas))

	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, "/items/123", nil))

	if respuesta.Code != http.StatusCreated {
		t.Fatalf("estado HTTP = %d, se esperaba %d", respuesta.Code, http.StatusCreated)
	}
	if got := valorContador(t, solicitudesHTTP.With(etiquetas)); got != contadorAntes+1 {
		t.Fatalf("contador = %v, se esperaba %v", got, contadorAntes+1)
	}
	if got := cantidadObservaciones(t, duracionSolicitudesHTTP.With(etiquetas)); got != observacionesAntes+1 {
		t.Fatalf("observaciones del histograma = %d, se esperaba %d", got, observacionesAntes+1)
	}
}

func TestMiddlewareMetricasExcluyeMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(MiddlewareMetricas())
	router.GET("/metrics", gin.WrapH(HandlerMetricas()))

	etiquetas := prometheus.Labels{
		"method":      http.MethodGet,
		"route":       "/metrics",
		"status_code": strconv.Itoa(http.StatusOK),
	}
	contadorAntes := valorContador(t, solicitudesHTTP.With(etiquetas))
	observacionesAntes := cantidadObservaciones(t, duracionSolicitudesHTTP.With(etiquetas))

	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if respuesta.Code != http.StatusOK {
		t.Fatalf("estado de /metrics = %d, se esperaba %d", respuesta.Code, http.StatusOK)
	}
	if got := valorContador(t, solicitudesHTTP.With(etiquetas)); got != contadorAntes {
		t.Fatalf("el scraping incrementó el contador: antes %v, después %v", contadorAntes, got)
	}
	if got := cantidadObservaciones(t, duracionSolicitudesHTTP.With(etiquetas)); got != observacionesAntes {
		t.Fatalf("el scraping incrementó el histograma: antes %d, después %d", observacionesAntes, got)
	}
}

func valorContador(t *testing.T, contador prometheus.Counter) float64 {
	t.Helper()
	metrica := &dto.Metric{}
	if err := contador.Write(metrica); err != nil {
		t.Fatalf("leer contador: %v", err)
	}
	return metrica.GetCounter().GetValue()
}

func cantidadObservaciones(t *testing.T, histograma prometheus.Observer) uint64 {
	t.Helper()
	metrica, ok := histograma.(prometheus.Metric)
	if !ok {
		t.Fatal("el histograma no implementa prometheus.Metric")
	}
	dtoMetrica := &dto.Metric{}
	if err := metrica.Write(dtoMetrica); err != nil {
		t.Fatalf("leer histograma: %v", err)
	}
	return dtoMetrica.GetHistogram().GetSampleCount()
}
