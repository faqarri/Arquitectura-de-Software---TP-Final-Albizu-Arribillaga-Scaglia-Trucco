# ADR-004: Consistencia y concurrencia

- **Estado:** Aceptada
- **Fecha:** 2026-11-11
- **Decisión de la cátedra:** D4
- **Relacionada con:** [ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md) (cupos y retenciones), [ADR-005](ADR-005-comunicacion-entre-servicios.md) (comunicación, outbox, reintentos), [ADR-008](ADR-008-contrato-propio.md) (idempotencia de Comprobantes), [ADR-006](ADR-006-busqueda.md) (indexación)

## Contexto

El enunciado pide al menos una operación con garantías de consistencia, de modo que **no pueda duplicarse ni perderse información** (§2.8), y que D4 justifique qué operaciones requieren garantías especiales y cómo se tratan la concurrencia, la duplicación, la pérdida y las fallas parciales.

En este sistema el riesgo se concentra en la compra, que atraviesa tres servicios con bases propias: Catálogo (cupos), Reservas (carrito y compra) y Comprobantes (factura y correo). No hay una transacción que los abarque, y entre ellos hay red: cualquier llamada o mensaje puede llegar dos veces, tarde o nunca.

Las decisiones de detalle se tomaron en los ADR de cada tema. Este ADR las reúne en un solo lugar: qué garantía tiene cada operación y con qué mecanismo se logra.

## Decisión

### Principios

1. **Transacciones locales, nunca distribuidas.** Cada servicio garantiza sus invariantes dentro de una transacción de su propia base. Entre servicios se coordina con idempotencia y reintentos, no con 2PC.
2. **Entrega al menos una vez + procesamiento idempotente = efecto exactamente una vez.** Toda llamada o mensaje se puede repetir sin consecuencias, así que reintentar siempre es seguro.
3. **Lo que tiene que ocurrir se registra antes de intentarlo.** Los eventos van a un outbox en la misma transacción que el cambio que los origina; un publicador los envía después.
4. **Un único reloj:** los vencimientos se comparan con `NOW()` de la base, no con la hora de cada instancia.

### Operaciones con garantías especiales

| Operación | Riesgo | Concurrencia | Duplicación | Pérdida | Falla parcial |
|---|---|---|---|---|---|
| **Retener cupo** (Catálogo, al agregar al carrito) | Vender el mismo lugar dos veces (RN-01) | `SELECT ... FOR UPDATE` sobre `inventario`, **en orden ascendente de `oferta_id`** (sin deadlocks entre packs), y recién después se leen las retenciones. Transacción `READ COMMITTED`. `CHECK (vendidos <= cupo_total)` como segunda barrera | `Idempotency-Key` con `UNIQUE` en `grupos_retencion`: un reintento devuelve el grupo ya creado; dos solicitudes simultáneas con la misma clave chocan en la restricción y la segunda lee la de la primera | `DisponibilidadCambiada` en el outbox de MySQL, en la misma transacción | Un pack retiene todas sus opciones o ninguna (una sola transacción, RN-07) |
| **Confirmar la compra** (Catálogo) | Confirmar a medias o con un cupo vencido (RN-02, RN-11) | Bloquea primero el inventario de todas las ofertas de la compra y después las retenciones: una retención que vence durante su propia confirmación no puede ser tomada por otro | La respuesta se guarda en `confirmaciones` con `compra_id` como clave: un reintento, aunque llegue mientras la primera sigue en curso, devuelve la misma respuesta y no suma vendidos de nuevo | `DisponibilidadCambiada` en el outbox, en la misma transacción | Todas las retenciones pasan a `CONFIRMADA` o ninguna; con una sola no vigente, responde `409` y no cambia nada |
| **Comprar** (Reservas) | Dos compras por un doble clic o un reintento del navegador; una compra trabada | Una compra por intento, identificada por su `Idempotency-Key` | `Idempotency-Key` obligatoria: la misma clave devuelve la misma compra | `CompraConfirmada` en el outbox de Reservas, en la misma transacción que pasa la compra a `CONFIRMADA` | Si Catálogo no responde (timeout o 5xx tras 2 reintentos), la compra queda `PENDIENTE` y un proceso de recuperación (cada 10 s) reintenta con la misma clave. Termina siempre: a lo sumo cuando vencen las retenciones, la respuesta pasa a ser `409` |
| **Liberar y vencer retenciones** (Catálogo) | Liberar dos veces; dos instancias venciendo lo mismo | Mismo orden de bloqueo. El barrido usa `FOR UPDATE SKIP LOCKED` sobre el inventario: varias instancias no procesan la misma oferta y nunca hacen esperar a un cliente | Liberar responde `204` siempre; vencer solo toca retenciones `ACTIVA` | `RetencionVencida` en el outbox | La validez depende de `expira_en > NOW()` en cada consulta, no del barrido: si el barrido se atrasa o cae, la retención vencida igual deja de contar |
| **Emitir el comprobante** (Comprobantes) | Dos facturas por la misma compra (RN-08) | Inserción con restricción única | `UNIQUE (consumidor, idempotency_key)`; para las compras internas la clave es el `compraId`. Un `CompraConfirmada` repetido se confirma sin emitir otra factura; el mismo `compraId` con otro contenido va a la DLQ | El evento interno `EnvioSolicitado` en el outbox, en la misma transacción que el comprobante | La compra no depende del correo: si el envío falla, el comprobante sigue emitido y el envío se reintenta |
| **Enviar el correo** (Comprobantes) | Correo perdido o repetido | Un worker por mensaje | Si el comprobante ya está `ENVIADO`, el mensaje se confirma sin enviar | Reintentos con espera creciente (5, 15 y 30 s); al agotarlos, `FALLIDO` y el mensaje queda en la DLQ para reprocesar | El comprobante queda `FALLIDO` y visible; no se pierde |
| **Indexar una oferta** (Catálogo → OpenSearch) | Que un evento viejo o repetido vuelva atrás el índice | — | Indexación con `version_type = external`: OpenSearch rechaza una versión menor o igual a la indexada | Outbox dentro del documento de MongoDB (`version` + `pendientePublicar`, una sola escritura atómica) y cola con reintentos y DLQ | El índice se puede reconstruir completo desde MongoDB sin cortar la búsqueda |

