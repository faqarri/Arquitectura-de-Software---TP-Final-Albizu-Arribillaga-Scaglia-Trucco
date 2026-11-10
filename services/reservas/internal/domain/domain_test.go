package domain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var ahora = time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)

func carritoCon(items ...ItemCarrito) Carrito { return Carrito{UsuarioID: "u1", Items: items} }

func item(id string, retenciones ...string) ItemCarrito {
	return ItemCarrito{ID: id, Retenciones: retenciones, Estado: ItemActivo, ExpiraEn: ahora.Add(10 * time.Minute)}
}

func TestItemVenceSegunElReloj(t *testing.T) {
	it := item("i1", "r1")
	if !it.Vigente(ahora) || it.EstadoVisible(ahora) != ItemActivo {
		t.Error("recién agregado debía estar vigente")
	}
	despues := ahora.Add(11 * time.Minute)
	if it.Vigente(despues) || it.EstadoVisible(despues) != ItemVencido {
		t.Error("pasado el vencimiento debía mostrarse VENCIDO")
	}
}

func TestCarritoRetencionesEItemsDe(t *testing.T) {
	c := carritoCon(item("i1", "r1"), item("pack", "r2", "r3"))
	if got := c.Retenciones(); strings.Join(got, ",") != "r1,r2,r3" {
		t.Errorf("retenciones = %v", got)
	}
	if got := c.ItemsDe([]string{"r3"}); len(got) != 1 || got[0].ID != "pack" {
		t.Errorf("ItemsDe(r3) = %v", got)
	}
}

func TestNuevaCompraValidaCarritoYEmail(t *testing.T) {
	if _, err := NuevaCompra("u1", "a@b.example", "k", carritoCon(), ahora); !errors.Is(err, ErrCarritoVacio) {
		t.Errorf("carrito vacío: %v", err)
	}
	if _, err := NuevaCompra("u1", "", "k", carritoCon(item("i1", "r1")), ahora); !errors.Is(err, ErrSinEmail) {
		t.Errorf("sin email: %v", err)
	}
	c, err := NuevaCompra("u1", "a@b.example", "k", carritoCon(item("i1", "r1"), item("i2", "r2")), ahora)
	if err != nil || c.Estado != CompraPendiente || len(c.Retenciones) != 2 || c.ID == "" {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestCompraConfirmadaSumaLineasYNoVuelveAtras(t *testing.T) {
	c, _ := NuevaCompra("u1", "a@b.example", "k", carritoCon(item("i1", "r1")), ahora)
	lineas := []LineaCompra{
		{RetencionID: "r1", Descripcion: "Vuelo", Destino: "Bariloche", Precio: decimal.RequireFromString("185000.50")},
		{RetencionID: "r2", Descripcion: "Cabaña", Destino: "Bariloche", Precio: decimal.RequireFromString("0.50")},
	}
	if err := c.Confirmar(lineas, ahora); err != nil {
		t.Fatal(err)
	}
	if c.Estado != CompraConfirmada || !c.Total.Equal(decimal.RequireFromString("185001")) {
		t.Errorf("estado=%s total=%s", c.Estado, c.Total)
	}
	if err := c.Rechazar([]string{"r1"}, ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("una compra confirmada no se puede rechazar: %v", err)
	}
	if err := c.Confirmar(lineas, ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("no se confirma dos veces: %v", err)
	}
	if d := c.Destinos(); len(d) != 2 || d[0] != "Bariloche" {
		t.Errorf("destinos = %v", d)
	}
}

func TestCompraRechazadaGuardaLasVencidas(t *testing.T) {
	c, _ := NuevaCompra("u1", "a@b.example", "k", carritoCon(item("i1", "r1")), ahora)
	if err := c.Rechazar([]string{"r1"}, ahora); err != nil {
		t.Fatal(err)
	}
	if c.Estado != CompraRechazada || c.RetencionesVencidas[0] != "r1" || len(c.Destinos()) != 0 {
		t.Errorf("c = %+v", c)
	}
	if err := c.Confirmar([]LineaCompra{{Precio: decimal.NewFromInt(1)}}, ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("una compra rechazada no se puede confirmar: %v", err)
	}
}

func TestConfirmarSinLineasEsInvalido(t *testing.T) {
	c, _ := NuevaCompra("u1", "a@b.example", "k", carritoCon(item("i1", "r1")), ahora)
	if err := c.Confirmar(nil, ahora); !errors.Is(err, ErrSinLineas) {
		t.Errorf("err = %v", err)
	}
}

var pdf = []byte("%PDF-1.4 guía")

func solicitud() SolicitudGuia {
	return SolicitudGuia{Titulo: "Trekking en el Catedral", Destino: "Bariloche", Autor: "Ana Pérez", Etiquetas: []string{"Trekking", " invierno ", "trekking"}, PDF: pdf}
}

func TestGuiaDeUnDestinoComprado(t *testing.T) {
	g, err := NuevaGuia("u1", solicitud(), []string{"bariloche"}, ahora)
	if err != nil {
		t.Fatal(err)
	}
	if g.Estado != GuiaPorEnviar || g.IdempotencyKey == "" || strings.Join(g.Etiquetas, ",") != "trekking,invierno" {
		t.Errorf("g = %+v", g)
	}
}

func TestGuiaDeUnDestinoNoComprado(t *testing.T) {
	if _, err := NuevaGuia("u1", solicitud(), []string{"Mendoza"}, ahora); !errors.Is(err, ErrDestinoNoComprado) {
		t.Errorf("err = %v", err)
	}
}

func TestGuiaValidaComoEstudiario(t *testing.T) {
	casos := map[string]func(*SolicitudGuia){
		"título corto":       func(s *SolicitudGuia) { s.Titulo = "ab" },
		"sin autor":          func(s *SolicitudGuia) { s.Autor = "" },
		"descripción larga":  func(s *SolicitudGuia) { s.Descripcion = strings.Repeat("x", 2001) },
		"etiqueta con coma":  func(s *SolicitudGuia) { s.Etiquetas = []string{"a,b"} },
		"once etiquetas":     func(s *SolicitudGuia) { s.Etiquetas = strings.Split("aa,bb,cc,dd,ee,ff,gg,hh,ii,jj,kk", ",") },
		"no es PDF":          func(s *SolicitudGuia) { s.PDF = []byte("hola") },
		"PDF de más de 10MB": func(s *SolicitudGuia) { s.PDF = append([]byte("%PDF"), bytes.Repeat([]byte{0}, MaxTamanioPDF)...) },
	}
	for nombre, modificar := range casos {
		s := solicitud()
		modificar(&s)
		if _, err := NuevaGuia("u1", s, []string{"Bariloche"}, ahora); err == nil {
			t.Errorf("%s: se esperaba error", nombre)
		}
	}
}

func TestNormalizarDestino(t *testing.T) {
	if NormalizarDestino("  San Carlos de  Bariloche ") != "san carlos de bariloche" || NormalizarDestino("Córdoba") != NormalizarDestino("CORDOBA") {
		t.Error("la normalización no ignora mayúsculas, acentos o espacios")
	}
}

func TestModeracionDeGuia(t *testing.T) {
	g, _ := NuevaGuia("u1", solicitud(), []string{"Bariloche"}, ahora)
	g.Registrada("doc12345", ahora)
	if g.Estado != GuiaPendiente || g.DocumentoID != "doc12345" {
		t.Errorf("g = %+v", g)
	}
	g.Moderada(false, "contenido duplicado", ahora)
	if g.Estado != GuiaRechazada || g.MotivoRechazo != "contenido duplicado" {
		t.Errorf("g = %+v", g)
	}
}
