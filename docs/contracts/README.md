# Contratos publicados

## API de Emisión de Comprobantes

| Versión | Estado | Contrato | URL |
|---|---|---|---|
| 1.0.0 | **Vigente** (mock disponible, implementación en curso) | [comprobantes/v1/openapi.yaml](comprobantes/v1/openapi.yaml) | Producción: _pendiente_ · Mock: `http://localhost:4010/v1` |

Decisión y justificación: [ADR-008](../adr/ADR-008-contrato-propio.md).

### ¿Para qué sirve?

Permite que tu sistema emita un comprobante (factura) por una operación de venta o reserva, lo descargue en PDF y, si querés, se lo envíe por correo a tu cliente. Pensado para integrarse al final de tu flujo de compra.

### Operaciones

| Operación | Método y ruta | Descripción |
|---|---|---|
| Emitir comprobante | `POST /v1/comprobantes` | Crea el comprobante. Requiere `Idempotency-Key` |
| Obtener comprobante | `GET /v1/comprobantes/{id}` | Datos y estado de envío |
| Descargar PDF | `GET /v1/comprobantes/{id}/pdf` | Archivo PDF |

### Autenticación

Todas las solicitudes llevan el header `X-API-Key`. Cada grupo consumidor recibe su clave por un canal privado (nunca se publica en repositorios).

### Idempotencia

`POST /v1/comprobantes` exige el header `Idempotency-Key` (recomendado: un UUID por operación de tu sistema, por ejemplo, derivado de tu id de orden).

| Situación | Respuesta |
|---|---|
| Clave nueva | `201` con el comprobante creado |
| Misma clave y mismo cuerpo | `200` con el comprobante ya emitido (no se duplica) |
| Misma clave y cuerpo distinto | `409` |

Esto permite **reintentar con seguridad** ante timeouts o `503`: nunca se emiten dos comprobantes para la misma clave. Las claves se conservan 7 días.

### Errores

Formato `application/problem+json`.

| Código | Significado | ¿Reintentar? |
|---|---|---|
| 400 | Cuerpo inválido según el esquema | No, corregir la solicitud |
| 401 | API Key ausente o inválida | No |
| 404 | Comprobante inexistente | No |
| 409 | Idempotency-Key reutilizada con otro cuerpo | No, usar clave nueva |
| 422 | Regla incumplida (por ejemplo, total cero) | No |
| 429 | Límite de solicitudes superado | Sí, tras `Retry-After` |
| 503 | No disponible temporalmente | Sí, con la **misma** Idempotency-Key |

### Condiciones de uso

- Límite tentativo: 60 solicitudes por minuto por API Key.
- Timeout recomendado para el consumidor: 5 s.
- El envío por correo es asíncrono: el `POST` responde con `estadoEnvio: PENDIENTE` y luego pasa a `ENVIADO` o `FALLIDO` (consultable con `GET`).

### Ejemplo

```bash
curl -X POST http://localhost:4010/v1/comprobantes \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <tu-api-key>" \
  -H "Idempotency-Key: 3f1c2a9e-8b7d-4e21-9c55-0a1b2c3d4e5f" \
  -d '{
    "referenciaExterna": "ORD-2026-000123",
    "cliente": { "nombre": "Ana Pérez", "email": "ana.perez@example.com" },
    "items": [ { "descripcion": "Entrada general", "cantidad": 2, "precioUnitario": 15000 } ],
    "moneda": "ARS",
    "enviarPorCorreo": true
  }'
```

### Probar contra el mock

```bash
docker compose up -d comprobantes-mock
```

El mock valida las solicitudes contra el contrato y responde con los ejemplos definidos.

### Política de versionado

- Cambios compatibles (campos opcionales, endpoints nuevos): nueva versión menor dentro de `/v1`, registrada en el historial.
- Cambios incompatibles: nueva ruta `/v2` en su propia carpeta. `/v1` se mantiene operativa hasta el final de la evaluación.

### Historial

| Fecha | Versión | Cambio |
|---|---|---|
| 2026-10-09 | 1.0.0 | Versión inicial |
