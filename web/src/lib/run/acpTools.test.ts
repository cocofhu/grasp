import { describe, expect, it } from 'vitest'
import { MAX_AGENT_TOOLS, sameTools, toolsFromAcp } from './acpTools'

describe('toolsFromAcp', () => {
  it('keeps tool_call rows in order with name and status only', () => {
    expect(
      toolsFromAcp([
        { kind: 'thought', title: 'x' },
        { kind: 'tool_call', title: 'read_file', status: 'completed' },
        { kind: 'tool_call', title: '' },
        { kind: 'plan', title: 'p' },
        { kind: 'tool_call', title: 'Shell' },
      ]),
    ).toEqual([{ title: 'read_file', status: 'completed' }, { title: 'Shell' }])
  })

  it('handles missing input and caps the list', () => {
    expect(toolsFromAcp(undefined)).toEqual([])
    const many = Array.from({ length: MAX_AGENT_TOOLS + 3 }, () => ({ kind: 'tool_call' as const, title: 't' }))
    expect(toolsFromAcp(many)).toHaveLength(MAX_AGENT_TOOLS)
  })
})

describe('sameTools', () => {
  it('compares title and status', () => {
    expect(sameTools(undefined, [])).toBe(true)
    expect(sameTools([{ title: 'a' }], [{ title: 'a', status: '' }])).toBe(true)
    expect(sameTools([{ title: 'a', status: 'running' }], [{ title: 'a', status: 'completed' }])).toBe(false)
    expect(sameTools([{ title: 'a' }], [{ title: 'b' }])).toBe(false)
    expect(sameTools([{ title: 'a' }], [])).toBe(false)
  })
})
