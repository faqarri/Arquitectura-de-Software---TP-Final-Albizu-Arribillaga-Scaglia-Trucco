# API Gateway

Punto de entrada único del sistema.

**Responsabilidades:** validación de JWT emitidos por Usuarios, ruteo hacia los servicios, rate limiting, balanceo de carga entre instancias con verificación de disponibilidad, propagación del `X-Correlation-Id`.

**Rutas previstas:**

| Prefijo | Servicio | Notas |
|---|---|---|
| `/api/auth`, `/api/usuarios` | Usuarios | |
| `/api/ofertas`, `/api/packs`, `/api/busqueda` | Catálogo | Incluye `GET /api/ofertas/{id}/disponibilidad` |
| `/api/carrito`, `/api/compras` | Reservas | Requieren JWT |
| `/api/tiempo-real/ofertas` | Tiempo Real | **SSE**, pública |
| `/api/tiempo-real/mis-retenciones` | Tiempo Real | **SSE**, requiere JWT; el gateway propaga el id de usuario |

**Rutas que NO se publican:** `/internal/*` de Catálogo (retener, liberar y confirmar retenciones). Solo Reservas las usa dentro de la red interna; el gateway responde `404` a cualquier ruta `/internal`.

**Requisitos de las rutas SSE (`/api/tiempo-real/*`):**

- Sin buffering de respuesta, para que los eventos lleguen al navegador al instante.
- Timeout de lectura largo (las conexiones duran minutos; el servicio envía un *heartbeat* cada 20 s).
- Rate limiting por **cantidad de conexiones abiertas** por cliente, no por solicitudes por minuto.
- Preferentemente HTTP/2, para no agotar el límite de conexiones por dominio del navegador.

La API pública de Comprobantes para otros grupos se expone en su propio despliegue (ver ADR-008).

**Tecnología:** a definir (candidatos: Traefik, Nginx o un gateway en Go). Debe cumplir los requisitos de SSE anteriores.

**Historial:** 2026-10-22: se agregan las rutas SSE de Tiempo Real y la disponibilidad, se excluyen explícitamente las rutas `/internal` y se quita `/api/sugerencias` (sugerencias fuera del alcance).
