import { useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { ApiError, mensajeDeError } from '../api/errores'
import type { TipoOferta } from '../api/tipos'
import { useCarrito } from '../carrito/Carrito'
import { useAvisos } from '../componentes/Avisos'
import { Ayuda, Cargando, MensajeError, Migas, SelectorCantidad } from '../componentes/ui'
import { fechaHora, moneda, nuevoId } from '../formato'
import { useCarga, useTitulo } from '../hooks/hooks'
import { MAX_CUPOS_POR_OFERTA, nombreTipo, precioDelPack, problemaDelPack } from '../reglas'
import { useSesion } from '../sesion/Sesion'
import { iconoTipo } from './Buscar'
import { Icono } from '../componentes/Iconos'

const TIPOS: TipoOferta[] = ['TRANSPORTE', 'ESTADIA', 'EXCURSION']

export function Pack() {
  const { id = '' } = useParams()
  const pack = useCarga(() => api.pack(id), [id])
  const { usuario } = useSesion()
  const { agregar } = useCarrito()
  const avisar = useAvisos()
  const navegar = useNavigate()
  const ubicacion = useLocation()
  const [elegidas, setElegidas] = useState<Set<string>>(new Set())
  const [cantidad, setCantidad] = useState(1)
  const [agregando, setAgregando] = useState(false)
  const [error, setError] = useState<unknown>()
  const [clave, setClave] = useState(() => nuevoId())
  useTitulo(pack.datos?.titulo ?? 'Pack')

  if (pack.error !== undefined) return <div className="pagina-angosta"><MensajeError error={pack.error} alReintentar={pack.recargar} /></div>
  if (!pack.datos) return <Cargando texto="Cargando el pack…" />
  const p = pack.datos

  const opciones = p.opciones.filter((o) => elegidas.has(o.id))
  const problema = problemaDelPack(opciones)
  const subtotal = opciones.reduce((t, o) => t + o.precio, 0)
  const total = precioDelPack(opciones, p.descuentoPorcentaje) * cantidad

  function alternar(oid: string) {
    setElegidas((s) => {
      const n = new Set(s)
      if (n.has(oid)) n.delete(oid)
      else n.add(oid)
      return n
    })
    setError(undefined)
  }

  async function alAgregar() {
    setAgregando(true)
    setError(undefined)
    try {
      await agregar({ packId: p.id, opciones: [...elegidas], cantidad }, clave)
      setClave(nuevoId())
      avisar({
        tipo: 'exito',
        titulo: 'Pack agregado al carrito',
        detalle: 'Te guardamos todas las opciones por 15 minutos.',
        accion: { etiqueta: 'Ver carrito', alHacer: () => navegar('/carrito') },
      })
      setElegidas(new Set())
    } catch (e) {
      if (!mensajeDeError(e).reintentable) setClave(nuevoId())
      setError(e)
    } finally {
      setAgregando(false)
    }
  }

  return (
    <div className="pagina-detalle">
      <Migas pasos={[{ texto: 'Buscar', a: '/' }, { texto: p.destino, a: `/?destino=${encodeURIComponent(p.destino)}` }, { texto: p.titulo }]} />
      <div className="detalle-columnas">
        <div className="detalle-principal">
          <span className="etiqueta etiqueta-exito">−{p.descuentoPorcentaje} % armando el pack</span>
          <h1>{p.titulo}</h1>
          <p className="destino">
            <Icono nombre="ubicacion" /> {p.destino}
          </p>
          {p.descripcion && <p>{p.descripcion}</p>}
          <p className="bajada">Elegí al menos una opción de cada tipo. Las fechas tienen que encajar: la estadía empieza después de llegar y las excursiones son durante la estadía.</p>

          {TIPOS.map((t) => {
            const deTipo = p.opciones.filter((o) => o.tipo === t)
            return (
              <fieldset key={t} className="grupo-opciones">
                <legend>
                  <span aria-hidden="true">{iconoTipo[t]}</span> {nombreTipo[t]}
                  {opciones.some((o) => o.tipo === t) ? <span className="etiqueta etiqueta-exito"><Icono nombre="check" /> elegido</span> : <span className="etiqueta etiqueta-neutro">falta elegir</span>}
                </legend>
                {deTipo.map((o) => (
                  <label key={o.id} className={`opcion-pack ${elegidas.has(o.id) ? 'elegida' : ''}`}>
                    <input type="checkbox" checked={elegidas.has(o.id)} onChange={() => alternar(o.id)} />
                    <span className="opcion-pack-texto">
                      <strong>{o.titulo}</strong>
                      <small>
                        {fechaHora(o.desde)} → {fechaHora(o.hasta)}
                      </small>
                    </span>
                    <span className="precio">{moneda(o.precio)}</span>
                  </label>
                ))}
              </fieldset>
            )
          })}
        </div>

        <aside className="tarjeta reservar" aria-labelledby="titulo-resumen">
          <h2 id="titulo-resumen">Tu pack</h2>
          {opciones.length === 0 ? (
            <p className="ayuda-campo">Todavía no elegiste opciones.</p>
          ) : (
            <ul className="lista-resumen lista-sin-estilo">
              {opciones.map((o) => (
                <li key={o.id}>
                  <span>{o.titulo}</span>
                  <span>{moneda(o.precio)}</span>
                </li>
              ))}
            </ul>
          )}
          {opciones.length > 0 && (
            <dl className="totales">
              <div>
                <dt>Subtotal</dt>
                <dd>{moneda(subtotal * cantidad)}</dd>
              </div>
              <div>
                <dt>Descuento ({p.descuentoPorcentaje} %)</dt>
                <dd>−{moneda(subtotal * cantidad - total)}</dd>
              </div>
              <div className="total">
                <dt>Total</dt>
                <dd>{moneda(total)}</dd>
              </div>
            </dl>
          )}

          <p className={`requisito ${problema ? '' : 'cumplido'}`} aria-live="polite">
            <Icono nombre={problema ? 'info' : 'check'} />{' '}
            {problema ?? 'La combinación es válida.'}
          </p>

          {error !== undefined && <MensajeError error={error} />}
          {error instanceof ApiError && typeof error.datos.ofertaId === 'string' && (
            <p className="ayuda-campo">
              Falló: <strong>{p.opciones.find((o) => o.id === error.datos.ofertaId)?.titulo ?? error.datos.ofertaId}</strong>. No se reservó ninguna opción.
            </p>
          )}

          {!usuario ? (
            <Link to={'/ingresar?volver=' + encodeURIComponent(ubicacion.pathname)} className="boton boton-primario boton-ancho">
              Ingresá para reservar
            </Link>
          ) : usuario.rol === 'admin' ? (
            <p className="ayuda-campo">Las cuentas de administración no compran.</p>
          ) : (
            <>
              <SelectorCantidad valor={cantidad} max={MAX_CUPOS_POR_OFERTA} alCambiar={setCantidad} etiqueta="Personas" />
              <button type="button" className="boton boton-primario boton-ancho" disabled={!!problema || agregando} aria-busy={agregando} onClick={alAgregar}>
                {agregando ? 'Reservando todas las opciones…' : 'Agregar pack al carrito'}
              </button>
            </>
          )}
          <Ayuda pregunta="¿Cómo se reserva un pack?">
            Se reservan todas las opciones juntas o ninguna: si alguna no tiene lugar, no se reserva nada y te avisamos cuál falló. Quedan guardadas 15 minutos.
          </Ayuda>
        </aside>
      </div>
    </div>
  )
}
