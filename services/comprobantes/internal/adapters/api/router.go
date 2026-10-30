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
	// Autenticar resuelve el grupo consumidor de cada X-API-Key.
	Autenticar *application.AutenticarConsumidor
	// Registrar atiende POST /v1/api-keys. Si es nil, la ruta no se registra.
	Registrar *application.RegistrarConsumidor
	// VerificarSalud devuelve error si una dependencia imprescindible no responde.
	VerificarSalud func(context.Context) error
	// TimeoutDependencias limita cuánto espera cada solicitud a la base (ADR-010).
	// Si es cero se usa TimeoutPorDefecto.
	TimeoutDependencias time.Duration
	// HabilitarErrorPrueba registra GET /error solo en entornos que lo habilitan explícitamente.
	HabilitarErrorPrueba bool
}

// TimeoutPorDefecto es el tiempo máximo que una solicitud espera a MySQL antes
// de responder 503 (ADR-010).
const TimeoutPorDefecto = 2 * time.Second

func NuevoRouter(d Dependencias) *gin.Engine {
	if d.TimeoutDependencias == 0 {
		d.TimeoutDependencias = TimeoutPorDefecto
	}
	r := gin.New()
	r.Use(gin.Recovery(), observabilidad.MiddlewareMetricas(), observabilidad.MiddlewareGin(d.Log))

	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))
	// Sin autenticación: lo usan Docker y el balanceador para saber si la instancia está viva.
	r.GET("/health", salud(d.Log, d.VerificarSalud))
	if d.HabilitarErrorPrueba {
		r.GET("/error", func(c *gin.Context) {
			ctx := c.Request.Context()
			observabilidad.Logger(ctx, d.Log).ErrorContext(ctx, "error de prueba simulado")
			problema(c, http.StatusInternalServerError, "error-simulado",
				"Error de prueba", "Se generó un error de prueba para validar la observabilidad.")
		})
	}

	// Sin API Key: es como se obtiene una. Se limita por IP para que no sirva
	// para mandar correos en masa.
	if d.Registrar != nil {
		r.POST("/v1/api-keys", registrar(d))
	}

	v1 := r.Group("/v1", autenticar(d), limitarSolicitudes(SolicitudesPorMinuto))
	v1.POST("/comprobantes", emitir(d))
	v1.GET("/comprobantes/:comprobanteId", obtener(d))

	r.NoRoute(func(c *gin.Context) {
		problema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado",
			"La ruta no existe. Las operaciones disponibles están en docs/contracts/README.md.")
	})
	return r
}

// salud no muestra el error interno: el endpoint es público y el detalle revelaría
// nombres de la infraestructura. El detalle queda en el log.
func salud(log *slog.Logger, verificar func(context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := verificar(ctx); err != nil {
			observabilidad.Logger(ctx, log).WarnContext(ctx, "chequeo de salud fallido", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"estado": "no disponible", "detalle": "la base de datos no responde"})
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
