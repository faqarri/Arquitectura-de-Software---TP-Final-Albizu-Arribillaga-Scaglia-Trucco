package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"agencia/usuarios/internal/domain"
)

type repositorioEnMemoria struct {
	usuarios map[string]*domain.Usuario
}

func nuevoRepositorioEnMemoria() *repositorioEnMemoria {
	return &repositorioEnMemoria{usuarios: make(map[string]*domain.Usuario)}
}

func (r *repositorioEnMemoria) Crear(_ context.Context, u *domain.Usuario) error {
	if _, ok := r.usuarios[u.Email]; ok {
		return domain.ErrEmailEnUso
	}
	copia := *u
	r.usuarios[u.Email] = &copia
	return nil
}

func (r *repositorioEnMemoria) PorEmail(_ context.Context, email string) (*domain.Usuario, error) {
	u, ok := r.usuarios[email]
	if !ok {
		return nil, domain.ErrNoEncontrado
	}
	copia := *u
	return &copia, nil
}

func TestRegistrarGuardaHashYNoExponePassword(t *testing.T) {
	repo := nuevoRepositorioEnMemoria()
	servicio, err := NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}

	salida, err := servicio.Registrar(context.Background(), "Ana Pérez", " ANA@example.com ", "clave-segura")
	if err != nil {
		t.Fatal(err)
	}
	guardado := repo.usuarios["ana@example.com"]
	if err := bcrypt.CompareHashAndPassword([]byte(guardado.HashPassword), []byte("clave-segura")); err != nil {
		t.Fatalf("no se guardó un hash bcrypt válido: %v", err)
	}
	if salida.Email != "ana@example.com" || salida.Rol != domain.RolCliente || salida.ID == "" {
		t.Fatalf("salida de registro inesperada: %+v", salida)
	}
	respuestaJSON, err := json.Marshal(salida)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(respuestaJSON), "password") || strings.Contains(string(respuestaJSON), "clave-segura") {
		t.Fatalf("la salida no debe incluir contraseña: %s", respuestaJSON)
	}
}

func TestRegistroValidaContraseñaYEmailDuplicado(t *testing.T) {
	repo := nuevoRepositorioEnMemoria()
	servicio, err := NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servicio.Registrar(context.Background(), "Ana", "ana@example.com", "corta"); err == nil {
		t.Fatal("se esperaba rechazar una contraseña corta")
	}
	if _, err := servicio.Registrar(context.Background(), "Ana", "ana@example.com", "clave-segura"); err != nil {
		t.Fatal(err)
	}
	if _, err := servicio.Registrar(context.Background(), "Otra Ana", "ANA@example.com", "otra-clave"); !errors.Is(err, domain.ErrEmailEnUso) {
		t.Fatalf("se esperaba email duplicado, got %v", err)
	}
}

func TestLoginGeneraJWTConClaimsAcordadosYExpiracionOchoHoras(t *testing.T) {
	repo := nuevoRepositorioEnMemoria()
	ahora := time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC)
	servicio, err := NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}
	servicio.ahora = func() time.Time { return ahora }
	if err := servicio.CrearAdmin(context.Background(), "Admin", "admin@example.com", "clave-segura"); err != nil {
		t.Fatal(err)
	}
	token, err := servicio.IniciarSesion(context.Background(), "ADMIN@example.com", "clave-segura")
	if err != nil {
		t.Fatal(err)
	}
	claims := &claimsUsuario{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			t.Fatalf("algoritmo inesperado: %s", token.Method.Alg())
		}
		return []byte("secreto-de-prueba-con-al-menos-32-caracteres"), nil
	}, jwt.WithTimeFunc(func() time.Time { return ahora }))
	if err != nil || !parsed.Valid {
		t.Fatalf("JWT inválido: %v", err)
	}
	if claims.Subject != repo.usuarios["admin@example.com"].ID || claims.Email != "admin@example.com" || claims.Rol != domain.RolAdmin {
		t.Fatalf("claims inesperados: %+v", claims)
	}
	if want := ahora.Add(8 * time.Hour); !claims.ExpiresAt.Time.Equal(want) {
		t.Fatalf("exp = %s, se esperaba %s", claims.ExpiresAt.Time, want)
	}
}

func TestLoginUsaErrorGenericoParaCredencialesInvalidas(t *testing.T) {
	repo := nuevoRepositorioEnMemoria()
	servicio, err := NuevoUsuarios(repo, "secreto-de-prueba-con-al-menos-32-caracteres")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servicio.IniciarSesion(context.Background(), "no-existe@example.com", "clave"); !errors.Is(err, domain.ErrCredenciales) {
		t.Fatalf("se esperaba error genérico de credenciales, got %v", err)
	}
}
