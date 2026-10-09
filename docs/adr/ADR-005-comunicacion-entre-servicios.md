# ADR-005: Comunicación entre servicios

- **Estado:** Propuesta (versión inicial, a validar en Entrega 2)
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D5

## Contexto

Algunas interacciones necesitan una respuesta inmediata para continuar (confirmar el precio antes de cobrar). Otras son consecuencias que no deben demorar ni hacer fallar la operación principal (enviar un correo).

## Decisión

### Comunicación síncrona (REST/JSON)

**Reservas → Catálogo** (al confirmar compra): obtiene precios vigentes y definición del pack.

| Aspecto | Valor inicial |
|---|---|
| Timeout | 2 s por intento |
| Reintentos | Hasta 2, solo para lecturas (GET, idempotentes), con backoff exponencial (200 ms, 400 ms) y jitter |
| Errores 4xx | No se reintentan; se informan al cliente |
| Errores 5xx / timeout | Se reintentan; si se agotan, la compra no se confirma y se responde `503` con mensaje "no pudimos confirmar precios, reintentá en unos minutos". El carrito se conserva |
| Respuesta tardía | Se descarta si llega después del timeout; no altera la compra |

Se evaluará un circuit breaker en D10.

### Comunicación asíncrona (RabbitMQ)

| Evento | Productor | Consumidor | Uso |
|---|---|---|---|
| `CompraConfirmada` | Reservas | Comprobantes | Emitir factura y enviar correo |
| `OfertaPublicada` | Catálogo | Reservas | Crear el cupo de la oferta |
| `OfertaActualizada` | Catálogo | Indexador de Catálogo | Actualizar el índice de búsqueda |

- **Publicación confiable:** Reservas guarda el evento en una tabla *outbox* dentro de la misma transacción de la compra y un proceso lo publica. Así no se pierde el evento si el broker no está disponible en ese momento.
- **Entrega al menos una vez:** el consumidor confirma (`ack`) solo después de procesar.
- **Idempotencia:** cada evento lleva `eventId`; Comprobantes usa además `compraId` como clave única, por lo que un evento duplicado no genera otro comprobante ni otro correo.
- **Mensajes fallidos:** hasta 3 reintentos con espera creciente; luego el mensaje pasa a una **dead letter queue** (`*.dlq`) para análisis y reprocesamiento manual.

## Alternativas consideradas

- **Llamada REST de Reservas a Comprobantes.** Haría depender la confirmación de la compra del envío de correo. Descartada.
- **Kafka.** Adecuado para alto volumen y reprocesamiento de historial; para el volumen esperado RabbitMQ es más simple de operar y ofrece DLQ de forma nativa.
- **Publicar el evento directamente tras el commit (sin outbox).** Si el proceso cae entre el commit y la publicación, el evento se pierde. Descartada.

## Consecuencias

- (+) La compra no depende de la disponibilidad del correo ni de Comprobantes.
- (+) No se pierden ni duplican comprobantes.
- (−) El correo llega con cierta demora respecto de la confirmación.
- (−) La compra sí depende de Catálogo en tiempo real.
