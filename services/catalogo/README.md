# Servicio Catálogo

**Responsabilidad:** qué se vende y cuánto queda. Descripción de ofertas (transporte, estadía, excursión) y packs; **inventario de cupos, retenciones temporales y disponibilidad** (`DISPONIBLE`, `RETENIDA`, `AGOTADA`). Valida las reglas de pack (RN-03, RN-04) al retener y calcula los precios con el descuento del pack (RN-05, RN-06) al confirmar. Consulta las guías del destino a Estudiario (HU-14). Ver [ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md).

**Estado (2026-11-10):** implementado y en `docker-compose` con dos instancias, incluida la búsqueda con OpenSearch (`GET /api/busqueda`, indexador y reconstrucción del índice, [ADR-006](../../docs/adr/ADR-006-busqueda.md)).

## Datos propios

| Almacén | Datos |
|---|---|
| MongoDB, base `catalogo` | Colecciones `ofertas` y `packs` (fuente de verdad de las descripciones y los precios). Cada oferta lleva `version` y `pendientePublicar`, el outbox dentro del documento de ADR-006 |
| MySQL, esquema `catalogo` | `inventario(oferta_id, cupo_total, vendidos, version)`, `grupos_retencion(id, idempotency_key UNIQUE, titular_id, pack_id, expira_en)`, `retenciones(id, grupo_id, oferta_id, cantidad, titular_id, estado, expira_en, compra_id, pack_id, descripcion, destino, precio_referencia)`, `confirmaciones(compra_id, respuesta)`, `outbox`, `app_logs` |
| OpenSearch | Índice de búsqueda detrás del alias `ofertas` (`ofertas-inicial` o `ofertas-<fecha>` después de una reconstrucción). Proyección de MongoDB: se puede reconstruir en cualquier momento |
| Redis | `oferta:v1:{id}` (detalle, 10 min + hasta 60 s) y `guias:v1:{destino}` (guías de Estudiario, 5 min). Opcional: el servicio funciona sin Redis |

Estados de retención: `ACTIVA`, `CONFIRMADA`, `LIBERADA`, `VENCIDA`. Una retención solo es válida si `estado = ACTIVA` **y** `expira_en > NOW()` (reloj de la base), aunque el barrido todavía no la haya marcado (RN-11).

**Diferencia con el modelo de ADR-014:** la `Idempotency-Key` de retener vive en `grupos_retencion` y no en cada retención, porque un pack crea varias retenciones con la misma clave. `retenciones` guarda además `descripcion`, `destino` y `precio_referencia`, para devolver la misma respuesta en un reintento.

## Dependencias

| Dependencia | Tipo | Uso | Si falla |
|---|---|---|---|
| MySQL (`catalogo`) | Almacenamiento | Inventario, retenciones y outbox, con transacciones y `SELECT ... FOR UPDATE` | `/health` da 503 (el gateway saca la instancia) y las operaciones responden `503` |
| MongoDB | Almacenamiento | Ofertas y packs | Igual que MySQL. El detalle cacheado sigue saliendo de Redis |
| Redis | Caché | Detalle de ofertas y guías | Se lee MongoDB. Timeout de 50 ms y, después de 5 errores seguidos, 30 s sin consultar Redis. **No** afecta `/health` |
| OpenSearch | Búsqueda | `GET /api/busqueda` e indexación | Solo la búsqueda responde `503 busqueda-no-disponible` (en ~2 s, con `Retry-After`). Los cambios esperan en `catalogo.indexacion` (reintentos a 5, 15 y 30 s, después DLQ). **No** afecta `/health` |
| RabbitMQ | Mensajería | Publica `DisponibilidadCambiada`, `RetencionVencida` y `OfertaActualizada`; consume `OfertaActualizada` para invalidar la caché y para indexar | Los eventos quedan en los outbox y se publican al volver. Reconecta con backoff |
| Estudiario (Grupo 2) | Proveedor externo | Guías del destino, con el cliente compartido `pkg/estudiario` (timeout 2 s, 2 reintentos, circuit breaker) | `GET /api/ofertas/{id}/guias` responde `200 {"disponible": false, "guias": []}`: el detalle nunca falla por el proveedor |

