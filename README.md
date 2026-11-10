# Agencia de Viajes — Práctico Integrador Arquitectura de Software 2026

Sistema basado en microservicios para una agencia de viajes: los clientes buscan y reservan **transportes** (avión, tren, colectivo), **estadías** y **excursiones**, o seleccionan **packs** promocionales combinando opciones de cada tipo. Al agregar una oferta al carrito, su cupo queda **retenido 15 minutos** para que nadie más lo tome mientras el cliente termina la compra. Si una oferta no tiene cupo, el sistema distingue si está **agotada** o solo **retenida** por otros clientes y avisa en tiempo real cuando se libera. Al confirmar una compra, el sistema emite un **comprobante** y lo envía por correo electrónico.

## Integrantes

| Nombre y apellido | Usuario GitHub |
|---|---|
| Facundo Arribillaga | faqarri |
| Augusto Albizu | AguAlbizu |
| Juan Ignacio Scaglia | Juani-Scaglia |
| Mariano Trucco | Mariantrucco |

## Objetivo

Construir un sistema que resuelva la reserva de servicios de viaje sin sobreventa de cupos, con retención temporal del cupo en el carrito, compras atómicas de packs (todo o nada), búsqueda indexada del catálogo, disponibilidad actualizada en tiempo real y notificación asíncrona con comprobante adjunto.

## Flujo principal

1. El cliente busca ofertas (filtros por destino, fechas, tipo y precio) y ve su disponibilidad: `DISPONIBLE`, `RETENIDA` o `AGOTADA`.
2. Agrega transportes, estadías, excursiones o un pack al carrito. Catálogo **retiene el cupo por 15 minutos** (un pack retiene todas sus opciones o ninguna) y el carrito muestra una cuenta regresiva.
3. Confirma la compra: Catálogo confirma **todas** las retenciones vigentes del carrito en una única transacción y devuelve los precios vigentes; Reservas registra la compra como `CONFIRMADA`. Si alguna retención venció, no se confirma ninguna.
4. Se publica el evento `CompraConfirmada`.
5. El servicio de Comprobantes emite la factura y la envía por correo al cliente.
6. Si el cliente abandona el carrito, la retención vence sola y el cupo vuelve a estar disponible. Quien estaba mirando esa oferta se entera en tiempo real.

## Ejecución local

Requisitos: Docker y Docker Compose.

```bash
cp .env.example .env      # completar valores locales (no se versiona)
docker compose up -d --build
```

> MySQL crea los esquemas y un usuario por servicio **solo la primera vez**, con el volumen vacío. Si ya habías levantado el entorno antes de esos cambios, ejecutá `docker compose down -v` y después `docker compose up -d` (borra los datos locales).

Grafana requiere que `GRAFANA_DB_PASSWORD` esté definida en `.env`; se usa para la cuenta MySQL de solo lectura que consulta `comprobantes.app_logs`. No reutilices una contraseña real de otra cuenta.

Si el puerto local `3306` ya está ocupado, configurá `MYSQL_PUERTO=3307` en `.env`; Compose seguirá conectando los servicios entre sí por la red interna. El endpoint de prueba `GET /error` devuelve HTTP 500 y registra un log `ERROR`, pero está desactivado por defecto (`HABILITAR_ENDPOINT_ERROR_PRUEBA=false`). Para habilitarlo en local, cambiá esa variable a `true` en `.env` y recreá Comprobantes con `docker compose up --build -d comprobantes`. Mantenelo deshabilitado en cualquier entorno compartido o desplegado.

Componentes:

| Componente | URL local |
|---|---|
| Frontend (React) | http://localhost:5173 |
| Catálogo, instancias 1 y 2 (ofertas, packs, cupos, búsqueda) | http://localhost:8083 · http://localhost:8084 (salud: `/health`) |
| Reservas (carrito, compras, guías) | http://localhost:8082 |
| Comprobantes (API pública v1, capacidad publicada) | http://localhost:8081 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (dashboard **Agencia de Viajes - Observabilidad**, carpeta **Observabilidad**) |
| Jaeger (trazas distribuidas) | http://localhost:16686 |
| RabbitMQ (management) | http://localhost:15672 |
| Mailpit (bandeja de correo local) | http://localhost:8025 |
| OpenSearch · MongoDB · Redis · MySQL | `:9200` · `:27017` · `:6379` · `:3306` (o `MYSQL_PUERTO`) |
| Mocks (Prism) de Comprobantes, de la API interna de Catálogo y de Estudiario | http://localhost:4010 · :4020 · :4030 |

