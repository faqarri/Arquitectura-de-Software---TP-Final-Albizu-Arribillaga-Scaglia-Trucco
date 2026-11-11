# Servicio Usuarios

Implementa el registro, el inicio de sesión y la emisión de JWT para los roles `cliente` y `admin`. Usa arquitectura hexagonal (dominio, puertos, casos de uso y adaptadores HTTP y MySQL, [ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md)); sus datos viven en el esquema MySQL `usuarios`.

## Rutas

| Método | Ruta | Respuesta |
|---|---|---|
| `POST` | `/api/usuarios` | `201` con `id`, `nombre`, `email` y `rol` |
| `POST` | `/api/sesiones` | `200` con `token` y `tipo: "Bearer"` |
| `GET` | `/health` | `200 {"estado":"ok"}` si MySQL responde |
| `GET` | `/metrics` | Métricas HTTP para Prometheus |

Registro:

```json
{"nombre":"Ana Pérez","email":"ana@example.com","password":"una-clave-segura"}
```

Inicio de sesión:

```json
{"email":"ana@example.com","password":"una-clave-segura"}
```

Las contraseñas se guardan únicamente como hash bcrypt. El registro siempre asigna el rol `cliente`; el rol no se acepta desde el cuerpo. El JWT usa HS256 y contiene `sub`, `email`, `rol` y `exp` (8 horas). El gateway lo valida y propaga la identidad en `X-Usuario-Id`, `X-Usuario-Email` y `X-Usuario-Rol`; los servicios internos no validan el token ([ADR-012](../../docs/adr/ADR-012-balanceo-de-carga.md)).

## Configuración y ejecución

El servicio se inicia con el Compose de la raíz:

```sh
docker compose up -d --build gateway usuarios
docker compose exec usuarios wget -qO- http://localhost:8080/health
```

Variables requeridas: `USUARIOS_DB_PASSWORD` y `JWT_SECRET` (mínimo 32 caracteres). La base debe haber sido inicializada desde un volumen vacío para que el script de MySQL cree el usuario `usuarios`.

Para crear un administrador inicial, definir ambas variables:

- `USUARIOS_ADMIN_EMAIL`
- `USUARIOS_ADMIN_PASSWORD`

El proceso lo crea solo si ese email no existe. Si ya existe como administrador, no lo modifica; si pertenece a un cliente, el servicio falla al iniciar para evitar promover cuentas accidentalmente. Si ambas variables no están configuradas, el servicio inicia sin sembrar un administrador y deja una advertencia estructurada.

Usuarios no publica un puerto al host; escucha en `8080` dentro de Compose y el Gateway lo expone indirectamente en `localhost:8080`. Prometheus scrapea sus métricas en `usuarios:8080/metrics`.

**Historial:** 2026-10-22: se agrega el email como claim del JWT. 2026-11-09: implementación de registro, login, bcrypt, JWT HS256 y administrador inicial por entorno.
