package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/domain"
)

func comprobante(t *testing.T, items ...domain.Item) *domain.Comprobante {
	t.Helper()
	c, err := domain.NuevoComprobante("interno", "clave", "hash", "ana.perez@example.com", items,
		time.Date(2026, 10, 22, 15, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	c.Nro = 42
	return c
}

func TestGenerarDevuelveUnPDF(t *testing.T) {
	c := comprobante(t,
		domain.Item{Descripcion: "Excursión a Villa General Belgrano x2", Precio: decimal.RequireFromString("30000")},
		domain.Item{Descripcion: "Cargo por servicio", Precio: decimal.RequireFromString("1500.5")},
		// Caracteres fuera de cp1252 no deben romper la generación.
		domain.Item{Descripcion: "Seguro “premium” 🌎 ≥ 1 año", Precio: decimal.RequireFromString("10")},
	)

	pdf, err := NuevoGenerador().Generar(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("el resultado no empieza con %%PDF: %q", pdf[:min(len(pdf), 16)])
	}
}

func TestGenerarConMuchosItemsYDescripcionesLargas(t *testing.T) {
	items := make([]domain.Item, 100)
	for i := range items {
		items[i] = domain.Item{Descripcion: strings.Repeat("Estadía en cabaña con desayuno ", 6), Precio: decimal.NewFromInt(int64(i + 1))}
	}
	pdf, err := NuevoGenerador().Generar(context.Background(), comprobante(t, items...))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatal("el resultado no es un PDF")
	}
}

func TestGenerarRespetaElContexto(t *testing.T) {
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	c := comprobante(t, domain.Item{Descripcion: "Pasaje", Precio: decimal.NewFromInt(10)})
	if _, err := NuevoGenerador().Generar(ctx, c); err == nil {
		t.Fatal("se esperaba error con el contexto cancelado")
	}
}

func TestFormatoARS(t *testing.T) {
	casos := map[string]string{
		"0":          "$ 0,00",
		"5":          "$ 5,00",
		"1500.5":     "$ 1.500,50",
		"31500":      "$ 31.500,00",
		"1234567.89": "$ 1.234.567,89",
		"999999.99":  "$ 999.999,99",
		"-1234.5":    "-$ 1.234,50",
	}
	for entrada, esperado := range casos {
		if got := FormatoARS(decimal.RequireFromString(entrada)); got != esperado {
			t.Errorf("FormatoARS(%s) = %q, se esperaba %q", entrada, got, esperado)
		}
	}
}
