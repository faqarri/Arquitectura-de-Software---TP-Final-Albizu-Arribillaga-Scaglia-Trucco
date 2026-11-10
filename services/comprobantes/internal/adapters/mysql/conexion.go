// Package mysql implementa ports.RepositorioComprobantes con GORM sobre MySQL.
package mysql

import (
	"fmt"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// Config son los datos de conexión al esquema propio del servicio. El usuario es
// el del servicio, nunca root (ADR-003).
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

// Conectar abre la conexión y configura el pool.
func Conectar(cfg Config) (*gorm.DB, error) {
	db, err := gorm.Open(gormmysql.Open(cfg.dsn()), &gorm.Config{
		// Silencioso: el logger de GORM escribe texto plano (rompe los logs JSON) y
		// trataría como error cada clave duplicada, que en este servicio es el
		// camino normal de un reintento idempotente. Los errores los registra el
		// servicio con su propio logger.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("conectar a MySQL: %w", err)
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
	return db, nil
}

// Migrar crea o actualiza las tablas del servicio.
func Migrar(db *gorm.DB) error {
	return db.AutoMigrate(&comprobanteModelo{}, &itemModelo{}, &outboxModelo{}, &appLogModelo{}, &consumidorModelo{}, &apiKeyModelo{})
}
