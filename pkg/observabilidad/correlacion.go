package observabilidad

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// HeaderCorrelacion viaja en las solicitudes HTTP y en los headers de los mensajes
// de RabbitMQ para seguir una operación entre servicios.
const HeaderCorrelacion = "X-Correlation-Id"

type claveCorrelacion struct{}

// ConCorrelacion guarda el id de correlación en el contexto.
func ConCorrelacion(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, claveCorrelacion{}, id)
}

// Correlacion devuelve el id de correlación del contexto, o "" si no hay.
func Correlacion(ctx context.Context) string {
	id, _ := ctx.Value(claveCorrelacion{}).(string)
	return id
}

// rutaSalud no se registra en los logs: Docker y el balanceador la consultan
// cada pocos segundos y taparían el resto.
const rutaSalud = "/health"

// MiddlewareGin toma el X-Correlation-Id de la solicitud (o genera uno), lo
// devuelve en la respuesta, lo deja en el contexto y registra un log por solicitud.
func MiddlewareGin(log *slog.Logger) gin.HandlerFunc {
	middlewareTrazas := otelgin.Middleware("agencia/pkg/observabilidad")
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderCorrelacion)
		if id == "" {
			id = uuid.NewString()
		}
		c.Header(HeaderCorrelacion, id)
		ctx := ConCorrelacion(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)

		inicio := time.Now()
		middlewareTrazas(c)
		ctx = c.Request.Context()

		if c.FullPath() == rutaSalud {
			return
		}
		Logger(ctx, log).InfoContext(ctx, "solicitud http",
			"metodo", c.Request.Method,
			"ruta", c.FullPath(),
			"estado", c.Writer.Status(),
			"duracion_ms", time.Since(inicio).Milliseconds(),
		)
	}
}
