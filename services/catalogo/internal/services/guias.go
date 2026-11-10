package services

import (
	"context"
	"log/slog"

	"agencia/catalogo/internal/repository"
	"agencia/pkg/estudiario"
)

// tamanioGuias es cuántas guías mejor calificadas se muestran en el detalle.
const tamanioGuias = 5

// buscadorGuias es lo que se usa de *estudiario.Cliente (permite un doble en tests).
type buscadorGuias interface {
	Buscar(ctx context.Context, b estudiario.Busqueda) (estudiario.Pagina, error)
}

// GuiaDestino es una guía tal como la muestra el detalle de la oferta (HU-14).
type GuiaDestino struct {
	ID                     string   `json:"id"`
	Titulo                 string   `json:"titulo"`
	Descripcion            string   `json:"descripcion,omitempty"`
	Autor                  string   `json:"autor"`
	Etiquetas              []string `json:"etiquetas"`
	Calificacion           *float64 `json:"calificacion"`
	CantidadCalificaciones int      `json:"cantidadCalificaciones"`
	// URLDescarga es el enlace público al PDF; no está cuando se usa el mock.
	URLDescarga string `json:"urlDescarga,omitempty"`
}

type GuiasDestino struct {
	Disponible bool          `json:"disponible"`
	Guias      []GuiaDestino `json:"guias"`
}

// Guias consulta a Estudiario (capacidad del Grupo 2, ADR-009) desde el
// microservicio, nunca desde el frontend.
type Guias struct {
	ofertas *Ofertas
	cli     buscadorGuias
	cache   *repository.Cache
	log     *slog.Logger
}

func NuevoGuias(ofertas *Ofertas, cli buscadorGuias, cache *repository.Cache, log *slog.Logger) *Guias {
	return &Guias{ofertas: ofertas, cli: cli, cache: cache, log: log}
}

// DeLaOferta devuelve las guías mejor calificadas del destino de la oferta.
// Se cachean 5 minutos por destino para no gastar el cupo de Estudiario (60
// por minuto). Si Estudiario falla o su circuito está abierto, responde
// disponible = false: el detalle nunca falla por el proveedor.
func (s *Guias) DeLaOferta(ctx context.Context, ofertaID string) (GuiasDestino, error) {
	o, _, err := s.ofertas.Detalle(ctx, ofertaID, false)
	if err != nil {
		return GuiasDestino{}, err
	}
	var res GuiasDestino
	if s.cache.ObtenerGuias(ctx, o.Destino, &res) {
		return res, nil
	}
	pagina, err := s.cli.Buscar(ctx, estudiario.Busqueda{
		Destino: o.Destino, Orden: estudiario.OrdenCalificacion, Tamanio: tamanioGuias,
	})
	if err != nil {
		s.log.WarnContext(ctx, "guías no disponibles: Estudiario no respondió", "destino", o.Destino, "error", err)
		return GuiasDestino{Disponible: false, Guias: []GuiaDestino{}}, nil
	}
	res = GuiasDestino{Disponible: true, Guias: make([]GuiaDestino, 0, len(pagina.Guias))}
	for _, g := range pagina.Guias {
		etiquetas := g.Etiquetas
		if etiquetas == nil {
			etiquetas = []string{}
		}
		res.Guias = append(res.Guias, GuiaDestino{
			ID: g.ID, Titulo: g.Titulo, Descripcion: g.Descripcion, Autor: g.Autor, Etiquetas: etiquetas,
			Calificacion: g.Calificacion, CantidadCalificaciones: g.CantidadCalificaciones, URLDescarga: g.URLDescarga,
		})
	}
	s.cache.GuardarGuias(ctx, o.Destino, res)
	return res, nil
}
