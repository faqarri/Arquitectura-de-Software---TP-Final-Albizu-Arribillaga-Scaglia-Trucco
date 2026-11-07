import { useCallback, useEffect, useRef, useState } from 'react'

/** Hora actual, actualizada cada `cadaMs`. Sirve para las cuentas regresivas. */
export function useAhora(cadaMs = 1000): number {
  const [ahora, setAhora] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setAhora(Date.now()), cadaMs)
    return () => clearInterval(t)
  }, [cadaMs])
  return ahora
}

export function useTitulo(titulo: string) {
  useEffect(() => {
    document.title = titulo ? `${titulo} · Agencia de Viajes` : 'Agencia de Viajes'
  }, [titulo])
}

export interface Carga<T> {
  datos: T | undefined
  error: unknown
  cargando: boolean
  recargar: () => void
}

/** Carga datos de la API con estado de carga y error, y descarta respuestas viejas. */
export function useCarga<T>(cargar: () => Promise<T>, deps: unknown[]): Carga<T> {
  const [datos, setDatos] = useState<T>()
  const [error, setError] = useState<unknown>()
  const [cargando, setCargando] = useState(true)
  const [vuelta, setVuelta] = useState(0)
  const ultima = useRef(0)

  useEffect(() => {
    const esta = ++ultima.current
    setCargando(true)
    setError(undefined)
    cargar().then(
      (d) => {
        if (esta !== ultima.current) return
        setDatos(d)
        setCargando(false)
      },
      (e) => {
        if (esta !== ultima.current) return
        setError(e)
        setCargando(false)
      },
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, vuelta])

  const recargar = useCallback(() => setVuelta((v) => v + 1), [])
  return { datos, error, cargando, recargar }
}

/** Repite `f` cada `cadaMs` mientras `activo` sea verdadero. */
export function useCadaTanto(f: () => void, cadaMs: number, activo = true) {
  const ref = useRef(f)
  ref.current = f
  useEffect(() => {
    if (!activo) return
    const t = setInterval(() => ref.current(), cadaMs)
    return () => clearInterval(t)
  }, [cadaMs, activo])
}
