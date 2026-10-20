import { describe, expect, it } from 'vitest'
import type { AcpEvent, ClarifyTurn } from '@/lib/shared/types'
import { autoPhaseRows, expandAutoPhase } from './takeoverTimeline'

const opening = '已中断自动执行，本节点转为人工接管（不会再自动执行）。'
const at = '2026-10-21T00:00:01Z'
const ev = (e: Partial<AcpEvent>): AcpEvent => ({ t: 0, kind: 'message', ...e }) as AcpEvent

describe('takeover opening turn: turns[0].events (auto phase)', () => {
  it('expands the auto phase into rail rows in front of the narration', () => {
    const turns: ClarifyTurn[] = [
      { role: 'agent', text: opening, at, events: [
        ev({ kind: 'thought', text: '先看登录页结构' }),
        ev({ kind: 'tool_call', title: 'Read LoginView.vue', status: 'completed' }),
        ev({ kind: 'tool_call', title: 'Edit LoginView.vue', status: 'running' }),
        ev({ kind: 'message', text: '正在调整表单布局' }),
      ] },
      { role: 'human', text: '继续', at },
    ]
    const out = expandAutoPhase(turns)
    expect(out).toHaveLength(3)
    expect(out[0]).toEqual({ role: 'agent', text: '正在调整表单布局', thought: '先看登录页结构', at, tools: [{ title: 'Read LoginView.vue', status: 'completed' }, { title: 'Edit LoginView.vue', status: 'running' }] })
    expect(out[1]).toEqual({ role: 'agent', text: opening, at })
    expect(out[1]).not.toHaveProperty('events')
    expect(out[2]).toBe(turns[1])
  })

  it('one row per sealed segment; earlier rows are handoff; timeline parts are kept', () => {
    const rows = autoPhaseRows({ role: 'agent', text: opening, at, events: [
      ev({ kind: 'message', text: '第一段' }),
      ev({ kind: 'segment' }),
      ev({ kind: 'timeline', parts: [{ kind: 'thought', text: '想' }, { kind: 'tool', title: 'Bash', status: 'completed' }, { kind: 'message', text: '第二段' }] }),
      ev({ kind: 'message', text: '第二段' }),
    ] })
    expect(rows.map((r) => [r.text, !!r.handoff])).toEqual([['第一段', true], ['第二段', false]])
    expect(rows[1]!.parts).toEqual([{ kind: 'thought', text: '想' }, { kind: 'tool', title: 'Bash', status: 'completed' }, { kind: 'message', text: '第二段' }])
  })

  it('a tools-only last row is handoff (never an empty failed reply)', () => {
    const rows = autoPhaseRows({ role: 'agent', text: opening, at, events: [ev({ kind: 'tool_call', title: 'Bash', status: 'failed' })] })
    expect(rows).toEqual([{ role: 'agent', text: '', at, tools: [{ title: 'Bash', status: 'failed' }], handoff: true }])
  })

  it('leaves turns alone when the field is absent, empty, or not on the opening agent turn', () => {
    const plain: ClarifyTurn[] = [{ role: 'agent', text: opening, at }]
    expect(expandAutoPhase(plain)).toBe(plain)
    const empty: ClarifyTurn[] = [{ role: 'agent', text: opening, at, events: [] }]
    expect(expandAutoPhase(empty)).toBe(empty)
    const later: ClarifyTurn[] = [{ role: 'human', text: 'x', at }, { role: 'agent', text: 'y', at, events: [ev({ text: 'z' })] }]
    expect(expandAutoPhase(later)).toBe(later)
    // Only plan / commands / liveness: nothing to draw, narration stays.
    const noise: ClarifyTurn[] = [{ role: 'agent', text: opening, at, events: [ev({ kind: 'plan', text: 'p' }), ev({ kind: 'commands' })] }]
    expect(expandAutoPhase(noise)).toEqual([{ role: 'agent', text: opening, at }])
  })

  it('drops the server narration prefix that repeats the auto phase\'s last message', () => {
    const events = [ev({ kind: 'message', text: '已确认改动范围' }), ev({ kind: 'tool_call', title: 'Edit', status: 'completed' })]
    const out = expandAutoPhase([{ role: 'agent', text: `已确认改动范围\n\n---\n${opening}`, at, events }])
    expect(out.map((t) => t.text)).toEqual(['已确认改动范围', opening])
    // timeline parts count too
    const withParts = expandAutoPhase([{ role: 'agent', text: `第二段\n\n---\n${opening}`, at, events: [ev({ kind: 'timeline', parts: [{ kind: 'message', text: '第二段' }, { kind: 'tool', title: 'Bash' }] })] }])
    expect(withParts[1]!.text).toBe(opening)
    // a different prefix (not shown above) is kept
    const other = expandAutoPhase([{ role: 'agent', text: `别的说明\n\n---\n${opening}`, at, events }])
    expect(other[1]!.text).toBe(`别的说明\n\n---\n${opening}`)
  })
})
