# ADR-014: Límites de los servicios — cupos en Catálogo con retención temporal

- **Estado:** Aceptada
- **Fecha:** 2026-10-22
- **Decisión de la cátedra:** D1
- **Reemplaza a:** [ADR-001](ADR-001-limites-de-servicios.md)

## Contexto

### Por qué se revisa ADR-001

ADR-001 ubicó los cupos en **Reservas** para que confirmar una compra fuera una única transacción local en un solo servicio (criterio 2, "frontera transaccional"). Agregar al carrito no retenía cupo: el cupo se validaba recién al comprar.

En la devolución de la Entrega 1 el profesor pidió dos cambios que invalidan ese criterio:

1. Los cupos deben vivir en **Catálogo**, el servicio dueño de la oferta.
2. Al **agregar al carrito**, el cupo debe quedar **retenido por un tiempo** (se adopta 15 minutos, configurable con `RETENCION_MINUTOS`).

Con estos dos pedidos, la operación crítica ya no ocurre en un único momento: se reparte entre *agregar al carrito* (retener) y *comprar* (confirmar). Además, las ofertas y los cupos quedan en Catálogo, mientras que el carrito y las compras siguen en Reservas. Hay que redefinir los límites y explicar cómo se mantienen RN-01 (no sobrevender) y RN-02 (compra atómica) sin una transacción distribuida.

Otros cambios del alcance que también afectan los límites:

- Las sugerencias de productos fueron quitadas del alcance.
- Se agrega el desafío bonus de actualizaciones en tiempo real (ver [ADR-015](ADR-015-disponibilidad-en-tiempo-real.md)).

## Decisión

### Servicios y propiedad de datos

| Servicio | Responsabilidad | Propiedad de datos | Almacenamiento |
|---|---|---|---|
| **Catálogo** | Qué se vende **y cuánto queda**: ofertas, packs, búsqueda, inventario, retenciones de cupo, disponibilidad; reglas de pack (RN-03, RN-04, RN-05) y precios | Ofertas y packs; inventario; retenciones; confirmaciones; outbox | MongoDB (descripciones) + MySQL esquema `catalogo` (inventario, retenciones, confirmaciones, outbox) + OpenSearch (índice derivado) |
| **Reservas** | Quién compró: carrito, compras y sus estados | Carritos (cada ítem con `retencionId` y `expiraEn`), compras (`PENDIENTE`, `CONFIRMADA`, `RECHAZADA`), outbox | MySQL esquema `reservas` |
| **Comprobantes** | Emisión de facturas y envío por correo. Capacidad publicada | Comprobantes, claves de idempotencia, envíos | MySQL esquema `comprobantes` |
| **Usuarios** | Identidad y autenticación | Usuarios, roles, hashes de contraseña | MySQL esquema `usuarios` |
| **Tiempo Real** | Difundir cambios de disponibilidad y vencimientos de retención a los navegadores (SSE) | Ninguno (sin estado) | — |

Criterios que se mantienen de ADR-001: patrones de acceso distintos, capacidad reutilizable aislada (Comprobantes) y cambio por motivos distintos. El criterio 2 se reemplaza por este: **el dueño del cupo es el dueño de la oferta, y la atomicidad se obtiene con el patrón de retención**, como se explica abajo.

### Modelo de datos de cupos (Catálogo, MySQL esquema `catalogo`)

```
inventario(oferta_id PK, cupo_total, vendidos, version)
    CHECK (vendidos >= 0 AND vendidos <= cupo_total)

retenciones(id PK, oferta_id, cantidad, titular_id, estado, expira_en,
            compra_id NULL, pack_id NULL, grupo_id,
            idempotency_key UNIQUE, creada_en)
    estado ∈ {ACTIVA, CONFIRMADA, LIBERADA, VENCIDA}
    INDEX (oferta_id, estado, expira_en)

confirmaciones(compra_id PK, respuesta JSON, creada_en)

outbox(id PK, tipo, payload JSON, creado_en, publicado_en NULL)
```

