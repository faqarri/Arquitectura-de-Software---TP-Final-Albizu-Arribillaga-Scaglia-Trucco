# API Gateway

Gateway HTTP propio en Go; escucha en el puerto `8080` y es el único servicio de aplicación publicado al host. Comprueba JWT HS256, autoriza las rutas por rol, elimina los headers `X-Usuario-*` que mande el cliente y, en rutas autenticadas, los vuelve a crear a partir de los claims validados. Propaga `X-Correlation-Id` y `traceparent`, aplica rate limiting y publica métricas Prometheus.

## Rutas

Las rutas no se reescriben: el gateway conserva `/api` al enviar cada request al servicio.

| Ruta | Servicio | Acceso |
|---|---|---|
| `POST /api/usuarios`, `POST /api/sesiones` | Usuarios | Público |
| `GET /api/busqueda`, `GET /api/ofertas`, `GET /api/ofertas/{id}`, `GET /api/ofertas/{id}/disponibilidad`, `GET /api/ofertas/{id}/guias`, `GET /api/packs[/{id}]` | Catálogo | Público |
| `POST /api/ofertas`, `PUT/DELETE /api/ofertas/{id}`, `POST /api/packs` | Catálogo | `admin` |
| `/api/carrito`, `/api/compras`, `/api/guias` | Reservas | `cliente` |
| `GET /api/tiempo-real/ofertas` | Tiempo Real | Público, si se configura |
| `GET /api/tiempo-real/mis-retenciones` | Tiempo Real | `cliente`, si se configura |
| `/health`, `/metrics` | Gateway | Público |

`/internal/*` y las rutas que no estén en esta tabla responden `404`. Comprobantes no se enruta por este gateway ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)).

**Límite inicial:** `GATEWAY_RATE_LIMIT_POR_MINUTO` (por defecto 120) por usuario autenticado o IP para rutas públicas, con ráfaga de hasta 20 requests. SSE solo se enruta cuando `TIEMPO_REAL_URL` está configurada; su límite por conexiones abiertas queda pendiente del servicio bonus.

## Ejecución local

```sh
docker compose up -d --build gateway usuarios reservas
curl http://localhost:8080/health
curl -i -X POST http://localhost:8080/api/usuarios \
  -H 'Content-Type: application/json' \
  -d '{"nombre":"Ana Pérez","email":"ana@example.com","password":"una-clave-segura"}'
curl -i -X POST http://localhost:8080/api/sesiones \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"una-clave-segura"}'
```

Configurar `JWT_SECRET` con al menos 32 caracteres (por ejemplo, generar uno con `openssl rand -hex 32`); Usuarios y Gateway usan exactamente el mismo secreto. `USUARIOS_ADMIN_EMAIL` y `USUARIOS_ADMIN_PASSWORD` son opcionales y deben definirse juntas. El límite se configura con `GATEWAY_RATE_LIMIT_POR_MINUTO`. En Compose, las URLs de Usuarios y Reservas usan los nombres internos. `GATEWAY_CATALOGO_INSTANCIAS` recibe las URLs de Catálogo separadas por coma; `TIEMPO_REAL_URL` es opcional.

El Compose no publica los puertos de Usuarios ni Reservas: así no se puede evitar el control de identidad del gateway. Los servicios `catalogo-1` y `catalogo-2` se configuran como upstreams de forma predeterminada en `.env.example`. Si se despliega el Gateway sin esas instancias, las rutas de Catálogo responderán `503`.

## Estado de implementación

Incluye autenticación/autorización, limpieza y propagación de identidad, reverse proxy, límite de solicitudes, health, métricas y logs/trazas. El pool de Catálogo distribuye entre `catalogo-1` y `catalogo-2` con round robin y health checks cada 5 s; retira tras 3 fallas y reincorpora tras 2 éxitos, con métricas por instancia. El dashboard muestra tráfico por upstream, salud, métricas de caché y alertas. Prometheus define `TasaErroresHTTPAlta` (>5 % 5xx por 1 min) e `InstanciaCatalogoFueraDelPool` (fuera por 1 min), visibles desde Grafana. La caída y recuperación de `catalogo-2` se probaron en ejecución local; falta probar la alerta global de 5xx bajo carga. El streaming y la limitación de conexiones SSE quedan para cuando exista Tiempo Real ([ADR-012](../docs/adr/ADR-012-balanceo-de-carga.md)).

**Tests:** `go test ./...` (no necesita Docker). Las dependencias del gateway son los servicios a los que reenvía, por HTTP; no tiene almacenamiento ni mensajería propios. Por eso sus tests de integración levantan **servidores HTTP reales** (`httptest.NewServer`) como instancias de Catálogo y servicios de destino, y prueban sobre la red: round robin entre instancias sanas, reintento de una lectura en otra instancia si la elegida está caída, que una escritura no se reintente, retiro del pool tras 3 fallas y reincorporación tras 2 éxitos con timeout de 1 s, validación del JWT y reemplazo de los headers de identidad, rutas protegidas e internas y límite de solicitudes por usuario.

**Historial:** 2026-10-22: se documentan headers de identidad y se excluyen rutas internas. 2026-11-09: primera implementación del Gateway Go para Usuarios y Reservas; Catálogo se enruta a una URL configurable. 2026-11-10: integración con las dos instancias de Catálogo en Compose.
