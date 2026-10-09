# Servicio Usuarios

**Responsabilidad:** registro, inicio de sesión y emisión de JWT con rol (`cliente`, `admin`).

**Datos propios (MySQL):** usuarios, roles, hash de contraseña (bcrypt). La contraseña nunca se almacena en claro ni se devuelve en respuestas.

**Dependencias:** MySQL.
