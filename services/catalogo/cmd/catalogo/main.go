// Servicio Catálogo: ofertas, packs, cupos, retenciones y disponibilidad.
// Arquitectura en capas (ADR-002): handlers → services → repository. Este
// archivo es el único que crea las dependencias y las conecta.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"agencia/catalogo/internal/events"
	"agencia/catalogo/internal/handlers"
	"agencia/catalogo/internal/jobs"
	"agencia/catalogo/internal/repository"
	"agencia/catalogo/internal/services"
	"agencia/pkg/estudiario"
	"agencia/pkg/observabilidad"
)

const nombreServicio = "catalogo"

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
	log = log.With("instancia", cfg.instancia)
	apagarTrazas, err := observabilidad.IniciarTrazas(ctx, nombreServicio)
	if err != nil {
		return err
	}
	defer apagarTrazas(context.Background())

	// MySQL: inventario, retenciones, confirmaciones y outbox.
	sqlDB, err := repository.ConectarMySQL(ctx, cfg.mysql, log)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := repository.MigrarMySQL(ctx, sqlDB); err != nil {
		return err
	}
	log, cerrarLogs := observabilidad.NuevoLoggerConMySQL(nombreServicio, sqlDB)
	defer cerrarLogs()
	log = log.With("instancia", cfg.instancia)

	// MongoDB: ofertas y packs.
	mongoDB, err := repository.ConectarMongo(ctx, cfg.mongoURL, cfg.mongoBase, log)
	if err != nil {
		return err
	}
	defer mongoDB.Client().Disconnect(context.Background())
	if err := repository.MigrarMongo(ctx, mongoDB); err != nil {
		return err
	}

	// Redis: caché opcional del detalle y de las guías.
	cache, err := repository.NuevaCache(repository.ConfigCache{URL: cfg.redisURL, Habilitada: cfg.cacheHabilitada}, log)
	if err != nil {
		return fmt.Errorf("configurar Redis: %w", err)
	}
	defer cache.Cerrar()
	if !cfg.cacheHabilitada || cfg.redisURL == "" {
		log.Warn("caché deshabilitada: el detalle se lee siempre de MongoDB")
	}

	if cfg.estudiarioAPIKey == "" {
		log.Warn("ESTUDIARIO_API_KEY no está configurada: el servicio real de guías va a rechazar las solicitudes (el mock las acepta)")
	}

	// Capa de datos.
	ofertasRepo := repository.NuevoOfertas(mongoDB)
	packsRepo := repository.NuevoPacks(mongoDB)
	inventario := repository.NuevoInventario(sqlDB)

	// Capa de servicios.
	ofertas := services.NuevoOfertas(ofertasRepo, inventario, cache, log)
	packs := services.NuevoPacks(packsRepo, ofertasRepo)
	retenciones := services.NuevoRetenciones(ofertasRepo, packsRepo, inventario,
		services.ConfigRetenciones{Minutos: cfg.retencionMinutos, MaxPorOferta: cfg.maxPorOferta})
	guias := services.NuevoGuias(ofertas,
		estudiario.NuevoCliente(estudiario.Config{
			URL: cfg.estudiarioURL, APIKey: cfg.estudiarioAPIKey, URLPublica: cfg.estudiarioURLPublica,
		}), cache, log)

	// OpenSearch: índice de búsqueda (ADR-006). Si no responde, solo la
	// búsqueda devuelve 503; no cuenta para /health.
	indice, err := repository.NuevoIndiceBusqueda(cfg.opensearchURL, cfg.aliasBusqueda)
	if err != nil {
		return err
	}
	busqueda := services.NuevoBusqueda(indice, ofertasRepo, log)

	// Capa HTTP.
	servidor := &http.Server{
		Addr: ":" + cfg.puertoHTTP,
		Handler: handlers.NuevoRouter(handlers.Dependencias{
			Log: log, Instancia: cfg.instancia,
			Ofertas: ofertas, Packs: packs, Retenciones: retenciones, Guias: guias, Busqueda: busqueda,
			VerificarSalud: func(ctx context.Context) error {
				if err := sqlDB.PingContext(ctx); err != nil {
					return fmt.Errorf("MySQL no responde: %w", err)
				}
				if err := mongoDB.Client().Ping(ctx, nil); err != nil {
					return fmt.Errorf("MongoDB no responde: %w", err)
				}
				return nil
			},
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
	g.Go(func() error {
		return events.IniciarPublicadorOutbox(ctx, cfg.rabbitURL, log, repository.NuevoOutbox(sqlDB))
	})
	g.Go(func() error { return events.IniciarPublicadorOfertas(ctx, cfg.rabbitURL, log, ofertasRepo) })
	g.Go(func() error { return events.IniciarConsumidorCache(ctx, cfg.rabbitURL, log, ofertas) })
	g.Go(func() error { return jobs.IniciarVencimiento(ctx, cfg.intervaloBarrido, log, retenciones) })
	// El indexador arranca recién con el índice listo; hasta entonces los
	// cambios esperan en la cola catalogo.indexacion.
	g.Go(func() error {
		if err := jobs.PrepararBusqueda(ctx, log, busqueda); err != nil {
			return nil // ctx cancelado: el servicio se está apagando
		}
		return events.IniciarIndexador(ctx, cfg.rabbitURL, log, busqueda)
	})

	err = g.Wait()
	log.Info("servicio detenido")
	return err
}
