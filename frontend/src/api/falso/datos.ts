// Datos de demostración, con la forma de las respuestas reales.

import type { GuiaDestino, Oferta } from '../tipos'

export interface OfertaFalsa extends Oferta {
  cupoTotal: number
  vendidos: number
  /** Cupos retenidos en carritos de otros clientes y cuándo vencen. */
  retenidosPorOtros: number
  liberanEn?: string
  despublicada?: boolean
}

const o = (x: Omit<OfertaFalsa, 'retenidosPorOtros' | 'vendidos'> & Partial<OfertaFalsa>): OfertaFalsa => ({
  vendidos: 0,
  retenidosPorOtros: 0,
  ...x,
})

export function ofertasIniciales(): OfertaFalsa[] {
  const enUnRato = new Date(Date.now() + 9 * 60_000).toISOString()
  return [
    o({ id: 'of-vuelo-bariloche-0712', tipo: 'TRANSPORTE', subtipoTransporte: 'AVION', titulo: 'Vuelo Córdoba → Bariloche', descripcion: 'Vuelo directo, equipaje de mano y despacho de 15 kg incluidos.', destino: 'Bariloche', origen: 'Córdoba', desde: '2026-12-07T08:30:00-03:00', hasta: '2026-12-07T10:45:00-03:00', precio: 185000, cupoTotal: 40, vendidos: 22 }),
    o({ id: 'of-hotel-llao-llao', tipo: 'ESTADIA', titulo: 'Hotel con vista al lago Nahuel Huapi', descripcion: 'Habitación doble con desayuno, 5 noches. Spa y traslado al centro.', destino: 'Bariloche', desde: '2026-12-07T14:00:00-03:00', hasta: '2026-12-12T10:00:00-03:00', precio: 640000, cupoTotal: 12, vendidos: 9 }),
    o({ id: 'of-exc-cerro-catedral', tipo: 'EXCURSION', titulo: 'Día completo en Cerro Catedral', descripcion: 'Aerosilla, almuerzo de montaña y guía bilingüe.', destino: 'Bariloche', desde: '2026-12-09T09:00:00-03:00', hasta: '2026-12-09T18:00:00-03:00', precio: 98000, cupoTotal: 20, vendidos: 20, retenidosPorOtros: 0 }),
    o({ id: 'of-exc-circuito-chico', tipo: 'EXCURSION', titulo: 'Circuito Chico en bicicleta', descripcion: 'Recorrido de 25 km con paradas en miradores. Incluye bici y casco.', destino: 'Bariloche', desde: '2026-12-10T09:30:00-03:00', hasta: '2026-12-10T15:00:00-03:00', precio: 45000, cupoTotal: 15, vendidos: 12, retenidosPorOtros: 3, liberanEn: enUnRato }),
    o({ id: 'of-colectivo-mendoza', tipo: 'TRANSPORTE', subtipoTransporte: 'COLECTIVO', titulo: 'Colectivo cama Córdoba → Mendoza', descripcion: 'Servicio cama ejecutivo con cena a bordo.', destino: 'Mendoza', origen: 'Córdoba', desde: '2026-12-18T22:00:00-03:00', hasta: '2026-12-19T07:30:00-03:00', precio: 62000, cupoTotal: 44, vendidos: 10 }),
    o({ id: 'of-bodega-lujan', tipo: 'EXCURSION', titulo: 'Bodegas de Luján de Cuyo', descripcion: 'Visita a tres bodegas con degustación y almuerzo de pasos.', destino: 'Mendoza', desde: '2026-12-20T10:00:00-03:00', hasta: '2026-12-20T17:00:00-03:00', precio: 120000, cupoTotal: 16, vendidos: 4 }),
    o({ id: 'of-posada-chacras', tipo: 'ESTADIA', titulo: 'Posada entre viñedos en Chacras de Coria', descripcion: 'Habitación superior, 3 noches, desayuno regional y pileta.', destino: 'Mendoza', desde: '2026-12-19T13:00:00-03:00', hasta: '2026-12-22T10:00:00-03:00', precio: 390000, cupoTotal: 8, vendidos: 3 }),
    o({ id: 'of-tren-salta', tipo: 'TRANSPORTE', subtipoTransporte: 'TREN', titulo: 'Tren a las Nubes', descripcion: 'Salida desde San Antonio de los Cobres hasta el viaducto La Polvorilla.', destino: 'Salta', origen: 'Salta', desde: '2027-01-11T07:00:00-03:00', hasta: '2027-01-11T19:00:00-03:00', precio: 150000, cupoTotal: 30, vendidos: 30, retenidosPorOtros: 0 }),
    o({ id: 'of-hotel-salta-centro', tipo: 'ESTADIA', titulo: 'Hotel boutique en el centro de Salta', descripcion: 'Casona colonial restaurada, 4 noches con desayuno.', destino: 'Salta', desde: '2027-01-10T14:00:00-03:00', hasta: '2027-01-14T10:00:00-03:00', precio: 420000, cupoTotal: 10, vendidos: 2 }),
    o({ id: 'of-exc-cafayate', tipo: 'EXCURSION', titulo: 'Quebrada de las Conchas y Cafayate', descripcion: 'Día completo con paradas en la Garganta del Diablo y bodegas.', destino: 'Salta', desde: '2027-01-12T07:30:00-03:00', hasta: '2027-01-12T20:00:00-03:00', precio: 89000, cupoTotal: 18, vendidos: 6 }),
    o({ id: 'of-vuelo-iguazu', tipo: 'TRANSPORTE', subtipoTransporte: 'AVION', titulo: 'Vuelo Buenos Aires → Puerto Iguazú', descripcion: 'Vuelo directo con equipaje de 23 kg.', destino: 'Iguazú', origen: 'Buenos Aires', desde: '2027-02-02T06:15:00-03:00', hasta: '2027-02-02T08:10:00-03:00', precio: 210000, cupoTotal: 60, vendidos: 15 }),
    o({ id: 'of-exc-cataratas', tipo: 'EXCURSION', titulo: 'Cataratas lado argentino con Gran Aventura', descripcion: 'Paseo en lancha bajo los saltos y circuito superior.', destino: 'Iguazú', desde: '2027-02-03T08:00:00-03:00', hasta: '2027-02-03T16:00:00-03:00', precio: 135000, cupoTotal: 25, vendidos: 25, retenidosPorOtros: 2, liberanEn: enUnRato }),
    o({ id: 'of-lodge-selva', tipo: 'ESTADIA', titulo: 'Lodge en la selva misionera', descripcion: 'Cabaña con galería, 3 noches con media pensión.', destino: 'Iguazú', desde: '2027-02-02T13:00:00-03:00', hasta: '2027-02-05T10:00:00-03:00', precio: 510000, cupoTotal: 6, vendidos: 1 }),
    o({ id: 'of-vuelo-ushuaia', tipo: 'TRANSPORTE', subtipoTransporte: 'AVION', titulo: 'Vuelo Córdoba → Ushuaia', descripcion: 'Con escala en Buenos Aires. Equipaje de 23 kg.', destino: 'Ushuaia', origen: 'Córdoba', desde: '2027-03-05T05:40:00-03:00', hasta: '2027-03-05T13:20:00-03:00', precio: 295000, cupoTotal: 50, vendidos: 8 }),
    o({ id: 'of-exc-canal-beagle', tipo: 'EXCURSION', titulo: 'Navegación por el Canal Beagle', descripcion: 'Isla de los Lobos, faro Les Eclaireurs y pingüinera.', destino: 'Ushuaia', desde: '2027-03-06T09:30:00-03:00', hasta: '2027-03-06T15:30:00-03:00', precio: 110000, cupoTotal: 30, vendidos: 11 }),
    o({ id: 'of-tren-cordoba-sierras', tipo: 'TRANSPORTE', subtipoTransporte: 'TREN', titulo: 'Tren de las Sierras', descripcion: 'Recorrido de Alta Córdoba a Cosquín por el valle de Punilla.', destino: 'Córdoba', origen: 'Córdoba', desde: '2026-11-28T08:00:00-03:00', hasta: '2026-11-28T11:00:00-03:00', precio: 18000, cupoTotal: 80, vendidos: 34 }),
  ]
}

