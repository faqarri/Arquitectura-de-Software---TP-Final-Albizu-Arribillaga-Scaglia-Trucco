# Registro de decisiones arquitectónicas

| ADR | Decisión | Estado |
|---|---|---|
| [ADR-001](ADR-001-limites-de-servicios.md) | D1 Límites de los servicios | Reemplazada por ADR-014 (2026-10-22) |
| [ADR-002](ADR-002-arquitectura-interna.md) | D2 Arquitectura interna de los servicios | Aceptada |
| [ADR-003](ADR-003-persistencia.md) | D3 Persistencia | Propuesta (versión inicial, revisada 2026-10-22; validación de Entrega 2 pendiente) |
| ADR-004 | D4 Consistencia y concurrencia | Pendiente (lo esencial está hoy en ADR-014 y ADR-005) |
| [ADR-005](ADR-005-comunicacion-entre-servicios.md) | D5 Comunicación entre servicios | Propuesta (versión inicial, revisada 2026-10-22 y 2026-10-23; validación de Entrega 2 pendiente) |
| [ADR-006](ADR-006-busqueda.md) | D6 Búsqueda | Aceptada |
| [ADR-007](ADR-007-cache.md) | D7 Caché | Aceptada |
| [ADR-008](ADR-008-contrato-propio.md) | D8 Contrato propio | Aceptada (revisada 2026-10-22: contrato 1.0.1; idempotencia por consumidor; redondeo) |
| [ADR-009](ADR-009-consumo-del-proveedor.md) | D9 Consumo del proveedor | Propuesta (estrategia general; se completa cuando se asigne el proveedor) |
| [ADR-010](ADR-010-resiliencia.md) | D10 Resiliencia | Aceptada |
| [ADR-011](ADR-011-observabilidad.md) | D11 Observabilidad | Primera versión (en elaboración) |
| [ADR-012](ADR-012-balanceo-de-carga.md) | D12 Balanceo de carga | En elaboración |
| [ADR-013](ADR-013-capacidad-y-costos.md) | D13 Capacidad y costos | Propuesta (plan, hipótesis y costos; resultados pendientes) |
| [ADR-014](ADR-014-cupos-en-catalogo-con-retencion.md) | D1 Límites de los servicios — cupos en Catálogo con retención temporal | Aceptada (revisada 2026-10-22: regla de bloqueo; validación de Entrega 2 pendiente) |
| [ADR-015](ADR-015-disponibilidad-en-tiempo-real.md) | Disponibilidad en tiempo real (agotado vs. retenido) — bonus | Aceptada |

Numeración: ADR-001 a ADR-013 corresponden a las decisiones D1 a D13 de la cátedra. Las decisiones adicionales y los reemplazos usan ADR-014 en adelante.

## Formato

Cada ADR contiene: **Estado**, **Fecha**, **Contexto**, **Decisión**, **Alternativas consideradas**, **Consecuencias** y, si corresponde, **Revisiones**.

## Criterio de cambios

La historia de una decisión **nunca se borra**.

| Situación | Qué se hace |
|---|---|
| Cambia la **decisión** de un ADR `Aceptada` (se elige otra alternativa) | El ADR original no se modifica: se marca `Reemplazada por ADR-XXX` con una nota breve. El nuevo ADR indica qué reemplaza y por qué fue necesario revisarlo. *Ejemplo:* ADR-001 → ADR-014. |
| La decisión de un ADR `Aceptada` se mantiene, pero cambian sus **detalles** | Se actualiza el mismo archivo y se agrega una fila en la sección **Revisiones** (fecha, cambio, motivo). *Ejemplo:* ADR-008, donde la capacidad publicada y la política de versionado se mantienen y cambia el contenido del contrato. |
| ADR en estado `Propuesta` (versión inicial) | Se revisa en el mismo archivo y se agrega una fila en **Revisiones**. *Ejemplo:* ADR-003 y ADR-005. |

La versión anterior completa de cualquier ADR revisado se puede consultar en el historial de git. Los cambios originados en devoluciones de la cátedra se resumen en [docs/devoluciones/](../devoluciones/).
