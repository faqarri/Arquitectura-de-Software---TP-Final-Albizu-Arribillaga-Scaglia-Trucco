package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"agencia/usuarios/internal/application"
	"agencia/usuarios/internal/domain"
)

type solicitudRegistro struct {
	Nombre   string `json:"nombre"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type solicitudSesion struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func registrar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var solicitud solicitudRegistro
		if !decodificarJSON(c, &solicitud) {
			return
		}
		salida, err := d.Usuarios.Registrar(c.Request.Context(), solicitud.Nombre, solicitud.Email, solicitud.Password)
		if err != nil {
			responderError(c, err)
			return
		}
		c.JSON(http.StatusCreated, salida)
	}
}

func iniciarSesion(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var solicitud solicitudSesion
		if !decodificarJSON(c, &solicitud) {
			return
		}
		token, err := d.Usuarios.IniciarSesion(c.Request.Context(), solicitud.Email, solicitud.Password)
		if err != nil {
			responderError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token, "tipo": "Bearer"})
	}
}

func decodificarJSON(c *gin.Context, destino any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		responderProblema(c, http.StatusBadRequest, "solicitud-invalida", "Solicitud inválida",
			"El cuerpo debe ser un JSON válido con los campos requeridos.")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		responderProblema(c, http.StatusBadRequest, "solicitud-invalida", "Solicitud inválida",
			"El cuerpo debe contener un único objeto JSON.")
		return false
	}
	return true
}

func responderError(c *gin.Context, err error) {
	var validacion *application.ErrorValidacion
	if errors.As(err, &validacion) {
		errores := make([]gin.H, 0, len(validacion.Campos))
		for _, campo := range validacion.Campos {
			errores = append(errores, gin.H{"campo": campo.Campo, "mensaje": campo.Mensaje})
		}
		responderProblema(c, http.StatusUnprocessableEntity, "solicitud-invalida", "Revisá los datos", "", gin.H{"errores": errores})
		return
	}
	switch {
	case errors.Is(err, domain.ErrEmailEnUso):
		responderProblema(c, http.StatusConflict, "email-registrado", "Email ya registrado", "Ya existe una cuenta con ese email.")
	case errors.Is(err, domain.ErrCredenciales):
		responderProblema(c, http.StatusUnauthorized, "credenciales-invalidas", "No pudimos ingresar", "El email o la contraseña no son correctos.")
	default:
		responderProblema(c, http.StatusInternalServerError, "error-interno", "Error interno", "No se pudo completar la solicitud.")
	}
}
