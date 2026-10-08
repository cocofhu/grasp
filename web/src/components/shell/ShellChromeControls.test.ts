// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import type { Run } from '@/lib/shared/types'

const push = vi.fn()

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/', meta: {}, query: {} }),
  useRouter: () => ({ push }),
}))

vi.mock('@/lib/composables/useShutdownState', () => ({
  isDraining: () => false,
}))

vi.mock('@/lib/composables/useAuth', () => ({
  useAuth: () => ({
    user: ref({ username: 'tester', expiresAt: 't' }),
    ready: ref(true),
  }),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    listRuns: vi.fn(),
    listNotifications: vi.fn(async () => ({
      items: [],
      page: 1,
      pageSize: 20,
      total: 0,
      allCount: 0,
      unreadCount: 0,
      readCount: 0,
    })),
    getRun: vi.fn(),
    artifactContent: vi.fn(),
    artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
    platformStatus: vi.fn().mockResolvedValue({
      cumulativeTokens: null,
      todayTokens: null,
      runningCount: 0,
      queuedCount: 0,
      asOf: '2026-08-12T00:00:00Z',
      timezone: 'UTC',
    }),
    markNotificationRead: vi.fn(async () => ({ status: 'ok' })),
    markAllNotificationsRead: vi.fn(async () => ({ status: 'ok' })),
  },
}))

import { api } from '@/lib/api/api'
import { setTheme } from '@/lib/shared/theme'
import { __resetNotificationsPageEntryForTests } from '@/lib/composables/useNotificationsPageEntry'
import {
  __resetRunTerminalNotificationsForTests,
  mapRunToNotification,
  RUN_TERMINAL_PANEL_LIMIT,
} from '@/lib/run/useRunTerminalNotifications'
import type { RunTerminalNotificationItem } from '@/lib/run/useRunTerminalNotifications'
import ShellChromeControls from './ShellChromeControls.vue'

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

function asItem(r: Run, extra: Partial<RunTerminalNotificationItem> = {}): RunTerminalNotificationItem {
  return { ...mapRunToNotification(r)!, unread: true, beforeBaseline: false, ...extra }
}

function seedList(items: RunTerminalNotificationItem[]) {
  const allCount = items.length
  const unreadCount = items.filter((x) => x.unread).length
  const readCount = allCount - unreadCount
  vi.mocked(api.listNotifications).mockImplementation(async (opts?: { page?: number; pageSize?: number }) => {
    const page = opts?.page && opts.page > 0 ? opts.page : 1
    const pageSize = opts?.pageSize && opts.pageSize > 0 ? opts.pageSize : 20
    const start = (page - 1) * pageSize
    return {
      items: items.slice(start, start + pageSize),
      page,
      pageSize,
      total: allCount,
      allCount,
      unreadCount,
      readCount,
    }
  })
}

function mountChrome(layout: 'bar' | 'sidebar' = 'sidebar') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...shell } },
  })
  return mount(ShellChromeControls, {
    props: { layout },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        LangSelect: { template: '<div data-testid="lang" />' },
        StatusMetrics: { template: '<div data-testid="status-metrics" />' },
        Transition: false,
        Teleport: true,
      },
    },
    attachTo: document.body,
  })
}

