// Errores de la API (application/problem+json con `code`) y el mensaje que ve el
// usuario. El mensaje se decide por status y code, nunca mostrando el error técnico.

export interface ErrorDeCampo {
  campo: string
  mensaje: string
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly detalle: string,
    readonly datos: Record<string, unknown> = {},
    readonly reintentarEn?: number,
  ) {
    super(detalle || code)
    this.name = 'ApiError'
  }

  get errores(): ErrorDeCampo[] {
    const e = this.datos.errores
    return Array.isArray(e) ? (e as ErrorDeCampo[]) : []
  }
}

/** Sin respuesta del servidor: sin conexión o el gateway no responde. */
export class ErrorDeRed extends Error {
  constructor() {
    super('sin conexión')
    this.name = 'ErrorDeRed'
  }
}

export interface MensajeError {
  titulo: string
  detalle: string
  /** El mismo pedido puede funcionar si se repite más tarde. */
  reintentable: boolean
}

const motivos: Record<string, string> = {
  LIMITE_POR_CARRITO: 'Podés reservar hasta 6 lugares de una misma oferta por carrito.',
  PACK_INVALIDO: 'La combinación elegida no es válida para este pack.',
  OFERTA_INEXISTENTE: 'La oferta ya no está publicada.',
}

export function mensajeDeError(e: unknown): MensajeError {
  if (e instanceof ErrorDeRed) {
    return {
      titulo: 'Sin conexión',
      detalle: 'No pudimos comunicarnos con el servidor. Revisá tu conexión e intentá de nuevo.',
      reintentable: true,
    }
  }
  if (!(e instanceof ApiError)) {
    return { titulo: 'Algo salió mal', detalle: 'Ocurrió un error inesperado. Intentá de nuevo.', reintentable: true }
  }
  if (e.status === 429) {
    const espera = e.reintentarEn ? ` Esperá ${e.reintentarEn} segundos.` : ' Esperá unos segundos.'
    return { titulo: 'Demasiadas solicitudes', detalle: 'Hiciste muchas solicitudes seguidas.' + espera, reintentable: true }
  }
  if (e.status >= 500) {
    return {
      titulo: 'Servicio no disponible',
      detalle: e.detalle || 'El servicio no responde en este momento. Intentá de nuevo en unos segundos.',
      reintentable: true,
    }
  }
  switch (e.code) {
    case 'sin-cupo':
      return e.datos.estado === 'RETENIDA'
        ? {
            titulo: 'Sin cupo por ahora',
            detalle: 'Hay lugares reservados en otros carritos que pueden liberarse' + proximaLiberacion(e.datos) + '.',
            reintentable: true,
          }
        : { titulo: 'Agotada', detalle: 'Ya no quedan lugares para esta oferta.', reintentable: false }
    case 'regla-incumplida':
      return {
        titulo: 'No se puede agregar',
        detalle: motivos[String(e.datos.motivo)] ?? e.detalle,
        reintentable: false,
      }
    case 'destino-no-comprado':
      return {
        titulo: 'Destino no comprado',
        detalle: 'Solo podés compartir guías de destinos que compraste.',
        reintentable: false,
      }
    case 'archivo-grande':
      return { titulo: 'Archivo demasiado grande', detalle: 'El PDF puede pesar hasta 10 MB.', reintentable: false }
    case 'archivo-invalido':
      return { titulo: 'Archivo inválido', detalle: 'El archivo tiene que ser un PDF.', reintentable: false }
    case 'carrito-vacio':
      return { titulo: 'Carrito vacío', detalle: 'Agregá al menos una oferta antes de comprar.', reintentable: false }
    case 'no-autenticado':
      return { titulo: 'Sesión vencida', detalle: 'Ingresá de nuevo para continuar.', reintentable: false }
    case 'credenciales-invalidas':
      return { titulo: 'No pudimos ingresar', detalle: 'El email o la contraseña no son correctos.', reintentable: false }
    case 'email-registrado':
      return {
        titulo: 'Email ya registrado',
        detalle: 'Ya existe una cuenta con ese email. Probá ingresar.',
        reintentable: false,
      }
    case 'no-encontrado':
      return { titulo: 'No encontrado', detalle: 'Lo que buscás no existe o ya no está disponible.', reintentable: false }
    case 'solicitud-invalida':
      return {
        titulo: 'Revisá los datos',
        detalle: e.errores.map((x) => x.mensaje).join('. ') || 'Hay datos inválidos.',
        reintentable: false,
      }
    default:
      return { titulo: 'No se pudo completar', detalle: e.detalle || 'Revisá los datos e intentá de nuevo.', reintentable: false }
  }
}

function proximaLiberacion(datos: Record<string, unknown>): string {
  const p = datos.proximaLiberacion
  if (typeof p !== 'string') return ''
  const d = new Date(p)
  if (Number.isNaN(d.getTime())) return ''
  return ' a partir de las ' + d.toLocaleTimeString('es-AR', { hour: '2-digit', minute: '2-digit' })
}
