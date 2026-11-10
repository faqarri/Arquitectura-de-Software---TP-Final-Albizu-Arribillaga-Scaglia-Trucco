package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"agencia/pkg/observabilidad"
)

const (
	headerUsuarioID    = "X-Usuario-Id"
	headerUsuarioEmail = "X-Usuario-Email"
	headerUsuarioRol   = "X-Usuario-Rol"
	headerCorrelacion  = "X-Correlation-Id"
)

type servidor struct {
	cfg       Config
	log       *slog.Logger
	limitador *limitadorPorCliente
	upstreams map[string]*httputil.ReverseProxy
	catalogo  *poolCatalogo
}

type ruta struct {
	servicio string
	patron   string
	rol      string
	sse      bool
}

var metricasGateway = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_solicitudes_total",
	Help: "Solicitudes del gateway a servicios upstream, desglosadas por instancia.",
}, []string{"instancia", "ruta", "estado"})

var saludInstanciaCatalogo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "gateway_instancia_sana",
	Help: "Indica si una instancia de Catálogo está disponible en el pool.",
}, []string{"instancia"})

func init() {
	prometheus.MustRegister(metricasGateway, saludInstanciaCatalogo)
}

func NuevoRouter(cfg Config, log *slog.Logger) (http.Handler, func(), error) {
	if err := cfg.Validar(); err != nil {
		return nil, nil, err
	}
	if log == nil {
		return nil, nil, errors.New("el logger del gateway es obligatorio")
	}
	upstreams := map[string]string{
		"usuarios": cfg.UsuariosURL, "reservas": cfg.ReservasURL,
	}
	if cfg.TiempoRealURL != "" {
		upstreams["tiempo-real"] = cfg.TiempoRealURL
	}
	proxies := make(map[string]*httputil.ReverseProxy, len(upstreams))
	for servicio, direccion := range upstreams {
		proxy, err := nuevoProxy(servicio, direccion, log)
		if err != nil {
			return nil, nil, err
		}
		proxies[servicio] = proxy
	}

	pool, err := nuevoPoolCatalogo(cfg.CatalogoURLs, log)
	if err != nil {
		return nil, nil, err
	}
	s := &servidor{
		cfg: cfg, log: log, limitador: nuevoLimitador(cfg.LimiteSolicitudesMin),
		upstreams: proxies, catalogo: pool,
	}
	r := gin.New()
	r.Use(gin.Recovery(), prepararSolicitud(), observabilidad.MiddlewareMetricas(), observabilidad.MiddlewareGin(log))
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"estado": "ok"}) })
	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))
	r.Any("/api", s.enrutar)
	r.Any("/api/*path", s.enrutar)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/internal/") || c.Request.URL.Path == "/internal" {
			responderProblema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
			return
		}
		responderProblema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
	})
	return r, pool.cerrar, nil
}

func (s *servidor) enrutar(c *gin.Context) {
	r := buscarRuta(c.Request.Method, c.Request.URL.Path, s.cfg.TiempoRealURL != "")
	if r == nil {
		if strings.HasPrefix(c.Request.URL.Path, "/api/internal/") {
			responderProblema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
			return
		}
		responderProblema(c, http.StatusNotFound, "no-encontrado", "Recurso no encontrado", "La ruta no existe.")
		return
	}

	var quien identidad
	if r.rol != "" {
		var err error
		quien, err = validarToken(c.GetHeader("Authorization"), s.cfg.SecretoJWT)
		if err != nil {
			responderProblema(c, http.StatusUnauthorized, "no-autenticado", "No autenticado", "Iniciá sesión para continuar.")
			return
		}
		if quien.Rol != r.rol {
			responderProblema(c, http.StatusForbidden, "sin-permiso", "Sin permiso", "Tu rol no permite realizar esta operación.")
			return
		}
		claveLimite := "usuario:" + quien.ID
		if !s.limitador.permitir(claveLimite, time.Now()) {
			c.Header("Retry-After", "1")
			responderProblema(c, http.StatusTooManyRequests, "limite-solicitudes", "Demasiadas solicitudes",
				"Esperá un momento antes de volver a intentarlo.")
			return
		}
		c.Request.Header.Set(headerUsuarioID, quien.ID)
		c.Request.Header.Set(headerUsuarioEmail, quien.Email)
		c.Request.Header.Set(headerUsuarioRol, quien.Rol)
	} else {
		claveLimite := "ip:" + ipCliente(c.Request)
		if !s.limitador.permitir(claveLimite, time.Now()) {
			c.Header("Retry-After", "1")
			responderProblema(c, http.StatusTooManyRequests, "limite-solicitudes", "Demasiadas solicitudes",
				"Esperá un momento antes de volver a intentarlo.")
			return
		}
	}

	if r.sse {
		c.Writer.Header().Set("X-Accel-Buffering", "no")
	}
	if r.servicio == "catalogo" {
		s.catalogo.ServeHTTP(c.Writer, c.Request)
		return
	}
	s.upstreams[r.servicio].ServeHTTP(c.Writer, c.Request)
}

