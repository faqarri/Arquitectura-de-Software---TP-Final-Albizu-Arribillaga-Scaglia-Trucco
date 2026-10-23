# ADR-003: Persistencia

- **Estado:** Aceptada (validada en la Entrega 2 el 2026-10-23; ver [Revisiones](#revisiones))
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
| Comprobantes | **MySQL** (esquema propio) | Inserción única por clave de idempotencia; consulta por id y consumidor; estado de envío; outbox | Restricción `UNIQUE (consumidor, idempotency_key)` garantiza RN-08 (un comprobante por compra o por clave de cada consumidor); el outbox en la misma transacción garantiza que el envío del correo no se pierda |
| Usuarios | **MySQL** (esquema propio) | Búsqueda por email; baja escritura | Simple y consistente. Contraseñas con bcrypt |
| Tiempo Real | — | — | Sin estado: solo mantiene conexiones SSE en memoria |

En el entorno local los esquemas MySQL pueden compartir una instancia, pero **cada servicio usa su propio esquema y credenciales**; ningún servicio accede al esquema de otro. Cada servicio tiene un usuario de MySQL con permisos solo sobre su esquema, creado al iniciar la base (`infra/mysql/init/02-usuarios.sh`) con contraseñas tomadas del `.env`.

Los importes de dinero se guardan como `DECIMAL(12,2)`, nunca como punto flotante, para que las sumas de precios y descuentos no acumulen errores de redondeo.

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
| 2026-10-22 | Se crean usuarios de MySQL por servicio con permisos solo sobre su esquema. Los importes se guardan como `DECIMAL(12,2)`. En Comprobantes, la clave única pasa a ser `(consumidor, idempotency_key)` y se agrega un outbox. | Revisión interna del diseño. El ADR ya decía "cada servicio con sus credenciales", pero la infraestructura no lo implementaba (todos usaban root). Los cambios de Comprobantes acompañan las revisiones de [ADR-005](ADR-005-comunicacion-entre-servicios.md#revisiones) y [ADR-008](ADR-008-contrato-propio.md#revisiones). |
| 2026-10-23 | **Validación de la Entrega 2. Pasa de Propuesta a Aceptada.** Verificado en ejecución con `docker compose` y el código integrado: (1) MySQL crea los esquemas `catalogo`, `reservas`, `comprobantes` y `usuarios` y un usuario por servicio con `GRANT ALL` solo sobre su esquema; el usuario `comprobantes` recibe `ERROR 1142` al intentar leer otro esquema, y el servicio se conecta con ese usuario, no con root. (2) En `comprobantes`, los importes son `decimal(12,2)` (`comprobantes.total`, `items_comprobante.precio`), existe `UNIQUE KEY ux_consumidor_clave (consumidor, idempotency_key)` y la tabla `outbox` (`payload` y `headers` JSON, índice en `publicado_en`). (3) Los tests de integración del repositorio contra MySQL real (testcontainers) pasan, con 84,8 % de cobertura del adaptador. (4) Una `CompraConfirmada` publicada dos veces genera un solo comprobante (RN-08). **Sin verificar todavía**, porque no tienen código: MongoDB y OpenSearch en Catálogo, y los esquemas de Catálogo, Reservas y Usuarios más allá de su creación. Se revalida cuando se implementen. | El enunciado pide validar D3 en la Entrega 2. Lo verificado no contradice la decisión, por eso se acepta; las partes sin código quedan explícitas. |
