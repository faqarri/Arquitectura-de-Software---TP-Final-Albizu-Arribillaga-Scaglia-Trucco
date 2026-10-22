# Servicio Comprobantes

**Responsabilidad:** emisión de facturas en PDF y envío **siempre** por correo al email indicado. Implementa la **capacidad publicada** (contrato 1.0.0 en `docs/contracts/comprobantes/v1/`).

**Datos propios (MySQL):** comprobantes (email, ítems, total), claves de idempotencia, estado de envío (`PENDIENTE`, `ENVIADO`, `FALLIDO`).

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MySQL | Almacenamiento | Comprobantes con clave única de idempotencia |
| RabbitMQ | Mensajería | Consume `CompraConfirmada`; DLQ para mensajes fallidos. Cola interna de envíos de correo con reintentos |
| SMTP (Mailpit en local) | Externa | Envío de correos con PDF adjunto |

**Entradas:**

| Entrada | Datos | Idempotencia |
|---|---|---|
| Evento `CompraConfirmada` (uso interno) | `compraId`, `email`, `items` (`descripcion`, `precio`) | Clave = `compraId` |
| `POST /v1/comprobantes` (uso externo, `X-API-Key`) | `email`, `items` (`descripcion`, `precio`) | Header `Idempotency-Key`; misma clave y cuerpo → `200` sin reenviar correo; cuerpo distinto → `409` |
| `GET /v1/comprobantes/{id}` (uso externo) | — | Consulta el estado de envío |

Las dos entradas usan el mismo caso de uso: emitir y encolar el envío. El correo se envía de forma asíncrona y el estado pasa de `PENDIENTE` a `ENVIADO` o, tras agotar los reintentos, a `FALLIDO`.

**Estructura tentativa (Go, capas):**

```
cmd/comprobantes/main.go
internal/handlers/   # API pública v1
internal/consumer/   # consumidor idempotente de CompraConfirmada
internal/services/   # emisión, numeración, generación de PDF, cálculo de total (422 si es 0)
internal/repository/ # MySQL
internal/mailer/     # SMTP y reintentos de envío
```

**Historial:** 2026-10-22: el contrato pasa a 1.0.0 (solo `email` e `items`, envío siempre por correo, sin descarga de PDF por API) ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).
