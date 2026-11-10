// Implementación en memoria de la API, para usar el frontend sin el resto del
// sistema. Responde con los mismos formatos, status y codes que los servicios.

import { nuevoId } from '../../formato'
import { ApiError } from '../errores'
import { leerToken, tokenDeDemostracion, usuarioDelToken } from '../../sesion/token'
import { MAX_CUPOS_POR_OFERTA, normalizarDestino, precioDelPack, problemaDelPack } from '../../reglas'
import { guiasPublicadas, ofertasIniciales, packsIniciales, type OfertaFalsa, type PackFalso } from './datos'
import { simulacion } from './simulacion'
import type { Api, Compra, GuiaDestino, Guia, ItemCarrito, Oferta, OfertaNueva, Pack, Rol } from '../tipos'

interface ItemFalso extends ItemCarrito {
  usuarioId: string
  opciones: string[]
}

interface CompraFalsa extends Compra {
  usuarioId: string
  clave: string
  items: ItemFalso[]
  resolverEn?: number
}

interface GuiaFalsa extends Guia {
  usuarioId: string
  autor: string
  descripcion: string
  moderarEn?: number
}

interface Estado {
  ofertas: OfertaFalsa[]
  packs: PackFalso[]
  usuarios: { id: string; nombre: string; email: string; password: string; rol: Rol }[]
  items: ItemFalso[]
  clavesItems: Record<string, string>
  compras: CompraFalsa[]
  guias: GuiaFalsa[]
  publicadas: Record<string, GuiaDestino[]>
}

const CLAVE = 'agencia.datos-falsos'
const MINUTOS_RETENCION = 15

function inicial(): Estado {
  const hace = (dias: number) => new Date(Date.now() - dias * 86400_000).toISOString()
  return {
    ofertas: ofertasIniciales(),
    packs: packsIniciales(),
    usuarios: [
      { id: 'u-ana', nombre: 'Ana Pérez', email: 'ana@example.com', password: 'demo1234', rol: 'cliente' },
      { id: 'u-admin', nombre: 'Administración', email: 'admin@example.com', password: 'admin1234', rol: 'admin' },
    ],
    items: [],
    clavesItems: {},
    compras: [
      {
        id: 'c-0001', usuarioId: 'u-ana', clave: 'semilla-0001', items: [], estado: 'CONFIRMADA', total: 283000, creadaEn: hace(12),
        lineas: [
          { descripcion: 'Vuelo Córdoba → Bariloche', destino: 'Bariloche', precio: 185000 },
          { descripcion: 'Día completo en Cerro Catedral', destino: 'Bariloche', precio: 98000 },
        ],
      },
    ],
    guias: [
      { id: 'g-0001', usuarioId: 'u-ana', autor: 'Ana Pérez', descripcion: '', titulo: 'Mapa escaneado del centro', destino: 'Bariloche', etiquetas: ['ciudad'], estado: 'RECHAZADA', motivoRechazo: 'El archivo no se puede leer; subí una versión con mejor resolución.', creadaEn: hace(10) },
    ],
    publicadas: structuredClone(guiasPublicadas),
  }
}

let estado: Estado = cargar()

function cargar(): Estado {
  try {
    const s = sessionStorage.getItem(CLAVE)
    if (s) return JSON.parse(s) as Estado
  } catch {
    // datos nuevos
  }
  return inicial()
}

function guardar() {
  try {
    sessionStorage.setItem(CLAVE, JSON.stringify(estado))
  } catch {
    // sin persistencia
  }
}

export function reiniciarDatosFalsos() {
  estado = inicial()
  guardar()
}

// ---------- auxiliares ----------

// Latencia simulada, para que se vean los estados de carga (en los tests, mínima).
const esperar = () => new Promise((r) => setTimeout(r, import.meta.env.MODE === 'test' ? 5 : 250 + Math.random() * 350))
const id = (prefijo: string) => prefijo + '-' + nuevoId().slice(0, 8)

function problema(status: number, code: string, detalle: string, datos: Record<string, unknown> = {}): never {
  throw new ApiError(status, code, detalle, { code, detail: detalle, ...datos }, status === 503 ? 5 : undefined)
}

function usuarioActual() {
  const u = usuarioDelToken(leerToken())
  if (!u) problema(401, 'no-autenticado', 'Ingresá para continuar.')
  return u
}

function exigirAdmin() {
  if (usuarioActual().rol !== 'admin') problema(403, 'prohibido', 'Solo un administrador puede hacer esto.')
}

function catalogo() {
  if (simulacion().catalogoCaido) problema(503, 'no-disponible', 'El catálogo no responde en este momento. Intentá de nuevo en unos segundos.')
}

