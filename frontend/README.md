# Frontend

Aplicación web (React + Vite + TypeScript) para clientes y administradores. Se comunica **solo con el API Gateway** (`/api`): nunca llama directo a Estudiario ni a Comprobantes.

## Cómo correrlo

```bash
docker compose up -d --build frontend   # desde la raíz → http://localhost:5173

cd frontend
npm install
npm run dev      # http://localhost:5173, con recarga en caliente
npm test         # unitarios y recorrido de la interfaz (Vitest + Testing Library)
npm run build    # verifica tipos y genera dist/
```

**Datos de demostración.** Mientras el gateway, Usuarios y Catálogo no estén, la aplicación usa una API en memoria (`src/api/falso/`) con los mismos formatos, status y `code` que los servicios: reservas de 15 minutos, agotada o retenida, máximo de 6 lugares, compras idempotentes, guías que pasan a moderación y se publican. Arriba de todo aparece la franja "Modo demostración".

- Cuentas: `ana@example.com` / `demo1234` (cliente que ya compró un viaje a Bariloche) y `admin@example.com` / `admin1234`.
- **Simular fallas** (botón abajo a la izquierda): Catálogo caído, servicio de guías caído, compra que queda procesando y reservas de 1 minuto. Sirve para ensayar la jornada de fallas.

**Contra el gateway real:** `VITE_DATOS_FALSOS=false`. En Docker es `FRONTEND_DATOS_FALSOS=false` en el `.env`. nginx deriva `/api` a `FRONTEND_GATEWAY_URL` (por defecto `http://gateway:8080`), sin buffering para el tiempo real y con hasta 12 MB por pedido para los PDF. En desarrollo, `VITE_GATEWAY_URL` indica a dónde va `/api` (por defecto `http://localhost:8080`).

## Pantallas

| Ruta | Pantalla | HU |
|---|---|---|
| `/` | Búsqueda con filtros, orden, paginación y packs con descuento. Los filtros quedan en la URL | HU-01 |
| `/ofertas/:id` | Detalle, disponibilidad (se actualiza cada 15 s), agregar al carrito y guías del destino | HU-02, HU-04, HU-05, HU-14 |
| `/packs/:id` | Armado del pack con validación de tipos y fechas en vivo y precio con descuento | HU-03 |
| `/carrito` | Ítems con cuenta regresiva, ítems vencidos, deshacer al quitar, confirmación de compra | HU-02, HU-04, HU-06 |
| `/compras`, `/compras/:id` | Mis compras y resultado: procesando (consulta cada 3 s), confirmada o rechazada | HU-06, HU-07, HU-08 |
| `/guias`, `/guias/nueva` | Mis guías con su estado y formulario para compartir (solo destinos comprados) | HU-13 |
| `/admin`, `/admin/packs` | Alta, edición y despublicación de ofertas; alta de packs | HU-09, HU-10 |
| `/ingresar` | Ingreso y registro; vuelve a la página donde estaba | HU-12 |
| `/ayuda` | Preguntas frecuentes y atajos de teclado | — |

**Sesión:** el JWT se guarda en `sessionStorage` (se borra al cerrar la pestaña) y se manda como `Authorization: Bearer`. El frontend lee `sub`, `email`, `rol` y `exp` para mostrar el menú, pero no confía en ellos: valida el gateway. Al vencer, o ante un `401`, la sesión se cierra y lleva a ingresar.

**Idempotencia:** cada "Agregar al carrito" lleva su `Idempotency-Key`, que se repite si el usuario reintenta tras un error reintentable. La clave de la compra se guarda hasta tener respuesta: si se corta la conexión y el usuario vuelve a comprar, no se crea una segunda compra.

## Lo que espera de cada servicio

Las rutas son las del plan (§3.1). Los formatos de **Reservas** y de **Catálogo** son los reales: los de Catálogo (oferta, disponibilidad, packs, guías del destino y búsqueda, incluido `orden` con `fecha`, `-fecha`, `precio` y `-precio`) se compararon contra el servicio el 2026-11-10 y coinciden con [`src/api/tipos.ts`](src/api/tipos.ts). Los de **Usuarios** son los que usa el frontend hasta que el servicio esté en `main`. Si cambian, se ajusta ese archivo y [`src/api/real.ts`](src/api/real.ts).

