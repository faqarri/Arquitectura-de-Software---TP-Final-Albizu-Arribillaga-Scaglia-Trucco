// Avisos breves (toasts) que confirman cada acción. Los de error no se van solos.

import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import { Icono, type NombreIcono } from './Iconos'

type Tipo = 'exito' | 'error' | 'info'

export interface Aviso {
  tipo: Tipo
  titulo: string
  detalle?: string
  accion?: { etiqueta: string; alHacer: () => void }
  /** Milisegundos hasta que se va solo; 0 para que quede hasta cerrarlo. */
  duracion?: number
}

interface AvisoVisible extends Aviso {
  id: number
}

const Contexto = createContext<((a: Aviso) => () => void) | null>(null)

const iconos: Record<Tipo, NombreIcono> = { exito: 'check', error: 'alerta', info: 'info' }

export function ProveedorAvisos({ children }: { children: ReactNode }) {
  const [avisos, setAvisos] = useState<AvisoVisible[]>([])
  const siguiente = useRef(1)

  const cerrar = useCallback((id: number) => setAvisos((as) => as.filter((a) => a.id !== id)), [])

  const avisar = useCallback(
    (a: Aviso) => {
      const id = siguiente.current++
      setAvisos((as) => [...as.slice(-3), { ...a, id }])
      const duracion = a.duracion ?? (a.tipo === 'error' ? 0 : 5000)
      if (duracion > 0) setTimeout(() => cerrar(id), duracion)
      return () => cerrar(id)
    },
    [cerrar],
  )

  const valor = useMemo(() => avisar, [avisar])
  return (
    <Contexto.Provider value={valor}>
      {children}
      <div className="avisos" aria-live="polite" aria-relevant="additions">
        {avisos.map((a) => (
          <div key={a.id} className={`aviso aviso-${a.tipo}`} role={a.tipo === 'error' ? 'alert' : 'status'}>
            <span className="aviso-icono" aria-hidden="true">
              <Icono nombre={iconos[a.tipo]} />
            </span>
            <div className="aviso-texto">
              <strong>{a.titulo}</strong>
              {a.detalle && <span>{a.detalle}</span>}
            </div>
            {a.accion && (
              <button
                type="button"
                className="boton boton-chico boton-secundario"
                onClick={() => {
                  a.accion!.alHacer()
                  cerrar(a.id)
                }}
              >
                {a.accion.etiqueta}
              </button>
            )}
            <button type="button" className="aviso-cerrar" aria-label="Cerrar aviso" onClick={() => cerrar(a.id)}>
              <Icono nombre="x" />
            </button>
          </div>
        ))}
      </div>
    </Contexto.Provider>
  )
}

export function useAvisos() {
  const v = useContext(Contexto)
  if (!v) throw new Error('useAvisos fuera de ProveedorAvisos')
  return v
}