function buscarOferta(ofertaId: string): OfertaFalsa {
  const o = estado.ofertas.find((x) => x.id === ofertaId && !x.despublicada)
  if (!o) problema(404, 'no-encontrado', 'La oferta no existe.')
  return o
}

const vigente = (it: ItemCarrito) => Date.parse(it.expiraEn) > Date.now()

function retenidosEnCarritos(ofertaId: string) {
  return estado.items.filter((it) => vigente(it) && (it.ofertaId === ofertaId || it.opciones.includes(ofertaId)))
}

function disponibilidadDe(o: OfertaFalsa) {
  if (o.liberanEn && Date.parse(o.liberanEn) <= Date.now()) {
    o.retenidosPorOtros = 0
    o.liberanEn = undefined
  }
  const enCarritos = retenidosEnCarritos(o.id)
  const retenidos = o.retenidosPorOtros + enCarritos.reduce((t, it) => t + it.cantidad, 0)
  const libres = Math.max(0, o.cupoTotal - o.vendidos - retenidos)
  if (libres > 0) return { ofertaId: o.id, estado: 'DISPONIBLE' as const, libres, proximaLiberacion: null }
  if (retenidos > 0) {
    const vencimientos = enCarritos.map((it) => Date.parse(it.expiraEn))
    if (o.liberanEn) vencimientos.push(Date.parse(o.liberanEn))
    return { ofertaId: o.id, estado: 'RETENIDA' as const, libres: 0, proximaLiberacion: new Date(Math.min(...vencimientos)).toISOString() }
  }
  return { ofertaId: o.id, estado: 'AGOTADA' as const, libres: 0, proximaLiberacion: null }
}

function publica(o: OfertaFalsa): Oferta {
  const { retenidosPorOtros: _r, liberanEn: _l, despublicada: _d, ...resto } = o
  return resto
}

function packPublico(p: PackFalso): Pack {
  return { ...p, opciones: p.opciones.map((x) => estado.ofertas.find((o) => o.id === x)).filter((o): o is OfertaFalsa => !!o).map(publica) }
}

function itemPublico(it: ItemFalso): ItemCarrito {
  const { usuarioId: _u, opciones: _o, ...resto } = it
  return { ...resto, estado: vigente(it) ? 'ACTIVO' : 'VENCIDO' }
}

function compraPublica(c: CompraFalsa): Compra {
  const { usuarioId: _u, clave: _c, items: _i, resolverEn: _r, ...resto } = c
  return resto
}

function guiaPublica(g: GuiaFalsa): Guia {
  const { usuarioId: _u, autor: _a, descripcion: _d, moderarEn: _m, ...resto } = g
  return resto
}

function resolverCompra(c: CompraFalsa) {
  if (c.estado !== 'PENDIENTE' || !c.resolverEn || Date.now() < c.resolverEn || simulacion().catalogoCaido) return
  confirmarORechazar(c)
  guardar()
}

function confirmarORechazar(c: CompraFalsa) {
  const vencidos = c.items.filter((it) => !vigente(it))
  if (vencidos.length > 0) {
    c.estado = 'RECHAZADA'
    c.retencionesVencidas = vencidos.map((it) => 'ret-' + it.id)
    c.lineas = []
    c.total = 0
    return
  }
  for (const it of c.items) {
    for (const ofertaId of it.ofertaId ? [it.ofertaId] : it.opciones) {
      const o = estado.ofertas.find((x) => x.id === ofertaId)
      if (o) o.vendidos += it.cantidad
    }
  }
  const ids = new Set(c.items.map((it) => it.id))
  estado.items = estado.items.filter((it) => !ids.has(it.id))
  c.estado = 'CONFIRMADA'
  c.lineas = c.items.map((it) => ({ descripcion: it.descripcion, destino: it.destino, precio: it.precioReferencia }))
  c.total = c.lineas.reduce((t, l) => t + l.precio, 0)
  c.resolverEn = undefined
}

function avanzarGuias(usuarioId: string) {
  for (const g of estado.guias.filter((x) => x.usuarioId === usuarioId)) {
    if (g.estado === 'POR_ENVIAR' && !simulacion().estudiarioCaido) {
      g.estado = 'PENDIENTE'
      g.moderarEn = Date.now() + 20_000
    }
    if (g.estado === 'PENDIENTE' && g.moderarEn && Date.now() >= g.moderarEn) {
      g.estado = 'PUBLICADA'
      const clave = normalizarDestino(g.destino)
      estado.publicadas[clave] = [
        ...(estado.publicadas[clave] ?? []),
        { id: g.id, titulo: g.titulo, descripcion: g.descripcion, autor: g.autor, etiquetas: g.etiquetas, calificacion: null, cantidadCalificaciones: 0 },
      ]
    }
  }
}

