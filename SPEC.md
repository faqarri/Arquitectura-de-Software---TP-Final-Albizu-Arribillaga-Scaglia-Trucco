# Especificación — Agencia de Viajes

Forma de trabajo elegida: **historias de usuario con criterios de aceptación** y un catálogo de reglas de negocio numeradas, referenciadas desde las historias.

> Los ítems marcados como **[SUPUESTO]** son decisiones tomadas para avanzar con el diseño y quedan sujetas a validación con la cátedra.

## Actores

- **Cliente**: busca ofertas, arma su carrito, compra y consulta sus compras.
- **Administrador**: gestiona ofertas, cupos y packs.
- **Sistema consumidor externo**: otro grupo que utiliza la API de Comprobantes.

## Entidades principales

| Entidad | Descripción |
|---|---|
| Oferta | Transporte (avión, tren, colectivo), Estadía o Excursión. Tiene destino, fechas, precio y cupo total. |
| Pack | Promoción abierta para un destino: ofrece un subconjunto de transportes, estadías y excursiones y un descuento. |
| Retención | Reserva temporal de una cantidad de cupos de una oferta para un cliente, creada al agregar al carrito. Vence a los `RETENCION_MINUTOS` (15 minutos). Estados: `ACTIVA`, `CONFIRMADA` (pasó a vendida al comprar), `LIBERADA` (el cliente la quitó del carrito), `VENCIDA`. Un pack genera una retención por cada opción elegida, y todas vencen juntas. |
| Carrito | Ítems seleccionados por el cliente antes de comprar. Cada ítem tiene su retención y su vencimiento (`expiraEn`). |
| Compra | Resultado de confirmar un carrito. Estados: `PENDIENTE`, `CONFIRMADA`, `RECHAZADA`. |
| Comprobante | Factura emitida por una compra confirmada y enviada por correo. |

### Estados de disponibilidad de una oferta

`libres = cupo total − vendidos − retenidos en retenciones activas no vencidas`

| Estado | Condición | Qué ve el cliente |
|---|---|---|
| `DISPONIBLE` | `libres > 0` | Cantidad de cupos libres; puede agregar al carrito |
| `RETENIDA` | `libres = 0`, pero hay retenciones activas de otros clientes | "Sin cupo por ahora: hay lugares reservados en carritos que pueden liberarse" y la hora de la próxima liberación posible (`proximaLiberacion`) |
| `AGOTADA` | `libres = 0` y no hay retenciones activas: todo está vendido | "Agotada" |

## Reglas de negocio

| ID | Regla |
|---|---|
| RN-01 | El cupo de una oferta nunca puede quedar negativo: la suma de cupos vendidos y retenidos activos nunca supera el cupo total, aun con operaciones concurrentes. |
| RN-02 | Una compra es **atómica**: se confirman todos sus ítems o ninguno. |
| RN-03 | Un pack exige elegir **al menos una opción de transporte, una de estadía y una de excursión**, todas entre las opciones ofrecidas por ese pack. |
| RN-04 | Las opciones de un pack deben ser coherentes en fechas: la estadía comienza en o después de la llegada del transporte y la excursión ocurre dentro del período de la estadía. |
| RN-05 | El precio de un pack es la suma de las opciones elegidas menos el porcentaje de descuento del pack. |
| RN-06 | El precio se toma del catálogo **al momento de confirmar** la compra, no al agregar al carrito. |
| RN-07 | Agregar al carrito **retiene cupo** durante `RETENCION_MINUTOS` (15 minutos). El cupo se valida al retener y se descuenta definitivamente al confirmar. Un pack retiene todas sus opciones o ninguna. |
| RN-08 | Cada compra confirmada genera **exactamente un** comprobante y **un** correo, aunque el evento se reciba más de una vez. |
| RN-09 | El pago es simulado: la compra pasa a `CONFIRMADA` si hay cupo y las validaciones son correctas. |
| RN-10 | Las contraseñas se almacenan únicamente como hash (bcrypt) y nunca se devuelven en ninguna respuesta. |
| RN-11 | Una retención vencida no cuenta como cupo retenido y no puede confirmarse, aunque todavía no se haya marcado como `VENCIDA`. Quitar un ítem del carrito libera su retención. Si el cliente abandona el carrito, la retención vence sola. |
| RN-12 | **[SUPUESTO]** Un carrito puede retener como máximo `RETENCION_MAX_CUPOS_POR_OFERTA` (6) cupos de una misma oferta. |
| RN-13 | Cuando no hay cupo libre, el sistema informa si la oferta está `AGOTADA` o `RETENIDA`; en el segundo caso, informa también cuándo puede liberarse un cupo. |

