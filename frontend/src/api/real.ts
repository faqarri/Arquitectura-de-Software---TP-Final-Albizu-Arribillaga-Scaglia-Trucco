// Cliente contra el gateway (/api). Todo pasa por el gateway: el frontend nunca
// llama directo a Estudiario ni a Comprobantes.

import { nuevoId } from '../formato'
import { ApiError, ErrorDeRed } from './errores'
import { guardarToken, leerToken } from '../sesion/token'
import type { Api, Compra, FiltrosBusqueda, Guia, Oferta, Pack } from './tipos'

const BASE = '/api'
const TIMEOUT_MS = 15000

interface Opciones {
  cuerpo?: unknown
  formulario?: FormData
  headers?: Record<string, string>
  /** Status, además de los 2xx, cuyo cuerpo es una respuesta válida (por ejemplo 409 de una compra rechazada). */
  aceptar?: number[]
}

export function idCorrelacion(): string {
  return 'web-' + nuevoId().slice(0, 13)
}

export async function pedir<T>(metodo: string, ruta: string, op: Opciones = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', 'X-Correlation-Id': idCorrelacion(), ...op.headers }
  const token = leerToken()
  if (token) headers.Authorization = 'Bearer ' + token
  let body: BodyInit | undefined
  if (op.formulario) body = op.formulario
  else if (op.cuerpo !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(op.cuerpo)
  }

  const control = new AbortController()
  const temporizador = setTimeout(() => control.abort(), TIMEOUT_MS)
  let res: Response
  try {
    res = await fetch(BASE + ruta, { method: metodo, headers, body, signal: control.signal })
  } catch {
    throw new ErrorDeRed()
  } finally {
    clearTimeout(temporizador)
  }

  const texto = await res.text()
  let datos: unknown = undefined
  if (texto) {
    try {
      datos = JSON.parse(texto)
    } catch {
      datos = undefined
    }
  }
  if (res.ok || op.aceptar?.includes(res.status)) return datos as T

  const p = (datos ?? {}) as Record<string, unknown>
  const espera = Number(res.headers.get('Retry-After'))
  if (res.status === 401 && token) guardarToken(null)
  throw new ApiError(
    res.status,
    typeof p.code === 'string' ? p.code : String(res.status),
    typeof p.detail === 'string' ? p.detail : '',
    p,
    Number.isFinite(espera) && espera > 0 ? espera : undefined,
  )
}

function consulta(f: FiltrosBusqueda): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(f)) {
    if (v !== undefined && v !== '' && v !== null) p.set(k, String(v))
  }
  const s = p.toString()
  return s ? '?' + s : ''
}

export const apiReal: Api = {
  async registrarse(s) {
    await pedir('POST', '/usuarios', { cuerpo: s })
  },
  iniciarSesion: (email, password) => pedir('POST', '/sesiones', { cuerpo: { email, password } }),

  buscar: (f) => pedir('GET', '/busqueda' + consulta(f)),
  oferta: (id) => pedir('GET', `/ofertas/${encodeURIComponent(id)}`),
  disponibilidad: (id) => pedir('GET', `/ofertas/${encodeURIComponent(id)}/disponibilidad`),
  guiasDelDestino: (id) => pedir('GET', `/ofertas/${encodeURIComponent(id)}/guias`),
  pack: (id) => pedir('GET', `/packs/${encodeURIComponent(id)}`),
  packs: async () => (await pedir<{ packs: Pack[] }>('GET', '/packs')).packs,

  carrito: () => pedir('GET', '/carrito'),
  agregarAlCarrito: (s, clave) => pedir('POST', '/carrito/items', { cuerpo: s, headers: { 'Idempotency-Key': clave } }),
  async quitarDelCarrito(id) {
    await pedir('DELETE', `/carrito/items/${encodeURIComponent(id)}`)
  },

  comprar: (clave) => pedir<Compra>('POST', '/compras', { headers: { 'Idempotency-Key': clave }, aceptar: [409] }),
  compra: (id) => pedir('GET', `/compras/${encodeURIComponent(id)}`),
  compras: async () => (await pedir<{ compras: Compra[] }>('GET', '/compras')).compras,

  guias: async () => (await pedir<{ guias: Guia[] }>('GET', '/guias')).guias,
  compartirGuia(s) {
    const f = new FormData()
    f.set('titulo', s.titulo)
    f.set('destino', s.destino)
    f.set('autor', s.autor)
    f.set('descripcion', s.descripcion)
    f.set('etiquetas', s.etiquetas.join(','))
    f.set('archivo', s.archivo)
    return pedir('POST', '/guias', { formulario: f })
  },

  crearOferta: (o) => pedir<Oferta>('POST', '/ofertas', { cuerpo: o }),
  editarOferta: (id, o) => pedir<Oferta>('PUT', `/ofertas/${encodeURIComponent(id)}`, { cuerpo: o }),
  async despublicarOferta(id) {
    await pedir('DELETE', `/ofertas/${encodeURIComponent(id)}`)
  },
  crearPack: (p) => pedir<Pack>('POST', '/packs', { cuerpo: p }),
}
