import { describe, expect, it } from 'vitest'
import { createPendingAcpBuffer } from './pendingAcpBuffer'

describe('createPendingAcpBuffer', () => {
  it('keeps latest cumulative events per nodeId', () => {
    const buf = createPendingAcpBuffer()
    buf.push({
      nodeId: 'n1',
      events: [{ kind: 'thought', text: 'a', t: 1 }],
      busy: true,
    })
    buf.push({
      nodeId: 'n1',
      events: [
        { kind: 'thought', text: 'ab', t: 2 },
        { kind: 'message', text: 'hi', t: 3 },
      ],
      busy: true,
    })
    buf.push({ nodeId: 'n2', events: [{ kind: 'message', text: 'other', t: 1 }] })
    expect(buf.size).toBe(2)
    const all = buf.takeAll()
    expect(all).toHaveLength(2)
    expect(all.map((f) => f.nodeId).sort()).toEqual(['n1', 'n2'])
    expect(buf.size).toBe(0)
  })

  it('empty event frame preserves prior rails but can update busy', () => {
    const buf = createPendingAcpBuffer()
    buf.push({
      nodeId: 'n1',
      events: [{ kind: 'thought', text: 'keep', t: 1 }],
      busy: true,
    })
    buf.push({ nodeId: 'n1', events: [], busy: false })
    const [f] = buf.peekAll()
    expect(f.busy).toBe(false)
  })
})
