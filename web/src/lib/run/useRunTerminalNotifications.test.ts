// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { Run } from '@/lib/shared/types'
import type { NotificationListItem } from '@/lib/api/apiTypes'

const authUser = ref<{ username: string; expiresAt: string } | null>({
  username: 'alice',
  expiresAt: 't',
})
const authReady = ref(true)

const serverItems = ref<NotificationListItem[]>([])

vi.mock('@/lib/api/api', () => ({
  api: {
    listNotifications: vi.fn(async (opts?: {
      page?: number
      pageSize?: number
      filter?: 'all' | 'unread' | 'read'
      signal?: AbortSignal
    }) => {
      const all = serverItems.value.map((x) => ({ ...x }))
      const allCount = all.length
      const unreadCount = all.filter((x) => x.unread).length
      const readCount = allCount - unreadCount
      const filter = opts?.filter ?? 'all'
      let filtered = all
      if (filter === 'unread') filtered = all.filter((x) => x.unread)
      else if (filter === 'read') filtered = all.filter((x) => !x.unread)
      const page = opts?.page ?? 1
      const pageSize = opts?.pageSize ?? 20
      const start = (page - 1) * pageSize
      return {
        items: filtered.slice(start, start + pageSize),
        page,
        pageSize,
        total: filtered.length,
        allCount,
        unreadCount,
        readCount,
      }
    }),
    markNotificationRead: vi.fn(async (runId: string) => {
      serverItems.value = serverItems.value.map((x) =>
        x.runId === runId ? { ...x, unread: false } : x,
      )
      return { status: 'ok' }
    }),
    markAllNotificationsRead: vi.fn(async () => {
      serverItems.value = serverItems.value.map((x) => ({ ...x, unread: false }))
      return { status: 'ok' }
    }),
  },
}))

vi.mock('@/lib/composables/useAuth', () => ({
  useAuth: () => ({
    user: authUser,
    ready: authReady,
  }),
}))

import { api } from '@/lib/api/api'
import {
  __resetRunTerminalNotificationsForTests,
  finishedApproxIso,
  formatUnreadBadge,
  isNoisyNotificationTitle,
  mapRunToNotification,
  isBeforeBaseline,
  NOTIFICATION_PAGE_SIZE,
  RUN_TERMINAL_PANEL_LIMIT,
  useRunTerminalNotifications,
} from './useRunTerminalNotifications'
import type { RunTerminalNotificationItem } from './useRunTerminalNotifications'

