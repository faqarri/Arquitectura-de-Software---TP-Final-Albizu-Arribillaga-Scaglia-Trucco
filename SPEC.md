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
| Oferta | Transporte (avión, tren, colectivo), Estadía o Excursión. Tiene destino, fechas, precio y cupo. |
| Pack | Promoción abierta para un destino: ofrece un subconjunto de transportes, estadías y excursiones y un descuento. |
| Carrito | Ítems seleccionados por el cliente antes de comprar. |
| Compra | Resultado de confirmar un carrito. Estados: `PENDIENTE`, `CONFIRMADA`, `RECHAZADA`. |
| Comprobante | Factura emitida por una compra confirmada. |

## Reglas de negocio

| ID | Regla |
|---|---|
| RN-01 | El cupo de una oferta nunca puede quedar negativo: no puede venderse más de lo disponible, aun con compras concurrentes. |
| RN-02 | Una compra es **atómica**: se confirman todos sus ítems o ninguno. |
| RN-03 | Un pack exige elegir **al menos una opción de transporte, una de estadía y una de excursión**, todas entre las opciones ofrecidas por ese pack. |
| RN-04 | Las opciones de un pack deben ser coherentes en fechas: la estadía comienza en o después de la llegada del transporte y la excursión ocurre dentro del período de la estadía. |
| RN-05 | El precio de un pack es la suma de las opciones elegidas menos el porcentaje de descuento del pack. |
| RN-06 | El precio se toma del catálogo **al momento de confirmar** la compra, no al agregar al carrito. |
| RN-07 | Agregar al carrito **no retiene cupo**. El cupo se valida y descuenta al confirmar. |
| RN-08 | Cada compra confirmada genera **exactamente un** comprobante y **un** correo, aunque el evento se reciba más de una vez. |
| RN-09 | Si el carrito contiene ofertas de un destino, el sistema sugiere packs y ofertas de ese destino que no estén en el carrito. |
| RN-10 | **[SUPUESTO]** El pago es simulado: la compra pasa a `CONFIRMADA` si hay cupo y las validaciones son correctas. |
| RN-11 | Las contraseñas se almacenan únicamente como hash (bcrypt) y nunca se devuelven en ninguna respuesta. |

## Historias de usuario

### HU-01 Buscar ofertas
Como **cliente** quiero buscar ofertas por destino, fechas, tipo y precio para encontrar opciones de viaje.

- Dado un destino, se listan las ofertas de ese destino paginadas.
- Se puede filtrar por tipo (transporte/estadía/excursión), subtipo de transporte, rango de fechas y rango de precio.
- Se puede ordenar por precio o por fecha.
- Una oferta creada o modificada por el administrador aparece en la búsqueda dentro del retraso máximo definido en D6.

### HU-02 Armar carrito
Como **cliente** quiero agregar ofertas a un carrito para comprarlas juntas.

- Se pueden agregar y quitar ofertas individuales.
- El carrito muestra el precio vigente de referencia.

### HU-03 Armar un pack
Como **cliente** quiero elegir opciones dentro de un pack para obtener el descuento.

- Solo se muestran las opciones ofrecidas por el pack.
- No se puede agregar el pack sin al menos una opción de cada tipo (RN-03).
- Una combinación con fechas incoherentes es rechazada con un mensaje claro (RN-04).

### HU-04 Recibir sugerencias
Como **cliente** quiero ver sugerencias relacionadas con mi carrito para completar mi viaje.

- Con al menos un ítem en el carrito, se muestran packs y ofertas del mismo destino (RN-09).

### HU-05 Comprar
Como **cliente** quiero confirmar mi carrito para asegurar mi viaje.

- Si hay cupo para todos los ítems, la compra queda `CONFIRMADA` y se descuentan los cupos.
- Si falta cupo en algún ítem, la compra queda `RECHAZADA`, no se descuenta ningún cupo y se informa qué ítem no tiene disponibilidad (RN-02).
- Dos clientes que compran el último cupo al mismo tiempo: solo uno obtiene la compra confirmada (RN-01).
- Reintentar la confirmación del mismo carrito no genera una segunda compra.

### HU-06 Recibir comprobante
Como **cliente** quiero recibir por correo el comprobante de mi compra.

- Tras confirmar, llega un correo con la factura adjunta en PDF.
- Si el envío falla, se reintenta; los mensajes que fallan reiteradamente quedan en una cola de mensajes fallidos para su análisis.
- Nunca se envían dos comprobantes por la misma compra (RN-08).

### HU-07 Consultar mis compras
Como **cliente** quiero ver mis compras y su estado.

### HU-08 Gestionar ofertas
Como **administrador** quiero crear, modificar y despublicar ofertas con su cupo.

### HU-09 Gestionar packs
Como **administrador** quiero definir packs indicando destino, opciones ofrecidas de cada tipo y descuento.

### HU-10 Emitir comprobantes para terceros
Como **sistema consumidor externo** quiero emitir comprobantes mediante una API documentada para integrarlo a mi flujo de venta.

- Ver [docs/contracts/README.md](docs/contracts/README.md).

### HU-11 Registrarse e iniciar sesión
Como **cliente** quiero registrarme e iniciar sesión para comprar.

- El login devuelve un token de acceso; nunca devuelve la contraseña (RN-11).

## Fuera de alcance (por ahora)

- Cancelaciones y reembolsos.
- Pago real con pasarela.
- Retención temporal de cupo en el carrito.

## Preguntas abiertas

- ¿El descuento del pack es fijo por pack o depende de la combinación elegida?
- ¿Un pack puede incluir más de una opción del mismo tipo (por ejemplo, dos excursiones)? Se asume que sí.
