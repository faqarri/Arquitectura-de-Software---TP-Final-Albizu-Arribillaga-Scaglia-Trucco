import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import type { FiltrosBusqueda, Oferta, Orden, SubtipoTransporte, TipoOferta } from '../api/tipos'
import { Cargando, MensajeError, Paginacion, Vacio } from '../componentes/ui'
import { fecha, moneda, plural } from '../formato'
import { useCarga, useTitulo } from '../hooks/hooks'
import { nombreTipo } from '../reglas'
import { Icono, IconoOferta } from '../componentes/Iconos'

const TAMANIO = 12
const CLAVE_RECIENTES = 'agencia.destinos-recientes'
const DESTINOS_SUGERIDOS = ['Bariloche', 'Mendoza', 'Salta', 'Iguazú', 'Ushuaia', 'Córdoba']

export const iconoTipo: Record<TipoOferta, ReactNode> = {
  TRANSPORTE: <IconoOferta tipo="TRANSPORTE" />,
  ESTADIA: <IconoOferta tipo="ESTADIA" />,
  EXCURSION: <IconoOferta tipo="EXCURSION" />,
}
export const nombreSubtipo: Record<SubtipoTransporte, string> = { AVION: 'Avión', TREN: 'Tren', COLECTIVO: 'Colectivo' }
const nombreOrden: Record<Orden, string> = { fecha: 'Fecha más próxima', precio: 'Menor precio', '-precio': 'Mayor precio' }

function filtrosDeUrl(p: URLSearchParams): FiltrosBusqueda {
  const num = (k: string) => (p.get(k) ? Number(p.get(k)) : undefined)
  return {
    q: p.get('q') || undefined,
    destino: p.get('destino') || undefined,
    tipo: (p.get('tipo') as TipoOferta) || undefined,
    subtipoTransporte: (p.get('subtipoTransporte') as SubtipoTransporte) || undefined,
    desde: p.get('desde') || undefined,
    hasta: p.get('hasta') || undefined,
    precioMin: num('precioMin'),
    precioMax: num('precioMax'),
    orden: (p.get('orden') as Orden) || undefined,
    pagina: num('pagina'),
  }
}

function leerRecientes(): string[] {
  try {
    return JSON.parse(localStorage.getItem(CLAVE_RECIENTES) ?? '[]') as string[]
  } catch {
    return []
  }
}

function recordarDestino(d: string) {
  try {
    const r = [d, ...leerRecientes().filter((x) => x.toLowerCase() !== d.toLowerCase())].slice(0, 5)
    localStorage.setItem(CLAVE_RECIENTES, JSON.stringify(r))
  } catch {
    // sin recientes
  }
}

