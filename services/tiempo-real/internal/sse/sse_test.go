package sse

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agencia/tiempo-real/internal/hub"
)

func servidor(t *testing.T, latido time.Duration) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.Nuevo()
	srv := httptest.NewServer(NuevoRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), h, latido))
	t.Cleanup(srv.Close)
	return srv, h
}

// abrir conecta y devuelve un lector de líneas del stream.
func abrir(t *testing.T, url string, headers map[string]string) (*http.Response, *bufio.Reader) {
	t.Helper()
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res, bufio.NewReader(res.Body)
}

// esperarLinea lee hasta encontrar una línea que empiece con prefijo.
func esperarLinea(t *testing.T, r *bufio.Reader, prefijo string) string {
	t.Helper()
	listo := make(chan string, 1)
	go func() {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				close(listo)
				return
			}
			if strings.HasPrefix(l, prefijo) {
				listo <- strings.TrimSpace(l)
				return
			}
		}
	}()
	select {
	case l, ok := <-listo:
		if !ok {
			t.Fatalf("el stream terminó antes de %q", prefijo)
		}
		return l
	case <-time.After(2 * time.Second):
		t.Fatalf("no llegó %q", prefijo)
	}
	return ""
}

func TestStreamDeOfertasEntregaLosEventosDeEsasOfertas(t *testing.T) {
	srv, h := servidor(t, time.Minute)
	res, r := abrir(t, srv.URL+"/api/tiempo-real/ofertas?ids=of-1,of-2", nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content-type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	esperarLinea(t, r, ": conectado")

	h.PublicarOferta("of-2", hub.Evento{Nombre: "disponibilidad", Datos: []byte(`{"ofertaId":"of-2","version":3}`)})
	if l := esperarLinea(t, r, "event:"); l != "event: disponibilidad" {
		t.Fatalf("línea %q", l)
	}
	if l := esperarLinea(t, r, "data:"); l != `data: {"ofertaId":"of-2","version":3}` {
		t.Fatalf("línea %q", l)
	}
}

func TestMandaLatidosParaQueNoSeCorteLaConexion(t *testing.T) {
	srv, _ := servidor(t, 50*time.Millisecond)
	_, r := abrir(t, srv.URL+"/api/tiempo-real/ofertas?ids=of-1", nil)
	esperarLinea(t, r, ": latido")
}

func TestMisRetencionesUsaElUsuarioQuePoneElGateway(t *testing.T) {
	srv, h := servidor(t, time.Minute)
	_, r := abrir(t, srv.URL+"/api/tiempo-real/mis-retenciones", map[string]string{"X-Usuario-Id": "u-1"})
	esperarLinea(t, r, ": conectado")
	h.PublicarUsuario("u-1", hub.Evento{Nombre: "retencion-vencida", Datos: []byte(`{}`)})
	esperarLinea(t, r, "event: retencion-vencida")
}

func TestValidaLaSolicitud(t *testing.T) {
	srv, _ := servidor(t, time.Minute)
	ids := make([]string, MaxOfertas+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("of-%d", i)
	}
	muchas := strings.Join(ids, ",")
	casos := map[string]struct {
		ruta    string
		headers map[string]string
		status  int
	}{
		"sin ids":             {"/api/tiempo-real/ofertas", nil, http.StatusBadRequest},
		"demasiadas ofertas":  {"/api/tiempo-real/ofertas?ids=" + muchas, nil, http.StatusBadRequest},
		"retenciones sin JWT": {"/api/tiempo-real/mis-retenciones", nil, http.StatusUnauthorized},
	}
	for nombre, c := range casos {
		res, _ := abrir(t, srv.URL+c.ruta, c.headers)
		if res.StatusCode != c.status {
			t.Errorf("%s: status %d, se esperaba %d", nombre, res.StatusCode, c.status)
		}
	}
}

func TestAlCerrarLaConexionSeDaDeBaja(t *testing.T) {
	srv, h := servidor(t, time.Minute)
	ctx, cancelar := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/tiempo-real/ofertas?ids=of-1", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	esperarLinea(t, bufio.NewReader(res.Body), ": conectado")
	cancelar()
	res.Body.Close()
	limite := time.Now().Add(2 * time.Second)
	for h.PublicarOferta("of-1", hub.Evento{}) != 0 {
		if time.Now().After(limite) {
			t.Fatal("la suscripción siguió activa después de cerrar la conexión")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
