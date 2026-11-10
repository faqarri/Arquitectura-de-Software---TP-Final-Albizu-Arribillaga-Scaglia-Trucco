package estudiario

import (
	"encoding/json"
	"fmt"
	"time"
)

// Forma de los datos de Estudiario (contrato 1.0.0). Solo se usa dentro del
// paquete. Lector tolerante: se ignoran los campos desconocidos y un status
// nuevo se traduce a EstadoDesconocido.

type documentoDTO struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Category        string   `json:"category"`
	Tags            []string `json:"tags"`
	Status          string   `json:"status"`
	RejectionReason *string  `json:"rejectionReason"`
	Author          struct {
		Name       string  `json:"name"`
		ExternalID *string `json:"externalId"`
	} `json:"author"`
	Rating struct {
		Average *float64 `json:"average"`
		Count   int      `json:"count"`
	} `json:"rating"`
	File struct {
		SizeBytes    int64  `json:"sizeBytes"`
		DownloadPath string `json:"downloadPath"`
	} `json:"file"`
	PublishedAt *time.Time `json:"publishedAt"`
}

type paginaDTO struct {
	Items []documentoDTO `json:"items"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
	Total int            `json:"total"`
}

var estadoDesdeProveedor = map[string]EstadoGuia{
	"pendiente": EstadoPendiente,
	"aprobado":  EstadoPublicada,
	"rechazado": EstadoRechazada,
}

var estadoAProveedor = map[EstadoGuia]string{
	EstadoPendiente: "pendiente",
	EstadoPublicada: "aprobado",
	EstadoRechazada: "rechazado",
}

func (d documentoDTO) aGuia() (Guia, error) {
	if d.ID == "" {
		return Guia{}, fmt.Errorf("%w: documento sin id", ErrRespuestaInvalida)
	}
	estado, ok := estadoDesdeProveedor[d.Status]
	if !ok {
		estado = EstadoDesconocido
	}
	g := Guia{
		ID:                     d.ID,
		Titulo:                 d.Title,
		Descripcion:            d.Description,
		Destino:                d.Category,
		Etiquetas:              d.Tags,
		Estado:                 estado,
		Autor:                  d.Author.Name,
		Calificacion:           d.Rating.Average,
		CantidadCalificaciones: d.Rating.Count,
		RutaDescarga:           d.File.DownloadPath,
		TamanioBytes:           d.File.SizeBytes,
		PublicadaEn:            d.PublishedAt,
	}
	if d.RejectionReason != nil {
		g.MotivoRechazo = *d.RejectionReason
	}
	if d.Author.ExternalID != nil {
		g.AutorExternoID = *d.Author.ExternalID
	}
	return g, nil
}

func (p paginaDTO) aPagina() (Pagina, error) {
	guias := make([]Guia, 0, len(p.Items))
	for _, d := range p.Items {
		g, err := d.aGuia()
		if err != nil {
			return Pagina{}, err
		}
		guias = append(guias, g)
	}
	return Pagina{Guias: guias, Pagina: p.Page, Tamanio: p.Size, Total: p.Total}, nil
}

type problemaDTO struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func leerProblema(cuerpo []byte) problemaDTO {
	var p problemaDTO
	_ = json.Unmarshal(cuerpo, &p) // si no es un problema válido, quedan los valores vacíos
	return p
}

func (p problemaDTO) aRechazo(status int) *ErrorRechazo {
	r := &ErrorRechazo{Status: status, Codigo: p.Code, Detalle: p.Detail}
	if len(p.Errors) > 0 {
		r.Campos = map[string]string{}
		for _, e := range p.Errors {
			r.Campos[e.Field] = e.Message
		}
	}
	return r
}
