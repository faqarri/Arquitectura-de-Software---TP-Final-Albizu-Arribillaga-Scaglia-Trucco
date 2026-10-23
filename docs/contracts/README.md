# Contratos publicados

## API de Emisión de Comprobantes

| Versión | Estado | Contrato | URL |
|---|---|---|---|
| 1.0.2 | **Vigente** (mock disponible, implementación en curso) | [comprobantes/v1/openapi.yaml](comprobantes/v1/openapi.yaml) | Producción: _pendiente_ · Mock: `http://localhost:4010/v1` |

Decisión y justificación: [ADR-008](../adr/ADR-008-contrato-propio.md).

### ¿Para qué sirve?

Le mandás el **email** de tu cliente y la **lista de ítems** que vendiste (descripción y precio). Nosotros generamos la factura en PDF y **siempre** se la enviamos por correo a ese email. Pensado para integrarse al final de tu flujo de compra o reserva: tu sistema no necesita generar PDFs ni operar un servidor de correo.

### Operaciones

| Operación | Método y ruta | Descripción |
|---|---|---|
| Emitir comprobante | `POST /v1/comprobantes` | Genera la factura y encola su envío por correo. Requiere `Idempotency-Key` |
| Obtener comprobante | `GET /v1/comprobantes/{id}` | Datos del comprobante y estado del envío por correo |

### Qué enviar

| Campo | Tipo | Obligatorio | Reglas |
|---|---|---|---|
| `email` | string (email) | Sí | Destinatario de la factura. Máximo 254 caracteres |
| `items` | array | Sí | Entre 1 y 100 ítems |
| `items[].descripcion` | string | Sí | Entre 1 y 200 caracteres |
| `items[].precio` | número | Sí | `>= 0`, en pesos argentinos (ARS). Si vendiste varias unidades, enviá el importe ya multiplicado (por ejemplo, `"Entrada general x2"`, `30000`) |

- No se aceptan otros campos: un campo no definido en el contrato responde `400`.
- El **total** (suma de los precios) debe ser mayor a cero; si no, responde `422`.
- Ningún precio ni el total pueden superar **9999999999.99** (el máximo admitido); si no, responde `422`.
- Todos los importes están en **ARS**; no hay campo de moneda.
- Los precios con **más de 2 decimales se redondean** a 2 (el 5 redondea hacia arriba: `10.005` → `10.01`). El total se calcula sumando los precios ya redondeados. Si después de redondear el total queda en cero, responde `422`.

### Qué se obtiene

| Campo | Descripción |
|---|---|
| `id` | Identificador del comprobante (UUID). Usalo para consultar el estado |
| `numero` | Número correlativo de la factura (por ejemplo, `B-0001-00000042`) |
| `email`, `items` | Lo que enviaste, con los precios redondeados a 2 decimales |
| `total` | Suma de los precios redondeados, en ARS |
| `fechaEmision` | Fecha y hora de emisión (ISO 8601, UTC) |
| `estadoEnvio` | `PENDIENTE` → `ENVIADO` o `FALLIDO` |

### Envío por correo

El envío es **asíncrono**: el `POST` responde enseguida con `estadoEnvio: PENDIENTE` y el correo sale después, con reintentos automáticos.

| Estado | Significado |
|---|---|
| `PENDIENTE` | El correo está encolado o en reintento |
| `ENVIADO` | El servidor de correo aceptó el mensaje |
| `FALLIDO` | Se agotaron los reintentos. El comprobante **sigue emitido**; no hace falta volver a emitirlo |

Para saber cómo terminó, consultá `GET /v1/comprobantes/{id}`.

### Autenticación

Todas las solicitudes llevan el header `X-API-Key`. Cada grupo consumidor recibe su clave por un canal privado (nunca se publica en repositorios).

### Idempotencia

`POST /v1/comprobantes` exige el header `Idempotency-Key` (recomendado: un UUID por operación de tu sistema, por ejemplo, derivado de tu id de orden).

| Situación | Respuesta |
|---|---|
| Clave nueva | `201` con el comprobante creado (`estadoEnvio: PENDIENTE`) |
| Misma clave y mismo cuerpo | `200` con el comprobante ya emitido y su estado de envío actual. **No se reenvía el correo** |
| Misma clave y cuerpo distinto | `409` |

Esto permite **reintentar con seguridad** ante timeouts o `503`: nunca se emiten dos comprobantes ni se envían dos correos para la misma clave. Las claves se conservan 7 días.

### Errores

Formato `application/problem+json` (RFC 9457).

