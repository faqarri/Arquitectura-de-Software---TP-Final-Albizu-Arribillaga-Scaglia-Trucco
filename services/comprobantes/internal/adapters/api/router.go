// Package api es el adaptador de entrada HTTP: la API pública v1 de Comprobantes
// (contrato en docs/contracts/comprobantes/v1/openapi.yaml).
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/comprobantes/internal/application"
	"agencia/pkg/observabilidad"
)

// Dependencias que necesita la API.
type Dependencias struct {
	Log     *slog.Logger
	Emitir  *application.EmitirComprobante
	Obtener *application.ObtenerComprobante
	// APIKeys asocia cada API Key con el nombre del grupo consumidor.
	APIKeys map[string]string
	// VerificarSalud devuelve error si una dependencia imprescindible no responde.
	VerificarSalud func(context.Context) error
}

func NuevoRouter(d Dependencias) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), observabilidad.MiddlewareGin(d.Log))

	// Sin autenticación: lo usan Docker y el balanceador para saber si la instancia está viva.
	r.GET("/health", salud(d.VerificarSalud))

	v1 := r.Group("/v1", autenticar(d.APIKeys), limitarSolicitudes())
	v1.POST("/comprobantes", emitir(d))
	v1.GET("/comprobantes/:comprobanteId", obtener(d))
	return r
}

func salud(verificar func(context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := verificar(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"estado": "no disponible", "detalle": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"estado": "ok"})
	}
}

// ParsearAPIKeys lee COMPROBANTES_API_KEYS con el formato "grupo:clave,grupo:clave"
// y devuelve un mapa clave → grupo.
func ParsearAPIKeys(valor string) (map[string]string, error) {
	claves := map[string]string{}
	for _, par := range strings.Split(valor, ",") {
		par = strings.TrimSpace(par)
		if par == "" {
			continue
		}
		grupo, clave, ok := strings.Cut(par, ":")
		if !ok || grupo == "" || clave == "" {
			return nil, fmt.Errorf("COMPROBANTES_API_KEYS: %q no tiene el formato grupo:clave", par)
		}
		claves[clave] = grupo
	}
	return claves, nil
}
