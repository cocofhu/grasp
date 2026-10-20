import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { Run } from '../shared/types'

const listRuns = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    listRuns: (...args: unknown[]) => listRuns(...args),
  },
}))

import { useRunBoard } from './useRunBoard'

function stubRun(partial: Partial<Run> & Pick<Run, 'id' | 'status'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'Workflow',
    trigger: 'manual',
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: 0,
    progress: 10,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

describe('useRunBoard', () => {
  beforeEach(() => {
    listRuns.mockReset()
  })

  it('loads dashboard columns with pageSize 5', async () => {
    listRuns.mockImplementation(async (params: { status?: string }) => {
      if (params.status === 'running,waiting_human') {
        return {
          items: [
            stubRun({ id: 'run-old', status: 'running', startedAt: '2026-07-18T10:00:00Z' }),
            stubRun({ id: 'run-new', status: 'waiting_human', startedAt: '2026-07-18T14:00:00Z' }),
          ],
          total: 2,
          page: 1,
          pageSize: 5,
          hasMore: false,
        }
      }
      return { items: [stubRun({ id: 'run-done', status: 'completed' })], total: 1, page: 1, pageSize: 5, hasMore: false }
    })

    const board = useRunBoard({ mode: 'dashboard' })
    await board.load()

    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        status: 'running,waiting_human',
        page: 1,
        pageSize: 5,
      }),
    )
    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        status: 'completed',
        page: 1,
        pageSize: 5,
      }),
    )
    expect(board.column('active').items.map((r) => r.id)).toEqual(['run-new', 'run-old'])
    expect(board.column('completed').items).toHaveLength(1)
  })

  it('loads full board main columns and optional extras', async () => {
    listRuns.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 100, hasMore: false })
    const extras = ref(new Set<string>(['failed']))
    const board = useRunBoard({ mode: 'full', extraStatuses: extras })
    await board.load()

    const statuses = listRuns.mock.calls.map((c) => c[0].status).sort()
    expect(statuses).toEqual(['completed', 'failed', 'running', 'waiting_human'])
    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        status: 'running',
        page: 1,
        pageSize: 100,
      }),
    )
    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        status: 'completed',
        page: 1,
        pageSize: 20,
      }),
    )
  })

  it('marks truncated when total exceeds page', async () => {
    listRuns.mockResolvedValue({
      items: Array.from({ length: 100 }, (_, i) => stubRun({ id: `run-${i}`, status: 'running' })),
      total: 150,
      page: 1,
      pageSize: 100,
      hasMore: true,
    })
    const board = useRunBoard({ mode: 'full' })
    await board.load()
    expect(board.column('running').truncated).toBe(true)
  })

  it('surfaces error when listRuns fails instead of silent empty', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    listRuns.mockImplementation(async (params: { status?: string }) => {
      if (params.status === 'running') throw new Error('network down')
      return { items: [], total: 0, page: 1, pageSize: 100, hasMore: false }
    })
    const board = useRunBoard({ mode: 'full' })
    await board.load()
    expect(board.error.value).toContain('running')
    expect(board.column('running').error).toBe('network down')
    expect(board.column('completed').error).toBeNull()
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('times out, clears loading, and does not treat abort as failure', async () => {
    vi.useFakeTimers()
    listRuns.mockReturnValue(new Promise(() => {}))
    const board = useRunBoard({ mode: 'dashboard', timeoutMs: 80 })
    const pending = board.load()
    await vi.advanceTimersByTimeAsync(80)
    await pending
    expect(board.loading.value).toBe(false)
    expect(board.terminalState.value).toBe('error')
    expect(board.hasLoaded.value).toBe(true)

    listRuns.mockReturnValue(new Promise(() => {}))
    const hung = board.load()
    board.abort()
    await hung
    expect(board.loading.value).toBe(false)
    expect(board.terminalState.value).toBe('cancelled')
    vi.useRealTimers()
  })

  it('discards stale requestSeq results', async () => {
    let resolveFirst!: (value: unknown) => void
    let resolveSecond!: (value: unknown) => void
    listRuns
      .mockReturnValueOnce(
        new Promise((r) => {
          resolveFirst = r
        }),
      )
      .mockReturnValueOnce(
        new Promise((r) => {
          resolveSecond = r
        }),
      )
      .mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 5, hasMore: false })

    const board = useRunBoard({ mode: 'dashboard' })
    const first = board.load()
    const second = board.load()
    resolveFirst({
      items: [{ id: 'stale', status: 'running', workflowId: 'wf', workflowName: 'P', trigger: 'manual', startedAt: '2026-07-18T12:00:00Z', durationSec: 0, progress: 0, nodeRuns: {}, artifacts: [] }],
      total: 1,
      page: 1,
      pageSize: 5,
      hasMore: false,
    })
    resolveSecond({ items: [], total: 0, page: 1, pageSize: 5, hasMore: false })
    await Promise.all([first, second])
    expect(board.loading.value).toBe(false)
    expect(board.column('active').items).toEqual([])
  })
})
