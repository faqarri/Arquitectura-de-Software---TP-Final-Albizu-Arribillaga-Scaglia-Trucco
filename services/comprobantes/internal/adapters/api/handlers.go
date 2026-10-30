package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

const (
	headerAPIKey         = "X-API-Key"
	headerIdempotencyKey = "Idempotency-Key"

	// SolicitudesPorMinuto es el límite por consumidor publicado en el contrato.
	SolicitudesPorMinuto = 60

	// reintentarEnSegundos se informa en Retry-After cuando una dependencia falla.
	reintentarEnSegundos = 5
)

// claveConsumidor es donde autenticar deja el nombre del grupo consumidor en el
// contexto de Gin, para que los handlers lo pasen a los casos de uso.
const claveConsumidor = "consumidor"

// autenticar identifica al grupo consumidor por su X-API-Key.
func autenticar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancelar := context.WithTimeout(c.Request.Context(), d.TimeoutDependencias)
		defer cancelar()
		grupo, err := d.Autenticar.Ejecutar(ctx, c.GetHeader(headerAPIKey))
		switch {
		case errors.Is(err, application.ErrClaveInvalida):
			problema(c, http.StatusUnauthorized, "no-autenticado", "No autenticado",
				"Falta el header X-API-Key o la clave no es válida. Para obtener una: POST /v1/api-keys.")
			return
		case err != nil:
			noDisponible(c, d, err)
			return
		}
		c.Set(claveConsumidor, grupo)
		c.Next()
	}
}

// limitarSolicitudes aplica un límite por consumidor con un token bucket en
// memoria. El límite es por instancia (ver limitaciones en ARCHITECTURE.md).
func limitarSolicitudes(porMinuto int) gin.HandlerFunc {
	var mu sync.Mutex
	limitadores := map[string]*rate.Limiter{}
	limitador := func(consumidor string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		l, ok := limitadores[consumidor]
		if !ok {
			l = rate.NewLimiter(rate.Every(time.Minute/time.Duration(porMinuto)), porMinuto)
			limitadores[consumidor] = l
		}
		return l
	}

	return func(c *gin.Context) {
		ahora := time.Now()
		reserva := limitador(c.GetString(claveConsumidor)).ReserveN(ahora, 1)
		if espera := reserva.DelayFrom(ahora); espera > 0 {
			reserva.CancelAt(ahora)
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(espera.Seconds()))))
			problema(c, http.StatusTooManyRequests, "demasiadas-solicitudes", "Demasiadas solicitudes",
				"Se superó el límite de "+strconv.Itoa(porMinuto)+" solicitudes por minuto.")
			return
		}
		c.Next()
	}
}

// emitir atiende POST /v1/comprobantes.
func emitir(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		clave := c.GetHeader(headerIdempotencyKey)
		if n := utf8.RuneCountInString(clave); n < minIdempotencyKey || n > maxIdempotencyKey {
			problemaValidacion(c, []errorCampo{{
				Campo:   headerIdempotencyKey,
				Mensaje: "es obligatorio y debe tener entre 8 y 64 caracteres",
			}})
			return
		}

		email, items, errores := decodificarSolicitud(http.MaxBytesReader(c.Writer, c.Request.Body, maxCuerpo))
		if errores != nil {
			problemaValidacion(c, errores)
			return
		}

		ctx, cancelar := context.WithTimeout(c.Request.Context(), d.TimeoutDependencias)
		defer cancelar()
		res, err := d.Emitir.Ejecutar(ctx, application.SolicitudEmision{
			Consumidor:     c.GetString(claveConsumidor),
			IdempotencyKey: clave,
			Email:          email,
			Items:          items,
		})
		switch {
		case err == nil:
		case errors.Is(err, application.ErrConflictoIdempotencia):
			problema(c, http.StatusConflict, "conflicto-idempotencia", "Clave de idempotencia reutilizada",
				"La Idempotency-Key ya se usó con un cuerpo distinto. Generá una clave nueva.")
			return
		case errors.Is(err, domain.ErrTotalCero):
			problema(c, http.StatusUnprocessableEntity, "regla-incumplida", "Regla de negocio incumplida",
				"El total del comprobante debe ser mayor a cero.")
			return
		case errors.Is(err, domain.ErrImporteFueraDeRango):
			problema(c, http.StatusUnprocessableEntity, "regla-incumplida", "Regla de negocio incumplida",
				"El precio de cada ítem y el total no pueden superar 9999999999.99.")
			return
		case errors.Is(err, domain.ErrSinItems), errors.Is(err, domain.ErrPrecioNegativo):
			problemaValidacion(c, []errorCampo{{Campo: "items", Mensaje: err.Error()}})
			return
		default:
			noDisponible(c, d, err)
			return
		}

		estado := http.StatusCreated
		if res.YaExistia {
			estado = http.StatusOK
		} else {
			c.Header("Location", "/v1/comprobantes/"+res.Comprobante.ID)
		}
		c.JSON(estado, aSalida(res.Comprobante))
	}
}

// obtener atiende GET /v1/comprobantes/{comprobanteId}.
func obtener(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancelar := context.WithTimeout(c.Request.Context(), d.TimeoutDependencias)
		defer cancelar()
		comp, err := d.Obtener.Ejecutar(ctx, c.GetString(claveConsumidor), c.Param("comprobanteId"))
		switch {
		case err == nil:
			c.JSON(http.StatusOK, aSalida(comp))
		case errors.Is(err, ports.ErrNoEncontrado):
			problema(c, http.StatusNotFound, "no-encontrado", "Comprobante no encontrado",
				"No existe un comprobante con ese id para tu API Key.")
		default:
			noDisponible(c, d, err)
		}
	}
}

// noDisponible responde 503 ante una falla de una dependencia (por ejemplo,
// MySQL). El consumidor puede reintentar con la misma Idempotency-Key.
func noDisponible(c *gin.Context, d Dependencias, err error) {
	ctx := c.Request.Context()
	observabilidad.Logger(ctx, d.Log).ErrorContext(ctx, "no se pudo atender la solicitud", "error", err)
	c.Header("Retry-After", strconv.Itoa(reintentarEnSegundos))
	problema(c, http.StatusServiceUnavailable, "no-disponible", "Servicio no disponible",
		"Reintentá en unos segundos con la misma Idempotency-Key; no se va a duplicar el comprobante.")
}

// problemaValidacion responde 400 con la lista de errores por campo.
func problemaValidacion(c *gin.Context, errores []errorCampo) {
	responderProblema(c, http.StatusBadRequest, "solicitud-invalida", "Solicitud inválida", "", errores)
}

// problema responde un error con formato application/problem+json (RFC 9457).
func problema(c *gin.Context, estado int, tipo, titulo, detalle string) {
	responderProblema(c, estado, tipo, titulo, detalle, nil)
}

func responderProblema(c *gin.Context, estado int, tipo, titulo, detalle string, errores []errorCampo) {
	cuerpo := gin.H{
		"type":   "https://agencia-viajes.example/errores/" + tipo,
		"title":  titulo,
		"status": estado,
	}
	if detalle != "" {
		cuerpo["detail"] = detalle
	}
	if len(errores) > 0 {
		cuerpo["errores"] = errores
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(estado, cuerpo)
}
