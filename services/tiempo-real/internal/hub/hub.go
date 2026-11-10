// Package hub guarda en memoria las conexiones abiertas y a qué ofertas o a qué
// usuario está suscripta cada una (ADR-015). Es el único estado del servicio, y
// se pierde al reiniciar: los navegadores reconectan y consultan el estado
// actual a Catálogo.
package hub

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// TamanioBuffer es cuántos eventos puede tener pendientes una conexión lenta
// antes de que se le descarten los nuevos. Son avisos de mejor esfuerzo.
const TamanioBuffer = 16

var (
	conexiones = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tiempo_real_conexiones",
		Help: "Conexiones SSE abiertas en esta instancia.",
	})
	descartados = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tiempo_real_eventos_descartados_total",
		Help: "Eventos no entregados porque la conexión tenía el buffer lleno.",
	})
)

func init() { prometheus.MustRegister(conexiones, descartados) }

// Evento es un evento SSE ya serializado.
type Evento struct {
	Nombre string // el campo "event:" de SSE
	Datos  []byte // JSON del campo "data:"
}

// Suscripcion es una conexión abierta. Eventos se lee hasta que se cancela.
type Suscripcion struct {
	Eventos chan Evento
	ofertas []string
	usuario string
}

// Hub reparte los eventos entre las suscripciones. Es seguro para usar desde
// varias goroutines.
type Hub struct {
	mu         sync.RWMutex
	porOferta  map[string]map[*Suscripcion]struct{}
	porUsuario map[string]map[*Suscripcion]struct{}
}

func Nuevo() *Hub {
	return &Hub{
		porOferta:  map[string]map[*Suscripcion]struct{}{},
		porUsuario: map[string]map[*Suscripcion]struct{}{},
	}
}

// SuscribirOfertas registra una conexión interesada en esas ofertas.
func (h *Hub) SuscribirOfertas(ids []string) *Suscripcion {
	s := &Suscripcion{Eventos: make(chan Evento, TamanioBuffer), ofertas: ids}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		agregar(h.porOferta, id, s)
	}
	conexiones.Inc()
	return s
}

// SuscribirUsuario registra una conexión que recibe los eventos de un usuario.
func (h *Hub) SuscribirUsuario(id string) *Suscripcion {
	s := &Suscripcion{Eventos: make(chan Evento, TamanioBuffer), usuario: id}
	h.mu.Lock()
	defer h.mu.Unlock()
	agregar(h.porUsuario, id, s)
	conexiones.Inc()
	return s
}

// Cancelar da de baja la conexión. Después de esto no recibe más eventos.
func (h *Hub) Cancelar(s *Suscripcion) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range s.ofertas {
		quitar(h.porOferta, id, s)
	}
	if s.usuario != "" {
		quitar(h.porUsuario, s.usuario, s)
	}
	conexiones.Dec()
}

// PublicarOferta manda el evento a las conexiones que miran esa oferta y
// devuelve a cuántas se entregó.
func (h *Hub) PublicarOferta(ofertaID string, e Evento) int {
	return h.publicar(h.porOferta, ofertaID, e)
}

// PublicarUsuario manda el evento a las conexiones de ese usuario.
func (h *Hub) PublicarUsuario(usuarioID string, e Evento) int {
	return h.publicar(h.porUsuario, usuarioID, e)
}

func (h *Hub) publicar(indice map[string]map[*Suscripcion]struct{}, clave string, e Evento) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	entregados := 0
	for s := range indice[clave] {
		select {
		case s.Eventos <- e:
			entregados++
		default:
			// Conexión lenta: no se frena a las demás. Al reconectar, el navegador
			// consulta el estado actual.
			descartados.Inc()
		}
	}
	return entregados
}

func agregar(indice map[string]map[*Suscripcion]struct{}, clave string, s *Suscripcion) {
	if indice[clave] == nil {
		indice[clave] = map[*Suscripcion]struct{}{}
	}
	indice[clave][s] = struct{}{}
}

func quitar(indice map[string]map[*Suscripcion]struct{}, clave string, s *Suscripcion) {
	delete(indice[clave], s)
	if len(indice[clave]) == 0 {
		delete(indice, clave)
	}
}
