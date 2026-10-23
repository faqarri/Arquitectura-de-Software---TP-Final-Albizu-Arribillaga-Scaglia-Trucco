package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func item(desc, precio string) Item {
	return Item{Descripcion: desc, Precio: decimal.RequireFromString(precio)}
}

func TestNuevoComprobanteRedondeaCadaItemYSumaLosRedondeados(t *testing.T) {
	items := []Item{item("a", "10.005"), item("b", "0.004"), item("c", "1.335")}

	c, err := NuevoComprobante("interno", "k", "h", "x@y.com", items, time.Now())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []string{"10.01", "0", "1.34"}
	for i, w := range want {
		if !c.Items[i].Precio.Equal(decimal.RequireFromString(w)) {
			t.Errorf("ítem %d: precio %s, se esperaba %s", i, c.Items[i].Precio, w)
		}
	}
	if !c.Total.Equal(decimal.RequireFromString("11.35")) {
		t.Errorf("total %s, se esperaba 11.35", c.Total)
	}
	if c.EstadoEnvio != EnvioPendiente {
		t.Errorf("estado %s, se esperaba PENDIENTE", c.EstadoEnvio)
	}
}

func TestNuevoComprobanteNoModificaLosItemsRecibidos(t *testing.T) {
	items := []Item{item("a", "10.005")}

	if _, err := NuevoComprobante("interno", "k", "h", "x@y.com", items, time.Now()); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if items[0].Precio.String() != "10.005" {
		t.Errorf("se modificó el precio original: %s", items[0].Precio)
	}
}

func TestNuevoComprobanteErrores(t *testing.T) {
	casos := []struct {
		nombre string
		items  []Item
		want   error
	}{
		{"sin ítems", nil, ErrSinItems},
		{"precio negativo", []Item{item("a", "10"), item("b", "-1")}, ErrPrecioNegativo},
		{"total cero", []Item{item("a", "0"), item("b", "0")}, ErrTotalCero},
		{"total cero después de redondear", []Item{item("a", "0.004")}, ErrTotalCero},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			_, err := NuevoComprobante("interno", "k", "h", "x@y.com", tc.items, time.Now())
			if !errors.Is(err, tc.want) {
				t.Errorf("error %v, se esperaba %v", err, tc.want)
			}
		})
	}
}

func TestNumero(t *testing.T) {
	c := &Comprobante{Nro: 42}
	if got := c.Numero(); got != "B-0001-00000042" {
		t.Errorf("número %s, se esperaba B-0001-00000042", got)
	}
}

func TestTransicionesDeEnvio(t *testing.T) {
	c := &Comprobante{EstadoEnvio: EnvioPendiente}
	if err := c.MarcarEnviado(); err != nil {
		t.Fatalf("PENDIENTE → ENVIADO: %v", err)
	}
	if err := c.MarcarEnviado(); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("ENVIADO → ENVIADO debería fallar, error: %v", err)
	}
	if err := c.MarcarFallido(); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("ENVIADO → FALLIDO debería fallar, error: %v", err)
	}

	f := &Comprobante{EstadoEnvio: EnvioPendiente}
	if err := f.MarcarFallido(); err != nil || f.EstadoEnvio != EnvioFallido {
		t.Errorf("PENDIENTE → FALLIDO: estado %s, error %v", f.EstadoEnvio, err)
	}
}
