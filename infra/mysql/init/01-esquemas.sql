-- Un esquema por servicio. Los usuarios y sus permisos se crean en 02-usuarios.sh.
CREATE DATABASE IF NOT EXISTS reservas;
CREATE DATABASE IF NOT EXISTS comprobantes;
CREATE DATABASE IF NOT EXISTS usuarios;
-- Catálogo: inventario de cupos, retenciones, confirmaciones y outbox (ADR-014).
-- Las descripciones de ofertas y packs siguen en MongoDB.
CREATE DATABASE IF NOT EXISTS catalogo;

CREATE TABLE IF NOT EXISTS comprobantes.app_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    fecha DATETIME(6) NOT NULL,
    nivel VARCHAR(16) NOT NULL,
    servicio VARCHAR(100) NOT NULL,
    mensaje TEXT NOT NULL,
    correlacion_id VARCHAR(64) NULL,
    trace_id CHAR(32) NULL,
    metadatos JSON NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_app_logs_fecha (fecha),
    INDEX idx_app_logs_correlacion (correlacion_id),
    INDEX idx_app_logs_trace (trace_id)
) ENGINE=InnoDB;