describe('ShellChromeControls notifications (g1.2)', () => {
  beforeEach(() => {
    localStorage.clear()
    setTheme('dark')
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
    push.mockReset()
    vi.mocked(api.listNotifications).mockReset()
    vi.mocked(api.getRun).mockReset()
    vi.mocked(api.artifactContent).mockReset()
    vi.mocked(api.markNotificationRead).mockReset()
    vi.mocked(api.markAllNotificationsRead).mockReset()
    vi.mocked(api.listNotifications).mockResolvedValue({
      items: [],
      page: 1,
      pageSize: 20,
      total: 0,
      allCount: 0,
      unreadCount: 0,
      readCount: 0,
    })
    vi.mocked(api.markNotificationRead).mockResolvedValue({ status: 'ok' })
    vi.mocked(api.markAllNotificationsRead).mockResolvedValue({ status: 'ok' })
    vi.mocked(api.getRun).mockResolvedValue(run({ id: 'r1', status: 'completed', artifacts: [] }))
    vi.mocked(api.artifactContent).mockImplementation(async (id: string) => ({
      id,
      name: 'summary.md',
      kind: 'markdown',
      nodeId: 'n1',
      runId: 'ok-2',
      workflowName: 'demo-wf',
      sizeBytes: 10,
      createdAt: '2026-08-10T12:00:00Z',
      content: '# hello',
    }))
  })

  afterEach(() => {
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
  })

  it('renders chrome with theme toggle and bell aria titled 通知', async () => {
    const wrapper = mountChrome()
    await flushPromises()
    expect(wrapper.find('[data-testid="shell-chrome-controls"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-chrome-controls"]').attributes('data-layout')).toBe('sidebar')
    expect(wrapper.find('[data-testid="lang"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-toggle"]').classes()).toContain('h-8')
    const bell = wrapper.find('[data-testid="run-notifications-bell"]')
    expect(bell.exists()).toBe(true)
    expect(bell.classes()).toContain('h-8')
    expect(bell.attributes('aria-label')).toBe('通知')
    expect(bell.attributes('aria-haspopup')).toBe('true')
    expect(bell.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[data-testid="run-notifications-badge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows panel empty state without a clickable runs escape', async () => {
    const wrapper = mountChrome()
    await flushPromises()
    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="run-notifications-panel"]').exists()).toBe(true)
    const empty = wrapper.find('[data-testid="run-notifications-empty"]')
    expect(empty.text()).toContain('暂无通知')
    expect(empty.text()).toContain('执行完成或失败后才会出现')
    expect(empty.text()).toContain('运行')
    expect(empty.find('a').exists()).toBe(false)
    expect(empty.find('button').exists()).toBe(false)
    wrapper.unmount()
  })

  it('caps dropdown at 5 items; view-all goes to /notifications; mark-all clears badge', async () => {
    seedList(
      Array.from({ length: 12 }, (_, i) =>
        asItem(
          run({
            id: `r${i}`,
            status: i === 0 ? 'failed' : 'completed',
            startedAt: `2026-08-10T${String(12 + (i % 10)).padStart(2, '0')}:${String(i).padStart(2, '0')}:00Z`,
          }),
        ),
      ),
    )
    const wrapper = mountChrome()
    await flushPromises()

    const badge = wrapper.find('[data-testid="run-notifications-badge"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('12')
    expect(badge.classes().join(' ')).toMatch(/bg-err/)
    // g2.3: sidebar unread badge is a small circular pill
    // force-radius-full keeps unread badge circular
    expect(badge.classes()).toContain('force-radius-full')
    expect(badge.classes()).toContain('rounded-full')
    expect(badge.classes()).toContain('h-3.5')

    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    expect(wrapper.findAll('[data-testid="run-notifications-item"]')).toHaveLength(
      RUN_TERMINAL_PANEL_LIMIT,
    )
    expect(RUN_TERMINAL_PANEL_LIMIT).toBe(5)
    expect(wrapper.find('[data-testid="run-notifications-more"]').text()).toContain('还有 7 条')
    expect(wrapper.find('[data-testid="run-notifications-view-all"]').text()).toBe('查看全部通知')

    await wrapper.find('[data-testid="run-notifications-mark-all"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="run-notifications-badge"]').exists()).toBe(false)

    await wrapper.find('[data-testid="run-notifications-view-all"]').trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/notifications' })
    expect(push).not.toHaveBeenCalledWith(
      expect.objectContaining({ path: '/runs' }),
    )
    wrapper.unmount()
  })

  it('shows before-baseline label on history items without counting them unread', async () => {
    seedList([
      asItem(run({ id: 'hist', status: 'completed', startedAt: '2026-08-01T12:00:00Z' }), {
        unread: false,
        beforeBaseline: true,
      }),
    ])
    const wrapper = mountChrome()
    await flushPromises()
    expect(wrapper.find('[data-testid="run-notifications-badge"]').exists()).toBe(false)
    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    const item = wrapper.find('[data-testid="run-notifications-item"]')
    expect(item.attributes('data-before-baseline')).toBe('true')
    expect(item.attributes('data-unread')).toBe('false')
    expect(item.text()).toContain('基线前·不计未读')
    wrapper.unmount()
  })

  it('clicking a preview item enters /notifications page 1 without locating', async () => {
    seedList([asItem(run({ id: 'fail-1', status: 'failed', title: 'boom' }))])
    const wrapper = mountChrome()
    await flushPromises()
    expect(wrapper.find('[data-testid="run-notifications-badge"]').text()).toBe('1')
    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    await wrapper.find('[data-testid="run-notifications-item"]').trigger('click')
    await flushPromises()
    expect(push).toHaveBeenCalledWith({ path: '/notifications' })
    expect(push).not.toHaveBeenCalledWith('/runs/fail-1')
    expect(wrapper.find('[data-testid="run-notifications-badge"]').text()).toBe('1')
    wrapper.unmount()
  })

  it('clicking completed preview also goes to /notifications', async () => {
    seedList([asItem(run({ id: 'ok-1', status: 'completed', title: 'done' }))])
    const wrapper = mountChrome()
    await flushPromises()
    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    await wrapper.find('[data-testid="run-notifications-item"]').trigger('click')
    await flushPromises()
    await nextTick()
    expect(push).toHaveBeenCalledWith({ path: '/notifications' })
    expect(api.getRun).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cleans noisy progress titles in the panel', async () => {
    seedList([
      asItem(
        run({
          id: 'noisy',
          status: 'completed',
          title: '运行中 3 / 等待 1',
          workflowName: '自我迭代',
        }),
      ),
    ])
    const wrapper = mountChrome()
    await flushPromises()
    await wrapper.find('[data-testid="run-notifications-bell"]').trigger('click')
    await nextTick()
    const item = wrapper.find('[data-testid="run-notifications-item"]')
    expect(item.text()).toContain('自我迭代 · 已完成')
    expect(item.text()).not.toMatch(/运行中/)
    wrapper.unmount()
  })
})

describe('ShellChromeControls theme icon (g2.1)', () => {
  beforeEach(() => {
    localStorage.clear()
    setTheme('dark')
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
  })

  afterEach(() => {
    delete document.startViewTransition
  })

  it('cross-fades sun and moon on click and updates immediately', async () => {
    const wrapper = mountChrome('sidebar')
    await flushPromises()
    const moon = wrapper.find('[data-testid="shell-theme-icon-moon"]')
    const sun = wrapper.find('[data-testid="shell-theme-icon-sun"]')
    expect(sun.classes()).toContain('is-active')
    expect(moon.classes()).not.toContain('is-active')

    await wrapper.find('[data-testid="shell-theme-toggle"]').trigger('click')
    await nextTick()
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(moon.classes()).toContain('is-active')
    expect(sun.classes()).not.toContain('is-active')

    await wrapper.find('[data-testid="shell-theme-toggle"]').trigger('click')
    await nextTick()
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(sun.classes()).toContain('is-active')
    wrapper.unmount()
  })

  it('applies the same stacked icons in bar layout', async () => {
    const wrapper = mountChrome('bar')
    await flushPromises()
    expect(wrapper.find('[data-testid="shell-theme-icon-moon"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-icon-sun"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-toggle"]').classes()).toContain('h-9')
    wrapper.unmount()
  })

  it('sidebar and bar share 280ms rotate and scale icon motion', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ShellChromeControls.vue'), 'utf8')
    const sidebar = mountChrome('sidebar')
    const bar = mountChrome('bar')
    await flushPromises()
    expect(sidebar.find('[data-testid="shell-theme-toggle"]').attributes('data-motion')).toBeUndefined()
    expect(bar.find('[data-testid="shell-theme-toggle"]').attributes('data-motion')).toBeUndefined()
    expect(src).not.toMatch(/data-motion="overlay-pop"/)
    expect(src).not.toMatch(/translateY\(-4px\)/)
    expect(src).not.toMatch(/scale\(0\.98\)/)
    expect(src).toMatch(/280ms ease/)
    expect(src).toMatch(/rotate\(-90deg\) scale\(0\.55\)/)
    expect(src).toMatch(/rotate\(90deg\) scale\(0\.55\)/)
    expect(src).toMatch(/rotate\(0deg\) scale\(1\)/)
    expect(src).toMatch(/view-transition-class:\s*theme-icon-moon/)
    expect(src).toMatch(/view-transition-class:\s*theme-icon-sun/)
    expect(src).toMatch(/prefers-reduced-motion:\s*reduce/)
    expect(src).toMatch(/prefers-reduced-motion:\s*reduce[\s\S]*\.shell-theme-icon\s*\{[^}]*transition:\s*none/)
    sidebar.unmount()
    bar.unmount()
  })

  it('keeps the previous icon until the view-transition callback (plan g1.2 review v1)', async () => {
    let update: (() => Promise<unknown>) | null = null
    document.startViewTransition = ((cb: () => unknown) => {
      update = () => Promise.resolve(cb())
      const pending = new Promise<void>(() => {})
      return {
        ready: pending,
        finished: pending,
        updateCallbackDone: pending,
        skipTransition() {},
        types: new Set<string>(),
      }
    }) as typeof document.startViewTransition

    const wrapper = mountChrome('sidebar')
    await flushPromises()
    expect(wrapper.find('[data-testid="shell-theme-icon-sun"]').classes()).toContain('is-active')

    await wrapper.find('[data-testid="shell-theme-toggle"]').trigger('click')
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(document.documentElement.classList.contains('theme-vt-capture')).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-icon-sun"]').classes()).toContain('is-active')
    expect(wrapper.find('[data-testid="shell-theme-icon-moon"]').classes()).not.toContain('is-active')

    await update!()
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(wrapper.find('[data-testid="shell-theme-icon-moon"]').classes()).toContain('is-active')
    expect(wrapper.find('[data-testid="shell-theme-icon-sun"]').classes()).not.toContain('is-active')
    expect(localStorage.getItem('grasp-theme')).toBe('light')

    delete document.startViewTransition
    wrapper.unmount()
  })

  it('rapid clicks settle on the last theme and the matching icon (plan g2.2)', async () => {
    const wrapper = mountChrome('sidebar')
    await flushPromises()
    const button = wrapper.find('[data-testid="shell-theme-toggle"]')
    await button.trigger('click')
    await button.trigger('click')
    await button.trigger('click')
    await nextTick()
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(wrapper.find('[data-testid="shell-theme-icon-moon"]').classes()).toContain('is-active')
    expect(wrapper.find('[data-testid="shell-theme-icon-sun"]').classes()).not.toContain('is-active')
    wrapper.unmount()
  })
})

