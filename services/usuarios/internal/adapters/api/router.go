package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/pkg/observabilidad"
	"agencia/usuarios/internal/application"
)

type Dependencias struct {
	Log            *slog.Logger
	Usuarios       *application.Usuarios
	VerificarSalud func(context.Context) error
}

func NuevoRouter(d Dependencias) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), observabilidad.MiddlewareMetricas(), observabilidad.MiddlewareGin(d.Log))
	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))
	r.GET("/health", func(c *gin.Context) {
		ctx, cancelar := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancelar()
		if err := d.VerificarSalud(ctx); err != nil {
			observabilidad.Logger(ctx, d.Log).WarnContext(ctx, "chequeo de salud fallido", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"estado": "no disponible", "detalle": "la base de datos no responde"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"estado": "ok"})
	})

	a := r.Group("/api")
	a.POST("/usuarios", registrar(d))
	a.POST("/sesiones", iniciarSesion(d))
	r.NoRoute(func(c *gin.Context) {
		responderProblema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
	})
	return r
}

func responderProblema(c *gin.Context, estado int, codigo, titulo, detalle string, extra ...gin.H) {
	cuerpo := gin.H{
		"type":   "https://agencia-viajes.example/errores/" + codigo,
		"title":  titulo,
		"status": estado,
		"code":   codigo,
	}
	if detalle != "" {
		cuerpo["detail"] = detalle
	}
	for _, campos := range extra {
		for clave, valor := range campos {
			cuerpo[clave] = valor
		}
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(estado, cuerpo)
}