Además de los `code` de la tabla de errores, Catálogo puede responder `503 busqueda-no-disponible` (con `Retry-After`) si cae OpenSearch: el frontend lo trata como cualquier `503`, y el detalle, el carrito y la compra siguen funcionando.

| Ruta | Respuesta que espera |
|---|---|
| `POST /api/usuarios` `{nombre, email, password}` | `201`. `409 email-registrado` si ya existe |
| `POST /api/sesiones` `{email, password}` | `200 {token}`. `401 credenciales-invalidas` |
| `GET /api/busqueda?q&destino&tipo&subtipoTransporte&desde&hasta&precioMin&precioMax&orden&pagina&tamanio` | `{items: Oferta[], pagina, tamanio, total}`. `orden`: `fecha`, `precio` o `-precio` |
| `GET /api/ofertas/{id}` | `Oferta`: `{id, tipo, subtipoTransporte?, titulo, descripcion, destino, origen?, desde, hasta, precio, cupoTotal?, vendidos?}` |
| `GET /api/ofertas/{id}/disponibilidad` | `{ofertaId, estado, libres, proximaLiberacion}` |
| `GET /api/ofertas/{id}/guias` | `{disponible, guias: [{id, titulo, descripcion?, autor, etiquetas, calificacion, cantidadCalificaciones, urlDescarga?}]}`. Con Estudiario caído, `disponible: false` (o cualquier error): se muestra "Guías no disponibles por el momento" |
| `GET /api/packs` · `GET /api/packs/{id}` | `{packs: Pack[]}` · `{id, titulo, destino, descripcion?, descuentoPorcentaje, opciones: Oferta[]}` |
| `POST/PUT/DELETE /api/ofertas…` · `POST /api/packs` | La oferta o el pack. Errores `400 solicitud-invalida` con `errores: [{campo, mensaje}]`, como Reservas |

Errores: `application/problem+json` con `code`. El mensaje se elige por `status` y `code` ([`src/api/errores.ts`](src/api/errores.ts)); nunca se muestra un error técnico.

## Disponibilidad y retenciones

| Estado | Qué se muestra |
|---|---|
| `DISPONIBLE` | "Disponible", los lugares que quedan y "Agregar al carrito" (cantidad limitada a los libres y al máximo de 6) |
| `RETENIDA` | "Sin cupo por ahora": hay lugares reservados en otros carritos que pueden liberarse a partir de `proximaLiberacion`. Botón deshabilitado |
| `AGOTADA` | "Agotada" y una sugerencia; botón deshabilitado |

- **Carrito:** cada ítem muestra la cuenta regresiva hasta `expiraEn` (en ámbar cuando faltan menos de 3 minutos, también en el contador del encabezado). Al llegar a cero, el ítem pasa a vencido sin recargar y ofrece "Volver a agregar". No se puede comprar con ítems vencidos.
- **Al comprar:** `201` confirma, `202` muestra "procesando" y consulta hasta el resultado, y `409` dice cuántos ítems vencieron.
- **Tiempo real (bonus, ADR-015):** todavía no. Mientras tanto, la disponibilidad del detalle se consulta cada 15 s y la cuenta regresiva corre en el navegador. Cuando exista el servicio se suma `EventSource` en el detalle y en el carrito (`TODO(tiempo-real)`).

## Usabilidad

Se siguieron las 10 heurísticas de Nielsen y las 8 reglas de oro de Shneiderman.

