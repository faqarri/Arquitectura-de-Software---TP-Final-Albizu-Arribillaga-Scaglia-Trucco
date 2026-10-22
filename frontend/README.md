# Frontend

Aplicación web (React + Vite) que centraliza la interacción de clientes y administradores. Se comunica exclusivamente con el API Gateway.

Pantallas previstas: login/registro, búsqueda de ofertas, detalle de oferta con su disponibilidad, detalle de pack con selección de opciones, carrito con cuenta regresiva de retenciones, confirmación de compra, mis compras, administración de ofertas y packs.

## Disponibilidad y retenciones

| Estado | Qué se muestra |
|---|---|
| `DISPONIBLE` | Cupos libres y botón "Agregar al carrito" |
| `RETENIDA` | "Sin cupo por ahora: hay lugares reservados en carritos que pueden liberarse" y la hora de `proximaLiberacion`; botón deshabilitado |
| `AGOTADA` | "Agotada"; botón deshabilitado |

- **Carrito:** cada ítem muestra una **cuenta regresiva** calculada a partir de `expiraEn`. Al llegar a cero, o al recibir `retencion-vencida`, el ítem se marca como vencido y ofrece "volver a agregar".
- **Al agregar:** un `409 RETENIDA` o `409 AGOTADA` se muestra con el mismo mensaje de la tabla; un `422` muestra el motivo (reglas de pack o límite de 6 cupos).
- **Al comprar:** `201` confirma; `409` indica qué ítems vencieron; `202` muestra "procesando" y consulta el estado de la compra hasta que sea `CONFIRMADA` o `RECHAZADA`.

## Tiempo real

- **Ofertas en pantalla:** `EventSource` sobre `/api/tiempo-real/ofertas?ids=...`, escuchando el evento `disponibilidad`. Se reabre cuando cambian las ofertas visibles.
- **Mis retenciones:** el `EventSource` nativo no permite enviar el header `Authorization`, así que `/api/tiempo-real/mis-retenciones` se lee con `fetch` y streaming de la respuesta (con el JWT en el header), escuchando `retencion-vencida`.
- **Versiones:** se guarda la última `version` por oferta y se descartan los eventos con versión menor o igual.
- **Al conectar o reconectar:** se consulta `GET /api/ofertas/{id}/disponibilidad` y el carrito, porque los eventos son avisos y no la fuente de verdad.

**Historial:** 2026-10-22: se agregan los estados de disponibilidad, la cuenta regresiva y el tiempo real; se quitan las sugerencias del carrito.
