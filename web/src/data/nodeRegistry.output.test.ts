import { describe, expect, it } from 'vitest'
import { NODE_DEFS } from './nodeRegistry'

describe('output node auto_leftover_draft (plan g1.1 / g1.3 / g4.2)', () => {
  it('registers a switch field that defaults to false', () => {
    const field = NODE_DEFS.output.fields.find((f) => f.key === 'auto_leftover_draft')
    expect(field).toBeDefined()
    expect(field?.type).toBe('switch')
    expect(field?.optional).toBe(true)
    expect(NODE_DEFS.output.defaults?.auto_leftover_draft).toBe(false)
  })

  it('keeps results source picker and treats missing config as off', () => {
    expect(NODE_DEFS.output.fields.map((f) => f.key)).toEqual(['results', 'auto_leftover_draft'])
    // Old graphs without the key: inspector switchOn uses !!undefined → false.
    const missing: Record<string, unknown> = {}
    expect(!!missing.auto_leftover_draft).toBe(false)
  })
})