## API

### Pública (vía gateway)

| Ruta | Acceso | Respuesta |
|---|---|---|
| `GET /api/busqueda?q&destino&tipo&subtipoTransporte&desde&hasta&precioMin&precioMax&orden&pagina&tamanio` | Libre | `{items: Oferta[], pagina, tamanio, total}` (ver [Búsqueda](#búsqueda)) |
| `GET /api/ofertas/{id}` | Libre | `Oferta`. Header `X-Cache: HIT` o `MISS`. Con `X-Usuario-Rol: admin` lee siempre de MongoDB, ve las despublicadas e incluye `cupoTotal` y `vendidos` |
| `GET /api/ofertas/{id}/disponibilidad` | Libre | `{ofertaId, estado, libres, proximaLiberacion, version}`. **Nunca se cachea** |
| `GET /api/ofertas/{id}/guias` | Libre | `{disponible, guias: [{id, titulo, descripcion, autor, etiquetas, calificacion, cantidadCalificaciones}]}` |
| `POST /api/ofertas` · `PUT /api/ofertas/{id}` · `DELETE /api/ofertas/{id}` | `admin` | La oferta (`201` / `200`) o `204`. El `DELETE` es una baja lógica |
| `GET /api/packs` · `GET /api/packs/{id}` | Libre | `{packs: Pack[]}` · `Pack` con sus `opciones` publicadas |
| `POST /api/packs` | `admin` | `201` con el pack |

Ejemplo de `Oferta`:

```json
{
  "id": "of-a4cd5357-31e4-4ea8-877d-4fe44cb15756",
  "tipo": "TRANSPORTE",
  "subtipoTransporte": "AVION",
  "titulo": "Vuelo Córdoba - Bariloche",
  "descripcion": "Directo",
  "destino": "Bariloche",
  "origen": "Córdoba",
  "desde": "2026-12-07T11:30:00Z",
  "hasta": "2026-12-07T13:45:00Z",
  "precio": 185000.5,
  "version": 1
}
```

Cuerpo para crear o editar (`OfertaNueva`, igual al del frontend): `{tipo, subtipoTransporte?, titulo, descripcion, destino, origen?, desde, hasta, precio, cupoTotal}`. Las fechas en ISO 8601. Un transporte exige `subtipoTransporte` (`AVION`, `TREN` o `COLECTIVO`) y `origen`.

Errores (`application/problem+json` con `code`):

| Status | `code` | Cuándo |
|---|---|---|
| 400 | `solicitud-invalida` | Datos inválidos, con `errores: [{campo, mensaje}]` |
| 403 | `prohibido` | Una escritura sin `X-Usuario-Rol: admin` |
| 404 | `no-encontrado` | La oferta o el pack no existe (o está despublicado) |
| 422 | `CUPO_MENOR_AL_OCUPADO` | Al editar, el cupo queda por debajo de lo vendido más lo retenido (RN-01) |
| 503 | `servicio-no-disponible` | MySQL o MongoDB no responden |
| 503 | `busqueda-no-disponible` | OpenSearch no responde (solo `GET /api/busqueda`) |

### Interna (no publicada en el gateway)

Contrato: [catalogo-retenciones.yaml](../../docs/contracts/internos/catalogo-retenciones.yaml) (1.0.0). Solo la usa Reservas, por la red interna (`http://catalogo:8080`).

| Operación | Ruta | Respuestas |
|---|---|---|
| Retener | `POST /internal/retenciones` (header `Idempotency-Key`) | `201` grupo con `expiraEn` · `404 OFERTA_INEXISTENTE` · `409 AGOTADA` / `409 RETENIDA` con `proximaLiberacion` · `422 LIMITE_POR_CARRITO` / `PACK_INVALIDO` |
| Liberar | `DELETE /internal/retenciones/{id}` | `204` siempre |
| Confirmar | `POST /internal/retenciones/confirmacion` (`Idempotency-Key` = `compraId`) | `200` líneas con precio · `409 RETENCION_NO_VIGENTE` con `retencionIds` |
| Reconstruir el índice de búsqueda | `POST /internal/busqueda/reconstruccion` (operación manual) | `200 {indice, documentos, recuperados, indicesBorrados, duracionSegundos}` · `409 reconstruccion-en-curso` |

### Operativa

`GET /health` (`{"estado": "ok", "instancia": "catalogo-1"}`, revisa MySQL y MongoDB) y `GET /metrics`. Toda respuesta lleva el header `X-Instancia`, que muestra qué instancia atendió (evidencia del balanceo, ADR-012).

## Búsqueda

[ADR-006](../../docs/adr/ADR-006-busqueda.md). Las ofertas se indexan en OpenSearch y se buscan siempre por el alias `ofertas`.

| Parámetro | Efecto |
|---|---|
| `q` | Texto libre en título, descripción y destino. Tienen que aparecer todas las palabras, en cualquiera de los campos; sin distinguir mayúsculas ni acentos |
| `destino` | Exacto, sin distinguir mayúsculas ni acentos (`cordoba` = `Córdoba`) |
| `tipo`, `subtipoTransporte` | `TRANSPORTE`, `ESTADIA`, `EXCURSION` · `AVION`, `TREN`, `COLECTIVO` |
| `desde`, `hasta` | `AAAA-MM-DD`: la oferta **empieza** en ese rango (`hasta` incluye todo el día) |
| `precioMin`, `precioMax` | Rango de precio |
| `orden` | `fecha`, `-fecha`, `precio`, `-precio`. Sin orden: por relevancia si hay `q`, si no por fecha. Siempre desempata por `id` |
| `pagina`, `tamanio` | Desde 1; 20 por defecto, máximo 50. Hasta 1000 resultados (`pagina × tamanio ≤ 1000`): más allá, `400` que sugiere refinar los filtros |

- Solo se devuelven ofertas publicadas. **No** incluye la disponibilidad: se consulta en el detalle.
- **Indexador:** consume `catalogo.indexacion` en las dos instancias, lee la oferta actual de MongoDB y la indexa con `version_type = external`. Un evento repetido o fuera de orden no pisa una versión más nueva.
- **Al arrancar**, si el alias no existe, una instancia crea `ofertas-inicial` con el alias en la misma operación y lo carga con todo MongoDB. El indexador arranca recién con el índice listo.
- **Retraso medido:** p95 1,1 s entre la escritura y la aparición en la búsqueda (objetivo ≤ 5 s).

## Eventos

| Evento | Exchange (fanout) | Cuerpo | Lo consumen |
|---|---|---|---|
| `DisponibilidadCambiada` | `catalogo.disponibilidad` | `{eventId, tipo, ofertaId, estado, libres, proximaLiberacion, version}` | Tiempo Real (Reservas lo descarta) |
| `RetencionVencida` | `catalogo.disponibilidad` | `{eventId, tipo, retencionId, ofertaId, titularId, expiraEn}` | Reservas (`reservas.retenciones`) y Tiempo Real |
| `OfertaActualizada` | `catalogo.ofertas` | `{eventId, tipo, ofertaId, version}` | Indexador (`catalogo.indexacion`, con DLQ) y Catálogo (`catalogo.cache`, invalida Redis) |

Los dos primeros salen del outbox de MySQL, en la misma transacción que el cambio. `OfertaActualizada` sale del outbox dentro del documento de MongoDB (ADR-006). Con dos instancias, un evento puede publicarse dos veces; los consumidores son idempotentes.

## Procesos en segundo plano

- **Vencimiento de retenciones** (cada `RETENCION_BARRIDO_SEGUNDOS`): por cada oferta con retenciones vencidas, bloquea su fila de inventario con `FOR UPDATE SKIP LOCKED`, marca `VENCIDA` y publica `RetencionVencida` y `DisponibilidadCambiada`. Varias instancias lo corren sin pisarse y nunca hace esperar a un cliente.
- **Publicador del outbox de MySQL** y **publicador de `OfertaActualizada`** (cada 1 s).
- **Invalidación de caché**: consume `OfertaActualizada` y borra `oferta:v1:{id}`.
- **Indexador de búsqueda**: consume `OfertaActualizada` (`catalogo.indexacion`) y actualiza OpenSearch.

**Regla de bloqueo:** retener, liberar, confirmar y vencer bloquean **primero el inventario** (en orden de `oferta_id`) y después las retenciones. Las transacciones son `READ COMMITTED`: con el bloqueo tomado, cada lectura ve lo último confirmado. Si MySQL elige una transacción como víctima de un deadlock, se reintenta hasta 3 veces (detalle en [ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)).

## Métricas propias

| Métrica | Para qué |
|---|---|
| `http_requests_total`, `http_request_duration_seconds` (de `pkg/observabilidad`) | Tráfico, latencia y errores. Prometheus agrega el label `instance` (`catalogo-1:8080` o `catalogo-2:8080`) |
| `cache_consultas_total{tipo, resultado}` | Tasa de aciertos de la caché (`acierto` / `fallo`) |
| `cache_errores_total`, `cache_circuito_abierto` | Fallas de Redis y si la caché está en pausa |
| `catalogo_detalle_oferta_segundos{origen}` | Latencia del detalle desde `cache` o desde `mongo` (la comparación de ADR-007) |
| `busqueda_retraso_indexacion_segundos` | Retraso entre la escritura y la aparición en la búsqueda (objetivo de ADR-006: p95 ≤ 5 s) |
| `busqueda_indexaciones_total{resultado}` | Ofertas indexadas, repetidas (versión ya indexada) o inexistentes |

## Configuración

| Variable | Defecto | Uso |
|---|---|---|
| `INSTANCIA_ID` | hostname | Nombre de la instancia en logs, `/health` y `X-Instancia` |
| `DB_HOST`, `DB_PUERTO`, `DB_USUARIO`, `DB_NOMBRE` | `localhost`, `3306`, `catalogo`, `catalogo` | MySQL |
| `DB_PASSWORD` | — (obligatoria) | `CATALOGO_DB_PASSWORD` del `.env` |
| `MONGO_URL`, `MONGO_BASE` | `mongodb://localhost:27017`, `catalogo` | MongoDB |
| `RABBITMQ_URL` | — (obligatoria) | RabbitMQ |
| `REDIS_URL` | vacío (sin caché) | Por ejemplo `redis://redis:6379/0` |
| `CACHE_HABILITADA` | `true` | `false` para medir sin caché (ADR-007) |
| `RETENCION_MINUTOS`, `RETENCION_MAX_CUPOS_POR_OFERTA`, `RETENCION_BARRIDO_SEGUNDOS` | `15`, `6`, `15` | ADR-014 |
| `ESTUDIARIO_URL`, `ESTUDIARIO_API_KEY` | mock, vacía | Proveedor de guías |
| `OPENSEARCH_URL`, `BUSQUEDA_ALIAS` | `http://localhost:9200`, `ofertas` | Índice de búsqueda |

## Estructura (Go, arquitectura en capas, [ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md))

```
cmd/catalogo/            main.go (arma las dependencias) y config.go
internal/handlers/       HTTP (Gin): API pública, /internal y traducción de errores
internal/services/       lógica de aplicación: ofertas, packs, retenciones, guías
internal/repository/     MySQL (inventario, retenciones, outbox), MongoDB (ofertas, packs), Redis y OpenSearch
internal/models/         entidades y reglas puras (RN-03, RN-04, RN-05, RN-12, RN-13)
internal/events/         publicadores y consumidor de RabbitMQ
internal/jobs/           vencimiento de retenciones y preparación del índice de búsqueda
```

`handlers → services → repository`, con `models` como base común. `events` y `jobs` reciben interfaces y no tocan la base. `arquitectura_test.go` verifica estas reglas en cada `go test`.

## Tests

```bash
cd services/catalogo
go test -short ./...   # unitarios y regla de capas, sin Docker
go test ./...          # además, integración contra MySQL 8.4, MongoDB 7 y OpenSearch 2.15 reales (testcontainers, necesita Docker)
```

- **Unitarios** (`internal/models`): disponibilidad, límite por carrito, validación de ofertas, reglas de pack (RN-03, RN-04) y precios con descuento (RN-05).
- **Integración contra MySQL** (`internal/repository`): **20 clientes contra el último cupo, gana exactamente uno** (los otros 19 reciben `RETENIDA`); 40 clientes contra 10 cupos sin sobreventa; retener y confirmar idempotentes, también con reintentos en paralelo; pack todo o nada; confirmar con una retención vencida no confirma ninguna y libera su cupo; un pack se confirma entero; liberar es idempotente; el barrido marca `VENCIDA` y deja el evento en el outbox; el cupo no baja de lo ocupado.
- **Integración contra MongoDB**: versión y outbox en el documento (desmarcar con una versión vieja no borra la marca), baja lógica y packs.
- **Búsqueda:** unitarios de los filtros y de la consulta que se arma; del manejador del indexador (reintento y DLQ); del handler (errores juntos). **Integración contra OpenSearch**: versiones externas (repetida y vieja no pisan), filtros, destino sin acentos, texto libre en varios campos, despublicadas fuera, paginación y reconstrucción con cambio atómico del alias.
- **Contrato** (`internal/handlers`): cada respuesta de la API interna se valida contra [catalogo-retenciones.yaml](../../docs/contracts/internos/catalogo-retenciones.yaml), incluido que el status esté documentado.

Ensayos en vivo (caída de Redis, vencimiento de una retención y flujo completo): [docs/ensayos/2026-11-09-catalogo.md](../../docs/ensayos/2026-11-09-catalogo.md). Guía de la operación crítica para la defensa: [docs/defensa/operacion-critica.md](../../docs/defensa/operacion-critica.md).

## Probarlo a mano

```bash
docker compose up -d --build
# Crear una oferta (como admin) en la instancia 1
curl -X POST localhost:8083/api/ofertas -H 'X-Usuario-Rol: admin' -H 'Content-Type: application/json' \
  -d '{"tipo":"EXCURSION","titulo":"Cerro Catedral","descripcion":"Día completo","destino":"Bariloche","desde":"2026-12-09T12:00:00Z","hasta":"2026-12-09T21:00:00Z","precio":98000,"cupoTotal":20}'
# Leerla dos veces (la segunda, de la otra instancia, sale de Redis: X-Cache: HIT)
curl -i localhost:8083/api/ofertas/<id>
curl -i localhost:8084/api/ofertas/<id>
# Disponibilidad, y agregarla al carrito por Reservas (llama a /internal/retenciones)
curl localhost:8083/api/ofertas/<id>/disponibilidad
curl -X POST localhost:8082/api/carrito/items -H 'X-Usuario-Id: ana' -H 'Idempotency-Key: item-ana-0001' \
  -H 'Content-Type: application/json' -d '{"ofertaId":"<id>","cantidad":2}'
```

Para ver vencer una retención rápido: `RETENCION_MINUTOS=1` en el `.env` y `docker compose up -d catalogo-1 catalogo-2`.

**Historial:**
- 2026-10-22: incorpora inventario, retenciones y disponibilidad (antes en Reservas), MySQL esquema `catalogo`, API interna y eventos de disponibilidad. Se quitan las sugerencias, el endpoint de precios para Reservas y el evento `OfertaPublicada` ([ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)).
- 2026-11-09: implementación del servicio (ofertas, packs, retenciones, disponibilidad, caché Redis, guías de Estudiario, eventos y barrido), dos instancias en `docker-compose` y tests unitarios y de integración. Contrato interno confirmado en 1.0.0.
