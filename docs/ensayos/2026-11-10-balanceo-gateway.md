# Ensayo de balanceo y recuperación del Gateway (2026-11-10)

## Objetivo

Verificar el round robin entre las dos instancias reales de Catálogo, la exclusión de una instancia caída, la alerta de Prometheus y su reincorporación al pool.

## Procedimiento

1. Levantar el entorno con `docker compose up -d --build`.
2. Consultar varias veces `GET http://localhost:8080/api/packs` y revisar el header `X-Instancia`.
3. Detener `catalogo-2` con `docker compose stop catalogo-2`.
4. Esperar los chequeos de salud del Gateway; consultar `gateway_instancia_sana` y repetir `GET /api/packs`.
5. Esperar la evaluación de la alerta `InstanciaCatalogoFueraDelPool` en Prometheus.
6. Iniciar `catalogo-2` con `docker compose start catalogo-2` y esperar sus chequeos saludables.

## Resultados

| Prueba | Resultado |
|---|---|
| Salud de ambas instancias y targets Prometheus | `catalogo-1` y `catalogo-2` respondieron `200`; ambos targets quedaron `up` |
| Round robin inicial | 6 solicitudes devolvieron `200` y alternaron entre `catalogo-1` y `catalogo-2` |
| Retiro de `catalogo-2` | Tras los chequeos fallidos, `gateway_instancia_sana{instancia="catalogo-2"}` pasó a `0` |
| Continuidad durante la caída | 3 solicitudes consecutivas a `/api/packs` devolvieron `200` desde `catalogo-1` |
| Alerta | `InstanciaCatalogoFueraDelPool` llegó a estado `firing` |
| Recuperación | Tras iniciar `catalogo-2`, su health respondió `200` y `gateway_instancia_sana{instancia="catalogo-2"}` volvió a `1` |

El dashboard de Grafana cargó sus 12 paneles. La fuente MySQL informó `Database Connection OK`; el usuario `grafana` conserva permisos `SELECT` únicamente sobre las tablas `app_logs` de Comprobantes, Reservas y Catálogo.

## Conclusión

Se comprobó en ejecución local la distribución, continuidad con una instancia fuera del pool, disparo de la alerta y recuperación automática. La decisión de balanceo de ADR-012 cumple el ensayo integrado. La alerta global de tasa 5xx y el streaming SSE no forman parte de esta prueba; SSE depende del servicio opcional Tiempo Real.
