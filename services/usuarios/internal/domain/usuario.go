package domain

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type Rol string

const (
	RolCliente Rol = "cliente"
	RolAdmin   Rol = "admin"
)

var (
	ErrDatosInvalidos = errors.New("datos de usuario inválidos")
	ErrEmailEnUso     = errors.New("el email ya está registrado")
	ErrCredenciales   = errors.New("credenciales inválidas")
	ErrNoEncontrado   = errors.New("usuario no encontrado")
)

type ErrorCampo struct {
	Campo   string
	Mensaje string
}

type Usuario struct {
	ID           string
	Nombre       string
	Email        string
	Rol          Rol
	HashPassword string
	CreadoEn     time.Time
}

func NormalizarEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func Validar(nombre, email, password string) []ErrorCampo {
	var errores []ErrorCampo
	nombre = strings.TrimSpace(nombre)
	email = NormalizarEmail(email)
	if utf8.RuneCountInString(nombre) < 2 || utf8.RuneCountInString(nombre) > 100 {
		errores = append(errores, ErrorCampo{Campo: "nombre", Mensaje: "El nombre debe tener entre 2 y 100 caracteres."})
	}
	direccion, err := mail.ParseAddress(email)
	if err != nil || direccion.Address != email || len(email) > 254 {
		errores = append(errores, ErrorCampo{Campo: "email", Mensaje: "Escribí un email válido de hasta 254 caracteres."})
	}
	if len(password) < 8 {
		errores = append(errores, ErrorCampo{Campo: "password", Mensaje: "La contraseña debe tener al menos 8 caracteres."})
	} else if len(password) > 72 {
		errores = append(errores, ErrorCampo{Campo: "password", Mensaje: "La contraseña no puede superar 72 bytes."})
	}
	return errores
}
