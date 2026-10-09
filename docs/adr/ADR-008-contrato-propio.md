# ADR-008: Contrato propio — API de Emisión de Comprobantes

- **Estado:** Aceptada
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D8

## Contexto

El sistema debe publicar una capacidad para que otro grupo la incorpore en un flujo importante de su propio sistema. **El grupo consumidor lo asigna la cátedra**, por lo que no se conoce su dominio.

## Decisión

Se publica la **API de Emisión de Comprobantes**, implementada por el servicio Comprobantes. Permite a un sistema externo emitir una factura con sus ítems, obtenerla en PDF y, opcionalmente, enviarla por correo a su cliente.

- **Contrato:** OpenAPI 3.0.3, en [`docs/contracts/comprobantes/v1/openapi.yaml`](../contracts/comprobantes/v1/openapi.yaml).
- **Autenticación:** API Key por grupo consumidor, en el header `X-API-Key`.
- **Idempotencia:** header `Idempotency-Key` obligatorio en la emisión. La misma clave con el mismo cuerpo devuelve el comprobante ya emitido; con un cuerpo distinto devuelve `409`.
- **Errores:** formato `application/problem+json` (RFC 9457) con códigos documentados.
- **Mock:** el mismo contrato se sirve con Prism (`docker compose up comprobantes-mock`) para que el consumidor pueda integrarse antes de la implementación real.

### Versionado y compatibilidad

- Versión mayor en la ruta (`/v1`) y versión semántica en `info.version` del contrato.
- Cambios **compatibles** (campos opcionales nuevos, nuevos endpoints) incrementan versión menor dentro de `/v1`.
- Cambios **incompatibles** (quitar o renombrar campos, cambiar tipos o semántica) requieren `/v2`. `/v1` se mantiene operativa hasta el final de la evaluación.
- Cada versión del contrato se conserva en su carpeta (`v1/`, `v2/`) y los cambios se registran en `docs/contracts/README.md`.

## Alternativas consideradas

- **API de disponibilidad y reserva de ofertas de viaje.** Es la capacidad más característica del dominio, pero solo aporta valor real a un consumidor cuyo negocio se relacione con viajes. Como el consumidor no se elige, el riesgo de que la integración resulte forzada ("una llamada aislada sin consecuencias") es alto.
- **Publicar eventos (AsyncAPI) en lugar de API.** Exige que el consumidor acceda a nuestro broker, lo que complica la autenticación y el despliegue entre grupos.

Se eligió comprobantes porque **cualquier sistema que venda o reserve algo** puede integrarla en su flujo de compra con consecuencias de negocio reales, y porque el mismo servicio se usa internamente, lo que garantiza que la capacidad esté ejercitada en nuestro propio flujo principal.

## Consecuencias

- (+) Integrable por cualquier grupo con independencia de su dominio.
- (+) Capacidad aislada: un pico externo no afecta las compras propias.
- (−) Expone una capacidad de soporte y no el núcleo del dominio; debe defenderse con el criterio anterior.
- (−) Requiere mantener la versión publicada operativa en la nube hasta el final de la evaluación.
