import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomUuid } from './randomUuid'

const v4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('randomUuid', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('uses crypto.randomUUID when the origin is secure', () => {
    expect(randomUuid()).toMatch(v4)
  })

  it('builds a v4 UUID on plain-http origins without crypto.randomUUID', () => {
    const getRandomValues = vi.fn((b: Uint8Array) => b.fill(0xff))
    vi.stubGlobal('crypto', { getRandomValues })
    const id = randomUuid()
    expect(id).toMatch(v4)
    expect(getRandomValues).toHaveBeenCalledOnce()
  })
})
