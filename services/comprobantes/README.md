# Servicio Comprobantes

**Responsabilidad:** emisión de facturas en PDF y envío **siempre** por correo al email indicado. Implementa la **capacidad publicada** (contrato 1.0.1 en `docs/contracts/comprobantes/v1/`).

**Datos propios (MySQL, esquema `comprobantes`):** comprobantes (email, ítems, total), claves de idempotencia por consumidor, estado de envío (`PENDIENTE`, `ENVIADO`, `FALLIDO`) y outbox.

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MySQL | Almacenamiento | Comprobantes con clave única de idempotencia por consumidor, y outbox |
| RabbitMQ | Mensajería | Consume `CompraConfirmada` (con DLQ). Cola interna `comprobantes.envios` (con DLQ) para enviar los correos con reintentos |
| SMTP (Mailpit en local) | Externa | Envío de correos con PDF adjunto |

**Entradas:**

| Entrada | Datos | Consumidor | Idempotencia |
|---|---|---|---|
| Evento `CompraConfirmada` (uso interno) | `compraId`, `email`, `items` (`descripcion`, `precio`) | `interno` | Clave = `compraId` |
| `POST /v1/comprobantes` (uso externo, `X-API-Key`) | `email`, `items` (`descripcion`, `precio`) | El grupo dueño de la API Key | Header `Idempotency-Key`; misma clave y cuerpo → `200` sin reenviar correo; cuerpo distinto → `409` |
| `GET /v1/comprobantes/{id}` (uso externo) | — | El grupo dueño de la API Key | Solo devuelve comprobantes de ese consumidor; los demás responden `404` |

Las dos primeras entradas usan el mismo caso de uso: **emitir y encolar el envío**.

## Emisión

1. Valida el cuerpo según el contrato (`400`) y que el total sea mayor a cero (`422`).
2. Calcula el **hash SHA-256 del cuerpo normalizado** (se vuelve a serializar el JSON, así que espacios u orden de los campos no cambian el hash).
3. En **una sola transacción**: inserta el comprobante (`PENDIENTE`) con sus ítems y el evento `EnvioSolicitado` en el outbox.
4. Si la inserción choca con `UNIQUE (consumidor, idempotency_key)`, busca el comprobante existente: con el mismo hash responde `200` con ese comprobante (sin reenviar el correo); con otro hash, `409`.

La clave de idempotencia es **por consumidor**: dos grupos pueden usar la misma clave sin chocar, y un `compraId` interno nunca se confunde con la clave de un grupo externo.

## Envío del correo

- Un **publicador de outbox** lleva `EnvioSolicitado` a la cola `comprobantes.envios`. Si el servicio se cae después del commit, el evento sigue en el outbox y se publica al volver: un comprobante nunca queda `PENDIENTE` para siempre.
- El **worker de envío** consume la cola: si el comprobante ya está `ENVIADO`, confirma el mensaje sin enviar nada; si no, genera el PDF, lo envía por SMTP y marca `ENVIADO`.
- Si el envío falla: hasta 3 reintentos con espera creciente. Al agotarlos, marca `FALLIDO` y el mensaje pasa a `comprobantes.envios.dlq`. El comprobante **sigue emitido**.

## Reglas de la API pública

- **Autenticación:** `X-API-Key`. Las claves válidas se configuran en `COMPROBANTES_API_KEYS` con el formato `grupo:clave,grupo:clave`. En la base se guarda el nombre del grupo (`consumidor`), nunca la clave.
- **Límite de solicitudes:** 60 por minuto por API Key, controlado por el propio servicio, porque esta API **no pasa por el gateway** ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)). Al superarlo responde `429` con `Retry-After`.
- **Importes:** se guardan como `DECIMAL(12,2)` (nunca como punto flotante), así las sumas no acumulan errores de redondeo. Un precio con más de 2 decimales **se redondea** a 2 (al más cercano; el 5 redondea hacia arriba: `10.005` → `10.01`). Primero se redondea cada ítem y después se suma el total, así el total siempre coincide con la suma de los ítems que se devuelven. El hash de idempotencia se calcula sobre el cuerpo **original**, antes de redondear. Si después de redondear el total queda en cero (por ejemplo, un único ítem de `0.004`), responde `422`.

## Modelo de datos

```
comprobantes(id UUID PK, nro BIGINT AUTO_INCREMENT UNIQUE, consumidor, idempotency_key,
             hash_solicitud, email, total DECIMAL(12,2), estado_envio, intentos_envio,
             fecha_emision, actualizado_en,
             UNIQUE (consumidor, idempotency_key))
items_comprobante(comprobante_id FK, orden, descripcion, precio DECIMAL(12,2),
                  PK (comprobante_id, orden))
outbox(id PK, tipo, payload JSON, creado_en, publicado_en NULL)
```

`numero` (por ejemplo `B-0001-00000042`) se arma a partir de `nro`.

## Estructura (Go, arquitectura hexagonal)

El dominio y los casos de uso no conocen HTTP, RabbitMQ, MySQL ni SMTP: dependen de **puertos** (interfaces) que implementan los **adaptadores**. Las dos entradas (API y evento) son adaptadores que llaman al mismo caso de uso. Justificación en [ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md).

```
cmd/comprobantes/main.go     # arma las dependencias y arranca la API, los consumidores y el publicador de outbox
internal/domain/             # Comprobante, Item, EstadoEnvio; reglas: total > 0, transiciones de estado
internal/application/        # casos de uso: EmitirComprobante, ObtenerComprobante, EnviarComprobante
internal/ports/              # interfaces: RepositorioComprobantes, GeneradorPDF, Mailer
internal/adapters/http/      # entrada: API pública v1 (Gin), API Key, rate limit, problem+json
internal/adapters/rabbit/    # entrada: consumidores de CompraConfirmada y comprobantes.envios; salida: publicador de outbox
internal/adapters/mysql/     # salida: comprobantes, ítems y outbox
internal/adapters/pdf/       # salida: generación del PDF
internal/adapters/smtp/      # salida: envío de correo (Mailpit en local)
```

**Historial:**
- 2026-10-22: el contrato pasa a 1.0.0 (solo `email` e `items`, envío siempre por correo, sin descarga de PDF por API) ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).
- 2026-10-22: idempotencia por consumidor con hash del cuerpo, outbox para el envío, importes `DECIMAL`, rate limit propio y estructura hexagonal ([ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md), [ADR-005](../../docs/adr/ADR-005-comunicacion-entre-servicios.md#revisiones), [ADR-008](../../docs/adr/ADR-008-contrato-propio.md#revisiones)).
