// Componentes comunes: cada estado se muestra siempre igual en toda la aplicación.

import { useEffect, useId, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { mensajeDeError } from '../api/errores'
import type { EstadoCompra, EstadoDisponibilidad, EstadoGuia } from '../api/tipos'
import { duracion, hora } from '../formato'
import { useAhora } from '../hooks/hooks'
import { Icono, type NombreIcono } from './Iconos'

export function Cargando({ texto = 'Cargando…' }: { texto?: string }) {
  return (
    <div className="cargando" role="status">
      <span className="girador" aria-hidden="true" />
      {texto}
    </div>
  )
}

export function MensajeError({ error, alReintentar }: { error: unknown; alReintentar?: () => void }) {
  const m = mensajeDeError(error)
  return (
    <div className="caja caja-error" role="alert">
      <strong>{m.titulo}</strong>
      <p>{m.detalle}</p>
      {alReintentar && m.reintentable && (
        <button type="button" className="boton boton-secundario" onClick={alReintentar}>
          Reintentar
        </button>
      )}
    </div>
  )
}

export function Vacio({ titulo, children }: { titulo: string; children?: ReactNode }) {
  return (
    <div className="vacio">
      <strong>{titulo}</strong>
      {children}
    </div>
  )
}

type Tono = 'exito' | 'aviso' | 'peligro' | 'neutro' | 'info'

function Etiqueta({ tono, icono, children }: { tono: Tono; icono: NombreIcono; children: ReactNode }) {
  return (
    <span className={`etiqueta etiqueta-${tono}`}>
      <Icono nombre={icono} /> {children}
    </span>
  )
}

const disponibilidad: Record<EstadoDisponibilidad, [Tono, NombreIcono, string]> = {
  DISPONIBLE: ['exito', 'check', 'Disponible'],
  RETENIDA: ['aviso', 'reloj', 'Sin cupo por ahora'],
  AGOTADA: ['peligro', 'x', 'Agotada'],
}

export function EstadoDisponibilidadEtiqueta({ estado }: { estado: EstadoDisponibilidad }) {
  const [tono, icono, texto] = disponibilidad[estado]
  return <Etiqueta tono={tono} icono={icono}>{texto}</Etiqueta>
}

const compra: Record<EstadoCompra, [Tono, NombreIcono, string]> = {
  PENDIENTE: ['info', 'reloj', 'Procesando'],
  CONFIRMADA: ['exito', 'check', 'Confirmada'],
  RECHAZADA: ['peligro', 'x', 'Rechazada'],
}

export function EstadoCompraEtiqueta({ estado }: { estado: EstadoCompra }) {
  const [tono, icono, texto] = compra[estado]
  return <Etiqueta tono={tono} icono={icono}>{texto}</Etiqueta>
}

export const textoGuia: Record<EstadoGuia, [Tono, NombreIcono, string, string]> = {
  POR_ENVIAR: ['neutro', 'reintentar', 'Por enviar', 'El servicio de guías no respondió. La enviamos sola en cuanto vuelva; no tenés que hacer nada.'],
  PENDIENTE: ['info', 'reloj', 'En moderación', 'La recibieron y la están revisando. Suele tardar unos minutos.'],
  PUBLICADA: ['exito', 'check', 'Publicada', 'Ya la pueden ver otros viajeros en las ofertas de ese destino.'],
  RECHAZADA: ['peligro', 'x', 'Rechazada', 'No se publicó. Revisá el motivo y, si querés, compartila de nuevo corregida.'],
}

export function EstadoGuiaEtiqueta({ estado }: { estado: EstadoGuia }) {
  const [tono, icono, texto] = textoGuia[estado]
  return <Etiqueta tono={tono} icono={icono}>{texto}</Etiqueta>
}

/** Tiempo que le queda a una retención. Avisa cuando faltan menos de 3 minutos. */
export function CuentaRegresiva({ expiraEn }: { expiraEn: string }) {
  const ahora = useAhora()
  const resta = Date.parse(expiraEn) - ahora
  if (resta <= 0) return <Etiqueta tono="peligro" icono="x">Reserva vencida</Etiqueta>
  const poco = resta < 3 * 60_000
  return (
    <span className={`cuenta ${poco ? 'cuenta-poco' : ''}`} title={`Reservado hasta las ${hora(expiraEn)}`}>
      <Icono nombre="reloj" /> Reservado por <strong className="numero">{duracion(resta)}</strong>
      <span className="solo-lectores"> minutos, hasta las {hora(expiraEn)}</span>
    </span>
  )
}

interface PropsDialogo {
  abierto: boolean
  titulo: string
  children: ReactNode
  confirmar: string
  cancelar?: string
  peligroso?: boolean
  ocupado?: boolean
  alConfirmar: () => void
  alCerrar: () => void
}

/** Diálogo modal nativo: Esc lo cierra y el foco queda adentro mientras está abierto. */
export function Dialogo(p: PropsDialogo) {
  const ref = useRef<HTMLDialogElement>(null)
  const idTitulo = useId()
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (p.abierto && !d.open) d.showModal?.()
    if (!p.abierto && d.open) d.close?.()
  }, [p.abierto])
  return (
    <dialog
      ref={ref}
      className="dialogo"
      aria-labelledby={idTitulo}
      onCancel={(e) => {
        e.preventDefault()
        if (!p.ocupado) p.alCerrar()
      }}
    >
      <h2 id={idTitulo}>{p.titulo}</h2>
      <div className="dialogo-cuerpo">{p.children}</div>
      <div className="acciones">
        <button type="button" className="boton boton-secundario" onClick={p.alCerrar} disabled={p.ocupado}>
          {p.cancelar ?? 'Cancelar'}
        </button>
        <button
          type="button"
          className={`boton ${p.peligroso ? 'boton-peligro' : 'boton-primario'}`}
          onClick={p.alConfirmar}
          disabled={p.ocupado}
          aria-busy={p.ocupado}
        >
          {p.ocupado ? 'Procesando…' : p.confirmar}
        </button>
      </div>
    </dialog>
  )
}

