# API Gateway

Punto de entrada único del sistema.

**Responsabilidades:** validación de JWT emitidos por Usuarios, ruteo hacia los servicios, rate limiting, balanceo de carga entre instancias con verificación de disponibilidad, propagación del `X-Correlation-Id`.

**Rutas previstas:**

| Prefijo | Servicio |
|---|---|
| `/api/auth`, `/api/usuarios` | Usuarios |
| `/api/ofertas`, `/api/packs`, `/api/busqueda`, `/api/sugerencias` | Catálogo |
| `/api/carrito`, `/api/compras` | Reservas |

La API pública de Comprobantes para otros grupos se expone en su propio despliegue (ver ADR-008).

**Tecnología:** a definir (candidatos: Traefik, Nginx o un gateway en Go).
