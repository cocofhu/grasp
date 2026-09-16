// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { InboxItem } from '@/lib/shared/types'

vi.mock('@/lib/api/api', () => ({
  api: {
    listGates: vi.fn(),
  },
  isPaginated: (data: unknown): data is { items: unknown[]; total: number } =>
    data != null && typeof data === 'object' && !Array.isArray(data) && 'items' in data,
}))

import { api } from '@/lib/api/api'
import { usePendingGates } from '@/lib/inbox/usePendingGates'

function clarify(id: string, state?: 'starting' | 'replying'): InboxItem {
  return {
    type: 'clarify',
    runId: `run-${id}`,
    nodeId: `node-${id}`,
    workflowName: 'Test',
    label: `Clarify ${id}`,
    done: false,
    requestedAt: '2026-07-04T00:00:00Z',
    updatedAt: '2026-07-04T00:00:00Z',
    ...(state ? { state } : {}),
  }
}

function gate(id: string): InboxItem {
  return {
    type: 'gate',
    runId: `run-${id}`,
    nodeId: `node-${id}`,
    workflowName: 'Test',
    title: `Gate ${id}`,
    bodyMd: '',
    actions: [{ id: 'approve', label: 'Approve' }],
    requestedAt: '2026-07-04T00:00:00Z',
  }
}

function paged(items: InboxItem[], total = items.length) {
  return { items, total, page: 1, pageSize: 20, hasMore: total > items.length }
}