export function Buscar() {
  useTitulo('Buscar ofertas')
  const [params, setParams] = useSearchParams()
  const filtros = filtrosDeUrl(params)
  const resultado = useCarga(() => api.buscar({ ...filtros, tamanio: TAMANIO }), [params.toString()])
  const packs = useCarga(() => api.packs(), [])
  const [borrador, setBorrador] = useState<FiltrosBusqueda>(filtros)
  const [errores, setErrores] = useState<Record<string, string>>({})
  const [recientes, setRecientes] = useState(leerRecientes)
  const campoTexto = useRef<HTMLInputElement>(null)

  useEffect(() => setBorrador(filtrosDeUrl(params)), [params])

  // Atajo: "/" lleva a la búsqueda desde cualquier lugar de la página.
  useEffect(() => {
    function tecla(e: KeyboardEvent) {
      const t = e.target as HTMLElement
      if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)) {
        e.preventDefault()
        campoTexto.current?.focus()
      }
    }
    window.addEventListener('keydown', tecla)
    return () => window.removeEventListener('keydown', tecla)
  }, [])

  function aplicar(f: FiltrosBusqueda) {
    const p = new URLSearchParams()
    for (const [k, v] of Object.entries({ ...f, pagina: f.pagina && f.pagina > 1 ? f.pagina : undefined })) {
      if (v !== undefined && v !== '') p.set(k, String(v))
    }
    setParams(p)
  }

  function enviar(e: FormEvent) {
    e.preventDefault()
    const err: Record<string, string> = {}
    if (borrador.desde && borrador.hasta && borrador.hasta < borrador.desde) err.hasta = 'Tiene que ser igual o posterior a "Desde".'
    if (borrador.precioMin !== undefined && borrador.precioMax !== undefined && borrador.precioMax < borrador.precioMin) {
      err.precioMax = 'Tiene que ser mayor o igual al mínimo.'
    }
    setErrores(err)
    if (Object.keys(err).length) return
    if (borrador.destino) {
      recordarDestino(borrador.destino)
      setRecientes(leerRecientes())
    }
    aplicar({ ...borrador, pagina: undefined })
  }

  const quitarFiltro = (k: keyof FiltrosBusqueda) => {
    const f = { ...filtros, [k]: undefined, pagina: undefined }
    if (k === 'tipo') f.subtipoTransporte = undefined
    aplicar(f)
  }

  const chips: { clave: keyof FiltrosBusqueda; texto: string }[] = []
  if (filtros.q) chips.push({ clave: 'q', texto: `"${filtros.q}"` })
  if (filtros.destino) chips.push({ clave: 'destino', texto: filtros.destino })
  if (filtros.tipo) chips.push({ clave: 'tipo', texto: nombreTipo[filtros.tipo] })
  if (filtros.subtipoTransporte) chips.push({ clave: 'subtipoTransporte', texto: nombreSubtipo[filtros.subtipoTransporte] })
  if (filtros.desde) chips.push({ clave: 'desde', texto: 'Desde ' + fecha(filtros.desde + 'T12:00:00') })
  if (filtros.hasta) chips.push({ clave: 'hasta', texto: 'Hasta ' + fecha(filtros.hasta + 'T12:00:00') })
  if (filtros.precioMin !== undefined) chips.push({ clave: 'precioMin', texto: 'Desde ' + moneda(filtros.precioMin) })
  if (filtros.precioMax !== undefined) chips.push({ clave: 'precioMax', texto: 'Hasta ' + moneda(filtros.precioMax) })

  const set = (cambio: Partial<FiltrosBusqueda>) => setBorrador((b) => ({ ...b, ...cambio }))
  const numero = (v: string) => (v === '' ? undefined : Math.max(0, Number(v)))
  const packsVisibles = (packs.datos ?? []).filter((p) => !filtros.destino || p.destino.toLowerCase() === filtros.destino.toLowerCase())
  const total = resultado.datos?.total ?? 0
  const pagina = resultado.datos?.pagina ?? 1

  return (
    <div className="pagina-busqueda">
      <section className="portada">
        <h1>¿A dónde querés viajar?</h1>
        <p className="bajada">Vuelos, trenes, colectivos, estadías y excursiones. Al agregar al carrito te guardamos el lugar 15 minutos.</p>
        <form className="buscador" role="search" onSubmit={enviar} noValidate>
          <div className="buscador-fila">
            <label className="campo campo-crece">
              <span>Destino</span>
              <input
                list="destinos-sugeridos"
                placeholder="Bariloche, Mendoza…"
                value={borrador.destino ?? ''}
                onChange={(e) => set({ destino: e.target.value || undefined })}
              />
              <datalist id="destinos-sugeridos">
                {[...new Set([...recientes, ...DESTINOS_SUGERIDOS])].map((d) => (
                  <option key={d} value={d} />
                ))}
              </datalist>
            </label>
            <label className="campo campo-crece">
              <span>
                Qué buscás <kbd title="Atajo de teclado">/</kbd>
              </span>
              <input ref={campoTexto} type="search" placeholder="trekking, bodegas, lago…" value={borrador.q ?? ''} onChange={(e) => set({ q: e.target.value || undefined })} />
            </label>
            <button type="submit" className="boton boton-primario boton-grande">
              Buscar
            </button>
          </div>

          <fieldset className="tipos">
            <legend className="solo-lectores">Tipo de oferta</legend>
            {([undefined, 'TRANSPORTE', 'ESTADIA', 'EXCURSION'] as (TipoOferta | undefined)[]).map((t) => (
              <label key={t ?? 'todos'} className={`opcion-pastilla ${borrador.tipo === t ? 'elegida' : ''}`}>
                <input
                  type="radio"
                  name="tipo"
                  checked={borrador.tipo === t}
                  onChange={() => aplicar({ ...filtros, tipo: t, subtipoTransporte: undefined, pagina: undefined })}
                />
                {t ? <>{iconoTipo[t]} {nombreTipo[t]}</> : 'Todo'}
              </label>
            ))}
          </fieldset>

          <details className="mas-filtros" open={Boolean(filtros.desde || filtros.hasta || filtros.precioMin || filtros.precioMax || filtros.subtipoTransporte)}>
            <summary>Más filtros: fechas, precio{borrador.tipo === 'TRANSPORTE' ? ' y medio de transporte' : ''}</summary>
            <div className="rejilla-filtros">
              {borrador.tipo === 'TRANSPORTE' && (
                <label className="campo">
                  <span>Medio</span>
                  <select value={borrador.subtipoTransporte ?? ''} onChange={(e) => set({ subtipoTransporte: (e.target.value as SubtipoTransporte) || undefined })}>
                    <option value="">Cualquiera</option>
                    {Object.entries(nombreSubtipo).map(([k, v]) => (
                      <option key={k} value={k}>
                        {v}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <label className="campo">
                <span>Desde</span>
                <input type="date" value={borrador.desde ?? ''} onChange={(e) => set({ desde: e.target.value || undefined })} />
              </label>
              <div className={`campo ${errores.hasta ? 'campo-con-error' : ''}`}>
                <label htmlFor="filtro-hasta">Hasta</label>
                <input
                  id="filtro-hasta"
                  type="date"
                  min={borrador.desde}
                  value={borrador.hasta ?? ''}
                  aria-invalid={Boolean(errores.hasta)}
                  aria-describedby={errores.hasta ? 'error-hasta' : undefined}
                  onChange={(e) => set({ hasta: e.target.value || undefined })}
                />
                {errores.hasta && <small id="error-hasta" className="error-campo">{errores.hasta}</small>}
              </div>
              <label className="campo">
                <span>Precio mínimo ($)</span>
                <input type="number" min={0} step={1000} inputMode="numeric" value={borrador.precioMin ?? ''} onChange={(e) => set({ precioMin: numero(e.target.value) })} />
              </label>
              <div className={`campo ${errores.precioMax ? 'campo-con-error' : ''}`}>
                <label htmlFor="filtro-precio-max">Precio máximo ($)</label>
                <input
                  id="filtro-precio-max"
                  type="number"
                  min={0}
                  step={1000}
                  inputMode="numeric"
                  value={borrador.precioMax ?? ''}
                  aria-invalid={Boolean(errores.precioMax)}
                  aria-describedby={errores.precioMax ? 'error-precio-max' : undefined}
                  onChange={(e) => set({ precioMax: numero(e.target.value) })}
                />
                {errores.precioMax && <small id="error-precio-max" className="error-campo">{errores.precioMax}</small>}
              </div>
            </div>
          </details>
        </form>
      </section>

      {packsVisibles.length > 0 && !filtros.tipo && !filtros.q && (
        <section aria-labelledby="titulo-packs" className="seccion">
          <h2 id="titulo-packs">Packs con descuento</h2>
          <div className="rejilla">
            {packsVisibles.map((p) => (
              <Link key={p.id} to={`/packs/${p.id}`} className="tarjeta tarjeta-enlace tarjeta-pack">
                <span className="etiqueta etiqueta-exito">−{p.descuentoPorcentaje} %</span>
                <h3>{p.titulo}</h3>
                <p className="destino">
                  <Icono nombre="ubicacion" /> {p.destino}
                </p>
                <p>{p.descripcion}</p>
                <span className="enlace-accion">Armar mi pack →</span>
              </Link>
            ))}
          </div>
        </section>
      )}

      <section aria-labelledby="titulo-resultados" className="seccion">
        <div className="encabezado-seccion">
          <h2 id="titulo-resultados">
            {resultado.cargando && !resultado.datos ? 'Buscando…' : plural(total, 'oferta', 'ofertas')}
          </h2>
          <label className="campo campo-en-linea">
            <span>Ordenar por</span>
            <select value={filtros.orden ?? 'fecha'} onChange={(e) => aplicar({ ...filtros, orden: e.target.value as Orden, pagina: undefined })}>
              {Object.entries(nombreOrden).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </label>
        </div>

        {chips.length > 0 && (
          <div className="chips" aria-label="Filtros aplicados">
            {chips.map((c) => (
              <button key={c.clave} type="button" className="chip" onClick={() => quitarFiltro(c.clave)} aria-label={`Quitar filtro ${c.texto}`}>
                {c.texto} <span aria-hidden="true">×</span>
              </button>
            ))}
            <button type="button" className="boton-enlace" onClick={() => aplicar({})}>
              Limpiar todo
            </button>
          </div>
        )}

        {resultado.error !== undefined ? (
          <MensajeError error={resultado.error} alReintentar={resultado.recargar} />
        ) : resultado.cargando && !resultado.datos ? (
          <Cargando texto="Buscando ofertas…" />
        ) : total === 0 ? (
          <Vacio titulo="No encontramos ofertas con esos filtros">
            <p>Probá con otro destino, ampliá las fechas o quitá algún filtro.</p>
            {chips.length > 0 && (
              <button type="button" className="boton boton-secundario" onClick={() => aplicar({})}>
                Ver todas las ofertas
              </button>
            )}
          </Vacio>
        ) : (
          <div aria-busy={resultado.cargando}>
            <ul className="rejilla lista-sin-estilo">
              {resultado.datos!.items.map((o) => (
                <li key={o.id}>
                  <TarjetaOferta oferta={o} />
                </li>
              ))}
            </ul>
            <Paginacion pagina={pagina} paginas={Math.ceil(total / TAMANIO)} alCambiar={(p) => aplicar({ ...filtros, pagina: p })} />
          </div>
        )}
      </section>
    </div>
  )
}

export function TarjetaOferta({ oferta: o }: { oferta: Oferta }) {
  return (
    <Link to={`/ofertas/${o.id}`} className="tarjeta tarjeta-enlace tarjeta-oferta">
      <span className="tipo">
        <IconoOferta tipo={o.tipo} subtipo={o.subtipoTransporte} /> {o.subtipoTransporte ? nombreSubtipo[o.subtipoTransporte] : nombreTipo[o.tipo]}
      </span>
      <h3>{o.titulo}</h3>
      <p className="destino">
        <Icono nombre="ubicacion" /> {o.destino}
      </p>
      <p className="fechas">
        {fecha(o.desde)}
        {o.hasta.slice(0, 10) !== o.desde.slice(0, 10) && ` → ${fecha(o.hasta)}`}
      </p>
      <p className="precio">
        <span className="solo-lectores">Precio: </span>
        {moneda(o.precio)}
      </p>
    </Link>
  )
}
