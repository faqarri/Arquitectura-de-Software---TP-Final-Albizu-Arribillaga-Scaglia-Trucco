package repository

// Tests de integración contra MongoDB real (contenedor mongo:7, el del
// docker-compose). El contenedor lo levanta TestMain.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"agencia/catalogo/internal/models"
)

func ofertaDePrueba() models.Oferta {
	return models.Oferta{
		ID: "of-" + uuid.NewString(), Tipo: models.Transporte, SubtipoTransporte: models.Avion,
		Titulo: "Vuelo Córdoba - Bariloche", Destino: "Bariloche", Origen: "Córdoba",
		Desde: time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC), Hasta: time.Date(2026, 7, 12, 10, 30, 0, 0, time.UTC),
		Precio: 185000.5,
	}
}

// ADR-006: cada escritura sube la versión y marca la oferta para publicar
// OfertaActualizada; desmarcar con una versión vieja no borra la marca.
func TestOfertasVersionYOutboxEnElDocumento(t *testing.T) {
	requiereMySQL(t)
	repo := NuevoOfertas(mongoDB)
	o := ofertaDePrueba()

	creada, err := repo.Crear(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	if creada.Version != 1 || !creada.Publicada {
		t.Fatalf("una oferta nueva arranca publicada en la versión 1: %+v", creada)
	}
	leida, err := repo.Obtener(ctx, o.ID)
	if err != nil || leida.Titulo != o.Titulo || leida.Precio != o.Precio || !leida.Desde.Equal(o.Desde) {
		t.Fatalf("leída distinta de la creada: %+v, %v", leida, err)
	}

	leida.Precio = 199000
	editada, err := repo.Actualizar(ctx, leida)
	if err != nil || editada.Version != 2 || editada.Precio != 199000 {
		t.Fatalf("la edición debería subir a la versión 2: %+v, %v", editada, err)
	}

	pendiente := func() *models.CambioPendiente {
		t.Helper()
		ps, err := repo.PendientesDePublicar(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.OfertaID == o.ID {
				return &p
			}
		}
		return nil
	}
	if p := pendiente(); p == nil || p.Version != 2 {
		t.Fatalf("la oferta debería estar pendiente de publicar en la versión 2: %+v", p)
	}
	// Se publicó la versión 1 mientras alguien la editaba: la marca sigue.
	if err := repo.MarcarPublicada(ctx, models.CambioPendiente{OfertaID: o.ID, Version: 1}); err != nil {
		t.Fatal(err)
	}
	if pendiente() == nil {
		t.Fatal("desmarcar con una versión vieja no debería borrar la marca")
	}
	if err := repo.MarcarPublicada(ctx, models.CambioPendiente{OfertaID: o.ID, Version: 2}); err != nil {
		t.Fatal(err)
	}
	if pendiente() != nil {
		t.Fatal("con la versión actual se desmarca")
	}

	// Baja lógica: no se borra, queda despublicada y vuelve a quedar pendiente.
	baja, err := repo.Despublicar(ctx, o.ID)
	if err != nil || baja.Publicada || baja.Version != 3 || pendiente() == nil {
		t.Fatalf("despublicar: %+v, %v", baja, err)
	}

	if _, err := repo.Obtener(ctx, "of-no-existe"); !errors.Is(err, models.ErrNoEncontrado) {
		t.Fatalf("esperaba ErrNoEncontrado: %v", err)
	}
}

func TestPacksConOpciones(t *testing.T) {
	requiereMySQL(t)
	ofertas, packs := NuevoOfertas(mongoDB), NuevoPacks(mongoDB)
	a, b := ofertaDePrueba(), ofertaDePrueba()
	for _, o := range []models.Oferta{a, b} {
		if _, err := ofertas.Crear(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	varias, err := ofertas.ObtenerVarias(ctx, []string{a.ID, b.ID, "of-no-existe"})
	if err != nil || len(varias) != 2 {
		t.Fatalf("ObtenerVarias: %d, %v", len(varias), err)
	}

	p := models.Pack{ID: "pk-" + uuid.NewString(), Titulo: "Bariloche", Destino: "Bariloche", DescuentoPorcentaje: 10, OpcionIDs: []string{a.ID, b.ID}}
	if err := packs.Crear(ctx, p); err != nil {
		t.Fatal(err)
	}
	leido, err := packs.Obtener(ctx, p.ID)
	if err != nil || len(leido.OpcionIDs) != 2 || leido.DescuentoPorcentaje != 10 {
		t.Fatalf("pack leído: %+v, %v", leido, err)
	}
}
