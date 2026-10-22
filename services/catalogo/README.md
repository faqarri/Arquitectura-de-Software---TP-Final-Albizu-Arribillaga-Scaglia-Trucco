# Servicio Catálogo

**Responsabilidad:** qué se vende y cuánto queda. Descripción de ofertas (transporte, estadía, excursión) y packs; búsqueda indexada; **inventario de cupos, retenciones temporales y disponibilidad** (`DISPONIBLE`, `RETENIDA`, `AGOTADA`). Valida las reglas de pack (RN-03, RN-04) al retener y calcula los precios con el descuento del pack (RN-05, RN-06) al confirmar. Ver [ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md).

**Datos propios:**

| Almacén | Datos |
|---|---|
| MongoDB | Ofertas, packs y sus opciones (fuente de verdad de las descripciones y precios) |
| MySQL, esquema `catalogo` | `inventario(oferta_id, cupo_total, vendidos, version)`, `retenciones(id, oferta_id, cantidad, titular_id, estado, expira_en, compra_id, pack_id, grupo_id, idempotency_key UNIQUE)`, `confirmaciones(compra_id, respuesta)`, `outbox` |
| OpenSearch | Índice de búsqueda (derivado y reconstruible) |

Estados de retención: `ACTIVA`, `CONFIRMADA`, `LIBERADA`, `VENCIDA`. Una retención solo es válida si `estado = ACTIVA` **y** `expira_en > NOW()` (reloj de la base).

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MongoDB | Almacenamiento | Fuente de verdad del catálogo |
| MySQL (`catalogo`) | Almacenamiento | Inventario, retenciones y outbox con transacciones y `SELECT ... FOR UPDATE` |
| OpenSearch | Motor de búsqueda | Búsqueda con filtros, orden y paginación |
| RabbitMQ | Mensajería | Publica `OfertaActualizada` y consume ese evento para indexar. Publica por outbox `DisponibilidadCambiada` y `RetencionVencida` en el exchange fanout `catalogo.disponibilidad` |

**Expone:**

| API | Rutas | Acceso |
|---|---|---|
| Pública (vía gateway) | `/api/ofertas`, `/api/packs`, `/api/busqueda`, `GET /api/ofertas/{id}/disponibilidad` | Frontend |
| Interna (**no publicada en el gateway**) | `POST /internal/retenciones` (Idempotency-Key), `DELETE /internal/retenciones/{id}` (idempotente), `POST /internal/retenciones/confirmacion` (Idempotency-Key = compraId) | Solo Reservas, dentro de la red interna |

**Procesos en segundo plano:**

- **Vencimiento de retenciones** (cada 15 s): marca `VENCIDA` las retenciones activas con `expira_en <= NOW()` usando `FOR UPDATE SKIP LOCKED`, para que varias instancias puedan correrlo sin pisarse, y publica eventos. La validez no depende de este proceso.
- **Publicador de outbox.**
- **Indexador** (`OfertaActualizada` → OpenSearch).

**Configuración:** `RETENCION_MINUTOS` (15), `RETENCION_MAX_CUPOS_POR_OFERTA` (6), `RETENCION_BARRIDO_SEGUNDOS` (15), credenciales de MySQL del esquema `catalogo`.

**Estructura tentativa (Go, arquitectura en capas; a confirmar en D2):**

```
cmd/catalogo/main.go
internal/handlers/     # HTTP (Gin): API pública y /internal
internal/services/     # lógica de aplicación: ofertas, packs, retenciones, disponibilidad
internal/repository/   # MongoDB (ofertas, packs) y MySQL (inventario, retenciones, outbox)
internal/search/       # OpenSearch e indexador
internal/events/       # outbox publisher y consumo RabbitMQ
internal/jobs/         # vencimiento de retenciones
```

**Historial:** 2026-10-22: incorpora inventario, retenciones y disponibilidad (antes en Reservas), MySQL esquema `catalogo`, API interna y eventos de disponibilidad. Se quitan las sugerencias, el endpoint de precios para Reservas y el evento `OfertaPublicada` ([ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)).
