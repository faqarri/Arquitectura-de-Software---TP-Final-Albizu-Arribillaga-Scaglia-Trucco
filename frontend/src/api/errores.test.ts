import { describe, expect, it } from 'vitest'
import { ApiError, ErrorDeRed, mensajeDeError } from './errores'

describe('mensajeDeError', () => {
  it('distingue retenida de agotada (RN-13) y muestra la hora de liberación', () => {
    const retenida = mensajeDeError(new ApiError(409, 'sin-cupo', '', { estado: 'RETENIDA', proximaLiberacion: '2026-11-07T18:45:00Z' }))
    expect(retenida.titulo).toBe('Sin cupo por ahora')
    expect(retenida.detalle).toMatch(/a partir de las/)
    expect(retenida.reintentable).toBe(true)

    const agotada = mensajeDeError(new ApiError(409, 'sin-cupo', '', { estado: 'AGOTADA' }))
    expect(agotada.titulo).toBe('Agotada')
    expect(agotada.reintentable).toBe(false)
  })

  it('traduce el límite por carrito sin mostrar el código', () => {
    const m = mensajeDeError(new ApiError(422, 'regla-incumplida', 'x', { motivo: 'LIMITE_POR_CARRITO' }))
    expect(m.detalle).toBe('Podés reservar hasta 6 lugares de una misma oferta por carrito.')
  })

  it('un 429 dice cuánto esperar', () => {
    expect(mensajeDeError(new ApiError(429, '429', '', {}, 12)).detalle).toMatch(/Esperá 12 segundos/)
  })

  it('un 503 o un corte de red se pueden reintentar', () => {
    expect(mensajeDeError(new ApiError(503, 'no-disponible', '')).reintentable).toBe(true)
    expect(mensajeDeError(new ErrorDeRed()).titulo).toBe('Sin conexión')
  })

  it('junta los errores por campo', () => {
    const e = new ApiError(400, 'solicitud-invalida', '', { errores: [{ campo: 'titulo', mensaje: 'Muy corto' }] })
    expect(mensajeDeError(e).detalle).toBe('Muy corto')
    expect(e.errores).toEqual([{ campo: 'titulo', mensaje: 'Muy corto' }])
  })
})