### Consumo de mensajes

Todos los consumidores (`CompraConfirmada`, `EnvioSolicitado`, `RetencionVencida`, `OfertaActualizada`) confirman el mensaje (`ack`) **después** de procesarlo, son idempotentes y separan los errores: uno temporal se reintenta con espera creciente; uno permanente (mensaje mal formado o regla incumplida) va directo a la DLQ. Un mensaje nunca se descarta en silencio.

## Alternativas consideradas

- **Transacción distribuida (2PC/XA) entre Catálogo y Reservas.** Daría atomicidad directa, pero bloquea recursos de dos servicios mientras se coordina, acopla su disponibilidad (si uno cae, el otro queda trabado) y no la soportan bien ni RabbitMQ ni MongoDB. Descartada.
- **Saga con compensación** (descontar el cupo al comprar y devolverlo si algo falla después). Agrega estados intermedios visibles y compensaciones que también pueden fallar. Con la retención, la "reserva previa" ya existe y su compensación es el vencimiento, que no requiere ninguna llamada ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)). Descartada.
- **Concurrencia optimista** (`UPDATE inventario ... WHERE version = ?` y reintentar si cambió). Evita bloquear, pero con una oferta muy pedida (el último lugar de un vuelo) casi todos los intentos fallan y se reintentan, y la regla de "primero el inventario, después las retenciones" queda más difícil de garantizar. El bloqueo pesimista con transacciones cortas es más simple de razonar y de defender. Descartada.
- **Aislamiento `SERIALIZABLE`.** Garantiza lo mismo sin pensar el orden de bloqueo, pero convierte lecturas en bloqueos compartidos y aumenta los deadlocks bajo carga. Se prefirió `READ COMMITTED` con bloqueos explícitos. Descartada.
- **Bloqueos distribuidos en Redis.** Suman un componente cuya caída afectaría la operación crítica, y el cupo igual tiene que actualizarse en MySQL. Descartada.
- **Publicar el evento directamente después del commit, sin outbox.** Si el servicio cae entre el commit y la publicación, el evento se pierde. Descartada.

## Consecuencias

- (+) No hay sobreventa ni compras a medias, aun con varias instancias de Catálogo y reintentos.
- (+) Reintentar es siempre seguro, así que cada servicio puede aplicar timeouts y reintentos sin coordinarse con los demás.
- (+) Ningún evento se pierde por una caída del servicio o del broker: queda en el outbox hasta publicarse.
- (+) Una compra trabada se resuelve sola, sin intervención manual.
- (−) La consistencia entre servicios es **eventual**: la factura llega segundos después de la compra, el índice de búsqueda tarda alrededor de 1 s y el carrito marca un ítem vencido hasta 15 s después.
- (−) Todas las operaciones sobre una misma oferta se serializan en su fila de inventario; una oferta muy demandada es un punto de contención (medido en [ADR-013](ADR-013-capacidad-y-costos.md)).
- (−) Un evento puede publicarse más de una vez (por ejemplo, con dos instancias de Catálogo). Es correcto porque los consumidores son idempotentes, pero obliga a que **todo** consumidor nuevo lo sea.
- (−) Si Comprobantes cae justo después de enviar un correo y antes de marcarlo `ENVIADO`, el reintento lo vuelve a enviar: el correo es "al menos una vez". La factura, en cambio, es una sola.
- (−) El alta de una oferta escribe en MySQL y en MongoDB sin transacción común. Se escribe primero el inventario: si falla MongoDB, queda un inventario sin oferta, que nadie puede retener.

## Verificación

| Garantía | Evidencia |
|---|---|
| 20 clientes contra el último cupo: gana exactamente uno | `TestConcurrenciaUltimoCupoGanaUnoSolo` (`services/catalogo/internal/repository`, MySQL 8.4 real) |
| Sin sobreventa: 40 clientes contra 10 cupos, se retienen 10 | `TestConcurrenciaSinSobreventa` |
| Retener y confirmar idempotentes, también en paralelo | `TestRetenerEsIdempotente`, `TestConfirmarEsAtomicaEIdempotente` |
| Todo o nada (pack y confirmación con una retención vencida) | `TestPackTodoONada`, `TestConfirmarConRetencionVencidaNoConfirmaNada`, `TestConfirmarPackIncompleto` |
| Las respuestas de la API interna cumplen el contrato | `TestRespuestasCumplenElContratoInterno` (`services/catalogo/internal/handlers`) |
| Una factura por compra aunque el evento llegue repetido | Tests de Comprobantes (`internal/adapters/mysql`, `internal/adapters/rabbit`) |
| Vencimiento automático y aviso a Reservas, en vivo | [Ensayo del 2026-11-09](../ensayos/2026-11-09-catalogo.md) |
| Caída del correo sin perder el comprobante | Ensayo en [ADR-010](ADR-010-resiliencia.md#revisiones) |

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-11-11 | Versión inicial. Reúne en un solo ADR las garantías que estaban repartidas en ADR-014, ADR-005, ADR-008 y ADR-006, ya implementadas y verificadas. | El índice tenía D4 como pendiente y el enunciado pide un ADR por decisión obligatoria. |
