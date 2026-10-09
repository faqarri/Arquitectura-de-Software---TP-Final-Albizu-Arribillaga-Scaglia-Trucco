# Agencia de Viajes — Práctico Integrador Arquitectura de Software 2026

Sistema basado en microservicios para una agencia de viajes: los clientes buscan y reservan **transportes** (avión, tren, colectivo), **estadías** y **excursiones**, o seleccionan **packs** promocionales combinando opciones de cada tipo. Al confirmar una compra, el sistema emite un **comprobante** y lo envía por correo electrónico.

## Integrantes

| Nombre y apellido | Usuario GitHub |
| Facundo Arribillaga | faqarri |
| Augusto Albizu | AguAlbizu |
| Juan Ignacio Scaglia | Juani-Scaglia |
| Mariano Trucco | --- |

## Objetivo

Construir un sistema que resuelva la reserva de servicios de viaje sin sobreventa de cupos, con compras atómicas de packs (todo o nada), búsqueda indexada del catálogo, sugerencias según el destino del carrito y notificación asíncrona con comprobante adjunto.

## Flujo principal

1. El cliente busca ofertas (filtros por destino, fechas, tipo y precio).
2. Agrega transportes, estadías, excursiones o un pack al carrito. El sistema sugiere packs y ofertas del mismo destino.
3. Confirma la compra: el servicio de Reservas valida reglas y descuenta los cupos en una única transacción.
4. Se publica el evento `CompraConfirmada`.
5. El servicio de Comprobantes emite la factura y la envía por correo al cliente.

## Ejecución local

Requisitos: Docker y Docker Compose.

```bash
cp .env.example .env      # completar valores locales (no se versiona)
docker compose up -d
```

Servicios de infraestructura disponibles en esta etapa:

| Componente | URL local |
|---|---|
| Mock del contrato de Comprobantes (Prism) | http://localhost:4010 |
| RabbitMQ (management) | http://localhost:15672 |
| Mailpit (bandeja de correo local) | http://localhost:8025 |
| OpenSearch | http://localhost:9200 |

> Estado: Entrega 1 (diseño y contrato con mock). Los microservicios se incorporan al `docker-compose.yml` a medida que se implementan.

## Parte desplegada

La capacidad publicada (API de Comprobantes) se desplegará en la nube con URL pública. _Pendiente: completar URL._

## Documentación

| Documento | Contenido |
|---|---|
| [SPEC.md](SPEC.md) | Alcance, historias de usuario, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arquitectura, diagramas, servicios y comunicaciones |
| [docs/adr/](docs/adr/) | Registro de decisiones arquitectónicas |
| [docs/contracts/](docs/contracts/) | Contrato versionado de la capacidad publicada |

## Forma de trabajo

- `main`: rama protegida, solo recibe merges por Pull Request. Contiene la versión evaluable.
- `feature/<servicio>-<descripcion>`: una rama por funcionalidad.
- Cada PR requiere al menos una revisión de otro integrante y que pasen los tests.
- Los secretos se cargan por variables de entorno (`.env`, excluido del repositorio). `.env.example` documenta las variables sin valores reales.
