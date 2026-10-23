# ADR-002: Arquitectura interna de los servicios

- **Estado:** Aceptada
- **Fecha:** 2026-10-22
- **Decisión de la cátedra:** D2

## Contexto

El enunciado exige que al menos dos servicios usen estilos o patrones de arquitectura interna distintos, aplicados de manera reconocible. El estilo de cada servicio tiene que responder a su problema, no repartirse al azar.

Los servicios tienen problemas muy distintos ([ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md)):

- **Comprobantes** recibe el mismo pedido por dos vías (API pública y evento `CompraConfirmada`) y depende de varias cosas externas e intercambiables: MySQL, SMTP, generación de PDF y RabbitMQ. Es además la capacidad publicada: es lo primero que se implementa y lo que más se va a probar con fallas provocadas.
- **Reservas** maneja el ciclo de vida de carrito y compra (`PENDIENTE`, `CONFIRMADA`, `RECHAZADA`), la recuperación de compras pendientes y la dependencia síncrona con Catálogo, que hay que poder simular caída o lenta en los tests.
- **Catálogo** concentra la operación crítica de cupos, cuya corrección depende de cómo se usa la base: transacciones, `SELECT ... FOR UPDATE`, orden de bloqueo y `SKIP LOCKED`. El resto es alta, baja, modificación y búsqueda.
- **Usuarios** es un ABM con autenticación.
- **Tiempo Real** no tiene dominio ni datos: reenvía eventos a conexiones SSE.

## Decisión

| Servicio | Estilo | Por qué |
|---|---|---|
| **Comprobantes** | **Hexagonal** (puertos y adaptadores) | Dos adaptadores de entrada (HTTP y consumidor RabbitMQ) usan el mismo caso de uso `EmitirComprobante`, que es exactamente el escenario del patrón. Los adaptadores de salida (MySQL, SMTP, PDF) se reemplazan por dobles en los tests unitarios, y el SMTP real o Mailpit se cambian por configuración. |
| **Reservas** | **Hexagonal** | El dominio (estados de compra y de ítem, transiciones válidas) se prueba sin infraestructura. El puerto `RetencionesCatalogo` permite probar timeouts, reintentos y la recuperación de compras `PENDIENTE` con un doble que falla a pedido. |
| **Catálogo** | **En capas** (handlers → services → repository) | Las reglas críticas viven en transacciones SQL. Esconder el repositorio detrás de un puerto genérico taparía justamente lo que hay que mostrar y revisar (bloqueos y su orden). Las capas son más directas y alcanzan para separar HTTP, lógica y acceso a datos. |
| **Usuarios** | **En capas** | ABM simple: un estilo más elaborado no aporta. |
| **Tiempo Real** | Componentes simples (`sse`, `hub`, `consumer`) | No tiene dominio ni persistencia; ningún patrón de capas o puertos agrega valor. |

Los dos estilos que se presentan como requisito son **hexagonal** (Comprobantes y Reservas) y **en capas** (Catálogo y Usuarios).

### Reglas de dependencia

**Hexagonal:**

```
adapters (http, rabbit, mysql, smtp, pdf, catalogo)
    │ implementan / llaman
    ▼
ports (interfaces)  ◄──  application (casos de uso)  ──►  domain (entidades y reglas)
```

- `domain` no importa ningún otro paquete del servicio ni bibliotecas de infraestructura (Gin, GORM, AMQP, SMTP).
- `application` depende de `domain` y de `ports`, nunca de `adapters`.
- `cmd/<servicio>/main.go` es el único lugar donde se crean los adaptadores y se conectan con los casos de uso.

**En capas:** `handlers → services → repository`. Una capa solo llama a la de abajo; `repository` no conoce HTTP y `handlers` no accede a la base.

### Cómo se verifica

- Revisión en cada Pull Request: un import que viole las reglas de dependencia no se aprueba.
- `go list -deps ./internal/domain/...` no debe incluir Gin, GORM ni el cliente de RabbitMQ (se puede automatizar como test).
- Los tests unitarios del dominio y de los casos de uso de los servicios hexagonales corren sin Docker.

## Alternativas consideradas

- **Todo en capas.** Cumple con el requisito de forma mínima, pero no aprovecha la doble entrada de Comprobantes ni facilita probar las fallas de Catálogo desde Reservas. Descartada.
- **Todo hexagonal.** Agrega interfaces y adaptadores sin beneficio en Usuarios y Tiempo Real, y en Catálogo esconde detrás de un puerto la lógica de bloqueo que hay que defender. Descartada.
- **CQRS en Catálogo** (modelo de escritura en MongoDB y MySQL, modelo de lectura en OpenSearch). La separación lectura/escritura ya existe de hecho por el índice de búsqueda, pero formalizarla como patrón suma complejidad en el servicio más cargado. Se deja como posible evolución.

## Consecuencias

- (+) Los dos estilos se reconocen en la estructura de carpetas y en las dependencias entre paquetes.
- (+) Comprobantes, el primer servicio que se implementa, ya muestra uno de los dos estilos en la Entrega 2.
- (+) Las reglas de dominio de los servicios hexagonales se prueban con tests unitarios rápidos, sin infraestructura.
- (−) Los servicios hexagonales tienen más archivos e interfaces, y más código de armado en `main.go`.
- (−) En Catálogo, la lógica crítica queda acoplada a MySQL. Se acepta porque es deliberada y se cubre con tests de integración contra MySQL real.
