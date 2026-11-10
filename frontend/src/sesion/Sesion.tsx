import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { alCambiarToken, guardarToken, leerToken, usuarioDelToken, type Usuario } from './token'

interface ValorSesion {
  usuario: Usuario | null
  /** La sesión se cerró sola (token vencido o rechazado), no porque el usuario salió. */
  vencida: boolean
  ingresar: (token: string) => void
  salir: () => void
}

const Contexto = createContext<ValorSesion | null>(null)

export function ProveedorSesion({ children }: { children: ReactNode }) {
  const [usuario, setUsuario] = useState(() => usuarioDelToken(leerToken()))
  const [vencida, setVencida] = useState(false)

  useEffect(
    () =>
      alCambiarToken(() => {
        const u = usuarioDelToken(leerToken())
        setUsuario((antes) => {
          if (antes && !u) setVencida(true)
          return u
        })
      }),
    [],
  )

  // El JWT dura 8 horas: al vencer, la sesión se cierra sin esperar un 401.
  useEffect(() => {
    if (!usuario) return
    const t = setTimeout(() => guardarToken(null), Math.max(0, usuario.expira - Date.now()))
    return () => clearTimeout(t)
  }, [usuario])

  const ingresar = useCallback((token: string) => {
    setVencida(false)
    guardarToken(token)
  }, [])

  const salir = useCallback(() => {
    guardarToken(null)
    setVencida(false)
  }, [])

  const valor = useMemo(() => ({ usuario, vencida, ingresar, salir }), [usuario, vencida, ingresar, salir])
  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

export function useSesion(): ValorSesion {
  const v = useContext(Contexto)
  if (!v) throw new Error('useSesion fuera de ProveedorSesion')
  return v
}
