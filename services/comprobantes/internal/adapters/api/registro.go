package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

const (
	// RegistrosPorHora es el límite de POST /v1/api-keys por IP.
	RegistrosPorHora = 5

	// timeoutRegistro incluye el envío del correo (hasta 10 s) además de la base.
	timeoutRegistro = 15 * time.Second
)

// solicitudRegistro es el cuerpo de POST /v1/api-keys.
type solicitudRegistro struct {
	Grupo *string `json:"grupo"`
	Email *string `json:"email"`
}

// registrar atiende POST /v1/api-keys: genera una API Key y la envía al email.
func registrar(d Dependencias) gin.HandlerFunc {
	limite := nuevoLimitePorIP(RegistrosPorHora, time.Hour)
	return func(c *gin.Context) {
		dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxCuerpo))
		dec.DisallowUnknownFields()
		var s solicitudRegistro
		if err := dec.Decode(&s); err != nil {
			problemaValidacion(c, []errorCampo{errorDeDecodificacion(err)})
			return
		}
		var errores []errorCampo
		switch {
		case s.Grupo == nil:
			errores = append(errores, errorCampo{Campo: "grupo", Mensaje: "es obligatorio"})
		case domain.ValidarGrupo(*s.Grupo) != nil:
			errores = append(errores, errorCampo{Campo: "grupo", Mensaje: domain.ErrGrupoInvalido.Error()})
		}
		switch {
		case s.Email == nil:
			errores = append(errores, errorCampo{Campo: "email", Mensaje: "es obligatorio"})
		case !emailValido(*s.Email) || len(*s.Email) > maxEmail:
			errores = append(errores, errorCampo{Campo: "email", Mensaje: "no es una dirección de correo válida"})
		}
		if errores != nil {
			problemaValidacion(c, errores)
			return
		}
		// Solo cuentan para el límite los registros válidos, que son los que mandan correo.
		if !limite.permitir(c) {
			return
		}

		ctx, cancelar := context.WithTimeout(c.Request.Context(), timeoutRegistro)
		defer cancelar()
		err := d.Registrar.Ejecutar(ctx, application.SolicitudRegistro{Grupo: *s.Grupo, Email: *s.Email})
		switch {
		case err == nil:
		case errors.Is(err, domain.ErrGrupoReservado):
			problemaValidacion(c, []errorCampo{{Campo: "grupo", Mensaje: err.Error()}})
			return
		case errors.Is(err, ports.ErrGrupoRegistrado):
			problema(c, http.StatusConflict, "grupo-registrado", "Grupo ya registrado",
				"Ese grupo ya está registrado con otro email. Usá el email con el que lo registraste o elegí otro nombre de grupo.")
			return
		default:
			ctxLog := c.Request.Context()
			c.Header("Retry-After", strconv.Itoa(reintentarEnSegundos))
			observabilidad.Logger(ctxLog, d.Log).ErrorContext(ctxLog, "no se pudo registrar la API Key", "grupo", *s.Grupo, "error", err)
			problema(c, http.StatusServiceUnavailable, "no-disponible", "Servicio no disponible",
				"No pudimos generar o enviar la API Key. Reintentá en unos minutos.")
			return
		}

		c.JSON(http.StatusAccepted, gin.H{
			"grupo":   *s.Grupo,
			"email":   *s.Email,
			"mensaje": "Te enviamos la API Key a " + *s.Email + ". Si no llega en unos minutos, revisá la carpeta de spam.",
		})
	}
}

// limitePorIP permite hasta n solicitudes por período por IP de origen.
type limitePorIP struct {
	n           int
	periodo     time.Duration
	mu          sync.Mutex
	limitadores map[string]*rate.Limiter
}

func nuevoLimitePorIP(n int, periodo time.Duration) *limitePorIP {
	return &limitePorIP{n: n, periodo: periodo, limitadores: map[string]*rate.Limiter{}}
}

// permitir consume un lugar para la IP de c. Si no queda, responde 429 y
// devuelve false.
func (l *limitePorIP) permitir(c *gin.Context) bool {
	l.mu.Lock()
	lim, ok := l.limitadores[c.ClientIP()]
	if !ok {
		lim = rate.NewLimiter(rate.Every(l.periodo/time.Duration(l.n)), l.n)
		l.limitadores[c.ClientIP()] = lim
	}
	l.mu.Unlock()

	ahora := time.Now()
	reserva := lim.ReserveN(ahora, 1)
	if espera := reserva.DelayFrom(ahora); espera > 0 {
		reserva.CancelAt(ahora)
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(espera.Seconds()))))
		problema(c, http.StatusTooManyRequests, "demasiadas-solicitudes", "Demasiadas solicitudes",
			"Se superó el límite de "+strconv.Itoa(l.n)+" registros por hora.")
		return false
	}
	return true
}
