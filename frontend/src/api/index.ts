import { apiFalsa } from './falso/servidor'
import { apiReal } from './real'
import type { Api } from './tipos'

/** Usa el gateway real; con VITE_DATOS_FALSOS=true, una API en memoria para demostrar sin backend. */
export const datosFalsos = import.meta.env.VITE_DATOS_FALSOS === 'true'

export const api: Api = datosFalsos ? apiFalsa : apiReal
