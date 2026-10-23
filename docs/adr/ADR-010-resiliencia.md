# ADR-010: Resiliencia

- **Estado:** Aceptada
- **Fecha:** 2026-10-23
- **Decisión de la cátedra:** D10

## Contexto

El enunciado pide identificar las dependencias de cada servicio, definir qué pasa cuando una falla o responde con lentitud, implementar al menos un mecanismo de protección y demostrarlo con una caída provocada registrada en `docs/POSTMORTEM.md`.

Ya hay piezas decididas que este ADR ordena y completa: timeouts y reintentos idempotentes ([ADR-005](ADR-005-comunicacion-entre-servicios.md)), outbox y DLQ ([ADR-005](ADR-005-comunicacion-entre-servicios.md), [ADR-006](ADR-006-busqueda.md)), compras `PENDIENTE` con recuperación y retenciones que vencen solas ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)), eventos de tiempo real de mejor esfuerzo ([ADR-015](ADR-015-disponibilidad-en-tiempo-real.md)) y caché opcional ([ADR-007](ADR-007-cache.md)).

## Decisión

### Principios

1. **Ninguna espera sin límite:** toda llamada de red tiene timeout.
2. **Reintentar solo lo idempotente**, con backoff y jitter, y con un tope.
3. **No insistir contra una dependencia caída:** circuit breaker en la llamada síncrona crítica (Reservas → Catálogo).
4. **Lo que no puede perderse se escribe primero en la base** (outbox) y se entrega después, con reintentos y DLQ.
5. **Degradar por funcionalidad:** si cae una dependencia, solo deja de funcionar lo que la necesita.

### Dependencias y comportamiento ante fallas

| Servicio | Dependencia | Si **falla** | Si **responde lento** |
|---|---|---|---|
| **Gateway** | Usuarios | No se puede registrar ni iniciar sesión. Las sesiones abiertas siguen: el gateway valida el JWT localmente con la clave compartida, sin llamar a Usuarios | Timeout de 5 s en `/api/auth`; responde `504` |
| | Catálogo, Reservas | El gateway saca la instancia caída del balanceo (verifica `/health`, D12); si no queda ninguna, `503` solo en esas rutas | Timeout por ruta (10 s); `504` |
| | Tiempo Real | Los streams SSE se cortan; el navegador reintenta conectarse y, mientras tanto, la interfaz sigue funcionando sin actualizaciones en vivo | — (las conexiones son largas por diseño) |
| **Usuarios** | MySQL | Login y registro responden `503`; `/health` falla | Timeout de consultas 2 s |
| **Catálogo** | MySQL (`catalogo`) | Retener, liberar, confirmar y disponibilidad responden `503`; `/health` falla y la instancia sale del balanceo. Búsqueda y detalle siguen | Timeout de transacción; los bloqueos esperan como máximo `innodb_lock_wait_timeout` (propuesta: 5 s, configurado por sesión) y luego se responde `503` |
| | MongoDB | El detalle responde desde la caché si la oferta estaba cacheada; si no, `503`. Las modificaciones del administrador fallan. Confirmar falla (necesita los precios, RN-06) y Reservas deja la compra `PENDIENTE` | Timeout 2 s |
| | OpenSearch | Solo la búsqueda responde `503` ("la búsqueda no está disponible, probá en unos minutos"). Detalle, carrito y compra siguen. Las indexaciones se reintentan y, si se agotan, van a la DLQ | Timeout 2 s en búsqueda |
| | RabbitMQ | Nada falla para el usuario: los eventos se acumulan en el outbox (tabla MySQL o marca en el documento, ADR-006) y se publican al volver. Tiempo real e indexación se atrasan | — |
| | Redis | Se lee MongoDB directamente; más latencia, sin errores ([ADR-007](ADR-007-cache.md)) | Timeout 50 ms; corte de 30 s tras 5 errores |
| **Reservas** | MySQL | Carrito y compras responden `503`; `/health` falla | Timeout 2 s |
| | Catálogo (API interna) | Retener: `503`, el ítem no se agrega. Liberar: el ítem se quita igual (la retención vence sola). Confirmar: la compra queda `PENDIENTE` (`202`) y la recuperación reintenta | Timeout 2 s, 2 reintentos; luego, igual que si fallara. El circuit breaker evita seguir cargándolo |
| | RabbitMQ | Las compras se confirman igual; `CompraConfirmada` espera en el outbox y el correo se atrasa. `RetencionVencida` no llega: el carrito usa `expiraEn` | — |
| **Comprobantes** | MySQL | `POST` y `GET` responden `503`; el consumidor de `CompraConfirmada` no confirma el mensaje y lo reintenta | Timeout 2 s |
| | RabbitMQ | La API sigue emitiendo: `EnvioSolicitado` espera en el outbox y el correo sale al volver. `CompraConfirmada` queda en la cola de Reservas | — |
| | SMTP | El comprobante queda emitido (`PENDIENTE`); el envío se reintenta y, si se agota, termina `FALLIDO` con el mensaje en la DLQ. **Es la caída elegida para demostrar** | Timeout SMTP 10 s por intento; cuenta como fallo |
| **Tiempo Real** | RabbitMQ | Deja de recibir avisos; las conexiones siguen abiertas sin eventos. Al volver, el cliente se pone al día consultando la disponibilidad | — |

