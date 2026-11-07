import { apiFalsa } from './falso/servidor'
import { apiReal } from './real'
import type { Api } from './tipos'

/** Mientras el gateway no esté, el frontend usa datos de demostración (VITE_DATOS_FALSOS=false para el gateway). */
export const datosFalsos = import.meta.env.VITE_DATOS_FALSOS !== 'false'

export const api: Api = datosFalsos ? apiFalsa : apiReal