func buscarRuta(metodo, path string, tiempoReal bool) *ruta {
	if strings.HasPrefix(path, "/internal/") || path == "/internal" || strings.HasPrefix(path, "/api/internal/") {
		return nil
	}
	switch {
	case path == "/api/usuarios" && metodo == http.MethodPost:
		return &ruta{servicio: "usuarios", patron: "/api/usuarios"}
	case path == "/api/sesiones" && metodo == http.MethodPost:
		return &ruta{servicio: "usuarios", patron: "/api/sesiones"}
	case path == "/api/busqueda" && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: "/api/busqueda"}
	case path == "/api/ofertas" && metodo == http.MethodPost:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas", rol: "admin"}
	case path == "/api/ofertas" && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas"}
	case rutaOferta(path, "/disponibilidad") && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas/:id/disponibilidad"}
	case rutaOferta(path, "/guias") && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas/:id/guias"}
	case rutaOferta(path, "") && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas/:id"}
	case rutaOferta(path, "") && metodo == http.MethodPut:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas/:id", rol: "admin"}
	case rutaOferta(path, "") && metodo == http.MethodDelete:
		return &ruta{servicio: "catalogo", patron: "/api/ofertas/:id", rol: "admin"}
	case (path == "/api/packs" || rutaPack(path)) && metodo == http.MethodGet:
		return &ruta{servicio: "catalogo", patron: patronPack(path)}
	case path == "/api/packs" && metodo == http.MethodPost:
		return &ruta{servicio: "catalogo", patron: "/api/packs", rol: "admin"}
	case path == "/api/carrito" && metodo == http.MethodGet:
		return &ruta{servicio: "reservas", patron: "/api/carrito", rol: "cliente"}
	case path == "/api/carrito/items" && metodo == http.MethodPost:
		return &ruta{servicio: "reservas", patron: "/api/carrito/items", rol: "cliente"}
	case rutaCarritoItem(path) && metodo == http.MethodDelete:
		return &ruta{servicio: "reservas", patron: "/api/carrito/items/:itemId", rol: "cliente"}
	case path == "/api/compras" && (metodo == http.MethodGet || metodo == http.MethodPost):
		return &ruta{servicio: "reservas", patron: "/api/compras", rol: "cliente"}
	case rutaCompra(path) && metodo == http.MethodGet:
		return &ruta{servicio: "reservas", patron: "/api/compras/:compraId", rol: "cliente"}
	case path == "/api/guias" && (metodo == http.MethodGet || metodo == http.MethodPost):
		return &ruta{servicio: "reservas", patron: "/api/guias", rol: "cliente"}
	case tiempoReal && path == "/api/tiempo-real/ofertas" && metodo == http.MethodGet:
		return &ruta{servicio: "tiempo-real", patron: "/api/tiempo-real/ofertas", sse: true}
	case tiempoReal && path == "/api/tiempo-real/mis-retenciones" && metodo == http.MethodGet:
		return &ruta{servicio: "tiempo-real", patron: "/api/tiempo-real/mis-retenciones", rol: "cliente", sse: true}
	default:
		return nil
	}
}

func rutaOferta(path, sufijo string) bool {
	partes := strings.Split(path, "/")
	if len(partes) != 4+boolComoInt(sufijo != "") || partes[1] != "api" || partes[2] != "ofertas" || partes[3] == "" {
		return false
	}
	return sufijo == "" && len(partes) == 4 || sufijo != "" && partes[4] == strings.TrimPrefix(sufijo, "/")
}

func boolComoInt(condicion bool) int {
	if condicion {
		return 1
	}
	return 0
}

func rutaPack(path string) bool {
	partes := strings.Split(path, "/")
	return len(partes) == 4 && partes[1] == "api" && partes[2] == "packs" && partes[3] != ""
}

func patronPack(path string) string {
	if rutaPack(path) {
		return "/api/packs/:id"
	}
	return "/api/packs"
}

func rutaCarritoItem(path string) bool {
	partes := strings.Split(path, "/")
	return len(partes) == 5 && partes[1] == "api" && partes[2] == "carrito" && partes[3] == "items" && partes[4] != ""
}

func rutaCompra(path string) bool {
	partes := strings.Split(path, "/")
	return len(partes) == 4 && partes[1] == "api" && partes[2] == "compras" && partes[3] != ""
}

func prepararSolicitud() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, header := range []string{headerUsuarioID, headerUsuarioEmail, headerUsuarioRol} {
			c.Request.Header.Del(header)
		}
		id := c.GetHeader(headerCorrelacion)
		if !idCorrelacionValido(id) {
			id = uuid.NewString()
		}
		c.Request.Header.Set(headerCorrelacion, id)
		c.Header(headerCorrelacion, id)
		c.Next()
	}
}

func idCorrelacionValido(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c)) {
			return false
		}
	}
	return true
}

func ipCliente(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func rutaMetrica(r *http.Request) string {
	if r == nil {
		return "unmatched"
	}
	if encontrada := buscarRuta(r.Method, r.URL.Path, true); encontrada != nil {
		return encontrada.patron
	}
	return "unmatched"
}

func responderProblema(c *gin.Context, estado int, codigo, titulo, detalle string) {
	responderProblemaHTTP(c.Writer, estado, codigo, titulo, detalle)
	c.Abort()
}

func responderProblemaHTTP(w http.ResponseWriter, estado int, codigo, titulo, detalle string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(estado)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "https://agencia-viajes.example/errores/" + codigo, "title": titulo,
		"status": estado, "code": codigo, "detail": detalle,
	})
}
