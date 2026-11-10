package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"agencia/catalogo/internal/models"
	"agencia/catalogo/internal/services"
)

// GET /api/ofertas/{id}. X-Cache dice si salió de Redis o de MongoDB.
func detalleOferta(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		o, origen, err := d.Ofertas.Detalle(c.Request.Context(), c.Param("id"), esAdmin(c))
		if err != nil {
			responderError(c, d, err)
			return
		}
		if origen == services.OrigenCache {
			c.Header("X-Cache", "HIT")
		} else {
			c.Header("X-Cache", "MISS")
		}
		c.JSON(http.StatusOK, o)
	}
}

// GET /api/ofertas/{id}/disponibilidad (HU-05). Nunca se cachea.
func disponibilidad(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		disp, err := d.Ofertas.Disponibilidad(c.Request.Context(), c.Param("id"))
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, disp)
	}
}

// GET /api/ofertas/{id}/guias (HU-14). Con Estudiario caído responde 200 con
// disponible = false: el detalle se ve igual, con el aviso.
func guiasDelDestino(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		g, err := d.Guias.DeLaOferta(c.Request.Context(), c.Param("id"))
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, g)
	}
}

// POST /api/ofertas (HU-09, solo admin).
func crearOferta(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var n models.OfertaNueva
		if !leerJSON(c, &n) {
			return
		}
		o, err := d.Ofertas.Crear(c.Request.Context(), n)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.Header("Location", "/api/ofertas/"+o.ID)
		c.JSON(http.StatusCreated, o)
	}
}

// PUT /api/ofertas/{id} (HU-09, solo admin).
func editarOferta(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var n models.OfertaNueva
		if !leerJSON(c, &n) {
			return
		}
		o, err := d.Ofertas.Actualizar(c.Request.Context(), c.Param("id"), n)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, o)
	}
}

// DELETE /api/ofertas/{id}: baja lógica (HU-09, solo admin).
func despublicarOferta(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := d.Ofertas.Despublicar(c.Request.Context(), c.Param("id")); err != nil {
			responderError(c, d, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// GET /api/packs
func listarPacks(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := d.Packs.Listar(c.Request.Context())
		if err != nil {
			responderError(c, d, err)
			return
		}
		if packs == nil {
			packs = []models.Pack{}
		}
		c.JSON(http.StatusOK, gin.H{"packs": packs})
	}
}

// GET /api/packs/{id} (HU-03)
func obtenerPack(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := d.Packs.Obtener(c.Request.Context(), c.Param("id"))
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, p)
	}
}

// POST /api/packs (HU-10, solo admin).
func crearPack(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var n models.PackNuevo
		if !leerJSON(c, &n) {
			return
		}
		p, err := d.Packs.Crear(c.Request.Context(), n)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.Header("Location", "/api/packs/"+p.ID)
		c.JSON(http.StatusCreated, p)
	}
}
