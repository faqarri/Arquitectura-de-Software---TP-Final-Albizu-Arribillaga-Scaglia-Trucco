# ADR-003: Persistencia

- **Estado:** Propuesta (versión inicial, a validar en Entrega 2)
- **Fecha:** 2026-10-09
- **Decisión de la cátedra:** D3

## Contexto

Cada servicio debe elegir el almacenamiento que mejor responda a sus patrones de acceso. El sistema debe usar al menos un almacenamiento relacional y uno no relacional.

## Decisión

| Servicio | Almacenamiento | Patrones de acceso | Justificación |
|---|---|---|---|
| Reservas | **MySQL** (relacional) | Escrituras concurrentes sobre cupos; lectura de compras por usuario | Transacciones ACID, actualizaciones condicionadas y restricciones (`CHECK cupo >= 0`) para RN-01 y RN-02 |
| Catálogo | **MongoDB** (documental) | Lectura de la oferta completa por id; esquemas distintos por tipo de oferta | Cada tipo (vuelo, tren, colectivo, estadía, excursión) tiene atributos propios; se lee como documento entero |
| Catálogo (búsqueda) | **OpenSearch** (motor de búsqueda) | Texto, filtros combinados, orden y paginación | Proyección derivada de MongoDB, reconstruible |
| Comprobantes | **MySQL** (esquema propio) | Inserción única por clave de idempotencia; consulta por id | Restricción `UNIQUE` sobre la clave de idempotencia garantiza RN-08 |
| Usuarios | **MySQL** (esquema propio) | Búsqueda por email; baja escritura | Simple y consistente. Contraseñas con bcrypt |

En el entorno local los esquemas MySQL pueden compartir una instancia, pero **cada servicio usa su propio esquema y credenciales**; ningún servicio accede al esquema de otro.

## Alternativas consideradas

- **PostgreSQL en lugar de MySQL.** Equivalente para estas necesidades. Se elige MySQL por experiencia previa del equipo.
- **Todo en MySQL.** El catálogo requeriría tablas por tipo o columnas JSON; no se cumpliría el requisito de almacenamiento no relacional con una necesidad real.
- **Búsqueda directa sobre MongoDB.** Viable para filtros simples, pero la búsqueda de texto y la combinación de filtros con relevancia es la especialidad de un motor de búsqueda.

## Consecuencias

- (+) Cada servicio usa el modelo adecuado a su acceso.
- (−) El índice de búsqueda es eventualmente consistente respecto de MongoDB; el retraso aceptable se define en D6.
- (−) Más componentes de infraestructura que operar y observar.