export function Migas({ pasos }: { pasos: { texto: string; a?: string }[] }) {
  return (
    <nav aria-label="Estás en" className="migas">
      <ol>
        {pasos.map((p, i) => (
          <li key={i}>{p.a ? <Link to={p.a}>{p.texto}</Link> : <span aria-current="page">{p.texto}</span>}</li>
        ))}
      </ol>
    </nav>
  )
}

export function Paginacion({ pagina, paginas, alCambiar }: { pagina: number; paginas: number; alCambiar: (p: number) => void }) {
  if (paginas <= 1) return null
  return (
    <nav aria-label="Páginas de resultados" className="paginacion">
      <button type="button" className="boton boton-secundario" disabled={pagina <= 1} onClick={() => alCambiar(pagina - 1)}>
        ← Anterior
      </button>
      <span>
        Página <strong>{pagina}</strong> de {paginas}
      </span>
      <button type="button" className="boton boton-secundario" disabled={pagina >= paginas} onClick={() => alCambiar(pagina + 1)}>
        Siguiente →
      </button>
    </nav>
  )
}

interface PropsCampo {
  etiqueta: string
  ayuda?: ReactNode
  error?: string
  opcional?: boolean
  children: (props: { id: string; 'aria-describedby'?: string; 'aria-invalid'?: boolean }) => ReactNode
}

/** Etiqueta, ayuda y error de un campo, conectados para lectores de pantalla. */
export function Campo({ etiqueta, ayuda, error, opcional, children }: PropsCampo) {
  const id = useId()
  const describe = [ayuda ? id + '-ayuda' : '', error ? id + '-error' : ''].filter(Boolean).join(' ') || undefined
  return (
    <div className={`campo ${error ? 'campo-con-error' : ''}`}>
      <label htmlFor={id}>
        {etiqueta} {opcional && <span className="opcional">(opcional)</span>}
      </label>
      {children({ id, 'aria-describedby': describe, 'aria-invalid': error ? true : undefined })}
      {ayuda && (
        <small id={id + '-ayuda'} className="ayuda-campo">
          {ayuda}
        </small>
      )}
      {error && (
        <small id={id + '-error'} className="error-campo">
          <span aria-hidden="true">! </span>
          {error}
        </small>
      )}
    </div>
  )
}

export function SelectorCantidad({ valor, max, alCambiar, etiqueta = 'Cantidad' }: { valor: number; max: number; alCambiar: (n: number) => void; etiqueta?: string }) {
  const id = useId()
  return (
    <div className="selector-cantidad">
      <label htmlFor={id}>{etiqueta}</label>
      <div className="selector-cantidad-controles">
        <button type="button" className="boton boton-secundario" aria-label="Uno menos" disabled={valor <= 1} onClick={() => alCambiar(valor - 1)}>
          −
        </button>
        <input
          id={id}
          type="number"
          inputMode="numeric"
          min={1}
          max={max}
          value={valor}
          onChange={(e) => {
            const n = Number(e.target.value)
            if (Number.isFinite(n)) alCambiar(Math.min(Math.max(1, Math.round(n)), Math.max(1, max)))
          }}
        />
        <button type="button" className="boton boton-secundario" aria-label="Uno más" disabled={valor >= max} onClick={() => alCambiar(valor + 1)}>
          +
        </button>
      </div>
    </div>
  )
}

/** Ayuda contextual plegable, para explicar un concepto donde se lo necesita. */
export function Ayuda({ pregunta, children }: { pregunta: string; children: ReactNode }) {
  return (
    <details className="ayuda">
      <summary>
        <Icono nombre="ayuda" /> {pregunta}
      </summary>
      <div>{children}</div>
    </details>
  )
}
