package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agencia/comprobantes/internal/ports"
)

var _ ports.RepositorioConsumidores = (*Repositorio)(nil)

func (r *Repositorio) EmailDelGrupo(ctx context.Context, grupo string) (string, error) {
	var m consumidorModelo
	err := r.db.WithContext(ctx).Where("grupo = ?", grupo).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ports.ErrNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("buscar grupo: %w", err)
	}
	return m.Email, nil
}

// AgregarClave crea el grupo si no existía y le agrega la clave, en una sola
// transacción. El grupo es la clave primaria de consumidores: si dos registros
// del mismo grupo compiten, el segundo encuentra la fila del primero y compara el email.
func (r *Repositorio) AgregarClave(ctx context.Context, grupo, email, hashClave string) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&consumidorModelo{Grupo: grupo, Email: email}).Error; err != nil {
			return err
		}
		var m consumidorModelo
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("grupo = ?", grupo).Take(&m).Error; err != nil {
			return err
		}
		if !strings.EqualFold(m.Email, email) {
			return ports.ErrGrupoRegistrado
		}
		return tx.Create(&apiKeyModelo{HashClave: hashClave, Grupo: grupo}).Error
	})
	if err != nil && !errors.Is(err, ports.ErrGrupoRegistrado) {
		return fmt.Errorf("registrar API Key: %w", err)
	}
	return err
}

func (r *Repositorio) GrupoPorClave(ctx context.Context, hashClave string) (string, error) {
	var m apiKeyModelo
	err := r.db.WithContext(ctx).Where("hash_clave = ?", hashClave).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ports.ErrNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("buscar API Key: %w", err)
	}
	return m.Grupo, nil
}