describe('ShellChromeControls unread badge pop', () => {
  function mockUnread(
    unreadCount: number,
    items: RunTerminalNotificationItem[],
  ) {
    vi.mocked(api.listNotifications).mockResolvedValue({
      items,
      page: 1,
      pageSize: 20,
      total: items.length,
      allCount: Math.max(items.length, unreadCount),
      unreadCount,
      readCount: 0,
    })
  }

  async function refreshFromFocus() {
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    await nextTick()
  }

  beforeEach(() => {
    localStorage.clear()
    setTheme('dark')
    __resetRunTerminalNotificationsForTests()
    __resetNotificationsPageEntryForTests()
    vi.mocked(api.listNotifications).mockReset()
    vi.mocked(api.listNotifications).mockResolvedValue({
      items: [],
      page: 1,
      pageSize: 20,
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

  it('pops on appear and on a changed label, not when 99+ or color stays', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ShellChromeControls.vue'), 'utf8')
    expect(src).toMatch(/\.shell-unread-badge\s*\{[^}]*animation:\s*shell-badge-pop 280ms ease/)
    expect(src).toMatch(/@keyframes shell-badge-pop[\s\S]*opacity:\s*0[\s\S]*scale\(0\.55\)[\s\S]*opacity:\s*1[\s\S]*scale\(1\)/)
    expect(src).not.toMatch(/shell-badge-pop-leave/)
    expect(src).toMatch(/prefers-reduced-motion:\s*reduce[\s\S]*\.shell-unread-badge\s*\{[^}]*animation:\s*none/)

    const sidebar = mountChrome('sidebar')
    await flushPromises()
    expect(sidebar.find('[data-testid="run-notifications-badge"]').exists()).toBe(false)

    mockUnread(2, [asItem(run({ id: 'a', status: 'failed' })), asItem(run({ id: 'b', status: 'completed' }))])
    await refreshFromFocus()
    const badge2 = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(badge2.text()).toBe('2')
    expect(badge2.classes()).toContain('shell-unread-badge')
    expect(badge2.classes().join(' ')).toMatch(/bg-err/)
    const node2 = badge2.element

    mockUnread(3, [
      asItem(run({ id: 'a', status: 'failed' })),
      asItem(run({ id: 'b', status: 'completed' })),
      asItem(run({ id: 'c', status: 'completed' })),
    ])
    await refreshFromFocus()
    const badge3 = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(badge3.text()).toBe('3')
    expect(badge3.element).not.toBe(node2)

    mockUnread(2, [asItem(run({ id: 'a', status: 'completed' })), asItem(run({ id: 'b', status: 'completed' }))])
    await refreshFromFocus()
    const badgeBack = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(badgeBack.text()).toBe('2')
    expect(badgeBack.element).not.toBe(badge3.element)
    expect(badgeBack.classes().join(' ')).toMatch(/bg-accent/)
    const accentNode = badgeBack.element

    mockUnread(2, [asItem(run({ id: 'a', status: 'failed' })), asItem(run({ id: 'b', status: 'completed' }))])
    await refreshFromFocus()
    const recolored = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(recolored.text()).toBe('2')
    expect(recolored.element).toBe(accentNode)
    expect(recolored.classes().join(' ')).toMatch(/bg-err/)

    mockUnread(99, [asItem(run({ id: 'a', status: 'completed' }))])
    await refreshFromFocus()
    const capped = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(capped.text()).toBe('99+')
    const cappedNode = capped.element

    mockUnread(100, [asItem(run({ id: 'a', status: 'completed' }))])
    await refreshFromFocus()
    const stillCapped = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(stillCapped.text()).toBe('99+')
    expect(stillCapped.element).toBe(cappedNode)

    mockUnread(0, [])
    await refreshFromFocus()
    expect(sidebar.find('[data-testid="run-notifications-badge"]').exists()).toBe(false)
    expect(sidebar.find('[data-testid="run-notifications-bell"]').classes().join(' ')).not.toMatch(/shell-badge-pop|rotate/)

    const bar = mountChrome('bar')
    await flushPromises()
    mockUnread(1, [asItem(run({ id: 'bar-1', status: 'completed' }))])
    await refreshFromFocus()
    const barBadge = bar.find('[data-testid="run-notifications-badge"]')
    expect(barBadge.text()).toBe('1')
    expect(barBadge.classes()).toContain('shell-unread-badge')
    expect(barBadge.classes()).toContain('h-4')
    const sidebarAgain = sidebar.find('[data-testid="run-notifications-badge"]')
    expect(sidebarAgain.text()).toBe('1')
    expect(sidebarAgain.classes()).toContain('shell-unread-badge')
    expect(sidebarAgain.classes()).toContain('h-3.5')

    sidebar.unmount()
    bar.unmount()
  })
})
