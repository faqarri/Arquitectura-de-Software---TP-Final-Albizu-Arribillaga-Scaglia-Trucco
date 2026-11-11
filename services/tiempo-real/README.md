# Servicio Tiempo Real

**Responsabilidad:** enviar a los navegadores, por **Server-Sent Events (SSE)**, los cambios de disponibilidad de las ofertas y el vencimiento de las retenciones del usuario. Es el desafío bonus "actualizaciones de información en tiempo real" ([ADR-015](../../docs/adr/ADR-015-disponibilidad-en-tiempo-real.md)).

**Datos propios:** ninguno. Es **sin estado**: solo mantiene en memoria las conexiones abiertas y a qué ofertas o usuario está suscripta cada una.

**Expone (vía gateway):**

| Ruta | Autenticación | Evento SSE | Contenido |
|---|---|---|---|
| `GET /api/tiempo-real/ofertas?ids=a,b,c` | Pública (máximo 50 ids) | `disponibilidad` | `{ ofertaId, estado, libres, proximaLiberacion, version }` |
| `GET /api/tiempo-real/mis-retenciones` | JWT (el gateway valida el token y propaga el id de usuario) | `retencion-vencida` | `{ retencionId, ofertaId, expiraEn }` |

Envía un *heartbeat* (comentario SSE) cada 20 s para que no se corte la conexión por inactividad.

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| RabbitMQ | Mensajería | Consume `DisponibilidadCambiada` y `RetencionVencida` del exchange fanout `catalogo.disponibilidad` con una **cola exclusiva por instancia** (se borra al desconectarse). **Sin DLQ**: los eventos son avisos; un mensaje mal formado se descarta y se registra en logs y métricas |

**Por qué escala sin coordinación:** cada instancia recibe **todos** los eventos (fanout) y los reenvía a las conexiones que tiene abiertas. No importa a qué instancia quedó conectado cada navegador.

**Garantías:** mejor esfuerzo. El cliente descarta eventos con `version` menor o igual a la última conocida y, al (re)conectar, consulta `GET /api/ofertas/{id}/disponibilidad` a Catálogo.

**Estructura (Go):**

```
cmd/tiempo-real/main.go
internal/sse/         # rutas SSE, latido, validación (hasta 50 ofertas; mis-retenciones exige X-Usuario-Id del gateway)
internal/hub/         # suscripciones en memoria: oferta → conexiones, usuario → conexiones; buffer de 16 por conexión
internal/consumidor/  # consumidor RabbitMQ (cola exclusiva sobre el fanout) y traducción de los eventos
```

**Configuración:** `PUERTO_HTTP` (`8080`), `RABBITMQ_URL` (obligatoria), `OTEL_EXPORTER_OTLP_ENDPOINT` (opcional). En Compose el gateway lo encuentra en `TIEMPO_REAL_URL` (por defecto `http://tiempo-real:8080`).

**Operativa:** `GET /health` y `GET /metrics`, con `tiempo_real_conexiones`, `tiempo_real_eventos_total{tipo,resultado}` y `tiempo_real_eventos_descartados_total`.

**Probar a mano** (con el entorno levantado):

```bash
curl -N "http://localhost:8080/api/tiempo-real/ofertas?ids=<id de una oferta>"
# en otra terminal, agregar esa oferta al carrito: llega "event: disponibilidad"
```

**Tests:** `go test -short ./...` corre los unitarios (hub, traducción de eventos y streams SSE con `httptest`). `go test ./...` agrega el de integración contra **RabbitMQ real** (testcontainers): dos instancias consumen el fanout `catalogo.disponibilidad` y las dos reciben cada evento tal como lo publica Catálogo, y un mensaje mal formado no detiene el consumidor. Necesita Docker.

**Historial:** 2026-10-22: servicio nuevo, incorporado en la devolución de la Entrega 1. 2026-11-10: implementación (ver Revisiones de [ADR-015](../../docs/adr/ADR-015-disponibilidad-en-tiempo-real.md)).
