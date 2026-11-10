import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { api, datosFalsos } from '../api'
import { ApiError } from '../api/errores'
import { useAvisos } from '../componentes/Avisos'
import { Campo, MensajeError } from '../componentes/ui'
import { useTitulo } from '../hooks/hooks'
import { useSesion } from '../sesion/Sesion'

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const MIN_PASSWORD = 8

/** Solo rutas internas, para que nadie arme un enlace que lleve a otro sitio después de ingresar. */
function destinoSeguro(volver: string | null): string {
  return volver && volver.startsWith('/') && !volver.startsWith('//') && !volver.startsWith('/ingresar') ? volver : '/'
}

export function Ingresar() {
  const [params, setParams] = useSearchParams()
  const registro = params.get('modo') === 'registro'
  const { usuario, ingresar } = useSesion()
  const navegar = useNavigate()
  const avisar = useAvisos()
  useTitulo(registro ? 'Crear cuenta' : 'Ingresar')

  const [nombre, setNombre] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [repetir, setRepetir] = useState('')
  const [verPassword, setVerPassword] = useState(false)
  const [errores, setErrores] = useState<Record<string, string>>({})
  const [errorServidor, setErrorServidor] = useState<unknown>()
  const [enviando, setEnviando] = useState(false)

  const volver = destinoSeguro(params.get('volver'))
  if (usuario) return <Navigate to={volver} replace />

  function cambiarModo(r: boolean) {
    const p = new URLSearchParams(params)
    if (r) p.set('modo', 'registro')
    else p.delete('modo')
    setParams(p, { replace: true })
    setErrores({})
    setErrorServidor(undefined)
  }

  function validar(): Record<string, string> {
    const e: Record<string, string> = {}
    if (registro && nombre.trim().length < 2) e.nombre = 'Escribí tu nombre.'
    if (!EMAIL.test(email.trim())) e.email = 'Escribí un email válido, por ejemplo ana@example.com.'
    if (registro && password.length < MIN_PASSWORD) e.password = `La contraseña tiene que tener al menos ${MIN_PASSWORD} caracteres.`
    if (!registro && !password) e.password = 'Escribí tu contraseña.'
    if (registro && repetir !== password) e.repetir = 'Las contraseñas no coinciden.'
    return e
  }

  async function enviar(ev: FormEvent) {
    ev.preventDefault()
    const e = validar()
    setErrores(e)
    setErrorServidor(undefined)
    if (Object.keys(e).length > 0) {
      document.getElementById('campo-' + Object.keys(e)[0])?.focus()
      return
    }
    setEnviando(true)
    try {
      if (registro) await api.registrarse({ nombre: nombre.trim(), email: email.trim(), password })
      const { token } = await api.iniciarSesion(email.trim(), password)
      ingresar(token)
      avisar({ tipo: 'exito', titulo: registro ? 'Cuenta creada' : 'Ingresaste', detalle: registro ? 'Ya podés reservar.' : undefined })
      navegar(volver, { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.errores.length > 0) {
        setErrores(Object.fromEntries(err.errores.map((x) => [x.campo, x.mensaje])))
      } else {
        setErrorServidor(err)
      }
    } finally {
      setEnviando(false)
    }
  }

  return (
    <div className="pagina-angosta">
      <h1>{registro ? 'Crear una cuenta' : 'Ingresar'}</h1>
      <p className="bajada">
        {registro ? 'Con tu cuenta podés reservar, comprar y compartir guías de viaje.' : 'Ingresá para reservar y ver tus compras.'}
      </p>

      <div className="pestanias" role="group" aria-label="Elegí qué querés hacer">
        <button type="button" aria-pressed={!registro} onClick={() => cambiarModo(false)}>
          Ya tengo cuenta
        </button>
        <button type="button" aria-pressed={registro} onClick={() => cambiarModo(true)}>
          Soy nuevo
        </button>
      </div>

      {errorServidor !== undefined && <MensajeError error={errorServidor} />}

      <form onSubmit={enviar} noValidate className="formulario tarjeta">
        {registro && (
          <Campo etiqueta="Nombre" error={errores.nombre}>
            {(p) => <input {...p} id="campo-nombre" autoComplete="name" value={nombre} onChange={(e) => setNombre(e.target.value)} />}
          </Campo>
        )}
        <Campo etiqueta="Email" error={errores.email}>
          {(p) => (
            <input {...p} id="campo-email" type="email" autoComplete="email" inputMode="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          )}
        </Campo>
        <Campo etiqueta="Contraseña" error={errores.password} ayuda={registro ? `Al menos ${MIN_PASSWORD} caracteres.` : undefined}>
          {(p) => (
            <div className="con-boton">
              <input
                {...p}
                id="campo-password"
                type={verPassword ? 'text' : 'password'}
                autoComplete={registro ? 'new-password' : 'current-password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <button type="button" className="boton boton-chico boton-secundario" aria-pressed={verPassword} onClick={() => setVerPassword((v) => !v)}>
                {verPassword ? 'Ocultar' : 'Mostrar'}
              </button>
            </div>
          )}
        </Campo>
        {registro && (
          <Campo etiqueta="Repetí la contraseña" error={errores.repetir}>
            {(p) => (
              <input
                {...p}
                id="campo-repetir"
                type={verPassword ? 'text' : 'password'}
                autoComplete="new-password"
                value={repetir}
                onChange={(e) => setRepetir(e.target.value)}
              />
            )}
          </Campo>
        )}
        <button type="submit" className="boton boton-primario boton-ancho" disabled={enviando} aria-busy={enviando}>
          {enviando ? (registro ? 'Creando la cuenta…' : 'Ingresando…') : registro ? 'Crear cuenta' : 'Ingresar'}
        </button>
      </form>

      {datosFalsos && !registro && (
        <div className="caja caja-info">
          <strong>Cuentas de ejemplo</strong>
          <ul className="lista-simple">
            {[
              ['ana@example.com', 'demo1234', 'Cliente (ya compró un viaje a Bariloche)'],
              ['admin@example.com', 'admin1234', 'Administrador'],
            ].map(([e, p, d]) => (
              <li key={e}>
                <span>
                  {d}: <code>{e}</code>
                </span>{' '}
                <button
                  type="button"
                  className="boton boton-chico boton-secundario"
                  onClick={() => {
                    setEmail(e)
                    setPassword(p)
                    setErrores({})
                  }}
                >
                  Completar
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="centrado">
        <Link to="/">Seguir mirando ofertas sin ingresar</Link>
      </p>
    </div>
  )
}
