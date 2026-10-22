# ADR-003: Persistencia

- **Estado:** Propuesta (versión inicial, a validar en Entrega 2; revisada el 2026-10-22, ver [Revisiones](#revisiones))
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D3

## Contexto

Cada servicio debe elegir el almacenamiento que mejor responda a sus patrones de acceso. El sistema debe usar al menos un almacenamiento relacional y uno no relacional.

## Decisión

| Servicio | Almacenamiento | Patrones de acceso | Justificación |
|---|---|---|---|
| Catálogo (descripciones) | **MongoDB** (documental) | Lectura de la oferta completa por id; esquemas distintos por tipo de oferta | Cada tipo (vuelo, tren, colectivo, estadía, excursión) tiene atributos propios; se lee como documento entero |
| Catálogo (cupos) | **MySQL** (esquema `catalogo`) | Escrituras concurrentes sobre la misma oferta (retener, liberar, confirmar); suma de retenciones activas; barrido de vencidas | Transacciones ACID con `SELECT ... FOR UPDATE`, restricciones (`CHECK vendidos <= cupo_total`, `UNIQUE idempotency_key`) y outbox en la misma transacción. Garantiza RN-01 y RN-02 (ver [ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)) |
| Catálogo (búsqueda) | **OpenSearch** (motor de búsqueda) | Texto, filtros combinados, orden y paginación | Proyección derivada de MongoDB, reconstruible |
| Reservas | **MySQL** (esquema `reservas`) | Carrito por usuario; compras por usuario; compras `PENDIENTE` para recuperación; outbox | Transacción local para cambiar el estado de la compra y registrar `CompraConfirmada` en el outbox |
| Comprobantes | **MySQL** (esquema propio) | Inserción única por clave de idempotencia; consulta por id; estado de envío | Restricción `UNIQUE` sobre la clave de idempotencia garantiza RN-08 (un comprobante y un correo por compra) |
| Usuarios | **MySQL** (esquema propio) | Búsqueda por email; baja escritura | Simple y consistente. Contraseñas con bcrypt |
| Tiempo Real | — | — | Sin estado: solo mantiene conexiones SSE en memoria |

En el entorno local los esquemas MySQL pueden compartir una instancia, pero **cada servicio usa su propio esquema y credenciales**; ningún servicio accede al esquema de otro.

## Alternativas consideradas

- **PostgreSQL en lugar de MySQL.** Equivalente para estas necesidades. Se elige MySQL por experiencia previa del equipo.
- **Todo en MySQL.** El catálogo requeriría tablas por tipo o columnas JSON; no se cumpliría el requisito de almacenamiento no relacional con una necesidad real.
- **Búsqueda directa sobre MongoDB.** Viable para filtros simples, pero la búsqueda de texto y la combinación de filtros con relevancia es la especialidad de un motor de búsqueda.
- **Cupos de Catálogo en MongoDB** (con transacciones multi-documento). Posible, pero la suma de retenciones activas, el bloqueo de filas y las restricciones `CHECK`/`UNIQUE` se expresan de forma más directa y verificable en un motor relacional, que el equipo ya usa en los demás servicios.

## Consecuencias

- (+) Cada servicio usa el modelo adecuado a su acceso.
- (−) El índice de búsqueda es eventualmente consistente respecto de MongoDB; el retraso aceptable se define en D6.
- (−) Más componentes de infraestructura que operar y observar.
- (−) Catálogo opera dos almacenes (MongoDB y MySQL) sin transacción común; la descripción y el inventario de una oferta se crean en dos escrituras separadas.

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-22 | Catálogo suma **MySQL (esquema `catalogo`)** para inventario, retenciones, confirmaciones y outbox. La fila de Reservas deja de incluir cupos (`CHECK cupo >= 0`) y pasa a describir carrito, compras y outbox. Se agrega Tiempo Real (sin estado), la alternativa "cupos en MongoDB" y la consecuencia de los dos almacenes en Catálogo. | Devolución de la Entrega 1: los cupos pasan a Catálogo con retención temporal ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)) y se agrega el servicio Tiempo Real ([ADR-015](ADR-015-disponibilidad-en-tiempo-real.md)). |
