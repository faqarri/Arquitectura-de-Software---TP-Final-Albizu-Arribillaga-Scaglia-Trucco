// Servicio Comprobantes: emite facturas y las envía por correo. Es la capacidad
// publicada (ADR-008) y sigue arquitectura hexagonal (ADR-002): este archivo es
// el único que crea los adaptadores y los conecta con los casos de uso.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"agencia/comprobantes/internal/adapters/api"
	"agencia/comprobantes/internal/adapters/mysql"
	"agencia/comprobantes/internal/adapters/pdf"
	"agencia/comprobantes/internal/adapters/rabbit"
	"agencia/comprobantes/internal/adapters/smtp"
	"agencia/comprobantes/internal/application"
	"agencia/pkg/observabilidad"
)

const nombreServicio = "comprobantes"

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

	cfg, err := cargarConfig()
	if err != nil {
		return err
	}

	apagarTrazas, err := observabilidad.IniciarTrazas(ctx, nombreServicio)
	if err != nil {
		return err
	}
	defer apagarTrazas(context.Background())

	db, err := mysql.Conectar(cfg.mysql)
	if err != nil {
		return err
	}
	if err := mysql.Migrar(db); err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// Adaptadores de salida.
	repo := mysql.NuevoRepositorio(db)
	generador := pdf.NuevoGenerador()
	mailer := smtp.NuevoMailer(cfg.smtp)

	// Casos de uso.
	emitir := application.NuevoEmitirComprobante(repo)
	obtener := application.NuevoObtenerComprobante(repo)
	enviar := application.NuevoEnviarComprobante(repo, generador, mailer)

	// Adaptadores de entrada.
	servidor := &http.Server{
		Addr: ":" + cfg.puertoHTTP,
		Handler: api.NuevoRouter(api.Dependencias{
			Log:            log,
			Emitir:         emitir,
			Obtener:        obtener,
			APIKeys:        cfg.apiKeys,
			VerificarSalud: sqlDB.PingContext,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.Info("API escuchando", "puerto", cfg.puertoHTTP)
		if err := servidor.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		apagado, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelar()
		return servidor.Shutdown(apagado)
	})
	g.Go(func() error { return rabbit.IniciarConsumidorCompras(ctx, cfg.rabbit, log, emitir) })
	g.Go(func() error { return rabbit.IniciarWorkerEnvios(ctx, cfg.rabbit, log, enviar) })
	g.Go(func() error { return rabbit.IniciarPublicadorOutbox(ctx, cfg.rabbit, log, repo) })

	err = g.Wait()
	log.Info("servicio detenido")
	return err
}
