// Package repository es la capa de acceso a datos de Catálogo:
//   - MySQL (esquema catalogo): inventario, retenciones, confirmaciones y outbox,
//     con las transacciones y bloqueos de ADR-014;
//   - MongoDB: ofertas y packs (ADR-003), con el outbox dentro del documento (ADR-006);
//   - Redis: caché del detalle de ofertas (ADR-007).
//
// No conoce HTTP ni a las capas de arriba (ADR-002).
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/go-sql-driver/mysql"
)

// ConfigMySQL son los datos de conexión al esquema propio, con el usuario del
// servicio (nunca root, ADR-003).
type ConfigMySQL struct {
	Host     string
	Puerto   string
	Usuario  string
	Password string
	Nombre   string
}

func (c ConfigMySQL) dsn() string {
	// time_zone UTC: NOW() y los DATETIME se interpretan igual en todas las instancias.
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC&time_zone=%s",
		c.Usuario, c.Password, c.Host, c.Puerto, c.Nombre, url.QueryEscape("'+00:00'"))
}

// ConectarMySQL abre la conexión y espera hasta 2 minutos a que MySQL acepte
// conexiones (en el primer docker compose up tarda en inicializarse, ADR-010).
func ConectarMySQL(ctx context.Context, cfg ConfigMySQL, log *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.dsn())
	if err != nil {
		return nil, fmt.Errorf("configurar MySQL: %w", err)
	}
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(15)
	db.SetConnMaxLifetime(30 * time.Minute)

	limite := time.Now().Add(2 * time.Minute)
	espera := time.Second
	for {
		ping, cancelar := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(ping)
		cancelar()
		if err == nil {
			return db, nil
		}
		if time.Now().After(limite) || ctx.Err() != nil {
			db.Close()
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

// Tablas del esquema catalogo (ADR-014). Se crean al arrancar; con IF NOT
// EXISTS, dos instancias pueden hacerlo a la vez.
var migraciones = []string{
	`CREATE TABLE IF NOT EXISTS inventario (
		oferta_id  VARCHAR(64) NOT NULL PRIMARY KEY,
		cupo_total INT NOT NULL,
		vendidos   INT NOT NULL DEFAULT 0,
		version    BIGINT NOT NULL DEFAULT 1,
		CONSTRAINT chk_inventario_vendidos CHECK (vendidos >= 0 AND vendidos <= cupo_total)
	) ENGINE=InnoDB`,

	// Una fila por solicitud de retención. La clave de idempotencia vive acá, y
	// no en cada retención, porque un pack crea varias retenciones con la misma clave.
	`CREATE TABLE IF NOT EXISTS grupos_retencion (
		id              CHAR(36) NOT NULL PRIMARY KEY,
		idempotency_key VARCHAR(64) NOT NULL,
		titular_id      VARCHAR(64) NOT NULL,
		pack_id         VARCHAR(64) NULL,
		expira_en       DATETIME(6) NOT NULL,
		creado_en       DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		UNIQUE KEY uq_grupos_idempotency (idempotency_key)
	) ENGINE=InnoDB`,

	`CREATE TABLE IF NOT EXISTS retenciones (
		id                CHAR(36) NOT NULL PRIMARY KEY,
		grupo_id          CHAR(36) NOT NULL,
		oferta_id         VARCHAR(64) NOT NULL,
		cantidad          INT NOT NULL,
		titular_id        VARCHAR(64) NOT NULL,
		estado            VARCHAR(16) NOT NULL,
		expira_en         DATETIME(6) NOT NULL,
		compra_id         VARCHAR(64) NULL,
		pack_id           VARCHAR(64) NULL,
		descripcion       VARCHAR(300) NOT NULL,
		destino           VARCHAR(120) NOT NULL,
		precio_referencia DECIMAL(14,2) NOT NULL,
		creada_en         DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		INDEX idx_retenciones_oferta (oferta_id, estado, expira_en),
		INDEX idx_retenciones_titular (titular_id, oferta_id, estado),
		INDEX idx_retenciones_grupo (grupo_id),
		CONSTRAINT chk_retenciones_cantidad CHECK (cantidad > 0)
	) ENGINE=InnoDB`,

	`CREATE TABLE IF NOT EXISTS confirmaciones (
		compra_id VARCHAR(64) NOT NULL PRIMARY KEY,
		respuesta JSON NOT NULL,
		creada_en DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	) ENGINE=InnoDB`,

	`CREATE TABLE IF NOT EXISTS outbox (
		id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		tipo         VARCHAR(64) NOT NULL,
		payload      JSON NOT NULL,
		headers      JSON NOT NULL,
		creado_en    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		publicado_en DATETIME(6) NULL,
		INDEX idx_outbox_pendientes (publicado_en, id)
	) ENGINE=InnoDB`,

	// Logs persistidos para Grafana (pkg/observabilidad, ADR-011).
	`CREATE TABLE IF NOT EXISTS app_logs (
		id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		fecha          DATETIME(6) NOT NULL,
		nivel          VARCHAR(16) NOT NULL,
		servicio       VARCHAR(100) NOT NULL,
		mensaje        TEXT NOT NULL,
		correlacion_id VARCHAR(64) NULL,
		trace_id       CHAR(32) NULL,
		metadatos      JSON NOT NULL,
		INDEX idx_app_logs_fecha (fecha),
		INDEX idx_app_logs_correlacion (correlacion_id),
		INDEX idx_app_logs_trace (trace_id)
	) ENGINE=InnoDB`,
}

// MigrarMySQL crea las tablas que falten.
func MigrarMySQL(ctx context.Context, db *sql.DB) error {
	for _, m := range migraciones {
		if _, err := db.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("migrar MySQL: %w", err)
		}
	}
	return nil
}

// Códigos de error de MySQL que se tratan aparte.
const (
	errClaveDuplicada = 1062
	errDeadlock       = 1213
	errEsperaBloqueo  = 1205
)

func esErrorMySQL(err error, codigo uint16) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == codigo
}

// enTransaccion ejecuta fn en una transacción READ COMMITTED: después de tomar
// los bloqueos, cada lectura ve lo último confirmado (no una foto vieja). Si
// MySQL elige la transacción como víctima de un deadlock, la reintenta hasta 3
// veces; con el orden de bloqueo de ADR-014 no debería pasar, pero es barato.
func enTransaccion(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	var err error
	for intento := range 3 {
		err = intentarTransaccion(ctx, db, fn)
		if !esErrorMySQL(err, errDeadlock) && !esErrorMySQL(err, errEsperaBloqueo) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(10*(intento+1)) * time.Millisecond):
		}
	}
	return err
}

func intentarTransaccion(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("iniciar transacción: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar transacción: %w", err)
	}
	return nil
}

// marcadores devuelve "?, ?, ?" para n valores y los valores como []any.
func marcadores(valores []string) (string, []any) {
	args := make([]any, len(valores))
	s := make([]byte, 0, len(valores)*3)
	for i, v := range valores {
		if i > 0 {
			s = append(s, ", "...)
		}
		s = append(s, '?')
		args[i] = v
	}
	return string(s), args
}