| Código | Significado | ¿Reintentar? |
|---|---|---|
| 400 | Cuerpo inválido según el esquema (incluye campos no definidos) | No, corregir la solicitud |
| 401 | API Key ausente o inválida | No |
| 404 | Comprobante inexistente | No |
| 409 | Idempotency-Key reutilizada con otro cuerpo | No, usar clave nueva |
| 422 | Regla incumplida: el total es cero, o un precio o el total superan 9999999999.99 | No |
| 429 | Límite de solicitudes superado | Sí, tras `Retry-After` |
| 503 | No disponible temporalmente | Sí, con la **misma** Idempotency-Key |

### Condiciones de uso

- Límite tentativo: 60 solicitudes por minuto por API Key.
- Timeout recomendado para el consumidor: 5 s.

### Ejemplo

```bash
curl -X POST http://localhost:4010/v1/comprobantes \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <tu-api-key>" \
  -H "Idempotency-Key: 3f1c2a9e-8b7d-4e21-9c55-0a1b2c3d4e5f" \
  -d '{
    "email": "ana.perez@example.com",
    "items": [
      { "descripcion": "Entrada general x2", "precio": 30000 },
      { "descripcion": "Cargo por servicio", "precio": 1500 }
    ]
  }'
```

Respuesta `201 Created` (header `Location: /v1/comprobantes/6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3`):

```json
{
  "id": "6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3",
  "numero": "B-0001-00000042",
  "email": "ana.perez@example.com",
  "items": [
    { "descripcion": "Entrada general x2", "precio": 30000 },
    { "descripcion": "Cargo por servicio", "precio": 1500 }
  ],
  "total": 31500,
  "fechaEmision": "2026-10-22T15:30:00Z",
  "estadoEnvio": "PENDIENTE"
}
```

Consultar el estado del envío:

```bash
curl http://localhost:4010/v1/comprobantes/6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3 \
  -H "X-API-Key: <tu-api-key>"
```

### Probar contra el mock

```bash
docker compose up -d comprobantes-mock
```

El mock (Prism) valida las solicitudes contra el contrato y responde con los ejemplos definidos. Las rutas incluyen `/v1` en el propio contrato, por lo que el mock y producción responden en la misma ruta.

### Política de versionado

Rige desde la versión 1.0.0.

- Aclaraciones de comportamiento que no cambian el esquema ni rechazan solicitudes que antes se aceptaban: nueva versión de parche (`1.0.x`), registrada en el historial.
- Cambios compatibles (campos opcionales en respuestas, endpoints nuevos): nueva versión menor dentro de `/v1`, registrada en el historial.
- Cambios incompatibles (quitar o renombrar campos, agregar campos obligatorios, cambiar tipos o semántica): nueva ruta `/v2` en su propia carpeta. `/v1` se mantiene operativa hasta el final de la evaluación.

### Historial

| Fecha | Versión | Cambio |
|---|---|---|
| 2026-10-09 | 0.1.0 (borrador) | Primera propuesta presentada en la Entrega 1 (publicada en ese momento con el número 1.0.0). Recibía datos del cliente (nombre, email, documento), ítems con cantidad y precio unitario, moneda (`ARS`/`USD`), `referenciaExterna` y `enviarPorCorreo` opcional, y tenía un endpoint de descarga del PDF. Se conserva en el historial de git (commit de la Entrega 1). |
| 2026-10-22 | 1.0.0 | Simplificación pedida en la devolución de la Entrega 1: el consumidor envía solo `email` e `items` (`descripcion`, `precio` en ARS) y el correo se envía **siempre**. Se quitan datos del cliente, cantidad, moneda, `enviarPorCorreo`, `referenciaExterna`, el estado `NO_SOLICITADO` y el endpoint de descarga de PDF. Las rutas incluyen `/v1` en el contrato. Se reemplazó el borrador **sin abrir `/v2`** porque ningún grupo lo consumía todavía (la cátedra aún no asignó consumidor); por eso no existía compatibilidad que preservar. Ver revisión en [ADR-008](../adr/ADR-008-contrato-propio.md#revisiones). |
| 2026-10-22 | 1.0.1 | Se documenta el redondeo: los precios con más de 2 decimales se redondean a 2 (el 5 hacia arriba), la respuesta devuelve los precios redondeados y el total es su suma. No cambia el esquema ni se rechaza ninguna solicitud que antes fuera válida. |
| 2026-10-23 | 1.0.2 | Se documenta el importe máximo: un precio o un total mayor a 9999999999.99 responde `422`. Esas solicitudes ya fallaban antes (no entran en el almacenamiento), pero con un `503` que invitaba a reintentar sin sentido. No cambia el esquema. |
