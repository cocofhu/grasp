// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Run, RunCurrentNode } from '@/lib/shared/types'
import {
  RECENT_RELAY_DEBOUNCE_MS,
  createRecentHold,
  notifyRecentActivity,
  onRecentActivity,
  recentRunActive,
  recentRunMark,
  recentRunTarget,
  sameRecentRows,
} from './recentRuns'

const cur = (kind: RunCurrentNode['kind'], state: RunCurrentNode['state'], mode: RunCurrentNode['mode'] = 'auto'): RunCurrentNode =>
  ({ id: 'n1', name: 'N', iteration: 1, kind, mode, state })
const run = (currentNode?: RunCurrentNode, status: Run['status'] = 'running') => ({ id: 'r1', status, currentNode }) as unknown as Run

describe('recentRunMark reuses the 待审批 row icons', () => {
  it.each([
    [cur('agent', 'running'), { motion: 'pulse' }],
    [cur('review', 'busy', 'manual'), { motion: 'pulse' }],
    [cur('agent', 'stopping', 'manual'), { motion: 'spin' }],
    [cur('clarify', 'running', 'clarify'), { motion: 'spin' }],
    [cur('gate', 'waiting_human', 'gate'), { icon: { name: 'pending-approval', toneClass: 'nav-row-type-icon--approval' } }],
    [cur('review', 'waiting_human', 'review'), { icon: { name: 'pending-review', toneClass: 'nav-row-type-icon--review' } }],
    [cur('takeover', 'waiting_human', 'manual'), { icon: { name: 'pending-review', toneClass: 'nav-row-type-icon--review' } }],
    [cur('clarify', 'waiting_human', 'clarify'), { icon: { name: 'pending-chat', toneClass: 'nav-row-type-icon--chat' } }],
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
    expect(recentRunTarget(run(cur('takeover', 'waiting_human', 'manual'), 'waiting_human'))).toEqual({ path: '/gates', query: { run: 'r1', node: 'n1' } })
    expect(recentRunTarget(run(cur('agent', 'running', 'manual')))).toEqual({ path: '/gates', query: { run: 'r1', node: 'n1' } })
    expect(recentRunTarget(run(undefined, 'completed'))).toBe('/runs/r1')
  })
  it('marks the row active on its detail, live and chat pages', () => {
    const r = run(undefined)
    expect(recentRunActive(r, { path: '/runs/r1', query: {} })).toBe(true)
    expect(recentRunActive(r, { path: '/runs/r1/live', query: {} })).toBe(true)
    expect(recentRunActive(r, { path: '/gates', query: { run: 'r1' } })).toBe(true)
    expect(recentRunActive(r, { path: '/gates', query: { run: 'r2' } })).toBe(false)
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
    vi.useRealTimers()
  })
})
