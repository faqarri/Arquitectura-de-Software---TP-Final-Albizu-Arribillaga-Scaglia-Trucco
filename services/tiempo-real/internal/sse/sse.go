// Package sse expone las suscripciones por Server-Sent Events. Las rutas son las
// mismas que publica el gateway (ADR-015).
package sse

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agencia/pkg/observabilidad"
	"agencia/tiempo-real/internal/hub"
)

const (
	// MaxOfertas por conexión, para que una sola no se suscriba a todo el catálogo.
	MaxOfertas = 50
	// Latido: un comentario SSE para que proxies y navegadores no corten la
	// conexión por inactividad.
	Latido = 20 * time.Second
	// headerUsuarioID lo pone el gateway después de validar el JWT.
	headerUsuarioID = "X-Usuario-Id"
)

// NuevoRouter arma las rutas del servicio.
func NuevoRouter(log *slog.Logger, h *hub.Hub, latido time.Duration) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), observabilidad.MiddlewareGin(log))
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"estado": "ok"}) })
	r.GET("/metrics", gin.WrapH(observabilidad.HandlerMetricas()))

	r.GET("/api/tiempo-real/ofertas", func(c *gin.Context) {
		ids, err := parsearIDs(c.Query("ids"))
		if err != "" {
			problema(c, http.StatusBadRequest, "solicitud-invalida", err)
			return
		}
		transmitir(c, h, h.SuscribirOfertas(ids), latido)
	})
	r.GET("/api/tiempo-real/mis-retenciones", func(c *gin.Context) {
		usuario := c.GetHeader(headerUsuarioID)
		if usuario == "" {
			problema(c, http.StatusUnauthorized, "no-autenticado", "Iniciá sesión para seguir tus reservas.")
			return
		}
		transmitir(c, h, h.SuscribirUsuario(usuario), latido)
	})
	return r
}

func parsearIDs(crudo string) ([]string, string) {
	vistos := map[string]bool{}
	var ids []string
	for _, id := range strings.Split(crudo, ",") {
		id = strings.TrimSpace(id)
		if id == "" || vistos[id] {
			continue
		}
		vistos[id] = true
		ids = append(ids, id)
	}
	switch {
	case len(ids) == 0:
		return nil, "Indicá al menos una oferta en ids (separadas por coma)."
	case len(ids) > MaxOfertas:
		return nil, fmt.Sprintf("Se pueden seguir hasta %d ofertas por conexión.", MaxOfertas)
	}
	return ids, ""
}

// transmitir mantiene la conexión abierta y escribe cada evento hasta que el
// cliente se va.
func transmitir(c *gin.Context, h *hub.Hub, s *hub.Suscripcion, latido time.Duration) {
	defer h.Cancelar(s)
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// retry: cuánto espera el navegador para reconectar si se corta.
	fmt.Fprint(w, "retry: 3000\n: conectado\n\n")
	w.Flush()

	tic := time.NewTicker(latido)
	defer tic.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-tic.C:
			fmt.Fprint(w, ": latido\n\n")
			w.Flush()
		case e := <-s.Eventos:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Nombre, e.Datos)
			w.Flush()
		}
	}
}

func problema(c *gin.Context, status int, code, detalle string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, gin.H{
		"type": "https://agencia-viajes.example/errores/" + code, "title": http.StatusText(status),
		"status": status, "code": code, "detail": detalle,
	})
}
