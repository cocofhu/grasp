import { describe, expect, it } from 'vitest'
import { mergeAcpEvents, type MergedAcpEvent } from './mergeAcpEvents'
import type { AcpEvent } from '../shared/types'

function ev(partial: Partial<AcpEvent> & Pick<AcpEvent, 'kind'>): AcpEvent {
  return { t: 0, ...partial }
}

function asMerged(events: AcpEvent[], live = true): MergedAcpEvent[] {
  return mergeAcpEvents([], events, { live })
}

describe('mergeAcpEvents', () => {
  it('preserves in-flight thought when incoming snapshot omits it', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'tool_call', title: 'Shell', status: 'completed' }),
      ev({ t: 1, kind: 'commands', title: 'npm test' }),
      ev({ t: 2, kind: 'thought', text: '正在启动服务并运行 Agent Studio…' }),
    ])

    const incoming: AcpEvent[] = [
      ev({ t: 0, kind: 'tool_call', title: 'Shell', status: 'completed' }),
      ev({ t: 1, kind: 'commands', title: 'npm test' }),
    ]

    const merged = mergeAcpEvents(prev, incoming, { live: true })
    expect(merged).toHaveLength(3)
    expect(merged[2].kind).toBe('thought')
    expect(merged[2].stableKey).toBe('inflight:thought')
    expect(merged[2].text).toBe('正在启动服务并运行 Agent Studio…')
  })

  it('monotonically grows thought text on incremental updates', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'thought', text: '短思考' }),
    ])

    const incoming: AcpEvent[] = [
      ev({ t: 0, kind: 'thought', text: '短思考 — 更长的一段流式输出' }),
    ]

    const merged = mergeAcpEvents(prev, incoming, { live: true })
    expect(merged).toHaveLength(1)
    expect(merged[0].stableKey).toBe('inflight:thought')
    expect(merged[0].text).toBe('短思考 — 更长的一段流式输出')
  })

  it('keeps longer prev thought when incoming frame is shorter', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'thought', text: '较长的 prev 思考文本' }),
    ])

    const incoming: AcpEvent[] = [
      ev({ t: 0, kind: 'thought', text: '较短' }),
    ]

    const merged = mergeAcpEvents(prev, incoming, { live: true })
    expect(merged[0].text).toBe('较长的 prev 思考文本')
    expect(merged[0].stableKey).toBe('inflight:thought')
  })

  it('does not resurrect thought after incoming settles with new tail events', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'thought', text: '旧思考' }),
    ])

    const incoming: AcpEvent[] = [
      ev({ t: 0, kind: 'thought', text: '旧思考' }),
      ev({ t: 1, kind: 'tool_call', title: 'Shell', status: 'running' }),
    ]

    const merged = mergeAcpEvents(prev, incoming, { live: true })
    expect(merged).toHaveLength(2)
    expect(merged[1].kind).toBe('tool_call')
    expect(merged[0].stableKey).not.toBe('inflight:thought')
  })

  it('prefix slice is preserved when caller prepends history (hasMore)', () => {
    const older: MergedAcpEvent[] = [
      { t: 0, kind: 'message', text: '更早的历史', stableKey: 'message:0:abc' },
    ]
    const tailPrev = asMerged([
      ev({ t: 1, kind: 'thought', text: '流式思考' }),
    ])
    const incoming: AcpEvent[] = []
    const mergedTail = mergeAcpEvents(tailPrev, incoming, { live: true })
    const withPrefix = [...older, ...mergedTail]

    expect(withPrefix).toHaveLength(2)
    expect(withPrefix[0].text).toBe('更早的历史')
    expect(withPrefix[0].stableKey).toBe('message:0:abc')
    expect(withPrefix[1].kind).toBe('thought')
    expect(withPrefix[1].stableKey).toBe('inflight:thought')
  })

  it('live empty incoming keeps full prev timeline (tool_call/message/thought)', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'tool_call', title: 'Shell', status: 'completed' }),
      ev({ t: 1, kind: 'message', text: 'agent reply' }),
      ev({ t: 2, kind: 'thought', text: 'still thinking' }),
    ])

    const merged = mergeAcpEvents(prev, [], { live: true })

    expect(merged).toHaveLength(3)
    expect(merged.map((e) => e.kind)).toEqual(['tool_call', 'message', 'thought'])
    expect(merged[0].title).toBe('Shell')
    expect(merged[1].text).toBe('agent reply')
    expect(merged[2].stableKey).toBe('inflight:thought')
    // Shallow copy — not the same array reference
    expect(merged).not.toBe(prev)
  })

  it('live empty incoming keeps prev when tail is message (no thought)', () => {
    const prev = asMerged([
      ev({ t: 0, kind: 'tool_call', title: 'Shell', status: 'completed' }),
      ev({ t: 1, kind: 'message', text: 'agent reply' }),
    ])

    const merged = mergeAcpEvents(prev, [], { live: true })

    expect(merged).toHaveLength(2)
    expect(merged.map((e) => e.kind)).toEqual(['tool_call', 'message'])
    expect(merged[0].title).toBe('Shell')
    expect(merged[1].text).toBe('agent reply')
    expect(merged).not.toBe(prev)
  })

  it('live empty incoming with empty prev stays empty (cold start)', () => {
    expect(mergeAcpEvents([], [], { live: true })).toEqual([])
  })
})
