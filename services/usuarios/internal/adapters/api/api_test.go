package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"agencia/usuarios/internal/application"
	"agencia/usuarios/internal/domain"
)

type repositorioPrueba struct {
	usuarios map[string]*domain.Usuario
}

func (r *repositorioPrueba) Crear(_ context.Context, u *domain.Usuario) error {
	if r.usuarios[u.Email] != nil {
		return domain.ErrEmailEnUso
	}
	copia := *u
	r.usuarios[u.Email] = &copia
	return nil
}

func (r *repositorioPrueba) PorEmail(_ context.Context, email string) (*domain.Usuario, error) {
	u := r.usuarios[email]
	if u == nil {
		return nil, domain.ErrNoEncontrado
	}
	copia := *u
	return &copia, nil
}

func TestRegistroYLoginHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositorioPrueba{usuarios: make(map[string]*domain.Usuario)}
	servicio, err := application.NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}
	router := NuevoRouter(Dependencias{
		Usuarios: servicio,
		Log:      slog.Default(),
		VerificarSalud: func(context.Context) error {
			return nil
		},
	})
	registro := httptest.NewRecorder()
	router.ServeHTTP(registro, httptest.NewRequest(http.MethodPost, "/api/usuarios",
		strings.NewReader(`{"nombre":"Ana Pérez","email":"ana@example.com","password":"clave-segura"}`)))
	if registro.Code != http.StatusCreated {
		t.Fatalf("registro status = %d, body = %s", registro.Code, registro.Body.String())
	}
	var usuario map[string]any
	if err := json.Unmarshal(registro.Body.Bytes(), &usuario); err != nil {
		t.Fatal(err)
	}
	if _, ok := usuario["password"]; ok {
		t.Fatal("la respuesta no debe incluir contraseña")
	}

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/sesiones",
		strings.NewReader(`{"email":"ana@example.com","password":"clave-segura"}`)))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
}

func TestRegistroDuplicadoYLoginInvalido(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositorioPrueba{usuarios: make(map[string]*domain.Usuario)}
	servicio, err := application.NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}
	router := NuevoRouter(Dependencias{Usuarios: servicio, Log: slog.Default()})
	cuerpo := `{"nombre":"Ana","email":"ana@example.com","password":"clave-segura"}`
	for i := 0; i < 2; i++ {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/usuarios", strings.NewReader(cuerpo)))
		if i == 0 && res.Code != http.StatusCreated || i == 1 && res.Code != http.StatusConflict {
			t.Fatalf("intento %d: status = %d, body = %s", i, res.Code, res.Body.String())
		}
	}
	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/sesiones",
		strings.NewReader(`{"email":"ana@example.com","password":"incorrecta"}`)))
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), "credenciales-invalidas") {
		t.Fatalf("login inválido: status = %d, body = %s", login.Code, login.Body.String())
	}
}
