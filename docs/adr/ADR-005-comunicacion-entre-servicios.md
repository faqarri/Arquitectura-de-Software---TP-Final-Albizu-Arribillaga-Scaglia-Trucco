# ADR-005: Comunicación entre servicios

- **Estado:** Propuesta (versión inicial, a validar en Entrega 2; revisada el 2026-10-22, ver [Revisiones](#revisiones))
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D5

## Contexto

Algunas interacciones necesitan una respuesta inmediata para continuar (asegurar el cupo antes de mostrarlo en el carrito, confirmar las retenciones antes de dar la compra por hecha). Otras son consecuencias que no deben demorar ni hacer fallar la operación principal (enviar un correo, avisar a los navegadores que cambió la disponibilidad).

## Decisión

### Comunicación síncrona (REST/JSON)

**Reservas → Catálogo**, API interna no publicada en el gateway ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)):

| Operación | Cuándo | Idempotencia |
|---|---|---|
| `POST /internal/retenciones` | Al agregar al carrito | `Idempotency-Key` generada por Reservas por cada ítem agregado |
| `DELETE /internal/retenciones/{id}` | Al quitar del carrito | Naturalmente idempotente (`204` aunque ya esté liberada o vencida) |
| `POST /internal/retenciones/confirmacion` | Al comprar | `Idempotency-Key` = `compraId` |

| Aspecto | Valor inicial |
|---|---|
| Timeout | 2 s por intento |
| Reintentos | Hasta 2, con backoff exponencial (200 ms, 400 ms) y jitter. Son seguros en las tres operaciones porque todas son idempotentes |
| Errores de negocio (`409 AGOTADA`, `409 RETENIDA`, `409 RETENCION_NO_VIGENTE`, `422`) | No se reintentan; se informan al cliente |
| Errores 5xx / timeout al **retener** | Se reintentan; si se agotan, el ítem no se agrega y se responde `503` ("no pudimos reservar tu lugar, reintentá en unos minutos") |
| Errores 5xx / timeout al **liberar** | Se reintentan; si se agotan, el ítem igual se quita del carrito: la retención vence sola |
| Errores 5xx / timeout al **confirmar** | Se reintentan; si se agotan, la compra queda `PENDIENTE` y un proceso de recuperación reintenta con la misma clave hasta obtener una respuesta concluyente. Al cliente se le responde `202` y consulta el estado de la compra |
| Respuesta tardía | Se descarta si llega después del timeout. Como el reintento usa la misma clave, el resultado que Catálogo haya guardado se obtiene en el reintento siguiente |

Se evaluará un circuit breaker en D10.

### Comunicación asíncrona (RabbitMQ)

| Evento | Productor | Consumidor | Exchange / cola | Uso |
|---|---|---|---|---|
| `CompraConfirmada` | Reservas | Comprobantes | Cola durable con DLQ | Emitir factura y enviar correo. Lleva `email`, `items` (`descripcion`, `precio`) y `compraId` |
| `OfertaActualizada` | Catálogo | Indexador de Catálogo | Cola durable con DLQ | Actualizar el índice de búsqueda |
| `DisponibilidadCambiada` | Catálogo | Tiempo Real | Fanout `catalogo.disponibilidad`, cola exclusiva por instancia, **sin DLQ** | Avisar a los navegadores (estado completo + `version`) |
| `RetencionVencida` | Catálogo | Tiempo Real, Reservas | Fanout `catalogo.disponibilidad`; Tiempo Real sin DLQ, Reservas con cola durable `reservas.retenciones` con DLQ | Avisar al usuario; marcar el ítem del carrito como vencido |

- **Publicación confiable:** Reservas y Catálogo guardan el evento en una tabla *outbox* dentro de la misma transacción del cambio, y un proceso lo publica. Así no se pierde el evento si el broker no está disponible en ese momento.
- **Entrega al menos una vez:** el consumidor confirma (`ack`) solo después de procesar.
- **Idempotencia:** cada evento lleva `eventId`. Comprobantes usa además `compraId` como clave única, así que un evento duplicado no genera otro comprobante ni otro correo. Marcar un ítem como vencido es idempotente por naturaleza. Tiempo Real descarta versiones viejas.
- **Mensajes fallidos:** hasta 3 reintentos con espera creciente; luego el mensaje pasa a una **dead letter queue** (`*.dlq`) para análisis y reprocesamiento manual. **Excepción:** las colas de Tiempo Real no tienen DLQ porque sus eventos son avisos de mejor esfuerzo; el cliente reconsulta el estado al reconectar ([ADR-015](ADR-015-disponibilidad-en-tiempo-real.md)).

## Alternativas consideradas

- **Llamada REST de Reservas a Comprobantes.** Haría depender la confirmación de la compra del envío de correo. Descartada.
- **Kafka.** Adecuado para alto volumen y reprocesamiento de historial; para el volumen esperado RabbitMQ es más simple de operar y ofrece DLQ de forma nativa.
- **Publicar el evento directamente tras el commit (sin outbox).** Si el proceso cae entre el commit y la publicación, el evento se pierde. Descartada.
- **Retener y confirmar de forma asíncrona (por eventos).** El cliente necesita saber en el momento si obtuvo el cupo; una respuesta diferida complicaría la experiencia sin beneficio. Descartada.

## Consecuencias

- (+) La compra no depende de la disponibilidad del correo ni de Comprobantes.
- (+) No se pierden ni duplican comprobantes.
- (+) Todas las llamadas síncronas son idempotentes, así que reintentar nunca duplica retenciones ni confirmaciones.
- (−) El correo llega con cierta demora respecto de la confirmación.
- (−) Agregar al carrito y comprar dependen de Catálogo en tiempo real.
- (−) Una compra puede quedar unos instantes en `PENDIENTE` si Catálogo no responde; el cliente debe consultar su estado.

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-22 | La llamada síncrona Reservas → Catálogo deja de ser "obtener precios y definición del pack al confirmar" y pasa a ser la API interna de retenciones (retener, liberar, confirmar), todas idempotentes, por lo que los reintentos ya no se limitan a GET. Se agrega el estado `PENDIENTE` con recuperación cuando la confirmación no tiene respuesta concluyente. Se elimina el evento `OfertaPublicada` (Reservas ya no crea cupos). Se agregan `DisponibilidadCambiada` y `RetencionVencida` sobre un exchange fanout, sin DLQ para Tiempo Real. `CompraConfirmada` pasa a llevar `email` e `items` con `descripcion` y `precio`, alineado con el contrato 1.0.0 de Comprobantes. Catálogo también usa outbox. | Devolución de la Entrega 1: cupos en Catálogo con retención ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)), tiempo real ([ADR-015](ADR-015-disponibilidad-en-tiempo-real.md)) y contrato simplificado ([ADR-008](ADR-008-contrato-propio.md)). |
