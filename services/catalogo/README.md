# Servicio Catálogo

**Responsabilidad:** descripción de ofertas (transporte, estadía, excursión) y packs; búsqueda indexada; sugerencias por destino.

**Datos propios:** ofertas, packs y sus opciones (MongoDB); índice de búsqueda (OpenSearch, derivado y reconstruible).

**Dependencias:**

| Dependencia | Tipo | Uso |
|---|---|---|
| MongoDB | Almacenamiento | Fuente de verdad del catálogo |
| OpenSearch | Motor de búsqueda | Búsqueda con filtros, orden y paginación |
| RabbitMQ | Mensajería | Publica `OfertaPublicada` y `OfertaActualizada`; consume `OfertaActualizada` para indexar |

**Expone:** API REST para frontend (vía gateway) y para Reservas (precios y definición de packs).

**Estructura tentativa (Go, arquitectura en capas):**

```
cmd/catalogo/main.go
internal/handlers/     # HTTP (Gin)
internal/services/     # lógica de aplicación, sugerencias
internal/repository/   # MongoDB
internal/search/       # OpenSearch e indexador
internal/events/       # publicación/consumo RabbitMQ
```