- `titular_id` es el id del usuario (un carrito por usuario). Lo usan el límite de cupos por carrito y Tiempo Real para avisarle al usuario que su retención venció.
- `grupo_id` agrupa las retenciones creadas en una misma solicitud. Un pack genera una retención por opción elegida, todas con el mismo `grupo_id`, `pack_id` y `expira_en`.
- `confirmaciones` guarda la respuesta de cada confirmación exitosa. Es lo que hace idempotente la confirmación (ver abajo).
- `version` crece en cada cambio que afecta la disponibilidad de la oferta. Viaja en los eventos para que los clientes descarten avisos viejos (ADR-015).

> **Agregado al modelo inicial:** las tablas `confirmaciones` y las columnas `pack_id`, `grupo_id` y `creada_en` no estaban en el bosquejo original. Se agregan porque la idempotencia de la confirmación necesita persistir su resultado y porque el descuento del pack (RN-05) y la regla "pack todo o nada" necesitan saber qué retenciones forman un pack.

### Disponibilidad de una oferta

```
retenidos_activos = SUM(cantidad) WHERE oferta_id = ? AND estado = 'ACTIVA' AND expira_en > NOW()
libres            = cupo_total − vendidos − retenidos_activos
```

| Estado | Condición | Dato adicional |
|---|---|---|
| `DISPONIBLE` | `libres > 0` | `libres` |
| `RETENIDA` | `libres = 0` y `retenidos_activos > 0` | `proximaLiberacion` = menor `expira_en` de las retenciones activas |
| `AGOTADA` | `libres = 0` y `retenidos_activos = 0` (todo vendido) | — |

### Operaciones

**Retener** (al agregar al carrito):

```
BEGIN
  SELECT ... FROM inventario WHERE oferta_id IN (...) ORDER BY oferta_id FOR UPDATE
  -- para cada oferta: calcular libres (fórmula de arriba)
  -- validar límite por carrito y, si es un pack, RN-03 y RN-04
  -- si alguna oferta no alcanza → ROLLBACK y 409 (AGOTADA o RETENIDA)
  INSERT retenciones (..., estado = 'ACTIVA', expira_en = NOW() + INTERVAL :minutos MINUTE)
  UPDATE inventario SET version = version + 1 WHERE oferta_id IN (...)
  INSERT outbox DisponibilidadCambiada (una por oferta)
COMMIT
```

- Un pack retiene **todas** sus opciones en la misma transacción o ninguna.
- Las filas se bloquean **en orden ascendente de `oferta_id`** para que dos packs que comparten opciones no se bloqueen mutuamente (deadlock).
- `NOW()` es siempre el reloj de la base, no el de la instancia del servicio. Así, con varias instancias de Catálogo, todas usan la misma hora.
- La `Idempotency-Key` (`UNIQUE`) evita duplicar la retención si Reservas reintenta tras un timeout: el reintento devuelve la retención ya creada.

**Regla de bloqueo (vale para las cuatro operaciones):** toda operación que cambia retenciones o el inventario de una oferta **bloquea primero las filas de `inventario` involucradas, en orden ascendente de `oferta_id`**, y recién después toca `retenciones`. Así, retener, liberar, confirmar y vencer sobre la misma oferta se ejecutan de a una, y todas toman los bloqueos en el mismo orden (sin deadlocks). Las lecturas que se hacen después de tomar el bloqueo usan `FOR UPDATE` o `FOR SHARE`, que en InnoDB leen el último valor confirmado y no una instantánea anterior.

**Liberar** (quitar del carrito): `ACTIVA → LIBERADA`. Si la retención ya está liberada o vencida, responde `204` igual (idempotente). Si pertenece a un pack, se libera todo su grupo. Bloquea antes el inventario de las ofertas del grupo, incrementa `version` y publica `DisponibilidadCambiada` por outbox.

**Confirmar** (al comprar), con `Idempotency-Key = compraId`:

```
BEGIN
  SELECT oferta_id FROM retenciones WHERE id IN (...)
     -- lectura sin bloqueo, solo para saber qué inventario bloquear (oferta_id nunca cambia)
  SELECT ... FROM inventario WHERE oferta_id IN (...) ORDER BY oferta_id FOR UPDATE
  SELECT respuesta FROM confirmaciones WHERE compra_id = ? FOR SHARE
     si existe → COMMIT y devolver la respuesta guardada (reintento tardío o concurrente)
  SELECT ... FROM retenciones WHERE id IN (...) FOR UPDATE
  si alguna no está ACTIVA, ya venció (expira_en <= NOW()) o no es del titular
     → ROLLBACK, 409 con las retenciones no vigentes (ninguna cambia)
  -- si una retención pertenece a un pack, deben venir todas las de su grupo
  UPDATE retenciones SET estado = 'CONFIRMADA', compra_id = ? WHERE id IN (...)
  UPDATE inventario SET vendidos = vendidos + cantidad, version = version + 1 (por oferta)
  -- calcular precios vigentes (RN-06) con descuento de pack (RN-05)
  INSERT confirmaciones (compra_id, respuesta)
  INSERT outbox DisponibilidadCambiada (por oferta)
COMMIT
```

La respuesta incluye una línea por ítem con `descripcion` y `precio` (con el descuento ya aplicado si es un pack). Reservas no necesita consultar precios ni definiciones de pack a Catálogo.

> **Por qué confirmar bloquea el inventario aunque no cambie la cantidad de cupos ocupados.** Si confirmar bloqueara solo la retención, podría pasar esto: una retención vence a las 10:00:00; a las 09:59:59 empieza su confirmación y la encuentra vigente; a las 10:00:01, una retención nueva de otro cliente sobre la misma oferta ya no la cuenta como activa y toma ese cupo; después la confirmación suma `vendidos + 1`. El mismo cupo quedaría vendido y retenido a la vez (violación de RN-01). Con la regla de bloqueo, la retención nueva espera a que la confirmación termine y ve el cupo como vendido.

**Vencer** (proceso periódico, cada `RETENCION_BARRIDO_SEGUNDOS`, por defecto 15 s):

```
SELECT DISTINCT oferta_id FROM retenciones WHERE estado = 'ACTIVA' AND expira_en <= NOW()
para cada oferta_id (una transacción corta por oferta):
  BEGIN
    SELECT ... FROM inventario WHERE oferta_id = ? FOR UPDATE SKIP LOCKED
    si la fila está bloqueada (otra instancia u otra operación) → ROLLBACK; se procesa en el próximo barrido
    SELECT id, titular_id, expira_en FROM retenciones
      WHERE oferta_id = ? AND estado = 'ACTIVA' AND expira_en <= NOW() FOR UPDATE
    UPDATE esas retenciones SET estado = 'VENCIDA'
    UPDATE inventario SET version = version + 1 WHERE oferta_id = ?
    INSERT outbox RetencionVencida (una por retención) y DisponibilidadCambiada
  COMMIT
```

`SKIP LOCKED` se aplica sobre la fila de `inventario`: varias instancias de Catálogo pueden correr el barrido a la vez sin procesar la misma oferta, y el barrido nunca hace esperar a una retención o confirmación de un cliente.

> **La validez depende de `expira_en > NOW()` en la consulta, no de este proceso.** Si el proceso se atrasa o se cae, una retención vencida igual deja de contar como retenida y ya no se puede confirmar. El proceso solo cambia la etiqueta a `VENCIDA` y avisa.

### API interna de Catálogo (no publicada en el gateway)

| Operación | Método y ruta | Respuestas |
|---|---|---|
| Retener | `POST /internal/retenciones` (header `Idempotency-Key`) | `201` retenciones creadas con `expiraEn` · `409 AGOTADA` · `409 RETENIDA` con `proximaLiberacion` · `422` reglas de pack o límite por carrito |
| Liberar | `DELETE /internal/retenciones/{id}` | `204` (también si ya estaba liberada o vencida) |
| Confirmar | `POST /internal/retenciones/confirmacion` (header `Idempotency-Key` = `compraId`) | `200` líneas con descripción y precio · `409 RETENCION_NO_VIGENTE` con las retenciones afectadas |

Política de Reservas al llamarla: **timeout de 2 s**, **hasta 2 reintentos** con backoff exponencial (200 ms, 400 ms) y jitter. Los reintentos son seguros porque las tres operaciones son idempotentes. Los **409 y 422 de negocio no se reintentan**.

### Cómo se preserva la atomicidad (RN-01 y RN-02)

