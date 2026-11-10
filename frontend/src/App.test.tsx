import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import { reiniciarDatosFalsos } from './api/falso/servidor'
import { App } from './App'

beforeAll(() => {
  // jsdom no implementa <dialog>.
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
})

describe('recorrido del cliente', () => {
  beforeEach(() => reiniciarDatosFalsos())

  it('ingresa, agrega al carrito y compra', async () => {
    const u = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/ofertas/of-vuelo-iguazu']}>
        <App />
      </MemoryRouter>,
    )

    await u.click(await screen.findByRole('link', { name: 'Ingresá para reservar' }))
    await u.click(screen.getAllByRole('button', { name: 'Completar' })[0])
    await u.click(screen.getByRole('button', { name: 'Ingresar' }))

    // Vuelve a la oferta donde estaba.
    await screen.findByRole('heading', { level: 1, name: 'Vuelo Buenos Aires → Puerto Iguazú' })
    await u.click(await screen.findByRole('button', { name: 'Agregar al carrito' }, { timeout: 3000 }))
    expect(await screen.findByText('Agregado al carrito')).toBeInTheDocument()

    await u.click(screen.getByRole('link', { name: /Carrito/ }))
    await screen.findByText(/Reservado por/)
    await u.click(screen.getByRole('button', { name: /^Comprar/ }))
    const dialogo = screen.getByRole('dialog', { hidden: true })
    await u.click(within(dialogo).getByRole('button', { name: /Confirmar y pagar/, hidden: true }))

    expect(await screen.findByRole('heading', { name: '¡Listo! Tu compra está confirmada' }, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Compartir una guía de Iguazú' })).toBeInTheDocument()
  }, 15000)

  it('el carrito sin sesión lleva a ingresar', async () => {
    render(
      <MemoryRouter initialEntries={['/carrito']}>
        <App />
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { level: 1, name: 'Ingresar' })).toBeInTheDocument()
  })
})
