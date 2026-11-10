package gateway

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entradaLimite struct {
	limitador *rate.Limiter
	ultima    time.Time
}

type limitadorPorCliente struct {
	mu             sync.Mutex
	entradas       map[string]*entradaLimite
	porMin         int
	ultimaLimpieza time.Time
}

func nuevoLimitador(porMinuto int) *limitadorPorCliente {
	return &limitadorPorCliente{entradas: make(map[string]*entradaLimite), porMin: porMinuto}
}

func (l *limitadorPorCliente) permitir(clave string, ahora time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ahora.Sub(l.ultimaLimpieza) >= time.Minute {
		for id, entrada := range l.entradas {
			if ahora.Sub(entrada.ultima) > 10*time.Minute {
				delete(l.entradas, id)
			}
		}
		l.ultimaLimpieza = ahora
	}
	entrada := l.entradas[clave]
	if entrada == nil {
		entrada = &entradaLimite{
			limitador: rate.NewLimiter(rate.Limit(l.porMin)/60, 20),
			ultima:    ahora,
		}
		l.entradas[clave] = entrada
	}
	entrada.ultima = ahora
	return entrada.limitador.AllowN(ahora, 1)
}
