#!/bin/bash
# Un usuario de MySQL por servicio, con permisos solo sobre su propio esquema (ADR-003).
# Las contraseñas vienen de variables de entorno (.env), nunca del repositorio.
# La imagen de MySQL ejecuta este archivo una sola vez, cuando el volumen de datos está vacío.

escapar_sql() {
  local valor="$1"
  valor=${valor//\\/\\\\}
  valor=${valor//\'/\'\'}
  printf '%s' "$valor"
}

crear_usuario() {
  local esquema="$1"
  local clave="$2"
  if [ -z "$clave" ]; then
    echo "Falta la contraseña de MySQL para el servicio '$esquema' (revisá .env)" >&2
    exit 1
  fi
  local clave_sql
  clave_sql=$(escapar_sql "$clave")
  mysql --protocol=socket -uroot -p"$MYSQL_ROOT_PASSWORD" <<SQL
CREATE USER IF NOT EXISTS '$esquema'@'%' IDENTIFIED BY '$clave_sql';
GRANT ALL PRIVILEGES ON \`$esquema\`.* TO '$esquema'@'%';
SQL
}

crear_usuario_grafana() {
  local clave="$1"
  if [ -z "$clave" ]; then
    echo "Falta GRAFANA_DB_PASSWORD en .env" >&2
    exit 1
  fi
  local clave_sql
  clave_sql=$(escapar_sql "$clave")
  mysql --protocol=socket -uroot -p"$MYSQL_ROOT_PASSWORD" <<SQL
CREATE USER IF NOT EXISTS 'grafana'@'%' IDENTIFIED BY '$clave_sql';
ALTER USER 'grafana'@'%' IDENTIFIED BY '$clave_sql';
REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'grafana'@'%';
GRANT SELECT ON \`comprobantes\`.\`app_logs\` TO 'grafana'@'%';
SQL
}

crear_usuario catalogo "$CATALOGO_DB_PASSWORD"
crear_usuario reservas "$RESERVAS_DB_PASSWORD"
crear_usuario comprobantes "$COMPROBANTES_DB_PASSWORD"
crear_usuario usuarios "$USUARIOS_DB_PASSWORD"
crear_usuario_grafana "$GRAFANA_DB_PASSWORD"
