package models

import (
	"errors"
	"testing"
)

func campos(err error) map[string]bool {
	var v *ErrValidacion
	res := map[string]bool{}
	if errors.As(err, &v) {
		for _, e := range v.Errores {
			res[e.Campo] = true
		}
	}
	return res
}

func ptr(f float64) *float64 { return &f }

func TestFiltrosPorDefecto(t *testing.T) {
	f := FiltrosBusqueda{Tipo: "transporte", SubtipoTransporte: " avion "}
	if err := f.Validar(); err != nil {
		t.Fatal(err)
	}
	if f.Pagina != 1 || f.Tamanio != TamanioPorDefecto || f.Desplazamiento() != 0 {
		t.Fatalf("por defecto página 1 de 20: %+v", f)
	}
	if f.Tipo != Transporte || f.SubtipoTransporte != Avion {
		t.Fatalf("tipo y subtipo se normalizan a mayúsculas: %+v", f)
	}
}

func TestFiltrosInvalidos(t *testing.T) {
	casos := []struct {
		nombre string
		f      FiltrosBusqueda
		campo  string
	}{
		{"tipo desconocido", FiltrosBusqueda{Tipo: "CRUCERO"}, "tipo"},
		{"subtipo desconocido", FiltrosBusqueda{SubtipoTransporte: "BARCO"}, "subtipoTransporte"},
		{"fecha mal escrita", FiltrosBusqueda{Desde: "10/12/2026"}, "desde"},
		{"rango de fechas invertido", FiltrosBusqueda{Desde: "2026-12-10", Hasta: "2026-12-01"}, "hasta"},
		{"precio negativo", FiltrosBusqueda{PrecioMin: ptr(-1)}, "precioMin"},
		{"rango de precios invertido", FiltrosBusqueda{PrecioMin: ptr(100), PrecioMax: ptr(50)}, "precioMax"},
		{"orden desconocido", FiltrosBusqueda{Orden: "nombre"}, "orden"},
		{"tamaño mayor a 50", FiltrosBusqueda{Tamanio: 51}, "tamanio"},
		{"página negativa", FiltrosBusqueda{Pagina: -1}, "pagina"},
		// ADR-006: from + size ≤ 1000 y como máximo 50 páginas.
		{"página 51", FiltrosBusqueda{Pagina: 51, Tamanio: 10}, "pagina"},
		{"más de 1000 resultados", FiltrosBusqueda{Pagina: 21, Tamanio: 50}, "pagina"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			err := c.f.Validar()
			if !campos(err)[c.campo] {
				t.Fatalf("se esperaba un error en %q, se obtuvo %v", c.campo, err)
			}
		})
	}
}

func TestUltimaPaginaPermitida(t *testing.T) {
	f := FiltrosBusqueda{Pagina: 20, Tamanio: 50}
	if err := f.Validar(); err != nil {
		t.Fatalf("la página 20 de 50 llega justo a 1000 resultados: %v", err)
	}
	if f.Desplazamiento() != 950 {
		t.Fatalf("from = %d", f.Desplazamiento())
	}
	f = FiltrosBusqueda{Pagina: 50, Tamanio: 20}
	if err := f.Validar(); err != nil {
		t.Fatalf("la página 50 de 20 es la última: %v", err)
	}
}

func TestNormalizarDestino(t *testing.T) {
	for entrada, esperado := range map[string]string{
		"  San Carlos de  BARILOCHE ": "san carlos de bariloche",
		"Córdoba":                     "cordoba",
		"Ñandú":                       "nandu",
		"bariloche":                   "bariloche",
	} {
		if got := NormalizarDestino(entrada); got != esperado {
			t.Errorf("NormalizarDestino(%q) = %q, se esperaba %q", entrada, got, esperado)
		}
	}
}
