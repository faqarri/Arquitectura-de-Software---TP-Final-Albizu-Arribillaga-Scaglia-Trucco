// Formatos de la API pública del gateway. Los de Reservas son los reales
// (services/reservas/README.md); los de Catálogo y Usuarios son los que espera
// el frontend hasta que esos servicios publiquen sus ejemplos (ver frontend/README.md).

export type TipoOferta = 'TRANSPORTE' | 'ESTADIA' | 'EXCURSION'
export type SubtipoTransporte = 'AVION' | 'TREN' | 'COLECTIVO'

export interface Oferta {
  id: string
  tipo: TipoOferta
  subtipoTransporte?: SubtipoTransporte
  titulo: string
  descripcion: string
  destino: string
  origen?: string
  desde: string
  hasta: string
  precio: number
  cupoTotal?: number
  vendidos?: number
}

export type Orden = 'precio' | '-precio' | 'fecha'

export interface FiltrosBusqueda {
  q?: string
  destino?: string
  tipo?: TipoOferta
  subtipoTransporte?: SubtipoTransporte
  desde?: string
  hasta?: string
  precioMin?: number
  precioMax?: number
  orden?: Orden
  pagina?: number
  tamanio?: number
}

export interface PaginaOfertas {
  items: Oferta[]
  pagina: number
  tamanio: number
  total: number
}

export type EstadoDisponibilidad = 'DISPONIBLE' | 'RETENIDA' | 'AGOTADA'

export interface Disponibilidad {
  ofertaId: string
  estado: EstadoDisponibilidad
  libres: number
  proximaLiberacion?: string | null
}

export interface Pack {
  id: string
  titulo: string
  destino: string
  descripcion?: string
  descuentoPorcentaje: number
  opciones: Oferta[]
}

export interface GuiaDestino {
  id: string
  titulo: string
  descripcion?: string
  autor: string
  etiquetas: string[]
  calificacion: number | null
  cantidadCalificaciones: number
  urlDescarga?: string
}

export interface GuiasDestino {
  disponible: boolean
  guias: GuiaDestino[]
}

// ---------- Reservas ----------

export interface ItemCarrito {
  id: string
  ofertaId?: string
  packId?: string
  cantidad: number
  descripcion: string
  destino: string
  precioReferencia: number
  expiraEn: string
  estado: 'ACTIVO' | 'VENCIDO'
}

export interface Carrito {
  items: ItemCarrito[]
  totalReferencia: number
}

export type SolicitudAgregar =
  | { ofertaId: string; cantidad: number }
  | { packId: string; opciones: string[]; cantidad: number }

export type EstadoCompra = 'PENDIENTE' | 'CONFIRMADA' | 'RECHAZADA'

export interface Compra {
  id: string
  estado: EstadoCompra
  total: number
  lineas: { descripcion: string; destino: string; precio: number }[]
  retencionesVencidas?: string[]
  creadaEn: string
}

export type EstadoGuia = 'POR_ENVIAR' | 'PENDIENTE' | 'PUBLICADA' | 'RECHAZADA'

export interface Guia {
  id: string
  titulo: string
  destino: string
  etiquetas: string[]
  estado: EstadoGuia
  motivoRechazo?: string
  creadaEn: string
}

export interface SolicitudGuia {
  titulo: string
  destino: string
  autor: string
  descripcion: string
  etiquetas: string[]
  archivo: File
}

// ---------- Usuarios ----------

export type Rol = 'cliente' | 'admin'

export interface SolicitudRegistro {
  nombre: string
  email: string
  password: string
}

export interface OfertaNueva {
  tipo: TipoOferta
  subtipoTransporte?: SubtipoTransporte
  titulo: string
  descripcion: string
  destino: string
  origen?: string
  desde: string
  hasta: string
  precio: number
  cupoTotal: number
}

export interface PackNuevo {
  titulo: string
  destino: string
  descripcion?: string
  descuentoPorcentaje: number
  opciones: string[]
}

export interface Api {
  registrarse(s: SolicitudRegistro): Promise<void>
  iniciarSesion(email: string, password: string): Promise<{ token: string }>

  buscar(f: FiltrosBusqueda): Promise<PaginaOfertas>
  oferta(id: string): Promise<Oferta>
  disponibilidad(id: string): Promise<Disponibilidad>
  guiasDelDestino(ofertaId: string): Promise<GuiasDestino>
  pack(id: string): Promise<Pack>
  packs(): Promise<Pack[]>

  carrito(): Promise<Carrito>
  agregarAlCarrito(s: SolicitudAgregar, idempotencyKey: string): Promise<ItemCarrito>
  quitarDelCarrito(itemId: string): Promise<void>

  /** 201 confirmada, 202 pendiente, 409 rechazada y 200 repetida devuelven la compra. */
  comprar(idempotencyKey: string): Promise<Compra>
  compra(id: string): Promise<Compra>
  compras(): Promise<Compra[]>

  guias(): Promise<Guia[]>
  compartirGuia(s: SolicitudGuia): Promise<Guia>

  crearOferta(o: OfertaNueva): Promise<Oferta>
  editarOferta(id: string, o: OfertaNueva): Promise<Oferta>
  despublicarOferta(id: string): Promise<void>
  crearPack(p: PackNuevo): Promise<Pack>
}
