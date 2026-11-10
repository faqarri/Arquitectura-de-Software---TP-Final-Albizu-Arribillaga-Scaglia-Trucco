# Guía de integración: API de Emisión de Comprobantes

Esta guía es para el grupo que va a usar nuestra API desde su proyecto. Explica cómo obtener la API Key, dónde ubicar cada dato y cómo hacer las llamadas.

**Qué hace la API:** le mandás el email de tu cliente y los ítems que vendió (descripción y precio). Nosotros generamos la factura en PDF y **siempre** se la enviamos por correo a ese email. Tu sistema no necesita generar PDFs ni tener un servidor de correo.

Contrato formal: [openapi.yaml](openapi.yaml) (versión 1.1.0). Resumen de reglas y errores: [README de contratos](../../README.md).

---

## Qué datos necesitás y dónde va cada uno

| Dato | Valor | Quién lo da | Dónde lo ubicás en tu proyecto |
|---|---|---|---|
| URL base | `<COMPLETAR: URL pública de la API>` | Nosotros (la publicamos acá y en el [README de contratos](../../README.md)) | Variable de entorno `COMPROBANTES_URL` |
| API Key | Empieza con `cmp_` | **Ustedes la generan** (paso 1) y les llega por correo | Variable de entorno `COMPROBANTES_API_KEY`, en su `.env` (nunca en el repositorio) |
| Nombre de grupo | El que elijan, por ejemplo `grupo-7` | Ustedes | Solo se usa al pedir la API Key |
| Email del grupo | Un email al que tengan acceso | Ustedes | Solo se usa al pedir la API Key; ahí les llega |

En cada solicitud la API Key viaja en el header **`X-API-Key`**.

Mientras la URL pública no esté publicada, pueden desarrollar contra el **mock** (paso 6).

---

## Paso 1: obtener la API Key

Hacen **una sola vez** un `POST /v1/api-keys`, sin API Key, con el nombre de su grupo y su email:

```bash
curl -X POST <COMPLETAR: URL pública de la API>/v1/api-keys \
  -H "Content-Type: application/json" \
  -d '{ "grupo": "grupo-7", "email": "contacto@grupo7.example" }'
```

Desde **Postman**:
1. Método **POST**, URL `<URL>/v1/api-keys`.
2. Pestaña **Body** → **raw** → **JSON**:
   ```json
   { "grupo": "grupo-7", "email": "contacto@grupo7.example" }
   ```
3. **Send**. No hace falta ningún header de autenticación.

Respuesta `202 Accepted`:

```json
{
  "grupo": "grupo-7",
  "email": "contacto@grupo7.example",
  "mensaje": "Te enviamos la API Key a contacto@grupo7.example. Si no llega en unos minutos, revisá la carpeta de spam."
}
```

**La API Key no viene en la respuesta: llega por correo** a ese email, con el asunto *"Tu API Key de la API de Comprobantes"*. Así comprobamos que el email es de ustedes. La clave se ve así: `cmp_` seguido de 43 caracteres.

Reglas:

| Campo | Reglas |
|---|---|
| `grupo` | Entre 3 y 50 caracteres: letras minúsculas, números y guiones (`grupo-7`, `agencia-norte`). Identifica sus comprobantes: ningún otro grupo puede verlos |
| `email` | Un email del grupo al que tengan acceso |

- El primer registro **asocia el grupo con ese email**. Nadie más puede pedir claves para su grupo con otro email (responde `409`).
- **Si pierden la clave**, repitan el mismo `POST` con **el mismo grupo y el mismo email**: les llega una nueva y las anteriores siguen funcionando.
- Límite: 5 registros por hora por IP.

| Respuesta | Qué pasó | Qué hacer |
|---|---|---|
| `202` | La clave salió por correo | Revisar el email (y la carpeta de spam) |
| `400` | Falta un campo, el grupo no cumple el formato o el email no es válido. El detalle viene en `errores` | Corregir y reenviar |
| `409` | El grupo ya está registrado con otro email | Usar el email original o elegir otro nombre de grupo |
| `429` | Más de 5 registros en una hora | Esperar lo que indica el header `Retry-After` |
| `503` | No pudimos generar o enviar el correo | Reintentar en unos minutos |

---

