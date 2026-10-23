-- Un esquema por servicio. Los usuarios y sus permisos se crean en 02-usuarios.sh.
CREATE DATABASE IF NOT EXISTS reservas;
CREATE DATABASE IF NOT EXISTS comprobantes;
CREATE DATABASE IF NOT EXISTS usuarios;
-- Catálogo: inventario de cupos, retenciones, confirmaciones y outbox (ADR-014).
-- Las descripciones de ofertas y packs siguen en MongoDB.
CREATE DATABASE IF NOT EXISTS catalogo;
