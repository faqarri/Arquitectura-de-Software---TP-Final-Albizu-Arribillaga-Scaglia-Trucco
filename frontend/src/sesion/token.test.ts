import { describe, expect, it } from 'vitest'
import { tokenDeDemostracion, usuarioDelToken } from './token'

describe('usuarioDelToken', () => {
  it('lee los claims del JWT', () => {
    const u = usuarioDelToken(tokenDeDemostracion('u-1', 'ñandú@example.com', 'admin'))
    expect(u).toMatchObject({ id: 'u-1', email: 'ñandú@example.com', rol: 'admin' })
  })
  it('descarta un token vencido o mal formado', () => {
    const b64 = (o: unknown) => btoa(JSON.stringify(o))
    expect(usuarioDelToken(`${b64({})}.${b64({ sub: 'u', email: 'a@b.c', exp: 1 })}.x`)).toBeNull()
    expect(usuarioDelToken('basura')).toBeNull()
    expect(usuarioDelToken(null)).toBeNull()
  })
})
