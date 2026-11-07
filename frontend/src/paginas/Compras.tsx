import { useEffect } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api'
import type { Compra } from '../api/tipos'
import { useCarrito } from '../carrito/Carrito'
import { Cargando, EstadoCompraEtiqueta, MensajeError, Migas, Vacio } from '../componentes/ui'
import { fechaHora, moneda, plural } from '../formato'
import { useCadaTanto, useCarga, useTitulo } from '../hooks/hooks'
import { useSesion } from '../sesion/Sesion'
import { Pasos } from './CarritoPagina'
import { Icono } from '../componentes/Iconos'

const CONSULTA_PENDIENTE_MS = 3000

export function CompraPagina() {
  const { id = '' } = useParams()
  const compra = useCarga(() => api.compra(id), [id])
  const { recargar: recargarCarrito } = useCarrito()
  const { usuario } = useSesion()
  const c = compra.datos
  useTitulo(c?.estado === 'CONFIRMADA' ? 'Compra confirmada' : 'Tu compra')

  // Una compra PENDIENTE se resuelve sola en el servicio: se consulta hasta que termine.
  useCadaTanto(compra.recargar, CONSULTA_PENDIENTE_MS, c?.estado === 'PENDIENTE')
  useEffect(() => {
    if (c && c.estado !== 'PENDIENTE') void recargarCarrito()
  }, [c?.estado, recargarCarrito, c])

  if (compra.error !== undefined && !c) return <div className="pagina-angosta"><MensajeError error={compra.error} alReintentar={compra.recargar} /></div>
  if (!c) return <Cargando texto="Cargando la compra…" />

  const destinos = [...new Set(c.lineas.map((l) => l.destino))]
  return (
    <div className="pagina-angosta">
      <Migas pasos={[{ texto: 'Mis compras', a: '/compras' }, { texto: `Compra del ${fechaHora(c.creadaEn)}` }]} />
      <Pasos actual={c.estado === 'CONFIRMADA' ? 3 : 2} />

      {c.estado === 'PENDIENTE' && (
        <div className="resultado resultado-info" role="status">
          <span className="girador girador-grande" aria-hidden="true" />
          <h1>Estamos procesando tu compra</h1>
          <p>Tu lugar sigue reservado. Puede tardar unos segundos; esta pantalla se actualiza sola. Podés cerrarla: el resultado va a aparecer en "Mis compras".</p>
        </div>
      )}
      {c.estado === 'CONFIRMADA' && (
        <div className="resultado resultado-exito" role="status">
          <span className="resultado-icono">
            <Icono nombre="check" />
          </span>
          <h1>¡Listo! Tu compra está confirmada</h1>
          <p>
            Te enviamos la factura por email{usuario ? <> a <strong>{usuario.email}</strong></> : ''}. Si no la ves en unos minutos, revisá la carpeta de spam.
          </p>
        </div>
      )}
      {c.estado === 'RECHAZADA' && (
        <div className="resultado resultado-error" role="alert">
          <span className="resultado-icono">
            <Icono nombre="x" />
          </span>
          <h1>No pudimos confirmar tu compra</h1>
          <p>
            Venció la reserva de {plural(c.retencionesVencidas?.length ?? 1, 'ítem', 'ítems')} antes de confirmar, así que no se compró nada y no se cobró nada. Los demás ítems
            siguen reservados en tu carrito.
          </p>
          <Link to="/carrito" className="boton boton-primario">
            Volver al carrito
          </Link>
        </div>
      )}

      {c.lineas.length > 0 && <DetalleCompra compra={c} />}

      {c.estado === 'CONFIRMADA' && (
        <div className="acciones acciones-izquierda">
          {destinos.map((d) => (
            <Link key={d} to={`/guias/nueva?destino=${encodeURIComponent(d)}`} className="boton boton-secundario">
              Compartir una guía de {d}
            </Link>
          ))}
          <Link to="/" className="boton boton-primario">
            Seguir buscando
          </Link>
        </div>
      )}
    </div>
  )
}

function DetalleCompra({ compra: c }: { compra: Compra }) {
  return (
    <section className="tarjeta" aria-label="Detalle de la compra">
      <ul className="lista-resumen lista-sin-estilo">
        {c.lineas.map((l, i) => (
          <li key={i}>
            <span>
              {l.descripcion} <small>· {l.destino}</small>
            </span>
            <span>{moneda(l.precio)}</span>
          </li>
        ))}
      </ul>
      <dl className="totales">
        <div className="total">
          <dt>Total</dt>
          <dd>{moneda(c.total)}</dd>
        </div>
      </dl>
    </section>
  )
}

export function MisCompras() {
  useTitulo('Mis compras')
  const compras = useCarga(() => api.compras(), [])
  const hayPendientes = compras.datos?.some((c) => c.estado === 'PENDIENTE') ?? false
  useCadaTanto(compras.recargar, CONSULTA_PENDIENTE_MS, hayPendientes)

  return (
    <div className="pagina-angosta">
      <h1>Mis compras</h1>
      {compras.error !== undefined && !compras.datos ? (
        <MensajeError error={compras.error} alReintentar={compras.recargar} />
      ) : !compras.datos ? (
        <Cargando texto="Cargando tus compras…" />
      ) : compras.datos.length === 0 ? (
        <Vacio titulo="Todavía no hiciste compras">
          <Link to="/" className="boton boton-primario">
            Buscar ofertas
          </Link>
        </Vacio>
      ) : (
        <ul className="lista-sin-estilo lista-items">
          {compras.datos.map((c) => (
            <li key={c.id}>
              <Link to={`/compras/${c.id}`} className="tarjeta tarjeta-enlace fila-compra">
                <span>
                  <strong>{[...new Set(c.lineas.map((l) => l.destino))].join(', ') || 'Compra'}</strong>
                  <small className="ayuda-campo"> {fechaHora(c.creadaEn)} · {plural(c.lineas.length, 'ítem', 'ítems')}</small>
                </span>
                <EstadoCompraEtiqueta estado={c.estado} />
                <span className="precio">{c.estado === 'RECHAZADA' ? '—' : moneda(c.total)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
