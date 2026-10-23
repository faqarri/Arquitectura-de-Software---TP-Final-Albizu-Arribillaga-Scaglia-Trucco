# ADR-007: Caché

- **Estado:** Aceptada
- **Fecha:** 2026-10-23
- **Decisión de la cátedra:** D7

## Contexto

El enunciado pide caché en al menos un flujo de lectura relevante, elegido por una necesidad observable y con métricas que muestren su impacto antes y después.

Flujos de lectura candidatos:

| Flujo | Lecturas / escrituras | Restricción |
|---|---|---|
| **Detalle de oferta** (`GET /api/ofertas/{id}`) | Se lee cada vez que un cliente abre una oferta desde la búsqueda, desde un pack o al volver al carrito. Solo lo escribe el administrador, pocas veces por día | Ninguna regla de negocio depende de él: el precio que se cobra se toma de MongoDB al confirmar (RN-06) |
| Resultados de búsqueda | Muy leídos, pero con combinaciones de filtros casi infinitas | Un cambio debe aparecer en la búsqueda en 5 s o menos ([ADR-006](ADR-006-busqueda.md)) |
| Disponibilidad | La más leída y la que más cambia | Es la fuente de verdad de las retenciones y cambia por el paso del tiempo ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)) |

**Necesidad observable (hipótesis a verificar con métricas):** la demanda de un viaje se concentra en pocas ofertas (destinos y fechas populares), así que el detalle de esas ofertas se lee muchas veces por cada escritura. Hoy cada lectura va a MongoDB. Con varias instancias de Catálogo (D12) y picos de tráfico, esas lecturas repetidas compiten con las escrituras del administrador y con el indexador. Si la medición de "antes" muestra que el detalle no es un cuello de botella, se documenta así en los resultados; no se fuerza la conclusión.

## Decisión

### Qué se cachea

**Solo el detalle de oferta**: la descripción, los atributos propios del tipo y el precio de referencia, tal como están en MongoDB. **La disponibilidad no se cachea nunca**: la respuesta de `GET /api/ofertas/{id}/disponibilidad` siempre se calcula en MySQL.

| Aspecto | Decisión |
|---|---|
| Tecnología | **Redis 7**, un contenedor compartido por todas las instancias de Catálogo |
| Patrón | *Cache-aside*: Catálogo busca en Redis; si no está, lee MongoDB y guarda el resultado |
| Clave | `oferta:v1:{id}` (el `v1` permite invalidar todo cambiando el formato) |
| Valor | JSON del detalle, incluida la `version` del documento |
| Vigencia (TTL) | 10 minutos, más un desvío aleatorio de hasta 60 s para que no venzan muchas claves juntas |
| Ofertas inexistentes | No se cachean (evita que un id inválido ocupe memoria) |
| Memoria | `maxmemory 128mb` con política `allkeys-lru`: si se llena, se descartan las menos usadas |

### Invalidación

| Mecanismo | Cuándo | Para qué |
|---|---|---|
| **Borrado al escribir** | Catálogo borra `oferta:v1:{id}` inmediatamente después de modificar la oferta en MongoDB, en la misma solicitud | Que el administrador vea su cambio al instante |
| **Borrado por evento** | El indexador, al procesar `OfertaActualizada` ([ADR-006](ADR-006-busqueda.md)), vuelve a borrar la clave | Cubre el caso en que el primer borrado falló (Redis caído un instante) y la carrera descrita abajo |
| **TTL** | A los 10 minutos como máximo | Última barrera: ningún dato queda desactualizado indefinidamente |

**Carrera conocida:** una lectura empieza, lee la versión vieja de MongoDB, el administrador modifica y borra la clave, y la lectura termina guardando la versión vieja. El borrado por evento (alrededor de 1 s después, ADR-006) la corrige. En el peor caso, si también fallara ese borrado, el TTL la limita a 10 minutos. Se acepta porque el detalle es informativo: **el precio que se cobra sale de MongoDB al confirmar (RN-06), nunca de la caché**, y la validación de packs al retener tampoco usa la caché.

