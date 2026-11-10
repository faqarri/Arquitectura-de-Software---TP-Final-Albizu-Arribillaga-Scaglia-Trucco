package ports

import (
	"context"

	"agencia/usuarios/internal/domain"
)

type Repositorio interface {
	Crear(context.Context, *domain.Usuario) error
	PorEmail(context.Context, string) (*domain.Usuario, error)
}
