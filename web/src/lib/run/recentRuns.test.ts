// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Run, RunCurrentNode } from '@/lib/shared/types'
import type { SoloSession } from '@/lib/api/apiTypes'
import {
  RECENT_RELAY_DEBOUNCE_MS,
  createRecentHold,
  historyEntries,
  notifyRecentActivity,
  onRecentActivity,
  resetRecentActivityRelayForTests,
  recentRunActive,
  recentRunMark,
  recentRunTarget,
  sameRecentRows,
} from './recentRuns'

const cur = (kind: RunCurrentNode['kind'], state: RunCurrentNode['state'], mode: RunCurrentNode['mode'] = 'auto'): RunCurrentNode =>
  ({ id: 'n1', name: 'N', iteration: 1, kind, mode, state })
const run = (currentNode?: RunCurrentNode, status: Run['status'] = 'running') => ({ id: 'r1', status, currentNode }) as unknown as Run

describe('recentRunMark uses the status primitives', () => {
  it.each([
    [cur('agent', 'running'), { icon: { name: 'breath-dot', toneClass: 'nav-row-type-icon--accent' } }],
    [cur('review', 'busy', 'manual'), { icon: { name: 'breath-dot', toneClass: 'nav-row-type-icon--accent' } }],
    [cur('agent', 'stopping', 'manual'), { icon: { name: 'arc-spinner', toneClass: 'nav-row-type-icon--accent' } }],
    [cur('clarify', 'running', 'clarify'), { icon: { name: 'arc-spinner', toneClass: 'nav-row-type-icon--accent' } }],
    [cur('gate', 'waiting_human', 'gate'), { icon: { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' } }],
    [cur('review', 'waiting_human', 'review'), { icon: { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' } }],
    [cur('takeover', 'waiting_human', 'manual'), { icon: { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' } }],
    [cur('clarify', 'waiting_human', 'clarify'), { icon: { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' } }],
  ])('%o', (node, mark) => {
    expect(recentRunMark(run(node, node.state === 'waiting_human' ? 'waiting_human' : 'running'))).toEqual(mark)
  })
  it('shows nothing for finished runs or runs without a current node', () => {
    expect(recentRunMark(run(undefined))).toBeNull()
    expect(recentRunMark(run(cur('agent', 'running'), 'completed'))).toBeNull()
  })
})

describe('recentRunTarget goes straight into the chat', () => {
  it('auto node → live stream; waiting/taken-over → inbox chat; else detail', () => {
    expect(recentRunTarget(run(cur('agent', 'running')))).toEqual({ path: '/runs/r1/live', query: { node: 'n1' } })
    expect(recentRunTarget(run(cur('takeover', 'waiting_human', 'manual'), 'waiting_human'))).toEqual({ path: '/runs/r1/nodes/n1' })
    expect(recentRunTarget(run(cur('agent', 'running', 'manual')))).toEqual({ path: '/runs/r1/nodes/n1' })
    expect(recentRunTarget(run(undefined, 'completed'))).toBe('/runs/r1')
    // Just taken over, auto call still unwinding: keep the stream (the chat opens once it waits).
    expect(recentRunTarget(run(cur('takeover', 'stopping', 'manual')))).toEqual({ path: '/runs/r1/live', query: { node: 'n1' } })
    // Chosen but not started: the live conversation, on any width. Not run detail.
    expect(recentRunTarget(run(cur('agent', 'entering'), 'waiting_human'))).toEqual({ path: '/runs/r1/live', query: { node: 'n1' } })
    expect(recentRunTarget(run(cur('agent', 'entering'), 'running'))).toEqual({ path: '/runs/r1/live', query: { node: 'n1' } })
    expect(recentRunMark(run(cur('agent', 'entering'), 'waiting_human'))).toEqual({
      icon: { name: 'breath-dot', toneClass: 'nav-row-type-icon--accent' },
    })
    // A person waiting still opens that node's chat.
    expect(recentRunTarget(run(cur('clarify', 'waiting_human', 'manual'), 'waiting_human'))).toEqual({ path: '/runs/r1/nodes/n1' })
    // Confirmed automatic, snapshot has no watchable node: still the conversation.
    expect(recentRunTarget(run(undefined, 'waiting_human'), undefined, { awaitAuto: true })).toBe('/runs/r1/live')
    expect(recentRunTarget(run(undefined, 'running'), undefined, { awaitAuto: true })).toBe('/runs/r1/live')
    // The same gap outside「自动过程」still opens run detail.
    expect(recentRunTarget(run(undefined, 'waiting_human'))).toBe('/runs/r1')
    // Finished runs leave, even if a stale entering node or an auto-row hint is attached.
    expect(recentRunTarget(run(cur('agent', 'entering'), 'completed'))).toBe('/runs/r1')
    expect(recentRunTarget(run(cur('agent', 'entering'), 'failed'))).toBe('/runs/r1')
    expect(recentRunTarget(run(cur('agent', 'entering'), 'cancelled'))).toBe('/runs/r1')
    expect(recentRunTarget(run(undefined, 'failed'), undefined, { awaitAuto: true })).toBe('/runs/r1')
    expect(recentRunTarget(run(undefined, 'cancelled'), undefined, { awaitAuto: true })).toBe('/runs/r1')
  })
  it('marks the row active on its detail, live and chat pages', () => {
    const r = run(undefined)
    expect(recentRunActive(r, { path: '/runs/r1', query: {} })).toBe(true)
    expect(recentRunActive(r, { path: '/runs/r1/live', query: {} })).toBe(true)
    expect(recentRunActive(r, { path: '/runs/r1/nodes/n1', query: {} })).toBe(true)
    expect(recentRunActive(r, { path: '/runs/r2/nodes/n1', query: {} })).toBe(false)
  })
})

describe('sameRecentRows', () => {
  it('ignores unrendered fields and detects order / mark changes', () => {
    const a = [run(cur('agent', 'running')), { ...run(undefined), id: 'r2' } as Run]
    const title = (r: Run) => r.id
    expect(sameRecentRows(a, a.map((r) => ({ ...r, lastActivityAt: 'x' }) as Run), title)).toBe(true)
    expect(sameRecentRows(a, [a[1], a[0]], title)).toBe(false)
    expect(sameRecentRows(a, [run(cur('agent', 'stopping', 'manual')), a[1]], title)).toBe(false)
  })
})

describe('createRecentHold', () => {
  afterEach(() => document.body.replaceChildren())
  function setup() {
    const list = document.createElement('div')
    const link = document.createElement('a')
    link.href = '#'
    link.textContent = 'row'
    list.append(link)
    const input = document.createElement('textarea')
    document.body.append(list, input)
    const hold = createRecentHold(document)
    const release = vi.fn()
    hold.attach(list, release)
    return { list, link, input, hold, release }
  }
  it('holds while the pointer is over the list and releases on leave', () => {
    const { list, hold, release } = setup()
    expect(hold.isHeld()).toBe(false)
    list.dispatchEvent(new Event('pointerenter'))
    expect(hold.isHeld()).toBe(true)
    list.dispatchEvent(new Event('pointerleave'))
    expect(hold.isHeld()).toBe(false)
    expect(release).toHaveBeenCalledTimes(1)
  })
  it('holds while a row has keyboard focus, not while the composer does', async () => {
    const { link, input, hold, release } = setup()
    link.focus()
    expect(hold.isHeld()).toBe(true)
    input.focus()
    link.dispatchEvent(new FocusEvent('focusout', { bubbles: true }))
    await Promise.resolve()
    expect(hold.isHeld()).toBe(false)
    expect(release).toHaveBeenCalled()
  })
  it('detach removes listeners', () => {
    const { list, hold, release } = setup()
    hold.detach()
    list.dispatchEvent(new Event('pointerenter'))
    expect(hold.isHeld()).toBe(false)
    expect(release).not.toHaveBeenCalled()
  })
})

describe('local activity relay', () => {
  beforeEach(() => { resetRecentActivityRelayForTests() })
  afterEach(() => {
    resetRecentActivityRelayForTests()
    vi.useRealTimers()
  })

  it('debounces bursts into one refresh', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const off = onRecentActivity(fn)
    notifyRecentActivity(); notifyRecentActivity(); notifyRecentActivity()
    vi.advanceTimersByTime(RECENT_RELAY_DEBOUNCE_MS)
    expect(fn).toHaveBeenCalledTimes(1)
    off()
    notifyRecentActivity()
    vi.advanceTimersByTime(RECENT_RELAY_DEBOUNCE_MS)
    expect(fn).toHaveBeenCalledTimes(1)
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('reads the latest state when a second notice arrives inside the window', () => {
    vi.useFakeTimers()
    let state = 'busy'
    const seen: string[] = []
    const off = onRecentActivity(() => { seen.push(state) })
    notifyRecentActivity()
    vi.advanceTimersByTime(100)
    state = 'idle'
    notifyRecentActivity()
    vi.advanceTimersByTime(RECENT_RELAY_DEBOUNCE_MS)
    expect(seen.at(-1)).toBe('idle')
    vi.advanceTimersByTime(RECENT_RELAY_DEBOUNCE_MS)
    expect(seen).toEqual(['idle', 'idle'])
    off()
    vi.clearAllTimers()
    vi.useRealTimers()
  })
})

describe('historyEntries merges finished runs with ended Solo sessions', () => {
  const hr = (id: string, at: Partial<Pick<Run, 'lastActivityAt' | 'startedAt' | 'createdAt'>> = {}) =>
    ({ id, status: 'completed', ...at }) as unknown as Run
  const hs = (id: string, at: Partial<Pick<SoloSession, 'closedAt' | 'lastActiveAt' | 'updatedAt'>> = {}) =>
    ({ id, title: id, status: 'closed', ...at }) as unknown as SoloSession
  const keys = (list: ReturnType<typeof historyEntries>) => list.map((e) => e.key)

  it('interleaves both kinds newest first', () => {
    const runs = [hr('r3', { lastActivityAt: '2026-11-03T10:00:00Z' }), hr('r1', { lastActivityAt: '2026-11-03T08:00:00Z' })]
    const solos = [hs('s0', { closedAt: '2026-11-03T07:00:00Z' }), hs('s2', { closedAt: '2026-11-03T09:00:00Z' })]
    const out = historyEntries(runs, solos, 20)
    expect(keys(out)).toEqual(['r3', 'solo:s2', 'r1', 'solo:s0'])
    expect(out[1]).toMatchObject({ kind: 'solo', session: solos[1] })
    expect(out[0]).toMatchObject({ kind: 'run', run: runs[0] })
  })

  it('falls back lastActivityAt → startedAt → createdAt and closedAt → lastActiveAt → updatedAt', () => {
    const runs = [hr('rs', { startedAt: '2026-11-03T06:00:00Z' }), hr('rc', { createdAt: '2026-11-03T02:00:00Z' })]
    const solos = [
      hs('sl', { lastActiveAt: '2026-11-03T05:00:00Z', updatedAt: '2026-11-03T12:00:00Z' }),
      hs('su', { updatedAt: '2026-11-03T03:00:00Z' }),
      hs('sc', { closedAt: '2026-11-03T07:00:00Z', lastActiveAt: '2026-11-03T01:00:00Z' }),
    ]
    expect(keys(historyEntries(runs, solos, 20))).toEqual(['solo:sc', 'rs', 'solo:sl', 'solo:su', 'rc'])
  })

  it('sorts a missing or unparseable time last and keeps equal times stable', () => {
    const solos = [hs('b', { closedAt: 'not a date' }), hs('a'), hs('c', { closedAt: '2026-11-03T01:00:00Z' })]
    expect(keys(historyEntries([], solos, 20))).toEqual(['solo:c', 'solo:a', 'solo:b'])
    const same = '2026-11-03T01:00:00Z'
    const tie = historyEntries([hr('r', { lastActivityAt: same })], [hs('s', { closedAt: same })], 20)
    expect(keys(tie)).toEqual(['r', 'solo:s'])
  })

  it('keeps the server run order and caps the merged list', () => {
    const runs = Array.from({ length: 20 }, (_, i) => hr(`r${i}`))
    const solos = Array.from({ length: 5 }, (_, i) => hs(`s${i}`, { closedAt: `2026-11-03T0${i}:00:00Z` }))
    const out = historyEntries(runs, solos, 20)
    expect(out).toHaveLength(20)
    expect(keys(out).slice(0, 5)).toEqual(['solo:s4', 'solo:s3', 'solo:s2', 'solo:s1', 'solo:s0'])
    expect(keys(out).slice(5)).toEqual(runs.slice(0, 15).map((r) => r.id))
    expect(historyEntries(runs, solos, 0)).toEqual([])
  })
})
