// Package pdf implementa ports.GeneradorPDF.
package pdf

import (
	"context"
	"errors"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

type Generador struct{}

func NuevoGenerador() *Generador { return &Generador{} }

var _ ports.GeneradorPDF = (*Generador)(nil)

// Generar: TODO(rabbit). Armar la factura con número, fecha, email, una línea por
// ítem (descripción y precio) y el total en ARS. Sugerencia: github.com/go-pdf/fpdf.
func (g *Generador) Generar(ctx context.Context, c *domain.Comprobante) ([]byte, error) {
	return nil, errors.New("no implementado")
}
