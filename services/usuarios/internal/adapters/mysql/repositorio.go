package mysql

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"agencia/usuarios/internal/domain"
)

type Repositorio struct {
	db *gorm.DB
}

func NuevoRepositorio(db *gorm.DB) *Repositorio {
	return &Repositorio{db: db}
}

func (r *Repositorio) Crear(ctx context.Context, usuario *domain.Usuario) error {
	modelo := usuarioModelo{
		ID: usuario.ID, Nombre: usuario.Nombre, Email: usuario.Email, Rol: string(usuario.Rol),
		HashPassword: usuario.HashPassword, CreadoEn: usuario.CreadoEn,
	}
	if err := r.db.WithContext(ctx).Create(&modelo).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.ErrEmailEnUso
		}
		return err
	}
	return nil
}

func (r *Repositorio) PorEmail(ctx context.Context, email string) (*domain.Usuario, error) {
	var modelo usuarioModelo
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&modelo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNoEncontrado
		}
		return nil, err
	}
	return &domain.Usuario{
		ID: modelo.ID, Nombre: modelo.Nombre, Email: modelo.Email, Rol: domain.Rol(modelo.Rol),
		HashPassword: modelo.HashPassword, CreadoEn: modelo.CreadoEn,
	}, nil
}
