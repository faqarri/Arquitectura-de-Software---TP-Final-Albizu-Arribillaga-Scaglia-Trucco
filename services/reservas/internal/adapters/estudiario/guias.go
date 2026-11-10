// Package estudiario implementa ports.GuiasExternas con el cliente compartido
// pkg/estudiario (capacidad del Grupo 2, ADR-009).
package estudiario

import (
	"context"
	"errors"
	"sort"
	"strings"

	"agencia/pkg/estudiario"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// maxPaginasModeracion acota cuántas páginas de 50 se leen por ciclo.
const maxPaginasModeracion = 10

// cliente es lo que se usa de *estudiario.Cliente.
type cliente interface {
	Registrar(ctx context.Context, r estudiario.Registro) (estudiario.Guia, error)
	Registradas(ctx context.Context, estado estudiario.EstadoGuia, pagina, tamanio int) (estudiario.Pagina, error)
}

type Guias struct {
	cli cliente
}

var _ ports.GuiasExternas = (*Guias)(nil)

func NuevoGuias(cli *estudiario.Cliente) *Guias { return &Guias{cli: cli} }

func (a *Guias) Registrar(ctx context.Context, g domain.Guia, pdf []byte) (string, error) {
	doc, err := a.cli.Registrar(ctx, estudiario.Registro{
		IdempotencyKey: g.IdempotencyKey,
		Titulo:         g.Titulo,
		Descripcion:    g.Descripcion,
		Destino:        g.Destino,
		Etiquetas:      g.Etiquetas,
		Autor:          g.Autor,
		AutorExternoID: g.UsuarioID,
		NombreArchivo:  "guia-" + g.ID + ".pdf",
		PDF:            pdf,
	})
	if err != nil {
		return "", traducir(err)
	}
	return doc.ID, nil
}

// Moderacion lee todas nuestras guías registradas (de a 50) y devuelve las
// ya moderadas. Si falla a mitad de camino, devuelve lo leído hasta ahí.
func (a *Guias) Moderacion(ctx context.Context) ([]ports.EstadoModeracion, error) {
	var res []ports.EstadoModeracion
	for pagina := 1; pagina <= maxPaginasModeracion; pagina++ {
		p, err := a.cli.Registradas(ctx, "", pagina, 50)
		if err != nil {
			if len(res) > 0 {
				return res, nil
			}
			return nil, traducir(err)
		}
		for _, g := range p.Guias {
			res = append(res, ports.EstadoModeracion{
				DocumentoID: g.ID,
				Publicada:   g.Estado == estudiario.EstadoPublicada,
				Rechazada:   g.Estado == estudiario.EstadoRechazada,
				Motivo:      g.MotivoRechazo,
			})
		}
		if len(p.Guias) == 0 || pagina*50 >= p.Total {
			break
		}
	}
	return res, nil
}

// traducir lleva los errores del cliente a los del puerto. Ningún código de
// Estudiario sale de acá: el motivo de un rechazo se arma con sus mensajes por campo.
func traducir(err error) error {
	var rechazo *estudiario.ErrorRechazo
	switch {
	case errors.As(err, &rechazo):
		return &ports.ErrGuiaRechazada{Motivo: motivo(rechazo)}
	case errors.Is(err, estudiario.ErrNoDisponible):
		return errors.Join(ports.ErrGuiasNoDisponible, err)
	default:
		// Respuesta fuera de contrato: se trata como no disponible y se reintenta.
		return errors.Join(ports.ErrGuiasNoDisponible, err)
	}
}

func motivo(r *estudiario.ErrorRechazo) string {
	if len(r.Campos) == 0 {
		if r.Detalle != "" {
			return r.Detalle
		}
		return "el servicio de guías rechazó los datos"
	}
	campos := make([]string, 0, len(r.Campos))
	for c, m := range r.Campos {
		campos = append(campos, c+": "+m)
	}
	sort.Strings(campos)
	return strings.Join(campos, "; ")
}
