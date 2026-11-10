#!/bin/sh
# Carga el catálogo de demostración (infra/semilla/ofertas.json) a través del
# gateway, como lo haría un administrador desde el frontend. Lo corre el
# servicio "semilla" de docker-compose al levantar el entorno.
#
# - Usa el administrador inicial de Usuarios (USUARIOS_ADMIN_EMAIL y
#   USUARIOS_ADMIN_PASSWORD del .env). Sin ellos, no carga nada.
# - Es idempotente: si ya existe el pack "Bariloche completo", no hace nada,
#   así que reiniciar el entorno no duplica ofertas.
set -eu

GATEWAY="${GATEWAY_URL:-http://gateway:8080}"
DATOS="${DATOS:-/semilla/ofertas.json}"

if [ -z "${USUARIOS_ADMIN_EMAIL:-}" ] || [ -z "${USUARIOS_ADMIN_PASSWORD:-}" ]; then
  echo "semilla: sin USUARIOS_ADMIN_EMAIL/USUARIOS_ADMIN_PASSWORD en .env; no se cargan ofertas de demostración."
  exit 0
fi

apk add --no-cache curl jq >/dev/null

# El gateway responde antes de tener Catálogo en el pool: esperar a que las packs respondan.
intentos=0
until curl -fsS "$GATEWAY/api/packs" >/dev/null 2>&1; do
  intentos=$((intentos + 1))
  if [ "$intentos" -ge 60 ]; then
    echo "semilla: el catálogo no respondió en 2 minutos; no se cargó nada." >&2
    exit 1
  fi
  sleep 2
done

if curl -fsS "$GATEWAY/api/packs" | jq -e '.packs[] | select(.titulo == "Bariloche completo")' >/dev/null; then
  echo "semilla: el catálogo de demostración ya estaba cargado."
  exit 0
fi

TOKEN=$(jq -n --arg e "$USUARIOS_ADMIN_EMAIL" --arg p "$USUARIOS_ADMIN_PASSWORD" '{email: $e, password: $p}' |
  curl -fsS -X POST "$GATEWAY/api/sesiones" -H 'Content-Type: application/json' -d @- | jq -r '.token')
if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo "semilla: no se pudo iniciar sesión como administrador." >&2
  exit 1
fi

# Crea cada oferta y guarda clave → id para armar los packs.
ids='{}'
n=$(jq '.ofertas | length' "$DATOS")
i=0
while [ "$i" -lt "$n" ]; do
  oferta=$(jq -c ".ofertas[$i]" "$DATOS")
  clave=$(echo "$oferta" | jq -r '.clave')
  id=$(echo "$oferta" | jq -c 'del(.clave)' |
    curl -fsS -X POST "$GATEWAY/api/ofertas" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @- |
    jq -r '.id')
  ids=$(echo "$ids" | jq -c --arg k "$clave" --arg v "$id" '. + {($k): $v}')
  i=$((i + 1))
done

m=$(jq '.packs | length' "$DATOS")
j=0
while [ "$j" -lt "$m" ]; do
  jq -c --argjson ids "$ids" ".packs[$j] | .opciones |= map(\$ids[.])" "$DATOS" |
    curl -fsS -X POST "$GATEWAY/api/packs" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @- >/dev/null
  j=$((j + 1))
done

echo "semilla: catálogo de demostración cargado ($n ofertas y $m packs)."
