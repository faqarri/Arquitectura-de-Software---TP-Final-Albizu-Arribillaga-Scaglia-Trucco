import { useState, type FormEvent } from 'react'
import { Link, NavLink, useNavigate, useParams } from 'react-router-dom'
import { api } from '../../api'
import { ApiError } from '../../api/errores'
import type { Oferta, OfertaNueva, SubtipoTransporte, TipoOferta } from '../../api/tipos'
import { useAvisos } from '../../componentes/Avisos'
import { Campo, Cargando, Dialogo, MensajeError, Migas, Paginacion, Vacio } from '../../componentes/ui'
import { fecha, moneda } from '../../formato'
import { useCarga, useTitulo } from '../../hooks/hooks'
import { nombreTipo } from '../../reglas'
import { iconoTipo, nombreSubtipo } from '../Buscar'
import { Icono, IconoOferta } from '../../componentes/Iconos'

const TAMANIO = 20

function Pestanias() {
  return (
    <nav className="pestanias" aria-label="Secciones de administración">
      <NavLink to="/admin" end>
        Ofertas
      </NavLink>
      <NavLink to="/admin/packs">Packs</NavLink>
    </nav>
  )
}

export function AdminOfertas() {
  useTitulo('Administrar ofertas')
  const [pagina, setPagina] = useState(1)
  const [texto, setTexto] = useState('')
  const [busqueda, setBusqueda] = useState('')
  const ofertas = useCarga(() => api.buscar({ q: busqueda || undefined, pagina, tamanio: TAMANIO, orden: 'fecha' }), [pagina, busqueda])
  const [aBorrar, setABorrar] = useState<Oferta>()
  const [borrando, setBorrando] = useState(false)
  const avisar = useAvisos()

  async function despublicar() {
    if (!aBorrar) return
    setBorrando(true)
    try {
      await api.despublicarOferta(aBorrar.id)
      avisar({ tipo: 'exito', titulo: 'Oferta despublicada', detalle: aBorrar.titulo })
      setABorrar(undefined)
      ofertas.recargar()
    } catch (e) {
      avisar({ tipo: 'error', titulo: 'No se pudo despublicar', detalle: e instanceof ApiError ? e.detalle : undefined })
    } finally {
      setBorrando(false)
    }
  }

  return (
    <div className="pagina-ancha">
      <div className="encabezado-seccion">
        <h1>Administración</h1>
        <Link to="/admin/ofertas/nueva" className="boton boton-primario">
          + Nueva oferta
        </Link>
      </div>
      <Pestanias />
      <form
        className="buscador-fila"
        role="search"
        onSubmit={(e) => {
          e.preventDefault()
          setPagina(1)
          setBusqueda(texto.trim())
        }}
      >
        <label className="campo campo-crece">
          <span>Buscar por título o destino</span>
          <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} />
        </label>
        <button type="submit" className="boton boton-secundario">
          Buscar
        </button>
      </form>

      {ofertas.error !== undefined ? (
        <MensajeError error={ofertas.error} alReintentar={ofertas.recargar} />
      ) : !ofertas.datos ? (
        <Cargando />
      ) : ofertas.datos.items.length === 0 ? (
        <Vacio titulo="No hay ofertas que coincidan" />
      ) : (
        <>
          <div className="tabla-contenedor">
            <table className="tabla">
              <caption className="solo-lectores">Ofertas publicadas</caption>
              <thead>
                <tr>
                  <th scope="col">Oferta</th>
                  <th scope="col">Destino</th>
                  <th scope="col">Fecha</th>
                  <th scope="col" className="numero">Precio</th>
                  <th scope="col" className="numero">Vendidos / cupo</th>
                  <th scope="col"><span className="solo-lectores">Acciones</span></th>
                </tr>
              </thead>
              <tbody>
                {ofertas.datos.items.map((o) => (
                  <tr key={o.id}>
                    <td>
                      <IconoOferta tipo={o.tipo} subtipo={o.subtipoTransporte} />{' '}
                      {o.titulo}
                    </td>
                    <td>{o.destino}</td>
                    <td>{fecha(o.desde)}</td>
                    <td className="numero">{moneda(o.precio)}</td>
                    <td className="numero">{o.cupoTotal !== undefined ? `${o.vendidos ?? 0} / ${o.cupoTotal}` : '—'}</td>
                    <td className="acciones-tabla">
                      <Link to={`/admin/ofertas/${o.id}`} className="boton boton-chico boton-secundario" aria-label={`Editar ${o.titulo}`}>
                        Editar
                      </Link>
                      <button type="button" className="boton boton-chico boton-peligro-suave" aria-label={`Despublicar ${o.titulo}`} onClick={() => setABorrar(o)}>
                        Despublicar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Paginacion pagina={pagina} paginas={Math.ceil(ofertas.datos.total / TAMANIO)} alCambiar={setPagina} />
        </>
      )}

      <Dialogo
        abierto={!!aBorrar}
        titulo="¿Despublicar la oferta?"
        confirmar="Despublicar"
        cancelar="No, dejarla"
        peligroso
        ocupado={borrando}
        alConfirmar={despublicar}
        alCerrar={() => setABorrar(undefined)}
      >
        <p>
          <strong>{aBorrar?.titulo}</strong> deja de aparecer en la búsqueda. Las compras ya hechas no cambian.
        </p>
      </Dialogo>
    </div>
  )
}

const vacia: OfertaNueva = { tipo: 'TRANSPORTE', subtipoTransporte: 'AVION', titulo: '', descripcion: '', destino: '', origen: '', desde: '', hasta: '', precio: 0, cupoTotal: 1 }

const aLocal = (iso: string) => {
  if (!iso) return ''
  const d = new Date(iso)
  const tz = d.getTimezoneOffset() * 60_000
  return new Date(d.getTime() - tz).toISOString().slice(0, 16)
}

export function EditarOferta() {
  const { id } = useParams()
  const nueva = !id
  const existente = useCarga(() => (id ? api.oferta(id) : Promise.resolve(undefined)), [id])
  useTitulo(nueva ? 'Nueva oferta' : 'Editar oferta')
  if (existente.error !== undefined) return <div className="pagina-angosta"><MensajeError error={existente.error} alReintentar={existente.recargar} /></div>
  if (!nueva && !existente.datos) return <Cargando />
  return <FormularioOferta key={id ?? 'nueva'} id={id} inicial={existente.datos} />
}

function FormularioOferta({ id, inicial }: { id?: string; inicial?: Oferta }) {
  const navegar = useNavigate()
  const avisar = useAvisos()
  const [o, setO] = useState<OfertaNueva>(() =>
    inicial
      ? { ...vacia, ...inicial, desde: aLocal(inicial.desde), hasta: aLocal(inicial.hasta), cupoTotal: inicial.cupoTotal ?? 1 }
      : vacia,
  )
  const [errores, setErrores] = useState<Record<string, string>>({})
  const [errorServidor, setErrorServidor] = useState<unknown>()
  const [guardando, setGuardando] = useState(false)
  const vendidos = inicial?.vendidos ?? 0
  const set = (c: Partial<OfertaNueva>) => setO((x) => ({ ...x, ...c }))

  function validar() {
    const e: Record<string, string> = {}
    if (o.titulo.trim().length < 3) e.titulo = 'Al menos 3 caracteres.'
    if (!o.destino.trim()) e.destino = 'Falta el destino.'
    if (o.tipo === 'TRANSPORTE' && !o.origen?.trim()) e.origen = 'Falta el origen.'
    if (!o.desde) e.desde = 'Falta la fecha de inicio.'
    if (!o.hasta) e.hasta = 'Falta la fecha de fin.'
    else if (o.desde && o.hasta < o.desde) e.hasta = 'No puede ser anterior al inicio.'
    if (!(o.precio > 0)) e.precio = 'Tiene que ser mayor a 0.'
    if (!(o.cupoTotal >= 1)) e.cupoTotal = 'Al menos 1 lugar.'
    else if (o.cupoTotal < vendidos) e.cupoTotal = `No puede ser menor a los ${vendidos} lugares ya vendidos.`
    return e
  }

  async function guardar(ev: FormEvent) {
    ev.preventDefault()
    const e = validar()
    setErrores(e)
    setErrorServidor(undefined)
    const primero = Object.keys(e)[0]
    if (primero) {
      document.getElementById('oferta-' + primero)?.focus()
      return
    }
    setGuardando(true)
    const cuerpo: OfertaNueva = {
      ...o,
      titulo: o.titulo.trim(),
      destino: o.destino.trim(),
      subtipoTransporte: o.tipo === 'TRANSPORTE' ? o.subtipoTransporte : undefined,
      origen: o.tipo === 'TRANSPORTE' ? o.origen?.trim() : undefined,
      desde: new Date(o.desde).toISOString(),
      hasta: new Date(o.hasta).toISOString(),
    }
    try {
      if (id) await api.editarOferta(id, cuerpo)
      else await api.crearOferta(cuerpo)
      avisar({ tipo: 'exito', titulo: id ? 'Cambios guardados' : 'Oferta publicada', detalle: 'Aparece en la búsqueda en unos segundos.' })
      navegar('/admin')
    } catch (err) {
      if (err instanceof ApiError && err.errores.length) setErrores(Object.fromEntries(err.errores.map((x) => [x.campo, x.mensaje])))
      else setErrorServidor(err)
    } finally {
      setGuardando(false)
    }
  }

  return (
    <div className="pagina-angosta">
      <Migas pasos={[{ texto: 'Administración', a: '/admin' }, { texto: id ? 'Editar oferta' : 'Nueva oferta' }]} />
      <h1>{id ? 'Editar oferta' : 'Nueva oferta'}</h1>
      <form className="formulario tarjeta" onSubmit={guardar} noValidate>
        {errorServidor !== undefined && <MensajeError error={errorServidor} />}
        <fieldset className="tipos">
          <legend>Tipo</legend>
          {(['TRANSPORTE', 'ESTADIA', 'EXCURSION'] as TipoOferta[]).map((t) => (
            <label key={t} className={`opcion-pastilla ${o.tipo === t ? 'elegida' : ''}`}>
              <input type="radio" name="tipo" checked={o.tipo === t} disabled={!!id} onChange={() => set({ tipo: t })} />
              {iconoTipo[t]} {nombreTipo[t]}
            </label>
          ))}
        </fieldset>
        {id && <small className="ayuda-campo">El tipo no se puede cambiar una vez publicada.</small>}
        {o.tipo === 'TRANSPORTE' && (
          <div className="rejilla-filtros">
            <Campo etiqueta="Medio">
              {(p) => (
                <select {...p} value={o.subtipoTransporte} onChange={(e) => set({ subtipoTransporte: e.target.value as SubtipoTransporte })}>
                  {Object.entries(nombreSubtipo).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
              )}
            </Campo>
            <Campo etiqueta="Origen" error={errores.origen}>
              {(p) => <input {...p} id="oferta-origen" value={o.origen ?? ''} onChange={(e) => set({ origen: e.target.value })} />}
            </Campo>
          </div>
        )}
        <Campo etiqueta="Título" error={errores.titulo}>
          {(p) => <input {...p} id="oferta-titulo" maxLength={150} value={o.titulo} onChange={(e) => set({ titulo: e.target.value })} />}
        </Campo>
        <Campo etiqueta="Destino" error={errores.destino} ayuda="Escribilo siempre igual (por ejemplo, Iguazú con tilde): se usa para buscar y para las guías.">
          {(p) => <input {...p} id="oferta-destino" value={o.destino} onChange={(e) => set({ destino: e.target.value })} />}
        </Campo>
        <Campo etiqueta="Descripción" opcional>
          {(p) => <textarea {...p} rows={3} value={o.descripcion} onChange={(e) => set({ descripcion: e.target.value })} />}
        </Campo>
        <div className="rejilla-filtros">
          <Campo etiqueta="Inicio" error={errores.desde}>
            {(p) => <input {...p} id="oferta-desde" type="datetime-local" value={o.desde} onChange={(e) => set({ desde: e.target.value })} />}
          </Campo>
          <Campo etiqueta="Fin" error={errores.hasta}>
            {(p) => <input {...p} id="oferta-hasta" type="datetime-local" min={o.desde} value={o.hasta} onChange={(e) => set({ hasta: e.target.value })} />}
          </Campo>
          <Campo etiqueta="Precio por persona ($)" error={errores.precio}>
            {(p) => <input {...p} id="oferta-precio" type="number" min={1} step={100} value={o.precio || ''} onChange={(e) => set({ precio: Number(e.target.value) })} />}
          </Campo>
          <Campo etiqueta="Cupo total" error={errores.cupoTotal} ayuda={id ? `Ya se vendieron ${vendidos}.` : undefined}>
            {(p) => <input {...p} id="oferta-cupoTotal" type="number" min={Math.max(1, vendidos)} value={o.cupoTotal || ''} onChange={(e) => set({ cupoTotal: Number(e.target.value) })} />}
          </Campo>
        </div>
        <div className="acciones">
          <Link to="/admin" className="boton boton-secundario">
            Cancelar
          </Link>
          <button type="submit" className="boton boton-primario" disabled={guardando} aria-busy={guardando}>
            {guardando ? 'Guardando…' : id ? 'Guardar cambios' : 'Publicar oferta'}
          </button>
        </div>
      </form>
    </div>
  )
}

export function AdminPacks() {
  useTitulo('Administrar packs')
  const packs = useCarga(() => api.packs(), [])
  return (
    <div className="pagina-ancha">
      <div className="encabezado-seccion">
        <h1>Administración</h1>
        <Link to="/admin/packs/nuevo" className="boton boton-primario">
          + Nuevo pack
        </Link>
      </div>
      <Pestanias />
      {packs.error !== undefined ? (
        <MensajeError error={packs.error} alReintentar={packs.recargar} />
      ) : !packs.datos ? (
        <Cargando />
      ) : packs.datos.length === 0 ? (
        <Vacio titulo="Todavía no hay packs" />
      ) : (
        <ul className="rejilla lista-sin-estilo">
          {packs.datos.map((p) => (
            <li key={p.id} className="tarjeta">
              <span className="etiqueta etiqueta-exito">−{p.descuentoPorcentaje} %</span>
              <h2 className="h3">{p.titulo}</h2>
              <p className="destino">
                <Icono nombre="ubicacion" /> {p.destino}
              </p>
              <p className="ayuda-campo">{p.opciones.length} opciones ofrecidas</p>
              <Link to={`/packs/${p.id}`}>Ver como cliente →</Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function NuevoPack() {
  useTitulo('Nuevo pack')
  const navegar = useNavigate()
  const avisar = useAvisos()
  const [destino, setDestino] = useState('')
  const ofertas = useCarga(() => (destino ? api.buscar({ destino, tamanio: 50 }) : Promise.resolve(undefined)), [destino])
  const [titulo, setTitulo] = useState('')
  const [descripcion, setDescripcion] = useState('')
  const [descuento, setDescuento] = useState(10)
  const [opciones, setOpciones] = useState<Set<string>>(new Set())
  const [errores, setErrores] = useState<Record<string, string>>({})
  const [errorServidor, setErrorServidor] = useState<unknown>()
  const [guardando, setGuardando] = useState(false)

  const elegidas = (ofertas.datos?.items ?? []).filter((o) => opciones.has(o.id))
  const faltanTipos = (['TRANSPORTE', 'ESTADIA', 'EXCURSION'] as TipoOferta[]).filter((t) => !elegidas.some((o) => o.tipo === t))

  async function guardar(ev: FormEvent) {
    ev.preventDefault()
    const e: Record<string, string> = {}
    if (!destino.trim()) e.destino = 'Escribí el destino.'
    if (titulo.trim().length < 3) e.titulo = 'Al menos 3 caracteres.'
    if (!(descuento >= 1 && descuento <= 99)) e.descuentoPorcentaje = 'Entre 1 y 99 %.'
    if (faltanTipos.length) e.opciones = 'Ofrecé al menos una opción de ' + faltanTipos.map((t) => nombreTipo[t].toLowerCase()).join(', ') + '.'
    setErrores(e)
    setErrorServidor(undefined)
    if (Object.keys(e).length) return
    setGuardando(true)
    try {
      await api.crearPack({ titulo: titulo.trim(), destino: destino.trim(), descripcion: descripcion.trim(), descuentoPorcentaje: descuento, opciones: [...opciones] })
      avisar({ tipo: 'exito', titulo: 'Pack creado' })
      navegar('/admin/packs')
    } catch (err) {
      if (err instanceof ApiError && err.errores.length) setErrores(Object.fromEntries(err.errores.map((x) => [x.campo, x.mensaje])))
      else setErrorServidor(err)
    } finally {
      setGuardando(false)
    }
  }

  return (
    <div className="pagina-angosta">
      <Migas pasos={[{ texto: 'Administración', a: '/admin' }, { texto: 'Packs', a: '/admin/packs' }, { texto: 'Nuevo pack' }]} />
      <h1>Nuevo pack</h1>
      <form className="formulario tarjeta" onSubmit={guardar} noValidate>
        {errorServidor !== undefined && <MensajeError error={errorServidor} />}
        <Campo etiqueta="Destino" error={errores.destino} ayuda="Al escribirlo aparecen sus ofertas para elegir.">
          {(p) => (
            <input
              {...p}
              value={destino}
              onChange={(e) => {
                setDestino(e.target.value)
                setOpciones(new Set())
              }}
            />
          )}
        </Campo>
        <Campo etiqueta="Título" error={errores.titulo}>
          {(p) => <input {...p} value={titulo} onChange={(e) => setTitulo(e.target.value)} />}
        </Campo>
        <Campo etiqueta="Descripción" opcional>
          {(p) => <textarea {...p} rows={2} value={descripcion} onChange={(e) => setDescripcion(e.target.value)} />}
        </Campo>
        <Campo etiqueta="Descuento (%)" error={errores.descuentoPorcentaje}>
          {(p) => <input {...p} type="number" min={1} max={99} value={descuento} onChange={(e) => setDescuento(Number(e.target.value))} />}
        </Campo>
        <fieldset className="grupo-opciones" aria-describedby={errores.opciones ? 'error-opciones' : undefined}>
          <legend>Opciones ofrecidas</legend>
          {!destino ? (
            <p className="ayuda-campo">Primero escribí el destino.</p>
          ) : ofertas.cargando ? (
            <Cargando />
          ) : ofertas.error !== undefined ? (
            <MensajeError error={ofertas.error} alReintentar={ofertas.recargar} />
          ) : (ofertas.datos?.items.length ?? 0) === 0 ? (
            <p className="ayuda-campo">No hay ofertas publicadas en {destino}.</p>
          ) : (
            ofertas.datos!.items.map((o) => (
              <label key={o.id} className={`opcion-pack ${opciones.has(o.id) ? 'elegida' : ''}`}>
                <input
                  type="checkbox"
                  checked={opciones.has(o.id)}
                  onChange={() =>
                    setOpciones((s) => {
                      const n = new Set(s)
                      if (n.has(o.id)) n.delete(o.id)
                      else n.add(o.id)
                      return n
                    })
                  }
                />
                <span className="opcion-pack-texto">
                  <strong>
                    {iconoTipo[o.tipo]} {o.titulo}
                  </strong>
                  <small>{fecha(o.desde)}</small>
                </span>
                <span className="precio">{moneda(o.precio)}</span>
              </label>
            ))
          )}
          {errores.opciones && (
            <small id="error-opciones" className="error-campo">
              {errores.opciones}
            </small>
          )}
        </fieldset>
        <div className="acciones">
          <Link to="/admin/packs" className="boton boton-secundario">
            Cancelar
          </Link>
          <button type="submit" className="boton boton-primario" disabled={guardando} aria-busy={guardando}>
            {guardando ? 'Creando…' : 'Crear pack'}
          </button>
        </div>
      </form>
    </div>
  )
}
