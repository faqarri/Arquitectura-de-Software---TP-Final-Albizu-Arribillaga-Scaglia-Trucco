# ADR-008: Contrato propio — API de Emisión de Comprobantes

- **Estado:** Aceptada (revisada el 2026-10-22, ver [Revisiones](#revisiones))
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D8

## Contexto

El sistema debe publicar una capacidad para que otro grupo la incorpore en un flujo importante de su propio sistema. **El grupo consumidor lo asigna la cátedra**, por lo que no se conoce su dominio.

## Decisión

Se publica la **API de Emisión de Comprobantes**, implementada por el servicio Comprobantes. Un sistema externo envía el email del destinatario y la lista de ítems vendidos (descripción y precio en ARS); el servicio genera la factura en PDF y **siempre** la envía por correo a ese email.

- **Contrato:** OpenAPI 3.0.3, en [`docs/contracts/comprobantes/v1/openapi.yaml`](../contracts/comprobantes/v1/openapi.yaml). Versión vigente: **1.0.2**.
- **Operaciones:** `POST /v1/comprobantes` (emitir y encolar el envío) y `GET /v1/comprobantes/{id}` (consultar el estado del envío: `PENDIENTE`, `ENVIADO`, `FALLIDO`).
- **Autenticación:** API Key por grupo consumidor, en el header `X-API-Key`.
- **Idempotencia:** header `Idempotency-Key` obligatorio en la emisión. La misma clave con el mismo cuerpo devuelve `200` con el comprobante ya emitido y **no reenvía el correo**; con un cuerpo distinto devuelve `409`.
- **Errores:** formato `application/problem+json` (RFC 9457) con códigos documentados (400, 401, 404, 409, 422, 429, 503).
- **Alcance por consumidor:** cada API Key identifica a un grupo consumidor. La clave de idempotencia es única **por consumidor** (`UNIQUE (consumidor, idempotency_key)`), y para detectar el `409` se guarda el hash SHA-256 del cuerpo normalizado. `GET /v1/comprobantes/{id}` solo devuelve comprobantes del mismo consumidor (los demás responden `404`, igual que si no existieran); los comprobantes de nuestras propias compras se registran con el consumidor `interno`.
- **Límite de solicitudes:** 60 por minuto por API Key. Lo aplica el propio servicio Comprobantes, porque esta API se expone en su propio despliegue y no pasa por el gateway del sistema.
- **Mock:** el mismo contrato se sirve con Prism (`docker compose up comprobantes-mock`) para que el consumidor pueda integrarse antes de la implementación real.

### Versionado y compatibilidad

- Versión mayor en la ruta (`/v1`) y versión semántica en `info.version` del contrato.
- Cambios **compatibles** (campos opcionales nuevos en respuestas, nuevos endpoints) incrementan versión menor dentro de `/v1`.
- Cambios **incompatibles** (quitar o renombrar campos, agregar campos obligatorios, cambiar tipos o semántica) requieren `/v2`. `/v1` se mantiene operativa hasta el final de la evaluación.
- Cada versión mayor del contrato se conserva en su carpeta (`v1/`, `v2/`) y los cambios se registran en `docs/contracts/README.md`.
- La política rige desde la versión **1.0.0**. Las versiones `0.x` son borradores sin consumidores y pueden cambiar sin abrir una nueva ruta.

## Alternativas consideradas

- **API de disponibilidad y reserva de ofertas de viaje.** Es la capacidad más característica del dominio, pero solo aporta valor real a un consumidor cuyo negocio se relacione con viajes. Como el consumidor no se elige, el riesgo de que la integración resulte forzada ("una llamada aislada sin consecuencias") es alto.
- **Publicar eventos (AsyncAPI) en lugar de API.** Exige que el consumidor acceda a nuestro broker, lo que complica la autenticación y el despliegue entre grupos.

Se eligió comprobantes porque **cualquier sistema que venda o reserve algo** puede integrarla en su flujo de compra con consecuencias de negocio reales, y porque el mismo servicio se usa internamente, lo que garantiza que la capacidad esté ejercitada en nuestro propio flujo principal.

## Consecuencias

- (+) Integrable por cualquier grupo con independencia de su dominio.
- (+) Capacidad aislada: un pico externo no afecta las compras propias.
- (+) Contrato mínimo (dos campos): bajo costo de integración y menos superficie de cambios incompatibles.
- (−) Expone una capacidad de soporte y no el núcleo del dominio; debe defenderse con el criterio anterior.
- (−) Requiere mantener la versión publicada operativa en la nube hasta el final de la evaluación.
- (−) El consumidor no puede descargar el PDF por API: lo recibe únicamente el destinatario por correo.

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-22 | El contrato pasa a recibir **solo** `email` e `items` (`descripcion` 1–200 caracteres, `precio` ≥ 0 en ARS). El envío por correo es **siempre** asíncrono (`PENDIENTE` → `ENVIADO`/`FALLIDO`). Se quitan nombre y documento del cliente, cantidad, moneda, `enviarPorCorreo`, `referenciaExterna`, el estado `NO_SOLICITADO` y el endpoint `GET /v1/comprobantes/{id}/pdf`. Las rutas incluyen `/v1` en el contrato para que mock y producción coincidan. La versión publicada en la Entrega 1 se renombra **0.1.0 (borrador)** y la nueva es **1.0.0**. Se agrega a la política que rige desde 1.0.0. | Devolución de la Entrega 1: el profesor pidió simplificar el contrato. Se reemplazó el borrador **sin abrir `/v2`** porque ningún grupo lo consumía (la cátedra aún no asignó consumidor), así que no había compatibilidad que preservar. La **decisión** de D8 (qué capacidad se publica, autenticación, idempotencia, política de versionado) no cambia; por eso se revisa este ADR en lugar de reemplazarlo (criterio en [README](README.md)). |
| 2026-10-22 | Se precisa cómo se implementan las garantías que el contrato ya prometía: idempotencia **por consumidor** con hash del cuerpo, `404` para comprobantes de otro consumidor, consumidor `interno` para las compras propias y rate limit aplicado por el propio servicio. **El contrato no cambia** (sigue en 1.0.0). | Revisión interna del diseño. Sin guardar el hash del cuerpo no se puede responder el `409` que promete el contrato; sin asociar cada comprobante a su consumidor no se puede cumplir el `404` "para este consumidor", y las claves internas y externas podían chocar entre sí. |
| 2026-10-22 | Contrato **1.0.1**: los precios con más de 2 decimales se redondean a 2 (el 5 hacia arriba) en lugar de rechazarse; la respuesta devuelve los precios redondeados. Se agrega a la política de versionado el nivel de parche para aclaraciones que no cambian el esquema. | Los importes se guardan como `DECIMAL(12,2)` ([ADR-003](ADR-003-persistencia.md#revisiones)) y el contrato no decía qué pasaba con más decimales. Se eligió redondear para no rechazar solicitudes que el esquema 1.0.0 ya permitía; por eso es un cambio compatible. |
| 2026-10-23 | Contrato **1.0.2**: un precio o un total mayor a 9999999999.99 responde `422` (regla incumplida). | Revisión de la implementación: esos importes no entran en `DECIMAL(12,2)` y la API respondía `503`, que le indica al consumidor que reintente aunque nunca va a funcionar. Esas solicitudes ya fallaban, así que no se rechaza nada que antes se aceptara. |
