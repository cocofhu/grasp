import { describe, expect, it } from 'vitest'
import { MAX_AGENT_TOOLS, partsFromAcp, sameParts, sameTools, timelineBlocks, toolsFromAcp } from './acpTools'

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

describe('partsFromAcp', () => {
  it('takes the last timeline of the open row', () => {
    const parts = partsFromAcp([
      { kind: 'timeline', parts: [{ kind: 'message', text: 'old' }] },
      { kind: 'message' },
      { kind: 'timeline', parts: [{ kind: 'thought', text: 't' }, { kind: 'tool', title: 'Read' }] },
    ])
    expect(parts).toEqual([{ kind: 'thought', text: 't' }, { kind: 'tool', title: 'Read' }])
  })

  it('ignores timelines sealed before a segment and empty ones', () => {
    expect(partsFromAcp([{ kind: 'timeline', parts: [{ kind: 'message', text: 'a' }] }, { kind: 'segment' }, { kind: 'message' }])).toBeUndefined()
    expect(partsFromAcp([{ kind: 'timeline', parts: [] }])).toBeUndefined()
    expect(partsFromAcp([{ kind: 'timeline' }])).toBeUndefined()
    expect(partsFromAcp(undefined)).toBeUndefined()
  })

  it('copies parts so the row never aliases the snapshot', () => {
    const src = [{ kind: 'message' as const, text: 'a' }]
    const out = partsFromAcp([{ kind: 'timeline', parts: src }])!
    out[0]!.text = 'b'
    expect(src[0]!.text).toBe('a')
  })
})

describe('sameParts', () => {
  it('compares every rendered field', () => {
    expect(sameParts(undefined, [])).toBe(true)
    expect(sameParts([{ kind: 'tool', title: 'a', status: 'running' }], [{ kind: 'tool', title: 'a', status: 'running' }])).toBe(true)
    expect(sameParts([{ kind: 'tool', title: 'a', status: 'running' }], [{ kind: 'tool', title: 'a', status: 'completed' }])).toBe(false)
    expect(sameParts([{ kind: 'tool', title: 'a' }], [{ kind: 'tool', title: 'a', output: 'x' }])).toBe(false)
    expect(sameParts([{ kind: 'message', text: 'a' }], [])).toBe(false)
  })
})

describe('timelineBlocks', () => {
  it('folds consecutive tools and keeps text runs in order', () => {
    const blocks = timelineBlocks([
      { kind: 'thought', text: 'look' },
      { kind: 'tool', title: 'Shell', status: 'completed', summary: 'ls', input: 'i', output: 'o' },
      { kind: 'tool' },
      { kind: 'message', text: 'found' },
      { kind: 'message', text: '   ' },
      { kind: 'tool', title: 'Read' },
    ])
    expect(blocks.map((b) => b.kind)).toEqual(['thought', 'tools', 'message', 'tools'])
    expect(blocks[1]).toMatchObject({
      tools: [{ title: 'Shell', status: 'completed', summary: 'ls', input: 'i', output: 'o' }, { title: 'tool' }],
      last: false,
    })
    expect(blocks[2]).toMatchObject({ text: 'found', index: 3 })
    expect(blocks.map((b) => b.last)).toEqual([false, false, false, true])
    expect(new Set(blocks.map((b) => b.key)).size).toBe(4)
    expect(timelineBlocks(undefined)).toEqual([])
  })
})
