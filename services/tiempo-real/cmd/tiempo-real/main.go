// Servicio Tiempo Real (bonus, ADR-015): reenvía a los navegadores, por SSE, los
// cambios de disponibilidad y los vencimientos de retenciones que publica
// Catálogo. Sin estado: se puede correr en varias instancias.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"agencia/pkg/observabilidad"
	"agencia/tiempo-real/internal/consumidor"
	"agencia/tiempo-real/internal/hub"
	"agencia/tiempo-real/internal/sse"
)

const nombreServicio = "tiempo-real"

func main() {
	log := observabilidad.NuevoLogger(nombreServicio)
	if err := ejecutar(log); err != nil {
		log.Error("el servicio terminó con error", "error", err)
		os.Exit(1)
	}
}

func ejecutar(log *slog.Logger) error {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	puerto := os.Getenv("PUERTO_HTTP")
	if puerto == "" {
		puerto = "8080"
	}
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		return fmt.Errorf("falta la variable de entorno RABBITMQ_URL")
	}
	apagarTrazas, err := observabilidad.IniciarTrazas(ctx, nombreServicio)
	if err != nil {
		return err
	}
	defer apagarTrazas(context.Background())

	h := hub.Nuevo()
	grupo, ctx := errgroup.WithContext(ctx)
	// Sin WriteTimeout: las conexiones SSE quedan abiertas mientras el navegador
	// mire la página. BaseContext hace que, al apagar, se cancelen y terminen.
	servidor := &http.Server{
		Addr:              ":" + puerto,
		Handler:           sse.NuevoRouter(log, h, sse.Latido),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	grupo.Go(func() error { return consumidor.Iniciar(ctx, rabbitURL, log, h) })
	grupo.Go(func() error {
		log.Info("API escuchando", "puerto", puerto)
		if err := servidor.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	grupo.Go(func() error {
		<-ctx.Done()
		apagado, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		// Las conexiones SSE se cortan y los navegadores reconectan a otra instancia.
		if err := servidor.Shutdown(apagado); err != nil {
			return servidor.Close()
		}
		return nil
	})
	return grupo.Wait()
}
