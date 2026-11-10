package api_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"agencia/comprobantes/internal/adapters/api"
	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// consumidoresEnMemoria es un doble de ports.RepositorioConsumidores.
type consumidoresEnMemoria struct {
	mu     sync.Mutex
	emails map[string]string // grupo → email
	claves map[string]string // hash → grupo
	fallar error
}

func nuevosConsumidores() *consumidoresEnMemoria {
	return &consumidoresEnMemoria{emails: map[string]string{}, claves: map[string]string{}}
}

func (r *consumidoresEnMemoria) EmailDelGrupo(_ context.Context, grupo string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallar != nil {
		return "", r.fallar
	}
	email, ok := r.emails[grupo]
	if !ok {
		return "", ports.ErrNoEncontrado
	}
	return email, nil
}

func (r *consumidoresEnMemoria) AgregarClave(_ context.Context, grupo, email, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.emails[grupo]; ok && !strings.EqualFold(e, email) {
		return ports.ErrGrupoRegistrado
	}
	r.emails[grupo] = email
	r.claves[hash] = grupo
	return nil
}

func (r *consumidoresEnMemoria) GrupoPorClave(_ context.Context, hash string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallar != nil {
		return "", r.fallar
	}
	grupo, ok := r.claves[hash]
	if !ok {
		return "", ports.ErrNoEncontrado
	}
	return grupo, nil
}

// buzon es un doble de ports.Mailer que guarda los correos enviados.
type buzon struct {
	mu      sync.Mutex
	correos []ports.Correo
	fallar  error
}

func (b *buzon) Enviar(_ context.Context, c ports.Correo) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fallar != nil {
		return b.fallar
	}
	b.correos = append(b.correos, c)
	return nil
}

var formatoClave = regexp.MustCompile(`cmp_[A-Za-z0-9_-]{43}`)

// ultimaClave devuelve la API Key del último correo enviado.
func (b *buzon) ultimaClave(t *testing.T) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.correos) == 0 {
		t.Fatal("no se envió ningún correo")
	}
	clave := formatoClave.FindString(b.correos[len(b.correos)-1].Cuerpo)
	if clave == "" {
		t.Fatalf("el correo no tiene una API Key: %q", b.correos[len(b.correos)-1].Cuerpo)
	}
	return clave
}

type entornoRegistro struct {
	srv          *httptest.Server
	comprobantes *repoEnMemoria
	consumidores *consumidoresEnMemoria
	buzon        *buzon
}

func nuevoEntornoRegistro(t *testing.T) entornoRegistro {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := entornoRegistro{
		comprobantes: &repoEnMemoria{porID: map[string]*domain.Comprobante{}},
		consumidores: nuevosConsumidores(),
		buzon:        &buzon{},
	}
	fijas := map[string]string{claveA: "grupo-a"}
	e.srv = httptest.NewServer(api.NuevoRouter(api.Dependencias{
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Emitir:         application.NuevoEmitirComprobante(e.comprobantes),
		Obtener:        application.NuevoObtenerComprobante(e.comprobantes),
		Autenticar:     application.NuevoAutenticarConsumidor(e.consumidores, fijas),
		Registrar:      application.NuevoRegistrarConsumidor(e.consumidores, e.buzon, []string{"grupo-a"}),
		VerificarSalud: func(context.Context) error { return nil },
	}))
	t.Cleanup(e.srv.Close)
	return e
}

func registro(grupo, email string) string {
	return `{"grupo":"` + grupo + `","email":"` + email + `"}`
}

func TestRegistroEnviaLaClavePorCorreoYLaClaveSirve(t *testing.T) {
	e := nuevoEntornoRegistro(t)

	r := pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "contacto@grupo7.example"))
	esperarEstado(t, r, 202)
	if strings.Contains(r.crudo, "cmp_") {
		t.Fatalf("la respuesta no debe incluir la clave: %s", r.crudo)
	}
	if got := e.buzon.correos[0].Para; got != "contacto@grupo7.example" {
		t.Errorf("la clave se envió a %q", got)
	}
	clave := e.buzon.ultimaClave(t)
	for hash := range e.consumidores.claves {
		if strings.Contains(hash, "cmp_") || hash == clave {
			t.Fatal("la clave se guardó en claro")
		}
	}

	r = pedir(t, e.srv, "POST", "/v1/comprobantes", clave, "orden-grupo7-1", cuerpoValido)
	esperarEstado(t, r, 201)
	for _, c := range e.comprobantes.porID {
		if c.Consumidor != "grupo-7" {
			t.Errorf("el comprobante quedó a nombre de %q", c.Consumidor)
		}
	}
}

