// Estado del carrito compartido por el encabezado (contador) y las páginas.
// Quitar un ítem se puede deshacer durante unos segundos: recién después se libera el cupo.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from '../api'
import { mensajeDeError } from '../api/errores'
import type { Carrito, ItemCarrito, SolicitudAgregar } from '../api/tipos'
import { useAvisos } from '../componentes/Avisos'
import { useSesion } from '../sesion/Sesion'

const ESPERA_DESHACER_MS = 6000

interface ValorCarrito {
  items: ItemCarrito[]
  cargando: boolean
  error: unknown
  recargar: () => Promise<void>
  agregar: (s: SolicitudAgregar, clave: string) => Promise<ItemCarrito>
  quitar: (item: ItemCarrito) => void
}

const Contexto = createContext<ValorCarrito | null>(null)

export function ProveedorCarrito({ children }: { children: ReactNode }) {
  const { usuario } = useSesion()
  const avisar = useAvisos()
  const [carrito, setCarrito] = useState<Carrito>({ items: [], totalReferencia: 0 })
  const [cargando, setCargando] = useState(false)
  const [error, setError] = useState<unknown>()
  const [ocultos, setOcultos] = useState<Set<string>>(new Set())
  const pendientes = useRef(new Map<string, ReturnType<typeof setTimeout>>())

  const recargar = useCallback(async () => {
    if (!usuario || usuario.rol !== 'cliente') {
      setCarrito({ items: [], totalReferencia: 0 })
      return
    }
    setCargando(true)
    try {
      setCarrito(await api.carrito())
      setError(undefined)
    } catch (e) {
      setError(e)
    } finally {
      setCargando(false)
    }
  }, [usuario])

  useEffect(() => {
    void recargar()
  }, [recargar])

  const agregar = useCallback(
    async (s: SolicitudAgregar, clave: string) => {
      const item = await api.agregarAlCarrito(s, clave)
      await recargar()
      return item
    },
    [recargar],
  )

  const mostrar = useCallback((id: string) => {
    setOcultos((o) => {
      const n = new Set(o)
      n.delete(id)
      return n
    })
  }, [])

  const quitar = useCallback(
    (item: ItemCarrito) => {
      setOcultos((o) => new Set(o).add(item.id))
      const t = setTimeout(async () => {
        pendientes.current.delete(item.id)
        try {
          await api.quitarDelCarrito(item.id)
          await recargar()
        } catch (e) {
          const m = mensajeDeError(e)
          avisar({ tipo: 'error', titulo: 'No se pudo quitar el ítem', detalle: m.detalle })
        } finally {
          mostrar(item.id)
        }
      }, ESPERA_DESHACER_MS)
      pendientes.current.set(item.id, t)
      avisar({
        tipo: 'info',
        titulo: 'Quitaste un ítem del carrito',
        detalle: item.descripcion,
        duracion: ESPERA_DESHACER_MS,
        accion: {
          etiqueta: 'Deshacer',
          alHacer: () => {
            clearTimeout(pendientes.current.get(item.id))
            pendientes.current.delete(item.id)
            mostrar(item.id)
          },
        },
      })
    },
    [avisar, mostrar, recargar],
  )

  const items = useMemo(() => carrito.items.filter((it) => !ocultos.has(it.id)), [carrito, ocultos])
  const valor = useMemo(() => ({ items, cargando, error, recargar, agregar, quitar }), [items, cargando, error, recargar, agregar, quitar])
  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

export function useCarrito(): ValorCarrito {
  const v = useContext(Contexto)
  if (!v) throw new Error('useCarrito fuera de ProveedorCarrito')
  return v
}