### Mecanismos

| Mecanismo | Dónde | Detalle |
|---|---|---|
| **Timeouts** | Toda llamada de red | Valores de la tabla anterior; ADR-005 para Reservas → Catálogo |
| **Reintentos idempotentes** | Reservas → Catálogo; consumidores de RabbitMQ | ADR-005: 2 reintentos (200 ms, 400 ms, con jitter) en HTTP; 3 reintentos con espera creciente en colas |
| **Circuit breaker** | Reservas → Catálogo | Ver abajo |
| **Outbox** | Reservas, Catálogo, Comprobantes | Ningún evento se pierde si el broker o el servicio caen después del commit |
| **DLQ** | Todas las colas salvo las de Tiempo Real | Los mensajes que agotan los reintentos se guardan para análisis y reprocesamiento |
| **Health checks** | `GET /health` en cada servicio, sin autenticación | Verifica las dependencias imprescindibles del servicio (su base de datos); **no** incluye las opcionales (Redis, OpenSearch), para que una falla parcial no saque la instancia del balanceo. Lo usan Docker y el gateway (D12) |
| **Límite de solicitudes** | Gateway; API pública de Comprobantes (60 por minuto por API Key) | Un consumidor que se excede no degrada al resto |
| **Degradación controlada** | Ver la tabla de dependencias | Caché opcional, búsqueda separada del flujo de compra, compra `PENDIENTE`, tiempo real de mejor esfuerzo |

### Circuit breaker Reservas → Catálogo

| Parámetro | Valor inicial |
|---|---|
| Se abre | 5 fallos seguidos (timeout o 5xx) o más del 50 % de fallos en las últimas 20 llamadas |
| Abierto | 30 s; las llamadas fallan al instante sin ir a la red |
| Semiabierto | Deja pasar 1 llamada; si funciona se cierra, si falla vuelve a abrirse |
| Qué cuenta como fallo | Timeout, error de conexión y 5xx. Los `409` y `422` de negocio **no** cuentan |

Con el circuito abierto: retener responde `503` al instante; liberar quita el ítem igual; confirmar deja la compra `PENDIENTE` y responde `202` al instante. El proceso de recuperación de compras pendientes también respeta el circuito: no reintenta mientras está abierto.

Los reintentos ocurren **dentro** del circuito: un intento con sus 2 reintentos cuenta como **una** llamada fallida. Así una caída no multiplica por tres la carga sobre Catálogo.

Métrica `circuit_breaker_estado{destino="catalogo"}` (0 cerrado, 1 semiabierto, 2 abierto) en el tablero de D11.

### Caída provocada a demostrar: servidor de correo caído

**Escenario:**

1. Con todo funcionando, se detiene Mailpit: `docker compose stop mailpit`.
2. Se emiten comprobantes por la API (`POST /v1/comprobantes`) y, cuando exista el flujo completo, con una compra.
3. Se observan los logs, las métricas, el estado de los comprobantes (`GET /v1/comprobantes/{id}`) y las colas en la consola de RabbitMQ.
4. A los 3 minutos se vuelve a levantar Mailpit (`docker compose start mailpit`) y se emite un comprobante nuevo.

**Resultado esperado:**

