package services

import (
	"context"

	"github.com/google/uuid"

	"agencia/catalogo/internal/models"
	"agencia/catalogo/internal/repository"
)

// maxPacksListados acota GET /api/packs (alcance mínimo de packs, plan §3.2).
const maxPacksListados = 50

type Packs struct {
	packs   *repository.Packs
	ofertas *repository.Ofertas
}

func NuevoPacks(packs *repository.Packs, ofertas *repository.Ofertas) *Packs {
	return &Packs{packs: packs, ofertas: ofertas}
}

// Crear da de alta un pack (HU-10). Sus opciones deben existir, estar
// publicadas y cubrir los tres tipos, así el cliente siempre puede armar uno válido.
func (s *Packs) Crear(ctx context.Context, n models.PackNuevo) (models.Pack, error) {
	if err := n.Validar(); err != nil {
		return models.Pack{}, err
	}
	encontradas, err := s.publicadas(ctx, n.Opciones)
	if err != nil {
		return models.Pack{}, err
	}
	if err := models.ValidarOpcionesDelPack(n.Opciones, encontradas); err != nil {
		return models.Pack{}, err
	}
	p := models.Pack{
		ID: "pk-" + uuid.NewString(), Titulo: n.Titulo, Destino: n.Destino, Descripcion: n.Descripcion,
		DescuentoPorcentaje: n.DescuentoPorcentaje, OpcionIDs: n.Opciones,
	}
	if err := s.packs.Crear(ctx, p); err != nil {
		return models.Pack{}, err
	}
	p.Opciones = ordenadas(p.OpcionIDs, encontradas)
	return p, nil
}

// Obtener devuelve el pack con sus opciones publicadas.
func (s *Packs) Obtener(ctx context.Context, id string) (models.Pack, error) {
	p, err := s.packs.Obtener(ctx, id)
	if err != nil {
		return models.Pack{}, err
	}
	encontradas, err := s.publicadas(ctx, p.OpcionIDs)
	if err != nil {
		return models.Pack{}, err
	}
	p.Opciones = ordenadas(p.OpcionIDs, encontradas)
	return p, nil
}

func (s *Packs) Listar(ctx context.Context) ([]models.Pack, error) {
	packs, err := s.packs.Listar(ctx, maxPacksListados)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, p := range packs {
		ids = append(ids, p.OpcionIDs...)
	}
	encontradas, err := s.publicadas(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range packs {
		packs[i].Opciones = ordenadas(packs[i].OpcionIDs, encontradas)
	}
	return packs, nil
}

func (s *Packs) publicadas(ctx context.Context, ids []string) (map[string]models.Oferta, error) {
	todas, err := s.ofertas.ObtenerVarias(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, o := range todas {
		if !o.Publicada {
			delete(todas, id)
		}
	}
	return todas, nil
}

func ordenadas(ids []string, ofertas map[string]models.Oferta) []models.Oferta {
	res := make([]models.Oferta, 0, len(ids))
	for _, id := range ids {
		if o, ok := ofertas[id]; ok {
			res = append(res, o)
		}
	}
	return res
}
