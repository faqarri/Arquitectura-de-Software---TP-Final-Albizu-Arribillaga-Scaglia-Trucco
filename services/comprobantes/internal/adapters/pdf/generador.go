// Package pdf implementa ports.GeneradorPDF.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// zonaArgentina: la fecha se muestra en hora de Argentina (UTC−3, sin horario
// de verano). Zona fija para no depender de la base tzdata en la imagen.
var zonaArgentina = time.FixedZone("ART", -3*60*60)

// Medidas en mm sobre A4 (210 × 297).
const (
	margen      = 15.0
	anchoUtil   = 210 - 2*margen
	anchoPrecio = 45.0
	anchoDesc   = anchoUtil - anchoPrecio
	altoLinea   = 7.0
)

type Generador struct{}

func NuevoGenerador() *Generador { return &Generador{} }

var _ ports.GeneradorPDF = (*Generador)(nil)

// Generar arma la factura: número, fecha de emisión, email, una línea por ítem
// (descripción y precio) y el total en ARS.
func (g *Generador) Generar(ctx context.Context, c *domain.Comprobante) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc := fpdf.New("P", "mm", "A4", "")
	doc.SetMargins(margen, margen, margen)
	doc.SetAutoPageBreak(true, margen)
	// Las fuentes estándar usan cp1252: el traductor convierte acentos y eñes desde UTF-8.
	tr := doc.UnicodeTranslatorFromDescriptor("")
	doc.SetTitle(tr("Factura "+c.Numero()), false)
	doc.SetAuthor("Agencia de Viajes", false)

	doc.AddPage()
	doc.SetFont("Helvetica", "B", 18)
	doc.CellFormat(anchoUtil, 10, "Agencia de Viajes", "", 1, "L", false, 0, "")
	doc.SetFont("Helvetica", "B", 14)
	doc.CellFormat(anchoUtil, 9, tr("Factura N.º "+c.Numero()), "", 1, "L", false, 0, "")
	doc.Ln(2)

	doc.SetFont("Helvetica", "", 11)
	fecha := c.FechaEmision.In(zonaArgentina).Format("02/01/2006 15:04")
	doc.CellFormat(anchoUtil, 6, tr("Fecha de emisión: "+fecha+" (hora de Argentina)"), "", 1, "L", false, 0, "")
	doc.CellFormat(anchoUtil, 6, tr("Email: "+c.Email), "", 1, "L", false, 0, "")
	doc.Ln(6)

	encabezado := func() {
		doc.SetFont("Helvetica", "B", 11)
		doc.SetFillColor(230, 230, 230)
		doc.CellFormat(anchoDesc, altoLinea, tr("Descripción"), "1", 0, "L", true, 0, "")
		doc.CellFormat(anchoPrecio, altoLinea, "Precio (ARS)", "1", 1, "R", true, 0, "")
		doc.SetFont("Helvetica", "", 11)
	}
	encabezado()

	_, altoPagina := doc.GetPageSize()
	for _, it := range c.Items {
		desc := tr(it.Descripcion)
		// SplitLines trabaja sobre bytes cp1252, como MultiCell (SplitText
		// espera UTF-8 y falla con el texto ya traducido).
		lineas := max(len(doc.SplitLines([]byte(desc), anchoDesc)), 1)
		alto := float64(lineas) * altoLinea
		// El salto de página se hace a mano para que la descripción (MultiCell)
		// y el precio queden en la misma página.
		if doc.GetY()+alto > altoPagina-margen {
			doc.AddPage()
			encabezado()
		}
		x, y := doc.GetXY()
		doc.MultiCell(anchoDesc, altoLinea, desc, "1", "L", false)
		doc.SetXY(x+anchoDesc, y)
		doc.CellFormat(anchoPrecio, alto, FormatoARS(it.Precio), "1", 1, "R", false, 0, "")
	}

	doc.SetFont("Helvetica", "B", 12)
	doc.CellFormat(anchoDesc, altoLinea+1, "Total (ARS)", "1", 0, "R", false, 0, "")
	doc.CellFormat(anchoPrecio, altoLinea+1, FormatoARS(c.Total), "1", 1, "R", false, 0, "")

	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, fmt.Errorf("generar PDF del comprobante %s: %w", c.ID, err)
	}
	return buf.Bytes(), nil
}

// FormatoARS da formato argentino a un importe, con 2 decimales: $ 31.500,00.
func FormatoARS(d decimal.Decimal) string {
	s := d.StringFixed(2)
	signo := ""
	if strings.HasPrefix(s, "-") {
		signo, s = "-", s[1:]
	}
	entero, decimales, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range entero {
		if i > 0 && (len(entero)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return signo + "$ " + b.String() + "," + decimales
}
