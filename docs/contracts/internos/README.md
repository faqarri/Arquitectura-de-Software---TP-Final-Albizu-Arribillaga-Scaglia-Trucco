# Contratos internos

Interfaces entre nuestros propios servicios. No se publican a otros grupos ni pasan por el gateway. Su objetivo es que cada integrante pueda trabajar contra un mock mientras el otro servicio no existe, y que los tests de cada lado detecten si alguien se aparta del acuerdo.

## API de retenciones de Catálogo (Reservas → Catálogo)

- **Contrato:** [catalogo-retenciones.yaml](catalogo-retenciones.yaml), versión **1.0.0, confirmada**. Lo redactó Reservas siguiendo [ADR-014](../../adr/ADR-014-cupos-en-catalogo-con-retencion.md) y [ADR-005](../../adr/ADR-005-comunicacion-entre-servicios.md) y lo confirmó Catálogo al implementarlo. Si cambia, se actualiza el YAML y se sube la versión.
- **Implementación:** `services/catalogo` (instancias `catalogo-1` y `catalogo-2`, alias de red `catalogo`). Reservas la usa por defecto (`CATALOGO_URL=http://catalogo:8080`).
- **Mock:** `catalogo-mock` en el docker-compose (`http://localhost:4020`), para probar Reservas sin Catálogo (`CATALOGO_URL=http://catalogo-mock:4010`).
- **Verificación:** los tests de `services/reservas/internal/adapters/catalogo` validan contra este YAML cada solicitud que arma el cliente de Reservas. Del lado de Catálogo, `services/catalogo/internal/handlers/contrato_integracion_test.go` valida contra este YAML cada respuesta de la API interna (201, 204, 200, 404, 409 y 422), con el router real y MySQL y MongoDB reales: si una respuesta se aparta del contrato, el test falla.
- **Cambios de la 1.0.0 respecto del borrador 0.1.0** (compatibles: Reservas no necesita cambios):
  - Se corrigió el ejemplo de la confirmación: 2 × 185000,50 = 370001,00 (decía 371001,00).
  - Los `409` de `AGOTADA` y `RETENIDA` informan también `libres`, opcional.
- **Agregados respecto de ADR-014:**
  - `descripcion`, `destino` y `precioReferencia` en cada retención: el carrito los muestra sin consultar a Catálogo.
  - `destino` en cada línea de la confirmación: lo usa la regla de las guías de viaje (HU-13).
  - `code` en los errores.

## Eventos de Catálogo que consume Reservas

Catálogo los publica en el exchange **fanout** `catalogo.disponibilidad`, durable ([ADR-015](../../adr/ADR-015-disponibilidad-en-tiempo-real.md)). Reservas lo declara también, con los mismos argumentos, y enlaza su cola durable `reservas.retenciones` (con reintentos y DLQ).

Cada mensaje lleva el tipo en la propiedad AMQP `type`, que `pkg/mensajeria` completa con el tipo del outbox; si no viene, se lee un campo `tipo` del cuerpo. Reservas ignora todo lo que no sea `RetencionVencida`.

**`RetencionVencida`** (persistente, `content-type: application/json`, header `X-Correlation-Id` opcional):

```json
{
  "eventId": "uuid",
  "retencionId": "3a9b8c7d-6e5f-4a3b-2c1d-0e9f8a7b6c5d",
  "ofertaId": "of-vuelo-bariloche-0712",
  "titularId": "5d2c9a1e-7b3f-4e8a-9c0d-1e2f3a4b5c6d",
  "expiraEn": "2026-11-09T15:45:00Z"
}
```

Reservas usa solo `retencionId`; los demás campos son para Tiempo Real. Marcar el ítem vencido es idempotente: un evento repetido, o de un ítem que ya no está, no hace nada.

## Evento que publica Reservas

**`CompraConfirmada`** → cola `comprobantes.compras`. El formato lo fijó Comprobantes: [README de Comprobantes, sección Mensajes](../../../services/comprobantes/README.md#mensajes). Reservas declara esa cola con los mismos argumentos que Comprobantes, así un evento no se pierde si Reservas arranca primero.
