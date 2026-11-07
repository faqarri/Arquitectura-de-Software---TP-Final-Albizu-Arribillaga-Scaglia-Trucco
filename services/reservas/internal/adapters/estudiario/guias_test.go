package estudiario

import (
	"context"
	"errors"
	"testing"

	"agencia/pkg/estudiario"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

type clienteFalso struct {
	err       error
	recibido  estudiario.Registro
	paginas   []estudiario.Pagina
	errPagina int // página (desde 1) que falla
}

func (c *clienteFalso) Registrar(_ context.Context, r estudiario.Registro) (estudiario.Guia, error) {
	c.recibido = r
	if c.err != nil {
		return estudiario.Guia{}, c.err
	}
	return estudiario.Guia{ID: "doc12345", Estado: estudiario.EstadoPendiente}, nil
}

func (c *clienteFalso) Registradas(_ context.Context, _ estudiario.EstadoGuia, pagina, _ int) (estudiario.Pagina, error) {
	if pagina == c.errPagina {
		return estudiario.Pagina{}, estudiario.ErrNoDisponible
	}
	return c.paginas[pagina-1], nil
}

func guia() domain.Guia {
	return domain.Guia{ID: "g1", UsuarioID: "u1", IdempotencyKey: "clave-1", Titulo: "Trekking", Destino: "Bariloche", Autor: "Ana", Etiquetas: []string{"trekking"}}
}

func TestRegistrarMandaNuestrosDatosEnSusTerminos(t *testing.T) {
	c := &clienteFalso{}
	id, err := (&Guias{cli: c}).Registrar(context.Background(), guia(), []byte("%PDF"))
	if err != nil || id != "doc12345" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if c.recibido.AutorExternoID != "u1" || c.recibido.Destino != "Bariloche" || c.recibido.IdempotencyKey != "clave-1" {
		t.Errorf("registro = %+v", c.recibido)
	}
}

func TestTraduceLosErrores(t *testing.T) {
	g := &Guias{cli: &clienteFalso{err: &estudiario.ErrorRechazo{Status: 422, Codigo: "VALIDATION_ERROR", Campos: map[string]string{"title": "muy corto"}}}}
	_, err := g.Registrar(context.Background(), guia(), nil)
	var rechazo *ports.ErrGuiaRechazada
	if !errors.As(err, &rechazo) || rechazo.Motivo != "title: muy corto" {
		t.Fatalf("err = %v", err)
	}

	g = &Guias{cli: &clienteFalso{err: estudiario.ErrCircuitoAbierto}}
	if _, err := g.Registrar(context.Background(), guia(), nil); !errors.Is(err, ports.ErrGuiasNoDisponible) {
		t.Fatalf("err = %v", err)
	}
}

func TestModeracionRecorreLasPaginas(t *testing.T) {
	pagina := func(n int, estado estudiario.EstadoGuia) estudiario.Pagina {
		guias := make([]estudiario.Guia, n)
		for i := range guias {
			guias[i] = estudiario.Guia{ID: string(rune('a' + i)), Estado: estado, MotivoRechazo: "x"}
		}
		return estudiario.Pagina{Guias: guias, Total: 60}
	}
	c := &clienteFalso{paginas: []estudiario.Pagina{pagina(50, estudiario.EstadoPublicada), pagina(10, estudiario.EstadoRechazada)}}
	estados, err := (&Guias{cli: c}).Moderacion(context.Background())
	if err != nil || len(estados) != 60 {
		t.Fatalf("estados=%d err=%v", len(estados), err)
	}
	if !estados[0].Publicada || !estados[59].Rechazada {
		t.Errorf("estados = %+v ... %+v", estados[0], estados[59])
	}

	c.errPagina = 2
	estados, err = (&Guias{cli: c}).Moderacion(context.Background())
	if err != nil || len(estados) != 50 {
		t.Errorf("con la página 2 caída se devuelve lo leído: %d, %v", len(estados), err)
	}
}
