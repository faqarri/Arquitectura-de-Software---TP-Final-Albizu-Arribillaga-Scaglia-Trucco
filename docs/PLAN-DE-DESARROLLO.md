# Plan de desarrollo hasta la presentación grupal

Guía de trabajo del grupo desde el **sábado 7 de noviembre** hasta la presentación (miércoles 11 o viernes 13, según el sorteo). Dice qué falta, quién lo hace, en qué orden y cómo saber que está terminado. Se actualiza a medida que avanzamos: tachen lo terminado y anoten los cambios de plan en la última sección.

**Si una decisión de este plan contradice un ADR, vale el ADR.** El plan dice *qué hacer*; los ADR, *cómo y por qué*.

---

## 1. Dónde estamos

| Exigencia de la presentación grupal (enunciado §6) | Estado al 7/11 | Responsable |
|---|---|---|
| Capacidad propia implementada y **desplegada en la nube con URL pública** | Implementada (contrato 1.1.0, con API Keys, outbox, reintentos y DLQ). **Falta desplegarla** | Arribillaga |
| Al menos tres servicios operativos | Solo Comprobantes | Todos |
| Punto de entrada único con autenticación, direccionamiento y control de tráfico | Diseñado (ADR-012), sin código | Trucco |
| Frontend completo, del ingreso hasta la funcionalidad compartida | Sin código | Arribillaga |
| Balanceo sobre 2+ instancias, con evidencia en la observabilidad | Diseñado (Catálogo ×2), sin código | Trucco + Albizu |
| Capacidad del proveedor (Estudiario) en el flujo principal | Contrato recibido, sin código | Scaglia + Albizu |
| Dos tipos de almacenamiento en el flujo | MySQL. MongoDB entra con Catálogo | Albizu |
| Caché en un flujo de lectura, con medición antes y después | Diseñada (ADR-007), sin código | Albizu + Arribillaga |
| Dos estilos de arquitectura reconocibles | Hexagonal (Comprobantes). En capas entra con Catálogo y Usuarios | Albizu + Trucco |
| Evento de dominio con consumidor idempotente y DLQ | Consumidor listo en Comprobantes. Falta que Reservas publique `CompraConfirmada` | Scaglia |
| Búsqueda indexada con paginación, filtros, orden y reconstrucción | Diseñada (ADR-006), sin código | Arribillaga + Albizu |
| Logs, métricas, trazas, tablero y **alerta** | Listos para Comprobantes. Faltan los servicios nuevos y la alerta | Trucco |
| Limitaciones y deuda técnica en ARCHITECTURE.md | Hay sección; actualizarla al final | Arribillaga |
| `docs/POSTMORTEM.md` | No existe (el ensayo previo está en ADR-010) | Arribillaga (con todos) |

---

## 2. Reparto

| Integrante | Es dueño de | Estilo |
|---|---|---|
| **Albizu** | **Catálogo**: ofertas (MongoDB), cupos y retenciones (MySQL), eventos, caché Redis, 2 instancias. Guías del destino en el detalle (consulta a Estudiario) | En capas |
| **Scaglia** | **Reservas**: carrito, compra, mis compras, `CompraConfirmada`. **Adaptador de Estudiario**, compartir guías y test de contrato. Cierre de Comprobantes | Hexagonal |
| **Trucco** | **Usuarios** (registro, login y JWT), **Gateway** (JWT, ruteo, balanceo, rate limit), **observabilidad** de los servicios nuevos y alerta. Tiempo Real si sobra tiempo | En capas / proxy |
| **Arribillaga** | **Frontend**, **búsqueda** (indexador y endpoint), **despliegue** de Comprobantes, pruebas de carga del escenario B, POSTMORTEM y documentación de cierre | — |

Cada uno escribe los tests y actualiza los ADR de lo suyo. **En la defensa individual cada uno defiende el sistema completo**: ver la sección 9.

---

## 3. Paso 0, todos juntos (sábado 7, una reunión de una hora como máximo)

Sin esto, cada uno inventa su propia versión de las interfaces y el lunes nada encaja.

### 3.1 Confirmar las interfaces entre servicios

**JWT** (lo emite Usuarios, lo valida el Gateway):

| Claim | Valor |
|---|---|
| `sub` | Id del usuario (UUID) |
| `email` | Email del usuario |
| `rol` | `cliente` o `admin` |
| `exp` | Vencimiento: 8 horas |

Firmado con HS256 y `JWT_SECRET` (ya está en `.env.example`). El gateway **borra** cualquier `X-Usuario-Id`, `X-Usuario-Email` o `X-Usuario-Rol` que mande el cliente y los vuelve a poner desde los claims. Los servicios **solo confían en esos headers** y no validan el JWT.

**API interna Catálogo → Reservas** (ADR-005 y ADR-014). Albizu escribe el cuerpo exacto de cada operación en `docs/contracts/internos/catalogo-retenciones.yaml` **el sábado**; Scaglia trabaja contra su mock de Prism hasta que Catálogo esté listo.

