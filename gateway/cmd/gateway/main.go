package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agencia/gateway/internal/gateway"
	"agencia/pkg/observabilidad"
)

func main() {
	log := observabilidad.NuevoLogger("gateway")
	if err := ejecutar(log); err != nil {
		log.Error("el servicio terminó con error", "error", err)
		os.Exit(1)
	}
}

func ejecutar(log *slog.Logger) error {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	cfg, err := cargarConfig()
	if err != nil {
		return err
	}
	apagarTrazas, err := observabilidad.IniciarTrazas(ctx, "gateway")
	if err != nil {
		return err
	}
	defer apagarTrazas(context.Background())

	handler, cerrarGateway, err := gateway.NuevoRouter(cfg, log)
	if err != nil {
		return err
	}
	defer cerrarGateway()
	servidor := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errores := make(chan error, 1)
	go func() {
		log.Info("API Gateway escuchando", "puerto", 8080)
		if err := servidor.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errores <- err
			return
		}
		errores <- nil
	}()

	select {
	case err := <-errores:
		return err
	case <-ctx.Done():
		apagado, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelar()
		if err := servidor.Shutdown(apagado); err != nil {
			return fmt.Errorf("cerrar API Gateway: %w", err)
		}
		return <-errores
	}
}

func cargarConfig() (gateway.Config, error) {
	limite, err := strconv.Atoi(valorO("GATEWAY_RATE_LIMIT_POR_MINUTO", "120"))
	if err != nil || limite < 1 {
		return gateway.Config{}, fmt.Errorf("GATEWAY_RATE_LIMIT_POR_MINUTO debe ser un entero positivo")
	}
	cfg := gateway.Config{
		SecretoJWT:           os.Getenv("JWT_SECRET"),
		UsuariosURL:          valorO("USUARIOS_URL", "http://usuarios:8080"),
		ReservasURL:          valorO("RESERVAS_URL", "http://reservas:8080"),
		TiempoRealURL:        os.Getenv("TIEMPO_REAL_URL"),
		LimiteSolicitudesMin: limite,
	}
	if instancias := strings.TrimSpace(os.Getenv("CATALOGO_INSTANCIAS")); instancias != "" {
		for _, instancia := range strings.Split(instancias, ",") {
			cfg.CatalogoURLs = append(cfg.CatalogoURLs, strings.TrimSpace(instancia))
		}
	}
	if err := cfg.Validar(); err != nil {
		return gateway.Config{}, err
	}
	return cfg, nil
}

func valorO(nombre, defecto string) string {
	if valor := os.Getenv(nombre); valor != "" {
		return valor
	}
	return defecto
}