| Momento | Qué se espera ver |
|---|---|
| Emisión | `POST` responde `201` con `estadoEnvio: PENDIENTE`, en el mismo tiempo que con el correo funcionando. **La emisión no depende del correo** |
| Reintentos | El worker registra 3 reintentos fallidos por comprobante, con espera creciente (5 s, 15 s y 30 s, como está implementado; ver [ADR-005](ADR-005-comunicacion-entre-servicios.md#revisiones)), cada uno en un log `WARN` con el `X-Correlation-Id` de la emisión |
| Agotados los reintentos | El comprobante pasa a `FALLIDO` y el mensaje aparece en `comprobantes.envios.dlq` |
| Mientras tanto | Las compras propias se siguen confirmando: Reservas no depende de Comprobantes |
| Al volver Mailpit | Los comprobantes nuevos llegan a `ENVIADO` |

**Límite conocido, que también se va a mostrar:** `FALLIDO` es un estado final (dominio de Comprobantes y contrato 1.0.2). Reprocesar el mensaje de la DLQ **no reenvía** el correo, porque el caso de uso solo envía comprobantes `PENDIENTE`. Se registra como acción de mejora: una operación administrativa de "reintentar envío" que pase `FALLIDO → PENDIENTE` y vuelva a encolar.

**Requisitos para poder demostrarlo:** ya están implementados en Comprobantes la API, el repositorio MySQL con outbox, el publicador de outbox y el worker de envíos con colas de reintento y DLQ (Entrega 2). Con las esperas implementadas, un comprobante llega a `FALLIDO` unos 50 s después de emitirse.

**Caída secundaria (si llega el tiempo):** Catálogo caído durante una compra. Se espera ver el circuit breaker abriéndose, la compra en `PENDIENTE` y su resolución automática cuando Catálogo vuelve. Requiere Reservas y Catálogo implementados.

### Qué se registra en `docs/POSTMORTEM.md`

| Sección | Contenido |
|---|---|
| Resumen | Qué se provocó, cuánto duró, qué impacto tuvo |
| Impacto | Cantidad de comprobantes afectados, cuántos terminaron `FALLIDO`, si alguna compra o emisión falló (lo esperado es que ninguna) |
| Línea temporal | Hora de cada evento: caída, primer reintento fallido, primera alerta, primer `FALLIDO`, recuperación, primer `ENVIADO` posterior |
| Detección | Qué lo mostró primero: alerta, métrica (crecimiento de la DLQ, errores de SMTP) o log; cuánto tardó en detectarse |
| Respuesta | Qué se hizo y en qué orden |
| Causa | Servidor SMTP detenido (provocado) |
| Qué funcionó y qué no | Mecanismos que se comportaron como se esperaba y diferencias con el resultado esperado de este ADR |
| Acciones de mejora | Con responsable; por ejemplo, la operación de reintento de envíos `FALLIDO` |
| Evidencia | Capturas del tablero, extractos de logs, conteo de la DLQ |

## Alternativas consideradas

- **Solo timeouts y reintentos, sin circuit breaker.** Ante una caída de Catálogo, cada solicitud de Reservas esperaría hasta 3 timeouts (≈ 7 s) antes de fallar, ocupando conexiones y cargando a Catálogo justo cuando intenta recuperarse. Descartada.
- **Circuit breaker en todas las dependencias.** Para las colas y la caché no aporta: el outbox y la DLQ ya resuelven la falla de forma asíncrona, y la caché tiene su propio corte simple. Se reserva para llamadas síncronas a servicios (Reservas → Catálogo y el proveedor externo, [ADR-009](ADR-009-consumo-del-proveedor.md)). Descartada como regla general.
- **Service mesh (por ejemplo, Istio) para timeouts, reintentos y circuit breaking.** Resuelve esto sin código, pero agrega una infraestructura que no se justifica para el tamaño del sistema ni para Docker Compose. Descartada.
- **Provocar la caída de MySQL como demostración principal.** Es más drástico, pero solo muestra `503` y health checks fallando; la caída del correo muestra varios mecanismos a la vez (separación entre emisión y envío, outbox, reintentos, DLQ, estado final). Descartada como principal.

## Consecuencias

- (+) Cada dependencia tiene un comportamiento definido ante falla y ante lentitud.
- (+) El flujo de compra no depende del correo, de la búsqueda, de la caché ni del tiempo real.
- (+) El circuit breaker protege a Catálogo y da respuestas rápidas al usuario durante una caída.
- (−) Más estados que explicar al usuario (`PENDIENTE` en compras y en envíos).
- (−) Los valores (timeouts, umbrales, esperas) son iniciales; se ajustan con las pruebas de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).
- (−) Un envío `FALLIDO` no tiene, por ahora, forma de reintentarse.
- (−) La demostración depende de que se completen las partes pendientes de Comprobantes.