| Operación | Idempotencia | Respuestas |
|---|---|---|
| `POST /internal/retenciones` `{ofertaId, cantidad, carritoId}` | `Idempotency-Key` por ítem | `201 {retencionId, expiraEn}` · `409 AGOTADA` / `409 RETENIDA {proximaLiberacion}` · `422` (pasa el máximo de 6) |
| `DELETE /internal/retenciones/{id}` | Naturalmente idempotente | `204` siempre |
| `POST /internal/retenciones/confirmacion` `{retencionIds, compraId}` | `Idempotency-Key = compraId` | `200 {items con precio vigente}` · `409 RETENCION_NO_VIGENTE {retencionIds vencidas}` |

**Rutas públicas del gateway** (las usa el frontend; el gateway agrega `/api`):

| Ruta | Servicio | Autenticación | HU |
|---|---|---|---|
| `POST /api/usuarios` · `POST /api/sesiones` | Usuarios | No | HU-12 |
| `GET /api/busqueda?destino&tipo&subtipoTransporte&desde&hasta&precioMin&precioMax&q&orden&pagina&tamanio` | Catálogo | No | HU-01 |
| `GET /api/ofertas/{id}` · `GET /api/ofertas/{id}/disponibilidad` · `GET /api/ofertas/{id}/guias` | Catálogo | No | HU-01, HU-05, HU-14 |
| `POST /api/ofertas` · `PUT /api/ofertas/{id}` · `DELETE /api/ofertas/{id}` | Catálogo | `admin` | HU-09 |
| `POST /api/packs` · `GET /api/packs/{id}` | Catálogo | `admin` / No | HU-03, HU-10 |
| `GET /api/carrito` · `POST /api/carrito/items` · `DELETE /api/carrito/items/{id}` | Reservas | `cliente` | HU-02, HU-04 |
| `POST /api/compras` · `GET /api/compras` · `GET /api/compras/{id}` | Reservas | `cliente` | HU-06, HU-08 |
| `POST /api/guias` (multipart con PDF) · `GET /api/guias` | Reservas | `cliente` | HU-13 |
| `GET /api/tiempo-real/ofertas?ids=…` · `GET /api/tiempo-real/mis-retenciones` (SSE) | Tiempo Real | No / `cliente` | HU-04, HU-05 (bonus) |

Cada dueño publica antes del domingo un ejemplo de respuesta de sus rutas en el README de su servicio, así Arribillaga arma las pantallas con datos falsos idénticos a los reales.

**Eventos (RabbitMQ):**

| Evento | Publica | Consume | Formato |
|---|---|---|---|
| `OfertaActualizada` | Catálogo | Indexador (búsqueda) e invalidación de caché | `{eventId, ofertaId, version}` (ADR-006) |
| `DisponibilidadCambiada` | Catálogo | Tiempo Real | ADR-015 |
| `RetencionVencida` | Catálogo | Reservas y Tiempo Real | `{eventId, retencionId, ofertaId, carritoId}` (ADR-015) |
| `CompraConfirmada` | Reservas | Comprobantes | **Ya fijado**: README de Comprobantes, sección *Mensajes* |

### 3.2 Recortes de alcance

Para que entre en el tiempo:
- **Packs (HU-03 y HU-10) mínimos:** el admin crea un pack con opciones y el cliente lo agrega como varias retenciones de una sola vez (todo o nada). El descuento es un porcentaje fijo.
- **Tiempo Real (bonus, ADR-015):** solo si el lunes a la noche el flujo principal anda de punta a punta. Es bonus: no hacerlo no resta de la nota base. Si no se hace, se avisa en el README.
- Todo lo recortado va a la sección de limitaciones de `docs/ARCHITECTURE.md`.

### 3.3 Proveedor: Estudiario (Grupo 2)

