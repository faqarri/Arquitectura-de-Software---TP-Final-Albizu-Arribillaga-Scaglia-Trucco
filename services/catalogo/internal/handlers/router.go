// Package handlers es la capa HTTP de Catálogo (Gin): la API pública, que el
// frontend usa a través del gateway, y la API interna /internal, que usa
// Reservas y no se publica en el gateway (ADR-014). Solo llama a services.
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/catalogo/internal/services"
	"agencia/pkg/observabilidad"
)

type Dependencias struct {
	Log         *slog.Logger
	Instancia   string
	Ofertas     *services.Ofertas
	Packs       *services.Packs
	Retenciones *services.Retenciones
	Guias       *services.Guias
	// VerificarSalud devuelve error si MySQL o MongoDB no responden. Redis no
	// cuenta: una caída de la caché no saca la instancia del balanceo (ADR-007).
	VerificarSalud func(context.Context) error
}

const (
	headerUsuarioRol   = "X-Usuario-Rol"
	headerIdempotencia = "Idempotency-Key"
	// HeaderInstancia identifica qué instancia respondió: es la evidencia más
	// directa del balanceo (ADR-012), junto con las métricas por instancia.
	HeaderInstancia = "X-Instancia"
)

func NuevoRouter(d Dependencias) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), func(c *gin.Context) {
		c.Header(HeaderInstancia, d.Instancia)
		c.Next()
	}, observabilidad.MiddlewareMetricas(), observabilidad.MiddlewareGin(d.Log))

	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))
	r.GET("/health", salud(d))

	api := r.Group("/api")
	api.GET("/ofertas/:id", detalleOferta(d))
	api.GET("/ofertas/:id/disponibilidad", disponibilidad(d))
	api.GET("/ofertas/:id/guias", guiasDelDestino(d))
	api.POST("/ofertas", soloAdmin, crearOferta(d))
	api.PUT("/ofertas/:id", soloAdmin, editarOferta(d))
	api.DELETE("/ofertas/:id", soloAdmin, despublicarOferta(d))
	api.GET("/packs", listarPacks(d))
	api.GET("/packs/:id", obtenerPack(d))
	api.POST("/packs", soloAdmin, crearPack(d))

	in := r.Group("/internal")
	in.POST("/retenciones", retener(d))
	in.DELETE("/retenciones/:id", liberar(d))
	in.POST("/retenciones/confirmacion", confirmar(d))

	r.NoRoute(func(c *gin.Context) {
		problema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
	})
	return r
}

func salud(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancelar := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancelar()
		if err := d.VerificarSalud(ctx); err != nil {
			observabilidad.Logger(ctx, d.Log).WarnContext(ctx, "chequeo de salud fallido", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"estado": "no disponible", "instancia": d.Instancia, "detalle": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"estado": "ok", "instancia": d.Instancia})
	}
}

// soloAdmin exige el rol que propaga el gateway desde el JWT (ADR-012). El
// servicio confía en ese header: el gateway borra el que mande el cliente.
func soloAdmin(c *gin.Context) {
	if c.GetHeader(headerUsuarioRol) != "admin" {
		problema(c, http.StatusForbidden, "prohibido", "Acceso restringido", "Solo un administrador puede modificar el catálogo.")
		return
	}
	c.Next()
}

func esAdmin(c *gin.Context) bool { return c.GetHeader(headerUsuarioRol) == "admin" }
