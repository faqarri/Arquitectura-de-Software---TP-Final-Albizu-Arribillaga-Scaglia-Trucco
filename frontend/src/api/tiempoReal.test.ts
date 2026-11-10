import { afterEach, describe, expect, it, vi } from 'vitest'
import { escucharEventos } from './tiempoReal'

function respuestaSSE(trozos: string[]) {
  const codificador = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      for (const t of trozos) c.enqueue(codificador.encode(t))
      // queda abierta, como una conexión SSE real
    },
  })
  return { ok: true, status: 200, body } as unknown as Response
}

afterEach(() => vi.unstubAllGlobals())

describe('escucharEventos', () => {
  it('interpreta los eventos aunque lleguen partidos y saltea los latidos', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        respuestaSSE(['retry: 3000\n: conectado\n\n', 'event: disponi', 'bilidad\ndata: {"libres":3,', '"version":2}\n\n: latido\n\n']),
      ),
    )
    const recibidos: [string, unknown][] = []
    const cerrar = escucharEventos('/tiempo-real/ofertas?ids=of-1', (n, d) => recibidos.push([n, d]))
    await vi.waitFor(() => expect(recibidos).toHaveLength(1))
    expect(recibidos[0]).toEqual(['disponibilidad', { libres: 3, version: 2 }])
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('/api/tiempo-real/ofertas?ids=of-1')
    cerrar()
  })

  it('no reintenta una solicitud rechazada (por ejemplo, sin sesión)', async () => {
    vi.useFakeTimers()
    const f = vi.fn().mockResolvedValue({ ok: false, status: 401, body: null } as unknown as Response)
    vi.stubGlobal('fetch', f)
    const cerrar = escucharEventos('/tiempo-real/mis-retenciones', () => {})
    await vi.advanceTimersByTimeAsync(10000)
    expect(f).toHaveBeenCalledTimes(1)
    cerrar()
    vi.useRealTimers()
  })

  it('reconecta si el servicio no responde', async () => {
    vi.useFakeTimers()
    const f = vi.fn().mockRejectedValue(new TypeError('red caída'))
    vi.stubGlobal('fetch', f)
    const cerrar = escucharEventos('/tiempo-real/ofertas?ids=of-1', () => {})
    await vi.advanceTimersByTimeAsync(6500)
    expect(f.mock.calls.length).toBeGreaterThanOrEqual(3)
    cerrar()
    vi.useRealTimers()
  })
})
