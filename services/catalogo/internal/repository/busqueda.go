package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"

	"agencia/catalogo/internal/models"
)

// Índice de búsqueda de ofertas en OpenSearch (ADR-006). Es una proyección de
// MongoDB que se puede reconstruir en cualquier momento. Siempre se accede por
// el alias, nunca por el nombre de un índice concreto.

const (
	timeoutBusqueda = 2 * time.Second
	timeoutEscritura = 5 * time.Second
	timeoutLote      = 30 * time.Second
)

// mapeoOfertas es el mapping de ADR-006. destino es keyword normalizado
// (minúsculas y sin acentos) para el filtro exacto, con un subcampo de texto
// para la búsqueda libre. precio es scaled_float con factor 100 (centavos).
// Los campos de texto usan el analizador "espanol": el del español (stopwords
// y stemmer) más asciifolding, para que "cordoba" encuentre "Córdoba".
const mapeoOfertas = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "refresh_interval": "1s",
    "analysis": {
      "normalizer": {
        "sin_acentos": { "type": "custom", "filter": ["lowercase", "asciifolding"] }
      },
      "filter": {
        "espanol_stop":    { "type": "stop", "stopwords": "_spanish_" },
        "espanol_stemmer": { "type": "stemmer", "language": "light_spanish" }
      },
      "analyzer": {
        "espanol": {
          "type": "custom", "tokenizer": "standard",
          "filter": ["lowercase", "espanol_stop", "asciifolding", "espanol_stemmer"]
        }
      }
    }
  },
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "id":                { "type": "keyword" },
      "tipo":              { "type": "keyword" },
      "subtipoTransporte": { "type": "keyword" },
      "destino": {
        "type": "keyword", "normalizer": "sin_acentos",
        "fields": { "texto": { "type": "text", "analyzer": "espanol" } }
      },
      "origen":        { "type": "keyword" },
      "titulo":        { "type": "text", "analyzer": "espanol" },
      "descripcion":   { "type": "text", "analyzer": "espanol" },
      "fechaInicio":   { "type": "date" },
      "fechaFin":      { "type": "date" },
      "precio":        { "type": "scaled_float", "scaling_factor": 100 },
      "publicada":     { "type": "boolean" },
      "version":       { "type": "long" },
      "actualizadaEn": { "type": "date" }
    }
  }
}`

// docIndice es el documento indexado de una oferta.
type docIndice struct {
	ID                string    `json:"id"`
	Tipo              string    `json:"tipo"`
	SubtipoTransporte string    `json:"subtipoTransporte,omitempty"`
	Titulo            string    `json:"titulo"`
	Descripcion       string    `json:"descripcion"`
	Destino           string    `json:"destino"`
	Origen            string    `json:"origen,omitempty"`
	FechaInicio       time.Time `json:"fechaInicio"`
	FechaFin          time.Time `json:"fechaFin"`
	Precio            float64   `json:"precio"`
	Publicada         bool      `json:"publicada"`
	Version           int64     `json:"version"`
	ActualizadaEn     time.Time `json:"actualizadaEn"`
}

func documentoDe(o models.Oferta) docIndice {
	return docIndice{
		ID: o.ID, Tipo: string(o.Tipo), SubtipoTransporte: string(o.SubtipoTransporte), Titulo: o.Titulo,
		Descripcion: o.Descripcion, Destino: o.Destino, Origen: o.Origen, FechaInicio: o.Desde.UTC(),
		FechaFin: o.Hasta.UTC(), Precio: o.Precio, Publicada: o.Publicada, Version: o.Version,
		ActualizadaEn: o.ActualizadaEn.UTC(),
	}
}

func (d docIndice) modelo() models.Oferta {
	return models.Oferta{
		ID: d.ID, Tipo: models.TipoOferta(d.Tipo), SubtipoTransporte: models.SubtipoTransporte(d.SubtipoTransporte),
		Titulo: d.Titulo, Descripcion: d.Descripcion, Destino: d.Destino, Origen: d.Origen,
		Desde: d.FechaInicio.UTC(), Hasta: d.FechaFin.UTC(), Precio: d.Precio, Publicada: d.Publicada,
		Version: d.Version, ActualizadaEn: d.ActualizadaEn.UTC(),
	}
}

// IndiceBusqueda habla con OpenSearch.
type IndiceBusqueda struct {
	cli   *opensearchapi.Client
	alias string
}

// NuevoIndiceBusqueda configura el cliente. No se conecta: si OpenSearch no
// está, la búsqueda responde 503 y el resto de Catálogo sigue funcionando.
func NuevoIndiceBusqueda(url, alias string) (*IndiceBusqueda, error) {
	cli, err := opensearchapi.NewClient(opensearchapi.Config{Client: opensearch.Config{
		Addresses: []string{url},
		// Un solo reintento del cliente ante 502/503/504: los reintentos de la
		// indexación los maneja la cola, y la búsqueda prefiere fallar rápido.
		MaxRetries: 1,
	}})
	if err != nil {
		return nil, fmt.Errorf("configurar OpenSearch: %w", err)
	}
	return &IndiceBusqueda{cli: cli, alias: alias}, nil
}

// Alias es el nombre por el que se consulta el índice.
func (r *IndiceBusqueda) Alias() string { return r.alias }

// NombreIndiceInicial es el índice que se crea la primera vez, junto con el alias.
func (r *IndiceBusqueda) NombreIndiceInicial() string { return r.alias + "-inicial" }

// NombreIndiceNuevo es el nombre de un índice para una reconstrucción.
func (r *IndiceBusqueda) NombreIndiceNuevo(t time.Time) string {
	return r.alias + "-" + t.UTC().Format("20060102-150405")
}

func estadoHTTP(resp *opensearch.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// AsegurarIndice crea el índice inicial con el alias si el alias todavía no
// existe. La creación del índice y del alias es una sola operación atómica: si
// las dos instancias de Catálogo arrancan juntas, solo una lo crea. Devuelve
// true si lo creó esta llamada (y entonces hay que cargarlo desde MongoDB).
func (r *IndiceBusqueda) AsegurarIndice(ctx context.Context) (bool, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	resp, err := r.cli.Indices.Alias.Exists(ctx, opensearchapi.AliasExistsReq{Alias: []string{r.alias}})
	switch estadoHTTP(resp) {
	case 200:
		return false, nil
	case 404:
	default:
		return false, fmt.Errorf("consultar el alias %s: %w", r.alias, err)
	}
	cuerpo, err := cuerpoIndice(r.alias)
	if err != nil {
		return false, err
	}
	creado, err := r.cli.Indices.Create(ctx, opensearchapi.IndicesCreateReq{Index: r.NombreIndiceInicial(), Body: cuerpo})
	if err != nil {
		if creado != nil && estadoHTTP(creado.Inspect().Response) == 400 && strings.Contains(err.Error(), "resource_already_exists") {
			return false, nil // lo creó la otra instancia
		}
		return false, fmt.Errorf("crear el índice %s: %w", r.NombreIndiceInicial(), err)
	}
	return true, nil
}

// cuerpoIndice arma el cuerpo de creación: el mapping y, si alias no es vacío,
// el alias en la misma operación.
func cuerpoIndice(alias string) (*bytes.Reader, error) {
	var def map[string]any
	if err := json.Unmarshal([]byte(mapeoOfertas), &def); err != nil {
		return nil, fmt.Errorf("mapping inválido: %w", err)
	}
	if alias != "" {
		def["aliases"] = map[string]any{alias: map[string]any{}}
	}
	b, _ := json.Marshal(def)
	return bytes.NewReader(b), nil
}

// CrearIndice crea un índice vacío con el mapping vigente, sin alias.
func (r *IndiceBusqueda) CrearIndice(ctx context.Context, nombre string) error {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	cuerpo, err := cuerpoIndice("")
	if err != nil {
		return err
	}
	if _, err := r.cli.Indices.Create(ctx, opensearchapi.IndicesCreateReq{Index: nombre, Body: cuerpo}); err != nil {
		return fmt.Errorf("crear el índice %s: %w", nombre, err)
	}
	return nil
}

// Indexar escribe la oferta en el índice del alias con version_type=external:
// OpenSearch rechaza (409) una versión menor o igual a la indexada, así que un
// evento duplicado o desordenado no puede volver atrás el índice. Devuelve
// false si la versión ya estaba (no es un error).
func (r *IndiceBusqueda) Indexar(ctx context.Context, o models.Oferta) (bool, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	cuerpo, _ := json.Marshal(documentoDe(o))
	version := int(o.Version)
	resp, err := r.cli.Index(ctx, opensearchapi.IndexReq{
		Index: r.alias, DocumentID: o.ID, Body: bytes.NewReader(cuerpo),
		Params: opensearchapi.IndexParams{
			Version: &version, VersionType: "external",
			// Sin el alias no se escribe: evita que OpenSearch cree solo un
			// índice "ofertas" con un mapping adivinado.
			RequireAlias: opensearchapi.ToPointer(true),
		},
	})
	if err != nil {
		if resp != nil && estadoHTTP(resp.Inspect().Response) == 409 {
			return false, nil
		}
		return false, fmt.Errorf("indexar la oferta %s: %w", o.ID, err)
	}
	return true, nil
}

// CargarLote indexa varias ofertas en el índice indicado con _bulk y versiones
// externas. Los conflictos de versión no son errores (ya hay algo más nuevo).
func (r *IndiceBusqueda) CargarLote(ctx context.Context, indice string, ofertas []models.Oferta) error {
	if len(ofertas) == 0 {
		return nil
	}
	ctx, cancelar := context.WithTimeout(ctx, timeoutLote)
	defer cancelar()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, o := range ofertas {
		_ = enc.Encode(map[string]any{"index": map[string]any{
			"_index": indice, "_id": o.ID, "version": o.Version, "version_type": "external",
		}})
		_ = enc.Encode(documentoDe(o))
	}
	resp, err := r.cli.Bulk(ctx, opensearchapi.BulkReq{Body: bytes.NewReader(b.Bytes())})
	if err != nil {
		return fmt.Errorf("cargar lote en %s: %w", indice, err)
	}
	if !resp.Errors {
		return nil
	}
	for _, item := range resp.Items {
		for _, res := range item {
			if res.Error != nil && res.Status != 409 {
				return fmt.Errorf("cargar la oferta %s en %s: %s: %s", res.ID, indice, res.Error.Type, res.Error.Reason)
			}
		}
	}
	return nil
}

// IndicesDelAlias devuelve los índices a los que apunta el alias.
func (r *IndiceBusqueda) IndicesDelAlias(ctx context.Context) ([]string, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	resp, err := r.cli.Indices.Alias.Get(ctx, opensearchapi.AliasGetReq{Alias: []string{r.alias}})
	if err != nil {
		if resp != nil && estadoHTTP(resp.Inspect().Response) == 404 {
			return nil, nil
		}
		return nil, fmt.Errorf("leer el alias %s: %w", r.alias, err)
	}
	var res []string
	for nombre := range resp.Indices {
		res = append(res, nombre)
	}
	return res, nil
}

// MoverAlias apunta el alias al índice nuevo y lo saca de los viejos en una
// sola operación atómica: las búsquedas nunca ven un índice a medio cargar.
func (r *IndiceBusqueda) MoverAlias(ctx context.Context, nuevo string, viejos []string) error {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	acciones := []map[string]any{{"add": map[string]any{"index": nuevo, "alias": r.alias}}}
	for _, v := range viejos {
		if v != nuevo {
			acciones = append(acciones, map[string]any{"remove": map[string]any{"index": v, "alias": r.alias}})
		}
	}
	cuerpo, _ := json.Marshal(map[string]any{"actions": acciones})
	if _, err := r.cli.Aliases(ctx, opensearchapi.AliasesReq{Body: bytes.NewReader(cuerpo)}); err != nil {
		return fmt.Errorf("mover el alias %s a %s: %w", r.alias, nuevo, err)
	}
	return nil
}

// Refrescar hace visibles los documentos recién cargados (antes de contarlos).
func (r *IndiceBusqueda) Refrescar(ctx context.Context, indice string) error {
	ctx, cancelar := context.WithTimeout(ctx, timeoutLote)
	defer cancelar()
	if _, err := r.cli.Indices.Refresh(ctx, &opensearchapi.IndicesRefreshReq{Indices: []string{indice}}); err != nil {
		return fmt.Errorf("refrescar %s: %w", indice, err)
	}
	return nil
}

// Contar devuelve la cantidad de documentos de un índice.
func (r *IndiceBusqueda) Contar(ctx context.Context, indice string) (int, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutEscritura)
	defer cancelar()
	resp, err := r.cli.Indices.Count(ctx, &opensearchapi.IndicesCountReq{Indices: []string{indice}})
	if err != nil {
		return 0, fmt.Errorf("contar %s: %w", indice, err)
	}
	return resp.Count, nil
}

// BorrarIndices elimina índices concretos (nunca el alias).
func (r *IndiceBusqueda) BorrarIndices(ctx context.Context, nombres []string) error {
	if len(nombres) == 0 {
		return nil
	}
	ctx, cancelar := context.WithTimeout(ctx, timeoutLote)
	defer cancelar()
	if _, err := r.cli.Indices.Delete(ctx, opensearchapi.IndicesDeleteReq{Indices: nombres}); err != nil {
		return fmt.Errorf("borrar %v: %w", nombres, err)
	}
	return nil
}

// Buscar ejecuta la búsqueda sobre el alias. Cualquier falla de OpenSearch se
// informa como models.ErrBusquedaNoDisponible.
func (r *IndiceBusqueda) Buscar(ctx context.Context, f models.FiltrosBusqueda) (models.PaginaOfertas, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutBusqueda)
	defer cancelar()
	cuerpo, _ := json.Marshal(ConsultaBusqueda(f))
	resp, err := r.cli.Search(ctx, &opensearchapi.SearchReq{Indices: []string{r.alias}, Body: bytes.NewReader(cuerpo)})
	if err != nil {
		return models.PaginaOfertas{}, fmt.Errorf("%w: %v", models.ErrBusquedaNoDisponible, err)
	}
	pagina := models.PaginaOfertas{
		Items: make([]models.Oferta, 0, len(resp.Hits.Hits)), Pagina: f.Pagina, Tamanio: f.Tamanio,
		Total: min(resp.Hits.Total.Value, models.MaxResultados),
	}
	for _, h := range resp.Hits.Hits {
		var d docIndice
		if err := json.Unmarshal(h.Source, &d); err != nil {
			return models.PaginaOfertas{}, fmt.Errorf("%w: documento ilegible %s: %v", models.ErrBusquedaNoDisponible, h.ID, err)
		}
		pagina.Items = append(pagina.Items, d.modelo())
	}
	return pagina, nil
}

// ConsultaBusqueda traduce los filtros (ya validados) a la consulta de
// OpenSearch de ADR-006. Todos los filtros van en contexto filter (no puntúan
// y se cachean); solo el texto libre aporta relevancia. El orden siempre
// desempata por id para que las páginas sean estables.
func ConsultaBusqueda(f models.FiltrosBusqueda) map[string]any {
	filtros := []any{term("publicada", true)}
	if f.Destino != "" {
		filtros = append(filtros, term("destino", models.NormalizarDestino(f.Destino)))
	}
	if f.Tipo != "" {
		filtros = append(filtros, term("tipo", string(f.Tipo)))
	}
	if f.SubtipoTransporte != "" {
		filtros = append(filtros, term("subtipoTransporte", string(f.SubtipoTransporte)))
	}
	if f.Desde != "" || f.Hasta != "" {
		rango := map[string]any{}
		if f.Desde != "" {
			rango["gte"] = f.Desde
		}
		if f.Hasta != "" {
			rango["lte"] = f.Hasta + "||/d" // hasta el final de ese día
		}
		filtros = append(filtros, map[string]any{"range": map[string]any{"fechaInicio": rango}})
	}
	if f.PrecioMin != nil || f.PrecioMax != nil {
		rango := map[string]any{}
		if f.PrecioMin != nil {
			rango["gte"] = *f.PrecioMin
		}
		if f.PrecioMax != nil {
			rango["lte"] = *f.PrecioMax
		}
		filtros = append(filtros, map[string]any{"range": map[string]any{"precio": rango}})
	}
	consulta := map[string]any{"filter": filtros}
	if f.Q != "" {
		// cross_fields: cada palabra puede estar en cualquiera de los campos
		// ("lago córdoba" = "lago" en la descripción y "córdoba" en el destino),
		// pero tienen que aparecer todas.
		consulta["must"] = []any{map[string]any{"multi_match": map[string]any{
			"query":    f.Q,
			"type":     "cross_fields",
			"fields":   []string{"titulo^2", "descripcion", "destino.texto^2"},
			"operator": "and",
		}}}
	}

	desempate := map[string]any{"id": "asc"}
	var orden []any
	switch f.Orden {
	case models.OrdenPrecio:
		orden = []any{map[string]any{"precio": "asc"}, desempate}
	case models.OrdenPrecioDesc:
		orden = []any{map[string]any{"precio": "desc"}, desempate}
	case models.OrdenFechaDesc:
		orden = []any{map[string]any{"fechaInicio": "desc"}, desempate}
	case models.OrdenFecha:
		orden = []any{map[string]any{"fechaInicio": "asc"}, desempate}
	default:
		if f.Q != "" {
			orden = []any{"_score", desempate}
		} else {
			orden = []any{map[string]any{"fechaInicio": "asc"}, desempate}
		}
	}
	return map[string]any{
		"query":            map[string]any{"bool": consulta},
		"sort":             orden,
		"from":             f.Desplazamiento(),
		"size":             f.Tamanio,
		"track_total_hits": models.MaxResultados,
	}
}

func term(campo string, valor any) map[string]any {
	return map[string]any{"term": map[string]any{campo: valor}}
}