### Si Redis falla

La caché es **opcional**: Catálogo funciona sin ella.

- Cada operación sobre Redis tiene un timeout de 50 ms. Si falla o tarda, se lee MongoDB directamente y se registra la métrica `cache_errores_total`.
- Después de 5 errores seguidos, Catálogo deja de consultar Redis durante 30 s (corte simple) para no sumar 50 ms a cada solicitud mientras Redis está caído.
- Un borrado fallido al escribir no hace fallar la modificación: el borrado por evento y el TTL lo cubren.
- `/health` de Catálogo **no** depende de Redis: una caída de la caché no saca instancias del balanceo.

### Cómo se mide el impacto

**Métricas** (Prometheus, tablero de D11):

- `cache_consultas_total{resultado="acierto|fallo"}` → tasa de aciertos.
- `cache_errores_total`.
- Latencia de `GET /api/ofertas/{id}` en histograma, con etiqueta `origen="cache|mongo"`.
- Operaciones por segundo en MongoDB (exportador de MongoDB).

**Prueba con k6, antes y después:** el mismo escenario se corre dos veces sobre el mismo entorno, con `CACHE_HABILITADA=false` y con `CACHE_HABILITADA=true`:

- 1000 ofertas cargadas; los ids se eligen con distribución sesgada (el 20 % de las ofertas recibe el 80 % de las lecturas) para reproducir la concentración de la demanda.
- 50 usuarios virtuales durante 3 minutos, después de 30 s de precalentamiento.
- Se comparan: latencia p50, p95 y p99, solicitudes por segundo, tasa de errores, tasa de aciertos y carga sobre MongoDB.

**Objetivos (no medidos):** tasa de aciertos ≥ 80 % y reducción de la latencia p95 del detalle de al menos 30 % respecto de la medición sin caché. Los resultados se registran en [ADR-013](ADR-013-capacidad-y-costos.md).

## Alternativas consideradas

- **Cachear resultados de búsqueda.** Las combinaciones de filtros hacen que cada clave se repita poco (tasa de aciertos baja), y no hay forma simple de saber qué resultados afecta un cambio de oferta. Para respetar el retraso máximo de 5 s de ADR-006, el TTL debería ser de pocos segundos. Además, OpenSearch ya cachea los filtros. Descartada.
- **Cachear la disponibilidad.** Es la fuente de verdad de las retenciones y cambia por el paso del tiempo: una caché mostraría `RETENIDA` cuando ya hay cupo, o `DISPONIBLE` cuando ya no. Descartada (ADR-014, ADR-015).
- **Caché en memoria de cada instancia.** No agrega infraestructura, pero con dos o más instancias de Catálogo (D12) cada una tiene su propia copia: invalidar exige avisar a todas y la tasa de aciertos se divide entre instancias. Descartada.
- **Cache HTTP (`Cache-Control`) en el gateway o el navegador.** No se puede invalidar desde el servidor: un cambio de precio de referencia seguiría visible en el navegador hasta que venza. Descartada como mecanismo principal.

## Consecuencias

- (+) Menos lecturas sobre MongoDB para las ofertas más consultadas; el efecto se medirá en D13.
- (+) Ninguna regla de negocio depende de la caché: precio al confirmar y disponibilidad siempre salen de la fuente de verdad.
- (+) Si Redis cae, el sistema sigue funcionando, solo más lento.
- (+) Mientras MongoDB está caído, los detalles que ya estaban en caché siguen respondiendo ([ADR-010](ADR-010-resiliencia.md)).
- (−) Un componente más que operar: Redis debe agregarse a `docker-compose.yml`.
- (−) El detalle puede mostrar un dato viejo por la carrera descrita, en general por alrededor de un segundo y como máximo 10 minutos.
- (−) Si la hipótesis de concentración de la demanda no se cumple, la tasa de aciertos será baja; la medición lo va a mostrar.
