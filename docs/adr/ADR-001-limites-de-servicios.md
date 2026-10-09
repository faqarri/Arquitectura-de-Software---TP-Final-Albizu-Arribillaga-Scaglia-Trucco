# ADR-001: Límites de los servicios

- **Estado:** Aceptada
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D1

## Contexto

El dominio incluye: descripción de ofertas de viaje y packs, búsqueda, sugerencias, carrito, compra con cupos limitados, emisión de comprobantes con envío por correo y gestión de usuarios. Además, el sistema debe publicar una capacidad para otro grupo.

La operación más crítica es **confirmar una compra**: no puede sobrevenderse un cupo (RN-01) y la compra de un pack debe ser atómica (RN-02).

## Decisión

Se definen cuatro servicios, cada uno dueño exclusivo de sus datos:

| Servicio | Responsabilidad | Propiedad de datos |
|---|---|---|
| Catálogo | Qué se vende: descripción de ofertas y packs, búsqueda, sugerencias | Ofertas, packs, índice de búsqueda |
| Reservas | Cuánto queda y quién compró: carrito, compras, cupos | Carritos, compras, inventario de cupos |
| Comprobantes | Emisión de facturas y notificación por correo | Comprobantes, claves de idempotencia, notificaciones |
| Usuarios | Identidad y autenticación | Usuarios, roles, hashes de contraseña |

Criterios aplicados:

1. **Patrones de acceso distintos.** El catálogo se lee mucho y se escribe poco (solo el administrador); las compras escriben con consistencia fuerte. Separarlos permite escalar y almacenar cada uno según su necesidad.
2. **Frontera transaccional de la operación crítica.** Los cupos viven en Reservas y no en Catálogo. Así, confirmar una compra (incluido un pack con varias ofertas) se resuelve en **una única transacción local**, sin necesidad de una saga entre servicios en el camino crítico.
3. **Capacidad reutilizable aislada.** Comprobantes no conoce el dominio de viajes: recibe ítems y montos. Esto permite usarlo internamente (por evento) y publicarlo a otro grupo (por API) sin exponer el resto del sistema.
4. **Cambian por motivos distintos.** Las reglas de pack y fechas cambian con el negocio; la lógica fiscal y de correo, por otros motivos.

## Relaciones

- Reservas consulta a Catálogo de forma síncrona el precio vigente y la definición del pack al confirmar.
- Catálogo publica `OfertaPublicada`; Reservas crea el registro de cupo correspondiente.
- Reservas publica `CompraConfirmada`; Comprobantes lo consume.
- El gateway valida los tokens emitidos por Usuarios.

## Alternativas consideradas

- **Cupos dentro de Catálogo.** Unificaba "la oferta" en un solo lugar, pero obligaba a coordinar entre Catálogo y Reservas cada compra (saga con compensaciones) y mezclaba una carga de lectura intensa con escrituras concurrentes. Descartada.
- **Un servicio de inventario por tipo de oferta (vuelos, hoteles, excursiones).** Más cercano a sistemas reales, pero convertía la compra de un pack en una transacción distribuida entre tres servicios. Mayor complejidad sin beneficio para el alcance. Descartada.
- **Comprobantes dentro de Reservas.** Menos servicios, pero acoplaba la capacidad publicada al núcleo transaccional: un pico de uso externo afectaría las compras propias. Descartada.

## Consecuencias

- (+) La operación crítica tiene consistencia fuerte con una transacción local.
- (+) La capacidad publicada puede desplegarse y escalarse independientemente.
- (−) La información de una oferta queda repartida: Catálogo tiene la descripción y Reservas el cupo. Mostrar "cupos disponibles" en la búsqueda requiere replicarlo (eventualmente consistente) o consultarlo; se resolverá en D6/D7.
- (−) La creación del cupo de una oferta nueva es asíncrona: una oferta puede estar visible antes de ser comprable durante un breve intervalo.
- (−) Reservas depende de Catálogo al confirmar; su indisponibilidad debe tratarse (D10).
