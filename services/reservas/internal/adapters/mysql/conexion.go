// Package mysql implementa los repositorios de Reservas y el outbox con GORM
// sobre MySQL (esquema reservas, ADR-003).
package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// Config son los datos de conexión al esquema propio. El usuario es el del
// servicio, nunca root (ADR-003).
type Config struct {
	Host     string
	Puerto   string
	Usuario  string
	Password string
	Nombre   string
}

func (c Config) dsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
		c.Usuario, c.Password, c.Host, c.Puerto, c.Nombre)
}

// Conectar abre la conexión y espera a que MySQL acepte conexiones, con backoff
// de hasta 2 minutos. En el primer docker compose up, MySQL puede figurar sano
// mientras todavía se inicializa (hallazgo del ensayo en ADR-010).
func Conectar(ctx context.Context, cfg Config, log *slog.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{DSN: cfg.dsn()}), &gorm.Config{
		// Silencioso: el logger de GORM escribe texto plano y trataría como error
		// cada clave duplicada, que acá es el camino normal de un reintento idempotente.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("configurar MySQL: %w", err)
	}
	if err := db.Use(tracing.NewPlugin()); err != nil {
		return nil, fmt.Errorf("instrumentar trazas de GORM: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	limite := time.Now().Add(2 * time.Minute)
	espera := time.Second
	for {
		ping, cancelar := context.WithTimeout(ctx, 3*time.Second)
		err = sqlDB.PingContext(ping)
		cancelar()
		if err == nil {
			return db, nil
		}
		if time.Now().After(limite) || ctx.Err() != nil {
			return nil, fmt.Errorf("conectar a MySQL: %w", err)
		}
		log.Warn("MySQL todavía no acepta conexiones, se reintenta", "error", err, "espera", espera.String())
		select {
		case <-ctx.Done():
		case <-time.After(espera):
		}
		espera = min(espera*2, 10*time.Second)
	}
}

// Migrar crea o actualiza las tablas del servicio.
func Migrar(db *gorm.DB) error {
	return db.AutoMigrate(&itemModelo{}, &itemRetencionModelo{}, &compraModelo{}, &lineaModelo{},
		&outboxModelo{}, &guiaModelo{}, &archivoGuiaModelo{}, &appLogModelo{})
}
