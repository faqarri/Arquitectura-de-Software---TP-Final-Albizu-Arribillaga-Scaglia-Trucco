# ADR-009: Consumo del proveedor

- **Estado:** Propuesta (estrategia general; se completa cuando la cátedra asigne el grupo proveedor)
- **Fecha:** 2026-10-23
- **Decisión de la cátedra:** D9

## Contexto

El sistema debe consumir una capacidad publicada por otro grupo y usarla en **un flujo importante**, con consecuencias de negocio; no alcanza con una llamada aislada ni con una pantalla de prueba. Según el enunciado:

- la comunicación sale **desde el microservicio responsable** de la funcionalidad, nunca desde el frontend ni a través de nuestro gateway;
- hay que trabajar solo con el contrato y la documentación publicados;
- hay que incluir un test de contrato que detecte cambios incompatibles;
- hay que definir timeouts, tratamiento de errores y qué ve el usuario cuando el proveedor falla, y demostrarlo en la jornada de fallas.

**La cátedra todavía no asignó el proveedor.** Este ADR fija la estrategia que vale para cualquier capacidad. Las secciones marcadas **[A COMPLETAR]** se llenan cuando se conozca el proveedor.

## Decisión

### Dónde vive el consumo

En el servicio **dueño de la funcionalidad** que usa la capacidad, elegido por el flujo y no por comodidad:

| Si la capacidad sirve para… | Servicio que la consume (orientativo) |
|---|---|
| Algo que ocurre al agregar al carrito o al comprar (por ejemplo, validar o cotizar) | Reservas |
| Información de las ofertas (por ejemplo, datos del destino) | Catálogo |
| Algo posterior a la compra (por ejemplo, notificar o registrar) | Reservas, por evento asíncrono |

El frontend nunca llama al proveedor. Nuestro gateway no interviene: la llamada sale del servicio hacia la URL pública del proveedor.

### Adaptador propio (capa anticorrupción)

- El servicio define un **puerto con nuestros propios términos** (por ejemplo, `ServicioExterno.Cotizar(destino, fechas)`) y un **adaptador** que lo implementa traduciendo al contrato del proveedor. En los servicios hexagonales ([ADR-002](ADR-002-arquitectura-interna.md)) es un adaptador de salida; en los servicios en capas, un paquete cliente usado solo desde la capa de servicios.
- Ningún tipo, nombre de campo ni código de error del proveedor sale del adaptador: el resto del servicio no se entera si el proveedor cambia de forma.
- Los errores del proveedor se traducen a errores propios: `NoDisponible` (reintentable), `Rechazado` (de negocio, no reintentable), `RespuestaInvalida` (el proveedor no respetó su contrato).

### Versión fijada del contrato

- Se guarda una copia del contrato consumido, **en la versión exacta** con la que se integró, en `docs/contracts/consumidos/<grupo>/<version>/`.
- El adaptador llama siempre a la versión mayor fijada (por ejemplo, `/v1`). Pasar a otra versión es un cambio deliberado, en un Pull Request propio, con el test de contrato actualizado.

### Timeouts, reintentos y circuit breaker

| Aspecto | Valor inicial (a ajustar con el SLA que publique el proveedor) |
|---|---|
| Timeout por intento | 2 s, o el que recomiende el proveedor si es menor |
| Reintentos | Hasta 2, con backoff exponencial (200 ms, 400 ms) y jitter, **solo en operaciones idempotentes**: `GET`, o `POST` si el proveedor admite una clave de idempotencia y se la enviamos. Nunca en un `POST` sin idempotencia |
| Qué se reintenta | Timeout, error de conexión, `502`, `503`, `504` y `429` (respetando `Retry-After`) |
| Qué no se reintenta | `4xx` de negocio o de validación |
| Circuit breaker | Se abre con 5 fallos seguidos o con más del 50 % de fallos en las últimas 20 llamadas; queda abierto 30 s; luego deja pasar 1 llamada de prueba (semiabierto) |
| Con el circuito abierto | Se responde enseguida con `NoDisponible`, sin llamar al proveedor |