describe('usePendingGates', () => {
  beforeEach(async () => {
    vi.mocked(api.listGates).mockReset()
    vi.mocked(api.listGates).mockResolvedValue(paged([]))
    await usePendingGates().refresh({ mode: 'force' })
  })

  it('shares items and count across composable instances', async () => {
    const a = usePendingGates()
    const b = usePendingGates()
    expect(a.items).toBe(b.items)
    expect(a.displayedItems).toBe(b.displayedItems)
    expect(a.count).toBe(b.count)

    vi.mocked(api.listGates).mockResolvedValue(paged([gate('1'), gate('2')], 2))
    await a.refresh({ mode: 'force' })
    expect(a.count.value).toBe(2)
    expect(b.count.value).toBe(2)
    expect(a.displayedItems.value).toHaveLength(2)
  })

  it('updates items and count from listGates on force refresh', async () => {
    vi.mocked(api.listGates).mockResolvedValue(paged([gate('1')], 1))
    const { items, count, refresh } = usePendingGates()
    await refresh({ mode: 'force' })
    expect(items.value).toHaveLength(1)
    expect(count.value).toBe(1)
  })

  it('keeps last known items when refresh fails', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    const { items, count, refresh, error } = usePendingGates()
    await refresh({ mode: 'force' })
    expect(count.value).toBe(1)

    vi.mocked(api.listGates).mockRejectedValueOnce(new Error('network'))
    await refresh({ mode: 'force' })
    expect(count.value).toBe(1)
    expect(items.value).toHaveLength(1)
    expect(error.value).toBe('network')
  })

  it('deduplicates concurrent refresh calls', async () => {
    let resolve!: (value: ReturnType<typeof paged>) => void
    const pending = new Promise<ReturnType<typeof paged>>((r) => {
      resolve = r
    })
    vi.mocked(api.listGates).mockReturnValue(pending)
    vi.mocked(api.listGates).mockClear()

    const { refresh } = usePendingGates()
    const first = refresh({ mode: 'force' })
    const second = refresh({ mode: 'force' })
    expect(api.listGates).toHaveBeenCalledTimes(1)

    resolve(paged([gate('1')], 1))
    await Promise.all([first, second])
    expect(usePendingGates().count.value).toBe(1)
  })

  it('records lastRefreshSource after a successful refresh', async () => {
    vi.mocked(api.listGates).mockResolvedValue(paged([gate('1')], 1))
    const { refresh, lastRefreshSource } = usePendingGates()
    expect(lastRefreshSource.value).toBeNull()
    await refresh({ source: 'sidebar-poll', mode: 'force' })
    expect(lastRefreshSource.value).toBe('sidebar-poll')
  })

  it('peek updates remoteItems and count without changing displayedItems', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    expect(pg.displayedItems.value).toHaveLength(1)
    expect(pg.count.value).toBe(1)

    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    await pg.peek({ source: 'sidebar-poll' })

    expect(pg.count.value).toBe(2)
    expect(pg.remoteItems.value).toHaveLength(2)
    expect(pg.displayedItems.value).toHaveLength(1)
    expect(pg.hasPendingUpdate.value).toBe(true)
    expect(pg.pendingMeta.value).toEqual({ added: 1, removed: 0 })
  })

  it('applyPending writes remote snapshot to displayedItems', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })

    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    await pg.peek()
    pg.applyPending()

    expect(pg.displayedItems.value).toHaveLength(2)
    expect(pg.hasPendingUpdate.value).toBe(false)
    expect(pg.pendingMeta.value).toBeNull()
  })

  it('peek with no changes clears pending state', async () => {
    vi.mocked(api.listGates).mockResolvedValue(paged([gate('1')], 1))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    await pg.peek()
    expect(pg.hasPendingUpdate.value).toBe(false)
  })

  it('peek diff tracks removed items', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })

    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    await pg.peek()

    expect(pg.hasPendingUpdate.value).toBe(true)
    expect(pg.pendingMeta.value).toEqual({ added: 0, removed: 1 })
  })

  it('submit source force-refreshes and applies immediately', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })

    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('2')], 1))
    await pg.refresh({ source: 'submit' })

    expect(pg.displayedItems.value).toHaveLength(1)
    expect(pg.hasPendingUpdate.value).toBe(false)
    expect(pg.lastRefreshSource.value).toBe('submit')
  })

  it('deduplicates concurrent peek calls', async () => {
    let resolve!: (value: ReturnType<typeof paged>) => void
    const pending = new Promise<ReturnType<typeof paged>>((r) => {
      resolve = r
    })
    vi.mocked(api.listGates).mockReturnValue(pending)
    vi.mocked(api.listGates).mockClear()

    const pg = usePendingGates()
    const first = pg.peek()
    const second = pg.peek()
    expect(api.listGates).toHaveBeenCalledTimes(1)

    resolve(paged([gate('1')], 1))
    await Promise.all([first, second])
  })

  it('force/submit during in-flight peek does not join peek or get stolen by setPending', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    expect(pg.displayedItems.value).toHaveLength(2)
    expect(pg.totalCount.value).toBe(2)

    let resolvePeek!: (value: ReturnType<typeof paged>) => void
    const peekPending = new Promise<ReturnType<typeof paged>>((r) => {
      resolvePeek = r
    })
    vi.mocked(api.listGates).mockClear()
    vi.mocked(api.listGates).mockReturnValueOnce(peekPending)

    const peekFlight = pg.peek({ source: 'sidebar-poll' })
    expect(api.listGates).toHaveBeenCalledTimes(1)

    // Submit force must start its own request — never await the peek promise.
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('2')], 1))
    const forceFlight = pg.refresh({ source: 'submit', mode: 'force' })
    expect(api.listGates).toHaveBeenCalledTimes(2)

    await forceFlight
    expect(pg.displayedItems.value.map((it) => it.nodeId)).toEqual(['node-2'])
    expect(pg.totalCount.value).toBe(1)
    expect(pg.hasPendingUpdate.value).toBe(false)
    expect(pg.lastRefreshSource.value).toBe('submit')

    // Stale peek resolves with the pre-approve snapshot — must not overwrite force.
    resolvePeek(paged([gate('1'), gate('2')], 2))
    await peekFlight
    expect(pg.displayedItems.value.map((it) => it.nodeId)).toEqual(['node-2'])
    expect(pg.totalCount.value).toBe(1)
    expect(pg.hasPendingUpdate.value).toBe(false)
  })

  it('removeItemLocally drops displayed/remote and decrements totalCount', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })

    pg.removeItemLocally('run-1:node-1')
    expect(pg.displayedItems.value).toHaveLength(1)
    expect(pg.remoteItems.value).toHaveLength(1)
    expect(pg.totalCount.value).toBe(1)
    expect(pg.displayedItems.value[0].nodeId).toBe('node-2')
  })

  it('restoreItemLocally reinserts after removeItemLocally', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1'), gate('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    const removed = pg.displayedItems.value.find((it) => it.nodeId === 'node-1')!
    pg.removeItemLocally('run-1:node-1')
    expect(pg.totalCount.value).toBe(1)

    pg.restoreItemLocally(removed)
    expect(pg.displayedItems.value.some((it) => it.nodeId === 'node-1')).toBe(true)
    expect(pg.remoteItems.value.some((it) => it.nodeId === 'node-1')).toBe(true)
    expect(pg.totalCount.value).toBe(2)
  })

  it('peek does not trigger refresh chrome or live announce', async () => {
    const { resetRefreshChrome, useRefreshChrome } = await import('@/lib/shared/refreshChrome')
    const { resetLoadingAnnouncer, useLoadingAnnouncer } = await import('@/lib/shared/loadingAnnouncer')
    resetRefreshChrome()
    resetLoadingAnnouncer()
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    const pg = usePendingGates()
    await pg.peek({ source: 'sidebar-poll' })
    const chrome = useRefreshChrome()
    expect(chrome.showTopBar.value).toBe(false)
    expect(chrome.dimContent.value).toBe(false)
    expect(useLoadingAnnouncer().liveMessage.value).toBe('')
    expect(pg.ariaBusy.value).toBe(false)
  })

  it('peek awaits in-flight force instead of starting a parallel peek', async () => {
    let resolveForce!: (value: ReturnType<typeof paged>) => void
    const forcePending = new Promise<ReturnType<typeof paged>>((r) => {
      resolveForce = r
    })
    vi.mocked(api.listGates).mockReturnValueOnce(forcePending)
    vi.mocked(api.listGates).mockClear()

    const pg = usePendingGates()
    const forceFlight = pg.refresh({ mode: 'force' })
    const peekFlight = pg.peek({ source: 'focus' })
    expect(api.listGates).toHaveBeenCalledTimes(1)

    resolveForce(paged([gate('1')], 1))
    await Promise.all([forceFlight, peekFlight])
    expect(pg.displayedItems.value).toHaveLength(1)
    expect(pg.totalCount.value).toBe(1)
  })

  it('peek that only flips state=replying updates cards and does not set hasPendingUpdate (plan g2.1)', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([clarify('1'), clarify('2')], 2))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    expect(pg.displayedItems.value.map((it) => ('state' in it ? it.state : undefined))).toEqual([
      undefined,
      undefined,
    ])

    vi.mocked(api.listGates).mockResolvedValueOnce(
      paged([clarify('1', 'replying'), clarify('2')], 2),
    )
    await pg.peek({ source: 'sidebar-poll' })

    expect(pg.hasPendingUpdate.value).toBe(false)
    expect(pg.pendingMeta.value).toBeNull()
    expect(pg.displayedItems.value).toHaveLength(2)
    expect(pg.displayedItems.value[0]).toMatchObject({ nodeId: 'node-1', state: 'replying' })
    expect(pg.displayedItems.value[1]).toMatchObject({ nodeId: 'node-2' })
    expect('state' in pg.displayedItems.value[1] ? pg.displayedItems.value[1].state : undefined).toBeUndefined()
  })

  it('patchItemReplying updates matching cards without changing membership', async () => {
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([clarify('1')], 1))
    const pg = usePendingGates()
    await pg.refresh({ mode: 'force' })
    pg.patchItemReplying('run-1:node-1', true)
    expect(pg.displayedItems.value[0]).toMatchObject({ state: 'replying' })
    expect(pg.hasPendingUpdate.value).toBe(false)
    pg.patchItemReplying('run-1:node-1', false)
    expect('state' in pg.displayedItems.value[0] ? pg.displayedItems.value[0].state : undefined).toBeUndefined()
  })

  it('peek passes live URL wf/projectId/tag to listGates (plan g2.2)', async () => {
    const prev = window.location.search
    window.history.replaceState({}, '', '/gates?wf=wf-1&projectId=proj-b&tag=urgent')
    vi.mocked(api.listGates).mockClear()
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([gate('1')], 1))
    const pg = usePendingGates()
    await pg.peek({ source: 'sidebar-poll' })
    expect(api.listGates).toHaveBeenCalledWith(
      expect.objectContaining({
        page: 1,
        pageSize: 20,
        wf: 'wf-1',
        projectId: 'proj-b',
        tag: 'urgent',
      }),
    )
    window.history.replaceState({}, '', prev || '/')
  })

  it('peek omits filter keys when the URL has none (plan g2.2 / g2.3)', async () => {
    const prev = window.location.search
    window.history.replaceState({}, '', '/gates')
    vi.mocked(api.listGates).mockClear()
    vi.mocked(api.listGates).mockResolvedValueOnce(paged([], 0))
    await usePendingGates().peek()
    expect(api.listGates).toHaveBeenCalledWith(
      expect.objectContaining({
        wf: undefined,
        projectId: undefined,
        tag: undefined,
      }),
    )
    window.history.replaceState({}, '', prev || '/')
  })
})
