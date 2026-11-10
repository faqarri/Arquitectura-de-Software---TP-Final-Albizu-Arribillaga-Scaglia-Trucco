import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { mensajeDeError } from '../api/errores'
import type { ItemCarrito } from '../api/tipos'
import { useCarrito } from '../carrito/Carrito'
import { useAvisos } from '../componentes/Avisos'
import { Ayuda, Cargando, CuentaRegresiva, Dialogo, MensajeError, Vacio } from '../componentes/ui'
import { moneda, nuevoId, plural } from '../formato'
import { useAhora, useTitulo } from '../hooks/hooks'
import { Icono } from '../componentes/Iconos'

// La clave de idempotencia de la compra se guarda hasta tener una respuesta:
// si se corta la conexión y el usuario reintenta, no se crea una segunda compra.
const CLAVE_COMPRA = 'agencia.compra-en-curso'

function claveDeCompra(): string {
  try {
    const previa = sessionStorage.getItem(CLAVE_COMPRA)
    if (previa) return previa
    const nueva = nuevoId()
    sessionStorage.setItem(CLAVE_COMPRA, nueva)
    return nueva
  } catch {
    return nuevoId()
  }
}

function olvidarClaveDeCompra() {
  try {
    sessionStorage.removeItem(CLAVE_COMPRA)
  } catch {
    // nada
  }
}

