// Package api es el adaptador de entrada HTTP de Reservas. Lo llama el gateway,
// que valida el JWT y completa X-Usuario-Id y X-Usuario-Email (ADR-012); el
// servicio confía en esos headers y no valida el token.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/application"
)

type Dependencias struct {
	Log      *slog.Logger
	Carritos *application.Carritos
	Compras  *application.Compras
	Guias    *application.Guias
	// VerificarSalud devuelve error si una dependencia imprescindible (MySQL) no responde.
	VerificarSalud func(context.Context) error
}

const (
	headerUsuarioID    = "X-Usuario-Id"
	headerUsuarioEmail = "X-Usuario-Email"
	headerIdempotencia = "Idempotency-Key"
	claveUsuario       = "usuario"
	claveEmail         = "email"
)

func NuevoRouter(d Dependencias) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), observabilidad.MiddlewareMetricas(), observabilidad.MiddlewareGin(d.Log))
	r.MaxMultipartMemory = 12 << 20

	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))
	r.GET("/health", salud(d))

	a := r.Group("/api", identificar)
	a.GET("/carrito", verCarrito(d))
	a.POST("/carrito/items", agregarAlCarrito(d))
	a.DELETE("/carrito/items/:itemId", quitarDelCarrito(d))
	a.POST("/compras", comprar(d))
	a.GET("/compras", listarCompras(d))
	a.GET("/compras/:compraId", obtenerCompra(d))
	a.POST("/guias", compartirGuia(d))
	a.GET("/guias", listarGuias(d))

	r.NoRoute(func(c *gin.Context) {
		problema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
	})
	return r
}

// identificar exige la identidad que propaga el gateway.
func identificar(c *gin.Context) {
	usuario := c.GetHeader(headerUsuarioID)
	if usuario == "" {
		problema(c, http.StatusUnauthorized, "no-autenticado", "No autenticado",
			"Falta la identidad del usuario: las solicitudes entran por el gateway con un JWT válido.")
		return
	}
	c.Set(claveUsuario, usuario)
	c.Set(claveEmail, c.GetHeader(headerUsuarioEmail))
	c.Next()
}

func salud(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancelar := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancelar()
		if err := d.VerificarSalud(ctx); err != nil {
			observabilidad.Logger(ctx, d.Log).WarnContext(ctx, "chequeo de salud fallido", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"estado": "no disponible", "detalle": "la base de datos no responde"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"estado": "ok"})
	}
}

// problema responde con formato application/problem+json (RFC 9457). code es
// estable para que el frontend decida qué mostrar.
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