export interface PackFalso {
  id: string
  titulo: string
  destino: string
  descripcion: string
  descuentoPorcentaje: number
  opciones: string[]
}

export function packsIniciales(): PackFalso[] {
  return [
    { id: 'pk-bariloche-verano', titulo: 'Bariloche completo', destino: 'Bariloche', descripcion: 'Elegí tu vuelo, tu estadía y al menos una excursión: 12 % de descuento sobre el total.', descuentoPorcentaje: 12, opciones: ['of-vuelo-bariloche-0712', 'of-hotel-llao-llao', 'of-exc-cerro-catedral', 'of-exc-circuito-chico'] },
    { id: 'pk-mendoza-vinos', titulo: 'Mendoza entre vinos', destino: 'Mendoza', descripcion: 'Colectivo cama, posada entre viñedos y bodegas: 10 % de descuento.', descuentoPorcentaje: 10, opciones: ['of-colectivo-mendoza', 'of-posada-chacras', 'of-bodega-lujan'] },
  ]
}

/** Guías ya publicadas en Estudiario, por destino. */
export const guiasPublicadas: Record<string, GuiaDestino[]> = {
  bariloche: [
    { id: 'd7f3k2m9qx4p8r1s', titulo: 'Trekking a Refugio Frey', descripcion: 'Dos días de caminata, qué llevar y dónde acampar.', autor: 'Lucía Fernández', etiquetas: ['trekking', 'verano'], calificacion: 4.6, cantidadCalificaciones: 12 },
    { id: 'k9m2p4q8r1s7t3v6', titulo: 'Bariloche con chicos', descripcion: 'Actividades para hacer en familia y dónde comer.', autor: 'Martín Gómez', etiquetas: ['familia'], calificacion: 4.1, cantidadCalificaciones: 7 },
    { id: 'z1x2c3v4b5n6m7a8', titulo: 'Chocolaterías del centro', autor: 'Sofía Ruiz', etiquetas: ['gastronomía'], calificacion: null, cantidadCalificaciones: 0 },
  ],
  mendoza: [
    { id: 'm4n5b6v7c8x9z0a1', titulo: 'Ruta del vino en bici', descripcion: 'Un día pedaleando por Maipú, con mapa de bodegas.', autor: 'Pablo Díaz', etiquetas: ['vino', 'bici'], calificacion: 4.8, cantidadCalificaciones: 21 },
  ],
}
