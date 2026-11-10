package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"agencia/pkg/mensajeria"
)

type indexadorFalso struct {
	err      error
	indexado []string
}

func (f *indexadorFalso) Indexar(_ context.Context, id string) error {
	f.indexado = append(f.indexado, id)
	return f.err
}

func entrega(cuerpo string, intentos int32) amqp.Delivery {
	return amqp.Delivery{Body: []byte(cuerpo), Headers: amqp.Table{mensajeria.HeaderIntentos: intentos}}
}

// ADR-006: el indexador reintenta ante fallas de OpenSearch o MongoDB y, al
// agotar los reintentos, manda el mensaje a la DLQ. Un mensaje mal formado va
// directo a la DLQ.
func TestManejadorIndexacion(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	evento := `{"eventId":"e1","tipo":"OfertaActualizada","ofertaId":"of-1","version":3}`

	casos := []struct {
		nombre    string
		cuerpo    string
		intentos  int32
		errIndice error
		esperado  mensajeria.Resultado
	}{
		{"indexa y confirma", evento, 0, nil, mensajeria.Confirmar},
		{"OpenSearch caído: reintenta", evento, 0, errors.New("connection refused"), mensajeria.Reintentar},
		{"último reintento fallido: DLQ", evento, mensajeria.MaxIntentos, errors.New("connection refused"), mensajeria.Descartar},
		{"mal formado: DLQ", `{"version":3}`, 0, nil, mensajeria.Descartar},
		{"no es JSON: DLQ", `hola`, 0, nil, mensajeria.Descartar},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			idx := &indexadorFalso{err: c.errIndice}
			r, _ := ManejadorIndexacion(log, idx)(context.Background(), entrega(c.cuerpo, c.intentos))
			if r != c.esperado {
				t.Fatalf("resultado %v, se esperaba %v", r, c.esperado)
			}
		})
	}

	idx := &indexadorFalso{}
	_, _ = ManejadorIndexacion(log, idx)(context.Background(), entrega(evento, 0))
	if len(idx.indexado) != 1 || idx.indexado[0] != "of-1" {
		t.Fatalf("se indexa la oferta del evento: %v", idx.indexado)
	}
}
