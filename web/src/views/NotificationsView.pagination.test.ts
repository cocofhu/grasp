// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import type { Run } from '@/lib/shared/types'
import { requestNotificationsPageReset, __resetNotificationsPageEntryForTests } from '@/lib/composables/useNotificationsPageEntry'

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: ref(false) }),
}))

vi.mock('@/lib/composables/useAuth', () => ({
  useAuth: () => ({
    user: ref({ username: 'tester', expiresAt: 't' }),
    ready: ref(true),
  }),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    listNotifications: vi.fn(async (opts?: {
      page?: number
      pageSize?: number
      filter?: 'all' | 'unread' | 'read'
      signal?: AbortSignal
    }) => {
      const all = listStore.value.map((x) => ({ ...x }))
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
    getRun: vi.fn(),
    artifactContent: vi.fn(),
    artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
    markNotificationRead: vi.fn(async (runId: string) => {
      listStore.value = listStore.value.map((x) =>
        x.runId === runId ? { ...x, unread: false } : x,
      )
      return { status: 'ok' }
    }),
    markAllNotificationsRead: vi.fn(async () => {
      listStore.value = listStore.value.map((x) => ({ ...x, unread: false }))
      return { status: 'ok' }
    }),
  },
}))

import { api } from '@/lib/api/api'
import {
  __resetRunTerminalNotificationsForTests,
  mapRunToNotification,
  NOTIFICATION_PAGE_SIZE,
  RUN_TERMINAL_PANEL_LIMIT,
  useRunTerminalNotifications,
} from '@/lib/run/useRunTerminalNotifications'
import type { RunTerminalNotificationItem } from '@/lib/run/useRunTerminalNotifications'

const listStore = ref<RunTerminalNotificationItem[]>([])
import NotificationsView from './NotificationsView.vue'

const viewSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'NotificationsView.vue'), 'utf8')
const poolSrc = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '../lib/run/useRunTerminalNotifications.ts'),
  'utf8',
)