> **Estado (10/11/2026):** Catálogo (en capas, dos instancias, MongoDB + MySQL, caché Redis, búsqueda con OpenSearch, guías de Estudiario), Reservas (hexagonal: carrito, compra con recuperación, `CompraConfirmada`, guías) y Comprobantes (hexagonal) funcionan de punta a punta: una compra confirmada termina en la factura por correo. El frontend tiene todas las pantallas y por ahora usa datos de demostración (`FRONTEND_DATOS_FALSOS=true`). **Falta integrar en `main` el API Gateway y el servicio Usuarios**; hasta entonces, los servicios se prueban directo con los headers `X-Usuario-*` que pondría el gateway (ver el README de cada servicio). Tiempo Real (bonus) no está implementado. Decisiones: [índice de ADRs](docs/adr/README.md). Limitaciones: [ARCHITECTURE.md §9](docs/ARCHITECTURE.md#9-limitaciones-conocidas-y-deuda-técnica).

### Observabilidad local

Prometheus recolecta las métricas de Comprobantes, Reservas y las dos instancias de Catálogo desde `/metrics`. Grafana aprovisiona Prometheus y MySQL como fuentes de datos y el dashboard **Agencia de Viajes - Observabilidad**, con tasa de solicitudes, latencia p95, respuestas 5xx y los logs recientes (`app_logs` de cada servicio, con `correlacion_id` y `trace_id`). Las trazas de una compra se siguen en Jaeger a través de Reservas, RabbitMQ y Comprobantes.

### Código y tests

El backend está en Go. Cada servicio es un módulo propio (`services/<servicio>/go.mod`) y `pkg/` es un módulo compartido (observabilidad, mensajería con outbox y DLQ, y el adaptador de Estudiario). El archivo `go.work` de la raíz los une para el editor:

```bash
cd services/catalogo && go test -short ./...   # unitarios, sin Docker (igual en reservas, comprobantes y pkg)
cd services/catalogo && go test ./...          # + integración contra MySQL, MongoDB, RabbitMQ u OpenSearch reales (testcontainers)
cd frontend && npm test
```

Pruebas de carga (k6) en [`tests/carga/`](tests/carga/): resultados en [ADR-013](docs/adr/ADR-013-capacidad-y-costos.md).

## Bonus

Desafío elegido: **actualizaciones de información en tiempo real** ([ADR-015](docs/adr/ADR-015-disponibilidad-en-tiempo-real.md)).

- El servicio **Tiempo Real** (sin estado) envía al navegador, por Server-Sent Events, los cambios de disponibilidad de las ofertas que se están mirando (evento `disponibilidad`) y el vencimiento de las retenciones del propio carrito (evento `retencion-vencida`).
- Catálogo publica `DisponibilidadCambiada` y `RetencionVencida` en un exchange fanout de RabbitMQ; cada instancia de Tiempo Real recibe todos los eventos, así que escala sin coordinación.
- Los eventos son avisos de mejor esfuerzo: al reconectar, el navegador consulta el estado actual.

## Parte desplegada

La capacidad publicada (API de Comprobantes) se despliega en una VM en la nube con HTTPS: procedimiento en [docs/DESPLIEGUE.md](docs/DESPLIEGUE.md), archivos en [`deploy/comprobantes/`](deploy/comprobantes/).

- URL pública: `<COMPLETAR: https://…>`
- Contrato y guía para el grupo consumidor: [docs/contracts/](docs/contracts/README.md)

## Documentación

| Documento | Contenido |
|---|---|
| [SPEC.md](SPEC.md) | Alcance, historias de usuario, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arquitectura, diagramas, servicios y comunicaciones |
| [docs/adr/](docs/adr/) | Registro de decisiones arquitectónicas |
| [docs/contracts/](docs/contracts/) | Contrato versionado de la capacidad publicada, contrato consumido (Estudiario) y contrato interno de Catálogo |
| [docs/DESPLIEGUE.md](docs/DESPLIEGUE.md) | Despliegue de Comprobantes en la nube |
| [docs/POSTMORTEM.md](docs/POSTMORTEM.md) | Análisis de caídas (provocadas y no planificadas) |
| [docs/PLAN-DE-DESARROLLO.md](docs/PLAN-DE-DESARROLLO.md) | Reparto y estado del trabajo hasta la presentación |
| [docs/devoluciones/](docs/devoluciones/) | Cambios pedidos en cada devolución de la cátedra y dónde se aplicaron |

## Forma de trabajo

- `main`: rama protegida, solo recibe merges por Pull Request. Contiene la versión evaluable y es el punto de partida común.
- **Una rama por integrante** (`albizu`, `arribillaga`, `scaglia`, `trucco`). Cada uno trabaja en la suya y, antes de empezar una tarea, la actualiza con lo último de `main`.
- El trabajo terminado se integra a `main` con un Pull Request desde la rama personal.
- Cada PR requiere al menos una revisión de otro integrante y que pasen los tests.
- Los secretos se cargan por variables de entorno (`.env`, excluido del repositorio). `.env.example` documenta las variables sin valores reales.