| Principio | Cómo se cumple |
|---|---|
| **Visibilidad del estado del sistema** (Nielsen 1) · **Retroalimentación informativa** (Shneiderman 3) | Indicadores de carga en cada pantalla; botones que dicen lo que pasa ("Reservando tu lugar…"); avisos que confirman cada acción; cuenta regresiva de cada reserva; contador del carrito que se pone ámbar si algo está por vencer; compra "procesando" que se actualiza sola; estado de cada guía; pasos de la compra |
| **Relación con el mundo real** (N2) | Español rioplatense, pesos con formato local, fechas legibles, "Sin cupo por ahora" en lugar de `RETENIDA`, "Personas" en lugar de "cantidad" |
| **Control y libertad** (N3) · **Revertir acciones fácilmente** (S6) · **Control del usuario** (S7) | Quitar del carrito se puede deshacer durante 6 s; diálogos con "Volver" y Esc; "Cancelar" en todos los formularios; quitar filtros de a uno o todos; la compra nunca avanza sola sin confirmación |
| **Consistencia y estándares** (N4, S1) | Los mismos componentes para estados, botones, campos y errores en toda la aplicación; colores e íconos fijos por estado; botón principal siempre a la derecha |
| **Prevención de errores** (N5, S5) | Cantidad limitada a los lugares libres y al máximo de 6; pack que no se puede agregar sin una opción de cada tipo ni con fechas incoherentes, explicando por qué; validación del PDF (tipo y 10 MB) antes de subirlo; el destino de la guía se elige de las compras; no se puede comprar con ítems vencidos; confirmación antes de comprar, despublicar o descartar un formulario; claves de idempotencia contra compras duplicadas |
| **Reconocer antes que recordar** (N6) · **Reducir la carga de memoria** (S8) | Destinos sugeridos y búsquedas recientes; filtros aplicados visibles como chips; migas de pan; resumen del pack y del carrito siempre a la vista; volver a la página donde estaba después de ingresar |
| **Flexibilidad y eficiencia** (N7) · **Atajos** (S2) | Tecla `/` para buscar; filtros en la URL (se pueden guardar o compartir); el tipo y el orden se aplican al instante; "Completar" las cuentas de ejemplo; etiquetas con Enter o coma; arrastrar y soltar el PDF |
| **Diseño estético y minimalista** (N8) | Una acción principal por pantalla; filtros avanzados plegados; ayudas plegables en lugar de textos largos |
| **Ayudar a reconocer y recuperarse de errores** (N9) | Mensajes en lenguaje claro que dicen qué pasó y qué hacer ("Esperá 12 segundos", "Volvé a agregar los ítems vencidos"); errores junto al campo y foco en el primero; "Reintentar" solo cuando tiene sentido; una compra rechazada aclara que no se cobró nada |
| **Ayuda y documentación** (N10) | Página de Ayuda con preguntas frecuentes y atajos; ayudas contextuales ("¿Qué pasa al agregar al carrito?", "¿Qué significa cada estado?") |
| **Diálogos que conducen a un cierre** (S4) | Compra en pasos (carrito, confirmación, factura) que termina en una pantalla de resultado con próximos pasos ("Compartir una guía de Bariloche", "Seguir buscando") |
| **Usabilidad universal** (S1, accesibilidad) | Navegación completa con teclado y foco visible; enlace "Saltar al contenido"; el foco va al contenido al cambiar de página; etiquetas y errores asociados a cada campo; avisos anunciados a lectores de pantalla; estados con ícono y texto, no solo color; tema azul oscuro con contraste AA; íconos dibujados en SVG (sin emojis, se ven igual en todos los sistemas); diseño para celular; respeta "reducir movimiento" |

## Estructura

```
src/api/            tipos.ts (formatos), real.ts (gateway), errores.ts (mensajes), falso/ (API de demostración)
src/sesion/         JWT y contexto de sesión
src/carrito/        estado compartido del carrito (contador del encabezado, deshacer)
src/componentes/    marco, avisos, diálogos, estados, cuenta regresiva, campos
src/paginas/        una por pantalla (admin/ para administración)
src/reglas.ts       reglas que se validan antes de enviar (RN-03, RN-04, RN-05, RN-12)
nginx/              configuración de la imagen (deriva /api al gateway)
```

**Historial:**
- 2026-10-22: se agregan los estados de disponibilidad, la cuenta regresiva y el tiempo real; se quitan las sugerencias del carrito.
- 2026-11-07: implementación de todas las pantallas con datos de demostración y contra el gateway, tests y servicio en `docker-compose`.
