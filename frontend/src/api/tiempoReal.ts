// Eventos en tiempo real (bonus, ADR-015): el servicio Tiempo Real los manda por
// Server-Sent Events a través del gateway. Se leen con fetch y no con
// EventSource porque EventSource no puede mandar el header Authorization, que
// necesita "mis retenciones". Son avisos de mejor esfuerzo: al reconectar se
// vuelve a consultar el estado actual.

import { useEffect, useRef } from 'react'
import { leerToken } from '../sesion/token'

const REINTENTO_MS = 3000

export type AlEvento = (nombre: string, datos: unknown) => void

/**
 * Abre la conexión y la mantiene: si se corta, reconecta y llama a alReconectar
 * para que la pantalla se ponga al día. Devuelve la función que la cierra.
 */
export function escucharEventos(ruta: string, alEvento: AlEvento, alReconectar?: () => void): () => void {
  let cerrado = false
  let conexiones = 0
  let control: AbortController | undefined
  let temporizador: ReturnType<typeof setTimeout> | undefined

  function procesar(bloque: string) {
    let nombre = 'message'
    const datos: string[] = []
    for (const linea of bloque.split('\n')) {
      if (linea.startsWith('event:')) nombre = linea.slice(6).trim()
      else if (linea.startsWith('data:')) datos.push(linea.slice(5).trimStart())
    }
    if (datos.length === 0) return // comentario (latido)
    try {
      alEvento(nombre, JSON.parse(datos.join('\n')))
    } catch {
      // un evento que no se entiende se ignora
    }
  }

  async function conectar() {
    if (cerrado) return
    control = new AbortController()
    const headers: Record<string, string> = { Accept: 'text/event-stream' }
    const token = leerToken()
    if (token) headers.Authorization = 'Bearer ' + token
    try {
      const res = await fetch('/api' + ruta, { headers, signal: control.signal })
      // 4xx: la solicitud no va a mejorar reintentando (por ejemplo, sesión vencida).
      if (res.status >= 400 && res.status < 500) {
        cerrado = true
        return
      }
      if (!res.ok || !res.body) throw new Error(`tiempo real respondió ${res.status}`)
      if (conexiones++ > 0) alReconectar?.()
      const lector = res.body.getReader()
      const decodificador = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { value, done } = await lector.read()
        if (done) break
        buffer += decodificador.decode(value, { stream: true }).replace(/\r/g, '')
        let fin: number
        while ((fin = buffer.indexOf('\n\n')) >= 0) {
          procesar(buffer.slice(0, fin))
          buffer = buffer.slice(fin + 2)
        }
      }
    } catch {
      // red caída, servicio caído o conexión cerrada: se reintenta
    }
    if (!cerrado) temporizador = setTimeout(() => void conectar(), REINTENTO_MS)
  }

  void conectar()
  return () => {
    cerrado = true
    clearTimeout(temporizador)
    control?.abort()
  }
}

/** Escucha los eventos de ruta mientras el componente esté montado (null = no escuchar). */
export function useEventos(ruta: string | null, alEvento: AlEvento, alReconectar?: () => void) {
  const manejadores = useRef({ alEvento, alReconectar })
  manejadores.current = { alEvento, alReconectar }
  useEffect(() => {
    if (!ruta) return
    return escucharEventos(
      ruta,
      (n, d) => manejadores.current.alEvento(n, d),
      () => manejadores.current.alReconectar?.(),
    )
  }, [ruta])
}