func TestRegistroConElMismoEmailDaOtraClaveYLaAnteriorSigueValiendo(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	esperarEstado(t, pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "contacto@grupo7.example")), 202)
	primera := e.buzon.ultimaClave(t)
	// El email se compara sin distinguir mayúsculas.
	esperarEstado(t, pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "Contacto@Grupo7.example")), 202)
	segunda := e.buzon.ultimaClave(t)

	if primera == segunda {
		t.Fatal("se esperaba una clave nueva")
	}
	for _, clave := range []string{primera, segunda} {
		esperarEstado(t, pedir(t, e.srv, "GET", "/v1/comprobantes/x", clave, "", ""), 404)
	}
}

func TestRegistroDeUnGrupoAjenoResponde409(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	esperarEstado(t, pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "contacto@grupo7.example")), 202)

	r := pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "intruso@example.com"))
	esperarProblema(t, r, 409, "grupo-registrado")
	if len(e.buzon.correos) != 1 {
		t.Error("no se debía enviar una clave al otro email")
	}
}

func TestRegistroInvalido(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	casos := map[string]string{
		"sin grupo":            `{"email":"a@b.example"}`,
		"sin email":            `{"grupo":"grupo-7"}`,
		"grupo con mayúscula":  registro("Grupo7", "a@b.example"),
		"grupo muy corto":      registro("g7", "a@b.example"),
		"grupo con espacios":   registro("grupo 7", "a@b.example"),
		"email inválido":       registro("grupo-7", "no-es-email"),
		"campo extra":          `{"grupo":"grupo-7","email":"a@b.example","clave":"x"}`,
		"grupo interno":        registro("interno", "a@b.example"),
		"grupo con clave fija": registro("grupo-a", "a@b.example"),
	}
	for nombre, cuerpo := range casos {
		r := pedir(t, e.srv, "POST", "/v1/api-keys", "", "", cuerpo)
		if r.estado != 400 {
			t.Errorf("%s: estado %d, se esperaba 400: %s", nombre, r.estado, r.crudo)
		}
	}
	if len(e.buzon.correos) != 0 {
		t.Error("no se debía enviar ningún correo")
	}
}

func TestRegistroSinCorreoNoGuardaLaClave(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	e.buzon.fallar = errors.New("SMTP caído")

	r := pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-7", "contacto@grupo7.example"))
	esperarProblema(t, r, 503, "no-disponible")
	if len(e.consumidores.emails) != 0 || len(e.consumidores.claves) != 0 {
		t.Error("no debía quedar registrado un grupo cuya clave no se envió")
	}
}

func TestRegistroLimitadoPorIP(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	for i := range api.RegistrosPorHora {
		grupo := "grupo-limite-" + string(rune('a'+i))
		esperarEstado(t, pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro(grupo, "x@y.example")), 202)
	}
	r := pedir(t, e.srv, "POST", "/v1/api-keys", "", "", registro("grupo-limite-z", "x@y.example"))
	esperarProblema(t, r, 429, "demasiadas-solicitudes")
	if r.headers.Get("Retry-After") == "" {
		t.Error("falta el header Retry-After")
	}
}

func TestAutenticacionConLaBaseCaidaResponde503(t *testing.T) {
	e := nuevoEntornoRegistro(t)
	e.consumidores.fallar = errors.New("base caída")
	esperarProblema(t, pedir(t, e.srv, "GET", "/v1/comprobantes/x", "cmp_desconocida", "", ""), 503, "no-disponible")
	// Las claves fijas no dependen de la base.
	esperarEstado(t, pedir(t, e.srv, "GET", "/v1/comprobantes/x", claveA, "", ""), 404)
}
