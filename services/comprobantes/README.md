# Servicio Comprobantes

**Responsabilidad:** emisión de facturas en PDF y envío por correo. Implementa la **capacidad publicada** (contrato en `docs/contracts/comprobantes/v1/`).

**Datos propios (MySQL):** comprobantes, claves de idempotencia, registro de envíos.

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MySQL | Almacenamiento | Comprobantes con clave única de idempotencia |
| RabbitMQ | Mensajería | Consume `CompraConfirmada`; DLQ para mensajes fallidos |
| SMTP (Mailpit en local) | Externa | Envío de correos con PDF adjunto |

**Entradas:** evento `CompraConfirmada` (uso interno) y API REST `/v1/comprobantes` (uso externo, API Key).

**Estructura tentativa (Go, capas):**

```
cmd/comprobantes/main.go
internal/handlers/   # API pública v1
internal/consumer/   # consumidor idempotente de CompraConfirmada
internal/services/   # emisión, numeración, generación de PDF
internal/repository/ # MySQL
internal/mailer/     # SMTP
```
