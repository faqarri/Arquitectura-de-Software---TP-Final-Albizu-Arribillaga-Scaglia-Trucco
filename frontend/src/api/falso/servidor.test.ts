import { beforeEach, describe, expect, it } from 'vitest'
import { guardarToken } from '../../sesion/token'
import { ApiError } from '../errores'
import { apiFalsa as api, reiniciarDatosFalsos } from './servidor'
import { cambiarSimulacion } from './simulacion'

async function ingresar(email = 'ana@example.com', password = 'demo1234') {
  guardarToken((await api.iniciarSesion(email, password)).token)
}

async function error(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p
  } catch (e) {
    return e as ApiError
  }
  throw new Error('se esperaba un error')
}

describe('API de demostración', () => {
  beforeEach(() => {
    reiniciarDatosFalsos()
    cambiarSimulacion({ catalogoCaido: false, estudiarioCaido: false, compraPendiente: false, retencionCorta: false })
    guardarToken(null)
  })

  it('el carrito exige sesión', async () => {
    expect((await error(api.carrito())).status).toBe(401)
  })

  it('agregar con la misma clave no duplica el ítem', async () => {
    await ingresar()
    const a = await api.agregarAlCarrito({ ofertaId: 'of-vuelo-bariloche-0712', cantidad: 2 }, 'clave-1')
    const b = await api.agregarAlCarrito({ ofertaId: 'of-vuelo-bariloche-0712', cantidad: 2 }, 'clave-1')
    expect(b.id).toBe(a.id)
    expect((await api.carrito()).items).toHaveLength(1)
  })

  it('distingue agotada de retenida y respeta el máximo de 6', async () => {
    await ingresar()
    expect((await error(api.agregarAlCarrito({ ofertaId: 'of-exc-cerro-catedral', cantidad: 1 }, 'k1'))).datos.estado).toBe('AGOTADA')
    const retenida = await error(api.agregarAlCarrito({ ofertaId: 'of-exc-cataratas', cantidad: 1 }, 'k2'))
    expect(retenida.datos.estado).toBe('RETENIDA')
    expect(retenida.datos.proximaLiberacion).toBeTruthy()
    await api.agregarAlCarrito({ ofertaId: 'of-vuelo-iguazu', cantidad: 5 }, 'k3')
    expect((await error(api.agregarAlCarrito({ ofertaId: 'of-vuelo-iguazu', cantidad: 2 }, 'k4'))).datos.motivo).toBe('LIMITE_POR_CARRITO')
  })

  it('comprar dos veces con la misma clave devuelve la misma compra', async () => {
    await ingresar()
    await api.agregarAlCarrito({ ofertaId: 'of-vuelo-iguazu', cantidad: 1 }, 'k')
    const c1 = await api.comprar('compra-1')
    const c2 = await api.comprar('compra-1')
    expect(c1.estado).toBe('CONFIRMADA')
    expect(c2.id).toBe(c1.id)
    expect((await api.carrito()).items).toHaveLength(0)
  })

  it('con el catálogo caído la compra queda pendiente', async () => {
    await ingresar()
    await api.agregarAlCarrito({ ofertaId: 'of-vuelo-iguazu', cantidad: 1 }, 'k')
    cambiarSimulacion({ catalogoCaido: true })
    expect((await api.comprar('compra-2')).estado).toBe('PENDIENTE')
  })

  it('solo se comparten guías de destinos comprados', async () => {
    await ingresar()
    const pdf = new File(['%PDF-1.4'], 'guia.pdf', { type: 'application/pdf' })
    const base = { titulo: 'Mi guía', autor: 'Ana', descripcion: '', etiquetas: [], archivo: pdf }
    expect((await error(api.compartirGuia({ ...base, destino: 'Salta' }))).code).toBe('destino-no-comprado')
    cambiarSimulacion({ estudiarioCaido: true })
    expect((await api.compartirGuia({ ...base, destino: 'bariloche' })).estado).toBe('POR_ENVIAR')
    cambiarSimulacion({ estudiarioCaido: false })
    expect((await api.guias())[0].estado).toBe('PENDIENTE')
  })

  it('las guías del destino se omiten si el servicio no responde', async () => {
    cambiarSimulacion({ estudiarioCaido: true })
    expect(await api.guiasDelDestino('of-vuelo-bariloche-0712')).toEqual({ disponible: false, guias: [] })
  })
})
