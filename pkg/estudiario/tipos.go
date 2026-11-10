// Package estudiario es el adaptador (capa anticorrupción) de la capacidad que
// nos provee el Grupo 2: registro y consulta de documentos, que usamos para las
// guías de viaje (ADR-009). Contrato fijado en
// docs/contracts/consumidos/estudiario/1.0.0/documents-v1.yaml.
//
// Ningún nombre de campo ni código de error de Estudiario sale de este paquete:
// los servicios usan los tipos y errores de acá.
package estudiario

import (
	"errors"
	"fmt"
	"time"
)

// EstadoGuia es el estado de moderación de una guía en Estudiario.
type EstadoGuia string

const (
	EstadoPendiente   EstadoGuia = "PENDIENTE"
	EstadoPublicada   EstadoGuia = "PUBLICADA"
	EstadoRechazada   EstadoGuia = "RECHAZADA"
	EstadoDesconocido EstadoGuia = "DESCONOCIDO" // lector tolerante: un estado nuevo del proveedor
)

// Guia es un documento de Estudiario, en nuestros términos.
type Guia struct {
	ID             string
	Titulo         string
	Descripcion    string
	Destino        string // la "category" de Estudiario
	Etiquetas      []string
	Estado         EstadoGuia
	MotivoRechazo  string
	Autor          string
	AutorExternoID string
	// Calificacion es nil si todavía no tiene calificaciones.
	Calificacion           *float64
	CantidadCalificaciones int
	RutaDescarga           string
	// URLDescarga es el enlace público al PDF (sin API Key), para el navegador.
	// Solo lo completa Buscar, y solo con Config.URLPublica configurada.
	URLDescarga  string
	TamanioBytes int64
	PublicadaEn  *time.Time
}

// Pagina es una página de resultados.
type Pagina struct {
	Guias   []Guia
	Pagina  int
	Tamanio int
	Total   int
}

// Orden de la búsqueda.
type Orden string

const (
	OrdenRelevancia   Orden = "relevance"
	OrdenCalificacion Orden = "rating"
	OrdenFecha        Orden = "date"
)

// Busqueda son los filtros de la búsqueda de guías publicadas.
type Busqueda struct {
	Texto           string
	Destino         string
	Etiquetas       []string
	CalificacionMin float64 // 0 = sin filtro
	Orden           Orden   // vacío = el defecto del proveedor
	Pagina          int     // desde 1; 0 = 1
	Tamanio         int     // 1 a 50; 0 = 20
}

// Registro es una guía a registrar.
type Registro struct {
	// IdempotencyKey identifica el registro: la misma en todos los reintentos.
	IdempotencyKey string
	Titulo         string
	Descripcion    string
	Destino        string
	Etiquetas      []string
	Autor          string
	AutorExternoID string
	NombreArchivo  string
	PDF            []byte
}

var (
	// ErrNoDisponible: el proveedor no respondió, respondió 5xx o 429, o el
	// circuito está abierto. Se puede reintentar más tarde.
	ErrNoDisponible = errors.New("el servicio de guías no está disponible")
	// ErrCircuitoAbierto envuelve a ErrNoDisponible: no se llamó al proveedor.
	ErrCircuitoAbierto = fmt.Errorf("%w: circuito abierto", ErrNoDisponible)
	// ErrRespuestaInvalida: el proveedor respondió algo que no cumple su contrato.
	ErrRespuestaInvalida = errors.New("el servicio de guías respondió algo que no cumple su contrato")
	// ErrNoEncontrada: la guía no existe, fue eliminada o no es visible para nosotros.
	ErrNoEncontrada = errors.New("guía no encontrada")
	// ErrNoAutorizado: Estudiario rechazó nuestra API Key (revisar ESTUDIARIO_API_KEY).
	// Envuelve a ErrNoDisponible: para los servicios es como si no respondiera.
	ErrNoAutorizado = fmt.Errorf("%w: API Key de Estudiario ausente o inválida", ErrNoDisponible)
)

// ErrorRechazo es un error de negocio o de validación del proveedor (4xx).
// Reintentar con los mismos datos no cambia el resultado.
type ErrorRechazo struct {
	Status  int
	Codigo  string // el code del proveedor, solo para logs
	Detalle string
	Campos  map[string]string // campo → mensaje
}

func (e *ErrorRechazo) Error() string {
	return fmt.Sprintf("el servicio de guías rechazó la solicitud (%d %s): %s", e.Status, e.Codigo, e.Detalle)
}