function validarOferta(o: OfertaNueva, vendidos = 0) {
  const errores: { campo: string; mensaje: string }[] = []
  if (o.titulo.trim().length < 3) errores.push({ campo: 'titulo', mensaje: 'El título tiene que tener al menos 3 caracteres' })
  if (!o.destino.trim()) errores.push({ campo: 'destino', mensaje: 'Falta el destino' })
  if (!(o.precio > 0)) errores.push({ campo: 'precio', mensaje: 'El precio tiene que ser mayor a 0' })
  if (Date.parse(o.hasta) < Date.parse(o.desde)) errores.push({ campo: 'hasta', mensaje: 'La fecha de fin no puede ser anterior a la de inicio' })
  if (o.cupoTotal < vendidos) errores.push({ campo: 'cupoTotal', mensaje: `El cupo no puede ser menor a los ${vendidos} lugares ya vendidos` })
  if (errores.length) problema(400, 'solicitud-invalida', 'Hay datos inválidos.', { errores })
}

// ---------- API ----------

export const apiFalsa: Api = {
  async registrarse(s) {
    await esperar()
    if (estado.usuarios.some((u) => u.email.toLowerCase() === s.email.toLowerCase())) {
      problema(409, 'email-registrado', 'Ya existe una cuenta con ese email.')
    }
    estado.usuarios.push({ id: id('u'), nombre: s.nombre, email: s.email, password: s.password, rol: 'cliente' })
    guardar()
  },

  async iniciarSesion(email, password) {
    await esperar()
    const u = estado.usuarios.find((x) => x.email.toLowerCase() === email.toLowerCase() && x.password === password)
    if (!u) problema(401, 'credenciales-invalidas', 'El email o la contraseña no son correctos.')
    return { token: tokenDeDemostracion(u.id, u.email, u.rol) }
  },

  async buscar(f) {
    await esperar()
    catalogo()
    const sin = (s: string) => normalizarDestino(s)
    let r = estado.ofertas.filter((o) => !o.despublicada)
    if (f.q) r = r.filter((o) => sin(`${o.titulo} ${o.descripcion} ${o.destino}`).includes(sin(f.q!)))
    if (f.destino) r = r.filter((o) => sin(o.destino) === sin(f.destino!))
    if (f.tipo) r = r.filter((o) => o.tipo === f.tipo)
    if (f.subtipoTransporte) r = r.filter((o) => o.subtipoTransporte === f.subtipoTransporte)
    if (f.desde) r = r.filter((o) => o.desde.slice(0, 10) >= f.desde!)
    if (f.hasta) r = r.filter((o) => o.desde.slice(0, 10) <= f.hasta!)
    if (f.precioMin !== undefined) r = r.filter((o) => o.precio >= f.precioMin!)
    if (f.precioMax !== undefined) r = r.filter((o) => o.precio <= f.precioMax!)
    const orden = f.orden ?? 'fecha'
    r = [...r].sort((a, b) =>
      orden === 'precio' ? a.precio - b.precio : orden === '-precio' ? b.precio - a.precio : a.desde.localeCompare(b.desde) || a.id.localeCompare(b.id),
    )
    const tamanio = Math.min(f.tamanio ?? 20, 50)
    const pagina = Math.max(f.pagina ?? 1, 1)
    return { items: r.slice((pagina - 1) * tamanio, pagina * tamanio).map(publica), pagina, tamanio, total: r.length }
  },

  async oferta(ofertaId) {
    await esperar()
    catalogo()
    return publica(buscarOferta(ofertaId))
  },

  async disponibilidad(ofertaId) {
    await esperar()
    catalogo()
    return disponibilidadDe(buscarOferta(ofertaId))
  },

  async guiasDelDestino(ofertaId) {
    await esperar()
    const o = buscarOferta(ofertaId)
    if (simulacion().estudiarioCaido) return { disponible: false, guias: [] }
    const guias = [...(estado.publicadas[normalizarDestino(o.destino)] ?? [])]
    guias.sort((a, b) => (b.calificacion ?? 0) - (a.calificacion ?? 0) || b.cantidadCalificaciones - a.cantidadCalificaciones)
    return { disponible: true, guias: guias.slice(0, 5) }
  },

  async pack(packId) {
    await esperar()
    catalogo()
    const p = estado.packs.find((x) => x.id === packId)
    if (!p) problema(404, 'no-encontrado', 'El pack no existe.')
    return packPublico(p)
  },

  async packs() {
    await esperar()
    catalogo()
    return estado.packs.map(packPublico)
  },

  async carrito() {
    await esperar()
    const u = usuarioActual()
    const items = estado.items.filter((it) => it.usuarioId === u.id).map(itemPublico)
    return { items, totalReferencia: items.filter((it) => it.estado === 'ACTIVO').reduce((t, it) => t + it.precioReferencia, 0) }
  },

  async agregarAlCarrito(s, clave) {
    await esperar()
    const u = usuarioActual()
    const previo = estado.clavesItems[u.id + ':' + clave]
    const existente = estado.items.find((it) => it.id === previo)
    if (existente) return itemPublico(existente)
    if (!(s.cantidad >= 1 && s.cantidad <= MAX_CUPOS_POR_OFERTA)) {
      problema(400, 'solicitud-invalida', '', { errores: [{ campo: 'cantidad', mensaje: 'La cantidad tiene que estar entre 1 y 6' }] })
    }
    if (simulacion().catalogoCaido) problema(503, 'no-disponible', 'Reintentá en unos minutos; el ítem no se agregó.')

    let ofertas: OfertaFalsa[]
    let item: ItemFalso
    const minutos = simulacion().retencionCorta ? 1 : MINUTOS_RETENCION
    const expiraEn = new Date(Date.now() + minutos * 60_000).toISOString()
    if ('packId' in s) {
      const p = estado.packs.find((x) => x.id === s.packId)
      if (!p) problema(404, 'no-encontrado', 'El pack no existe.')
      ofertas = s.opciones.map(buscarOferta)
      if (s.opciones.some((x) => !p.opciones.includes(x))) problema(422, 'regla-incumplida', 'Elegiste una opción que no es de este pack.', { motivo: 'PACK_INVALIDO' })
      const motivo = problemaDelPack(ofertas)
      if (motivo) problema(422, 'regla-incumplida', motivo, { motivo: 'PACK_INVALIDO' })
      const precio = precioDelPack(ofertas, p.descuentoPorcentaje) * s.cantidad
      item = { id: id('it'), usuarioId: u.id, packId: p.id, opciones: s.opciones, cantidad: s.cantidad, descripcion: `Pack ${p.titulo}`, destino: p.destino, precioReferencia: precio, expiraEn, estado: 'ACTIVO' }
    } else {
      const o = buscarOferta(s.ofertaId)
      ofertas = [o]
      item = { id: id('it'), usuarioId: u.id, ofertaId: o.id, opciones: [], cantidad: s.cantidad, descripcion: o.titulo, destino: o.destino, precioReferencia: o.precio * s.cantidad, expiraEn, estado: 'ACTIVO' }
    }
    for (const o of ofertas) {
      const mios = retenidosEnCarritos(o.id).filter((it) => it.usuarioId === u.id).reduce((t, it) => t + it.cantidad, 0)
      if (mios + s.cantidad > MAX_CUPOS_POR_OFERTA) {
        problema(422, 'regla-incumplida', 'Superás el máximo de 6 lugares por oferta.', { motivo: 'LIMITE_POR_CARRITO' })
      }
      const d = disponibilidadDe(o)
      if (d.libres < s.cantidad) {
        const hayRetenidos = o.retenidosPorOtros > 0 || retenidosEnCarritos(o.id).length > 0
        problema(409, 'sin-cupo', 'No queda cupo suficiente.',
          hayRetenidos
            ? { estado: 'RETENIDA', ofertaId: o.id, proximaLiberacion: d.proximaLiberacion ?? o.liberanEn }
            : { estado: 'AGOTADA', ofertaId: o.id })
      }
    }
    estado.items.push(item)
    estado.clavesItems[u.id + ':' + clave] = item.id
    guardar()
    return itemPublico(item)
  },

  async quitarDelCarrito(itemId) {
    await esperar()
    const u = usuarioActual()
    const antes = estado.items.length
    estado.items = estado.items.filter((it) => !(it.id === itemId && it.usuarioId === u.id))
    if (estado.items.length === antes) problema(404, 'no-encontrado', 'El ítem no está en tu carrito.')
    guardar()
  },

  async comprar(clave) {
    await esperar()
    const u = usuarioActual()
    const repetida = estado.compras.find((c) => c.usuarioId === u.id && c.clave === clave)
    if (repetida) {
      resolverCompra(repetida)
      return compraPublica(repetida)
    }
    const items = estado.items.filter((it) => it.usuarioId === u.id)
    if (items.length === 0) problema(422, 'carrito-vacio', 'El carrito está vacío.')
    const c: CompraFalsa = { id: id('c'), usuarioId: u.id, clave, items: structuredClone(items), estado: 'PENDIENTE', total: 0, lineas: [], creadaEn: new Date().toISOString() }
    estado.compras.unshift(c)
    if (simulacion().catalogoCaido || simulacion().compraPendiente) {
      c.resolverEn = Date.now() + 6000
    } else {
      confirmarORechazar(c)
    }
    guardar()
    return compraPublica(c)
  },

  async compra(compraId) {
    await esperar()
    const u = usuarioActual()
    const c = estado.compras.find((x) => x.id === compraId && x.usuarioId === u.id)
    if (!c) problema(404, 'no-encontrado', 'No existe una compra con ese id para tu usuario.')
    resolverCompra(c)
    return compraPublica(c)
  },

  async compras() {
    await esperar()
    const u = usuarioActual()
    const propias = estado.compras.filter((c) => c.usuarioId === u.id)
    propias.forEach(resolverCompra)
    return propias.map(compraPublica)
  },

  async guias() {
    await esperar()
    const u = usuarioActual()
    avanzarGuias(u.id)
    guardar()
    return estado.guias.filter((g) => g.usuarioId === u.id).map(guiaPublica)
  },

  async compartirGuia(s) {
    await esperar()
    const u = usuarioActual()
    if (s.archivo.size > 10 * 1024 * 1024) problema(413, 'archivo-grande', 'El PDF puede pesar hasta 10 MB.')
    if (s.archivo.type && s.archivo.type !== 'application/pdf') problema(415, 'archivo-invalido', 'El archivo tiene que ser PDF.')
    const comprados = estado.compras
      .filter((c) => c.usuarioId === u.id && c.estado === 'CONFIRMADA')
      .flatMap((c) => c.lineas.map((l) => normalizarDestino(l.destino)))
    if (!comprados.includes(normalizarDestino(s.destino))) {
      problema(403, 'destino-no-comprado', 'No tenés una compra confirmada de ese destino.')
    }
    const g: GuiaFalsa = {
      id: id('g'), usuarioId: u.id, autor: s.autor, descripcion: s.descripcion, titulo: s.titulo, destino: s.destino, etiquetas: s.etiquetas,
      estado: simulacion().estudiarioCaido ? 'POR_ENVIAR' : 'PENDIENTE', creadaEn: new Date().toISOString(),
      moderarEn: simulacion().estudiarioCaido ? undefined : Date.now() + 20_000,
    }
    estado.guias.unshift(g)
    guardar()
    return guiaPublica(g)
  },

  async crearOferta(o) {
    await esperar()
    exigirAdmin()
    catalogo()
    validarOferta(o)
    const nueva: OfertaFalsa = { ...o, id: id('of'), vendidos: 0, retenidosPorOtros: 0 }
    estado.ofertas.push(nueva)
    guardar()
    return publica(nueva)
  },

  async editarOferta(ofertaId, o) {
    await esperar()
    exigirAdmin()
    catalogo()
    const actual = buscarOferta(ofertaId)
    validarOferta(o, actual.vendidos)
    Object.assign(actual, o)
    guardar()
    return publica(actual)
  },

  async despublicarOferta(ofertaId) {
    await esperar()
    exigirAdmin()
    catalogo()
    buscarOferta(ofertaId).despublicada = true
    guardar()
  },

  async crearPack(p) {
    await esperar()
    exigirAdmin()
    catalogo()
    const errores: { campo: string; mensaje: string }[] = []
    if (p.titulo.trim().length < 3) errores.push({ campo: 'titulo', mensaje: 'El título tiene que tener al menos 3 caracteres' })
    if (!(p.descuentoPorcentaje > 0 && p.descuentoPorcentaje < 100)) errores.push({ campo: 'descuentoPorcentaje', mensaje: 'El descuento tiene que estar entre 1 y 99 %' })
    const tipos = new Set(p.opciones.map((x) => buscarOferta(x).tipo))
    if (tipos.size < 3) errores.push({ campo: 'opciones', mensaje: 'Ofrecé al menos una opción de cada tipo' })
    if (errores.length) problema(400, 'solicitud-invalida', 'Hay datos inválidos.', { errores })
    const nuevo: PackFalso = { id: id('pk'), titulo: p.titulo, destino: p.destino, descripcion: p.descripcion ?? '', descuentoPorcentaje: p.descuentoPorcentaje, opciones: p.opciones }
    estado.packs.push(nuevo)
    guardar()
    return packPublico(nuevo)
  },
}
