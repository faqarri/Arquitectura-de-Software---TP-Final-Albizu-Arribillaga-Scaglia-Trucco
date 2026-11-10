package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const secretoPrueba = "secreto-de-prueba-del-gateway-de-32-caracteres"

func TestProxyValidaTokenYReemplazaHeadersDeIdentidad(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ruta": r.URL.RequestURI(), "id": r.Header.Get(headerUsuarioID),
			"email": r.Header.Get(headerUsuarioEmail), "rol": r.Header.Get(headerUsuarioRol),
			"correlacion": r.Header.Get(headerCorrelacion),
		})
	}))
	defer upstream.Close()
	handler := nuevoHandlerPrueba(t, upstream.URL, 120)

	req := httptest.NewRequest(http.MethodGet, "/api/carrito?detalle=1", nil)
	req.Header.Set("Authorization", "Bearer "+tokenPrueba(t, "cliente-12", "ana@example.com", "cliente"))
	req.Header.Set(headerUsuarioID, "admin-falso")
	req.Header.Set(headerUsuarioEmail, "falso@example.com")
	req.Header.Set(headerUsuarioRol, "admin")
	req.Header.Set(headerCorrelacion, "web-123")
	res := nuevoRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var recibido map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &recibido); err != nil {
		t.Fatal(err)
	}
	if recibido["id"] != "cliente-12" || recibido["email"] != "ana@example.com" || recibido["rol"] != "cliente" {
		t.Fatalf("identidad upstream no provino del JWT: %+v", recibido)
	}
	if recibido["ruta"] != "/api/carrito?detalle=1" || recibido["correlacion"] != "web-123" {
		t.Fatalf("ruta o correlación no propagada: %+v", recibido)
	}
	if res.Header().Get(headerCorrelacion) != "web-123" {
		t.Fatalf("respuesta sin id de correlación: %v", res.Header())
	}
}

func TestProteccionDeRutasYRechazoDeRutasInternas(t *testing.T) {
	handler := nuevoHandlerPrueba(t, "http://127.0.0.1:1", 120)
	pruebas := []struct {
		nombre string
		metodo string
		ruta   string
		token  string
		want   int
	}{
		{"sin token", http.MethodGet, "/api/compras", "", http.StatusUnauthorized},
		{"token malformado", http.MethodGet, "/api/carrito", "no-es-jwt", http.StatusUnauthorized},
		{"cliente no administra", http.MethodPost, "/api/ofertas", tokenPrueba(t, "c-1", "c@example.com", "cliente"), http.StatusForbidden},
		{"admin no usa carrito de cliente", http.MethodGet, "/api/carrito", tokenPrueba(t, "a-1", "a@example.com", "admin"), http.StatusForbidden},
		{"ruta interna", http.MethodGet, "/internal/retenciones", tokenPrueba(t, "a-1", "a@example.com", "admin"), http.StatusNotFound},
		{"ruta interna bajo api", http.MethodGet, "/api/internal/retenciones", tokenPrueba(t, "a-1", "a@example.com", "admin"), http.StatusNotFound},
	}
	for _, prueba := range pruebas {
		t.Run(prueba.nombre, func(t *testing.T) {
			req := httptest.NewRequest(prueba.metodo, prueba.ruta, nil)
			if prueba.token != "" {
				req.Header.Set("Authorization", "Bearer "+prueba.token)
			}
			res := nuevoRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != prueba.want {
				t.Fatalf("status = %d, se esperaba %d: %s", res.Code, prueba.want, res.Body.String())
			}
		})
	}
}

func TestRutaPublicaEliminaHeadersDeIdentidadYGeneraCorrelacion(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": r.Header.Get(headerUsuarioID), "email": r.Header.Get(headerUsuarioEmail),
			"rol": r.Header.Get(headerUsuarioRol), "correlacion": r.Header.Get(headerCorrelacion),
		})
	}))
	defer upstream.Close()
	handler := nuevoHandlerPrueba(t, upstream.URL, 120)

	req := httptest.NewRequest(http.MethodPost, "/api/usuarios", strings.NewReader(`{"nombre":"Ana"}`))
	req.Header.Set(headerUsuarioID, "falso")
	req.Header.Set(headerUsuarioEmail, "falso@example.com")
	req.Header.Set(headerUsuarioRol, "admin")
	req.Header.Set(headerCorrelacion, "no valido")
	res := nuevoRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var recibido map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &recibido); err != nil {
		t.Fatal(err)
	}
	if recibido["id"] != "" || recibido["email"] != "" || recibido["rol"] != "" {
		t.Fatalf("se propagó identidad no verificada en ruta pública: %+v", recibido)
	}
	if recibido["correlacion"] == "" || recibido["correlacion"] != res.Header().Get(headerCorrelacion) {
		t.Fatalf("no se reemplazó el id de correlación inválido: %+v", recibido)
	}
}

func TestRateLimitPorUsuario(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	handler := nuevoHandlerPrueba(t, upstream.URL, 1)
	token := tokenPrueba(t, "cliente-1", "ana@example.com", "cliente")
	for i := 0; i < 21; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/carrito", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := nuevoRecorder()
		handler.ServeHTTP(res, req)
		want := http.StatusNoContent
		if i == 20 {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("solicitud %d: status = %d, se esperaba %d", i+1, res.Code, want)
		}
	}
}

type recorderConCloseNotify struct {
	*httptest.ResponseRecorder
	notificacion chan bool
}

func nuevoRecorder() *recorderConCloseNotify {
	return &recorderConCloseNotify{ResponseRecorder: httptest.NewRecorder(), notificacion: make(chan bool)}
}

func (r *recorderConCloseNotify) CloseNotify() <-chan bool {
	return r.notificacion
}

func nuevoHandlerPrueba(t *testing.T, upstream string, limite int) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := Config{
		SecretoJWT: secretoPrueba, UsuariosURL: upstream, ReservasURL: upstream,
		CatalogoURLs: []string{upstream}, LimiteSolicitudesMin: limite,
	}
	handler, cerrar, err := NuevoRouter(cfg, slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cerrar)
	return handler
}

func tokenPrueba(t *testing.T, id, email, rol string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Email: email, Rol: rol, RegisteredClaims: jwt.RegisteredClaims{
			Subject: id, IssuedAt: jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	firmado, err := token.SignedString([]byte(secretoPrueba))
	if err != nil {
		t.Fatal(err)
	}
	return firmado
}