function run(partial: Partial<Run> & Pick<Run, 'id' | 'status'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'demo-wf',
    title: partial.title ?? `Run ${partial.id}`,
    trigger: 'manual',
    startedAt: partial.startedAt ?? '2026-08-10T12:00:00Z',
    durationSec: partial.durationSec ?? 1,
    progress: 100,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

function item(
  r: Run,
  extra: Partial<RunTerminalNotificationItem> = {},
): RunTerminalNotificationItem {
  const n = mapRunToNotification(r)!
  return { ...n, unread: true, beforeBaseline: false, ...extra }
}

function seedList(items: RunTerminalNotificationItem[]) {
  serverItems.value = items.map((x) => ({ ...x }))
}

describe('formatUnreadBadge', () => {
  it('hides zero, shows 1–98, caps at 99+', () => {
    expect(formatUnreadBadge(0)).toBe('')
    expect(formatUnreadBadge(1)).toBe('1')
    expect(formatUnreadBadge(98)).toBe('98')
    expect(formatUnreadBadge(99)).toBe('99+')
    expect(formatUnreadBadge(120)).toBe('99+')
  })
})

describe('isNoisyNotificationTitle / mapRunToNotification', () => {
  it('maps completed/failed and rejects other statuses', () => {
    expect(mapRunToNotification(run({ id: 'r1', status: 'completed' }))).toMatchObject({
      runId: 'r1',
      status: 'completed',
    })
    expect(mapRunToNotification(run({ id: 'r2', status: 'failed' }))?.status).toBe('failed')
    expect(mapRunToNotification(run({ id: 'r3', status: 'running' }))).toBeNull()
    expect(mapRunToNotification(run({ id: 'r4', status: 'waiting_human' }))).toBeNull()
    expect(mapRunToNotification(run({ id: 'r5', status: 'cancelled' }))).toBeNull()
  })

  it('falls back title to workflowName then runId', () => {
    expect(
      mapRunToNotification(run({ id: 'r1', status: 'completed', title: '  ' }))?.title,
    ).toBe('demo-wf')
    expect(
      mapRunToNotification(
        run({ id: 'r2', status: 'completed', title: '', workflowName: '' }),
      )?.title,
    ).toBe('r2')
  })

  it('detects progress-noise titles and uses neutral template', () => {
    expect(isNoisyNotificationTitle('运行中 3 / 暂停 1')).toBe(true)
    expect(isNoisyNotificationTitle('产物这里根据Run 分页')).toBe(false)
    const n = mapRunToNotification(
      run({
        id: 'r1',
        status: 'completed',
        title: '运行中 2 · 等待人工 1',
        workflowName: '自我迭代',
        durationSec: 120,
      }),
    )
    expect(n?.titleNeutral).toBe(true)
    expect(n?.title).toBe('自我迭代 · completed')
    expect(n?.finishedApprox).toBe('2026-08-10T12:02:00.000Z')
  })

  it('computes finishedApprox from startedAt + durationSec', () => {
    expect(
      finishedApproxIso(
        run({ id: 'r1', status: 'completed', startedAt: '2026-08-10T12:00:00Z', durationSec: 90 }),
      ),
    ).toBe('2026-08-10T12:01:30.000Z')
  })
})

describe('useRunTerminalNotifications', () => {
  beforeEach(() => {
    localStorage.clear()
    authUser.value = { username: 'alice', expiresAt: 't' }
    authReady.value = true
    seedList([])
    __resetRunTerminalNotificationsForTests()
    vi.mocked(api.listNotifications).mockClear()
    vi.mocked(api.markNotificationRead).mockClear()
    vi.mocked(api.markAllNotificationsRead).mockClear()
  })

  it('loads pool via listNotifications and consumes server unread flags', async () => {
    seedList([
      item(run({ id: 'b', status: 'failed', startedAt: '2026-08-10T13:00:00Z', durationSec: 60 })),
      item(run({ id: 'a', status: 'completed', startedAt: '2026-08-10T12:00:00Z', durationSec: 3600 })),
    ])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(api.listNotifications).toHaveBeenCalled()
    expect(n.pool.value.map((x) => x.runId)).toEqual(['b', 'a'])
    expect(n.unreadCount.value).toBe(2)
    expect(n.badgeLabel.value).toBe('2')
    expect(n.hasUnreadFailed.value).toBe(true)
  })

  it('treats pre-enable history as read so badge is not inventory', async () => {
    seedList(
      Array.from({ length: 7 }, (_, i) =>
        item(run({ id: `r${i}`, status: 'completed', startedAt: '2026-08-01T12:00:00Z' }), {
          unread: false,
          beforeBaseline: true,
        }),
      ),
    )
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.poolTotal.value).toBe(7)
    expect(n.unreadCount.value).toBe(0)
    expect(n.badgeLabel.value).toBe('')
    expect(n.listItems.value.every((x) => x.unread === false)).toBe(true)
    expect(n.listItems.value.every((x) => x.beforeBaseline === true)).toBe(true)
    expect(
      isBeforeBaseline(
        { finishedApprox: '2026-08-01T12:00:00Z', startedAt: '2026-08-01T12:00:00Z' },
        new Date().toISOString(),
      ),
    ).toBe(true)
  })

  it('computes unread from GET item flags after baseline', async () => {
    seedList([
      item(run({ id: 'a', status: 'completed' }), { unread: false }),
      item(run({ id: 'b', status: 'completed' })),
    ])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.unreadCount.value).toBe(1)
    expect(n.badgeLabel.value).toBe('1')
    expect(n.hasUnreadFailed.value).toBe(false)
    expect(n.previewItems.value.find((x) => x.runId === 'a')?.unread).toBe(false)
    expect(n.previewItems.value.find((x) => x.runId === 'b')?.unread).toBe(true)
  })

  it('markRead updates only that run and posts to server', async () => {
    seedList([
      item(run({ id: 'a', status: 'failed' })),
      item(run({ id: 'b', status: 'completed' })),
    ])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.unreadCount.value).toBe(2)
    n.markRead('a')
    expect(n.unreadCount.value).toBe(1)
    expect(n.hasUnreadFailed.value).toBe(false)
    await vi.waitFor(() => {
      expect(api.markNotificationRead).toHaveBeenCalledWith('a')
      expect(serverItems.value.find((x) => x.runId === 'a')?.unread).toBe(false)
    })
    expect(n.previewItems.value.find((x) => x.runId === 'b')?.unread).toBe(true)
  })

  it('markAllRead clears badge and posts without an id list', async () => {
    seedList([
      item(run({ id: 'a', status: 'failed' })),
      item(run({ id: 'b', status: 'completed' })),
    ])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.unreadCount.value).toBe(2)
    n.markAllRead()
    expect(n.unreadCount.value).toBe(0)
    expect(n.badgeLabel.value).toBe('')
    await vi.waitFor(() => {
      expect(api.markAllNotificationsRead).toHaveBeenCalledWith()
      expect(serverItems.value.every((x) => x.unread === false)).toBe(true)
    })
  })

  it('previewItems caps at 5 and remainingCount is T-5', async () => {
    seedList(
      Array.from({ length: 12 }, (_, i) =>
        item(run({ id: `r${i}`, status: i % 2 === 0 ? 'completed' : 'failed' })),
      ),
    )
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'manual' })
    expect(n.previewItems.value).toHaveLength(RUN_TERMINAL_PANEL_LIMIT)
    expect(RUN_TERMINAL_PANEL_LIMIT).toBe(5)
    expect(n.remainingCount.value).toBe(7)
    expect(n.poolTotal.value).toBe(12)
    expect(NOTIFICATION_PAGE_SIZE).toBe(20)
  })

  it('refreshPage loads server-paginated list slice', async () => {
    seedList(
      Array.from({ length: 25 }, (_, i) =>
        item(run({ id: `r${i}`, status: 'completed' })),
      ),
    )
    const n = useRunTerminalNotifications()
    await n.refreshPage({ page: 2, filter: 'all', source: 'page' })
    expect(n.listItems.value).toHaveLength(5)
    expect(n.listTotal.value).toBe(25)
    expect(n.allCount.value).toBe(25)
  })

  it('marks post-baseline items without beforeBaseline and keeps them unread until read', async () => {
    seedList([
      item(run({ id: 'new', status: 'completed', startedAt: '2026-08-10T12:00:00Z' })),
      item(run({ id: 'old', status: 'completed', startedAt: '2019-06-01T12:00:00Z' }), {
        unread: false,
        beforeBaseline: true,
      }),
    ])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    await n.refreshPage({ page: 1, filter: 'all', source: 'page' })
    const newer = n.listItems.value.find((x) => x.runId === 'new')
    const older = n.listItems.value.find((x) => x.runId === 'old')
    expect(newer?.beforeBaseline).toBe(false)
    expect(newer?.unread).toBe(true)
    expect(older?.beforeBaseline).toBe(true)
    expect(older?.unread).toBe(false)
    expect(n.unreadCount.value).toBe(1)
    expect(n.listItems.value.map((x) => x.runId)).toEqual(expect.arrayContaining(['new', 'old']))
  })

  it('badge is empty when unread is 0', async () => {
    seedList([item(run({ id: 'a', status: 'completed' }))])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    n.markRead('a')
    expect(n.unreadCount.value).toBe(0)
    expect(n.badgeLabel.value).toBe('')
  })

  it('does not use localStorage as unread authority', async () => {
    seedList([item(run({ id: 'read-1', status: 'completed' }), { unread: false })])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.unreadCount.value).toBe(0)
    expect(n.previewItems.value.find((x) => x.runId === 'read-1')?.unread).toBe(false)
    localStorage.clear()
    expect(n.unreadCount.value).toBe(0)
  })

  it('does not hydrate before auth; rehydrates on user settle without focus', async () => {
    authReady.value = false
    authUser.value = null
    seedList([
      item(run({ id: 'a', status: 'completed' })),
      item(run({ id: 'b', status: 'failed' })),
      item(run({ id: 'c', status: 'completed' })),
    ])
    vi.mocked(api.listNotifications).mockClear()

    const n = useRunTerminalNotifications()
    n.startPolling()
    expect(n.ensureUsername()).toBe(false)
    expect(n.unreadCount.value).toBe(0)
    expect(n.badgeLabel.value).toBe('')
    expect(api.listNotifications).not.toHaveBeenCalled()

    authUser.value = { username: 'alice', expiresAt: 't' }
    authReady.value = true
    await vi.waitFor(() => {
      expect(n.unreadCount.value).toBe(3)
      expect(n.badgeLabel.value).toBe('3')
    })
    expect(localStorage.length).toBe(0)
    n.stopPolling()
  })

  it('markRead survives module reset + auth rehydrate (page refresh)', async () => {
    seedList([
      item(run({ id: 'a', status: 'failed' })),
      item(run({ id: 'b', status: 'completed' })),
    ])
    const n1 = useRunTerminalNotifications()
    await n1.refresh({ source: 'mount' })
    expect(n1.unreadCount.value).toBe(2)
    n1.markRead('a')
    await vi.waitFor(() =>
      expect(serverItems.value.find((x) => x.runId === 'a')?.unread).toBe(false),
    )
    expect(n1.unreadCount.value).toBe(1)

    __resetRunTerminalNotificationsForTests()
    authReady.value = false
    authUser.value = null

    const n2 = useRunTerminalNotifications()
    n2.startPolling()
    expect(n2.ensureUsername()).toBe(false)
    expect(n2.unreadCount.value).toBe(0)

    authUser.value = { username: 'alice', expiresAt: 't' }
    authReady.value = true
    await vi.waitFor(() => {
      expect(n2.unreadCount.value).toBe(1)
      expect(n2.badgeLabel.value).toBe('1')
    })
    expect(n2.previewItems.value.find((x) => x.runId === 'a')?.unread).toBe(false)
    expect(n2.previewItems.value.find((x) => x.runId === 'b')?.unread).toBe(true)
    n2.stopPolling()
  })

  it('markAllRead survives logout and same-account re-login', async () => {
    seedList([
      item(run({ id: 'a', status: 'failed' })),
      item(run({ id: 'b', status: 'completed' })),
    ])
    const n1 = useRunTerminalNotifications()
    await n1.refresh({ source: 'mount' })
    expect(n1.unreadCount.value).toBe(2)
    n1.markAllRead()
    await vi.waitFor(() => expect(serverItems.value.every((x) => x.unread === false)).toBe(true))
    expect(n1.unreadCount.value).toBe(0)

    authUser.value = null
    authReady.value = true
    __resetRunTerminalNotificationsForTests()

    const n2 = useRunTerminalNotifications()
    n2.startPolling()
    await vi.waitFor(() => {
      expect(n2.ensureUsername()).toBe(true)
    })
    authUser.value = { username: 'alice', expiresAt: 't' }
    await vi.waitFor(() => {
      expect(n2.unreadCount.value).toBe(0)
      expect(n2.badgeLabel.value).toBe('')
    })
    expect(serverItems.value.every((x) => x.unread === false)).toBe(true)
    n2.stopPolling()
  })

  it('markRead before auth settle does not call server or stamp anonymous prefs', async () => {
    authReady.value = false
    authUser.value = null

    const n = useRunTerminalNotifications()
    n.markRead('ghost')
    expect(api.markNotificationRead).not.toHaveBeenCalled()
    expect(localStorage.length).toBe(0)
  })

  it('markAllRead before auth settle does not call server or stamp anonymous prefs', async () => {
    authReady.value = false
    authUser.value = null

    const n = useRunTerminalNotifications()
    n.markAllRead()
    expect(api.markAllNotificationsRead).not.toHaveBeenCalled()
    expect(localStorage.length).toBe(0)
  })

  it('new terminal events after markAllRead still count as unread', async () => {
    seedList([item(run({ id: 'a', status: 'completed' }))])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(n.unreadCount.value).toBe(1)
    n.markAllRead()
    await vi.waitFor(() =>
      expect(serverItems.value.find((x) => x.runId === 'a')?.unread).toBe(false),
    )
    expect(n.unreadCount.value).toBe(0)

    seedList([
      item(run({ id: 'new', status: 'failed', startedAt: '2026-08-10T14:00:00Z' })),
      item(run({ id: 'a', status: 'completed' }), { unread: false }),
    ])
    await n.refresh({ source: 'manual' })
    expect(n.unreadCount.value).toBe(1)
    expect(n.previewItems.value.find((x) => x.runId === 'new')?.unread).toBe(true)
    expect(n.previewItems.value.find((x) => x.runId === 'a')?.unread).toBe(false)
  })

  it('empty pool refresh does not POST mark-all', async () => {
    seedList([])
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'mount' })
    expect(api.markNotificationRead).not.toHaveBeenCalled()
    expect(api.markAllNotificationsRead).not.toHaveBeenCalled()
    __resetRunTerminalNotificationsForTests()
    await n.refresh({ source: 'manual' })
    expect(api.markAllNotificationsRead).not.toHaveBeenCalled()
    expect(n.poolTotal.value).toBe(0)
  })
})
