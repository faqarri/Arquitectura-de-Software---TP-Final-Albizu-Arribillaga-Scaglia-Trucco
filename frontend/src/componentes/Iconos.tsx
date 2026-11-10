// Íconos de línea en SVG: se ven igual en todos los sistemas (los emojis no) y
// toman el color del texto. Son decorativos: el significado siempre está en el texto.

import type { SubtipoTransporte, TipoOferta } from '../api/tipos'

const trazos = {
  avion: ['M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z'],
  tren: ['M4 15V6a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3v9a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3Z', 'M4 11h16', 'M8 15h.01', 'M16 15h.01', 'm8 19-2 3', 'm16 19 2 3'],
  colectivo: ['M8 6v6', 'M15 6v6', 'M2 12h19.6', 'M18 18h3s.5-1.7.8-2.8c.1-.4.2-.8.2-1.2 0-.4-.1-.8-.2-1.2l-1.4-5C20.1 6.8 19.1 6 18 6H4a2 2 0 0 0-2 2v10h3', 'M9 18h5', 'M5 18a2 2 0 1 0 4 0 2 2 0 1 0-4 0', 'M14 18a2 2 0 1 0 4 0 2 2 0 1 0-4 0'],
  estadia: ['M2 4v16', 'M2 8h18a2 2 0 0 1 2 2v10', 'M2 17h20', 'M6 8v9'],
  excursion: ['M2 12a10 10 0 1 0 20 0 10 10 0 1 0-20 0', 'm16.24 7.76-2.12 6.36-6.36 2.12 2.12-6.36z'],
  ubicacion: ['M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0Z', 'M9 10a3 3 0 1 0 6 0 3 3 0 1 0-6 0'],
  carrito: ['M7 21a1 1 0 1 0 2 0 1 1 0 1 0-2 0', 'M18 21a1 1 0 1 0 2 0 1 1 0 1 0-2 0', 'M2.05 2.05h2l2.66 12.42a2 2 0 0 0 2 1.58h9.78a2 2 0 0 0 1.95-1.57l1.65-7.43H5.12'],
  archivo: ['M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z', 'M14 2v4a2 2 0 0 0 2 2h4'],
  reloj: ['M2 12a10 10 0 1 0 20 0 10 10 0 1 0-20 0', 'M12 6v6l4 2'],
  check: ['M20 6 9 17l-5-5'],
  x: ['M18 6 6 18', 'm6 6 12 12'],
  info: ['M2 12a10 10 0 1 0 20 0 10 10 0 1 0-20 0', 'M12 16v-4', 'M12 8h.01'],
  alerta: ['M2 12a10 10 0 1 0 20 0 10 10 0 1 0-20 0', 'M12 8v4', 'M12 16h.01'],
  ayuda: ['M2 12a10 10 0 1 0 20 0 10 10 0 1 0-20 0', 'M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3', 'M12 17h.01'],
  reintentar: ['M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8', 'M21 3v5h-5', 'M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16', 'M8 16H3v5'],
  menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
  estrella: ['M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z'],
} as const

export type NombreIcono = keyof typeof trazos

export function Icono({ nombre, relleno = false, className = '' }: { nombre: NombreIcono; relleno?: boolean; className?: string }) {
  return (
    <svg
      className={`icono ${className}`}
      viewBox="0 0 24 24"
      fill={relleno ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {trazos[nombre].map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  )
}

const porTipo: Record<TipoOferta, NombreIcono> = { TRANSPORTE: 'avion', ESTADIA: 'estadia', EXCURSION: 'excursion' }
const porSubtipo: Record<SubtipoTransporte, NombreIcono> = { AVION: 'avion', TREN: 'tren', COLECTIVO: 'colectivo' }

export function IconoOferta({ tipo, subtipo }: { tipo: TipoOferta; subtipo?: SubtipoTransporte }) {
  return <Icono nombre={subtipo ? porSubtipo[subtipo] : porTipo[tipo]} />
}
