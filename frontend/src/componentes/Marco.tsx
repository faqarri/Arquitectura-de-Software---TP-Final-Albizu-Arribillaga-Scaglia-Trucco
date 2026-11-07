import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { datosFalsos } from '../api'
import { reiniciarDatosFalsos } from '../api/falso/servidor'
import { alCambiarSimulacion, cambiarSimulacion, simulacion, type Simulacion } from '../api/falso/simulacion'
import type { Rol } from '../api/tipos'
import { useCarrito } from '../carrito/Carrito'
import { useAhora } from '../hooks/hooks'
import { useSesion } from '../sesion/Sesion'
import { useAvisos } from './Avisos'
import { Icono } from './Iconos'

export function Marco() {
  const { usuario, salir, vencida } = useSesion()
  const { items } = useCarrito()
  const ahora = useAhora(5000)
  const ubicacion = useLocation()
  const navegar = useNavigate()
  const avisar = useAvisos()
  const principal = useRef<HTMLElement>(null)
  const [menuAbierto, setMenuAbierto] = useState(false)

  // Al cambiar de página, el foco va al contenido: el lector de pantalla anuncia la página nueva.
  useEffect(() => {
    setMenuAbierto(false)
    principal.current?.focus({ preventScroll: true })
    window.scrollTo(0, 0)
  }, [ubicacion.pathname])

  useEffect(() => {
    if (!vencida) return
    avisar({ tipo: 'info', titulo: 'Tu sesión venció', detalle: 'Ingresá de nuevo para continuar donde estabas.', duracion: 8000 })
    navegar('/ingresar?volver=' + encodeURIComponent(ubicacion.pathname + ubicacion.search), { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vencida])

  const activos = items.filter((it) => Date.parse(it.expiraEn) > ahora)
  const porVencer = activos.some((it) => Date.parse(it.expiraEn) - ahora < 3 * 60_000)

  return (
    <>
      <a href="#contenido" className="saltar">
        Saltar al contenido
      </a>
      {datosFalsos && (
        <div className="franja-demo" role="note">
          Modo demostración: los datos son de ejemplo y no se hacen compras reales.
        </div>
      )}
      <header className="encabezado">
        <div className="encabezado-interior">
          <Link to="/" className="marca" aria-label="Agencia de Viajes, inicio">
            <span className="marca-logo" aria-hidden="true">
              <Icono nombre="avion" />
            </span>
            <span>Agencia de Viajes</span>
          </Link>
          <button
            type="button"
            className="boton-menu"
            aria-expanded={menuAbierto}
            aria-controls="navegacion"
            onClick={() => setMenuAbierto((a) => !a)}
          >
            <Icono nombre="menu" /> Menú
          </button>
          <nav id="navegacion" aria-label="Principal" className={`navegacion ${menuAbierto ? 'abierta' : ''}`}>
            <NavLink to="/" end>
              Buscar
            </NavLink>
            {usuario?.rol === 'cliente' && (
              <>
                <NavLink to="/compras">Mis compras</NavLink>
                <NavLink to="/guias">Mis guías</NavLink>
              </>
            )}
            {usuario?.rol === 'admin' && <NavLink to="/admin">Administración</NavLink>}
            <NavLink to="/ayuda">Ayuda</NavLink>
            {usuario?.rol !== 'admin' && (
              <NavLink to="/carrito" className={`enlace-carrito ${porVencer ? 'por-vencer' : ''}`}>
                <Icono nombre="carrito" /> Carrito
                {activos.length > 0 && (
                  <span className="contador" aria-label={`${activos.length} ítems${porVencer ? ', alguno por vencer' : ''}`}>
                    {activos.length}
                  </span>
                )}
              </NavLink>
            )}
            {usuario ? (
              <span className="usuario">
                <span className="usuario-email" title={usuario.email}>
                  {usuario.email}
                  {usuario.rol === 'admin' && ' (admin)'}
                </span>
                <button
                  type="button"
                  className="boton boton-chico boton-secundario"
                  onClick={() => {
                    salir()
                    avisar({ tipo: 'exito', titulo: 'Cerraste la sesión' })
                    navegar('/')
                  }}
                >
                  Salir
                </button>
              </span>
            ) : (
              <Link to={'/ingresar?volver=' + encodeURIComponent(ubicacion.pathname)} className="boton boton-chico boton-primario">
                Ingresar
              </Link>
            )}
          </nav>
        </div>
      </header>
      <main id="contenido" ref={principal} tabIndex={-1} className="contenido">
        <Outlet />
      </main>
      <footer className="pie">
        <span>Agencia de Viajes · TP Arquitectura de Software 2026 (UCC)</span>
        <Link to="/ayuda">¿Necesitás ayuda?</Link>
      </footer>
      {datosFalsos && <PanelSimulacion />}
    </>
  )
}

/** Exige sesión (y rol, si se indica); si no, lleva a ingresar y después vuelve acá. */
export function ConSesion({ rol, children }: { rol?: Rol; children: ReactNode }) {
  const { usuario } = useSesion()
  const ubicacion = useLocation()
  if (!usuario) return <Navigate to={'/ingresar?volver=' + encodeURIComponent(ubicacion.pathname + ubicacion.search)} replace />
  if (rol && usuario.rol !== rol) {
    return (
      <div className="pagina-angosta">
        <h1>No tenés acceso a esta sección</h1>
        <p>{rol === 'admin' ? 'Solo un administrador puede ver esta página.' : 'Esta página es para clientes.'}</p>
        <Link to="/" className="boton boton-primario">
          Ir a buscar ofertas
        </Link>
      </div>
    )
  }
  return <>{children}</>
}

const opciones: { clave: keyof Simulacion; texto: string }[] = [
  { clave: 'catalogoCaido', texto: 'Catálogo caído' },
  { clave: 'estudiarioCaido', texto: 'Servicio de guías caído' },
  { clave: 'compraPendiente', texto: 'La próxima compra queda procesando' },
  { clave: 'retencionCorta', texto: 'Reservas de 1 minuto' },
]

function PanelSimulacion() {
  const [abierto, setAbierto] = useState(false)
  const [s, setS] = useState(simulacion)
  useEffect(() => alCambiarSimulacion(() => setS(simulacion())), [])
  const activas = opciones.filter((o) => s[o.clave]).length
  return (
    <aside className="panel-simulacion" aria-label="Simulación de fallas">
      <button type="button" className="boton boton-chico boton-secundario" aria-expanded={abierto} onClick={() => setAbierto((a) => !a)}>
        Simular fallas{activas > 0 && ` (${activas})`}
      </button>
      {abierto && (
        <div className="panel-simulacion-cuerpo">
          <p>Solo en modo demostración, para ver cómo responde la aplicación.</p>
          {opciones.map((o) => (
            <label key={o.clave} className="casilla">
              <input type="checkbox" checked={s[o.clave]} onChange={(e) => cambiarSimulacion({ [o.clave]: e.target.checked })} />
              {o.texto}
            </label>
          ))}
          <button
            type="button"
            className="boton boton-chico boton-secundario"
            onClick={() => {
              reiniciarDatosFalsos()
              window.location.reload()
            }}
          >
            Reiniciar datos de ejemplo
          </button>
        </div>
      )}
    </aside>
  )
}
