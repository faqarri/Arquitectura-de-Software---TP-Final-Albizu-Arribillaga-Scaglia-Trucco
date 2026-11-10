import { useRef, useState, type DragEvent, type FormEvent, type KeyboardEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { ApiError } from '../api/errores'
import { useAvisos } from '../componentes/Avisos'
import { Ayuda, Campo, Cargando, Dialogo, EstadoGuiaEtiqueta, MensajeError, Migas, Vacio, textoGuia } from '../componentes/ui'
import { fechaHora, tamanio } from '../formato'
import { useCadaTanto, useCarga, useTitulo } from '../hooks/hooks'
import { MAX_ETIQUETAS, MAX_TAMANIO_PDF } from '../reglas'
import { useSesion } from '../sesion/Sesion'
import { Icono } from '../componentes/Iconos'

export function MisGuias() {
  useTitulo('Mis guías')
  const guias = useCarga(() => api.guias(), [])
  const enCurso = guias.datos?.some((g) => g.estado === 'POR_ENVIAR' || g.estado === 'PENDIENTE') ?? false
  useCadaTanto(guias.recargar, 10000, enCurso)

  return (
    <div className="pagina-angosta">
      <div className="encabezado-seccion">
        <h1>Mis guías de viaje</h1>
        <Link to="/guias/nueva" className="boton boton-primario">
          Compartir una guía
        </Link>
      </div>
      <p className="bajada">Compartí en PDF lo que aprendiste en tu viaje. Las guías publicadas aparecen en las ofertas de ese destino.</p>
      {guias.error !== undefined && !guias.datos ? (
        <MensajeError error={guias.error} alReintentar={guias.recargar} />
      ) : !guias.datos ? (
        <Cargando texto="Cargando tus guías…" />
      ) : guias.datos.length === 0 ? (
        <Vacio titulo="Todavía no compartiste guías">
          <p>Podés compartir una guía de cualquier destino que hayas comprado.</p>
        </Vacio>
      ) : (
        <ul className="lista-sin-estilo lista-items">
          {guias.datos.map((g) => (
            <li key={g.id} className="tarjeta">
              <div className="encabezado-seccion">
                <h2 className="h3">{g.titulo}</h2>
                <EstadoGuiaEtiqueta estado={g.estado} />
              </div>
              <p className="ayuda-campo">
                <Icono nombre="ubicacion" /> {g.destino} · compartida el {fechaHora(g.creadaEn)}
                {g.etiquetas.length > 0 && ' · ' + g.etiquetas.join(', ')}
              </p>
              <p>{textoGuia[g.estado][3]}</p>
              {g.motivoRechazo && (
                <p className="caja caja-error">
                  <strong>Motivo: </strong>
                  {g.motivoRechazo}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
      <Ayuda pregunta="¿Qué significa cada estado?">
        <ul className="lista-simple">
          {Object.values(textoGuia).map(([, , nombre, texto]) => (
            <li key={nombre}>
              <strong>{nombre}:</strong> {texto}
            </li>
          ))}
        </ul>
      </Ayuda>
    </div>
  )
}

interface Errores {
  destino?: string
  titulo?: string
  autor?: string
  descripcion?: string
  etiquetas?: string
  archivo?: string
}

export function CompartirGuia() {
  useTitulo('Compartir una guía')
  const [params] = useSearchParams()
  const { usuario } = useSesion()
  const navegar = useNavigate()
  const avisar = useAvisos()
  const compras = useCarga(() => api.compras(), [])
  const destinos = [...new Set((compras.datos ?? []).filter((c) => c.estado === 'CONFIRMADA').flatMap((c) => c.lineas.map((l) => l.destino)))]

  const pedido = params.get('destino') ?? ''
  const [destino, setDestino] = useState<string>()
  const [titulo, setTitulo] = useState('')
  const [autor, setAutor] = useState(() => usuario?.email.split('@')[0] ?? '')
  const [descripcion, setDescripcion] = useState('')
  const [etiquetas, setEtiquetas] = useState<string[]>([])
  const [etiqueta, setEtiqueta] = useState('')
  const [archivo, setArchivo] = useState<File>()
  const [arrastrando, setArrastrando] = useState(false)
  const [errores, setErrores] = useState<Errores>({})
  const [errorServidor, setErrorServidor] = useState<unknown>()
  const [enviando, setEnviando] = useState(false)
  const [descartar, setDescartar] = useState(false)
  const entradaArchivo = useRef<HTMLInputElement>(null)

  const destinoElegido = destino ?? (destinos.find((d) => d.toLowerCase() === pedido.toLowerCase()) || (destinos.length === 1 ? destinos[0] : ''))
  const cambiado = Boolean(titulo || descripcion || etiquetas.length || archivo)

  function elegirArchivo(f: File | undefined) {
    if (!f) return
    let error: string | undefined
    if (f.type !== 'application/pdf' && !f.name.toLowerCase().endsWith('.pdf')) error = 'El archivo tiene que ser un PDF.'
    else if (f.size > MAX_TAMANIO_PDF) error = `Pesa ${tamanio(f.size)} y el máximo es 10 MB.`
    setErrores((e) => ({ ...e, archivo: error }))
    setArchivo(error ? undefined : f)
  }

  function agregarEtiqueta() {
    const t = etiqueta.trim().replace(/,$/, '').trim()
    if (!t) return
    if (t.length < 2 || t.length > 40) {
      setErrores((e) => ({ ...e, etiquetas: 'Cada etiqueta tiene que tener entre 2 y 40 caracteres.' }))
      return
    }
    if (etiquetas.length >= MAX_ETIQUETAS) {
      setErrores((e) => ({ ...e, etiquetas: `Podés poner hasta ${MAX_ETIQUETAS} etiquetas.` }))
      return
    }
    if (!etiquetas.some((x) => x.toLowerCase() === t.toLowerCase())) setEtiquetas([...etiquetas, t])
    setEtiqueta('')
    setErrores((e) => ({ ...e, etiquetas: undefined }))
  }

  function teclaEtiqueta(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      agregarEtiqueta()
    } else if (e.key === 'Backspace' && !etiqueta && etiquetas.length) {
      setEtiquetas(etiquetas.slice(0, -1))
    }
  }

  function validar(): Errores {
    const e: Errores = {}
    if (!destinoElegido) e.destino = 'Elegí el destino de la guía.'
    const t = titulo.trim().length
    if (t < 3 || t > 150) e.titulo = 'El título tiene que tener entre 3 y 150 caracteres.'
    const a = autor.trim().length
    if (a < 2 || a > 100) e.autor = 'El nombre tiene que tener entre 2 y 100 caracteres.'
    if (descripcion.length > 2000) e.descripcion = 'La descripción puede tener hasta 2000 caracteres.'
    if (!archivo) e.archivo = errores.archivo ?? 'Elegí el PDF de la guía.'
    return e
  }

  async function enviar(ev: FormEvent) {
    ev.preventDefault()
    const e = validar()
    setErrores(e)
    setErrorServidor(undefined)
    const primero = Object.keys(e)[0]
    if (primero) {
      document.getElementById('guia-' + primero)?.focus()
      return
    }
    setEnviando(true)
    try {
      const g = await api.compartirGuia({ titulo: titulo.trim(), destino: destinoElegido, autor: autor.trim(), descripcion: descripcion.trim(), etiquetas, archivo: archivo! })
      avisar(
        g.estado === 'POR_ENVIAR'
          ? { tipo: 'info', titulo: 'Guardamos tu guía', detalle: 'El servicio de guías no responde ahora; la enviamos sola en cuanto vuelva.', duracion: 8000 }
          : { tipo: 'exito', titulo: 'Recibimos tu guía', detalle: 'Está en moderación. Te mostramos el estado en "Mis guías".' },
      )
      navegar('/guias')
    } catch (err) {
      if (err instanceof ApiError && err.errores.length > 0) {
        setErrores(Object.fromEntries(err.errores.map((x) => [x.campo === 'etiquetas' ? 'etiquetas' : x.campo, x.mensaje])))
      } else {
        setErrorServidor(err)
      }
    } finally {
      setEnviando(false)
    }
  }

  const soltar = (e: DragEvent) => {
    e.preventDefault()
    setArrastrando(false)
    elegirArchivo(e.dataTransfer.files[0])
  }

  const errorResumen = Object.values(errores).filter(Boolean)
  return (
    <div className="pagina-angosta">
      <Migas pasos={[{ texto: 'Mis guías', a: '/guias' }, { texto: 'Compartir una guía' }]} />
      <h1>Compartir una guía de viaje</h1>
      <p className="bajada">Solo de destinos que compraste. Antes de publicarla la revisa un moderador.</p>

      {compras.error !== undefined && !compras.datos ? (
        <MensajeError error={compras.error} alReintentar={compras.recargar} />
      ) : !compras.datos ? (
        <Cargando texto="Buscando tus destinos…" />
      ) : destinos.length === 0 ? (
        <Vacio titulo="Todavía no tenés destinos para compartir">
          <p>Vas a poder compartir una guía cuando tengas una compra confirmada.</p>
          <Link to="/" className="boton boton-primario">
            Buscar ofertas
          </Link>
        </Vacio>
      ) : (
        <form className="formulario tarjeta" onSubmit={enviar} noValidate>
          {errorResumen.length > 1 && (
            <div className="caja caja-error" role="alert">
              <strong>Revisá {errorResumen.length} campos marcados.</strong>
            </div>
          )}
          {errorServidor !== undefined && <MensajeError error={errorServidor} />}

          <Campo etiqueta="Destino" error={errores.destino} ayuda="Aparecen los destinos de tus compras confirmadas.">
            {(p) => (
              <select {...p} id="guia-destino" value={destinoElegido} onChange={(e) => setDestino(e.target.value)}>
                <option value="" disabled>
                  Elegí un destino
                </option>
                {destinos.map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
            )}
          </Campo>

          <Campo etiqueta="Título" error={errores.titulo} ayuda={`${titulo.trim().length}/150 caracteres`}>
            {(p) => <input {...p} id="guia-titulo" maxLength={150} placeholder="Ej.: Trekking a Refugio Frey en dos días" value={titulo} onChange={(e) => setTitulo(e.target.value)} />}
          </Campo>

          <Campo etiqueta="Nombre que se muestra como autor" error={errores.autor}>
            {(p) => <input {...p} id="guia-autor" maxLength={100} autoComplete="name" value={autor} onChange={(e) => setAutor(e.target.value)} />}
          </Campo>

          <Campo etiqueta="Descripción" opcional error={errores.descripcion} ayuda={`${descripcion.length}/2000 caracteres`}>
            {(p) => <textarea {...p} id="guia-descripcion" rows={4} maxLength={2000} value={descripcion} onChange={(e) => setDescripcion(e.target.value)} />}
          </Campo>

          <Campo etiqueta="Etiquetas" opcional error={errores.etiquetas} ayuda={`Escribí una y apretá Enter o coma. ${etiquetas.length}/${MAX_ETIQUETAS}.`}>
            {(p) => (
              <div className="entrada-etiquetas">
                {etiquetas.map((t) => (
                  <button key={t} type="button" className="chip" aria-label={`Quitar etiqueta ${t}`} onClick={() => setEtiquetas(etiquetas.filter((x) => x !== t))}>
                    {t} <span aria-hidden="true">×</span>
                  </button>
                ))}
                <input
                  {...p}
                  id="guia-etiquetas"
                  value={etiqueta}
                  placeholder={etiquetas.length ? '' : 'trekking, invierno…'}
                  disabled={etiquetas.length >= MAX_ETIQUETAS}
                  onChange={(e) => setEtiqueta(e.target.value)}
                  onKeyDown={teclaEtiqueta}
                  onBlur={agregarEtiqueta}
                />
              </div>
            )}
          </Campo>

          <Campo etiqueta="Archivo PDF" error={errores.archivo} ayuda="Hasta 10 MB.">
            {(p) => (
              <div
                className={`zona-archivo ${arrastrando ? 'arrastrando' : ''}`}
                onDragOver={(e) => {
                  e.preventDefault()
                  setArrastrando(true)
                }}
                onDragLeave={() => setArrastrando(false)}
                onDrop={soltar}
              >
                <input {...p} ref={entradaArchivo} id="guia-archivo" type="file" accept="application/pdf,.pdf" className="solo-lectores" onChange={(e) => elegirArchivo(e.target.files?.[0])} />
                {archivo ? (
                  <div className="archivo-elegido">
                    <Icono nombre="archivo" className="icono-grande" />
                    <span>
                      <strong>{archivo.name}</strong> <small>({tamanio(archivo.size)})</small>
                    </span>
                    <button
                      type="button"
                      className="boton boton-chico boton-secundario"
                      onClick={() => {
                        setArchivo(undefined)
                        if (entradaArchivo.current) entradaArchivo.current.value = ''
                      }}
                    >
                      Quitar
                    </button>
                  </div>
                ) : (
                  <p>
                    Arrastrá el PDF acá o{' '}
                    <button type="button" className="boton-enlace" onClick={() => entradaArchivo.current?.click()}>
                      elegilo de tu equipo
                    </button>
                  </p>
                )}
              </div>
            )}
          </Campo>

          <div className="acciones">
            <button type="button" className="boton boton-secundario" disabled={enviando} onClick={() => (cambiado ? setDescartar(true) : navegar('/guias'))}>
              Cancelar
            </button>
            <button type="submit" className="boton boton-primario" disabled={enviando} aria-busy={enviando}>
              {enviando ? 'Enviando la guía…' : 'Compartir guía'}
            </button>
          </div>
        </form>
      )}

      <Dialogo
        abierto={descartar}
        titulo="¿Descartar la guía?"
        confirmar="Descartar"
        cancelar="Seguir editando"
        peligroso
        alConfirmar={() => navegar('/guias')}
        alCerrar={() => setDescartar(false)}
      >
        <p>Se pierde lo que escribiste y el archivo elegido.</p>
      </Dialogo>
    </div>
  )
}
