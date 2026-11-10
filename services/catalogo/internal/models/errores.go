package models

import (
	"errors"
	"fmt"
	"time"
)

// Códigos estables que viajan en el campo code de los errores (los usan
// Reservas y el frontend para decidir qué hacer).
const (
	CodigoAgotada            = "AGOTADA"
	CodigoRetenida           = "RETENIDA"
	CodigoLimitePorCarrito   = "LIMITE_POR_CARRITO"
	CodigoPackInvalido       = "PACK_INVALIDO"
	CodigoOfertaInexistente  = "OFERTA_INEXISTENTE"
	CodigoRetencionNoVigente = "RETENCION_NO_VIGENTE"
	CodigoCupoMenorAlOcupado = "CUPO_MENOR_AL_OCUPADO"
	CodigoSolicitudInvalida  = "solicitud-invalida"
	CodigoNoEncontrado       = "no-encontrado"
	CodigoDependenciaCaida   = "servicio-no-disponible"
	// CodigoBusquedaNoDisponible: OpenSearch no responde (ADR-006). El resto de
	// Catálogo sigue funcionando.
	CodigoBusquedaNoDisponible  = "busqueda-no-disponible"
	CodigoReconstruccionEnCurso = "reconstruccion-en-curso"
)

// ErrNoEncontrado: la oferta o el pack no existen (o están despublicados).
var ErrNoEncontrado = errors.New("no encontrado")

// ErrorCampo es un problema de validación de un campo de la solicitud.
type ErrorCampo struct {
	Campo   string `json:"campo"`
	Mensaje string `json:"mensaje"`
}

type ErrValidacion struct {
	Errores []ErrorCampo
}

func (e *ErrValidacion) Error() string { return fmt.Sprintf("solicitud inválida: %v", e.Errores) }

// ErrOfertaInexistente: se pidió retener una oferta o un pack que no existe.
type ErrOfertaInexistente struct {
	ID string
}

func (e *ErrOfertaInexistente) Error() string { return "oferta inexistente: " + e.ID }

// ErrSinCupo: no hay cupo libre suficiente. Codigo es AGOTADA o RETENIDA (RN-13).
type ErrSinCupo struct {
	Codigo            string
	OfertaID          string
	Libres            int
	ProximaLiberacion *time.Time
}

func (e *ErrSinCupo) Error() string {
	return fmt.Sprintf("sin cupo en %s (%s, quedan %d)", e.OfertaID, e.Codigo, e.Libres)
}

// ErrRegla: se incumple una regla de negocio (LIMITE_POR_CARRITO, PACK_INVALIDO,
// CUPO_MENOR_AL_OCUPADO).
type ErrRegla struct {
	Codigo  string
	Detalle string
}

func (e *ErrRegla) Error() string { return e.Codigo + ": " + e.Detalle }

// ErrRetencionesNoVigentes: alguna retención de la compra venció, se liberó o es
// de otro titular. No se confirmó ninguna (RN-02, RN-11).
type ErrRetencionesNoVigentes struct {
	IDs []string
}

func (e *ErrRetencionesNoVigentes) Error() string {
	return fmt.Sprintf("retenciones no vigentes: %v", e.IDs)
}
