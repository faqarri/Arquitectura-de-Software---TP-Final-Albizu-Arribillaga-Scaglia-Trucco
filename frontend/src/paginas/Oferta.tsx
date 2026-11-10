import { useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { mensajeDeError } from '../api/errores'
import type { Disponibilidad, Oferta as TOferta } from '../api/tipos'
import { useCarrito } from '../carrito/Carrito'
import { useAvisos } from '../componentes/Avisos'
import { Ayuda, Cargando, EstadoDisponibilidadEtiqueta, MensajeError, Migas, SelectorCantidad } from '../componentes/ui'
import { fechaHora, hora, moneda, nuevoId, plural } from '../formato'
import { useCadaTanto, useCarga, useTitulo } from '../hooks/hooks'
import { MAX_CUPOS_POR_OFERTA, nombreTipo } from '../reglas'
import { useSesion } from '../sesion/Sesion'
import { nombreSubtipo } from './Buscar'
import { Icono, IconoOferta } from '../componentes/Iconos'

const REFRESCO_DISPONIBILIDAD_MS = 15000

export function Oferta() {
  const { id = '' } = useParams()
  const oferta = useCarga(() => api.oferta(id), [id])
  useTitulo(oferta.datos?.titulo ?? 'Oferta')

  if (oferta.error !== undefined) {
    return (
      <div className="pagina-angosta">
        <Migas pasos={[{ texto: 'Buscar', a: '/' }, { texto: 'Oferta' }]} />
        <MensajeError error={oferta.error} alReintentar={oferta.recargar} />
      </div>
    )
  }
  if (!oferta.datos) return <Cargando texto="Cargando la oferta…" />
  const o = oferta.datos

  return (
    <div className="pagina-detalle">
      <Migas pasos={[{ texto: 'Buscar', a: '/' }, { texto: o.destino, a: `/?destino=${encodeURIComponent(o.destino)}` }, { texto: o.titulo }]} />
      <div className="detalle-columnas">
        <article className="detalle-principal">
          <span className="tipo">
            <IconoOferta tipo={o.tipo} subtipo={o.subtipoTransporte} /> {o.subtipoTransporte ? nombreSubtipo[o.subtipoTransporte] : nombreTipo[o.tipo]}
          </span>
          <h1>{o.titulo}</h1>
          <p className="destino">
            <Icono nombre="ubicacion" /> {o.origen ? `${o.origen} → ${o.destino}` : o.destino}</p>
          <p>{o.descripcion}</p>
          <dl className="datos">
            <div>
              <dt>{o.tipo === 'TRANSPORTE' ? 'Salida' : o.tipo === 'ESTADIA' ? 'Entrada' : 'Comienza'}</dt>
              <dd>{fechaHora(o.desde)}</dd>
            </div>
            <div>
              <dt>{o.tipo === 'TRANSPORTE' ? 'Llegada' : o.tipo === 'ESTADIA' ? 'Salida' : 'Termina'}</dt>
              <dd>{fechaHora(o.hasta)}</dd>
            </div>
            <div>
              <dt>Precio por persona</dt>
              <dd className="precio">{moneda(o.precio)}</dd>
            </div>
          </dl>
        </article>
        <Reservar oferta={o} />
      </div>
      <GuiasDelDestino oferta={o} />
    </div>
  )
}

function Reservar({ oferta: o }: { oferta: TOferta }) {
  const { usuario } = useSesion()
  const { items, agregar } = useCarrito()
  const avisar = useAvisos()
  const navegar = useNavigate()
  const ubicacion = useLocation()
  const disp = useCarga(() => api.disponibilidad(o.id), [o.id])
  const [cantidad, setCantidad] = useState(1)
  const [agregando, setAgregando] = useState(false)
  const [error, setError] = useState<unknown>()
  const [clave, setClave] = useState(() => nuevoId())

  // Sin el servicio de tiempo real, la disponibilidad se vuelve a consultar cada tanto.
  useCadaTanto(disp.recargar, REFRESCO_DISPONIBILIDAD_MS, !agregando)

  const enCarrito = items.filter((it) => it.ofertaId === o.id && Date.parse(it.expiraEn) > Date.now()).reduce((t, it) => t + it.cantidad, 0)
  const d: Disponibilidad | undefined = disp.datos
  const maximo = Math.max(0, Math.min(MAX_CUPOS_POR_OFERTA - enCarrito, d?.libres ?? 0))
  const cant = Math.min(cantidad, Math.max(1, maximo))

  async function alAgregar() {
    setAgregando(true)
    setError(undefined)
    try {
      await agregar({ ofertaId: o.id, cantidad: cant }, clave)
      setClave(nuevoId())
      setCantidad(1)
      avisar({
        tipo: 'exito',
        titulo: 'Agregado al carrito',
        detalle: `Te guardamos ${plural(cant, 'lugar', 'lugares')} por 15 minutos.`,
        accion: { etiqueta: 'Ver carrito', alHacer: () => navegar('/carrito') },
      })
      disp.recargar()
    } catch (e) {
      // Un error que no es reintentable cambia la situación: el próximo intento es otro pedido.
      if (!mensajeDeError(e).reintentable) setClave(nuevoId())
      setError(e)
      disp.recargar()
    } finally {
      setAgregando(false)
    }
  }

  return (
    <aside className="tarjeta reservar" aria-labelledby="titulo-reservar">
      <h2 id="titulo-reservar">Reservar</h2>
      <p className="precio precio-grande">
        {moneda(o.precio * cant)}
        {cant > 1 && <small> · {cant} × {moneda(o.precio)}</small>}
      </p>

      {disp.error !== undefined && !d ? (
        <MensajeError error={disp.error} alReintentar={disp.recargar} />
      ) : !d ? (
        <Cargando texto="Consultando lugares…" />
      ) : (
        <div className="disponibilidad" aria-live="polite">
          <EstadoDisponibilidadEtiqueta estado={d.estado} />
          {d.estado === 'DISPONIBLE' && <p>{d.libres <= 5 ? `¡Quedan ${plural(d.libres, 'lugar', 'lugares')}!` : `Quedan ${d.libres} lugares.`}</p>}
          {d.estado === 'RETENIDA' && (
            <p>
              Hay lugares reservados en carritos de otros viajeros que pueden liberarse
              {d.proximaLiberacion ? <> a partir de las <strong>{hora(d.proximaLiberacion)}</strong></> : ' en unos minutos'}. Esta pantalla se actualiza sola.
            </p>
          )}
          {d.estado === 'AGOTADA' && <p>Se vendieron todos los lugares. Probá con otra fecha u otra oferta de {o.destino}.</p>}
        </div>
      )}

      {enCarrito > 0 && (
        <p className="caja caja-info">
          Ya tenés {plural(enCarrito, 'lugar', 'lugares')} de esta oferta en tu <Link to="/carrito">carrito</Link>.
        </p>
      )}

      {error !== undefined && <MensajeError error={error} />}

      {!usuario ? (
        <Link to={'/ingresar?volver=' + encodeURIComponent(ubicacion.pathname)} className="boton boton-primario boton-ancho">
          Ingresá para reservar
        </Link>
      ) : usuario.rol === 'admin' ? (
        <p className="ayuda-campo">Las cuentas de administración no compran.</p>
      ) : d?.estado === 'DISPONIBLE' && maximo > 0 ? (
        <>
          <SelectorCantidad valor={cant} max={maximo} alCambiar={setCantidad} etiqueta="Personas" />
          {maximo < MAX_CUPOS_POR_OFERTA && (
            <small className="ayuda-campo">
              {enCarrito > 0 && d.libres > MAX_CUPOS_POR_OFERTA - enCarrito
                ? `Podés reservar hasta ${MAX_CUPOS_POR_OFERTA} lugares por oferta.`
                : `Máximo ${maximo} (lugares disponibles).`}
            </small>
          )}
          <button type="button" className="boton boton-primario boton-ancho" onClick={alAgregar} disabled={agregando} aria-busy={agregando}>
            {agregando ? 'Reservando tu lugar…' : 'Agregar al carrito'}
          </button>
        </>
      ) : d?.estado === 'DISPONIBLE' ? (
        <p className="ayuda-campo">Ya tenés el máximo de {MAX_CUPOS_POR_OFERTA} lugares de esta oferta en tu carrito.</p>
      ) : (
        <button type="button" className="boton boton-primario boton-ancho" disabled>
          Sin lugares para agregar
        </button>
      )}

      <Ayuda pregunta="¿Qué pasa al agregar al carrito?">
        Te guardamos el lugar durante 15 minutos para que nadie más lo tome mientras terminás la compra. Si no comprás en ese tiempo, la reserva vence y el lugar se libera.
        El precio final se toma al confirmar la compra.
      </Ayuda>
    </aside>
  )
}

function GuiasDelDestino({ oferta: o }: { oferta: TOferta }) {
  const guias = useCarga(() => api.guiasDelDestino(o.id), [o.id])
  return (
    <section className="seccion guias-destino" aria-labelledby="titulo-guias">
      <h2 id="titulo-guias">Guías de viajeros sobre {o.destino}</h2>
      {guias.cargando ? (
        <Cargando texto="Buscando guías…" />
      ) : guias.error !== undefined || !guias.datos?.disponible ? (
        <p className="caja caja-neutra">
          <Icono nombre="info" /> Guías no disponibles por el momento. El resto de la oferta funciona con normalidad.
        </p>
      ) : guias.datos.guias.length === 0 ? (
        <p>
          Todavía nadie compartió una guía de {o.destino}. Si viajaste, podés <Link to={`/guias/nueva?destino=${encodeURIComponent(o.destino)}`}>compartir la tuya</Link>.
        </p>
      ) : (
        <ul className="lista-guias lista-sin-estilo">
          {guias.datos.guias.map((g) => (
            <li key={g.id} className="tarjeta">
              <div className="encabezado-seccion">
                <h3>{g.titulo}</h3>
                <span className="calificacion">
                  {g.calificacion === null ? (
                    'Sin calificaciones'
                  ) : (
                    <>
                      <Icono nombre="estrella" relleno />{' '}
                      {g.calificacion.toLocaleString('es-AR')}
                      <span className="solo-lectores"> de 5</span> <small>({g.cantidadCalificaciones})</small>
                    </>
                  )}
                </span>
              </div>
              {g.descripcion && <p>{g.descripcion}</p>}
              <p className="ayuda-campo">
                Por {g.autor}
                {g.etiquetas.length > 0 && ' · ' + g.etiquetas.join(', ')}
              </p>
              {g.urlDescarga && (
                <a href={g.urlDescarga} className="boton boton-chico boton-secundario">
                  Descargar PDF
                </a>
              )}
            </li>
          ))}
        </ul>
      )}
      <p className="ayuda-campo">Las guías las publica Estudiario, un servicio externo que las modera.</p>
    </section>
  )
}