## Historias de usuario

### HU-01 Buscar ofertas
Como **cliente** quiero buscar ofertas por destino, fechas, tipo y precio para encontrar opciones de viaje.

- Dado un destino, se listan las ofertas de ese destino paginadas.
- Se puede filtrar por tipo (transporte/estadía/excursión), subtipo de transporte, rango de fechas y rango de precio.
- Se puede ordenar por precio o por fecha.
- Una oferta creada o modificada por el administrador aparece en la búsqueda dentro del retraso máximo definido en D6.
- El detalle de una oferta muestra su estado de disponibilidad (RN-13).

### HU-02 Armar carrito
Como **cliente** quiero agregar ofertas a un carrito para comprarlas juntas.

- Se pueden agregar y quitar ofertas individuales.
- El carrito muestra el precio vigente de referencia.
- Cada ítem muestra el tiempo que le queda a su retención (cuenta regresiva).

### HU-03 Armar un pack
Como **cliente** quiero elegir opciones dentro de un pack para obtener el descuento.

- Solo se muestran las opciones ofrecidas por el pack.
- No se puede agregar el pack sin al menos una opción de cada tipo (RN-03).
- Una combinación con fechas incoherentes es rechazada con un mensaje claro (RN-04).
- Si alguna opción elegida no tiene cupo, no se retiene ninguna y se informa cuál falló (RN-07).

### HU-04 Asegurar mi lugar mientras compro
Como **cliente** quiero que al agregar una oferta al carrito se me reserve el cupo por un tiempo, para no perderlo mientras termino de armar el viaje.

- Al agregar al carrito, el cupo queda retenido 15 minutos y el carrito muestra hasta cuándo (RN-07).
- Mientras la retención está activa, otros clientes no pueden tomar ese cupo (RN-01).
- Dos clientes que agregan el último cupo al mismo tiempo: solo uno lo retiene; el otro recibe "retenida" (RN-01, RN-13).
- Quitar el ítem del carrito libera el cupo de inmediato (RN-11).
- Cuando la retención vence, el ítem se marca como vencido en el carrito sin que el cliente tenga que recargar la página, y su cupo vuelve a estar disponible para otros (RN-11).
- Un ítem vencido se puede volver a agregar si todavía hay cupo.
- No se pueden retener más de 6 cupos de la misma oferta en un carrito (RN-12).

### HU-05 Saber por qué no hay cupo
Como **cliente** quiero saber si una oferta sin cupo está agotada o solo retenida por otros, para decidir si espero o busco otra opción.

- Una oferta sin cupo libre muestra `AGOTADA` o `RETENIDA` (RN-13).
- Si está `RETENIDA`, se muestra cuándo puede liberarse el próximo cupo.
- Si mientras la miro se libera un cupo (otro cliente lo quitó o su retención venció), la pantalla pasa a `DISPONIBLE` sin recargar.
- Si mientras la miro se vende el último cupo retenido, la pantalla pasa a `AGOTADA` sin recargar.
- Si la conexión en tiempo real se corta, al reconectarse la pantalla muestra el estado actual.

### HU-06 Comprar
Como **cliente** quiero confirmar mi carrito para asegurar mi viaje.

- Si todas las retenciones del carrito están vigentes, la compra queda `CONFIRMADA` y los cupos retenidos pasan a vendidos (RN-02).
- Si alguna retención venció, la compra queda `RECHAZADA`, no se vende ningún cupo y se informa qué ítem venció (RN-02, RN-11). Las demás retenciones siguen vigentes hasta su vencimiento.
- El total se calcula con los precios vigentes al confirmar (RN-05, RN-06).
- Reintentar la confirmación del mismo carrito no genera una segunda compra.
- Si el sistema no puede completar la confirmación en el momento, la compra queda `PENDIENTE` y se resuelve automáticamente; el cliente ve "procesando" y luego el resultado final.

