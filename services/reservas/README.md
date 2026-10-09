# Servicio Reservas

**Responsabilidad:** carrito, confirmación de compras, estados de compra y **cupos disponibles**. Contiene la operación crítica del sistema (sin sobreventa, compra atómica).

**Datos propios (MySQL):** carritos, compras, ítems de compra, inventario de cupos, outbox de eventos.

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MySQL | Almacenamiento | Transacción de compra y cupos |
| Catálogo | REST síncrono | Precio vigente y definición de pack al confirmar (timeout 2 s, 2 reintentos) |
| RabbitMQ | Mensajería | Publica `CompraConfirmada` (vía outbox); consume `OfertaPublicada` |

**Estructura tentativa (Go, arquitectura hexagonal):** el dominio (reglas RN-01 a RN-07) no depende de frameworks ni de la base, lo que facilita los tests unitarios de las reglas.

```
cmd/reservas/main.go
internal/domain/          # entidades y reglas: Compra, Cupo, validación de packs y fechas
internal/application/     # casos de uso: ConfirmarCompra, AgregarAlCarrito
internal/ports/           # interfaces: RepositorioCompras, CatalogoClient, PublicadorEventos
internal/adapters/http/   # handlers (Gin)
internal/adapters/mysql/  # repositorios (GORM)
internal/adapters/catalogo/ # cliente REST con timeout y reintentos
internal/adapters/rabbit/ # outbox publisher y consumidor
```