### Qué ve el usuario si el proveedor falla

Depende de si la capacidad es imprescindible para el flujo. **[A COMPLETAR]** con una de estas dos opciones:

- **Imprescindible** (sin ella la operación no tiene sentido): la operación se rechaza rápido con un mensaje claro ("No pudimos completar X porque un servicio externo no responde; reintentá en unos minutos") y nada queda a medio hacer.
- **No imprescindible:** la operación sigue sin ese dato o acción. Si la acción debe ocurrir igual (por ejemplo, notificar), se registra en un outbox y se reintenta de forma asíncrona, y el usuario ve "pendiente".

En ningún caso el usuario ve un error técnico ni espera más que el timeout total (2 s × 3 intentos + esperas ≈ 7 s como máximo; con el circuito abierto, instantáneo).

### Test de contrato

- **Test de consumidor** (corre en cada Pull Request, sin red): con la copia fijada del OpenAPI del proveedor, verifica que las solicitudes que arma el adaptador respetan el esquema y que el adaptador sabe interpretar los ejemplos de respuesta y de error del contrato (biblioteca `kin-openapi` de Go).
- **Detección de cambios incompatibles** (corre todos los días y antes de cada demo): descarga el contrato que el proveedor publica y lo compara con la copia fijada usando `oasdiff breaking`. Si hay un cambio incompatible en la versión que usamos, el test falla y avisa antes de que falle la integración.
- **Prueba contra el mock del proveedor**, si lo publica, como test de integración.

### Observabilidad

Métricas por proveedor y operación: cantidad de llamadas, latencia, errores por tipo, reintentos y estado del circuit breaker. Los logs incluyen el `X-Correlation-Id`, que también se envía al proveedor si su contrato lo admite.

### [A COMPLETAR] cuando se asigne el proveedor

| Dato | Valor |
|---|---|
| Grupo proveedor y capacidad | |
| URL y versión del contrato fijada | |
| Autenticación (cómo se obtiene la credencial; se guarda en `.env`, nunca en el repositorio) | |
| Flujo importante en el que se usa y consecuencia de negocio | |
| Servicio consumidor | |
| Operaciones usadas, si son idempotentes y cómo | |
| Timeout definitivo (según su SLA) | |
| Imprescindible o no, y mensaje que ve el usuario | |
| Caída que se va a demostrar | |

## Alternativas consideradas

- **Llamar al proveedor desde el frontend.** Lo prohíbe el enunciado. Además expondría la credencial del proveedor en el navegador y no permitiría reintentos, circuit breaker ni observabilidad propios. Descartada.
- **Canalizar la llamada a través de nuestro gateway.** También lo prohíbe el enunciado. Además, el gateway quedaría acoplado a la lógica de un servicio. Descartada.
- **Usar los tipos del proveedor en todo el servicio**, sin adaptador. Es más rápido de escribir, pero cualquier cambio del proveedor se propaga a todo nuestro código. Descartada.
- **Reintentar todas las operaciones.** Un `POST` no idempotente reintentado tras un timeout puede ejecutar dos veces la operación en el proveedor (por ejemplo, dos cobros). Descartada.

## Consecuencias

- (+) Un cambio o una caída del proveedor queda encerrado en un adaptador y en un circuito.
- (+) El test de contrato avisa de los cambios incompatibles antes de que rompan el flujo.
- (+) El usuario recibe un mensaje claro y nunca espera más que el timeout total.
- (−) Más código (puerto, adaptador, traducción de errores) que una llamada directa.
- (−) Hasta que se asigne el proveedor, el flujo y la experiencia ante fallas no se pueden cerrar ni probar.
- (−) Mantener la copia fijada del contrato exige revisarla cuando el proveedor publique versiones nuevas.
