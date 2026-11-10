package repository

// Tests de la caché sin Redis: verifican el comportamiento ante fallas de ADR-007.
// No necesitan Docker.

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"agencia/catalogo/internal/models"
)

func cacheSinRedis(t *testing.T) *Cache {
	t.Helper()
	// Puerto 1: nadie escucha, así que cada operación falla enseguida.
	c, err := NuevaCache(ConfigCache{URL: "redis://127.0.0.1:1/0", Habilitada: true, DuracionCorte: 200 * time.Millisecond},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Cerrar)
	return c
}

// Con Redis caído, la caché informa un fallo (y quien llama lee MongoDB). Después
// de 5 errores seguidos deja de consultar Redis durante la pausa, y al terminar
// la pausa lo vuelve a probar.
func TestCacheCortaDespuesDeErroresSeguidos(t *testing.T) {
	c := cacheSinRedis(t)
	for i := range 5 {
		if !c.disponible() {
			t.Fatalf("cortó antes de tiempo, en el intento %d", i+1)
		}
		if _, ok := c.ObtenerOferta(ctx, "of-1"); ok {
			t.Fatal("sin Redis no puede haber acierto")
		}
	}
	if c.disponible() {
		t.Fatal("después de 5 errores seguidos debería dejar de consultar Redis")
	}
	// Durante la pausa no intenta: responde fallo al instante, sin esperar el timeout.
	inicio := time.Now()
	c.ObtenerOferta(ctx, "of-1")
	c.GuardarOferta(ctx, models.Oferta{ID: "of-1"})
	c.BorrarOferta(ctx, "of-1")
	if time.Since(inicio) > 20*time.Millisecond {
		t.Fatalf("en pausa no debería tocar Redis (tardó %v)", time.Since(inicio))
	}

	time.Sleep(250 * time.Millisecond)
	if !c.disponible() {
		t.Fatal("pasada la pausa debería volver a probar Redis")
	}
	// Sigue caído: un solo error más vuelve a cortar.
	c.ObtenerOferta(ctx, "of-1")
	if c.disponible() {
		t.Fatal("al reintentar con Redis todavía caído, debería volver a cortar enseguida")
	}
}

func TestCacheDeshabilitada(t *testing.T) {
	c, err := NuevaCache(ConfigCache{URL: "redis://127.0.0.1:1/0", Habilitada: false}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.disponible() {
		t.Fatal("con CACHE_HABILITADA=false no se consulta Redis")
	}
	if _, ok := c.ObtenerOferta(ctx, "of-1"); ok {
		t.Fatal("sin caché nunca hay acierto")
	}
}
