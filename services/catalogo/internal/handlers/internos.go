package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"agencia/catalogo/internal/services"
)

// API interna de retenciones (docs/contracts/internos/catalogo-retenciones.yaml).
// La usa solo Reservas, dentro de la red interna: no pasa por el gateway.

// POST /internal/retenciones
func retener(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var s services.SolicitudRetencion
		if !leerJSON(c, &s) {
			return
		}
		s.IdempotencyKey = c.GetHeader(headerIdempotencia)
		g, err := d.Retenciones.Retener(c.Request.Context(), s)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusCreated, g)
	}
}

// DELETE /internal/retenciones/{id}: siempre 204, aunque ya no esté vigente o no exista.
func liberar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := d.Retenciones.Liberar(c.Request.Context(), c.Param("id")); err != nil {
			responderError(c, d, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// POST /internal/retenciones/confirmacion. La Idempotency-Key es el compraId:
// si viene y no coincide con el cuerpo, la solicitud es inconsistente.
func confirmar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var s services.SolicitudConfirmacion
		if !leerJSON(c, &s) {
			return
		}
		if clave := c.GetHeader(headerIdempotencia); clave != "" && clave != s.CompraID {
			problema(c, http.StatusBadRequest, "solicitud-invalida", "Solicitud inválida",
				"La Idempotency-Key de la confirmación debe ser el compraId.")
			return
		}
		conf, err := d.Retenciones.Confirmar(c.Request.Context(), s)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, conf)
	}
}
