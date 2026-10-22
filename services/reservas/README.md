# Servicio Reservas

**Responsabilidad:** carrito, confirmación de compras y estados de compra (`PENDIENTE`, `CONFIRMADA`, `RECHAZADA`). Coordina la operación crítica: pide a Catálogo retener cupo al agregar al carrito y confirmar las retenciones al comprar, y registra el resultado. **No guarda cupos**: los cupos y las retenciones son de Catálogo ([ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)).

**Datos propios (MySQL, esquema `reservas`):** carritos (cada ítem guarda `retencionId` y `expiraEn`; un pack guarda las retenciones de todas sus opciones), compras (con el email del cliente) e ítems de compra con el precio confirmado, outbox de eventos.

**Identidad del cliente:** el id y el email del usuario llegan en los headers `X-Usuario-Id` y `X-Usuario-Email`, que el gateway completa a partir del JWT. El email se guarda en la compra al crearla, así el evento `CompraConfirmada` y el proceso de recuperación de compras pendientes no dependen de Usuarios.

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MySQL | Almacenamiento | Carrito, compras y outbox |
| Catálogo (API interna) | REST síncrono | `POST /internal/retenciones`, `DELETE /internal/retenciones/{id}`, `POST /internal/retenciones/confirmacion`. Timeout 2 s, hasta 2 reintentos con backoff y jitter; los 409 y 422 de negocio no se reintentan |
| RabbitMQ | Mensajería | Publica `CompraConfirmada` (vía outbox) con `email` e `items` (`descripcion`, `precio`). Consume `RetencionVencida` (cola `reservas.retenciones`, con DLQ) para marcar ítems del carrito como vencidos |

**Comportamiento ante fallas de Catálogo:**

| Operación | Si Catálogo no responde |
|---|---|
| Agregar al carrito | `503`; el ítem no se agrega |
| Quitar del carrito | El ítem se quita igual; la retención vence sola |
| Comprar | La compra queda `PENDIENTE` (`202`); un **proceso de recuperación** reintenta la confirmación con la misma `Idempotency-Key` (= `compraId`) hasta obtener `200` o `409` |

**Estructura (Go, arquitectura hexagonal, [ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md)):** el dominio (estados de la compra y del ítem del carrito, reglas de transición) no depende de frameworks ni de la base, lo que facilita los tests unitarios.

```
cmd/reservas/main.go
internal/domain/            # entidades y reglas: Carrito, ItemCarrito (retención, vencimiento), Compra y sus estados
internal/application/       # casos de uso: AgregarAlCarrito, QuitarDelCarrito, ConfirmarCompra, RecuperarComprasPendientes, MarcarRetencionVencida
internal/ports/             # interfaces: RepositorioCompras, RetencionesCatalogo, PublicadorEventos
internal/adapters/http/     # handlers (Gin)
internal/adapters/mysql/    # repositorios (GORM)
internal/adapters/catalogo/ # cliente REST de la API interna con timeout, reintentos e Idempotency-Key
internal/adapters/rabbit/   # outbox publisher y consumidor de RetencionVencida
```

**Historial:** 2026-10-22: deja de guardar cupos (pasan a Catálogo). Retiene al agregar al carrito y confirma retenciones al comprar. Se agrega el estado `PENDIENTE` con recuperación. Ya no consulta precios ni consume `OfertaPublicada`; consume `RetencionVencida` ([ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)). 2026-10-22: el email del cliente se obtiene del header `X-Usuario-Email` y se guarda en la compra.
