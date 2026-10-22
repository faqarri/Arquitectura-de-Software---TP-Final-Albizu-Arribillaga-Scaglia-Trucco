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

**Estructura tentativa (Go):**

```
cmd/tiempo-real/main.go
internal/sse/        # handlers SSE, heartbeat, registro de conexiones
internal/hub/        # suscripciones en memoria: oferta → conexiones, usuario → conexiones
internal/consumer/   # consumidor RabbitMQ (cola exclusiva sobre el fanout)
```

**Historial:** 2026-10-22: servicio nuevo, incorporado en la devolución de la Entrega 1.