## Paso 2: guardar la API Key en su proyecto

La API Key es un secreto: **no la suban al repositorio**.

1. En el `.env` de su proyecto (que tiene que estar en `.gitignore`), agreguen:
   ```bash
   # API de Comprobantes (grupo Agencia de Viajes)
   COMPROBANTES_URL=<COMPLETAR: URL pública de la API>
   COMPROBANTES_API_KEY=<pegar acá la clave que llegó por correo>
   ```
2. En su `.env.example` (que sí se versiona), dejen el lugar vacío para que cada integrante complete el suyo:
   ```bash
   COMPROBANTES_URL=
   COMPROBANTES_API_KEY=
   ```
3. Si usan Docker Compose, pásenlas al servicio que llama a la API:
   ```yaml
   services:
     su-servicio:
       environment:
         COMPROBANTES_URL: ${COMPROBANTES_URL}
         COMPROBANTES_API_KEY: ${COMPROBANTES_API_KEY}
   ```
4. En el código, léanlas de las variables de entorno (ver ejemplos del paso 3).

---

## Paso 3: emitir un comprobante

Llamen a la API **al final de su flujo de compra**, cuando la operación ya está confirmada.

```
POST <COMPROBANTES_URL>/v1/comprobantes
Content-Type: application/json
X-API-Key: <COMPROBANTES_API_KEY>
Idempotency-Key: <un identificador único de la operación, por ejemplo el id de su orden>
```

```json
{
  "email": "cliente@example.com",
  "items": [
    { "descripcion": "Entrada general x2", "precio": 30000 },
    { "descripcion": "Cargo por servicio", "precio": 1500 }
  ]
}
```

| Campo | Reglas |
|---|---|
| `email` | El email de **su cliente**: ahí le llega la factura. Hasta 254 caracteres |
| `items` | Entre 1 y 100 |
| `items[].descripcion` | Entre 1 y 200 caracteres |
| `items[].precio` | Número `>= 0`, en pesos argentinos (ARS). Si vendieron varias unidades, manden el importe ya multiplicado |
| `Idempotency-Key` (header) | Entre 8 y 64 caracteres. **La misma en cada reintento de la misma operación** |

- El total (suma de los precios) tiene que ser mayor a 0.
- Los precios se redondean a 2 decimales.
- No se aceptan campos extra.

Respuesta `201 Created`:

```json
{
  "id": "6b0c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3",
  "numero": "B-0001-00000042",
  "email": "cliente@example.com",
  "items": [
    { "descripcion": "Entrada general x2", "precio": 30000 },
    { "descripcion": "Cargo por servicio", "precio": 1500 }
  ],
  "total": 31500,
  "fechaEmision": "2026-10-22T15:30:00Z",
  "estadoEnvio": "PENDIENTE"
}
```

El correo sale unos segundos después (`estadoEnvio: PENDIENTE`). **Guarden el `id`** si quieren consultar cómo terminó el envío.

### Ejemplo en JavaScript (Node 18+)

```js
const URL_BASE = process.env.COMPROBANTES_URL;
const API_KEY = process.env.COMPROBANTES_API_KEY;

async function emitirComprobante(orden) {
  const respuesta = await fetch(`${URL_BASE}/v1/comprobantes`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-API-Key": API_KEY,
      "Idempotency-Key": `orden-${orden.id}`, // la misma en cada reintento
    },
    body: JSON.stringify({
      email: orden.emailCliente,
      items: orden.items.map((i) => ({ descripcion: i.nombre, precio: i.importe })),
    }),
    signal: AbortSignal.timeout(5000),
  });
  if (respuesta.status === 201 || respuesta.status === 200) {
    return await respuesta.json(); // guardar comprobante.id
  }
  throw new Error(`Comprobantes respondió ${respuesta.status}: ${await respuesta.text()}`);
}
```

### Ejemplo en Go

```go
func emitirComprobante(ctx context.Context, ordenID, email string, items []Item) (*Comprobante, error) {
	cuerpo, _ := json.Marshal(map[string]any{"email": email, "items": items})
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		os.Getenv("COMPROBANTES_URL")+"/v1/comprobantes", bytes.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", os.Getenv("COMPROBANTES_API_KEY"))
	req.Header.Set("Idempotency-Key", "orden-"+ordenID) // la misma en cada reintento

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comprobantes respondió %d", res.StatusCode)
	}
	var c Comprobante
	return &c, json.NewDecoder(res.Body).Decode(&c)
}

// Item y Comprobante: los campos de la respuesta, con tags json
// ("descripcion", "precio", "id", "numero", "estadoEnvio", etc.).
```