function run(partial: Partial<Run> & Pick<Run, 'id' | 'status'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'demo-wf',
    title: partial.title ?? `Run ${partial.id}`,
    trigger: 'manual',
    startedAt: partial.startedAt ?? '2026-08-10T12:00:00Z',
    durationSec: 1,
    progress: 100,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

function makeItems(n: number, unreadCount = n): RunTerminalNotificationItem[] {
  return Array.from({ length: n }, (_, i) => {
    const r = run({
      id: `n-${String(i).padStart(2, '0')}`,
      status: i % 7 === 0 ? 'failed' : 'completed',
      startedAt: `2026-08-10T${String(10 + Math.floor(i / 60)).padStart(2, '0')}:${String(i % 60).padStart(2, '0')}:00Z`,
      title: `通知 ${i}`,
    })
    return {
      ...mapRunToNotification(r)!,
      unread: i < unreadCount,
      beforeBaseline: false,
    }
  })
}

/** ui-fade uses mode=out-in (~200ms leave then enter); settle before list DOM asserts. */
async function settleListFade() {
  await flushPromises()
  await nextTick()
  await new Promise((r) => setTimeout(r, 450))
  await flushPromises()
  await nextTick()
}

async function mountView(items: RunTerminalNotificationItem[]) {
  listStore.value = items.map((x) => ({ ...x }))
  vi.mocked(api.listNotifications).mockImplementation(async (opts) => {
    const all = listStore.value.map((x) => ({ ...x }))
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
  })
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...shell } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/notifications', component: NotificationsView },
      { path: '/runs/:id', component: { template: '<div data-testid="run-detail" />' } },
    ],
  })
  await router.push('/notifications')
  await router.isReady()
  const wrapper = mount(NotificationsView, {
    global: {
      plugins: [i18n, router],
      stubs: { Icon: true, Transition: false },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('NotificationsView pagination (g1/g2/g4)', () => {
  beforeEach(() => {
    localStorage.clear()
    listStore.value = []
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
    vi.mocked(api.listNotifications).mockReset()
    vi.mocked(api.listNotifications).mockResolvedValue({
      items: [],
      total: 0,
      allCount: 0,
      unreadCount: 0,
      readCount: 0,
    })
  })

  afterEach(() => {
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
  })

  it('N=20 has no pager and no duplicated page range in the header (g1.2 / g2.1 / g4.1)', async () => {
    const { wrapper } = await mountView(makeItems(20))
    expect(wrapper.findAll('[data-testid="notifications-item"]')).toHaveLength(20)
    expect(wrapper.find('[data-testid="notifications-pagination"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('N=21 shows two pages and slices 20+1 (g1.1 / g1.2 / g4.1)', async () => {
    const { wrapper } = await mountView(makeItems(21))
    expect(wrapper.findAll('[data-testid="notifications-item"]')).toHaveLength(20)
    const pager = wrapper.find('[data-testid="notifications-pagination"]')
    expect(pager.exists()).toBe(true)
    expect(wrapper.find('[data-testid="notifications-pager-summary"]').text()).toBe('共 21 条 · 每页 20')
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)

    const next = pager.findAll('button.pg-btn')[1]!
    await next.trigger('click')
    await settleListFade()
    expect(wrapper.findAll('[data-testid="notifications-item"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switching filter resets to page 1 and may hide pager (g1.3 / g4.1)', async () => {
    const items = makeItems(21).map((item, i) => ({ ...item, unread: i >= 18 }))
    const { wrapper } = await mountView(items)
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await settleListFade()

    await wrapper.find('[data-testid="notifications-filter-unread"]').trigger('click')
    await settleListFade()
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="notifications-pagination"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="notifications-item"]').length).toBeLessThanOrEqual(20)
    wrapper.unmount()
  })

  it('clamps to last data page when current unread page becomes empty (g1.4 / g4.1)', async () => {
    const items = makeItems(21)
    const { wrapper } = await mountView(items)
    await wrapper.find('[data-testid="notifications-filter-unread"]').trigger('click')
    await settleListFade()
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await settleListFade()
    expect(wrapper.findAll('[data-testid="notifications-item"]')).toHaveLength(1)
    const lastId = wrapper.find('[data-testid="notifications-item"]').attributes('data-run-id')!
    useRunTerminalNotifications().markRead(lastId)
    await settleListFade()
    expect(wrapper.find('[data-testid="notifications-pagination"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="notifications-item"]')).toHaveLength(20)
    wrapper.unmount()
  })

  it('keeps page on non-filter data change when still valid (g1.4)', async () => {
    const items = makeItems(25)
    const { wrapper } = await mountView(items)
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await settleListFade()
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    const firstOnPage = wrapper.find('[data-testid="notifications-item"]').attributes('data-run-id')!
    useRunTerminalNotifications().markRead(firstOnPage)
    await nextTick()
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="notifications-item"]').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('paging does not change unread badge or preview slice / fetchPool (g1.5 / g4.1)', async () => {
    const items = makeItems(25)
    const { wrapper } = await mountView(items)
    await flushPromises()
    const callsAfterMount = vi.mocked(api.listNotifications).mock.calls.length
    const n = useRunTerminalNotifications()
    await n.refresh({ source: 'manual' })
    expect(n.unreadCount.value).toBe(25)
    expect(n.previewItems.value).toHaveLength(RUN_TERMINAL_PANEL_LIMIT)
    const previewIds = n.previewItems.value.map((x) => x.runId)

    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await flushPromises()
    await nextTick()
    expect(n.unreadCount.value).toBe(25)
    expect(wrapper.find('[data-testid="notifications-unread-count"]').text()).toContain('25')
    expect(n.previewItems.value.map((x) => x.runId)).toEqual(previewIds)
    expect(vi.mocked(api.listNotifications).mock.calls.length).toBeGreaterThan(callsAfterMount)
    expect(api.listNotifications).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not write page into the URL (g3.3 / g4.1)', async () => {
    const { wrapper, router } = await mountView(makeItems(21))
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await nextTick()
    expect(router.currentRoute.value.query.page).toBeUndefined()
    expect(router.currentRoute.value.fullPath).not.toContain('page=')
    wrapper.unmount()
  })

  it('empty list hides range and pager (g1.4 / g2.1)', async () => {
    const { wrapper } = await mountView([])
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="notifications-pagination"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('topbar entry signal resets filter and page without query (g3.1)', async () => {
    const { wrapper, router } = await mountView(makeItems(21))
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await wrapper.find('[data-testid="notifications-filter-unread"]').trigger('click')
    await nextTick()
    requestNotificationsPageReset()
    await nextTick()
    expect(wrapper.find('[data-testid="notifications-page-range"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="notifications-filter-all"]').classes().join(' ')).toMatch(/border-accent/)
    expect(router.currentRoute.value.fullPath).not.toContain('page=')
    wrapper.unmount()
  })

  it('hides the annotated subtitle and keeps filters, mark-all, and the empty state', async () => {
    expect(viewSrc).not.toContain('pages.notifications.subtitle')
    const { wrapper } = await mountView([])
    expect(wrapper.get('.notifications-header').text()).toBe('通知')
    expect(wrapper.get('[data-testid="notifications-filter-all"]').text()).toContain('全部')
    expect(wrapper.get('[data-testid="notifications-filter-unread"]').text()).toContain('未读')
    expect(wrapper.get('[data-testid="notifications-filter-read"]').text()).toContain('已读')
    expect(wrapper.get('[data-testid="notifications-mark-all"]').text()).toContain('全部已读')
    expect(wrapper.text()).toContain('暂无通知')
    expect(wrapper.text()).toContain('执行完成或失败后才会出现在这里')
    expect(wrapper.text()).not.toContain('查看工作流完成与失败的消息，及时了解执行结果。')
    expect(wrapper.text()).not.toContain('Review completed and failed workflows and their results.')
    wrapper.unmount()
  })

  it('mark-all-read still clears the whole pool unread count (g1.5)', async () => {
    const { wrapper } = await mountView(makeItems(25))
    await wrapper.find('[data-testid="notifications-pagination"]').findAll('button.pg-btn')[1]!.trigger('click')
    await wrapper.find('[data-testid="notifications-mark-all"]').trigger('click')
    await nextTick()
    expect(useRunTerminalNotifications().unreadCount.value).toBe(0)
    wrapper.unmount()
  })
})

describe('NotificationsView pagination conventions (g4.4 / g1.5)', () => {
  it('keeps min-h-11 on tabs and mark-all', () => {
    expect(viewSrc).toMatch(/min-h-11 border border-line bg-transparent/)
    expect(viewSrc).toMatch(/min-h-11 border-b-2 border-transparent px-4/)
  })

  it('gates Pagination on filteredTotal > PAGE_SIZE with shrink-0 and no pageSizeOptions', () => {
    expect(viewSrc).toMatch(/const PAGE_SIZE = NOTIFICATION_PAGE_SIZE/)
    expect(viewSrc).toMatch(/<Pagination[\s\S]*v-if="filteredTotal > PAGE_SIZE"/)
    expect(viewSrc).toMatch(/class="shrink-0"/)
    expect(viewSrc).not.toMatch(/page-size-options/)
    expect(viewSrc).not.toMatch(/pageSizeOptions/)
  })

  it('loads paginated slices from GET /notifications with server counts', () => {
    expect(poolSrc).toMatch(/listNotifications/)
    expect(poolSrc).toMatch(/export const NOTIFICATION_PAGE_SIZE = 20/)
    expect(NOTIFICATION_PAGE_SIZE).toBe(20)
    expect(poolSrc).not.toMatch(/RUN_TERMINAL_POOL_SIZE/)
  })

  it('wraps the list in ui-fade with listFadeKey (g3)', () => {
    expect(viewSrc).toMatch(/name="ui-fade"/)
    expect(viewSrc).toMatch(/listFadeKey/)
    expect(viewSrc).toMatch(/data-testid="notifications-list-fade"/)
  })
})
