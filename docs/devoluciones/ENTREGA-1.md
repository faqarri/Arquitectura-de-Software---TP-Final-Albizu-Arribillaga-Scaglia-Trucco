# Devolución de la Entrega 1 — cambios aplicados

- **Fecha de aplicación:** 2026-10-22
- **Rama:** `feature/devolucion-entrega-1`

Este documento resume qué pidió la cátedra, qué decidimos y dónde quedó documentado cada cambio, para poder seguir la trazabilidad y defenderlo.

## Resumen

| # | Pedido | Decisión | Dónde quedó |
|---|---|---|---|
| 1 | Simplificar el contrato de Comprobantes | Solo `email` e `items` (`descripcion`, `precio` en ARS); envío por correo siempre y asíncrono; versión 1.0.0 | [openapi.yaml](../contracts/comprobantes/v1/openapi.yaml), [contracts/README](../contracts/README.md), [ADR-008 § Revisiones](../adr/ADR-008-contrato-propio.md#revisiones) |
| 2 | Cupos en Catálogo con retención al agregar al carrito | Patrón de retención de 15 minutos con confirmación atómica en Catálogo | [ADR-014](../adr/ADR-014-cupos-en-catalogo-con-retencion.md) (reemplaza a [ADR-001](../adr/ADR-001-limites-de-servicios.md)), [ADR-003](../adr/ADR-003-persistencia.md#revisiones), [ADR-005](../adr/ADR-005-comunicacion-entre-servicios.md#revisiones) |
| 3 | Distinguir agotado de retenido | Estados `DISPONIBLE`, `RETENIDA`, `AGOTADA` y servicio Tiempo Real con SSE (bonus) | [ADR-015](../adr/ADR-015-disponibilidad-en-tiempo-real.md) |
| — | Sugerencias fuera del alcance | Se eliminan de todos los documentos vigentes | [SPEC § Historial](../../SPEC.md#historial-de-cambios) |

## Cambio 1: contrato de Comprobantes

**Qué cambió:**

- La solicitud ahora tiene solo `email` e `items[]` con `descripcion` (1 a 200 caracteres) y `precio` (≥ 0, ARS).
- Se quitaron nombre y documento del cliente, `cantidad`, `moneda`, `enviarPorCorreo`, `referenciaExterna`, el estado `NO_SOLICITADO` y `GET /v1/comprobantes/{id}/pdf`.
- Se mantienen: `Idempotency-Key` obligatoria (misma clave y mismo cuerpo → `200` sin reenviar correo; cuerpo distinto → `409`), `GET /v1/comprobantes/{id}` para el estado del envío, `X-API-Key` y los errores `problem+json` 400, 401, 404, 409, 422 (total 0), 429 y 503.
- Se agregó `additionalProperties: false`: un consumidor que mande campos de la versión anterior recibe un `400` explícito en lugar de que se ignoren sin aviso.
- Las rutas incluyen `/v1` dentro de `paths` (antes estaba en `servers`), para que el mock Prism y producción respondan en la misma ruta (`/v1/comprobantes`).

**Versión:** la publicada en la Entrega 1 queda en el historial como **0.1.0 (borrador)**; la nueva es **1.0.0**. No se abrió `/v2` porque ningún grupo consumía el contrato todavía (la cátedra aún no asignó consumidor). La política de versionado rige desde 1.0.0.

**Validación:** `npx @redocly/cli lint` y `npx @apidevtools/swagger-cli validate` → válido. Quedan dos advertencias de estilo intencionales: el server del mock en `localhost` y la falta del campo `license`.

**Impacto interno:** el evento `CompraConfirmada` ahora lleva `email` e `items` (`descripcion`, `precio`). Un pack se factura como una línea por opción, con el precio ya descontado (el contrato no admite precios negativos, así que el descuento no puede ir como una línea aparte).

## Cambio 2: cupos en Catálogo con retención

**Qué cambió:**

- Catálogo pasa a ser dueño de ofertas, packs, **inventario, retenciones y disponibilidad**. Suma MySQL (esquema `catalogo`) para inventario, retenciones, confirmaciones y outbox.
- Reservas conserva carrito (cada ítem con `retencionId` y `expiraEn`), compras (`PENDIENTE`, `CONFIRMADA`, `RECHAZADA`) y outbox, y llama a la API interna `/internal/retenciones` de Catálogo.
- Se eliminaron la consulta de precios Reservas → Catálogo (la confirmación devuelve los precios) y el evento `OfertaPublicada`.
- Nueva regla (supuesto): máximo 6 cupos retenidos por oferta por carrito.

**Agregados al diseño recibido (decisiones nuestras, a defender):**

| Agregado | Por qué |
|---|---|
| Tabla `confirmaciones(compra_id, respuesta)` | La confirmación es idempotente por `compraId`: hay que guardar la respuesta para devolverla igual en un reintento, incluso con los precios de ese momento |
| Columnas `pack_id` y `grupo_id` en `retenciones` | Para aplicar el descuento del pack al confirmar y tratar al pack como una unidad (todo o nada) |
| `titular_id` = id de usuario | Lo necesitan el límite de 6 cupos y Tiempo Real para avisar al dueño de la retención |
| Bloqueo en orden de `oferta_id` | Evita deadlocks entre packs que comparten opciones |
| `NOW()` de la base como único reloj | Varias instancias de Catálogo no discrepan sobre si algo venció |
| `FOR UPDATE SKIP LOCKED` en el proceso de vencimiento | Varias instancias pueden correrlo sin procesar la misma fila |
| Reglas de pack (RN-03 a RN-05) en Catálogo | Catálogo retiene el pack completo y conoce su definición; así Reservas no necesita consultarla |

**ADRs:** ADR-001 se marcó "Reemplazada por ADR-014" sin borrar su contenido. ADR-014 es nuevo. ADR-003 y ADR-005 (Propuestas) se revisaron en el mismo archivo con una sección "Revisiones".

## Cambio 3: agotado vs. retenido en tiempo real (bonus)

- Estados `DISPONIBLE`, `RETENIDA` (con `proximaLiberacion`) y `AGOTADA`. Retener sin cupo responde `409 AGOTADA` o `409 RETENIDA`.
- Nuevo servicio **Tiempo Real**, sin estado, con SSE vía gateway (`/api/tiempo-real/ofertas`, `/api/tiempo-real/mis-retenciones`).
- Catálogo publica por outbox `DisponibilidadCambiada` (estado completo + `version`) y `RetencionVencida` en el exchange fanout `catalogo.disponibilidad`. Cada instancia de Tiempo Real tiene su cola exclusiva, sin DLQ. Reservas tiene una cola durable con DLQ.
- Detalle técnico: el `EventSource` nativo no permite el header `Authorization`, así que el stream autenticado se lee con `fetch` y streaming de la respuesta.

## Otros cambios realizados

| Cambio | Archivo |
|---|---|
| Se agregó la fila separadora que faltaba en la tabla de Integrantes (no se veía como tabla) | `README.md` |
| Se aclaró que la API pública de Comprobantes no pasa por el gateway (el diagrama de contenedores mostraba `gateway → Comprobantes`) | `docs/ARCHITECTURE.md` |
| Se quitó el `;` de los mensajes del diagrama de secuencia: Mermaid lo interpreta como separador de sentencias y cortaba el mensaje | `docs/ARCHITECTURE.md` |
| Criterio de cambios en ADRs: reemplazo si cambia la decisión, revisión si cambian los detalles o si el ADR está "Propuesta" | `docs/adr/README.md`, `CLAUDE.md` |
| Se agregó `CLAUDE.md` con el contexto del proyecto para las próximas sesiones con Claude Code | `CLAUDE.md` |
| Los cambios del equipo en SPEC (RN-07 "retiene cupo" y RN-10 sin `[SUPUESTO]`) se conservaron e integraron | `SPEC.md` |

## Para tener en cuenta al levantar el entorno

El esquema `catalogo` se crea en `infra/mysql/init/01-esquemas.sql`, que MySQL ejecuta **solo cuando el volumen está vacío**. Si ya habían levantado el entorno antes, ejecuten `docker compose down -v` y luego `docker compose up -d`. Ojo: esto borra los datos locales.
