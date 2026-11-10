package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"agencia/catalogo/internal/models"
	"agencia/pkg/observabilidad"
)

// problema responde con application/problem+json (RFC 9457). code es estable:
// Reservas y el frontend deciden por status y code, nunca por el texto.
func problema(c *gin.Context, estado int, code, titulo, detalle string, extra ...gin.H) {
	cuerpo := gin.H{
		"type":   "https://agencia-viajes.example/errores/" + code,
		"title":  titulo,
		"status": estado,
		"code":   code,
	}
	if detalle != "" {
		cuerpo["detail"] = detalle
	}
	for _, e := range extra {
		for k, v := range e {
			cuerpo[k] = v
		}
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(estado, cuerpo)
}

// responderError traduce los errores de services al contrato. Lo que no es un
// error de negocio es una falla de una dependencia (MySQL o MongoDB): 503, que
// Reservas puede reintentar porque las operaciones son idempotentes.
func responderError(c *gin.Context, d Dependencias, err error) {
	var (
		validacion  *models.ErrValidacion
		inexistente *models.ErrOfertaInexistente
		sinCupo     *models.ErrSinCupo
		regla       *models.ErrRegla
		noVigentes  *models.ErrRetencionesNoVigentes
	)
	switch {
	case errors.As(err, &validacion):
		problema(c, http.StatusBadRequest, models.CodigoSolicitudInvalida, "Solicitud inválida",
			"Revisá los datos marcados.", gin.H{"errores": validacion.Errores})
	case errors.As(err, &inexistente):
		problema(c, http.StatusNotFound, models.CodigoOfertaInexistente, "Oferta inexistente",
			"La oferta o el pack no existe o ya no está publicado.", gin.H{"ofertaId": inexistente.ID})
	case errors.As(err, &sinCupo):
		extra := gin.H{"ofertaId": sinCupo.OfertaID, "libres": sinCupo.Libres}
		if sinCupo.ProximaLiberacion != nil {
			extra["proximaLiberacion"] = sinCupo.ProximaLiberacion
		}
		problema(c, http.StatusConflict, sinCupo.Codigo, "Sin cupo", "", extra)
	case errors.As(err, &regla):
		problema(c, http.StatusUnprocessableEntity, regla.Codigo, "Regla incumplida", regla.Detalle)
	case errors.As(err, &noVigentes):
		problema(c, http.StatusConflict, models.CodigoRetencionNoVigente, "Retenciones no vigentes",
			"Alguna retención venció, se liberó o no es del titular. No se confirmó ninguna.",
			gin.H{"retencionIds": noVigentes.IDs})
	case errors.Is(err, models.ErrNoEncontrado):
		problema(c, http.StatusNotFound, models.CodigoNoEncontrado, "No encontrado", "")
	case errors.Is(err, models.ErrBusquedaNoDisponible):
		ctx := c.Request.Context()
		observabilidad.Logger(ctx, d.Log).WarnContext(ctx, "la búsqueda no está disponible", "error", err)
		c.Header("Retry-After", "5")
		problema(c, http.StatusServiceUnavailable, models.CodigoBusquedaNoDisponible, "Búsqueda no disponible",
			"La búsqueda no está disponible en este momento. Intentá de nuevo en unos segundos.")
	case errors.Is(err, models.ErrReconstruccionEnCurso):
		problema(c, http.StatusConflict, models.CodigoReconstruccionEnCurso, "Reconstrucción en curso",
			"Ya hay una reconstrucción del índice corriendo en esta instancia.")
	default:
		ctx := c.Request.Context()
		observabilidad.Logger(ctx, d.Log).ErrorContext(ctx, "no se pudo atender la solicitud", "error", err)
		problema(c, http.StatusServiceUnavailable, models.CodigoDependenciaCaida, "Servicio no disponible",
			"No pudimos completar la operación. Intentá de nuevo en unos segundos.")
	}
}

// leerJSON decodifica el cuerpo; si no se puede, responde 400 y devuelve false.
func leerJSON(c *gin.Context, destino any) bool {
	if err := c.ShouldBindJSON(destino); err != nil {
		problema(c, http.StatusBadRequest, models.CodigoSolicitudInvalida, "Solicitud inválida",
			"El cuerpo no es un JSON válido o tiene campos con formato incorrecto (las fechas van en ISO 8601).")
		return false
	}
	return true
}