1. **RN-01 (sin sobreventa):** retener, liberar, confirmar y vencer ocurren dentro de una transacción local de Catálogo con la fila del inventario bloqueada (`FOR UPDATE`), siempre antes de tocar las retenciones. Dos clientes que piden el último cupo al mismo tiempo quedan serializados: el segundo ve `libres = 0` y recibe `409`. Una confirmación y una retención nueva sobre la misma oferta también quedan serializadas, así que una retención que vence durante su confirmación no libera ese cupo para otro. El `CHECK` de la base es una segunda barrera.
2. **RN-02 (compra atómica):** todas las retenciones de una compra pasan a `CONFIRMADA` **en una sola transacción local de Catálogo**, o ninguna. No hay una transacción distribuida: lo que se coordina entre servicios es solo el **estado de la compra en Reservas**, y eso se resuelve con idempotencia:
   - Si Catálogo responde `200`, Reservas marca la compra `CONFIRMADA` y publica `CompraConfirmada` por outbox.
   - Si responde `409`, la compra queda `RECHAZADA` y se informa qué ítems vencieron. Las demás retenciones siguen activas hasta su vencimiento, así que el cliente puede volver a agregar el ítem vencido y reintentar.
   - Si **no hay respuesta concluyente** (timeout o 5xx tras los reintentos), la compra queda `PENDIENTE` y un **proceso de recuperación** reintenta con la misma clave. Si Catálogo ya había confirmado, devuelve la respuesta guardada en `confirmaciones`; si no, confirma o rechaza. El proceso **siempre termina**: a lo sumo en `RETENCION_MINUTOS`, las retenciones vencen y la respuesta pasa a ser `409`.
3. **Abandono del carrito sin compensación:** si el usuario se va, no hay que deshacer nada. La retención vence sola y el cupo vuelve a contar como libre. El vencimiento cumple el papel de compensación automática.

### Reglas asociadas

- `RETENCION_MINUTOS` (por defecto 15): duración de una retención.
- `RETENCION_MAX_CUPOS_POR_OFERTA` (por defecto 6) **[SUPUESTO]**: máximo de cupos retenidos por oferta y por carrito (titular). Evita que un usuario bloquee todo el cupo sin intención de comprar. Lo controla Catálogo dentro de la transacción de retención.

### Relaciones resultantes

- Reservas → Catálogo: API interna síncrona (retener, liberar, confirmar).
- Catálogo → RabbitMQ: `OfertaActualizada` (indexación), `DisponibilidadCambiada` y `RetencionVencida` (outbox).
- Reservas consume `RetencionVencida` para marcar el ítem del carrito como vencido.
- Reservas → Comprobantes: `CompraConfirmada`, que incluye el email del cliente y las líneas con descripción y precio. Reservas obtiene el email del token: Usuarios lo incluye como claim en el JWT y el gateway lo propaga en el header `X-Usuario-Email`. Reservas lo guarda en la compra al crearla, así el evento y el proceso de recuperación no dependen de ningún otro servicio.
- Tiempo Real consume `DisponibilidadCambiada` y `RetencionVencida` (ADR-015).
- Se eliminan la consulta de precios Reservas → Catálogo y el evento `OfertaPublicada`: Reservas ya no guarda cupos, así que no necesita enterarse de las ofertas nuevas.

## Alternativas consideradas

- **Mantener los cupos en Reservas y agregar la retención allí.** Era lo más simple transaccionalmente (todo en un esquema), pero no respondía al pedido del profesor. Además, Reservas necesitaría las definiciones de pack de Catálogo para validar al retener (otra vez una consulta síncrona) y Catálogo no podría mostrar la disponibilidad sin replicarla. Descartada.
- **Saga con compensación** (descontar en Catálogo al comprar y compensar si algo falla después). Agrega estados intermedios visibles, compensaciones que también pueden fallar y más lógica de coordinación. Con la retención, la "reserva previa" de la saga ya existe y su compensación es el vencimiento, que no requiere ninguna llamada. Descartada.
- **Retención en Redis con TTL.** El vencimiento automático es natural, pero la retención y el inventario quedan en almacenes distintos y no se pueden actualizar en una misma transacción con `vendidos` y el outbox. Las notificaciones de vencimiento de Redis no garantizan entrega, contar los "retenidos activos" por oferta requiere estructuras adicionales y suma un componente de infraestructura. Descartada.