Ya nos mandaron contrato y guía ([README](https://github.com/MichiSil/ProyectoEstudiario/blob/main/docs/contracts/README.md), [documents-v1.yaml](https://github.com/MichiSil/ProyectoEstudiario/blob/main/docs/contracts/documents-v1.yaml)). Ese mismo día:
- Responderles quién es el referente y qué operaciones usamos (mensaje redactado en el chat del grupo).
- Pedirles la URL pública y la API Key (por privado; va en el `.env`, nunca en el repo).
- Coordinar que tengan **guías ya aprobadas** con `category` de destinos nuestros para la demo, y que alguien de su grupo modere durante la presentación.

### 3.4 Historias nuevas para SPEC.md (Scaglia)

- **HU-13 Compartir una guía de viaje:** como cliente que compró un viaje, quiero subir una guía en PDF de ese destino para compartirla con otros viajeros. Solo destinos con una compra `CONFIRMADA`; PDF de hasta 10 MB; veo si quedó pendiente, publicada o rechazada (con motivo).
- **HU-14 Ver guías del destino:** como cliente, en el detalle de una oferta quiero ver las guías mejor calificadas de ese destino. Si el servicio de guías no responde, el detalle se ve igual, con el aviso "Guías no disponibles por el momento".

---

## 4. Trabajo de cada integrante, paso a paso

Convenciones para todos (las mismas de Comprobantes): un módulo Go por servicio (`services/<servicio>/go.mod`, módulo `agencia/<servicio>`, `replace agencia/pkg => ../../pkg`), logs y trazas con `pkg/observabilidad`, `GET /health` en español sin autenticación, `GET /metrics`, usuario de MySQL propio por servicio, Dockerfile construido desde la raíz y un servicio en `docker-compose.yml`. **Copien la estructura de `services/comprobantes`** en vez de empezar de cero.

### 4.1 Albizu: Catálogo

1. **Esqueleto (sábado).**
   - `services/catalogo` en capas: `handlers/`, `services/`, `repository/`.
   - Conexión a MongoDB (ofertas) y a MySQL (esquema `catalogo`: inventario y retenciones).
   - `/health` y `/metrics`.
   - Agregarlo a `docker-compose.yml` como `catalogo-1` y `catalogo-2`, la misma imagen con distinto `INSTANCIA_ID`.
   - Publicar el contrato interno de retenciones (3.1).
2. **Ofertas (HU-09).**
   - Alta, modificación y baja lógica de transportes, estadías y excursiones en MongoDB, solo con `X-Usuario-Rol: admin`.
   - Detalle público `GET /api/ofertas/{id}`.
   - Cada escritura deja `OfertaActualizada` en el outbox (copiar el patrón del paquete `rabbit` de Comprobantes).
   - **Regla:** el cupo total no puede quedar por debajo de los vendidos.
3. **Cupos y retenciones (HU-04 y HU-05, la operación crítica de la defensa).**
   - Las tres operaciones internas, idempotentes.
   - Bloqueo **primero del inventario** en orden de `oferta_id` y **después** de las retenciones, con `SELECT … FOR UPDATE` (ADR-014).
   - Máximo de 6 cupos por oferta y carrito (RN-12).
   - Barrido de vencimientos con `FOR UPDATE SKIP LOCKED` cada `RETENCION_BARRIDO_SEGUNDOS`. Publica `RetencionVencida` y `DisponibilidadCambiada`.
   - `GET /api/ofertas/{id}/disponibilidad` → `DISPONIBLE` / `RETENIDA {proximaLiberacion}` / `AGOTADA`. **Nunca se cachea.**
4. **Packs mínimos (HU-03 y HU-10).** Un pack en MongoDB con sus opciones. Al retener un pack se retienen todas las opciones en una sola transacción (todo o nada, RN-07). Fechas coherentes (RN-04).
5. **Caché Redis del detalle (ADR-007).**
   - Cache-aside con la clave `oferta:v1:{id}` y TTL de 10 minutos con jitter.
   - Se borra al escribir y otra vez al consumir `OfertaActualizada`.
   - Timeout de 50 ms y corte de 30 s después de 5 errores seguidos.
   - Métricas `cache_consultas_total{resultado="acierto|fallo"}` y `cache_errores_total`.
   - Agregar `redis:7` a `docker-compose.yml` con `maxmemory 128mb` y `allkeys-lru`.
6. **Guías del destino (HU-14, consulta a Estudiario).**
   - `GET /api/ofertas/{id}/guias` → `GET /v1/documents?category=<destino>&sort=rating&size=5` a través del adaptador de Estudiario. Scaglia lo arma como paquete compartido (4.2, paso 5); Albizu lo usa.
   - Cachear la respuesta por destino 5 minutos en Redis, para no gastar el cupo de 60 por minuto.
   - Si falla o el circuito está abierto, responde `200 {"disponible": false, "guias": []}`: el detalle nunca falla por Estudiario.
7. **Tests.**
   - Unitarios de las reglas de cupo y de pack.
   - **De integración contra MySQL real** (testcontainers, como en Comprobantes), incluido el de **concurrencia**: 20 goroutines intentan retener el último cupo y exactamente una gana.
   - Uno de integración contra MongoDB.
8. **Documentación.** Revisiones en ADR-014 (implementado, resultado del test de concurrencia) y ADR-007 (medición de la caché que entrega Arribillaga). README de `services/catalogo`.

**Terminado cuando:** con `docker compose up`, dos instancias sanas; crear una oferta, retenerla dos veces en paralelo sobre el último cupo da `201` a una y `409` a la otra; la retención vence sola a los 15 minutos (con `RETENCION_MINUTOS=1` para probar).

### 4.2 Scaglia: Reservas, Estudiario y cierre de Comprobantes

1. **Esqueleto (sábado).** `services/reservas` hexagonal, con la misma estructura de carpetas que Comprobantes:
   - Puerto `RetencionesCatalogo`, implementado contra el **mock de Prism** del contrato interno de Albizu.
   - Puerto `RepositorioCompras` sobre MySQL (esquema `reservas`).
   - Outbox y publicador: copiar el paquete `rabbit` de Comprobantes.
2. **Carrito (HU-02 y HU-04).**
   - Agregar un ítem llama a `POST /internal/retenciones` y quitarlo, a `DELETE`.
   - Timeout de 2 s por intento, 2 reintentos con backoff de 200 y 400 ms con jitter, y `Idempotency-Key` por ítem (ADR-005).
   - Errores de negocio (`409`, `422`): no se reintentan y se informan. 5xx o timeout al agregar: `503` "no pudimos reservar tu lugar".
   - Consumir `RetencionVencida` (cola durable `reservas.retenciones` con DLQ) y marcar el ítem como vencido.
3. **Compra (HU-06 y HU-08).**
   - `POST /api/compras` crea la compra `PENDIENTE` y llama a `/internal/retenciones/confirmacion` con `Idempotency-Key = compraId`:
     - `200` → `CONFIRMADA` y, **en la misma transacción**, `CompraConfirmada` en el outbox, con `email` de `X-Usuario-Email` e `items` con descripción y precio vigente.
     - `409` → `RECHAZADA`, informando qué ítem venció.
     - Sin respuesta concluyente → queda `PENDIENTE` y un proceso de recuperación reintenta con la misma clave.
   - Reintentar el mismo carrito no crea otra compra.
   - `GET /api/compras` y `/{id}`, solo las del usuario.
   - **Con esto, el flujo compra → `CompraConfirmada` → factura → correo queda de punta a punta.**
4. **Adaptador de Estudiario (D9). Hacerlo temprano: lo usan Reservas y Catálogo.**
   - Paquete Go reutilizable, por ejemplo `pkg/estudiario`, o uno dentro de cada servicio si prefieren no compartir. Expone **nuestros** términos: `BuscarGuias(destino) []Guia`, `RegistrarGuia(...) (id, estado)`, `EstadoDeGuias(...)`. Ningún tipo ni código de Estudiario sale del paquete.
   - Timeout de 5 s para leer y 20 s para registrar. Hasta 3 reintentos (1, 2 y 4 s) **solo** ante error de red, timeout, `409`, `429` (respetando `Retry-After`) o `503`. Al registrar, siempre con la **misma** `Idempotency-Key`.
   - **Circuit breaker:** se abre con 5 fallos seguidos, queda abierto 30 s y después hace una llamada de prueba (ADR-009).
   - Errores traducidos a los nuestros: `NoDisponible`, `Rechazado` y `RespuestaInvalida`. Decidir siempre por `status` y `code`, nunca por `title` o `detail`.
   - **Lector tolerante:** ignorar campos desconocidos y aceptar valores nuevos de `status` y `source`.
   - Mandar `X-Correlation-Id` con nuestro `correlacion_id`.
   - Configuración: `ESTUDIARIO_URL` y `ESTUDIARIO_API_KEY` en `.env` y en `.env.example` vacía.
5. **Compartir guías (HU-13).**
   - `POST /api/guias` (multipart: título, PDF, descripción y etiquetas). Valida que el usuario tenga una compra `CONFIRMADA` de ese destino; si no, `403`.
   - Guarda la guía localmente como `POR_ENVIAR`, con su `Idempotency-Key` (UUID), y la envía a Estudiario con `authorExternalId = id del usuario` y `category = destino`.
   - Si Estudiario no responde, queda `POR_ENVIAR` y un proceso la reintenta (mismo patrón que el outbox).
   - Cada 5 minutos consulta `GET /v1/submissions` y actualiza a `PENDIENTE`, `PUBLICADA` o `RECHAZADA {motivo}`.
   - `GET /api/guias` → las del usuario, con su estado.
6. **Test de contrato (exigido en la defensa).**
   - Copiar el contrato a `docs/contracts/consumidos/estudiario/1.0.0/documents-v1.yaml`.
   - En `go test`: cargar el YAML con `github.com/getkin/kin-openapi`, levantar su mock de Prism (testcontainers) y validar contra el esquema **nuestras solicitudes y sus respuestas**: éxito, `404`, `422`, `429` y `503`, forzados con `Prefer`.
   - Otro test compara la versión mayor del YAML copiado con la del publicado: si cambia, falla.
7. **Cierre de Comprobantes** (hallazgo del ensayo en ADR-010): `restart: on-failure`, healthcheck de MySQL por TCP (`mysqladmin ping -h 127.0.0.1`) y reintentos de conexión al iniciar. Pasarle a Arribillaga la lista de servicios y variables para el despliegue.
8. **Tests.** Unitarios de los estados de la compra y del carrito. De integración contra MySQL y RabbitMQ (testcontainers): confirmar publica exactamente un `CompraConfirmada`.
9. **Documentación.**
   - ADR-009: completar los `[A COMPLETAR]` con Estudiario, las dos operaciones, qué ve el usuario y el test de contrato. Pasarlo a "Aceptada".
   - SPEC.md: HU-13 y HU-14.
   - README de `services/reservas`.

**Terminado cuando:** desde Postman, a través del gateway, agregar al carrito, comprar y que llegue el correo con la factura; subir una guía y ver su estado; con `ESTUDIARIO_URL` apuntando a un puerto apagado, la compra sigue funcionando y la guía queda `POR_ENVIAR`.

### 4.3 Trucco: Usuarios, Gateway y observabilidad

1. **Usuarios (sábado y domingo), en capas** (HU-12).
   - `POST /api/usuarios` registra con contraseña en **bcrypt** (RN-10: nunca se devuelve ni se guarda en claro).
   - `POST /api/sesiones` emite el JWT acordado en 3.1.
   - Un usuario `admin` inicial por variable de entorno, para la demo.
   - Tests unitarios y uno de integración contra MySQL.
2. **Gateway en Go** (ADR-012), en `gateway/`, puerto 8080:
   - Valida el JWT y borra y vuelve a poner los `X-Usuario-*`. Controla el rol por ruta (tabla de 3.1).
   - Ruteo según la tabla de 3.1. **Comprobantes no pasa por el gateway** (ADR-008).
   - **Balanceo round robin** entre `catalogo-1` y `catalogo-2`: health check activo cada 5 s con timeout de 1 s; sale del pool a las 3 fallas y vuelve a las 2 respuestas sanas.
   - Rate limit por usuario o IP. SSE sin buffering (si entra Tiempo Real).
   - Propaga `X-Correlation-Id` y `traceparent`.
   - Métricas `gateway_solicitudes_total{instancia, ruta, estado}` y `gateway_instancia_sana{instancia}`.
3. **Observabilidad de todo el sistema** (ADR-011).
   - Prometheus scrapea Usuarios, Catálogo ×2, Reservas y Gateway.
   - En el tablero **Agencia de Viajes - Observabilidad**: un panel de solicitudes por instancia de Catálogo (la evidencia del balanceo), uno de salud de cada instancia, el hit rate de la caché, las compras por estado y el estado del circuito de Estudiario.
   - **Alertas en Grafana**, exigidas para la presentación: al menos tasa de 5xx > 5 % durante 1 minuto e instancia de Catálogo fuera del pool.
   - Completar ADR-011 (objetivo de servicio y criterios de alerta) y ADR-012 (pasa a "Aceptada", con la evidencia de apagar `catalogo-2`).
4. **Bonus Tiempo Real (ADR-015), solo con el flujo principal terminado.** `services/tiempo-real`, sin estado: consume el fanout `catalogo.disponibilidad` con una cola exclusiva por instancia y reenvía por SSE a través del gateway.

**Terminado cuando:** con el frontend, registro, login y navegación como `cliente` y como `admin`; `docker compose stop catalogo-2` y el tablero muestra que deja de recibir tráfico y la alerta se dispara; al levantarla, vuelve al pool.

### 4.4 Arribillaga: Frontend, búsqueda, despliegue y cierre

1. **Despliegue de Comprobantes (sábado y domingo, lo antes posible: el otro grupo lo necesita).**
   - Una VM chica en la nube (Oracle Cloud Free Tier, DigitalOcean o AWS Lightsail) con Docker y un `docker-compose` reducido: MySQL, RabbitMQ y Comprobantes, con el correo por Gmail.
   - El `.env` del servidor con las variables de Gmail y contraseñas propias (nunca en el repo). HTTPS (por ejemplo, Caddy delante) y abrir solo el puerto de la API.
   - Reemplazar `<COMPLETAR: URL pública>` en `docs/contracts/comprobantes/v1/openapi.yaml`, en `docs/contracts/README.md` y en la guía de integración, y completar el contacto.
   - Documentar el procedimiento en `docs/DESPLIEGUE.md` y la URL en el README.
   - Mandarle al grupo consumidor la guía y la URL. Probar desde afuera: `POST /v1/api-keys`, emitir y que llegue el correo.
2. **Frontend React + Vite** (`frontend/`, servido en `docker-compose`). Primero con datos falsos (los ejemplos de 3.1), después contra el gateway:
   - Registro y login, con el JWT guardado en memoria o `sessionStorage`.
   - **Búsqueda** con filtros, orden y paginación (HU-01).
   - **Detalle de oferta**: disponibilidad (HU-05), botón de agregar al carrito y **guías del destino** (HU-14, con el aviso si no están disponibles).
   - **Carrito** con la cuenta regresiva de cada retención y los ítems vencidos (HU-02 y HU-04).
   - **Compra** con los estados *procesando*, *confirmada* y *rechazada con el ítem vencido* (HU-06). **Mis compras** con el estado del comprobante (HU-08).
   - **Compartir guía** y **Mis guías** con su estado (HU-13).
   - Admin: alta y edición de ofertas y packs (HU-09 y HU-10).
   - Mensajes claros para `503`, `409` y `429`. El frontend **nunca** llama a Estudiario ni a Comprobantes directo: todo por el gateway.
3. **Búsqueda (ADR-006), coordinando con Albizu** (componente dentro de Catálogo o servicio aparte, como acuerden el sábado):
   - Índice `ofertas` en OpenSearch detrás del alias, con el mapeo de ADR-006.
   - **Indexador:** consume `OfertaActualizada`, lee la oferta de MongoDB e indexa solo si la versión es más nueva.
   - `GET /api/busqueda` con filtros en contexto `filter`, orden por precio o fecha (desempate por `id`), paginación de 20 por defecto, máximo 50 por página y 50 páginas.
   - `POST /internal/busqueda/reconstruccion`: índice nuevo con todo MongoDB y cambio de alias.
   - Medir el retraso real entre una escritura y su aparición en la búsqueda contra los 5 s de ADR-006.
4. **Pruebas de carga del escenario B** (ADR-013, k6 en `tests/carga/`):
   - Búsqueda y detalle **con y sin caché** (la evidencia que pide D7: comparar latencia y carga en MongoDB).
   - El flujo de carrito y compra con concurrencia sobre pocas ofertas.
   - **Contra el mock de Estudiario, no contra su servicio real** (cupo de 60 por minuto).
   - Anotar el límite principal, la alternativa de escalado y los costos en ADR-013.
5. **Documentación de cierre (martes):**
   - README: cómo se levanta todo con `cp .env.example .env && docker compose up -d --build`, la URL desplegada y dónde está cada cosa.
   - `docs/ARCHITECTURE.md`: servicios nuevos en los diagramas y la sección de **limitaciones y deuda técnica** actualizada, con los recortes de 3.2.
   - `docs/POSTMORTEM.md` (sección 6).
   - `docs/devoluciones/` si hubo devolución de la Entrega 2.

---

## 5. Orden de integración (puntos de control)

| Cuándo | Punto de control | Cómo se verifica |
|---|---|---|
| **Sáb 7 noche** | Interfaces de 3.1 en el repo. Esqueletos levantando con `/health`. Mensaje a Estudiario enviado | `docker compose up -d --build` y todos los `/health` en `ok` |
| **Dom 8 noche** | Cada servicio funciona aislado (Usuarios y JWT, Catálogo con cupos, Reservas contra el mock de Catálogo, frontend con datos falsos). **Comprobantes desplegado con URL pública** | Postman contra cada servicio. Alguien de afuera registra una API Key en la URL pública |
| **Lun 9 noche** | **Flujo principal de punta a punta desde el frontend:** login, búsqueda, detalle con guías, carrito, compra, correo con la factura. Balanceo andando | Una persona que no lo programó lo recorre entero sin ayuda |
| **Mar 10** | Caché medida, alerta, pruebas de carga, ensayo de caídas, POSTMORTEM y documentación. Tiempo Real si hay margen. **`main` congelado a la noche** | Clonar `main` en una carpeta limpia, seguir el README y que todo levante |
| **Mié 11** | Presentación (si es el 13, el 11 y el 12 son de margen y de bonus) | — |

Reglas para que la integración no explote:
- **Pull Requests chicos y diarios** a `main`, con una revisión de otro integrante. Nada de un PR gigante el martes.
- Antes de empezar cada día: `git switch <tu-rama>` y `git merge main`.
- Si algo de 3.1 tiene que cambiar, se avisa en el chat **antes** de cambiarlo y se actualiza este documento.

---

## 6. Jornada de fallas controladas y `docs/POSTMORTEM.md`

El martes, **todos juntos**, con el tablero de Grafana abierto. Se ensayan y se graban (capturas o video) estas caídas:

| Caída provocada | Comando | Comportamiento esperado | Dueño |
|---|---|---|---|
| Una instancia de Catálogo | `docker compose stop catalogo-2` | El gateway la saca del pool en unos 15 s, el tráfico va a `catalogo-1`, salta la alerta y vuelve sola al levantarla | Trucco |
| El proveedor (Estudiario) | `ESTUDIARIO_URL` a un puerto apagado, o `Prefer: code=503` contra su mock | El circuito se abre. El detalle muestra "Guías no disponibles"; compartir una guía queda `POR_ENVIAR` y se envía cuando vuelve | Scaglia |
| El correo | `docker compose stop mailpit` (o credenciales SMTP inválidas) | Comprobantes reintenta (5, 15 y 30 s), termina `FALLIDO` y el mensaje queda en la DLQ; la compra sigue `CONFIRMADA` (ya ensayado en ADR-010) | Scaglia |
| Redis | `docker compose stop redis` | Catálogo sigue respondiendo desde MongoDB, sube `cache_errores_total` y el hit rate cae a 0 | Albizu |

Arribillaga redacta `docs/POSTMORTEM.md`: impacto, línea de tiempo (con horas reales), detección (qué alerta o panel lo mostró), respuesta, causa y acciones de mejora. Cada dueño le pasa los datos de su caída.

---

## 7. Checklist contra el enunciado (para revisar el martes)

| Enunciado | Dónde se ve | ✔ |
|---|---|---|
| 3+ microservicios y api gateway (§2.1) | Usuarios, Catálogo, Reservas, Comprobantes y Gateway en `docker-compose.yml` | ☒ |
| Dos estilos de arquitectura (§2.1) | Hexagonal: Comprobantes, Reservas y Usuarios. En capas: Catálogo (ADR-002) | ☒ |
| Frontend completo (§2.2) | `frontend/`, del login hasta la compra y las guías | ☒ |
| Balanceo con 2+ instancias, detección de caídas y evidencia (§2.3) | Gateway + `catalogo-1`/`catalogo-2` + panel por instancia; [ensayo integrado](./ensayos/2026-11-10-balanceo-gateway.md) | ☒ |
| Comunicación síncrona con timeout, reintentos y respuestas tardías (§2.5) | Reservas → Catálogo (ADR-005), Catálogo/Reservas → Estudiario (ADR-009) | ☒ |
| Evento de dominio y mensajes fallidos (§2.5) | `CompraConfirmada` y `EnvioSolicitado` con DLQ | ☒ |
| Búsqueda con paginación, filtros, orden y retraso definido (§2.6) | `GET /api/busqueda`, ADR-006 | ☒ |
| Caché con métricas de impacto (§2.7) | Redis + panel de hit rate + medición de ADR-007 | ☒ |
| Relacional + no relacional, y operación con garantías de consistencia (§2.8) | MySQL + MongoDB. Retenciones y confirmación (ADR-014). Idempotencia de Comprobantes | ☒ |
| Mecanismo de protección y caída en POSTMORTEM (§2.9) | Circuit breaker, timeouts, reintentos, outbox y DLQ. `docs/POSTMORTEM.md` | ☒ |
| Logs estructurados, métricas, trazas, tablero y alerta (§2.10) | Grafana, Jaeger y Prometheus | ☒ |
| Tests unitarios, de integración contra dependencia real y de carga (§2.11) | `go test ./...` por servicio, testcontainers y `tests/carga/` | ☒ |
| Repo público, rama principal, procedimiento único, secretos fuera (§2.12) | `main`, README, `.env.example` sin secretos reales | ☒ |
| Capacidad propia desplegada con URL pública (§4.1 y §6) | `docs/contracts/` con la URL real | ☒ |
| Capacidad del proveedor en el flujo principal, desde el microservicio (§4.1) | Guías en Catálogo y Reservas, nunca desde el frontend | ☒ |
| Test de contrato del proveedor (§4.1, defensa) | `go test` en Reservas con kin-openapi + Prism | ☒ |
| ADR D1 a D13 al día (§5) | `docs/adr/` | ☒ |
| Limitaciones y deuda técnica (§6) | `docs/ARCHITECTURE.md` | ☒ |
| Bonus: actualizaciones en tiempo real | `services/tiempo-real` (SSE por el gateway), ADR-015 | ☒ |

---

## 8. Configuración y secretos

- Toda variable nueva va a **`.env.example` sin valores reales** (`cambiar` o vacía) y al servicio en `docker-compose.yml`. Avisar en el chat, porque todos tocan esos dos archivos.
- **Nunca** van al repo: la contraseña de aplicación de Gmail, la API Key de Estudiario, el `JWT_SECRET` real ni el `.env` del servidor en la nube.
- Variables nuevas previstas: `ESTUDIARIO_URL`, `ESTUDIARIO_API_KEY`, `REDIS_URL`, `OPENSEARCH_URL`, `MONGO_URL`, `INSTANCIA_ID`, `ADMIN_EMAIL` y `ADMIN_PASSWORD` (usuario admin inicial).

---

## 9. Preparación para la defensa individual

Cada uno defiende **todo** el sistema. El martes a la noche, o el día que quede libre, cada dueño explica su parte a los otros tres (15 minutos cada uno) con este foco:

| Tema que más van a preguntar | Lo explica |
|---|---|
| La operación crítica: retenciones concurrentes, orden de bloqueo, `SKIP LOCKED` y el test de concurrencia | Albizu |
| Idempotencia de punta a punta: `Idempotency-Key`, outbox, consumidor idempotente y DLQ | Scaglia |
| Integración con el proveedor: adaptador, circuit breaker, qué ve el usuario y test de contrato | Scaglia |
| Balanceo: health checks, qué pasa con una instancia caída, cómo se ve en el tablero | Trucco |
| Observabilidad: seguir una compra con `correlacion_id` y `trace_id`, y la alerta | Trucco |
| Búsqueda y caché: retraso del índice, invalidación y medición antes y después | Arribillaga y Albizu |
| Pruebas de carga: límite principal, escalado y costos | Arribillaga |

---

## 10. Cambios a este plan

| Fecha | Cambio | Motivo |
|---|---|---|
| 2026-11-07 | Versión inicial | Reparto del trabajo pendiente para la presentación grupal, con el proveedor ya asignado (Estudiario, Grupo 2) |
| 2026-11-07 | Avance de Scaglia: Reservas implementado (carrito, compra con recuperación, `CompraConfirmada`, `RetencionVencida`, guías), adaptador de Estudiario con test de contrato (`pkg/estudiario`), `pkg/mensajeria` compartido y arreglo del arranque en frío. El contrato interno de retenciones (paso 3.1) lo redactó Scaglia como borrador en `docs/contracts/internos/`: **Albizu lo tiene que confirmar o corregir**. | Reservas no podía avanzar sin ese contrato. |
| 2026-11-07 | Avance del frontend (lo empezó Scaglia; sigue a cargo de Arribillaga): todas las pantallas de 4.4 paso 2 en `frontend/`, con datos de demostración que respetan los formatos de Reservas y un panel para simular fallas. Ya está en `docker-compose` (`http://localhost:5173`). Los formatos que espera de Catálogo y Usuarios están en `frontend/README.md`: **Albizu y quien haga Usuarios los confirman o se ajusta el frontend**. Falta conectarlo al gateway (`FRONTEND_DATOS_FALSOS=false`) y el tiempo real. | Adelantar la parte visible mientras se terminan los servicios. |
| 2026-11-09 | Avance de Albizu: **Catálogo implementado** (`services/catalogo`, en capas): ofertas y packs en MongoDB con `OfertaActualizada`; cupos, retenciones, confirmación y barrido en MySQL con la regla de bloqueo de ADR-014; caché Redis del detalle (ADR-007); guías de Estudiario con caché por destino; `/health`, `/metrics` y header `X-Instancia`. En `docker-compose` como `catalogo-1` y `catalogo-2` (alias de red `catalogo`), más `redis`; Reservas ya usa el Catálogo real y Prometheus mide las dos instancias. Contrato interno confirmado en 1.0.0. Tests: unitarios de reglas, regla de capas, integración contra MySQL (incluido el de concurrencia: 20 clientes, gana uno) y contra MongoDB. **Pendiente para otros:** el indexador y `GET /api/busqueda` (Arribillaga; la cola `catalogo.indexacion` ya recibe los cambios), las rutas de Catálogo en el gateway con balanceo entre las dos instancias (Trucco) y los paneles por instancia y de caché en Grafana. | Paso 4.1 del plan. |
| 2026-11-09 | Avance de Trucco: Usuarios con bcrypt/JWT, integración MySQL, health/métricas y Gateway Go con autorización, ruteo, rate limit, correlación y protección de headers. Pool Catálogo round robin con health checks (5 s, 1 s timeout, 3 fallas para retirar, 2 éxitos para reincorporar), paneles y reglas de alertas implementados y cubiertos con pruebas locales. Falta integrar `catalogo-1`/`catalogo-2` de Albizu y demostrar caída/recuperación con evidencia; el frontend continúa con datos falsos hasta completar Catálogo. | Registrar el progreso verificable y sus dependencias pendientes. |
| 2026-11-10 | Integración de Catálogo en la rama Trucco: `catalogo-1` y `catalogo-2` quedan en Compose, Gateway recibe sus URLs, Prometheus scrapea las instancias y Grafana combina sus métricas con las del pool. El frontend sigue en modo demostración hasta que estén listas las rutas de búsqueda y el indexador. Falta ensayar la caída y recuperación de una instancia y verificar las alertas en vivo. | Continuar la parte de Trucco sobre la integración entregada por Albizu y dejar explícitos los pasos de validación restantes. |
| 2026-11-10 | Ensayo integrado de Trucco: seis requests alternaron entre las dos instancias; al detener `catalogo-2`, el Gateway siguió respondiendo desde `catalogo-1`, la alerta se disparó y la instancia volvió saludable al reiniciarla. Se registra la evidencia en `docs/ensayos/2026-11-10-balanceo-gateway.md` y ADR-012 pasa a Aceptada. | Cerrar el criterio de balanceo con evidencia local reproducible. |
| 2026-11-10 | Avance de Arribillaga. **Búsqueda (4.4 paso 3) terminada** dentro de Catálogo: indexador de `catalogo.indexacion`, `GET /api/busqueda` y `POST /internal/busqueda/reconstruccion`, con tests unitarios y de integración contra OpenSearch. Retraso medido: p95 1,1 s (ADR-006). **Pruebas de carga (paso 4) hechas:** `tests/carga/catalogo.js` y `compras.js`; caché antes y después (90 % de aciertos, p95 −32 %, cumple ADR-007), B1 (112 compras/s sin errores), B2 (50 de 50 cupos, sin sobreventa); límites, escalado y diagnóstico corregido del outbox en ADR-013. **Despliegue (paso 1) preparado:** `deploy/comprobantes/` (compose reducido + Caddy con HTTPS) y `docs/DESPLIEGUE.md`; **falta crear la VM** y reemplazar los `<COMPLETAR: URL pública>`. **Frontend (paso 2):** formatos de Catálogo confirmados contra el servicio real; falta conectarlo al gateway. **Cierre (paso 5):** README (se resolvió un conflicto de merge que había quedado commiteado), limitaciones de ARCHITECTURE.md y `docs/POSTMORTEM.md`, con dos incidentes reales; faltan las caídas de la jornada grupal (catalogo-2 y Estudiario). Además, Jaeger limitado a 20 000 trazas en memoria (sin límite agotó la memoria en la prueba de carga). **Pendiente para otros:** paneles y alertas de búsqueda (p95 del retraso, mensajes en `catalogo.indexacion.dlq`) en el tablero (Trucco); la ruta `GET /api/busqueda` en el gateway, balanceada entre las dos instancias (Trucco). | Pasos 4.4.1 a 4.4.5 del plan. |
| 2026-11-10 | Cierre (hecho por Scaglia con ayuda, cubriendo pendientes de todo el grupo). **Despliegue:** API de Comprobantes en Azure (`https://68-211-124-251.sslip.io`), verificada de punta a punta y con la URL en contrato, guía y README. **Ensayos de caída con tiempos medidos** (POSTMORTEM #4 y #5, cerrados): se corrigieron dos fallas que veía el cliente, los `502` del gateway mientras una instancia de Catálogo caída seguía en el pool (ahora reintenta las lecturas en otra instancia) y el `502` al compartir una guía con Estudiario caído (ahora responde `POR_ENVIAR` en 8 s). **Alerta de 5xx** validada en vivo y ampliada a lo que ve el cliente en el gateway; ADR-011 pasa a Aceptada. **Frontend** contra los servicios reales por defecto y **catálogo de demostración** cargado por el servicio `semilla`. **Bonus Tiempo Real implementado** (servicio, gateway y frontend), verificado con los dos eventos. Arreglos menores: valor por defecto de las instancias de Catálogo en el gateway, tests de Usuarios dependientes de la fecha. Checklist del enunciado completo. | Dejar `main` listo para la evaluación. |
