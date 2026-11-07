// Reglas de negocio que el frontend verifica antes de enviar, para prevenir
// errores (el servicio las vuelve a validar).

import type { Oferta, TipoOferta } from './api/tipos'

export const MAX_CUPOS_POR_OFERTA = 6 // RN-12
export const MAX_TAMANIO_PDF = 10 * 1024 * 1024 // RN-20 de Estudiario
export const MAX_ETIQUETAS = 10

export const nombreTipo: Record<TipoOferta, string> = {
  TRANSPORTE: 'Transporte',
  ESTADIA: 'Estadía',
  EXCURSION: 'Excursión',
}

/** RN-03 y RN-04: al menos una opción de cada tipo y fechas coherentes. Devuelve el problema o null. */
export function problemaDelPack(elegidas: Oferta[]): string | null {
  const de = (t: TipoOferta) => elegidas.filter((o) => o.tipo === t)
  const faltan = (['TRANSPORTE', 'ESTADIA', 'EXCURSION'] as TipoOferta[]).filter((t) => de(t).length === 0)
  if (faltan.length > 0) {
    return 'Elegí al menos una opción de ' + faltan.map((t) => nombreTipo[t].toLowerCase()).join(', ') + '.'
  }
  const llegada = Math.max(...de('TRANSPORTE').map((o) => Date.parse(o.hasta)))
  for (const e of de('ESTADIA')) {
    if (Date.parse(e.desde) < llegada) {
      return `La estadía "${e.titulo}" empieza antes de que llegue el transporte.`
    }
  }
  for (const x of de('EXCURSION')) {
    const dentro = de('ESTADIA').some((e) => Date.parse(x.desde) >= Date.parse(e.desde) && Date.parse(x.hasta) <= Date.parse(e.hasta))
    if (!dentro) return `La excursión "${x.titulo}" queda fuera de las fechas de la estadía.`
  }
  return null
}

/** RN-05: suma de las opciones menos el descuento del pack. */
export function precioDelPack(elegidas: Oferta[], descuentoPorcentaje: number): number {
  const suma = elegidas.reduce((t, o) => t + o.precio, 0)
  return Math.round(suma * (100 - descuentoPorcentaje)) / 100
}

/** Compara destinos sin distinguir mayúsculas ni acentos, igual que Reservas. */
export function normalizarDestino(s: string): string {
  return s.normalize('NFD').replace(/[̀-ͯ]/g, '').trim().toLowerCase()
}
