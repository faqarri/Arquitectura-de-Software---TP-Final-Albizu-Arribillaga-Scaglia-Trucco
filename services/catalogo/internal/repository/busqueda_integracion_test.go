package repository

// Test de integración contra OpenSearch real (la misma imagen del
// docker-compose). Verifica ADR-006: versiones externas (un evento repetido o
// viejo no pisa el índice), filtros, normalización del destino, ofertas
// despublicadas fuera de los resultados y el cambio atómico del alias.
// Necesita Docker; con go test -short se saltea.

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"agencia/catalogo/internal/models"
)

func levantarOpenSearch(t *testing.T) string {
	t.Helper()
	c, err := testcontainers.Run(ctx, "opensearchproject/opensearch:2.15.0",
		testcontainers.WithEnv(map[string]string{
			"discovery.type":          "single-node",
			"DISABLE_SECURITY_PLUGIN": "true",
			"OPENSEARCH_JAVA_OPTS":    "-Xms512m -Xmx512m",
		}),
		testcontainers.WithExposedPorts("9200/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/_cluster/health").WithPort("9200/tcp").WithStartupTimeout(3*time.Minute)),
	)
	if err != nil {
		t.Fatalf("no se pudo levantar OpenSearch: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	puerto, _ := c.MappedPort(ctx, "9200/tcp")
	return fmt.Sprintf("http://%s:%s", host, puerto.Port())
}

func ofertaIndexable(id, destino string, tipo models.TipoOferta, precio float64, dia int) models.Oferta {
	o := models.Oferta{
		ID: id, Tipo: tipo, Titulo: "Oferta en " + destino, Descripcion: "Paseo por el lago y el cerro",
		Destino: destino, Desde: time.Date(2026, 7, dia, 9, 0, 0, 0, time.UTC),
		Hasta: time.Date(2026, 7, dia+2, 18, 0, 0, 0, time.UTC), Precio: precio, Publicada: true,
		Version: 1, ActualizadaEn: time.Now().UTC(),
	}
	if tipo == models.Transporte {
		o.SubtipoTransporte = models.Avion
		o.Origen = "Buenos Aires"
	}
	return o
}

func buscarOFallar(t *testing.T, idx *IndiceBusqueda, f models.FiltrosBusqueda) []string {
	t.Helper()
	if err := f.Validar(); err != nil {
		t.Fatal(err)
	}
	p, err := idx.Buscar(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(p.Items))
	for i, o := range p.Items {
		ids[i] = o.ID
	}
	return ids
}

func TestIndiceDeBusqueda(t *testing.T) {
	requiereMySQL(t)
	idx, err := NuevoIndiceBusqueda(levantarOpenSearch(t), "ofertas")
	if err != nil {
		t.Fatal(err)
	}

	creado, err := idx.AsegurarIndice(ctx)
	if err != nil || !creado {
		t.Fatalf("la primera vez se crea el índice con el alias: %v %v", creado, err)
	}
	if creado, err = idx.AsegurarIndice(ctx); err != nil || creado {
		t.Fatalf("la segunda vez no se crea de nuevo: %v %v", creado, err)
	}

	ofertas := []models.Oferta{
		ofertaIndexable("of-a", "San Carlos de Bariloche", models.Excursion, 30000, 10),
		ofertaIndexable("of-b", "Córdoba", models.Transporte, 85000, 5),
		ofertaIndexable("of-c", "Córdoba", models.Estadia, 120000, 20),
	}
	for _, o := range ofertas {
		if nueva, err := idx.Indexar(ctx, o); err != nil || !nueva {
			t.Fatalf("indexar %s: %v %v", o.ID, nueva, err)
		}
	}

	t.Run("versiones externas", func(t *testing.T) {
		repetida := ofertas[0]
		if nueva, err := idx.Indexar(ctx, repetida); err != nil || nueva {
			t.Fatalf("la misma versión otra vez no es un error ni pisa: %v %v", nueva, err)
		}
		v3 := ofertas[0]
		v3.Version, v3.Precio = 3, 31000
		if nueva, err := idx.Indexar(ctx, v3); err != nil || !nueva {
			t.Fatalf("una versión más nueva se indexa: %v %v", nueva, err)
		}
		vieja := ofertas[0]
		vieja.Version, vieja.Precio = 2, 99999
		if nueva, err := idx.Indexar(ctx, vieja); err != nil || nueva {
			t.Fatalf("una versión vieja que llega tarde se descarta: %v %v", nueva, err)
		}
		if err := idx.Refrescar(ctx, "ofertas"); err != nil {
			t.Fatal(err)
		}
		// El filtro de destino es exacto pero ignora mayúsculas y acentos.
		p, err := idx.Buscar(ctx, models.FiltrosBusqueda{Destino: "SAN CARLOS DE BARILOCHE", Pagina: 1, Tamanio: 20})
		if err != nil || len(p.Items) != 1 {
			t.Fatalf("filtro de destino: %+v %v", p, err)
		}
		if p.Items[0].Precio != 31000 || p.Items[0].Version != 3 {
			t.Fatalf("quedó la versión 3: %+v", p.Items[0])
		}
	})

	t.Run("filtros y orden", func(t *testing.T) {
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Destino: "cordoba", Orden: models.OrdenPrecioDesc}); !slices.Equal(ids, []string{"of-c", "of-b"}) {
			t.Fatalf("destino sin acento y orden por precio descendente: %v", ids)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Tipo: models.Transporte, SubtipoTransporte: models.Avion}); !slices.Equal(ids, []string{"of-b"}) {
			t.Fatalf("tipo y subtipo: %v", ids)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Desde: "2026-07-05", Hasta: "2026-07-10"}); !slices.Equal(ids, []string{"of-b", "of-a"}) {
			t.Fatalf("rango de fechas inclusivo hasta el final del día, orden por fecha: %v", ids)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{PrecioMin: ptr(50000), PrecioMax: ptr(100000)}); !slices.Equal(ids, []string{"of-b"}) {
			t.Fatalf("rango de precio: %v", ids)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Q: "PASEO lago"}); len(ids) != 3 {
			t.Fatalf("texto libre sin distinguir mayúsculas: %v", ids)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Q: "lago cordoba"}); !slices.Equal(ids, []string{"of-b", "of-c"}) {
			t.Fatalf("texto libre: todas las palabras, también en el destino y sin acentos: %v", ids)
		}
		f := models.FiltrosBusqueda{Tamanio: 2, Pagina: 2}
		if ids := buscarOFallar(t, idx, f); !slices.Equal(ids, []string{"of-c"}) {
			t.Fatalf("segunda página de 2: %v", ids)
		}
	})

	t.Run("las despublicadas no aparecen", func(t *testing.T) {
		baja := ofertas[2]
		baja.Version, baja.Publicada = 2, false
		if _, err := idx.Indexar(ctx, baja); err != nil {
			t.Fatal(err)
		}
		_ = idx.Refrescar(ctx, "ofertas")
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Destino: "Córdoba"}); !slices.Equal(ids, []string{"of-b"}) {
			t.Fatalf("la oferta dada de baja no se devuelve: %v", ids)
		}
	})

	t.Run("reconstrucción con cambio atómico del alias", func(t *testing.T) {
		viejos, err := idx.IndicesDelAlias(ctx)
		if err != nil || !slices.Equal(viejos, []string{idx.NombreIndiceInicial()}) {
			t.Fatalf("el alias apunta al índice inicial: %v %v", viejos, err)
		}
		nuevo := idx.NombreIndiceNuevo(time.Now())
		if err := idx.CrearIndice(ctx, nuevo); err != nil {
			t.Fatal(err)
		}
		if err := idx.CargarLote(ctx, nuevo, []models.Oferta{ofertas[1], ofertaIndexable("of-d", "Mendoza", models.Estadia, 50000, 1)}); err != nil {
			t.Fatal(err)
		}
		// Repetir el lote no falla: los conflictos de versión se ignoran.
		if err := idx.CargarLote(ctx, nuevo, []models.Oferta{ofertas[1]}); err != nil {
			t.Fatal(err)
		}
		if err := idx.MoverAlias(ctx, nuevo, viejos); err != nil {
			t.Fatal(err)
		}
		if err := idx.Refrescar(ctx, nuevo); err != nil {
			t.Fatal(err)
		}
		if n, err := idx.Contar(ctx, nuevo); err != nil || n != 2 {
			t.Fatalf("el índice nuevo tiene los 2 documentos cargados: %d %v", n, err)
		}
		if err := idx.BorrarIndices(ctx, viejos); err != nil {
			t.Fatal(err)
		}
		if actuales, _ := idx.IndicesDelAlias(ctx); !slices.Equal(actuales, []string{nuevo}) {
			t.Fatalf("el alias quedó solo en el índice nuevo: %v", actuales)
		}
		if ids := buscarOFallar(t, idx, models.FiltrosBusqueda{Destino: "mendoza"}); !slices.Equal(ids, []string{"of-d"}) {
			t.Fatalf("la búsqueda usa el índice nuevo: %v", ids)
		}
	})
}
