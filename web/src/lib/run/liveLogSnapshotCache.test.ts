import { afterEach, describe, expect, it } from 'vitest'
import {
  clearAllLiveLogSnapshots,
  clearLiveLogSnapshotsExceptRun,
  cloneEventPageSnapshot,
  getLiveLogSnapshot,
  listLiveLogSnapshotsForRun,
  putLiveLogEventPage,
  putLiveLogMcpCalls,
} from './liveLogSnapshotCache'
import type { MergedAcpEvent } from './mergeAcpEvents'

function evt(title: string, t = 0): MergedAcpEvent {
  return { kind: 'message', title, t, stableKey: `k:${t}` }
}

describe('liveLogSnapshotCache', () => {
  afterEach(() => {
    clearAllLiveLogSnapshots()
  })

  it('stores and restores event pages per runId+nodeId', () => {
    putLiveLogEventPage('run-a', 'node-1', {
      events: [evt('hello')],
      nextCursor: 'c1',
      hasMore: true,
      live: true,
    })
    const snap = getLiveLogSnapshot('run-a', 'node-1')
    expect(snap?.eventPage?.events[0].title).toBe('hello')
    expect(snap?.eventPage?.hasMore).toBe(true)

    const cloned = cloneEventPageSnapshot(snap!.eventPage!)
    cloned.events.push(evt('mutated', 1))
    expect(getLiveLogSnapshot('run-a', 'node-1')?.eventPage?.events).toHaveLength(1)
  })

  it('stores mcpCalls and lists snapshots for a run', () => {
    putLiveLogMcpCalls('run-a', 'node-1', [
      { at: 't', tool: 'read_artifact', args: '{}', result: 'ok', isError: false },
    ])
    putLiveLogEventPage('run-a', 'node-2', {
      events: [evt('n2')],
      nextCursor: '',
      hasMore: false,
      live: false,
    })
    putLiveLogEventPage('run-b', 'node-1', {
      events: [evt('other')],
      nextCursor: '',
      hasMore: false,
      live: false,
    })

    const listed = listLiveLogSnapshotsForRun('run-a')
    expect(listed.map((x) => x.nodeId).sort()).toEqual(['node-1', 'node-2'])
  })

  it('clearLiveLogSnapshotsExceptRun drops other runs only', () => {
    putLiveLogEventPage('run-a', 'n1', {
      events: [evt('a')],
      nextCursor: '',
      hasMore: false,
      live: false,
    })
    putLiveLogEventPage('run-b', 'n1', {
      events: [evt('b')],
      nextCursor: '',
      hasMore: false,
      live: false,
    })
    clearLiveLogSnapshotsExceptRun('run-a')
    expect(getLiveLogSnapshot('run-a', 'n1')?.eventPage?.events[0].title).toBe('a')
    expect(getLiveLogSnapshot('run-b', 'n1')).toBeUndefined()
  })

  it('refuses empty page overwrite of a non-empty cached timeline', () => {
    putLiveLogEventPage('run-a', 'node-1', {
      events: [evt('keep-me')],
      nextCursor: '',
      hasMore: false,
      live: true,
    })
    putLiveLogEventPage('run-a', 'node-1', {
      events: [],
      nextCursor: '',
      hasMore: false,
      live: true,
    })
    expect(getLiveLogSnapshot('run-a', 'node-1')?.eventPage?.events).toHaveLength(1)
    expect(getLiveLogSnapshot('run-a', 'node-1')?.eventPage?.events[0].title).toBe('keep-me')
  })

  it('hard-remount restore keeps non-empty pages via list+clone', () => {
    putLiveLogEventPage('run-a', 'node-1', {
      events: [evt('cached')],
      nextCursor: 'c',
      hasMore: false,
      live: true,
    })
    // Simulate resetRunState: clear reactive pages, then restore from cache.
    const restored: Record<string, ReturnType<typeof cloneEventPageSnapshot>> = {}
    for (const { nodeId, snapshot } of listLiveLogSnapshotsForRun('run-a')) {
      if (!snapshot.eventPage?.events.length) continue
      restored[nodeId] = cloneEventPageSnapshot(snapshot.eventPage)
    }
    expect(restored['node-1']?.events[0].title).toBe('cached')
    expect(restored['node-1']?.live).toBe(true)
  })
})
