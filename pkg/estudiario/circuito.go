package estudiario

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// estadoCircuito del circuit breaker (ADR-009).
type estadoCircuito int

const (
	cerrado estadoCircuito = iota
	abierto
	semiabierto
)

var metricaCircuito = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "estudiario_circuito_abierto",
	Help: "1 si el circuit breaker hacia Estudiario está abierto o semiabierto; 0 si está cerrado.",
})

var metricaLlamadas = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "estudiario_llamadas_total",
	Help: "Llamadas a Estudiario por operación y resultado (ok, rechazo, no_disponible, circuito_abierto, invalida).",
}, []string{"operacion", "resultado"})

func init() {
	prometheus.MustRegister(metricaCircuito, metricaLlamadas)
}

// circuito se abre después de FallosParaAbrir llamadas fallidas seguidas, queda
// abierto Pausa y después deja pasar una sola llamada de prueba (semiabierto):
// si sale bien se cierra; si falla, vuelve a abrirse.
type circuito struct {
	fallosParaAbrir int
	pausa           time.Duration
	ahora           func() time.Time

	mu            sync.Mutex
	estado        estadoCircuito
	fallos        int
	abiertoHasta  time.Time
	pruebaEnCurso bool
}

func nuevoCircuito(fallosParaAbrir int, pausa time.Duration) *circuito {
	return &circuito{fallosParaAbrir: fallosParaAbrir, pausa: pausa, ahora: time.Now}
}

// permitir dice si se puede llamar al proveedor.
func (c *circuito) permitir() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.estado {
	case cerrado:
		return true
	case abierto:
		if c.ahora().Before(c.abiertoHasta) {
			return false
		}
		c.estado = semiabierto
		c.pruebaEnCurso = true
		return true
	default: // semiabierto: una sola llamada de prueba a la vez
		if c.pruebaEnCurso {
			return false
		}
		c.pruebaEnCurso = true
		return true
	}
}

// registrar anota el resultado de una llamada permitida. exito es false solo
// cuando el proveedor no estuvo disponible: un rechazo de negocio (4xx) es una
// respuesta sana y no abre el circuito.
func (c *circuito) registrar(exito bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruebaEnCurso = false
	if exito {
		c.estado = cerrado
		c.fallos = 0
		metricaCircuito.Set(0)
		return
	}
	c.fallos++
	if c.estado == semiabierto || c.fallos >= c.fallosParaAbrir {
		c.estado = abierto
		c.abiertoHasta = c.ahora().Add(c.pausa)
		metricaCircuito.Set(1)
	}
}

func (c *circuito) abiertoAhora() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.estado != cerrado
}
