import { describe, expect, it } from 'vitest'
import type { Oferta } from './api/tipos'
import { normalizarDestino, precioDelPack, problemaDelPack } from './reglas'

const of = (id: string, tipo: Oferta['tipo'], desde: string, hasta: string, precio = 100): Oferta => ({
  id, tipo, titulo: id, descripcion: '', destino: 'Bariloche', desde, hasta, precio,
})

const vuelo = of('vuelo', 'TRANSPORTE', '2026-12-07T08:00:00Z', '2026-12-07T10:00:00Z', 1000)
const hotel = of('hotel', 'ESTADIA', '2026-12-07T14:00:00Z', '2026-12-12T10:00:00Z', 3000)
const excursion = of('excursion', 'EXCURSION', '2026-12-09T09:00:00Z', '2026-12-09T18:00:00Z', 1000)

describe('problemaDelPack (RN-03 y RN-04)', () => {
  it('pide una opción de cada tipo', () => {
    expect(problemaDelPack([vuelo])).toBe('Elegí al menos una opción de estadía, excursión.')
  })
  it('acepta una combinación coherente', () => {
    expect(problemaDelPack([vuelo, hotel, excursion])).toBeNull()
  })
  it('rechaza una estadía que empieza antes de llegar', () => {
    const temprano = { ...hotel, desde: '2026-12-07T09:00:00Z' }
    expect(problemaDelPack([vuelo, temprano, excursion])).toMatch(/empieza antes de que llegue/)
  })
  it('rechaza una excursión fuera de la estadía', () => {
    const tarde = { ...excursion, desde: '2026-12-13T09:00:00Z', hasta: '2026-12-13T12:00:00Z' }
    expect(problemaDelPack([vuelo, hotel, tarde])).toMatch(/fuera de las fechas/)
  })
})

describe('precioDelPack (RN-05)', () => {
  it('resta el descuento a la suma', () => {
    expect(precioDelPack([vuelo, hotel, excursion], 12)).toBe(4400)
  })
})

describe('normalizarDestino', () => {
  it('ignora mayúsculas, acentos y espacios', () => {
    expect(normalizarDestino('  IGUAZÚ ')).toBe(normalizarDestino('iguazu'))
  })
})
