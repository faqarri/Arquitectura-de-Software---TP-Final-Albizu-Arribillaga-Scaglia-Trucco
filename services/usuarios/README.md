# Servicio Usuarios

**Responsabilidad:** registro, inicio de sesión y emisión de JWT con rol (`cliente`, `admin`).

**Datos propios (MySQL):** usuarios, roles, hash de contraseña (bcrypt). La contraseña nunca se almacena en claro ni se devuelve en respuestas.

**Dependencias:** MySQL.

**Contenido del JWT (claims):** `sub` (id de usuario), `email`, `rol` y `exp`. El gateway valida el token y propaga estos datos a los servicios en los headers `X-Usuario-Id`, `X-Usuario-Email` y `X-Usuario-Rol`. Reservas usa el email para el evento `CompraConfirmada`, así no necesita consultar a Usuarios ([ADR-014](../../docs/adr/ADR-014-cupos-en-catalogo-con-retencion.md)).

**Historial:** 2026-10-22: se agrega el email como claim del JWT.
