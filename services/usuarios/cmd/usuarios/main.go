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

	"agencia/pkg/observabilidad"
	"agencia/usuarios/internal/adapters/api"
	"agencia/usuarios/internal/adapters/mysql"
	"agencia/usuarios/internal/application"
)

const nombreServicio = "usuarios"

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

	servicio, err := application.NuevoUsuarios(mysql.NuevoRepositorio(db), cfg.secretoJWT)
	if err != nil {
		return err
	}
	if cfg.adminEmail != "" {
		if err := servicio.CrearAdmin(ctx, "Administrador", cfg.adminEmail, cfg.adminPass); err != nil {
			return err
		}
		log.Info("administrador inicial verificado", "email", cfg.adminEmail)
	} else {
		log.Warn("no se configuró el administrador inicial; definir USUARIOS_ADMIN_EMAIL y USUARIOS_ADMIN_PASSWORD para la demo")
	}

	servidor := &http.Server{
		Addr: ":" + cfg.puertoHTTP,
		Handler: api.NuevoRouter(api.Dependencias{
			Log: log, Usuarios: servicio, VerificarSalud: sqlDB.PingContext,
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
	err = g.Wait()
	log.Info("servicio detenido")
	return err
}
