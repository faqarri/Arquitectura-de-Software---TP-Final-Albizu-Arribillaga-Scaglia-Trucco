package application

import (
	"context"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// ObtenerComprobante devuelve un comprobante solo si pertenece al consumidor.
type ObtenerComprobante struct {
	repo ports.RepositorioComprobantes
}

func NuevoObtenerComprobante(repo ports.RepositorioComprobantes) *ObtenerComprobante {
	return &ObtenerComprobante{repo: repo}
}

func (uc *ObtenerComprobante) Ejecutar(ctx context.Context, consumidor, id string) (*domain.Comprobante, error) {
	return uc.repo.BuscarPorID(ctx, consumidor, id)
}