### HU-07 Recibir comprobante
Como **cliente** quiero recibir por correo el comprobante de mi compra.

- Tras confirmar, llega un correo al email de mi cuenta con la factura adjunta en PDF.
- Si el envío falla, se reintenta; los mensajes que fallan reiteradamente quedan en una cola de mensajes fallidos para su análisis.
- Nunca se envían dos comprobantes por la misma compra (RN-08).

### HU-08 Consultar mis compras
Como **cliente** quiero ver mis compras y su estado.

### HU-09 Gestionar ofertas
Como **administrador** quiero crear, modificar y despublicar ofertas con su cupo.

- El cupo total no puede fijarse por debajo de los cupos ya vendidos.

### HU-10 Gestionar packs
Como **administrador** quiero definir packs indicando destino, opciones ofrecidas de cada tipo y descuento.

### HU-11 Emitir comprobantes para terceros
Como **sistema consumidor externo** quiero emitir comprobantes mediante una API documentada para integrarlo a mi flujo de venta.

- Envío el email del destinatario y los ítems (descripción y precio); el comprobante se envía siempre por correo.
- Ver [docs/contracts/README.md](docs/contracts/README.md).

### HU-12 Registrarse e iniciar sesión
Como **cliente** quiero registrarme e iniciar sesión para comprar.

- El login devuelve un token de acceso; nunca devuelve la contraseña (RN-10).

## Fuera de alcance (por ahora)

- Cancelaciones y reembolsos.
- Pago real con pasarela.
- Sugerencias de productos según el carrito (quitadas del alcance en la devolución de la Entrega 1).
- Extender una retención ya creada: si vence, el cliente vuelve a agregar el ítem.

## Preguntas abiertas

- ¿El descuento del pack es fijo por pack o depende de la combinación elegida?
- ¿Un pack puede incluir más de una opción del mismo tipo (por ejemplo, dos excursiones)? Se asume que sí.

## Historial de cambios

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-09 | Versión inicial (Entrega 1). | — |
| 2026-10-22 | **Retención de cupo:** se agrega la entidad Retención, los estados de disponibilidad, RN-11 a RN-13 y las HU-04 y HU-05. RN-07 pasa de "no retiene cupo" a "retiene cupo 15 minutos" (cambio iniciado por el equipo y completado acá). RN-01 incluye los cupos retenidos. HU-06 (Comprar) se reescribe: el rechazo ocurre por retenciones vencidas y se agrega el estado `PENDIENTE`. Se quita la retención de "Fuera de alcance". | Devolución de la Entrega 1: cupos en Catálogo con retención de 15 minutos ([ADR-014](docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)) y distinción agotado/retenido en tiempo real ([ADR-015](docs/adr/ADR-015-disponibilidad-en-tiempo-real.md)). |
| 2026-10-22 | **Sugerencias quitadas del alcance:** se eliminan RN-09 ("el sistema sugiere packs y ofertas del destino") y HU-04 ("Recibir sugerencias"). | Decisión de alcance tomada tras la Entrega 1. |
| 2026-10-22 | **Renumeración de reglas:** RN-10 (pago simulado) → RN-09, sin la marca `[SUPUESTO]` (cambio del equipo); RN-11 (bcrypt) → RN-10. RN-01 a RN-08 conservan su número. | Consecuencia de quitar RN-09. |
| 2026-10-22 | **Renumeración de historias:** HU-01 a HU-03 sin cambios; HU-04 nueva (retención); HU-05 nueva (agotado vs. retenido); HU-05 → HU-06; HU-06 → HU-07; HU-07 → HU-08; HU-08 → HU-09; HU-09 → HU-10; HU-10 → HU-11; HU-11 → HU-12. | Consecuencia de quitar la HU de sugerencias y agregar las de retención. |
| 2026-10-22 | HU-07 indica que el comprobante se envía al email de la cuenta; HU-11 refleja el contrato 1.0.0 (solo email e ítems, envío siempre por correo). | Devolución de la Entrega 1: simplificación del contrato de Comprobantes ([ADR-008](docs/adr/ADR-008-contrato-propio.md)). |
