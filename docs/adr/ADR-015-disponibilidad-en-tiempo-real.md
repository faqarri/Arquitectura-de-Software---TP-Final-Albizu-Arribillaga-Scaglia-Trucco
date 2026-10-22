# ADR-015: Disponibilidad en tiempo real (agotado vs. retenido)

- **Estado:** Aceptada
- **Fecha:** 2026-10-22
- **Decisión de la cátedra:** — (decisión adicional; desafío bonus "actualizaciones de información en tiempo real")
- **Relacionada con:** [ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)

## Contexto

Con la retención de cupo (ADR-014), "no hay cupo" puede significar dos cosas muy distintas para el cliente:

- **Agotada:** todo el cupo está vendido; no va a volver a haber.
- **Retenida:** no hay cupo libre, pero parte está retenida en carritos de otros usuarios y puede liberarse en minutos.

En la devolución de la Entrega 1 se pidió distinguir ambos casos. Lo incorporamos al alcance como nuestro desafío bonus "actualizaciones de información en tiempo real": el cliente que mira una oferta retenida se entera **sin recargar** cuando vuelve a estar disponible, y el que tiene ítems en el carrito se entera cuando una retención suya venció.

## Decisión

### Estados de disponibilidad

Los calcula Catálogo (fórmula en ADR-014):

| Estado | Significado para el cliente |
|---|---|
| `DISPONIBLE` | Hay cupos libres (`libres`) |
| `RETENIDA` | No hay libres, pero hay retenciones activas; se informa `proximaLiberacion` (menor `expira_en`) |
| `AGOTADA` | Todo vendido |

- Retener sin cupo responde `409 AGOTADA` o `409 RETENIDA` con `proximaLiberacion`.
- Consulta puntual (fuente de verdad): `GET /api/ofertas/{id}/disponibilidad` → `{ ofertaId, estado, libres, proximaLiberacion, version }`.

### Nuevo servicio Tiempo Real

Servicio **sin estado** (no tiene base de datos) que mantiene conexiones **Server-Sent Events (SSE)** con los navegadores, expuestas a través del gateway:

| Ruta (vía gateway) | Autenticación | Evento SSE | Contenido |
|---|---|---|---|
| `GET /api/tiempo-real/ofertas?ids=a,b,c` | Pública (máximo 50 ids) | `disponibilidad` | `{ ofertaId, estado, libres, proximaLiberacion, version }` |
| `GET /api/tiempo-real/mis-retenciones` | JWT (el gateway valida y propaga el id de usuario) | `retencion-vencida` | `{ retencionId, ofertaId, expiraEn }` |

- Cada 20 s se envía un comentario SSE (*heartbeat*) para que el gateway y los proxies no corten la conexión por inactividad.
- El `EventSource` nativo del navegador no permite enviar el header `Authorization`. El stream público usa `EventSource`. Para `mis-retenciones`, el frontend lee el SSE con `fetch` y streaming de la respuesta, que sí permite el header. Se descarta pasar el JWT en la URL porque quedaría en los logs.

### Eventos y mensajería

- Catálogo publica por **outbox**, dentro de la misma transacción del cambio:
  - `DisponibilidadCambiada`: estado **completo** de la oferta (no un delta) más `version`, que crece en cada cambio de esa oferta (`inventario.version`).
  - `RetencionVencida`: `retencionId`, `ofertaId`, `titularId`, `expiraEn`.
- Ambos eventos van a un **exchange fanout** `catalogo.disponibilidad`:
  - Cada instancia de Tiempo Real declara su propia **cola exclusiva** (se borra cuando la instancia se desconecta). Así **cada instancia recibe todos los eventos** y reenvía cada uno a las conexiones suscriptas que tiene abiertas. Se escala agregando instancias, sin coordinación ni estado compartido: no importa a qué instancia quedó conectado cada navegador.
  - Reservas tiene una cola **durable con DLQ** (`reservas.retenciones`). Consume `RetencionVencida` para marcar el ítem del carrito como vencido y descarta `DisponibilidadCambiada`.

### Los eventos son avisos, no fuente de verdad

- El cliente guarda la última `version` de cada oferta y **descarta los eventos con versión menor o igual**. Esto resuelve los desórdenes y duplicados.
- Al **conectar o reconectar**, el cliente consulta `GET /api/ofertas/{id}/disponibilidad` (y su carrito) para partir de un estado correcto.
- Por eso las colas de Tiempo Real **no tienen DLQ**: si un aviso se pierde, la próxima consulta, el próximo evento o la respuesta `409` al intentar retener corrigen la vista. Las colas de los demás consumidores (Comprobantes, Reservas, indexador) sí tienen DLQ, porque allí perder un mensaje sí tiene consecuencias de negocio.
- La cuenta regresiva del carrito se calcula en el navegador a partir de `expiraEn`. El aviso `retencion-vencida` confirma el vencimiento, pero no es imprescindible.

## Alternativas consideradas

- **WebSockets.** Permiten comunicación bidireccional, que no hace falta: el navegador solo recibe. Requieren *upgrade* de protocolo en el gateway y reconexión manual. SSE es HTTP común, reconecta solo y es más simple de operar. Descartada.
- **Polling periódico** a `/disponibilidad`. Es lo más simple, pero obliga a elegir entre latencia (consultas espaciadas) y carga sobre Catálogo (cada cliente consulta cada pocos segundos aunque nada cambie). Se conserva solo como respaldo al reconectar. Descartada como mecanismo principal.
- **Notificar desde Catálogo directamente.** Evita un servicio, pero mezcla miles de conexiones largas con la carga transaccional de Catálogo. Además, con varias instancias de Catálogo, cada una necesitaría enterarse de los cambios hechos por las otras, es decir, igual haría falta el broker. Descartada.

## Consecuencias

- (+) El cliente distingue "agotado" de "retenido" y sabe cuándo puede liberarse un cupo.
- (+) Tiempo Real escala horizontalmente sin coordinación y no tiene estado que respaldar.
- (+) Catálogo no maneja conexiones largas: publica eventos y sigue.
- (−) **Conexiones largas en el gateway:** la ruta `/api/tiempo-real` necesita timeouts largos, sin buffering de respuesta, y se limita por cantidad de conexiones abiertas en lugar de por solicitudes por minuto.
- (−) Con HTTP/1.1 el navegador permite unas 6 conexiones por dominio. Varias pestañas con dos streams cada una pueden agotarlas; se mitiga sirviendo el gateway con HTTP/2.
- (−) **Mejor esfuerzo:** un aviso puede perderse o llegar tarde, y se corrige al reconectar o al actuar. Ninguna regla de negocio depende de estos eventos.
- (−) Cada instancia de Tiempo Real procesa todos los eventos, aunque no tenga clientes interesados. Es aceptable para el volumen esperado; si creciera, se podría pasar a un exchange `topic` por oferta.
- (−) El vencimiento se notifica con el retraso del proceso de vencimiento de Catálogo (por defecto hasta 15 s) más la latencia del outbox.
