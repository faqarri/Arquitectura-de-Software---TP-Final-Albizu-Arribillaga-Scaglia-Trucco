package gateway

import (
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type identidad struct {
	ID    string
	Email string
	Rol   string
}

type claims struct {
	Email string `json:"email"`
	Rol   string `json:"rol"`
	jwt.RegisteredClaims
}

func validarToken(authorization, secreto string) (identidad, error) {
	partes := strings.Fields(authorization)
	if len(partes) != 2 || !strings.EqualFold(partes[0], "Bearer") || partes[1] == "" {
		return identidad{}, errors.New("formato de autorización inválido")
	}
	c := &claims{}
	token, err := jwt.ParseWithClaims(partes[1], c, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("algoritmo JWT no permitido")
		}
		return []byte(secreto), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || token == nil || !token.Valid {
		return identidad{}, errors.New("JWT inválido o vencido")
	}
	if c.ExpiresAt == nil || c.Subject == "" || c.Email == "" || (c.Rol != "cliente" && c.Rol != "admin") {
		return identidad{}, errors.New("JWT incompleto o con rol inválido")
	}
	return identidad{ID: c.Subject, Email: c.Email, Rol: c.Rol}, nil
}
