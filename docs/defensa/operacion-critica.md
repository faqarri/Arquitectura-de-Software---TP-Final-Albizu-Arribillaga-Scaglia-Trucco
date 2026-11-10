# Guía para la defensa: la operación crítica (retenciones de cupo)

Resumen para explicarle al grupo, en unos 15 minutos, la parte que más van a preguntar en la defensa individual (plan §9). El detalle está en [ADR-014](../adr/ADR-014-cupos-en-catalogo-con-retencion.md) y en el código de `services/catalogo/internal/repository/inventario.go`.

## 1. El problema

Dos clientes no pueden quedarse con el mismo cupo (RN-01) y una compra se confirma entera o nada (RN-02). Pero los cupos viven en **Catálogo** y las compras en **Reservas**: no hay una transacción que abarque los dos servicios.

## 2. La solución: retener primero, confirmar después

1. **Retener** (al agregar al carrito): Catálogo aparta el cupo por 15 minutos.
2. **Confirmar** (al comprar): Catálogo pasa todas las retenciones de la compra a vendidas en **una sola transacción local**.
3. **Si el cliente se va**, no hay que deshacer nada: la retención vence sola. El vencimiento hace de compensación.

Cuentas que hay que saber de memoria:

```
retenidos = SUM(cantidad) de retenciones ACTIVA con expira_en > NOW()
libres    = cupo_total − vendidos − retenidos
```

`DISPONIBLE` si hay libres; `RETENIDA` si no hay libres pero hay retenciones activas (con `proximaLiberacion`); `AGOTADA` si todo está vendido.

## 3. Por qué no se vende dos veces el mismo cupo

- Cada operación (retener, liberar, confirmar, vencer) hace **`SELECT ... FROM inventario ... FOR UPDATE`** sobre la fila de la oferta **antes** de mirar las retenciones. Dos clientes que piden el último cupo quedan en fila: el segundo espera, y cuando entra ve `libres = 0` y recibe `409 RETENIDA`.
- **Orden de bloqueo:** si una operación toca varias ofertas (un pack), las bloquea en **orden ascendente de `oferta_id`**. Si dos packs comparten ofertas, las piden en el mismo orden y no se bloquean mutuamente (sin deadlock).
- **Primero el inventario, después las retenciones, siempre.** Pregunta típica: "¿por qué confirmar bloquea el inventario si no cambia cuántos cupos hay ocupados?". Respuesta: sin ese bloqueo, una retención que vence justo durante su propia confirmación podría ser tomada por otro cliente, y el mismo cupo quedaría vendido y retenido a la vez.
- **`READ COMMITTED`:** con el bloqueo tomado, cada lectura ve lo último confirmado y no una foto vieja de la transacción.
- **El reloj es el de la base (`NOW()`)**, no el de cada instancia: con dos instancias de Catálogo, las dos usan la misma hora.
- Segunda barrera: `CHECK (vendidos <= cupo_total)` en la tabla.

## 4. Por qué no se duplica ni se pierde nada (idempotencia)

Reservas reintenta si Catálogo tarda (timeout de 2 s, 2 reintentos). Que reintentar sea seguro depende de:

| Operación | Cómo es idempotente |
|---|---|
| Retener | `Idempotency-Key` con restricción `UNIQUE` en `grupos_retencion`. Un reintento devuelve el grupo ya creado. Si dos llegan a la vez, una inserta y la otra choca con la clave única y lee la de la primera |
| Liberar | Siempre `204`: liberar algo ya liberado, vencido o inexistente no hace nada |
| Confirmar | La respuesta se guarda en `confirmaciones` con `compra_id` como clave. Un reintento devuelve la misma respuesta y no vuelve a sumar vendidos |

Los eventos (`DisponibilidadCambiada`, `RetencionVencida`) se escriben en la tabla `outbox` **en la misma transacción** que el cambio: o se guardan los dos o ninguno. Un publicador los manda a RabbitMQ después.

## 5. El barrido de vencidas y `SKIP LOCKED`

Cada 15 s, cada instancia busca ofertas con retenciones vencidas y, por cada una:

```sql
SELECT ... FROM inventario WHERE oferta_id = ? FOR UPDATE SKIP LOCKED
```

`SKIP LOCKED` significa "si la fila está bloqueada, saltéala en vez de esperar". Así, si las dos instancias barren a la vez, no procesan la misma oferta, y el barrido **nunca hace esperar a un cliente** que está reteniendo o comprando: esa oferta queda para el próximo barrido.

Pregunta típica: "¿y si el barrido se cae?". Nada grave: la validez depende de `expira_en > NOW()` en cada consulta, no del barrido. Una retención vencida deja de contar y no se puede confirmar aunque siga marcada `ACTIVA`. El barrido solo cambia la etiqueta a `VENCIDA` y avisa a Reservas.

## 6. La evidencia (lo que hay que mostrar)

- **Test de concurrencia** (`TestConcurrenciaUltimoCupoGanaUnoSolo`): 20 goroutines intentan retener el último cupo al mismo tiempo; gana exactamente una y las otras 19 reciben `RETENIDA`. Contra MySQL 8.4 real.
- `TestConcurrenciaSinSobreventa`: 40 clientes contra 10 cupos, se retienen exactamente 10.
- Reintentos en paralelo de retener y de confirmar: mismo resultado, sin duplicar vendidos.
- Confirmar con una retención vencida: no se confirma ninguna (todo o nada) y su cupo queda libre.
- Para correrlos: `cd services/catalogo && go test ./internal/repository/ -run Concurrencia -v` (necesita Docker).
- En vivo: [ensayo del 2026-11-09](../ensayos/2026-11-09-catalogo.md), con el vencimiento y el flujo completo.

## 7. Limitaciones que conviene admitir

- Todas las operaciones sobre una misma oferta se serializan en su fila de inventario: una oferta muy pedida es un punto de contención. Las transacciones son cortas; se mide en la prueba de carga (D13).
- Un cliente puede tener cupo apartado 15 minutos sin comprar; se acota con el máximo de 6 por oferta y por carrito (RN-12).
- El alta de una oferta escribe en MySQL y en MongoDB sin transacción común. Se escribe primero el inventario: si falla MongoDB, queda un inventario sin oferta, que nadie puede retener porque retener lee la oferta de MongoDB. Es basura inofensiva, no una inconsistencia visible.
- Con dos instancias, un evento puede publicarse dos veces; los consumidores son idempotentes.
