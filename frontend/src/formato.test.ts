import { afterEach, describe, expect, it, vi } from 'vitest'
import { duracion, nuevoId } from './formato'

describe('nuevoId', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('genera un UUID v4 aunque no haya crypto.randomUUID (página servida por HTTP)', () => {
    const real = globalThis.crypto
    vi.stubGlobal('crypto', { getRandomValues: real.getRandomValues.bind(real) })
    expect(nuevoId()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  })
})

describe('duracion', () => {
  it('muestra minutos y segundos, sin negativos', () => {
    expect(duracion(125_000)).toBe('2:05')
    expect(duracion(-5)).toBe('0:00')
  })
})
