-- Un esquema por servicio. Las credenciales por servicio se configurarán vía variables de entorno.
CREATE DATABASE IF NOT EXISTS reservas;
CREATE DATABASE IF NOT EXISTS comprobantes;
CREATE DATABASE IF NOT EXISTS usuarios;
-- Catálogo: inventario de cupos, retenciones, confirmaciones y outbox (ADR-014).
-- Las descripciones de ofertas y packs siguen en MongoDB.
CREATE DATABASE IF NOT EXISTS catalogo;