---

## Paso 4: consultar cómo terminó el envío (opcional)

```
GET <COMPROBANTES_URL>/v1/comprobantes/{id}
X-API-Key: <COMPROBANTES_API_KEY>
```

| `estadoEnvio` | Significado |
|---|---|
| `PENDIENTE` | El correo está encolado o reintentándose |
| `ENVIADO` | El servidor de correo lo aceptó |
| `FALLIDO` | Se agotaron los reintentos. El comprobante **sigue emitido**: no lo vuelvan a emitir |

Solo pueden ver los comprobantes de su grupo: los de otro grupo responden `404`.

---

## Paso 5: reintentos y errores

**Reintentar es seguro** si usan la misma `Idempotency-Key`: nunca se emiten dos facturas ni se mandan dos correos para la misma clave.

| Situación | Respuesta |
|---|---|
| Clave nueva | `201`: comprobante creado |
| Misma clave y mismo cuerpo | `200`: el mismo comprobante, **sin** reenviar el correo |
| Misma clave y otro cuerpo | `409`: usen una clave nueva |

| Código | Qué significa | ¿Reintentar? |
|---|---|---|
| 400 | Cuerpo inválido o falta la `Idempotency-Key`. El detalle viene en `errores` | No, corregir |
| 401 | Falta `X-API-Key` o la clave no es válida | No: revisen `COMPROBANTES_API_KEY` o pidan otra (paso 1) |
| 404 | El comprobante no existe o es de otro grupo | No |
| 409 | `Idempotency-Key` reutilizada con otro cuerpo | No, usar una clave nueva |
| 422 | El total es cero, o un importe supera 9.999.999.999,99 | No |
| 429 | Más de 60 solicitudes por minuto | Sí, después de `Retry-After` |
| 503 | No disponible momentáneamente | Sí, **con la misma** `Idempotency-Key` |

Los errores vienen en formato `application/problem+json`, con `type`, `title`, `status`, `detail` y, en los `400`, la lista `errores` (`campo` y `mensaje`).

Recomendaciones:
- Timeout de **5 s** por llamada.
- Hasta 2 o 3 reintentos con espera creciente (por ejemplo, 1 s y 3 s) ante timeout, `429` o `503`.
- Que un error nuestro **no bloquee su compra**: si falla, guarden el pedido y reintenten más tarde con la misma clave.

---

## Paso 6: probar sin depender de nosotros (mock)

El contrato tiene un mock que responde con los ejemplos y valida las solicitudes. Solo necesitan el archivo `openapi.yaml` que les compartimos y Docker. Desde la carpeta donde está el archivo:

```bash
docker run --rm -p 4010:4010 -v "${PWD}/openapi.yaml:/tmp/openapi.yaml:ro" stoplight/prism:5 mock -h 0.0.0.0 --multiprocess=false /tmp/openapi.yaml
```

Queda en `http://localhost:4010`. Usen `COMPROBANTES_URL=http://localhost:4010` y cualquier valor en `COMPROBANTES_API_KEY`. El mock exige que el header esté presente, pero no verifica la clave ni manda correos.

---

## Checklist

- [ ] Pedimos la API Key con `POST /v1/api-keys` y nos llegó por correo.
- [ ] `COMPROBANTES_URL` y `COMPROBANTES_API_KEY` están en nuestro `.env`, que no se sube al repositorio.
- [ ] `.env.example` tiene las dos variables vacías.
- [ ] Mandamos `X-API-Key` e `Idempotency-Key` en cada `POST /v1/comprobantes`.
- [ ] La `Idempotency-Key` es la misma en cada reintento de la misma operación.
- [ ] Usamos timeout de 5 s y reintentamos solo ante timeout, `429` o `503`.
- [ ] Un error de la API no bloquea la compra en nuestro sistema.

## Contacto

Ante dudas o problemas: `<COMPLETAR: email o canal de contacto del grupo>`.