## Consecuencias

- (+) Se respeta RN-01 y RN-02 con transacciones locales, sin transacción distribuida.
- (+) El cliente tiene su cupo asegurado mientras completa la compra (mejor experiencia).
- (+) La disponibilidad (`DISPONIBLE`, `RETENIDA`, `AGOTADA`) se calcula donde están los datos y alimenta el tiempo real (ADR-015).
- (+) El abandono del carrito no requiere compensación.
- (+) Reservas ya no consulta precios: confirmar devuelve los precios.
- (−) **Agregar al carrito** pasa a depender de Catálogo en tiempo real (antes era local). Si Catálogo no está disponible, no se puede agregar (ver D10).
- (−) Un usuario puede bloquear cupo hasta 15 minutos sin comprar; se acota con el límite de 6 cupos por oferta y por carrito.
- (−) Catálogo usa dos almacenes (MongoDB y MySQL). El alta de una oferta escribe en ambos sin transacción común: si falla la segunda escritura, la oferta queda visible pero no retenible hasta reconciliarse (ver limitaciones en ARCHITECTURE).
- (−) Las reglas de pack (RN-03, RN-04, RN-05) pasan a Catálogo. Reservas conserva el ciclo de vida de carrito y compra y la recuperación de compras pendientes. Esto se tendrá en cuenta en D2 al elegir el estilo interno de cada servicio.
- (−) Catálogo recibe más escrituras concurrentes (retenciones). Se acota bloqueando solo las filas de inventario involucradas.
- (−) Todas las operaciones sobre una misma oferta se serializan en su fila de inventario. Una oferta muy demandada (por ejemplo, el último vuelo de un feriado) es un punto de contención; las transacciones son cortas, así que se acepta y se medirá en las pruebas de carga (D13).

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-22 | Se agrega la **regla de bloqueo**: retener, liberar, confirmar y vencer bloquean primero el inventario (en orden de `oferta_id`) y después las retenciones. Confirmar vuelve a consultar `confirmaciones` con `FOR SHARE` después de tomar el bloqueo. El barrido de vencidas pasa a una transacción por oferta con `SKIP LOCKED` sobre `inventario`. Liberar incrementa `version` y publica `DisponibilidadCambiada`. Se define que el email de `CompraConfirmada` sale del JWT (claim de Usuarios, header `X-Usuario-Email` del gateway). | Revisión interna del diseño. (1) Como confirmar y vencer bloqueaban la retención y no el inventario, una retención que vencía durante su propia confirmación podía ser tomada por otro cliente: el cupo quedaba vendido y retenido a la vez (viola RN-01). Además, el orden de bloqueo era inverso al de retener. (2) Un reintento de Reservas que llegaba mientras la primera confirmación seguía en curso encontraba las retenciones ya `CONFIRMADA` y respondía `409` en lugar de la respuesta guardada. (3) `CompraConfirmada` necesita el email del cliente y no estaba definido de dónde lo obtiene Reservas. La decisión de D1 no cambia, por eso se revisa este ADR en lugar de reemplazarlo. |
| 2026-10-23 | **Validación de la Entrega 2.** Verificado: Comprobantes, el único servicio con código, respeta los límites decididos. Tiene su propio esquema y un usuario de MySQL que no puede leer los esquemas de los demás (`ERROR 1142`); recibe las compras solo por el evento `CompraConfirmada`, con el email y las líneas que define este ADR, y no conoce ofertas, cupos ni retenciones. **Sin verificar todavía**, porque Catálogo y Reservas no tienen código: el patrón de retención, la regla de bloqueo, la confirmación atómica y la ausencia de sobreventa con concurrencia. Se van a verificar con tests de integración contra MySQL real y con el escenario B2 de [ADR-013](ADR-013-capacidad-y-costos.md) (200 usuarios contra 50 cupos de una misma oferta). La decisión se mantiene sin cambios. | El enunciado pide validar D1 en la Entrega 2. Se deja constancia de qué se pudo verificar y qué no, en lugar de darla por validada completa. |
