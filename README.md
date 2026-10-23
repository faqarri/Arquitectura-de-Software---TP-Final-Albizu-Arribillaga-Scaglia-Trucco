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

Componentes disponibles en esta etapa:

| Componente | URL local |
|---|---|
| Servicio Comprobantes (API pública v1) | http://localhost:8081 (salud: `/health`) |
| Mock del contrato de Comprobantes (Prism) | http://localhost:4010 |
| RabbitMQ (management) | http://localhost:15672 |
| Mailpit (bandeja de correo local) | http://localhost:8025 |
| OpenSearch | http://localhost:9200 |

> Estado: Entrega 2 en curso. Comprobantes tiene su esqueleto (ver [su README](services/comprobantes/README.md)); los demás servicios se incorporan al `docker-compose.yml` a medida que se implementan.

### Código y tests

El backend está en Go. Cada servicio es un módulo propio (`services/<servicio>/go.mod`) y `pkg/` es un módulo compartido con logs, correlación y trazas. El archivo `go.work` de la raíz los une para el editor y para correr todo junto:

```bash
go test ./pkg/... ./services/comprobantes/...
```

## Bonus

Desafío elegido: **actualizaciones de información en tiempo real** ([ADR-015](docs/adr/ADR-015-disponibilidad-en-tiempo-real.md)).

- El servicio **Tiempo Real** (sin estado) envía al navegador, por Server-Sent Events, los cambios de disponibilidad de las ofertas que se están mirando (evento `disponibilidad`) y el vencimiento de las retenciones del propio carrito (evento `retencion-vencida`).
- Catálogo publica `DisponibilidadCambiada` y `RetencionVencida` en un exchange fanout de RabbitMQ; cada instancia de Tiempo Real recibe todos los eventos, así que escala sin coordinación.
- Los eventos son avisos de mejor esfuerzo: al reconectar, el navegador consulta el estado actual.

## Parte desplegada

La capacidad publicada (API de Comprobantes) se desplegará en la nube con URL pública. _Pendiente: completar URL._

## Documentación

| Documento | Contenido |
|---|---|
| [SPEC.md](SPEC.md) | Alcance, historias de usuario, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arquitectura, diagramas, servicios y comunicaciones |
| [docs/adr/](docs/adr/) | Registro de decisiones arquitectónicas |
| [docs/contracts/](docs/contracts/) | Contrato versionado de la capacidad publicada |
| [docs/devoluciones/](docs/devoluciones/) | Cambios pedidos en cada devolución de la cátedra y dónde se aplicaron |

## Forma de trabajo

- `main`: rama protegida, solo recibe merges por Pull Request. Contiene la versión evaluable y es el punto de partida común.
- **Una rama por integrante** (`albizu`, `arribillaga`, `scaglia`, `trucco`). Cada uno trabaja en la suya y, antes de empezar una tarea, la actualiza con lo último de `main`.
- El trabajo terminado se integra a `main` con un Pull Request desde la rama personal.
- Cada PR requiere al menos una revisión de otro integrante y que pasen los tests.
- Los secretos se cargan por variables de entorno (`.env`, excluido del repositorio). `.env.example` documenta las variables sin valores reales.
