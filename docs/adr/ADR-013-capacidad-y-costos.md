# ADR-013: Capacidad y costos

- **Estado:** Aceptada (primera versión: escenario A ejecutado el 2026-10-23; el escenario B se ejecuta cuando existan Catálogo y Reservas; ver [Revisiones](#revisiones))
- **Fecha:** 2026-10-23
- **Decisión de la cátedra:** D13

## Contexto

El enunciado pide pruebas de carga con resultados documentados y analizados, el principal límite identificado, una alternativa de escalado y una estimación de costos.

Estado al 2026-10-23: el único servicio con código es **Comprobantes** (API, repositorio MySQL con outbox y worker de envíos). El **escenario A se ejecutó** y sus resultados están en [Resultados](#resultados). Catálogo y Reservas todavía no existen, así que el escenario B sigue siendo un plan. Los números de las secciones de objetivos, hipótesis y costos son objetivos o estimaciones; los de Resultados son mediciones.

## Decisión

### Herramienta y entorno

- **k6**, con los scripts versionados en `tests/carga/` (a crear junto con la implementación).
- Se ejecuta contra el entorno de `docker compose` en una sola máquina. Se registran las características de esa máquina (CPU, memoria, disco) junto con cada resultado, porque los números absolutos dependen de ella.
- Mientras corre la prueba se observa el tablero de D11 (latencia, errores, uso del pool de MySQL, largo de las colas, CPU y memoria de cada contenedor). Mientras Prometheus y Grafana no estén desplegados ([ADR-011](ADR-011-observabilidad.md)), se usan los resultados de k6, la consola de RabbitMQ, `docker stats` y las trazas de Jaeger.

### Escenario A: emisión de comprobantes (`POST /v1/comprobantes`)

Es la capacidad publicada y la única parte del sistema implementada hoy.

**Requisito previo:** la API limita a 60 solicitudes por minuto por API Key (1 por segundo). Con pocas claves, la prueba mediría el limitador y no el servicio: para una tasa de N solicitudes por segundo hacen falta más de N claves. Se usan **400 API Keys de prueba** (`COMPROBANTES_API_KEYS` del entorno de prueba), repartidas **por solicitud** y no por usuario virtual, más una corrida aparte con una sola clave para verificar el `429`. En A4, cada reintento usa la misma API Key que la solicitud original, porque la idempotencia es por consumidor.

| Corrida | Carga | Para qué |
|---|---|---|
| A1. Humo | 1 solicitud por segundo, 1 minuto | Verificar el script y el entorno |
| A2. Carga nominal | 20 solicitudes por segundo constantes, 5 minutos | Comparar con el objetivo |
| A3. Carga creciente | De 10 a 300 solicitudes por segundo en 10 minutos (`ramping-arrival-rate`); se corta cuando los errores superan el 1 % o el p95 supera 1 s | Encontrar el límite |
| A4. Idempotencia bajo carga | 10 % de las solicitudes repiten una `Idempotency-Key` ya usada | Verificar `200` sin duplicados y su costo |
| A5. Límite de solicitudes | 1 sola API Key, 2 solicitudes por segundo | Verificar `429` con `Retry-After` |

```javascript
// tests/carga/comprobantes.js (esquema del script A2)
import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const claves = __ENV.API_KEYS.split(',');

export const options = {
  scenarios: {
    nominal: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1s',
               duration: '5m', preAllocatedVUs: 50 },
  },
  thresholds: { http_req_failed: ['rate<0.001'], http_req_duration: ['p(95)<300', 'p(99)<500'] },
};

export default function () {
  const cuerpo = JSON.stringify({
    email: `cliente${__VU}@example.com`,
    items: [{ descripcion: 'Excursión x2', precio: 30000 }, { descripcion: 'Cargo por servicio', precio: 1500 }],
  });
  const r = http.post(`${__ENV.BASE_URL}/v1/comprobantes`, cuerpo, { headers: {
    'Content-Type': 'application/json',
    'X-API-Key': claves[__VU % claves.length],
    'Idempotency-Key': uuidv4(),
  }});
  check(r, { 'emitido (201)': (res) => res.status === 201 });
}
```

**Qué se mide:**

| Métrica | Fuente |
|---|---|
| Latencia p50, p95 y p99 del `POST`; solicitudes por segundo; tasa de errores por código | k6 |
| Conexiones del pool de MySQL en uso y en espera (máximo configurado: 20) | Métricas del servicio (`database/sql` `DBStats`) |
| Transacciones por segundo y esperas de bloqueo en MySQL | Exportador de MySQL |
| Largo de `comprobantes.envios` y tiempo hasta `ENVIADO` | RabbitMQ y métricas del worker |
| Correos por segundo enviados | Métricas del worker y Mailpit |
| CPU y memoria de cada contenedor | `docker stats` o cAdvisor |

**Objetivos (no medidos):** con 20 solicitudes por segundo, p95 ≤ 300 ms, p99 < 500 ms (el objetivo de servicio de [ADR-011](ADR-011-observabilidad.md)) y menos de 0,1 % de errores; el 95 % de los correos llega a `ENVIADO` en 60 s o menos (también de ADR-011), sin acumulación sostenida en la cola.

### Escenario B: flujo de compra (plan, cuando estén Catálogo y Reservas)

| Corrida | Carga | Qué se verifica |
|---|---|---|
| B1. Demanda distribuida | 200 usuarios virtuales que buscan, retienen y compran sobre 500 ofertas distintas | Latencia de retener y de confirmar; tasa de compras `PENDIENTE` |
| B2. Oferta caliente | 200 usuarios virtuales que intentan retener la **misma** oferta, con 50 cupos | Que nunca se vendan ni retengan más de 50 (RN-01: al final, `vendidos + retenidos activos ≤ 50`); latencia bajo contención; esperas de bloqueo (`Innodb_row_lock_waits`, `Innodb_row_lock_time`) |
| B3. Vencimiento masivo | 5000 retenciones que vencen en el mismo minuto | Duración del barrido, retraso de `RetencionVencida` y de los avisos SSE |

### Hipótesis del principal límite

Se ordenan por probabilidad. La prueba confirma o descarta cada una.

1. **Envío de correos (Comprobantes).** El worker genera un PDF y hace una conexión SMTP por correo. Si se emite más rápido de lo que se envía, la cola `comprobantes.envios` crece sin parar. No afecta la latencia del `POST` (que solo inserta), pero sí el tiempo hasta `ENVIADO`. Señal: cola creciente con CPU del worker alta o esperas de SMTP. **Resultado: casi.** El límite estuvo en el camino asíncrono, pero un paso antes: el **publicador del outbox** (ver [Resultados](#resultados)). La cola de envíos nunca pasó de 2 mensajes.
2. **Pool de conexiones de MySQL de Comprobantes (20).** Cada `POST` usa una transacción (comprobante, ítems y outbox) y cada commit escribe a disco. Señal: conexiones en espera mayores que cero y latencia creciente con la CPU de MySQL disponible.
3. **Contención en la fila de inventario de una oferta caliente (Catálogo).** La regla de bloqueo de ADR-014 serializa todas las operaciones de una misma oferta: la capacidad para esa oferta es aproximadamente 1 / duración de la transacción, sin importar cuántas instancias haya. Señal: en B2, la latencia de retener crece de forma lineal con la cantidad de usuarios concurrentes. Es un límite **deliberado**, porque garantiza RN-01.

### Alternativas de escalado

| Límite | Alternativa | Costo |
|---|---|---|
| **Publicador del outbox (límite medido: ≈ 45 eventos/s)** | Hoy publica un lote de 50 eventos por segundo (`tamanioLoteOutbox = 50`, `IntervaloOutbox = 1 s`), cada uno con su confirmación y su `UPDATE`. Alternativas, de menor a mayor esfuerzo: (1) volver a leer enseguida mientras el lote venga lleno, en lugar de esperar al próximo segundo; (2) lotes más grandes, con confirmaciones del broker en bloque y un solo `UPDATE ... WHERE id IN (...)`; (3) varios publicadores, cada uno con `SELECT ... FOR UPDATE SKIP LOCKED` sobre el outbox | Bajo. La opción 1 es un cambio de pocas líneas |
| Envío de correos | Varios consumidores del worker (`prefetch` mayor o más instancias); conexión SMTP reutilizada | Bajo. El estado ya está en la base |
| Pool de MySQL de Comprobantes | Más instancias del servicio (no tiene estado propio) y luego más recursos para MySQL (escalado vertical) | Con varias instancias, el límite de 60 solicitudes por minuto debe contarse en Redis y no en memoria |
| Oferta caliente | **Partir el cupo** de la oferta en N filas de inventario (por ejemplo, 4 filas de 25 cupos) y retener en cualquiera con lugar | Alto: complica el cálculo de disponibilidad y la regla de bloqueo. Solo si B2 muestra que hace falta |
| Catálogo en general | Más instancias detrás del gateway (D12): bloqueos de fila en MySQL y `SKIP LOCKED` en el barrido ya lo permiten | Bajo |

### Estimación de costos del despliegue de Comprobantes

En la nube solo se despliega la capacidad publicada ([ADR-008](ADR-008-contrato-propio.md)), que necesita el servicio, MySQL, RabbitMQ (outbox y worker) y un servidor SMTP real (Mailpit es solo local). El proceso corre todo el tiempo: aunque la API no reciba solicitudes, el worker y el publicador de outbox tienen que estar activos.

**Opción elegida: una máquina virtual con Docker Compose** (por ejemplo, Hetzner Cloud, 2 vCPU y 4 GB de RAM):

| Rubro | Estimación mensual |
|---|---|
| Cómputo: VM 2 vCPU / 4 GB con IPv4 (servicio, MySQL, RabbitMQ y Caddy para HTTPS en contenedores) | ≈ € 4,50 |
| Base de datos: MySQL en la misma VM, volumen incluido (40 GB) | € 0 adicional |
| Copias de seguridad: snapshots automáticos de la VM (≈ 20 % del costo de la VM) | ≈ € 0,90 |
| Correo: proveedor SMTP transaccional en plan gratuito (volumen de prueba: decenas de correos por día) | € 0 (verificar el límite del plan) |
| Dominio: subdominio gratuito o del proveedor; certificado de Let's Encrypt vía Caddy | € 0 |
| **Total** | **≈ € 5,50 por mes (≈ USD 6)** |

**Opción comparada: servicios gestionados en Google Cloud:**

| Rubro | Estimación mensual |
|---|---|
| Cómputo: Cloud Run con 1 instancia siempre activa (1 vCPU, 512 MiB, facturación por instancia; ≈ 2,6 millones de vCPU-segundo menos el nivel gratuito) | ≈ USD 45 |
| Base de datos: Cloud SQL para MySQL `db-f1-micro` + 10 GB de disco SSD | ≈ USD 10–12 |
| RabbitMQ: plan gratuito de un proveedor gestionado | USD 0 (con límites de conexiones y mensajes) |
| Correo | USD 0 (igual que la opción elegida) |
| **Total** | **≈ USD 55–60 por mes** |

Precios tomados de fuentes públicas el 2026-10-23. Las fuentes no coinciden entre sí (y Hetzner ajustó precios en 2026), así que **hay que verificarlos en las páginas oficiales antes de contratar**.

**Por qué la VM:** cuesta unas diez veces menos, reutiliza el mismo `docker-compose` que el entorno local y la carga esperada (un grupo consumidor, decenas de solicitudes por día) está muy por debajo de su capacidad. Lo que se resigna (alta disponibilidad, copias y actualizaciones gestionadas) se acepta para un despliegue que debe durar hasta el final de la evaluación. Si la capacidad tuviera uso real, la opción gestionada es el camino.

### Resultados

**Entorno:** Docker Desktop en Windows 11, con 16 CPU y 7,4 GiB de RAM asignados a Docker. Todos los contenedores (Comprobantes, MySQL 8.4, RabbitMQ 3.13, Mailpit, Jaeger) y k6 0.54.0 en la misma máquina, en la red de `docker compose`. Fecha: 2026-10-23. Script: [`tests/carga/comprobantes.js`](../../tests/carga/comprobantes.js).

| Corrida | Solicitudes | Tasa | Errores | p50 | p95 | p99 | Máx. | Resultado |
|---|---|---|---|---|---|---|---|---|
| A1. Humo | 61 | 1/s | 0 % | 16 ms | 22 ms | 25 ms | 25 ms | 61/61 `201` |
| A2. Nominal | 6001 | 20/s | 0 % | 15 ms | 21 ms | 26 ms | 63 ms | **Cumple los objetivos** (p95 ≤ 300 ms, p99 < 500 ms, < 0,1 % de errores) |
| A3. Creciente | 92 999 | 10 → 300/s (media 155/s) | 0 % | 19 ms | 33 ms | 49 ms | 555 ms | La API **no llegó a su límite** a 300/s |
| A4. Idempotencia | 2401 | 20/s | 0 % | 15 ms | 20 ms | 23 ms | 30 ms | 2175 `201` nuevos y 226 reintentos `200` con el mismo comprobante |
| A5. Límite | Ráfaga de 80 con 1 API Key | — | — | — | — | — | — | 73 `201` y luego `429` `application/problem+json` con `Retry-After: 1` (*token bucket*: ráfaga de 60 más la recarga de 1 por segundo) |

**Camino asíncrono** (muestras cada ~20 s durante A2 y A3; 100 % de CPU = un núcleo):

| Momento | Emisiones/s (k6) | Correos `ENVIADO`/s | Outbox sin publicar | Cola `comprobantes.envios` | CPU Comprobantes / MySQL |
|---|---|---|---|---|---|
| A2 (todo el tramo) | 20 | 20 | 7 a 19, estable | 1 a 2 | ~55 % / ~30 % |
| A3, minuto 1 | ~40 | ~32 | 38 | 2 | 129 % / 70 % |
| A3, minuto 3 | ~100 | ~48 | 1 902 | 2 | 158 % / 86 % |
| A3, minuto 6 | ~190 | ~46 | 16 852 | 2 | 233 % / 138 % |
| A3, minuto 10 | ~300 | ~44 | **62 830** | 2 | 288 % / 171 % |

**Análisis:**

- **El principal límite es el publicador del outbox, con ≈ 45 eventos por segundo.** A partir de unas 50 emisiones por segundo el outbox crece de forma lineal, mientras la cola de envíos sigue casi vacía: el worker y el SMTP (Mailpit) procesan todo lo que les llega. El techo coincide con el diseño del publicador (un lote de 50 eventos por segundo, publicados y marcados de a uno). Es un límite de **configuración**, no de infraestructura (ver Alternativas de escalado).
- **Consecuencia para el negocio:** ningún comprobante se pierde (el outbox está en la base) y la API sigue respondiendo en milisegundos, pero durante un pico el tiempo hasta `ENVIADO` crece sin límite. Al final de A3, con 62 830 eventos pendientes, vaciar el outbox lleva unos 23 minutos. El objetivo de ADR-011 (95 % de los correos `ENVIADO` en 60 s) se cumple hasta unas 45 emisiones por segundo sostenidas.
- **La API soporta al menos 300 solicitudes por segundo** en esta máquina, con p99 < 50 ms y sin errores; no se encontró su límite. Comprobantes llegó a ~2,9 núcleos y MySQL a ~1,7, así que el siguiente límite probable es la CPU. Hipótesis 2 (pool de MySQL): **no apareció** a esta carga.
- **Contexto:** la carga esperada de la capacidad publicada (un grupo consumidor, decenas de solicitudes por día, con un máximo de 60 por minuto por API Key) está más de 1000 veces por debajo de lo probado. El límite del outbox importa para picos de compras propias, no para el uso externo previsto.
- **Limitaciones de la medición:** k6 y el sistema compartieron la máquina; Mailpit acepta correos mucho más rápido que un SMTP real, así que en producción el envío podría pasar a ser el límite; y los datos de CPU son de `docker stats`, porque Prometheus todavía no está desplegado.

Pendiente: escenario B (flujo de compra y oferta caliente) cuando existan Catálogo y Reservas, y la medición de caché antes y después de [ADR-007](ADR-007-cache.md).

## Alternativas consideradas

- **JMeter o Locust en lugar de k6.** Equivalentes en capacidad. k6 se elige porque los scripts son JavaScript simple versionado junto al código, se ejecuta desde la línea de comandos sin interfaz gráfica y expone umbrales (`thresholds`) que hacen fallar la prueba si no se cumple un objetivo. Descartadas.
- **Probar primero el flujo de compra completo.** Es el flujo más interesante, pero hoy no existe. Esperar retrasaría toda la medición. Se empieza por Comprobantes y se suma el escenario B cuando esté implementado. Descartada como orden.
- **Desplegar en servicios gestionados (Cloud Run + Cloud SQL).** Más robusto y con menos operación, pero unas diez veces más caro para la carga esperada, y requiere un RabbitMQ gestionado aparte. Descartada para la evaluación.
- **Desplegar en un plan gratuito de una plataforma (PaaS).** Muchas suspenden el servicio por inactividad; el worker y el publicador de outbox dejarían de correr y la capacidad publicada no estaría siempre disponible, como exige el enunciado. Descartada.

## Consecuencias

- (+) El escenario A está medido: la API cumple los objetivos con amplio margen y se identificó el principal límite (publicador del outbox, ≈ 45 eventos/s), que tiene una solución de bajo costo.
- (+) Las hipótesis indican qué mirar primero y qué escalar.
- (+) El despliegue cuesta alrededor de USD 6 por mes.
- (−) El flujo de compra (escenario B) y la caché todavía no están medidos.
- (−) Mientras no se ajuste el publicador del outbox, un pico de más de ~45 emisiones por segundo atrasa los correos (sin perderlos).
- (−) La VM es un único punto de falla, y las copias de seguridad y las actualizaciones las operamos nosotros.
- (−) La prueba de carga exige configurar varias API Keys de prueba, o un límite de solicitudes configurable.

## Revisiones

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-10-23 | Se ejecuta el escenario A (A1 a A5) y se registran los resultados. El requisito previo pasa de 50 a **400 API Keys repartidas por solicitud** y, en A4, el reintento usa la misma API Key que la solicitud original. Se agrega a las alternativas de escalado el publicador del outbox, que resultó ser el principal límite. Pasa de Propuesta a Aceptada como primera versión. | Al preparar A3 se vio que 50 claves de 60 por minuto solo permiten 50 solicitudes por segundo: la rampa hasta 300 habría medido el limitador. Además, como la idempotencia es por consumidor, un reintento con otra API Key es una emisión nueva y no prueba la idempotencia. |