export function CarritoPagina() {
  useTitulo('Carrito')
  const { items, cargando, error, recargar, quitar, agregar } = useCarrito()
  const ahora = useAhora()
  const navegar = useNavigate()
  const avisar = useAvisos()
  const [confirmando, setConfirmando] = useState(false)
  const [comprando, setComprando] = useState(false)
  const [errorCompra, setErrorCompra] = useState<unknown>()
  const [reagregando, setReagregando] = useState<string>()

  const activos = items.filter((it) => Date.parse(it.expiraEn) > ahora)
  const vencidos = items.filter((it) => Date.parse(it.expiraEn) <= ahora)
  const total = activos.reduce((t, it) => t + it.precioReferencia, 0)

  async function comprar() {
    setComprando(true)
    setErrorCompra(undefined)
    try {
      const compra = await api.comprar(claveDeCompra())
      olvidarClaveDeCompra()
      setConfirmando(false)
      await recargar()
      navegar(`/compras/${compra.id}`, { state: { recienCreada: true } })
    } catch (e) {
      if (!mensajeDeError(e).reintentable) olvidarClaveDeCompra()
      setErrorCompra(e)
    } finally {
      setComprando(false)
    }
  }

  async function volverAAgregar(it: ItemCarrito) {
    if (!it.ofertaId) {
      navegar(`/packs/${it.packId}`)
      return
    }
    setReagregando(it.id)
    try {
      await agregar({ ofertaId: it.ofertaId, cantidad: it.cantidad }, nuevoId())
      await api.quitarDelCarrito(it.id)
      await recargar()
      avisar({ tipo: 'exito', titulo: 'Lo volvimos a reservar', detalle: `${it.descripcion}: tenés 15 minutos más.` })
    } catch (e) {
      const m = mensajeDeError(e)
      avisar({ tipo: 'error', titulo: m.titulo, detalle: m.detalle })
    } finally {
      setReagregando(undefined)
    }
  }

  if (error !== undefined && items.length === 0) {
    return (
      <div className="pagina-angosta">
        <h1>Tu carrito</h1>
        <MensajeError error={error} alReintentar={recargar} />
      </div>
    )
  }

  return (
    <div className="pagina-carrito">
      <h1>Tu carrito</h1>
      <Pasos actual={1} />
      {cargando && items.length === 0 ? (
        <Cargando texto="Cargando tu carrito…" />
      ) : items.length === 0 ? (
        <Vacio titulo="Tu carrito está vacío">
          <p>Buscá un destino y agregá transportes, estadías o excursiones.</p>
          <Link to="/" className="boton boton-primario">
            Buscar ofertas
          </Link>
        </Vacio>
      ) : (
        <div className="detalle-columnas">
          <section aria-label="Ítems del carrito">
            {vencidos.length > 0 && (
              <div className="caja caja-aviso" role="status">
                <strong>{vencidos.length === 1 ? 'Venció la reserva de un ítem' : `Vencieron las reservas de ${vencidos.length} ítems`}</strong>
                <p>Su lugar se liberó. Podés volver a agregarlos si todavía hay cupo, o quitarlos para comprar el resto.</p>
              </div>
            )}
            <ul className="lista-sin-estilo lista-items">
              {items.map((it) => {
                const vencido = Date.parse(it.expiraEn) <= ahora
                return (
                  <li key={it.id} className={`tarjeta item-carrito ${vencido ? 'vencido' : ''}`}>
                    <div className="item-carrito-texto">
                      <h2 className="h3">
                        {it.ofertaId ? <Link to={`/ofertas/${it.ofertaId}`}>{it.descripcion}</Link> : <Link to={`/packs/${it.packId}`}>{it.descripcion}</Link>}
                      </h2>
                      <p className="destino">
                        <Icono nombre="ubicacion" /> {it.destino} · {plural(it.cantidad, 'persona', 'personas')}
                      </p>
                      <CuentaRegresiva expiraEn={it.expiraEn} />
                    </div>
                    <div className="item-carrito-acciones">
                      <span className={`precio ${vencido ? 'tachado' : ''}`}>{moneda(it.precioReferencia)}</span>
                      {vencido && (
                        <button type="button" className="boton boton-chico boton-primario" disabled={reagregando === it.id} onClick={() => volverAAgregar(it)}>
                          {reagregando === it.id ? 'Reservando…' : 'Volver a agregar'}
                        </button>
                      )}
                      <button type="button" className="boton boton-chico boton-secundario" onClick={() => quitar(it)} aria-label={`Quitar ${it.descripcion}`}>
                        Quitar
                      </button>
                    </div>
                  </li>
                )
              })}
            </ul>
            <Ayuda pregunta="¿Por qué hay una cuenta regresiva?">
              Al agregar algo al carrito te guardamos el lugar 15 minutos, así nadie más lo toma mientras terminás. Cuando el tiempo llega a cero, la reserva vence y el lugar
              vuelve a estar disponible para otros.
            </Ayuda>
          </section>

          <aside className="tarjeta reservar" aria-labelledby="titulo-total">
            <h2 id="titulo-total">Resumen</h2>
            <dl className="totales">
              <div className="total">
                <dt>Total de referencia</dt>
                <dd>{moneda(total)}</dd>
              </div>
            </dl>
            <p className="ayuda-campo">El precio final se toma al confirmar la compra. Te mandamos la factura por email.</p>
            {errorCompra !== undefined && !confirmando && <MensajeError error={errorCompra} alReintentar={() => setConfirmando(true)} />}
            <button type="button" className="boton boton-primario boton-ancho" disabled={activos.length === 0 || vencidos.length > 0} onClick={() => setConfirmando(true)}>
              Comprar {activos.length > 0 && `(${plural(activos.length, 'ítem', 'ítems')})`}
            </button>
            {vencidos.length > 0 && <small className="ayuda-campo">Antes de comprar, volvé a agregar o quitá los ítems vencidos.</small>}
            <Link to="/" className="boton boton-secundario boton-ancho">
              Seguir buscando
            </Link>
          </aside>
        </div>
      )}

      <Dialogo
        abierto={confirmando}
        titulo="¿Confirmás la compra?"
        confirmar={`Confirmar y pagar ${moneda(total)}`}
        cancelar="Volver al carrito"
        ocupado={comprando}
        alConfirmar={comprar}
        alCerrar={() => setConfirmando(false)}
      >
        <ul className="lista-resumen lista-sin-estilo">
          {activos.map((it) => (
            <li key={it.id}>
              <span>
                {it.descripcion} <small>× {it.cantidad}</small>
              </span>
              <span>{moneda(it.precioReferencia)}</span>
            </li>
          ))}
        </ul>
        <p className="ayuda-campo">El pago es simulado. Se compran todos los ítems o ninguno.</p>
        {errorCompra !== undefined && <MensajeError error={errorCompra} />}
      </Dialogo>
    </div>
  )
}

/** Dónde está el usuario dentro de la compra. */
export function Pasos({ actual }: { actual: 1 | 2 | 3 }) {
  const pasos = ['Carrito', 'Confirmación', 'Factura por email']
  return (
    <ol className="pasos" aria-label="Pasos de la compra">
      {pasos.map((p, i) => (
        <li key={p} className={i + 1 < actual ? 'hecho' : i + 1 === actual ? 'actual' : ''} aria-current={i + 1 === actual ? 'step' : undefined}>
          <span className="pasos-numero" aria-hidden="true">
            {i + 1 < actual ? <Icono nombre="check" /> : i + 1}
          </span>
          {p}
        </li>
      ))}
    </ol>
  )
}
