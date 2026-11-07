package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Guías de viaje (HU-13): un cliente comparte una guía en PDF de un destino que
// compró. Se publica en Estudiario, que la modera (ADR-009).

// EstadoGuia es el estado de una guía compartida.
type EstadoGuia string

const (
	GuiaPorEnviar EstadoGuia = "POR_ENVIAR" // todavía no la aceptó Estudiario (no respondió o falló)
	GuiaPendiente EstadoGuia = "PENDIENTE"  // registrada, esperando moderación
	GuiaPublicada EstadoGuia = "PUBLICADA"
	GuiaRechazada EstadoGuia = "RECHAZADA" // la moderación o la validación de Estudiario la rechazó
)

const MaxTamanioPDF = 10 << 20 // límite de Estudiario (RN-20)

var (
	ErrDestinoNoComprado = errors.New("solo podés compartir guías de destinos que compraste")
	ErrPDFInvalido       = errors.New("el archivo tiene que ser un PDF de hasta 10 MB")
)

// ErrValidacionGuia indica qué campo de la guía no es válido.
type ErrValidacionGuia struct {
	Campo   string
	Mensaje string
}

func (e *ErrValidacionGuia) Error() string { return e.Campo + ": " + e.Mensaje }

// Guia es una guía de viaje compartida por un cliente.
type Guia struct {
	ID             string
	UsuarioID      string
	IdempotencyKey string // la misma en todos los envíos a Estudiario
	Titulo         string
	Descripcion    string
	Destino        string
	Etiquetas      []string
	Autor          string
	DocumentoID    string // id en Estudiario, cuando lo aceptó
	Estado         EstadoGuia
	MotivoRechazo  string
	Intentos       int
	CreadaEn       time.Time
	ActualizadaEn  time.Time
}

// SolicitudGuia son los datos que manda el cliente.
type SolicitudGuia struct {
	Titulo      string
	Descripcion string
	Destino     string
	Etiquetas   []string
	Autor       string
	PDF         []byte
}

// NuevaGuia valida la guía y que el destino esté entre los comprados. Las reglas
// de formato son las del contrato de Estudiario, para rechazar acá lo que allá
// se rechazaría.
func NuevaGuia(usuarioID string, s SolicitudGuia, destinosComprados []string, ahora time.Time) (*Guia, error) {
	s.Titulo = strings.TrimSpace(s.Titulo)
	s.Autor = strings.TrimSpace(s.Autor)
	s.Destino = strings.TrimSpace(s.Destino)
	switch {
	case !entre(s.Titulo, 3, 150):
		return nil, &ErrValidacionGuia{"titulo", "debe tener entre 3 y 150 caracteres"}
	case !entre(s.Autor, 2, 100):
		return nil, &ErrValidacionGuia{"autor", "debe tener entre 2 y 100 caracteres"}
	case !entre(s.Destino, 2, 120):
		return nil, &ErrValidacionGuia{"destino", "debe tener entre 2 y 120 caracteres"}
	case utf8.RuneCountInString(s.Descripcion) > 2000:
		return nil, &ErrValidacionGuia{"descripcion", "admite hasta 2000 caracteres"}
	}
	etiquetas, err := normalizarEtiquetas(s.Etiquetas)
	if err != nil {
		return nil, err
	}
	if len(s.PDF) == 0 || len(s.PDF) > MaxTamanioPDF || !strings.HasPrefix(string(s.PDF[:min(len(s.PDF), 5)]), "%PDF") {
		return nil, ErrPDFInvalido
	}
	if !contieneDestino(destinosComprados, s.Destino) {
		return nil, ErrDestinoNoComprado
	}
	return &Guia{
		ID:             uuid.NewString(),
		UsuarioID:      usuarioID,
		IdempotencyKey: uuid.NewString(),
		Titulo:         s.Titulo,
		Descripcion:    s.Descripcion,
		Destino:        s.Destino,
		Etiquetas:      etiquetas,
		Autor:          s.Autor,
		Estado:         GuiaPorEnviar,
		CreadaEn:       ahora.UTC(),
		ActualizadaEn:  ahora.UTC(),
	}, nil
}

// Registrada: Estudiario aceptó la guía y quedó esperando moderación.
func (g *Guia) Registrada(documentoID string, ahora time.Time) {
	g.DocumentoID = documentoID
	g.Estado = GuiaPendiente
	g.ActualizadaEn = ahora.UTC()
}

// Moderada actualiza el estado según la moderación de Estudiario.
func (g *Guia) Moderada(publicada bool, motivo string, ahora time.Time) {
	if publicada {
		g.Estado = GuiaPublicada
		g.MotivoRechazo = ""
	} else {
		g.Estado = GuiaRechazada
		g.MotivoRechazo = motivo
	}
	g.ActualizadaEn = ahora.UTC()
}

// FalloEnvio cuenta un intento fallido; la guía sigue POR_ENVIAR.
func (g *Guia) FalloEnvio(ahora time.Time) {
	g.Intentos++
	g.ActualizadaEn = ahora.UTC()
}

func entre(s string, minimo, maximo int) bool {
	n := utf8.RuneCountInString(s)
	return n >= minimo && n <= maximo
}

func normalizarEtiquetas(etiquetas []string) ([]string, error) {
	vistas := map[string]bool{}
	var res []string
	for _, e := range etiquetas {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || vistas[e] {
			continue
		}
		if !entre(e, 2, 40) || strings.Contains(e, ",") {
			return nil, &ErrValidacionGuia{"etiquetas", "cada etiqueta debe tener entre 2 y 40 caracteres, sin comas"}
		}
		vistas[e] = true
		res = append(res, e)
	}
	if len(res) > 10 {
		return nil, &ErrValidacionGuia{"etiquetas", "admite hasta 10 etiquetas"}
	}
	return res, nil
}

// NormalizarDestino compara destinos sin distinguir mayúsculas ni acentos
// ("Córdoba" y "cordoba" son el mismo).
func NormalizarDestino(d string) string {
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")
	return strings.Join(strings.Fields(r.Replace(strings.ToLower(d))), " ")
}

func contieneDestino(destinos []string, destino string) bool {
	buscado := NormalizarDestino(destino)
	for _, d := range destinos {
		if NormalizarDestino(d) == buscado {
			return true
		}
	}
	return false
}
