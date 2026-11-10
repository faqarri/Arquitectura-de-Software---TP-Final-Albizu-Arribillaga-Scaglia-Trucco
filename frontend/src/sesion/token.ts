// El JWT vive en sessionStorage: se pierde al cerrar la pestaña y no queda en disco.

import type { Rol } from '../api/tipos'

export interface Usuario {
  id: string
  email: string
  rol: Rol
  /** Vencimiento en milisegundos. */
  expira: number
}

const CLAVE = 'agencia.token'
const oyentes = new Set<() => void>()

export function leerToken(): string | null {
  try {
    return sessionStorage.getItem(CLAVE)
  } catch {
    return null
  }
}

export function guardarToken(token: string | null) {
  try {
    if (token) sessionStorage.setItem(CLAVE, token)
    else sessionStorage.removeItem(CLAVE)
  } catch {
    // Sin sessionStorage la sesión dura lo que dure la página.
  }
  oyentes.forEach((f) => f())
}

export function alCambiarToken(f: () => void): () => void {
  oyentes.add(f)
  return () => oyentes.delete(f)
}

/** Lee los claims del JWT (sub, email, rol, exp). No verifica la firma: eso lo hace el gateway. */
export function usuarioDelToken(token: string | null): Usuario | null {
  if (!token) return null
  const partes = token.split('.')
  if (partes.length !== 3) return null
  try {
    const base64 = partes[1].replace(/-/g, '+').replace(/_/g, '/')
    const json = decodeURIComponent(
      Array.from(atob(base64), (c) => '%' + c.charCodeAt(0).toString(16).padStart(2, '0')).join(''),
    )
    const claims = JSON.parse(json) as { sub?: string; email?: string; rol?: string; exp?: number }
    if (!claims.sub || !claims.email || !claims.exp) return null
    const expira = claims.exp * 1000
    if (expira <= Date.now()) return null
    return { id: claims.sub, email: claims.email, rol: claims.rol === 'admin' ? 'admin' : 'cliente', expira }
  } catch {
    return null
  }
}

/** Arma un token sin firma con los mismos claims que Usuarios. Solo para los datos de demostración. */
export function tokenDeDemostracion(sub: string, email: string, rol: Rol): string {
  const b64 = (o: unknown) =>
    btoa(unescape(encodeURIComponent(JSON.stringify(o)))).replace(/=+$/, '').replace(/\+/g, '-').replace(/\//g, '_')
  const exp = Math.floor(Date.now() / 1000) + 8 * 3600
  return `${b64({ alg: 'none', typ: 'JWT' })}.${b64({ sub, email, rol, exp })}.demo`
}
