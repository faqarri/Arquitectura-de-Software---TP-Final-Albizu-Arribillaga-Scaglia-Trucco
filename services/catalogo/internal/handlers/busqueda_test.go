package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Los parámetros mal escritos responden 400 con todos los problemas juntos,
// sin llegar a OpenSearch (Busqueda es nil: si se llamara, el test fallaría).
func TestBusquedaParametrosInvalidos(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NuevoRouter(Dependencias{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Instancia: "prueba"})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/busqueda?pagina=x&precioMin=barato&tamanio=60&orden=nombre", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var cuerpo struct {
		Code    string `json:"code"`
		Errores []struct {
			Campo string `json:"campo"`
		} `json:"errores"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cuerpo); err != nil {
		t.Fatal(err)
	}
	campos := map[string]bool{}
	for _, e := range cuerpo.Errores {
		campos[e.Campo] = true
	}
	for _, c := range []string{"pagina", "precioMin", "tamanio", "orden"} {
		if !campos[c] {
			t.Errorf("falta el error de %s: %s", c, rec.Body)
		}
	}
	if cuerpo.Code != "solicitud-invalida" {
		t.Errorf("code %q", cuerpo.Code)
	}
}
