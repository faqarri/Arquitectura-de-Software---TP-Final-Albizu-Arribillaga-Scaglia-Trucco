package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"agencia/usuarios/internal/domain"
	"agencia/usuarios/internal/ports"
)

const duracionToken = 8 * time.Hour

type UsuarioSalida struct {
	ID     string     `json:"id"`
	Nombre string     `json:"nombre"`
	Email  string     `json:"email"`
	Rol    domain.Rol `json:"rol"`
}

type claimsUsuario struct {
	Email string     `json:"email"`
	Rol   domain.Rol `json:"rol"`
	jwt.RegisteredClaims
}

type Usuarios struct {
	repo       ports.Repositorio
	secretoJWT []byte
	ahora      func() time.Time
}

func NuevoUsuarios(repo ports.Repositorio, secretoJWT string) (*Usuarios, error) {
	if repo == nil {
		return nil, errors.New("el repositorio de usuarios es obligatorio")
	}
	if len(secretoJWT) < 32 {
		return nil, errors.New("JWT_SECRET debe tener al menos 32 caracteres")
	}
	return &Usuarios{repo: repo, secretoJWT: []byte(secretoJWT), ahora: time.Now}, nil
}

func (u *Usuarios) Registrar(ctx context.Context, nombre, email, password string) (UsuarioSalida, error) {
	nombre = strings.TrimSpace(nombre)
	email = domain.NormalizarEmail(email)
	errores := domain.Validar(nombre, email, password)
	if len(errores) != 0 {
		return UsuarioSalida{}, &ErrorValidacion{Campos: errores}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UsuarioSalida{}, fmt.Errorf("generar hash de contraseña: %w", err)
	}
	usuario := &domain.Usuario{
		ID: uuid.NewString(), Nombre: nombre, Email: email, Rol: domain.RolCliente,
		HashPassword: string(hash), CreadoEn: u.ahora().UTC(),
	}
	if err := u.repo.Crear(ctx, usuario); err != nil {
		return UsuarioSalida{}, err
	}
	return aSalida(usuario), nil
}

func (u *Usuarios) IniciarSesion(ctx context.Context, email, password string) (string, error) {
	usuario, err := u.repo.PorEmail(ctx, domain.NormalizarEmail(email))
	if err != nil {
		if errors.Is(err, domain.ErrNoEncontrado) {
			return "", domain.ErrCredenciales
		}
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(usuario.HashPassword), []byte(password)) != nil {
		return "", domain.ErrCredenciales
	}
	ahora := u.ahora().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsUsuario{
		Email: usuario.Email,
		Rol:   usuario.Rol,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   usuario.ID,
			IssuedAt:  jwt.NewNumericDate(ahora),
			ExpiresAt: jwt.NewNumericDate(ahora.Add(duracionToken)),
		},
	})
	firmado, err := token.SignedString(u.secretoJWT)
	if err != nil {
		return "", fmt.Errorf("firmar JWT: %w", err)
	}
	return firmado, nil
}

func (u *Usuarios) CrearAdmin(ctx context.Context, nombre, email, password string) error {
	nombre = strings.TrimSpace(nombre)
	email = domain.NormalizarEmail(email)
	errores := domain.Validar(nombre, email, password)
	if len(errores) != 0 {
		return fmt.Errorf("configuración de administrador inválida: %v", errores)
	}
	existente, err := u.repo.PorEmail(ctx, email)
	if err == nil {
		if existente.Rol != domain.RolAdmin {
			return fmt.Errorf("el email configurado para admin ya pertenece a un cliente")
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNoEncontrado) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("generar hash de contraseña del administrador: %w", err)
	}
	return u.repo.Crear(ctx, &domain.Usuario{
		ID: uuid.NewString(), Nombre: nombre, Email: email, Rol: domain.RolAdmin,
		HashPassword: string(hash), CreadoEn: u.ahora().UTC(),
	})
}

func aSalida(usuario *domain.Usuario) UsuarioSalida {
	return UsuarioSalida{ID: usuario.ID, Nombre: usuario.Nombre, Email: usuario.Email, Rol: usuario.Rol}
}

type ErrorValidacion struct {
	Campos []domain.ErrorCampo
}

func (e *ErrorValidacion) Error() string { return "los datos enviados no son válidos" }
