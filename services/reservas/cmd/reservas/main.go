// Servicio Reservas: carrito, compras y guías de viaje. Sigue arquitectura
// hexagonal (ADR-002): este archivo es el único que crea los adaptadores y los
// conecta con los casos de uso.
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

	"agencia/pkg/estudiario"
	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/adapters/api"
	"agencia/reservas/internal/adapters/catalogo"
	adaptadorestudiario "agencia/reservas/internal/adapters/estudiario"
	"agencia/reservas/internal/adapters/mysql"
	"agencia/reservas/internal/adapters/rabbit"
	"agencia/reservas/internal/application"
)

const nombreServicio = "reservas"

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

	db, err := mysql.Conectar(ctx, cfg.mysql, log)
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
	log, cerrarLogs := observabilidad.NuevoLoggerConMySQL(nombreServicio, sqlDB)
	defer cerrarLogs()

	if cfg.estudiarioAPIKey == "" {
		log.Warn("ESTUDIARIO_API_KEY no está configurada: el servicio real de guías va a rechazar las solicitudes (el mock las acepta)")
	}

	// Adaptadores de salida.
	carritos := mysql.NuevoCarritos(db)
	compras := mysql.NuevoCompras(db)
	guiasRepo := mysql.NuevoGuias(db)
	cat := catalogo.NuevoCliente(catalogo.Config{URL: cfg.catalogoURL})
	externas := adaptadorestudiario.NuevoGuias(estudiario.NuevoCliente(estudiario.Config{URL: cfg.estudiarioURL, APIKey: cfg.estudiarioAPIKey}))

	// Casos de uso.
	ucCarritos := application.NuevoCarritos(carritos, cat)
	ucCompras := application.NuevoCompras(compras, carritos, cat)
	ucGuias := application.NuevoGuias(guiasRepo, compras, externas)

	// Adaptadores de entrada.
	servidor := &http.Server{
		Addr: ":" + cfg.puertoHTTP,
		Handler: api.NuevoRouter(api.Dependencias{
			Log: log, Carritos: ucCarritos, Compras: ucCompras, Guias: ucGuias,
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
	g.Go(func() error { return rabbit.IniciarPublicadorOutbox(ctx, cfg.rabbitURL, log, mysql.NuevoOutbox(db)) })
	g.Go(func() error { return rabbit.IniciarConsumidorRetenciones(ctx, cfg.rabbitURL, log, ucCarritos) })

	// Procesos periódicos. Un error se registra y se reintenta en el próximo ciclo.
	g.Go(func() error {
		return periodico(ctx, cfg.intervaloRecupero, log.With("componente", "recuperación de compras"), func(ctx context.Context) error {
			n, err := ucCompras.RecuperarPendientes(ctx, 20)
			if n > 0 {
				log.Info("compras pendientes resueltas", "cantidad", n)
			}
			return err
		})
	})
	g.Go(func() error {
		return periodico(ctx, cfg.intervaloEnvio, log.With("componente", "envío de guías"), func(ctx context.Context) error {
			// Estudiario admite 10 registros por minuto: 5 por ciclo deja margen.
			n, err := ucGuias.EnviarPendientes(ctx, 5)
			if n > 0 {
				log.Info("guías enviadas a Estudiario", "cantidad", n)
			}
			return err
		})
	})
	g.Go(func() error {
		return periodico(ctx, cfg.intervaloModeracion, log.With("componente", "moderación de guías"), func(ctx context.Context) error {
			n, err := ucGuias.ActualizarModeracion(ctx)
			if n > 0 {
				log.Info("guías moderadas", "cantidad", n)
			}
			return err
		})
	})

	err = g.Wait()
	log.Info("servicio detenido")
	return err
}

// periodico ejecuta tarea cada intervalo hasta que ctx se cancela. Los errores
// se registran (una vez por cada error distinto) y no detienen el servicio.
func periodico(ctx context.Context, intervalo time.Duration, log *slog.Logger, tarea func(context.Context) error) error {
	tic := time.NewTicker(intervalo)
	defer tic.Stop()
	ultimo := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tic.C:
			ctxTarea, cancelar := context.WithTimeout(ctx, intervalo)
			err := tarea(ctxTarea)
			cancelar()
			switch {
			case err != nil && err.Error() != ultimo:
				log.Warn("la tarea periódica falló; se reintenta en el próximo ciclo", "error", err)
				ultimo = err.Error()
			case err == nil && ultimo != "":
				log.Info("la tarea periódica se recuperó")
				ultimo = ""
			}
		}
	}
}
