import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CuentaRegresiva } from './ui'

describe('CuentaRegresiva', () => {
  afterEach(() => vi.useRealTimers())

  it('cuenta hacia atrás y avisa al vencer', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-07T12:00:00Z'))
    render(<CuentaRegresiva expiraEn="2026-11-07T12:02:05Z" />)
    expect(screen.getByText('2:05')).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(130_000)
    })
    expect(screen.getByText('Reserva vencida')).toBeInTheDocument()
  })
})
