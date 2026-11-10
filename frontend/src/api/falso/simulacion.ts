// Fallas que se pueden activar en modo demostración, para ver cómo responde la
// interfaz cuando un servicio no está (las mismas de la jornada de fallas).

export interface Simulacion {
  catalogoCaido: boolean
  estudiarioCaido: boolean
  compraPendiente: boolean
  retencionCorta: boolean
}

const CLAVE = 'agencia.simulacion'
const oyentes = new Set<() => void>()
let actual: Simulacion = leer()

function leer(): Simulacion {
  const base: Simulacion = { catalogoCaido: false, estudiarioCaido: false, compraPendiente: false, retencionCorta: false }
  try {
    return { ...base, ...JSON.parse(sessionStorage.getItem(CLAVE) ?? '{}') }
  } catch {
    return base
  }
}

export function simulacion(): Simulacion {
  return actual
}

export function cambiarSimulacion(cambio: Partial<Simulacion>) {
  actual = { ...actual, ...cambio }
  try {
    sessionStorage.setItem(CLAVE, JSON.stringify(actual))
  } catch {
    // sin persistencia
  }
  oyentes.forEach((f) => f())
}

export function alCambiarSimulacion(f: () => void): () => void {
  oyentes.add(f)
  return () => oyentes.delete(f)
}
