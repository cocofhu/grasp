// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nav from '@/locales/zh-CN/nav.json'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'

const routeState = reactive({ path: '/dashboard', query: {} as Record<string, unknown>, meta: {} as Record<string, unknown> })

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: routerMocks.push }),
  RouterLink: {
    props: ['to'],
    template:
      '<a :href="typeof to === \'string\' ? to : (to.path || \'\')" :data-to="typeof to === \'string\' ? to : (to.path || \'\')" @click="$emit(\'click\')"><slot /></a>',
  },
}))

const routerMocks = vi.hoisted(() => ({
  push: vi.fn(),
}))

const gateMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  const mocks = {
    peek: vi.fn(),
    refresh: vi.fn(),
    count: { value: 2 },
    displayedItems: ref<Array<Record<string, unknown>>>([]),
    sidebarExtras: ref<Array<Record<string, unknown>>>([]),
    startFailedKeys: ref<string[]>([]),
    activeInboxKey: ref<string | null>(null),
    itemKey: (it: { runId: string; nodeId: string }) => `${it.runId}:${it.nodeId}`,
    patchRunTitle: (runId: string, title: string) => {
      const apply = (rows: Array<Record<string, unknown>>) =>
        rows.map((row) => (row.runId === runId ? { ...row, runTitle: title } : row))
      mocks.displayedItems.value = apply(mocks.displayedItems.value)
      mocks.sidebarExtras.value = apply(mocks.sidebarExtras.value)
    },
  }
  return mocks
})

const notifMocks = vi.hoisted(() => ({
  unreadCount: { value: 0 },
}))

const favMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    displayItems: ref<
      Array<{
        workflowId: string
        favoritedAt: number
        name: string
        status: 'draft' | 'published'
      }>
    >([]),
    hydrateDisplay: vi.fn(async () => undefined),
    unfavorite: vi.fn(),
    getFavoriteWorkflow: vi.fn(),
    reorderFavorites: vi.fn(),
  }
})

const breakpointMocks = vi.hoisted(() => ({
  isMobile: { __v_isRef: true, value: false },
}))

const launchMocks = vi.hoisted(() => ({
  openLaunch: vi.fn(),
}))

vi.mock('@/lib/inbox/usePendingGates', () => ({
  usePendingGates: () => ({
    count: gateMocks.count,
    peek: gateMocks.peek,
    refresh: gateMocks.refresh,
    displayedItems: gateMocks.displayedItems,
    sidebarExtras: gateMocks.sidebarExtras,
    startFailedKeys: gateMocks.startFailedKeys,
    activeInboxKey: gateMocks.activeInboxKey,
    itemKey: gateMocks.itemKey,
    patchRunTitle: gateMocks.patchRunTitle,
  }),
}))

const apiMocks = vi.hoisted(() => ({
  updateRunTitle: vi.fn(),
  renameSoloSession: vi.fn(),
  closeSoloSession: vi.fn(),
  listRuns: vi.fn(),
  listSoloSessions: vi.fn(),
  cancelRun: vi.fn(),
}))

const workspaceMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    ensureLoaded: vi.fn(async () => ({ name: 'ws' })),
    workspace: ref<{ name: string } | null>({ name: 'ws' }),
  }
})

vi.mock('@/lib/api/api', () => ({
  api: {
    updateRunTitle: (...args: unknown[]) => apiMocks.updateRunTitle(...args),
    listRuns: (...args: unknown[]) => apiMocks.listRuns(...args),
    listSoloSessions: (...args: unknown[]) => apiMocks.listSoloSessions(...args),
    cancelRun: (...args: unknown[]) => apiMocks.cancelRun(...args),
  },
}))

vi.mock('@/lib/composables/useWorkspaceContext', () => ({
  useWorkspaceContext: () => ({
    ensureLoaded: () => workspaceMocks.ensureLoaded(),
    workspace: workspaceMocks.workspace,
  }),
}))

vi.mock('@/lib/run/useRunTerminalNotifications', () => ({
  useRunTerminalNotifications: () => ({
    unreadCount: notifMocks.unreadCount,
  }),
}))

vi.mock('@/lib/run/useWorkflowFavorites', () => ({
  useWorkflowFavorites: () => ({
    displayItems: favMocks.displayItems,
    hydrateDisplay: favMocks.hydrateDisplay,
    unfavorite: favMocks.unfavorite,
    getFavoriteWorkflow: favMocks.getFavoriteWorkflow,
    reorderFavorites: favMocks.reorderFavorites,
  }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => breakpointMocks,
}))

vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({
  useWorkflowRunLaunch: () => ({
    openLaunch: launchMocks.openLaunch,
  }),
}))

import enNav from '@/locales/en/nav.json'
import { endInboxCancelDeparture, inboxCancelDepartureRunId } from '@/lib/inbox/inboxCancelDeparture'
import { __resetSidebarHandoffForTests, armSidebarHandoff, SIDEBAR_FLIGHT_MS } from '@/lib/inbox/sidebarHandoff'
import { __resetSidebarSectionsForTests, toggleSection } from '@/lib/shared/sidebarSections'
import { knownInboxProcessingLock } from '@/lib/inbox/inboxShareIntent'
import { useAuth } from '@/lib/composables/useAuth'
import { useToast } from '@/lib/composables/useToast'
import AppSidebarNav from './AppSidebarNav.vue'
import { RECENT_RELAY_DEBOUNCE_MS, notifyRecentActivity, resetRecentActivityRelayForTests } from '@/lib/run/recentRuns'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { readArtifactPanelOpen, resetConversationDrawers, writeArtifactPanelOpen } from '@/lib/chat/conversationDrawer'

/** Runs the default listRuns stub treats as still unfinished, in sidebar order. */
function unfinishedRunsFromInbox() {
  const displayed = gateMocks.displayedItems.value as Array<{
    runId?: string
    nodeId?: string
    runTitle?: string
    workflowName?: string
  }>
  const extras = gateMocks.sidebarExtras.value as Array<{
    runId?: string
    nodeId?: string
    runTitle?: string
    workflowName?: string
  }>
  const keyOf = (it: { runId?: string; nodeId?: string }) =>
    gateMocks.itemKey({ runId: String(it.runId ?? ''), nodeId: String(it.nodeId ?? '') })
  const shown = new Set(displayed.map((it) => keyOf(it)))
  const pending = [...extras.filter((it) => !shown.has(keyOf(it))), ...displayed]
  const seen = new Set<string>()
  const items = []
  for (const it of pending) {
    const id = String(it.runId ?? '')
    if (!id || seen.has(id)) continue
    seen.add(id)
    items.push({
      id,
      workflowId: 'wf',
      workflowName: typeof it.workflowName === 'string' ? it.workflowName : '',
      title: typeof it.runTitle === 'string' ? it.runTitle : '',
      status: 'waiting_human',
      trigger: 'manual',
      startedAt: 'T0',
      durationSec: 0,
      progress: 0,
    })
  }
  return items
}

beforeEach(() => {
  vi.clearAllMocks()
  favMocks.unfavorite.mockReset()
  gateMocks.peek.mockResolvedValue(undefined)
  gateMocks.refresh.mockResolvedValue(undefined)
  gateMocks.count.value = 2
  gateMocks.displayedItems.value = []
  gateMocks.sidebarExtras.value = []
  gateMocks.startFailedKeys.value = []
  gateMocks.activeInboxKey.value = null
  routerMocks.push.mockReset()
  notifMocks.unreadCount.value = 0
  favMocks.displayItems.value = []
  favMocks.hydrateDisplay.mockResolvedValue(undefined)
  breakpointMocks.isMobile.value = false
  endInboxCancelDeparture()
  routeState.path = '/dashboard'
  routeState.query = {}
  routeState.meta = {}
  knownInboxProcessingLock.value = false
  localStorage.clear()
  __resetSidebarSectionsForTests()
  __resetSidebarHandoffForTests()
  resetConversationDrawers()
  useAuth().clearUser()
  apiMocks.updateRunTitle.mockReset()
  apiMocks.renameSoloSession.mockReset()
  apiMocks.closeSoloSession.mockReset()
  apiMocks.listRuns.mockReset()
  apiMocks.listSoloSessions.mockReset()
  apiMocks.cancelRun.mockReset()
  apiMocks.cancelRun.mockResolvedValue({ status: 'cancelled' })
  // Inbox rows in these tests stand in for unfinished runs unless a case stubs listRuns itself.
  apiMocks.listRuns.mockImplementation(() => {
    const items = unfinishedRunsFromInbox()
    return Promise.resolve({ items, total: items.length })
  })
  apiMocks.listSoloSessions.mockResolvedValue({ sessions: [], maxSandboxes: 0 })
  workspaceMocks.ensureLoaded.mockReset()
  workspaceMocks.ensureLoaded.mockResolvedValue({ name: 'ws' })
  workspaceMocks.workspace.value = { name: 'ws' }
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  resetRecentActivityRelayForTests()
  const toast = useToast()
  for (const item of [...toast.toasts.value]) toast.dismiss(item.id)
  vi.useFakeTimers()
})

function mountNav(attachOrOptions: boolean | { realIcon?: boolean } = false) {
  const attach = attachOrOptions === true
  const realIcon = typeof attachOrOptions === 'object' && attachOrOptions.realIcon === true
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...nav } },
  })
  return mount(AppSidebarNav, {
    attachTo: attach ? document.body : undefined,
    global: {
      plugins: [i18n],
      stubs: realIcon ? {} : { Icon: true },
      directives: { 'hover-ink': vHoverInk },
    },
  })
}

describe('AppSidebarNav', () => {
  it('keeps one 正在执行 row per run through clarify, auto and the next waiting node', async () => {
    const runRow = (currentNode: Record<string, unknown>, status = 'running') => ({
      id: 'run-x', workflowId: 'wf', workflowName: 'w', title: '把登录做清楚', status, startedAt: 'T0', currentNode,
    })
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-x', nodeId: 'clarify', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '把登录做清楚' },
      { type: 'clarify', runId: 'run-y', nodeId: 'clarify', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '另一个' },
    ]
    gateMocks.activeInboxKey.value = 'run-x:clarify'
    routeState.path = '/gates'
    const wrapper = mountNav()
    await flushPromises()
    expect(apiMocks.listRuns).toHaveBeenCalledWith(expect.objectContaining({ status: 'queued,running,waiting_human' }))
    const keys = () => wrapper.findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))
    const rowX = () => wrapper.get('[data-key="run-x"]')
    expect(keys()).toEqual(['run-x', 'run-y'])
    expect(wrapper.get('[data-testid="nav-pending-approvals"]').text()).toContain('正在执行2')

    // 确认并流转: the pending item leaves; the row stays in place, running, before any reload.
    let resolveRuns: (v: unknown) => void = () => {}
    apiMocks.listRuns.mockImplementationOnce(() => new Promise((res) => { resolveRuns = res }))
    gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.runId !== 'run-x')
    await nextTick()
    expect(keys()).toEqual(['run-x', 'run-y'])
    expect(rowX().attributes('data-motion')).toBe('pulse')
    expect(rowX().get('.nav-row-title').text()).toBe('把登录做清楚')
    expect(rowX().classes()).toContain('active')

    // The run reports its auto node: same row, same title, still executing.
    resolveRuns({
      items: [
        runRow({ id: 'impl', name: '实现', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' }),
        { id: 'run-y', workflowId: 'wf', workflowName: 'w', title: '另一个', status: 'waiting_human', startedAt: 'T0' },
      ],
      total: 2,
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="nav-running-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))).toEqual(['run-y'])
    expect(wrapper.get('[data-testid="nav-auto-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))).toEqual(['run-x'])
    expect(wrapper.get('[data-testid="nav-pending-approvals"] .sidebar-section-count').text()).toBe('1')
    expect(wrapper.get('[data-testid="nav-auto-process"] .sidebar-section-count').text()).toBe('1')
    expect(wrapper.get('[data-testid="nav-auto-process"]').text()).toContain('自动过程')
    expect(rowX().attributes('data-motion')).toBeUndefined()
    expect(rowX().attributes('data-flight')).toBeUndefined()
    expect(rowX().get('icon-stub').attributes('name')).toBe('breath-dot')
    expect(rowX().get('.nav-row-title').text()).toBe('把登录做清楚')
    expect(rowX().get('[data-testid="nav-auto-node-name"]').text()).toBe('实现')
    await rowX().trigger('click')
    expect(routerMocks.push).toHaveBeenLastCalledWith({ path: '/runs/run-x/live', query: { node: 'impl' } })

    // Next node waits for the user: the row flies back to 正在执行.
    apiMocks.listRuns.mockResolvedValue({
      items: [
        runRow({ id: 'deliver', name: '交付', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' }, 'waiting_human'),
        { id: 'run-y', workflowId: 'wf', workflowName: 'w', title: '另一个', status: 'waiting_human', startedAt: 'T0' },
      ],
      total: 2,
    })
    routeState.path = '/board'
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-auto-process"]').exists()).toBe(false)
    expect(keys()).toEqual(['run-x', 'run-y'])
    expect(rowX().attributes('data-section')).toBe('running')
    expect(rowX().attributes('data-node-id')).toBe('deliver')
    expect(rowX().get('.nav-row-title').text()).toBe('把登录做清楚')
    expect(wrapper.text()).not.toContain('Run #')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('puts active Solo sessions in the shared 正在执行 section', async () => {
    apiMocks.listSoloSessions.mockResolvedValue({
      sessions: [
        {
          id: 'solo-1',
          title: 'hello',
          agentName: '需求澄清',
          status: 'starting',
          restarts: 0,
          busy: false,
          waiting: 0,
          createdAt: '2026-10-23T00:00:00Z',
          updatedAt: '2026-10-23T00:00:00Z',
        },
        {
          id: 'solo-closed',
          title: '已关闭',
          agentName: '需求澄清',
          status: 'closed',
          restarts: 0,
          busy: false,
          waiting: 0,
          createdAt: '2026-10-23T00:00:00Z',
          updatedAt: '2026-10-23T00:00:00Z',
        },
      ],
      maxSandboxes: 5,
    })
    const wrapper = mountNav()
    await flushPromises()
    const section = wrapper.get('[data-testid="nav-pending-approvals"]')
    expect(section.get('.sidebar-section-count').text()).toBe('1')
    const heading = section.get('[data-testid="nav-running-heading"]')
    expect(heading.classes()).toContain('grasp-status-host')
    expect(heading.find('[name="run-live"], [data-icon="run-live"]').exists()).toBe(true)
    const row = section.get('[data-kind="solo"]')
    expect(row.attributes('data-key')).toBe('solo-1')
    expect(row.get('.nav-row-title').text()).toBe('hello')
    expect(row.attributes('data-motion')).toBe('spin')
    expect(row.get('.nav-row-title').element.nextElementSibling).toBe(row.get('[data-testid="inbox-motion-spin"]').element)
    await row.trigger('click')
    expect(routerMocks.push).toHaveBeenLastCalledWith('/solo/solo-1')
    await row.trigger('contextmenu', { clientX: 20, clientY: 20 })
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="nav-pending-menu-share"]')).toBeNull()
    expect(document.querySelector('[data-testid="nav-pending-menu-rename"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="nav-pending-menu-cancel"]')).toBeTruthy()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps queued and waiting rows still while a confirmed running row remains busy', async () => {
    apiMocks.listRuns.mockResolvedValue({ items: [
      { id: 'queued', workflowId: 'wf', workflowName: 'w', title: '排队', status: 'queued', startedAt: 'T0' },
      { id: 'waiting', workflowId: 'wf', workflowName: 'w', title: '等待人工', status: 'waiting_human', startedAt: 'T0' },
      { id: 'running', workflowId: 'wf', workflowName: 'w', title: '执行中', status: 'running', startedAt: 'T0' },
    ] })
    const wrapper = mountNav()
    await flushPromises()
    const queued = wrapper.get('[data-key="queued"]')
    const waiting = wrapper.get('[data-key="waiting"]')
    expect(queued.attributes('data-motion')).toBeUndefined()
    expect(waiting.attributes('data-motion')).toBeUndefined()
    expect(queued.get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('queue')
    expect(waiting.get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('user-hold')
    expect(wrapper.get('[data-key="running"]').attributes('data-motion')).toBe('pulse')
    wrapper.unmount()
  })

  it('keeps the same 7 unfinished runs when the inbox has other runs and history opens', async () => {
    const unfinished = Array.from({ length: 7 }, (_, i) => ({
      id: `run-${i}`,
      workflowId: 'wf',
      workflowName: 'w',
      title: `执行 ${i}`,
      status: 'running',
      startedAt: 'T0',
    }))
    const inboxFor = (runId: string, title: string) => ({
      type: 'clarify' as const,
      runId,
      nodeId: 'n',
      label: '节点',
      kind: 'clarify',
      workflowName: 'w',
      runTitle: title,
    })
    const extras = Array.from({ length: 4 }, (_, i) => inboxFor(`extra-${i}`, `多余 ${i}`))
    apiMocks.listRuns.mockResolvedValue({ items: unfinished, total: 7 })
    gateMocks.displayedItems.value = [
      ...unfinished.map((r) => inboxFor(r.id, r.title)),
      ...extras,
      inboxFor('hist-done', '已结束的历史'),
    ]
    const wrapper = mountNav()
    await flushPromises()
    const keys = () => wrapper.findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))
    const count = () => wrapper.get('.sidebar-section-count').text()
    expect(keys()).toEqual(unfinished.map((r) => r.id))
    expect(count()).toBe('7')

    routeState.path = '/runs/hist-done'
    await flushPromises()
    expect(routeState.path).toBe('/runs/hist-done')
    expect(keys()).toEqual(unfinished.map((r) => r.id))
    expect(count()).toBe('7')
    expect(wrapper.text()).not.toContain('多余')
    expect(wrapper.text()).not.toContain('已结束的历史')

    apiMocks.listRuns.mockRejectedValueOnce(new Error('offline'))
    gateMocks.displayedItems.value = extras
    await flushPromises()
    expect(keys()).toEqual(unfinished.map((r) => r.id))
    expect(count()).toBe('7')

    gateMocks.displayedItems.value = unfinished.map((r) => inboxFor(r.id, r.title))
    await flushPromises()
    workspaceMocks.ensureLoaded.mockImplementationOnce(async () => null)
    gateMocks.displayedItems.value = extras
    await flushPromises()
    expect(keys()).toEqual(unfinished.map((r) => r.id))
    expect(count()).toBe('7')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('names the row after the run whether or not it is selected', async () => {
    apiMocks.listRuns.mockResolvedValue({
      items: [{ id: 'run-s', workflowId: 'wf', workflowName: 'w', title: '启动服务 我来指定需求', status: 'running', startedAt: 'T0' }],
      total: 1,
    })
    const wrapper = mountNav()
    await flushPromises()
    const title = () => wrapper.get('[data-key="run-s"]').get('.nav-row-title').text()
    expect(title()).toBe('启动服务 我来指定需求')
    // Selecting it opens a starting item whose own title came from elsewhere.
    gateMocks.sidebarExtras.value = [
      { type: 'clarify', runId: 'run-s', nodeId: 'ap', label: '哦哦发哦佛啊', kind: 'clarify', state: 'starting', workflowName: 'w', runTitle: '哦哦发哦佛啊' },
    ]
    gateMocks.activeInboxKey.value = 'run-s:ap'
    routeState.path = '/gates'
    await flushPromises()
    expect(wrapper.get('[data-key="run-s"]').classes()).toContain('active')
    expect(title()).toBe('启动服务 我来指定需求')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows a blank title bar instead of a stand-in while a starting run has no title yet', async () => {
    gateMocks.sidebarExtras.value = [
      { type: 'clarify', runId: 'run-new', nodeId: 'incoming', label: '', kind: 'clarify', state: 'starting', workflowName: '', runTitle: '' },
    ]
    const wrapper = mountNav()
    await flushPromises()
    const row = wrapper.get('[data-key="run-new"]')
    expect(row.find('[data-testid="nav-pending-title-skeleton"]').exists()).toBe(true)
    expect(row.text()).not.toContain('…')
    expect(row.text()).not.toContain('Run #')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('renders nav links from sidebar config', async () => {
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('nav').exists()).toBe(true)
    expect(gateMocks.refresh).toHaveBeenCalled()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('does not show a gates nav item or badge in workspace chrome', async () => {
    notifMocks.unreadCount.value = 46
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-gates-badge"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/gates"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('待办')
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-notifications-badge"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows the notification badge from the shared unreadCount in settings chrome', async () => {
    routeState.path = '/notifications'
    notifMocks.unreadCount.value = 46
    const wrapper = mountNav()
    await flushPromises()
    const badge = wrapper.find('[data-testid="nav-notifications-badge"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('46')
    expect(wrapper.find('[data-to="/notifications"]').classes()).toContain('active')
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-gates-badge"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides quick-workflows section when there are no favorites', async () => {
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('快捷工作流')
    // Primary workspace nav still present (plan g1.1)
    expect(wrapper.find('[data-to="/board"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/settings"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/dashboard"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/gates"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/dashboard"]').text()).toContain('新需求')
    expect(wrapper.find('[data-to="/board"]').text()).toContain('看板')
    expect(wrapper.text()).not.toContain('待办')
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(true)
    const workspace = wrapper.find('[data-testid="nav-workspace-chrome"]')
    expect(workspace.exists()).toBe(true)
    expect(workspace.findAll('a.nav-item').map((l) => l.attributes('data-to'))).toEqual([
      '/dashboard',
      '/board',
      '/workflows',
      '/stats',
      '/settings',
    ])
    expect(workspace.find('[data-to="/workflows"]').text()).toContain('工作流')
    expect(workspace.find('[data-to="/stats"]').text()).toContain('用量统计')
    expect(wrapper.text()).not.toContain('配置')
    await wrapper.setProps({ rail: true })
    expect(workspace.findAll('a.nav-item').map((link) => link.attributes('data-to'))).toEqual(['/dashboard'])
    expect(workspace.find('[data-to="/stats"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })




  it('settings chrome replaces workspace items and hides quick workflows (plan g2.1 / g1.2)', async () => {
    routeState.path = '/notifications'
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').attributes('data-to')).toBe('/dashboard')
    const categoryLinks = wrapper
      .find('[data-testid="nav-settings-chrome"]')
      .findAll('a.nav-item')
      .filter((l) => l.attributes('data-testid') !== 'nav-back-home')
    expect(categoryLinks[0].attributes('data-to')).toBe('/board')
    expect(categoryLinks[0].text()).toContain('看板')
    expect(wrapper.findAll('[data-testid="settings-nav-group"]').map((heading) => heading.text())).toEqual(['工作区工具', '执行配置', '平台配置'])
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(false)
    const settingsTos = categoryLinks.map((link) => (
      link.attributes('data-testid') === 'nav-settings-integrations'
        ? '/settings?integrations=1'
        : link.attributes('data-to')
    ))
    expect(settingsTos).toEqual([
      '/board',
      '/requirements',
      '/workflows',
      '/assistant',
      '/artifacts',
      '/notifications',
      '/settings/schedules',
      '/settings/audit',
      '/agents',
      '/sandboxes',
      '/settings/projects',
      '/settings/credentials',
      '/settings/variables',
      '/settings/assistant',
      '/settings/notification-policy',
      '/settings/external-mcp',
      '/settings',
      '/settings?integrations=1',
      '/settings/workspace',
    ])
    expect(wrapper.find('[data-testid="nav-settings-integrations"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('返回工作区')
    expect(wrapper.text()).toContain('常规')
    expect(wrapper.find('[data-to="/agents"]').text()).toContain('智能体')
    expect(wrapper.text()).not.toContain('待办')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('highlights usage stats in the workspace list and hides that text on the wide sidebar', async () => {
    routeState.path = '/stats'
    routeState.query = { window: '7d' }
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    const links = wrapper.find('[data-testid="nav-workspace-chrome"]').findAll('a.nav-item')
    expect(links.map((link) => link.attributes('data-to'))).toEqual([
      '/dashboard',
      '/board',
      '/workflows',
      '/stats',
      '/settings',
    ])
    const stats = wrapper.get('[data-to="/stats"]')
    expect(stats.classes()).toContain('active')
    expect(stats.text()).toContain('用量统计')
    expect(wrapper.get('[data-to="/settings"]').classes()).not.toContain('active')
    await wrapper.setProps({ rail: true })
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').findAll('a.nav-item').map((link) => link.attributes('data-to'))).toEqual(['/dashboard'])
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps workspace chrome and highlights workflows on workflow routes', async () => {
    routeState.path = '/workflows'
    const list = mountNav()
    await flushPromises()
    expect(list.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(list.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(list.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    expect(list.text()).not.toContain('返回工作区')
    expect(list.find('[data-to="/workflows"]').classes()).toContain('active')
    list.unmount()

    routeState.path = '/workflows/abc'
    routeState.query = { tab: 'agents' }
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    const workflowsLink = wrapper.find('[data-to="/workflows"]')
    expect(workflowsLink.classes()).toContain('active')
    expect(wrapper.find('[data-to="/board"]').classes()).not.toContain('active')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('highlights general only on exact /settings (plan g2.2)', async () => {
    routeState.path = '/settings'
    const general = mountNav()
    await flushPromises()
    const links = general.findAll('[data-to="/settings"]')
    const generalLink = links.find((l) => l.text().includes('常规'))
    expect(generalLink?.classes()).toContain('active')
    expect(general.find('[data-to="/settings/platform-rules"]').exists()).toBe(false)
    general.unmount()
    vi.useRealTimers()
  })

  it('full pages do not insert settings chrome (plan g3.1)', async () => {
    routeState.path = '/runs/run-1'
    routeState.meta = { full: true }
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('uses workspace chrome and highlights board for the kanban and run list', async () => {
    routeState.path = '/board'
    const board = mountNav()
    await flushPromises()
    expect(board.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(board.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(board.find('[data-to="/board"]').classes()).toContain('active')
    expect(board.find('[data-to="/dashboard"]').classes()).not.toContain('active')
    expect(board.find('[data-to="/runs"]').exists()).toBe(false)
    board.unmount()

    routeState.path = '/runs'
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/board"]').classes()).toContain('active')
    expect(wrapper.find('[data-to="/dashboard"]').classes()).not.toContain('active')
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('switches chrome after navigation and still renders the target (g2.2)', async () => {
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    // Stored favorites must not render a quick-workflow block or the starred name.
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('快捷工作流')
    expect(wrapper.text()).not.toContain('夜间回归')

    routeState.path = '/agents'
    await flushPromises()
    await nextTick()
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/agents"]').text()).toContain('智能体')

    routeState.path = '/dashboard'
    await flushPromises()
    await nextTick()
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('source includes chrome slide transition and reduced-motion instant swap (g2.2)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSidebarNav.vue'), 'utf8')
    expect(src).toMatch(/chrome-slide-left/)
    expect(src).toMatch(/chrome-slide-right/)
    expect(src).toMatch(/translateX/)
    expect(src).toMatch(/prefers-reduced-motion:\s*reduce/)
    expect(src).toMatch(/chromeHasMounted/)
  })

  it('keeps the new-request active row transparent and other selections gray', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSidebarNav.vue'), 'utf8')
    const style = src.slice(src.indexOf('<style scoped>'))
    const block = (selector: string) => {
      const found = style.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`, 's'))
      expect(found, selector).not.toBeNull()
      return found![0]
    }

    const createIdle = block('\\.sidebar-workspace-nav \\.nav-create-action')
    expect(createIdle).toContain('background: transparent')
    expect(createIdle).toContain('font-weight: 500')
    expect(createIdle).toContain('--hover-ink-color: rgb(var(--c-txt) / .045)')

    const createActive = block('\\.sidebar-workspace-nav \\.nav-create-action\\.active')
    expect(createActive).toContain('background: transparent')
    expect(createActive).toContain('font-weight: 600')
    expect(createActive).not.toMatch(/rgb\(var\(--c-txt\)\s*\/\s*\.055\)/)

    const itemActive = block('\\.sidebar-workspace-nav \\.nav-item\\.active')
    expect(itemActive).toContain('background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065))')

    const settingsActive = block('\\.settings-directory \\.nav-item\\.active')
    expect(settingsActive).toContain('background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065))')

    const genericAt = style.indexOf('.sidebar-workspace-nav .nav-item.active')
    const createAt = style.indexOf('.sidebar-workspace-nav .nav-create-action.active')
    expect(genericAt).toBeGreaterThanOrEqual(0)
    expect(createAt).toBeGreaterThan(genericAt)
  })

  it('workspace and settings nav-items use v-hover-ink (plan g2.1 / g2.3)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSidebarNav.vue'), 'utf8')
    expect(src).toMatch(/v-hover-ink/)
    // All three nav-item RouterLinks carry the directive.
    expect([...src.matchAll(/v-hover-ink/g)].length).toBeGreaterThanOrEqual(3)

    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    expect(css).toMatch(/\.nav-item\s*\{[^}]*--hover-ink-color:\s*rgb\(var\(--c-elevated\)\)/s)
    expect(css).not.toMatch(/\.nav-item\s*\{[^}]*hover:bg-elevated/s)
    // review v1: ink under bare text via z-index:-1
    expect(css).toMatch(/\.hover-ink-host\s*>\s*\.hover-ink\s*\{[^}]*z-index:\s*-1/s)
    expect(css).toMatch(/\.hover-ink-host\s*>\s*:not\(\.hover-ink\)/)
    expect(css).toMatch(/transition:\s*transform\s*350ms/)
    // plan g1.1 / g1.3: shared transform centering (workspace + settings), no neg-margin box
    expect(css).toMatch(/translate\(-50%,\s*-50%\)\s*scale\(0\)/)
    expect(css).not.toMatch(/margin:\s*calc\(\s*var\(--ink-d/)
  })

  it('keeps active workspace nav icons left-aligned after left-edge ink (plan g2.1)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    // Active only elevates fill/weight — no horizontal padding/margin that would skew icons.
    expect(css).toMatch(/\.nav-item\.active\s*\{[^}]*bg-elevated[^}]*font-semibold/s)
    expect(css).not.toMatch(/\.nav-item\.active\s*\{[^}]*(?:pl-|pr-|ml-|mr-|translateX|padding-left|margin-left)/s)
    expect(css).toMatch(/\.hover-ink-host\s*\{[^}]*overflow:\s*clip/s)
    expect(css).not.toMatch(/margin:\s*calc\(\s*var\(--ink-d/)

    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )

    routeState.path = '/dashboard'
    const wrapper = mountNav()
    await flushPromises()
    const chrome = wrapper.find('[data-testid="nav-workspace-chrome"]')
    const links = chrome.findAll('a.nav-item')
    expect(links.length).toBeGreaterThanOrEqual(2)

    const active = links.find((l) => l.classes().includes('active'))
    const idle = links.find((l) => !l.classes().includes('active'))
    expect(active).toBeTruthy()
    expect(idle).toBeTruthy()

    const activeEl = active!.element as HTMLElement
    const idleEl = idle!.element as HTMLElement
    for (const el of [activeEl, idleEl]) {
      Object.defineProperty(el, 'getBoundingClientRect', {
        configurable: true,
        value: () => ({
          left: 12,
          top: 40,
          width: 180,
          height: 36,
          right: 192,
          bottom: 76,
          x: 12,
          y: 40,
          toJSON: () => ({}),
        }),
      })
    }

    const iconLeft = (el: HTMLElement) => {
      const icon = el.querySelector('[data-icon], .icon, svg, i, span') as HTMLElement | null
      // Stubbed Icon has no layout; measure first non-ink child box via style contract.
      const child = [...el.children].find((c) => !c.classList.contains('hover-ink')) as HTMLElement
      expect(child).toBeTruthy()
      Object.defineProperty(child, 'getBoundingClientRect', {
        configurable: true,
        value: () => ({
          left: 24,
          top: 48,
          width: 17,
          height: 17,
          right: 41,
          bottom: 65,
          x: 24,
          y: 48,
          toJSON: () => ({}),
        }),
      })
      return (icon ?? child).getBoundingClientRect().left
    }

    const beforeActive = iconLeft(activeEl)
    const beforeIdle = iconLeft(idleEl)
    expect(Math.abs(beforeActive - beforeIdle)).toBeLessThanOrEqual(1)

    activeEl.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 14, clientY: 58, pointerType: 'mouse', bubbles: true }),
    )
    expect(activeEl.classList.contains('is-hover-ink')).toBe(true)
    expect(activeEl.scrollLeft).toBe(0)
    expect(Math.abs(iconLeft(activeEl) - iconLeft(idleEl))).toBeLessThanOrEqual(1)

    // plan g1.2 / g2.1: leave must collapse --ink-d (sticky-shift root cause)
    const d = Number.parseFloat(activeEl.style.getPropertyValue('--ink-d'))
    expect(d).toBeGreaterThan(0)
    activeEl.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
    expect(activeEl.classList.contains('is-hover-ink')).toBe(false)
    const { HOVER_INK_DURATION_MS } = await import('@/lib/shared/hoverInkGeometry')
    await vi.advanceTimersByTimeAsync(HOVER_INK_DURATION_MS)
    expect(activeEl.style.getPropertyValue('--ink-d')).toBe('0px')
    expect(Math.abs(iconLeft(activeEl) - iconLeft(idleEl))).toBeLessThanOrEqual(1)

    wrapper.unmount()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('settings chrome retract collapses ink box after leave (plan g1.3 / g2.2)', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )

    routeState.path = '/settings'
    routeState.meta = { settingsSection: 'general' }
    const wrapper = mountNav()
    await flushPromises()
    // allow settings chrome mount animation
    await vi.advanceTimersByTimeAsync(400)
    await nextTick()

    const chrome = wrapper.find('[data-testid="nav-settings-chrome"]')
    expect(chrome.exists()).toBe(true)
    const items = chrome.findAll('a.nav-item')
    expect(items.length).toBeGreaterThanOrEqual(2)

    const target = (items.find((l) => l.attributes('data-testid') === 'nav-back-home') ?? items[0])
      .element as HTMLElement
    Object.defineProperty(target, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({
        left: 12,
        top: 80,
        width: 180,
        height: 36,
        right: 192,
        bottom: 116,
        x: 12,
        y: 80,
        toJSON: () => ({}),
      }),
    })

    target.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 14, clientY: 98, pointerType: 'mouse', bubbles: true }),
    )
    expect(target.classList.contains('is-hover-ink')).toBe(true)
    expect(Number.parseFloat(target.style.getPropertyValue('--ink-d'))).toBeGreaterThan(100)

    target.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
    const { HOVER_INK_DURATION_MS } = await import('@/lib/shared/hoverInkGeometry')
    await vi.advanceTimersByTimeAsync(HOVER_INK_DURATION_MS)
    expect(target.style.getPropertyValue('--ink-d')).toBe('0px')
    expect(target.scrollLeft).toBe(0)

    wrapper.unmount()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('keeps hover-ink host and ink node after route-driven active class patch', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    expect(css).toMatch(/(?:^|\n)\.hover-ink\s*\{[^}]*position:\s*absolute/s)

    routeState.path = '/dashboard'
    const wrapper = mountNav()
    await flushPromises()

    const chrome = wrapper.find('[data-testid="nav-workspace-chrome"]')
    const dashboard = chrome.find('[data-to="/dashboard"]').element as HTMLElement
    const board = chrome.find('[data-to="/board"]').element as HTMLElement
    expect(dashboard.classList.contains('hover-ink-host')).toBe(true)
    expect((dashboard.firstElementChild as HTMLElement | null)?.classList.contains('hover-ink')).toBe(true)

    routeState.path = '/board'
    await flushPromises()
    await nextTick()

    const nextActive = chrome.find('a.nav-item.active').element as HTMLElement
    expect(nextActive).toBe(board)
    expect(nextActive.classList.contains('active')).toBe(true)
    expect(nextActive.classList.contains('hover-ink-host')).toBe(true)
    expect((nextActive.firstElementChild as HTMLElement | null)?.classList.contains('hover-ink')).toBe(true)
    expect(dashboard.classList.contains('hover-ink-host')).toBe(true)
    expect((dashboard.firstElementChild as HTMLElement | null)?.classList.contains('hover-ink')).toBe(true)

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('lists pending approvals under the nav and never shows processed records', async () => {
    gateMocks.displayedItems.value = [
      { type: 'gate', runId: 'run-1', nodeId: 'g1', title: '发布前确认', runTitle: '发布前确认', workflowName: 'w' },
      { type: 'clarify', runId: 'run-2', nodeId: 'c1', label: '需求规格', runTitle: '需求规格', kind: 'clarify', state: 'starting', workflowName: 'w' },
      { type: 'clarify', runId: 'run-3', nodeId: 'r1', label: '代码评审', runTitle: '代码评审', kind: 'review', state: 'replying', workflowName: 'w' },
    ]
    gateMocks.startFailedKeys.value = []
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()

    const pending = wrapper.get('[data-testid="nav-pending-approvals"]')
    expect(pending.text()).toContain('正在执行')
    expect(wrapper.find('[data-testid="nav-inbox-records"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-record-row"]').exists()).toBe(false)

    const rows = pending.findAll('[data-testid="nav-pending-row"]')
    expect(rows.map((row) => row.text())).toEqual(['发布前确认', '需求规格', '代码评审'])
    expect(rows[0].find('[data-testid="inbox-motion-spin"]').exists()).toBe(false)
    expect(rows[0].find('[data-testid="inbox-motion-pulse"]').exists()).toBe(false)
    expect(rows[1].attributes('data-motion')).toBeUndefined()
    expect(rows[1].get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('arc-spinner')
    expect(rows[2].attributes('data-motion')).toBeUndefined()
    expect(rows[2].get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('breath-dot')
    expect(rows[2].text()).not.toContain('正在回复')

    rows[0].element.classList.add('check')
    expect(rows[0].classes()).toContain('nav-item')
    expect(rows[0].classes()).not.toContain('active')

    gateMocks.activeInboxKey.value = 'run-1:g1'
    routeState.path = '/gates'
    await nextTick()
    expect(rows[0].classes()).toContain('active')

    await rows[1].trigger('click')
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-2', node: 'c1' } })
    expect(routerMocks.push).toHaveBeenCalledTimes(1)

    gateMocks.startFailedKeys.value = ['run-2:c1']
    await nextTick()
    expect(rows[1].attributes('data-motion')).toBeUndefined()
    expect(rows[1].find('[data-testid="inbox-motion-spin"]').exists()).toBe(false)

    gateMocks.displayedItems.value = []
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-inbox-records"]').exists()).toBe(false)

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('left-aligns pending row titles, keeping the motion mark at the row end', async () => {
    const longLabel = `需求澄清${'很长的节点名'.repeat(12)}`
    gateMocks.displayedItems.value = [
      { type: 'gate', runId: 'run-1', nodeId: 'g1', title: '发布前确认', runTitle: '发布前确认', workflowName: 'w' },
      { type: 'clarify', runId: 'run-2', nodeId: 'c1', label: '需求澄清', runTitle: '需求澄清', kind: 'clarify', state: 'starting', workflowName: 'w' },
      { type: 'clarify', runId: 'run-3', nodeId: 'long', label: longLabel, runTitle: longLabel, kind: 'clarify', state: 'starting', workflowName: 'w' },
      { type: 'clarify', runId: 'run-4', nodeId: 'pulse', label: '回复中的澄清', runTitle: '回复中的澄清', kind: 'clarify', state: 'replying', workflowName: 'w' },
    ]
    const wrapper = mountNav()
    await flushPromises()

    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSidebarNav.vue'), 'utf8')
    const titleRule = src.match(/\.nav-row-title\s*\{[^}]*\}/)?.[0] ?? ''
    expect(titleRule).toMatch(/text-align:\s*(?:start|left)/)
    // happy-dom only resolves styles for connected nodes, and unit tests do not load Tailwind.
    const style = document.createElement('style')
    style.textContent = titleRule
    document.head.appendChild(style)
    document.body.appendChild(wrapper.element)

    const pending = wrapper.get('[data-testid="nav-pending-approvals"]')
    expect(wrapper.find('[data-testid="nav-inbox-records"]').exists()).toBe(false)
    const rows = pending.findAll('[data-testid="nav-pending-row"]')
    expect(rows).toHaveLength(4)

    const expectRowStart = (row: ReturnType<typeof rows.find>, markTestId?: string) => {
      const title = row!.get('.nav-row-title')
      const align = getComputedStyle(title.element).textAlign
      expect(['left', 'start']).toContain(align)
      expect(title.classes()).toEqual(expect.arrayContaining(['min-w-0', 'flex-1', 'truncate', 'text-start']))
      expect(row!.classes()).toContain('text-start')
      expect(title.classes().some((name) => /^(pl|ml|indent)-/.test(name))).toBe(false)
      if (markTestId) {
        const mark = row!.get(`[data-testid="${markTestId}"]`)
        expect(title.element.nextElementSibling).toBe(mark.element)
      } else {
        expect(title.element.nextElementSibling).toBeNull()
      }
    }

    expectRowStart(rows[0], 'nav-pending-type-icon')
    expectRowStart(rows[1], 'nav-pending-type-icon')
    expect(rows[1].get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('arc-spinner')
    expectRowStart(rows[2], 'nav-pending-type-icon')
    expect(rows[2].get('.nav-row-title').text()).toBe(longLabel)
    expectRowStart(rows[3], 'nav-pending-type-icon')
    expect(rows[3].get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('breath-dot')

    expect(rows[0].classes()).not.toContain('active')
    gateMocks.activeInboxKey.value = 'run-1:g1'
    routeState.path = '/gates'
    await nextTick()
    expect(rows[0].classes()).toContain('active')
    expectRowStart(rows[0], 'nav-pending-type-icon')

    const heading = pending.get('div')
    expect(heading.classes()).toContain('px-3')
    expect(heading.classes()).not.toContain('nav-row-title')
    expect(heading.classes()).not.toContain('text-start')
    for (const link of wrapper.findAll('a.nav-item')) {
      expect(link.classes()).not.toContain('text-start')
      expect(link.find('.nav-row-title').exists()).toBe(false)
    }

    await rows[2].trigger('click')
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-3', node: 'long' } })

    style.remove()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps a selected pending title at weight 400 and selected nav items at 600', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const dir = dirname(fileURLToPath(import.meta.url))
    const css = readFileSync(join(dir, '../../styles/global.css'), 'utf8')
    expect(css).toMatch(/\.nav-item\.active\s*\{[^}]*bg-elevated[^}]*font-semibold[^}]*text-txt/s)
    const src = readFileSync(join(dir, 'AppSidebarNav.vue'), 'utf8')
    const titleRule = src.match(/\.nav-row-title\s*\{[^}]*\}/)?.[0] ?? ''
    expect(titleRule).toMatch(/font-weight:\s*400/)

    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'c1', label: '需求澄清', kind: 'clarify', state: 'starting', workflowName: 'w' },
    ]
    gateMocks.activeInboxKey.value = 'run-1:c1'
    routeState.path = '/gates'
    const wrapper = mountNav()
    await flushPromises()

    const style = document.createElement('style')
    style.textContent = `${titleRule}\n.nav-item.active { font-weight: 600; }`
    document.head.appendChild(style)
    document.body.appendChild(wrapper.element)

    const pendingRow = wrapper.get('[data-testid="nav-pending-row"]')
    const pendingTitle = pendingRow.get('.nav-row-title')
    expect(pendingRow.classes()).toContain('active')
    expect(getComputedStyle(pendingTitle.element).fontWeight).toBe('400')
    for (const link of wrapper.findAll('a.nav-item')) {
      expect(link.find('.nav-row-title').exists()).toBe(false)
    }

    routeState.path = '/runs'
    await nextTick()
    const board = wrapper.get('[data-to="/board"]')
    expect(board.classes()).toContain('active')
    expect(pendingRow.classes()).not.toContain('active')
    expect(getComputedStyle(board.element).fontWeight).toBe('600')
    expect(getComputedStyle(pendingTitle.element).fontWeight).toBe('400')
    for (const to of ['/dashboard', '/board', '/workflows', '/stats', '/settings']) {
      expect(wrapper.get(`[data-to="${to}"]`).find('.nav-row-title').exists()).toBe(false)
    }

    routeState.path = '/settings'
    await flushPromises()
    await nextTick()
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    const settingsChrome = wrapper.get('[data-testid="nav-settings-chrome"]')
    const activeSettings = settingsChrome.findAll('a.nav-item').find((link) => link.classes().includes('active'))
    expect(activeSettings).toBeTruthy()
    expect(getComputedStyle(activeSettings!.element).fontWeight).toBe('600')
    expect(settingsChrome.find('.nav-row-title').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)

    style.remove()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps executing row marks dynamic and shows a static type icon only while waiting', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-s', nodeId: 's', label: '启动中', kind: 'clarify', state: 'starting', workflowName: 'w' },
      { type: 'clarify', runId: 'run-p', nodeId: 'p', label: '正在回复', kind: 'review', state: 'replying', workflowName: 'w' },
      { type: 'clarify', runId: 'run-f', nodeId: 'f', label: '启动失败', kind: 'clarify', state: 'starting', workflowName: 'w' },
      { type: 'clarify', runId: 'run-c', nodeId: 'c', label: '需求澄清', kind: 'clarify', workflowName: 'w' },
      { type: 'clarify', runId: 'run-r', nodeId: 'r', label: '复审', kind: 'review', workflowName: 'w' },
      { type: 'gate', runId: 'run-g', nodeId: 'g', title: '人工门禁', workflowName: 'w' },
    ]
    gateMocks.startFailedKeys.value = ['run-f:f']
    const wrapper = mountNav({ realIcon: true })
    await flushPromises()

    const rows = wrapper.get('[data-testid="nav-pending-approvals"]').findAll('[data-testid="nav-pending-row"]')
    expect(rows).toHaveLength(6)

    const expectStatusIcon = (row: (typeof rows)[number], name: string, toneClass: string, glyph: string) => {
      expect(row.find('[data-testid="inbox-motion-spin"]').exists()).toBe(false)
      expect(row.find('[data-testid="inbox-motion-pulse"]').exists()).toBe(false)
      expect(row.attributes('data-motion')).toBeUndefined()
      expect(row.findAll('svg')).toHaveLength(1)
      const icon = row.get('[data-testid="nav-pending-type-icon"]')
      expect(icon.attributes('data-icon')).toBe(name)
      expect(icon.attributes('width')).toBe('16')
      expect(icon.attributes('height')).toBe('16')
      expect(icon.classes()).toContain('nav-row-type-icon')
      expect(icon.classes()).toContain(toneClass)
      expect(icon.html()).toContain(glyph)
      expect(row.get('.nav-row-title').element.nextElementSibling).toBe(icon.element)
    }
    expectStatusIcon(rows[0], 'arc-spinner', 'nav-row-type-icon--accent', 'M12 3A9 9 0 1 1 3 12')
    expectStatusIcon(rows[1], 'breath-dot', 'nav-row-type-icon--accent', 'grasp-heart-ring')
    expect(rows[1].get('[data-testid="nav-pending-type-icon"]').attributes('data-icon-motion')).toBe('busy')
    expect(rows[1].get('.nav-breath-ping').attributes('vector-effect')).toBe('non-scaling-stroke')
    expect(rows[1].findAll('svg')).toHaveLength(1)
    expect(rows[0].find('.nav-breath-ping').exists()).toBe(false)
    expect(rows[3].find('.nav-breath-ping').exists()).toBe(false)
    expect(rows[5].find('.nav-breath-ping').exists()).toBe(false)

    expect(rows[2].attributes('data-motion')).toBeUndefined()
    expect(rows[2].findAll('svg')).toHaveLength(0)
    expect(rows[2].find('[data-testid="nav-pending-type-icon"]').exists()).toBe(false)
    expect(rows[2].get('.nav-row-title').element.nextElementSibling).toBeNull()

    const expectTypeIcon = (row: (typeof rows)[number], toneClass: string, glyph: string) => {
      expect(row.find('[data-testid="inbox-motion-spin"]').exists()).toBe(false)
      expect(row.find('[data-testid="inbox-motion-pulse"]').exists()).toBe(false)
      expect(row.findAll('svg')).toHaveLength(1)
      const icon = row.get('[data-testid="nav-pending-type-icon"]')
      expect(icon.attributes('width')).toBe('16')
      expect(icon.attributes('height')).toBe('16')
      expect(icon.classes()).toContain('nav-row-type-icon')
      expect(icon.classes()).toContain(toneClass)
      expect(icon.html()).toContain(glyph)
      expect(row.get('.nav-row-title').element.nextElementSibling).toBe(icon.element)
    }
    expectTypeIcon(rows[3], 'nav-row-type-icon--hold', 'M3.8 20a6.4')
    expect(rows[3].html()).not.toContain('M8 5h8')
    expectTypeIcon(rows[4], 'nav-row-type-icon--hold', 'cx="18.6"')
    expectTypeIcon(rows[5], 'nav-row-type-icon--hold', 'grasp-user-dot')
    expect(rows[5].html()).not.toContain('M8 5h8')

    gateMocks.activeInboxKey.value = 'run-c:c'
    routeState.path = '/gates'
    await nextTick()
    expect(rows[3].classes()).toContain('active')
    expectTypeIcon(rows[3], 'nav-row-type-icon--hold', 'M3.8 20a6.4')

    gateMocks.activeInboxKey.value = 'run-s:s'
    await nextTick()
    expect(rows[0].classes()).toContain('active')
    expectStatusIcon(rows[0], 'arc-spinner', 'nav-row-type-icon--accent', 'M12 3A9 9 0 1 1 3 12')

    gateMocks.activeInboxKey.value = 'run-p:p'
    await nextTick()
    expect(rows[1].classes()).toContain('active')
    expectStatusIcon(rows[1], 'breath-dot', 'nav-row-type-icon--accent', 'grasp-heart-ring')
    expect(rows[1].get('.nav-breath-ping').exists()).toBe(true)
    expect(rows[0].find('.nav-breath-ping').exists()).toBe(false)

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('switches one executing row icon when state or currentNode changes, without remounting', async () => {
    const node = (id: string, state: string, kind = 'clarify', mode = 'clarify') => ({
      id, name: id, iteration: 1, kind, mode, state,
    })
    const run = (id: string, currentNode: ReturnType<typeof node> | undefined, status = 'running') => ({
      id, workflowId: 'wf', workflowName: 'w', title: id, status, startedAt: 'T0', currentNode,
    })
    const row = (runId: string, nodeId: string, title: string, state?: string) => ({
      type: 'clarify' as const,
      runId,
      nodeId,
      label: title,
      kind: 'clarify' as const,
      workflowName: 'w',
      runTitle: title,
      ...(state ? { state } : {}),
    })
    const publish = async (items: ReturnType<typeof run>[]) => {
      apiMocks.listRuns.mockResolvedValue({ items, total: items.length })
      notifyRecentActivity()
      await vi.advanceTimersByTimeAsync(RECENT_RELAY_DEBOUNCE_MS)
      await flushPromises()
    }
    gateMocks.displayedItems.value = [row('run-a', 'n1', '甲'), row('run-b', 'n2', '乙')]
    const wrapper = mountNav()
    await flushPromises()
    const icon = (key: string) => wrapper.get(`[data-key="${key}"]`).get('[data-testid="nav-pending-type-icon"]').attributes('name')
    const rowEl = wrapper.get('[data-key="run-a"]').element
    expect(icon('run-a')).toBe('user-hold')
    expect(icon('run-b')).toBe('user-hold')

    const composer = document.createElement('textarea')
    composer.value = '草稿还在'
    document.body.append(composer)
    composer.focus()

    gateMocks.displayedItems.value = [row('run-a', 'n1', '甲', 'replying'), row('run-b', 'n2', '乙')]
    await nextTick()
    expect(icon('run-a')).toBe('breath-dot')
    expect(icon('run-b')).toBe('user-hold')
    expect(wrapper.get('[data-key="run-a"]').element).toBe(rowEl)

    gateMocks.displayedItems.value = [row('run-a', 'n1', '甲'), row('run-b', 'n2', '乙')]
    await nextTick()
    expect(icon('run-a')).toBe('user-hold')
    expect(icon('run-b')).toBe('user-hold')

    gateMocks.displayedItems.value = [row('run-a', 'n1', '甲', 'starting'), row('run-b', 'n2', '乙')]
    await nextTick()
    expect(icon('run-a')).toBe('arc-spinner')
    expect(icon('run-b')).toBe('user-hold')

    await publish([
      run('run-a', node('n1', 'busy', 'review', 'manual'), 'waiting_human'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-a')).toBe('arc-spinner')
    expect(icon('run-b')).toBe('user-hold')

    gateMocks.displayedItems.value = [row('run-a', 'n1', '甲'), row('run-b', 'n2', '乙')]
    await nextTick()
    expect(icon('run-a')).toBe('breath-dot')
    expect(icon('run-b')).toBe('user-hold')
    expect(wrapper.get('[data-key="run-a"]').element).toBe(rowEl)

    await publish([
      run('run-a', node('n1', 'running', 'clarify', 'clarify'), 'running'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-a')).toBe('arc-spinner')
    expect(icon('run-b')).toBe('user-hold')

    await publish([
      run('run-a', node('n1', 'stopping', 'agent', 'manual'), 'running'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-a')).toBe('arc-spinner')
    expect(icon('run-b')).toBe('user-hold')

    await publish([
      run('run-a', node('other', 'running', 'agent', 'auto'), 'running'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-a')).toBe('user-hold')
    expect(icon('run-b')).toBe('user-hold')

    await publish([
      run('run-a', undefined, 'queued'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-a')).toBe('queue')
    expect(icon('run-b')).toBe('user-hold')

    gateMocks.sidebarExtras.value = [row('run-e', 'e', '占位')]
    await publish([
      run('run-a', undefined, 'queued'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
      run('run-e', node('e', 'waiting_human'), 'waiting_human'),
    ])
    expect(icon('run-e')).toBe('user-hold')
    gateMocks.sidebarExtras.value = [row('run-e', 'e', '占位', 'replying')]
    await nextTick()
    expect(icon('run-e')).toBe('breath-dot')
    expect(icon('run-a')).toBe('queue')
    expect(icon('run-b')).toBe('user-hold')
    await publish([
      run('run-a', undefined, 'queued'),
      run('run-b', node('n2', 'waiting_human'), 'waiting_human'),
      run('run-e', node('e', 'running', 'clarify', 'clarify'), 'running'),
    ])
    expect(icon('run-e')).toBe('arc-spinner')
    expect(icon('run-a')).toBe('queue')
    expect(icon('run-b')).toBe('user-hold')

    expect(composer.value).toBe('草稿还在')
    expect(document.activeElement).toBe(composer)
    composer.remove()
    wrapper.unmount()
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('hides pending approvals on the settings nav even when items exist', async () => {
    routeState.path = '/settings'
    gateMocks.displayedItems.value = [
      { type: 'gate', runId: 'run-1', nodeId: 'g1', title: '发布前确认', workflowName: 'w' },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-inbox-records"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('正在执行')
    expect(wrapper.text()).not.toContain('发布前确认')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows run titles, tooltips, and keeps the row link and motion marks', async () => {
    const repos = '[{"name":"approving","url":"https://git.example/approving.git"}]'
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'clarify', label: '需求澄清', kind: 'clarify', state: 'starting', workflowName: 'w', runTitle: '把登录做清楚' },
      { type: 'clarify', runId: 'run-2', nodeId: 'clarify', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '补充空状态文案' },
      { type: 'clarify', runId: 'run-abc', nodeId: 'empty', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '  ' },
      { type: 'clarify', runId: 'run-json', nodeId: 'raw', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '{"foo":1}' },
      { type: 'gate', runId: 'run-repo', nodeId: 'g1', title: '人工门禁', workflowName: 'w', runTitle: repos },
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    const rows = wrapper.findAll('[data-testid="nav-pending-row"]')
    expect(rows.map((row) => row.get('.nav-row-title').text())).toEqual([
      '把登录做清楚',
      '补充空状态文案',
      'w',
      'w',
      'approving',
    ])
    expect(rows[0].text()).not.toContain('需求澄清')
    expect(rows[4].text()).not.toContain('[')
    expect(rows[0].attributes('title')).toBe('把登录做清楚 · 需求澄清')
    expect(rows[2].attributes('title')).toBe('w · 需求澄清')
    expect(rows[4].attributes('title')).toBe('approving · 人工门禁')
    expect(rows[0].attributes('data-motion')).toBeUndefined()
    expect(rows[0].get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('arc-spinner')
    await rows[1].trigger('click')
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-2', node: 'clarify' } })
    expect(enNav.nav.pendingPin).toBe('Pin')
    expect(enNav.nav.pendingUnpin).toBe('Unpin')
    expect(enNav.nav.pendingRename).toBe('Rename')
    expect(enNav.nav.pendingCancelRun).toBe('Cancel run')
    expect(enNav.nav.pendingCancelConfirm).toContain('Running')
    expect(enNav.nav.pendingNameEmpty).toBe('Name cannot be empty')
    expect(enNav.nav.pendingNameTooLong).toBe('80 characters maximum')
    expect(enNav.nav.pendingRenameFailed).toBe('Save failed')
    expect(nav.nav.pendingCancelRun).toBe('取消运行')
    expect(nav.nav.pendingCancelConfirm).toContain('正在执行')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('opens pin and rename from the row menu, and closes it without changing the row', async () => {
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    gateMocks.sidebarExtras.value = [
      { type: 'clarify', runId: 'run-e', nodeId: 'e', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '临时行' },
    ]
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-b', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第二条' },
      { type: 'clarify', runId: 'run-c', nodeId: 'c', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第三条' },
      { type: 'clarify', runId: 'run-b', nodeId: 'other', label: '代码评审', kind: 'review', workflowName: 'w', runTitle: '第二条' },
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    const sourceOrder = () => wrapper.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))
    expect(sourceOrder()).toEqual(['run-e', 'run-b', 'run-c'])

    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 })
    wrapper.get('[data-key="run-c"]').element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    await nextTick()
    const menu = document.querySelector('[data-testid="nav-pending-menu"]')
    expect(menu).toBeTruthy()
    expect(menu?.textContent).toContain('置顶')
    expect(menu?.textContent).toContain('重命名')
    expect(menu?.textContent).toContain('取消运行')
    const firstMenuItems = [...document.querySelectorAll('[data-testid="nav-pending-menu"] [role="menuitem"]')]
    expect(firstMenuItems.at(-1)?.getAttribute('data-testid')).toBe('nav-pending-menu-cancel')
    expect(firstMenuItems.at(-1)?.className).toBe(firstMenuItems[0]?.className)
    expect(firstMenuItems.at(-1)?.className).not.toContain('text-err')
    expect(wrapper.find('[data-testid="nav-pending-rename-input"]').exists()).toBe(false)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-e', 'run-b', 'run-c'])

    wrapper.get('[data-key="run-c"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-e', 'run-b', 'run-c'])

    const stored = gateMocks.displayedItems.value.map((it) => `${it.runId}:${it.nodeId}`)
    wrapper.get('[data-key="run-c"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-c', 'run-e', 'run-b'])
    expect(wrapper.get('[data-key="run-c"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(true)
    expect(wrapper.get('[data-key="run-b"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(false)
    expect(gateMocks.displayedItems.value.map((it) => `${it.runId}:${it.nodeId}`)).toEqual(stored)

    wrapper.get('[data-key="run-b"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('置顶')
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(sourceOrder()).toEqual(['run-b', 'run-c', 'run-e'])

    wrapper.get('[data-key="run-b"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('取消置顶')
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(sourceOrder()).toEqual(['run-c', 'run-e', 'run-b'])
    expect(wrapper.get('[data-key="run-b"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(false)

    wrapper.unmount()
    const again = mountNav(true)
    await flushPromises()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-c',
      'run-e',
      'run-b',
    ])
    useAuth().setUser({ username: 'bob', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-e',
      'run-b',
      'run-c',
    ])
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-d', nodeId: 'd', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '更新的一行' },
      { type: 'clarify', runId: 'run-b', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第二条' },
    ]
    gateMocks.sidebarExtras.value = []
    await flushPromises()
    expect(again.find('[data-testid="nav-pending-row"]').attributes('data-key')).not.toBe('run-c')
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-d', nodeId: 'd', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '更新的一行' },
      { type: 'clarify', runId: 'run-b', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第二条' },
      { type: 'clarify', runId: 'run-c', nodeId: 'c', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第三条' },
    ]
    routeState.path = '/workflows'
    await flushPromises()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-c',
      'run-d',
      'run-b',
    ])
    again.unmount()
    vi.useRealTimers()
  })

  it('renames in place from the menu or F2 and keeps invalid drafts', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'a', label: '需求澄清', kind: 'clarify', state: 'starting', workflowName: 'w', runTitle: '把登录做清楚' },
      { type: 'clarify', runId: 'run-1', nodeId: 'b', label: '代码评审', kind: 'review', workflowName: 'w', runTitle: '把登录做清楚' },
      { type: 'clarify', runId: 'run-2', nodeId: 'a', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '补充空状态文案' },
      { type: 'clarify', runId: 'run-empty', nodeId: 'e', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '' },
    ]
    const wrapper = mountNav(true)
    await flushPromises()

    await wrapper.get('[data-key="run-1"]').trigger('keydown', { key: 'F2' })
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    let input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    expect((input.element as HTMLInputElement).value).toBe('把登录做清楚')
    expect(wrapper.get('[data-key="run-1"]').get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('arc-spinner')
    routerMocks.push.mockClear()
    await wrapper.get('[data-editing="true"]').trigger('click')
    expect(routerMocks.push).not.toHaveBeenCalled()
    wrapper.get('[data-editing="true"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 8, clientY: 8 }),
    )
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()

    await input.trigger('keydown', { key: 'Escape' })
    await nextTick()
    expect(wrapper.find('[data-testid="nav-pending-rename-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-key="run-1"]').get('.nav-row-title').text()).toBe('把登录做清楚')
    expect(apiMocks.updateRunTitle).not.toHaveBeenCalled()

    await wrapper.get('[data-key="run-empty"]').trigger('keydown', { key: 'F2' })
    await nextTick()
    expect((wrapper.get('[data-testid="nav-pending-rename-input"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-testid="nav-pending-rename-input"]').trigger('keydown', { key: 'Escape' })

    wrapper.get('[data-key="run-1"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 16, clientY: 16 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-rename"]') as HTMLButtonElement).click()
    await nextTick()
    input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    expect((input.element as HTMLInputElement).value).toBe('把登录做清楚')

    await input.setValue('   ')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(apiMocks.updateRunTitle).not.toHaveBeenCalled()
    expect((wrapper.get('[data-testid="nav-pending-rename-input"]').element as HTMLInputElement).value).toBe('   ')
    expect(useToast().toasts.value.some((item) => item.message === '名字不能为空')).toBe(true)

    const tooLong = '字'.repeat(81)
    await wrapper.get('[data-testid="nav-pending-rename-input"]').setValue(tooLong)
    await wrapper.get('[data-testid="nav-pending-rename-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(apiMocks.updateRunTitle).not.toHaveBeenCalled()
    expect((wrapper.get('[data-testid="nav-pending-rename-input"]').element as HTMLInputElement).value).toBe(tooLong)
    expect(useToast().toasts.value.some((item) => item.message === '最长 80 字')).toBe(true)

    await wrapper.get('[data-testid="nav-pending-rename-input"]').setValue(`  ${'字'.repeat(80)}  `)
    apiMocks.updateRunTitle.mockResolvedValueOnce({ id: 'run-1', title: '字'.repeat(80) })
    await wrapper.get('[data-testid="nav-pending-rename-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(apiMocks.updateRunTitle).toHaveBeenCalledWith('run-1', '字'.repeat(80))
    expect(wrapper.findAll('.nav-row-title').map((node) => node.text()).filter((text) => text === '字'.repeat(80))).toHaveLength(1)
    expect(wrapper.get('[data-key="run-2"]').get('.nav-row-title').text()).toBe('补充空状态文案')

    wrapper.get('[data-key="run-2"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 16, clientY: 16 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-rename"]') as HTMLButtonElement).click()
    await nextTick()
    input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    await input.setValue('  补充空状态文案  ')
    await input.trigger('blur')
    document.body.tabIndex = -1
    document.body.focus()
    await vi.advanceTimersByTimeAsync(0)
    expect(apiMocks.updateRunTitle).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="nav-pending-rename-input"]').exists()).toBe(false)

    wrapper.get('[data-key="run-2"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 16, clientY: 16 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-rename"]') as HTMLButtonElement).click()
    await nextTick()
    input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    await input.setValue('空状态短名')
    apiMocks.updateRunTitle.mockResolvedValueOnce({ id: 'run-2', title: '空状态短名' })
    await input.trigger('blur')
    document.body.focus()
    await vi.advanceTimersByTimeAsync(0)
    await flushPromises()
    expect(apiMocks.updateRunTitle).toHaveBeenCalledWith('run-2', '空状态短名')
    expect(wrapper.get('[data-key="run-2"]').get('.nav-row-title').text()).toBe('空状态短名')

    wrapper.get('[data-key="run-2"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 16, clientY: 16 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-rename"]') as HTMLButtonElement).click()
    await nextTick()
    input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    await input.setValue('保存失败的名字')
    apiMocks.updateRunTitle.mockRejectedValueOnce(new Error('offline'))
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[data-testid="nav-pending-rename-input"]').element).toBeTruthy()
    expect((wrapper.get('[data-testid="nav-pending-rename-input"]').element as HTMLInputElement).value).toBe('保存失败的名字')
    expect(useToast().toasts.value.some((item) => item.message === '保存失败')).toBe(true)
    expect(routerMocks.push).not.toHaveBeenCalled()
    expect(wrapper.get('[data-key="run-2"]').find('.nav-row-title').exists()).toBe(false)

    gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.nodeId !== 'a' || it.runId !== 'run-2')
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-pending-rename-input"]').exists()).toBe(false)
    expect(apiMocks.updateRunTitle).toHaveBeenCalledTimes(3)

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('ignores a second submit while the first rename is in flight', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'a', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '把登录做清楚' },
    ]
    let resolveSave: (value: { id: string; title: string }) => void = () => {}
    apiMocks.updateRunTitle.mockImplementation(
      () => new Promise((resolve) => {
        resolveSave = resolve
      }),
    )
    const wrapper = mountNav(true)
    await flushPromises()
    await wrapper.get('[data-testid="nav-pending-row"]').trigger('keydown', { key: 'F2' })
    await nextTick()
    const input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    await input.setValue('新名字')
    const pending = input.trigger('keydown', { key: 'Enter' })
    await Promise.resolve()
    await input.trigger('keydown', { key: 'Enter' })
    expect(apiMocks.updateRunTitle).toHaveBeenCalledTimes(1)
    resolveSave({ id: 'run-1', title: '新名字' })
    await pending
    await flushPromises()
    expect(wrapper.get('.nav-row-title').text()).toBe('新名字')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('opens the same menu on a touch long-press and skips it when the finger moves', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'a', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '把登录做清楚' },
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    const row = wrapper.get('[data-testid="nav-pending-row"]')
    await row.trigger('pointerdown', { pointerType: 'touch', clientX: 10, clientY: 12 })
    await vi.advanceTimersByTimeAsync(499)
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('重命名')
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('取消运行')
    const longPressItems = [...document.querySelectorAll('[data-testid="nav-pending-menu"] [role="menuitem"]')]
    expect(longPressItems.at(-1)?.getAttribute('data-testid')).toBe('nav-pending-menu-cancel')
    routerMocks.push.mockClear()
    await row.trigger('click')
    expect(routerMocks.push).not.toHaveBeenCalled()
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()

    await row.trigger('pointerdown', { pointerType: 'touch', clientX: 10, clientY: 12 })
    await row.trigger('pointermove', { pointerType: 'touch', clientX: 30, clientY: 12 })
    await vi.advanceTimersByTimeAsync(500)
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()

    await row.trigger('pointerdown', { pointerType: 'touch', clientX: 10, clientY: 12 })
    await vi.advanceTimersByTimeAsync(200)
    await row.trigger('pointerup', { pointerType: 'touch', clientX: 10, clientY: 12 })
    await vi.advanceTimersByTimeAsync(400)
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()

    await row.trigger('pointerdown', { pointerType: 'mouse', clientX: 10, clientY: 12 })
    await vi.advanceTimersByTimeAsync(600)
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('adds share approval after pin and rename, and only navigates with a one-shot intent', async () => {
    expect(enNav.nav.pendingShareStarting).toBe('Starting')
    expect(enNav.nav.pendingShareLocked).toBe('Processing')
    expect(enNav.nav.pendingShareUsed).toBe('Link already used')
    expect(enNav.nav.pendingShareBlocked).toBe('Share approval ({reason})')
    gateMocks.displayedItems.value = [
      {
        type: 'gate',
        nodeType: 'human_gate',
        runId: 'run-g',
        nodeId: 'g',
        title: '人工门禁',
        workflowName: 'w',
        runTitle: '可分享门禁',
      },
      {
        type: 'clarify',
        kind: 'review',
        runId: 'run-r',
        nodeId: 'r',
        label: '代码评审',
        workflowName: 'w',
        runTitle: '可分享复审',
        shareLink: { state: 'revoked' },
      },
      {
        type: 'clarify',
        kind: 'clarify',
        runId: 'run-c',
        nodeId: 'c',
        label: '需求澄清',
        workflowName: 'w',
        runTitle: '可分享澄清',
        shareLink: { state: 'expired' },
      },
      {
        type: 'gate',
        nodeType: 'agent',
        runId: 'run-x',
        nodeId: 'x',
        title: '其他',
        workflowName: 'w',
        runTitle: '不可分享',
      },
      {
        type: 'clarify',
        kind: 'clarify',
        state: 'starting',
        runId: 'run-s',
        nodeId: 's',
        label: '需求澄清',
        workflowName: 'w',
        runTitle: '启动中',
      },
      {
        type: 'gate',
        nodeType: 'human_gate',
        runId: 'run-u',
        nodeId: 'u',
        title: '人工门禁',
        workflowName: 'w',
        runTitle: '已使用',
        shareLink: { state: 'used' },
      },
    ]
    const wrapper = mountNav(true)
    await flushPromises()

    const openRow = (key: string, y = 30) => {
      wrapper.get(`[data-key="${key}"]`).element.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: y }),
      )
    }

    openRow('run-g')
    await nextTick()
    const labels = [...document.querySelectorAll('[data-testid="nav-pending-menu"] [role="menuitem"]')].map(
      (el) => el.getAttribute('data-testid'),
    )
    expect(labels).toEqual([
      'nav-pending-menu-pin',
      'nav-pending-menu-rename',
      'nav-pending-menu-share',
      'nav-pending-menu-cancel',
    ])
    const share = document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement
    expect(share.textContent).toContain('分享审批')
    expect(share.disabled).toBe(false)
    expect(share.title).toBe('分享审批')

    routerMocks.push.mockClear()
    await wrapper.get('[data-key="run-g"]').trigger('click')
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-g', node: 'g' } })

    openRow('run-g')
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement).click()
    await nextTick()
    expect(routerMocks.push).toHaveBeenLastCalledWith({
      path: '/gates',
      query: { run: 'run-g', node: 'g', share: '1' },
    })
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()

    openRow('run-r')
    await nextTick()
    expect((document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement).disabled).toBe(false)
    openRow('run-c')
    await nextTick()
    expect((document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement).disabled).toBe(false)

    openRow('run-x')
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu-share"]')).toBeNull()
    expect(document.querySelector('[data-testid="nav-pending-menu-pin"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="nav-pending-menu-rename"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="nav-pending-menu-cancel"]')).toBeTruthy()

    routerMocks.push.mockClear()
    openRow('run-s')
    await nextTick()
    const starting = document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement
    expect(starting.disabled).toBe(true)
    expect(starting.textContent).toContain('分享审批（启动中）')
    expect(starting.title).toBe('分享审批（启动中）')
    expect(starting.getAttribute('aria-label')).toBe('分享审批（启动中）')
    expect(starting.getAttribute('data-reason')).toBe('starting')
    starting.click()
    expect(routerMocks.push).not.toHaveBeenCalled()

    openRow('run-u')
    await nextTick()
    const used = document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement
    expect(used.disabled).toBe(true)
    expect(used.textContent).toContain('分享审批（链接已使用）')
    expect(used.title).toBe('分享审批（链接已使用）')
    used.click()
    expect(routerMocks.push).not.toHaveBeenCalled()

    const previousWidth = window.innerWidth
    window.innerWidth = 360
    wrapper.get('[data-key="run-u"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 340, clientY: 40 }),
    )
    await nextTick()
    const wideMenu = document.querySelector('[data-testid="nav-pending-menu"]') as HTMLElement
    expect(wideMenu.style.left).toBe('80px')
    window.innerWidth = previousWidth

    knownInboxProcessingLock.value = true
    openRow('run-g')
    await nextTick()
    const locked = document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement
    expect(locked.disabled).toBe(true)
    expect(locked.textContent).toContain('分享审批（正在处理）')
    expect(locked.title).toBe('分享审批（正在处理）')
    locked.click()
    expect(routerMocks.push).not.toHaveBeenCalled()
    knownInboxProcessingLock.value = false

    const row = wrapper.get('[data-key="run-g"]')
    await row.trigger('pointerdown', { pointerType: 'touch', clientX: 12, clientY: 40 })
    await vi.advanceTimersByTimeAsync(500)
    expect(document.querySelector('[data-testid="nav-pending-menu-share"]')).toBeTruthy()

    expect(window.innerHeight).toBeGreaterThan(200)
    openRow('run-g', window.innerHeight - 4)
    await nextTick()
    const clamped = document.querySelector('[data-testid="nav-pending-menu"]') as HTMLElement
    expect(clamped.style.top).toBe(`${window.innerHeight - 136}px`)
    openRow('run-x', window.innerHeight - 4)
    await nextTick()
    const shortMenu = document.querySelector('[data-testid="nav-pending-menu"]') as HTMLElement
    expect(shortMenu.style.top).toBe(`${window.innerHeight - 104}px`)

    wrapper.unmount()
    vi.useRealTimers()
  })
})

describe('AppSidebarNav cancel run', () => {
  function openCancelMenu(wrapper: ReturnType<typeof mountNav>, key = 'run-1') {
    wrapper.get(`[data-key="${key}"]`).element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
  }

  it('opens a themed confirm first, dismisses without calling cancel, then cancels and refreshes', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'a', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '把登录做清楚' },
      { type: 'clarify', runId: 'run-2', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '另一条' },
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="nav-pending-row"]')).toHaveLength(2)

    openCancelMenu(wrapper)
    await nextTick()
    const cancelItem = document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement
    expect(cancelItem.textContent).toContain('取消运行')
    expect(cancelItem.className).toContain('pending-row-menu__item')
    expect(cancelItem.className).not.toContain('text-err')
    cancelItem.click()
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(apiMocks.cancelRun).not.toHaveBeenCalled()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('取消运行？')
    expect(dialog?.textContent).toContain('不可恢复')
    expect(dialog?.textContent).toContain('正在执行')

    ;(document.querySelector('[data-testid="nav-pending-cancel-dismiss"]') as HTMLButtonElement).click()
    await nextTick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(apiMocks.cancelRun).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="nav-pending-row"]')).toHaveLength(2)

    openCancelMenu(wrapper)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    document.querySelector('[role="dialog"]')?.parentElement?.querySelector('.absolute.inset-0')?.dispatchEvent(
      new MouseEvent('click', { bubbles: true }),
    )
    await nextTick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(apiMocks.cancelRun).not.toHaveBeenCalled()

    openCancelMenu(wrapper)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    apiMocks.listRuns.mockImplementation(() => {
      const items = unfinishedRunsFromInbox().filter((item) => item.id !== 'run-1')
      return Promise.resolve({ items, total: items.length })
    })
    const listCalls = apiMocks.listRuns.mock.calls.length
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(apiMocks.cancelRun).toHaveBeenCalledTimes(1)
    expect(apiMocks.cancelRun).toHaveBeenCalledWith('run-1')
    expect(apiMocks.listRuns.mock.calls.length).toBeGreaterThan(listCalls)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(useToast().toasts.value.some((item) => item.message === '已取消运行')).toBe(true)
    expect(wrapper.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual(['run-2'])
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps cancel available when share is blocked, and maps finished or missing errors', async () => {
    gateMocks.displayedItems.value = [
      {
        type: 'clarify',
        kind: 'clarify',
        state: 'starting',
        runId: 'run-s',
        nodeId: 's',
        label: '需求澄清',
        workflowName: 'w',
        runTitle: '启动中',
      },
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    openCancelMenu(wrapper, 'run-s')
    await nextTick()
    const share = document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement
    expect(share.disabled).toBe(true)
    const cancelItem = document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement
    expect(cancelItem.disabled).toBe(false)
    cancelItem.click()
    await nextTick()

    apiMocks.cancelRun.mockRejectedValueOnce(Object.assign(new Error('already finished'), { status: 400 }))
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.querySelector('[data-testid="nav-pending-cancel-error"]')?.textContent).toContain('当前状态不可取消')
    expect(document.querySelector('[role="dialog"]')).toBeTruthy()
    expect(useToast().toasts.value.some((item) => item.message === '已取消运行')).toBe(false)
    expect(wrapper.find('[data-key="run-s"]').exists()).toBe(true)

    apiMocks.cancelRun.mockRejectedValueOnce(Object.assign(new Error('not found'), { status: 404 }))
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.querySelector('[data-testid="nav-pending-cancel-error"]')?.textContent).toContain('运行不存在或已删除')
    expect(document.querySelector('[role="dialog"]')).toBeTruthy()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('blocks a second confirm and close while cancel is in flight', async () => {
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-1', nodeId: 'a', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '把登录做清楚' },
    ]
    let resolveCancel: (value: { status: string }) => void = () => {}
    apiMocks.cancelRun.mockImplementation(
      () => new Promise((resolve) => {
        resolveCancel = resolve
      }),
    )
    const wrapper = mountNav(true)
    await flushPromises()
    openCancelMenu(wrapper)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    const confirm = document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement
    const dismiss = document.querySelector('[data-testid="nav-pending-cancel-dismiss"]') as HTMLButtonElement
    confirm.click()
    await Promise.resolve()
    expect(confirm.disabled).toBe(true)
    expect(dismiss.disabled).toBe(true)
    expect(confirm.textContent).toContain('取消中')
    confirm.click()
    dismiss.click()
    document.querySelector('[role="dialog"]')?.parentElement?.querySelector('button')?.dispatchEvent(
      new MouseEvent('click', { bubbles: true }),
    )
    await nextTick()
    expect(apiMocks.cancelRun).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[role="dialog"]')).toBeTruthy()
    resolveCancel({ status: 'cancelled' })
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    wrapper.unmount()
    vi.useRealTimers()
  })

  function pendingClarify(runId: string, nodeId: string, runTitle: string) {
    return {
      type: 'clarify',
      runId,
      nodeId,
      label: '需求澄清',
      kind: 'clarify',
      workflowName: 'w',
      runTitle,
    }
  }

  async function confirmFromMenu(wrapper: ReturnType<typeof mountNav>, key = 'run-1') {
    openCancelMenu(wrapper, key)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await flushPromises()
  }

  it('opens the new-request page when cancelling the inbox conversation on screen', async () => {
    routeState.path = '/gates'
    routeState.query = { run: 'run-1', node: 'a' }
    gateMocks.displayedItems.value = [
      pendingClarify('run-1', 'a', '把登录做清楚'),
      pendingClarify('run-2', 'b', '另一条'),
    ]
    let refreshAfterLeave = false
    let leftBeforeRefreshSettled = false
    apiMocks.listRuns.mockImplementation(() => {
      if (refreshAfterLeave && !leftBeforeRefreshSettled) {
        leftBeforeRefreshSettled =
          routerMocks.push.mock.calls.length === 1 && inboxCancelDepartureRunId() === 'run-1'
      }
      const items = unfinishedRunsFromInbox().filter((item) => !refreshAfterLeave || item.id !== 'run-1')
      return Promise.resolve({ items, total: items.length })
    })
    const wrapper = mountNav(true)
    await flushPromises()
    refreshAfterLeave = true
    await confirmFromMenu(wrapper)
    expect(leftBeforeRefreshSettled).toBe(true)
    expect(routerMocks.push).toHaveBeenCalledTimes(1)
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/dashboard' })
    expect(routerMocks.push.mock.calls[0]?.[0]).not.toHaveProperty('query')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(useToast().toasts.value.some((item) => item.message === '已取消运行')).toBe(true)
    expect(wrapper.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual(['run-2'])
    expect(inboxCancelDepartureRunId()).toBeNull()
    expect(wrapper.text()).not.toContain('没有待审批项')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('treats a query-less inbox and an array run query as the open conversation', async () => {
    gateMocks.activeInboxKey.value = 'run-1:a'
    routeState.path = '/gates'
    routeState.query = {}
    gateMocks.displayedItems.value = [pendingClarify('run-1', 'a', '把登录做清楚')]
    const wrapper = mountNav(true)
    await flushPromises()
    await confirmFromMenu(wrapper)
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/dashboard' })

    routerMocks.push.mockClear()
    routeState.query = { run: ['run-1'] }
    gateMocks.activeInboxKey.value = null
    gateMocks.displayedItems.value = [pendingClarify('run-1', 'a', '把登录做清楚')]
    await flushPromises()
    await confirmFromMenu(wrapper)
    expect(routerMocks.push).toHaveBeenCalledTimes(1)
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/dashboard' })
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('stays on the conversation and keeps the existing error when cancel fails', async () => {
    routeState.path = '/gates'
    routeState.query = { run: 'run-1', node: 'a' }
    gateMocks.displayedItems.value = [pendingClarify('run-1', 'a', '把登录做清楚')]
    const wrapper = mountNav(true)
    await flushPromises()
    openCancelMenu(wrapper)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    apiMocks.cancelRun.mockRejectedValueOnce(Object.assign(new Error('already finished'), { status: 400 }))
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(routerMocks.push).not.toHaveBeenCalled()
    expect(routeState.path).toBe('/gates')
    expect(routeState.query).toEqual({ run: 'run-1', node: 'a' })
    expect(document.querySelector('[data-testid="nav-pending-cancel-error"]')?.textContent).toContain('当前状态不可取消')
    expect(document.querySelector('[role="dialog"]')).toBeTruthy()
    expect(inboxCancelDepartureRunId()).toBeNull()
    expect(wrapper.find('[data-key="run-1"]').exists()).toBe(true)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('does not change the page when the cancelled run is not the open inbox conversation', async () => {
    gateMocks.displayedItems.value = [
      pendingClarify('run-1', 'a', '把登录做清楚'),
      pendingClarify('run-2', 'b', '另一条'),
    ]
    const wrapper = mountNav(true)
    await flushPromises()
    const pages: Array<{ path: string; query: Record<string, unknown> }> = [
      { path: '/dashboard', query: {} },
      { path: '/board', query: {} },
      { path: '/runs', query: {} },
      { path: '/runs/run-1', query: {} },
      { path: '/runs/run-1/live', query: { node: 'a' } },
      { path: '/gates', query: { run: 'run-2', node: 'b' } },
    ]
    for (const page of pages) {
      routerMocks.push.mockClear()
      routeState.path = page.path
      routeState.query = { ...page.query }
      await nextTick()
      await confirmFromMenu(wrapper, 'run-1')
      expect(routerMocks.push).not.toHaveBeenCalled()
      expect(routeState.path).toBe(page.path)
      expect(routeState.query).toEqual(page.query)
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    }
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('holds the open conversation until the leave finishes, and does not navigate before success', async () => {
    routeState.path = '/gates'
    routeState.query = { run: 'run-1', node: 'a' }
    gateMocks.displayedItems.value = [
      pendingClarify('run-1', 'a', '把登录做清楚'),
      pendingClarify('run-2', 'b', '另一条'),
    ]
    let resolveCancel: (value: { status: string }) => void = () => {}
    apiMocks.cancelRun.mockImplementation(
      () => new Promise((resolve) => {
        resolveCancel = resolve
      }),
    )
    const wrapper = mountNav(true)
    await flushPromises()
    openCancelMenu(wrapper)
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-cancel-confirm"]') as HTMLButtonElement).click()
    await Promise.resolve()
    expect(inboxCancelDepartureRunId()).toBe('run-1')
    expect(routerMocks.push).not.toHaveBeenCalled()
    resolveCancel({ status: 'cancelled' })
    await flushPromises()
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/dashboard' })
    expect(inboxCancelDepartureRunId()).toBeNull()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows 正在执行 once the workspace becomes ready, without leaving the page', async () => {
    const run = {
      id: 'run-live',
      workflowId: 'wf',
      workflowName: 'w',
      title: '登录还在跑',
      status: 'running',
      startedAt: 'T0',
    }
    const solo = {
      id: 'solo-1',
      title: '单独会话',
      agentName: '需求澄清',
      status: 'running',
      restarts: 0,
      busy: true,
      waiting: 0,
      createdAt: '2026-10-23T00:00:00Z',
      updatedAt: '2026-10-23T00:00:00Z',
    }
    apiMocks.listSoloSessions.mockResolvedValue({ sessions: [], maxSandboxes: 0 })
    await useSoloSessions().refresh()
    apiMocks.listSoloSessions.mockClear()
    workspaceMocks.workspace.value = null
    workspaceMocks.ensureLoaded.mockReset()
    workspaceMocks.ensureLoaded.mockResolvedValueOnce(null)
    workspaceMocks.ensureLoaded.mockResolvedValue({ name: 'ws' })
    apiMocks.listRuns.mockResolvedValue({ items: [run], total: 1 })
    apiMocks.listSoloSessions.mockResolvedValue({ sessions: [solo], maxSandboxes: 1 })

    const wrapper = mountNav()
    await flushPromises()
    expect(routeState.path).toBe('/dashboard')
    expect(apiMocks.listRuns).not.toHaveBeenCalled()
    expect(apiMocks.listSoloSessions).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('正在执行')

    workspaceMocks.workspace.value = { name: 'ws' }
    await nextTick()
    await flushPromises()

    expect(routeState.path).toBe('/dashboard')
    expect(apiMocks.listRuns).toHaveBeenCalledTimes(1)
    expect(apiMocks.listRuns).toHaveBeenCalledWith(expect.objectContaining({
      status: 'queued,running,waiting_human',
      sort: 'last_activity',
      order: 'desc',
    }))
    expect(apiMocks.listSoloSessions).toHaveBeenCalledTimes(1)
    const section = wrapper.get('[data-testid="nav-pending-approvals"]')
    expect(section.text()).toContain('正在执行')
    expect(section.get('[data-key="run-live"]').text()).toContain('登录还在跑')
    expect(section.get('[data-kind="solo"]').attributes('data-key')).toBe('solo-1')
    const create = wrapper.get('[data-to="/dashboard"]')
    expect(create.text()).toContain('新需求')
    expect(create.element.compareDocumentPosition(section.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('reads unfinished runs again when a hidden tab becomes visible', async () => {
    const wrapper = mountNav()
    await flushPromises()
    const callsAfterMount = apiMocks.listRuns.mock.calls.length
    expect(callsAfterMount).toBeGreaterThan(0)

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(apiMocks.listRuns.mock.calls.length).toBe(callsAfterMount)

    apiMocks.listRuns.mockResolvedValue({
      items: [{ id: 'run-back', workflowId: 'wf', workflowName: 'w', title: '切回来了', status: 'queued', startedAt: 'T0' }],
      total: 1,
    })
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    expect(apiMocks.listRuns.mock.calls.length).toBe(callsAfterMount + 1)
    expect(routeState.path).toBe('/dashboard')
    expect(wrapper.get('[data-key="run-back"]').text()).toContain('切回来了')
    expect(wrapper.get('[data-testid="nav-pending-approvals"]').text()).toContain('正在执行')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps a landed unfinished list when activity notices arrive before the read returns', async () => {
    const landed = {
      items: [{ id: 'run-live', workflowId: 'wf', workflowName: 'w', title: '登录还在跑', status: 'running', startedAt: 'T0' }],
      total: 1,
    }
    const pending: Array<{ resolve: (value: unknown) => void; reject: (reason: unknown) => void; signal?: AbortSignal }> = []
    apiMocks.listRuns.mockImplementation((args?: { signal?: AbortSignal }) => new Promise((resolve, reject) => {
      pending.push({ resolve, reject, signal: args?.signal })
    }))

    const wrapper = mountNav()
    await flushPromises()
    expect(pending).toHaveLength(1)
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)

    notifyRecentActivity()
    await vi.advanceTimersByTimeAsync(RECENT_RELAY_DEBOUNCE_MS)
    notifyRecentActivity()
    await vi.advanceTimersByTimeAsync(RECENT_RELAY_DEBOUNCE_MS)
    expect(pending).toHaveLength(1)
    expect(pending[0].signal?.aborted).toBe(false)

    pending[0].resolve(landed)
    await flushPromises()
    expect(wrapper.get('[data-key="run-live"]').text()).toContain('登录还在跑')
    expect(pending).toHaveLength(2)
    expect(pending[0].signal?.aborted).toBe(false)

    notifyRecentActivity()
    await vi.advanceTimersByTimeAsync(RECENT_RELAY_DEBOUNCE_MS)
    notifyRecentActivity()
    await vi.advanceTimersByTimeAsync(RECENT_RELAY_DEBOUNCE_MS)
    expect(pending).toHaveLength(2)
    expect(pending[1].signal?.aborted).toBe(false)
    expect(wrapper.get('[data-key="run-live"]').text()).toContain('登录还在跑')

    pending[1].reject(new Error('offline'))
    await flushPromises()
    expect(wrapper.get('[data-key="run-live"]').text()).toContain('登录还在跑')
    expect(wrapper.get('[data-testid="nav-pending-approvals"]').text()).toContain('正在执行')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides 正在执行 when nothing is unfinished and on the settings nav', async () => {
    apiMocks.listRuns.mockResolvedValue({ items: [], total: 0 })
    apiMocks.listSoloSessions.mockResolvedValue({ sessions: [], maxSandboxes: 0 })
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-to="/dashboard"]').text()).toContain('新需求')
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('正在执行')
    wrapper.unmount()

    routeState.path = '/settings'
    apiMocks.listRuns.mockResolvedValue({
      items: [{ id: 'run-1', workflowId: 'wf', workflowName: 'w', title: '还在跑', status: 'running', startedAt: 'T0' }],
      total: 1,
    })
    const settings = mountNav()
    await flushPromises()
    expect(settings.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(settings.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    expect(settings.text()).not.toContain('正在执行')
    expect(settings.text()).not.toContain('还在跑')
    settings.unmount()
    vi.useRealTimers()
  })
})

describe('pending rows leave the artifact panel switch alone', () => {
  it('does not rewrite the session switch when opening a pending row', async () => {
    writeArtifactPanelOpen(true)
    gateMocks.displayedItems.value = [
      { type: 'gate', runId: 'run-1', nodeId: 'g1', title: '发布前确认', runTitle: '发布前确认', workflowName: 'w' },
      { type: 'clarify', runId: 'run-2', nodeId: 'c1', label: '需求规格', runTitle: '需求规格', kind: 'clarify', workflowName: 'w' },
    ]
    const wrapper = mountNav()
    await flushPromises()
    const rows = wrapper.get('[data-testid="nav-pending-approvals"]').findAll('[data-testid="nav-pending-row"]')
    await rows[1].trigger('click')
    await rows[0].trigger('click')
    expect(readArtifactPanelOpen()).toBe(true)
    writeArtifactPanelOpen(false)
    await rows[1].trigger('click')
    expect(readArtifactPanelOpen()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })
})

describe('自动过程 grouping and flight', () => {
  const auto = (id: string, name: string, state: 'running' | 'stopping' = 'running') => ({
    id, name, iteration: 1, kind: 'agent', mode: 'auto', state,
  })
  const run = (id: string, title: string, status: string, currentNode?: Record<string, unknown>) => ({
    id, workflowId: 'wf', workflowName: 'w', title, status, startedAt: 'T0', ...(currentNode ? { currentNode } : {}),
  })
  const clarify = (runId: string, nodeId: string, runTitle: string) => ({
    type: 'clarify' as const, runId, nodeId, label: nodeId, kind: 'clarify', workflowName: 'w', runTitle,
  })

  async function show(items: unknown[]) {
    apiMocks.listRuns.mockResolvedValue({ items, total: items.length })
    const wrapper = mountNav(true)
    await flushPromises()
    return wrapper
  }

  it('splits waiting, queued and Solo from automatic runs, and hides an empty group', async () => {
    apiMocks.listSoloSessions.mockResolvedValue({
      sessions: [{ id: 'solo-1', title: '单独', agentName: '需求澄清', status: 'running', restarts: 0, busy: false, waiting: 0, createdAt: 'T', updatedAt: 'T' }],
      maxSandboxes: 1,
    })
    const wrapper = await show([
      run('wait', '等人', 'waiting_human', { id: 'gate', name: '审批', iteration: 1, kind: 'gate', mode: 'manual', state: 'waiting_human' }),
      run('queue', '排队', 'queued'),
      run('boot', '启动', 'running', { id: 'clarify', name: '澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'running' }),
      run('take', '接管', 'running', { id: 'impl', name: '实现', iteration: 1, kind: 'takeover', mode: 'manual', state: 'stopping' }),
      run('go', '自动', 'running', auto('impl', '实现')),
      run('stop', '停止中', 'running', auto('test', '测试', 'stopping')),
      run('blank', '无节点名', 'running', auto('x', '   ')),
    ])
    const running = wrapper.get('[data-testid="nav-running-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))
    const automatic = wrapper.get('[data-testid="nav-auto-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))
    expect(running).toEqual(['wait', 'queue', 'boot', 'take', 'solo-1'])
    expect(automatic).toEqual(['go', 'stop', 'blank'])
    expect(new Set([...running, ...automatic]).size).toBe(running.length + automatic.length)
    expect(wrapper.get('[data-testid="nav-pending-approvals"] .sidebar-section-count').text()).toBe('5')
    expect(wrapper.get('[data-testid="nav-auto-process"] .sidebar-section-count').text()).toBe('3')
    expect(wrapper.get('[data-key="go"]').get('[data-testid="nav-auto-node-name"]').text()).toBe('实现')
    expect(wrapper.get('[data-key="stop"]').get('[data-testid="nav-auto-node-name"]').text()).toBe('测试')
    expect(wrapper.get('[data-key="blank"]').find('[data-testid="nav-auto-node-name"]').exists()).toBe(false)
    expect(wrapper.get('[data-key="go"]').attributes('data-flight')).toBeUndefined()
    const headings = wrapper.findAll('[data-testid="nav-running-heading"], [data-testid="nav-auto-heading"]').map((el) => el.attributes('data-testid'))
    expect(headings).toEqual(['nav-running-heading', 'nav-auto-heading'])
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('flies in only after this handoff is confirmed automatic', async () => {
    gateMocks.displayedItems.value = [clarify('run-x', 'clarify', '登录')]
    const wrapper = mountNav(true)
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-auto-process"]').exists()).toBe(false)

    armSidebarHandoff('run-x', 'clarify')
    await flushPromises()
    expect(wrapper.get('[data-key="run-x"]').attributes('data-section')).toBe('running')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBeUndefined()

    apiMocks.listRuns.mockResolvedValue({
      items: [run('run-x', '登录', 'running', auto('impl', '实现'))],
      total: 1,
    })
    gateMocks.displayedItems.value = []
    await flushPromises()
    const row = wrapper.get('[data-key="run-x"]')
    expect(row.attributes('data-section')).toBe('auto')
    expect(row.attributes('data-flight')).toBe('in')
    expect(row.attributes('data-flight-landing')).toBe('row')
    expect(wrapper.get('[data-testid="nav-auto-process"] .sidebar-section-count').text()).toBe('1')
    expect(wrapper.find('[data-testid="nav-pending-approvals"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('does not fly in when the next node is a person or the run ends', async () => {
    gateMocks.displayedItems.value = [clarify('run-h', 'clarify', '人工'), clarify('run-e', 'clarify', '结束')]
    const wrapper = mountNav(true)
    await flushPromises()
    armSidebarHandoff('run-h', 'clarify')
    armSidebarHandoff('run-e', 'clarify')
    apiMocks.listRuns.mockResolvedValue({
      items: [run('run-h', '人工', 'waiting_human', { id: 'gate', name: '审批', iteration: 1, kind: 'gate', mode: 'manual', state: 'waiting_human' })],
      total: 1,
    })
    gateMocks.displayedItems.value = []
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-auto-process"]').exists()).toBe(false)
    expect(wrapper.get('[data-key="run-h"]').attributes('data-section')).toBe('running')
    expect(wrapper.get('[data-key="run-h"]').attributes('data-flight')).toBeUndefined()
    expect(wrapper.find('[data-key="run-e"]').exists()).toBe(false)
    expect(document.querySelector('[data-flight="in"]')).toBeNull()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('places a run that is already automatic on first paint, and flies back when it waits again', async () => {
    const wrapper = await show([run('run-x', '登录', 'running', auto('impl', '实现'))])
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBeUndefined()
    expect(wrapper.get('[data-key="run-x"]').attributes('data-section')).toBe('auto')

    apiMocks.listRuns.mockResolvedValue({
      items: [run('run-x', '登录', 'waiting_human', { id: 'gate', name: '审批', iteration: 1, kind: 'gate', mode: 'manual', state: 'waiting_human' })],
      total: 1,
    })
    routeState.path = '/board'
    await flushPromises()
    expect(wrapper.get('[data-key="run-x"]').attributes('data-section')).toBe('running')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBe('back')
    expect(wrapper.get('[data-key="run-x"]').get('[data-testid="nav-pending-type-icon"]').attributes('name')).toBe('user-hold')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('renames the node when one automatic step follows another', async () => {
    const wrapper = await show([run('run-x', '登录', 'running', auto('impl', '实现'))])
    apiMocks.listRuns.mockResolvedValue({
      items: [run('run-x', '登录', 'running', auto('test', '测试'))],
      total: 1,
    })
    routeState.path = '/runs'
    await flushPromises()
    expect(wrapper.get('[data-key="run-x"]').attributes('data-section')).toBe('auto')
    expect(wrapper.get('[data-testid="nav-auto-node-name"]').text()).toBe('测试')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBeUndefined()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('drops the move when reduced motion is on', async () => {
    const original = window.matchMedia
    window.matchMedia = ((query: string) => ({
      matches: query.includes('reduce'),
      media: query,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent() { return false },
      onchange: null,
    })) as typeof window.matchMedia
    gateMocks.displayedItems.value = [clarify('run-x', 'clarify', '登录')]
    const wrapper = mountNav(true)
    await flushPromises()
    armSidebarHandoff('run-x', 'clarify')
    apiMocks.listRuns.mockResolvedValue({
      items: [run('run-x', '登录', 'running', auto('impl', '实现'))],
      total: 1,
    })
    gateMocks.displayedItems.value = []
    await flushPromises()
    expect(wrapper.get('[data-key="run-x"]').attributes('data-section')).toBe('auto')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBeUndefined()
    expect(SIDEBAR_FLIGHT_MS).toBeLessThanOrEqual(600)
    window.matchMedia = original
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('flies toward the heading and updates the count when the target group is folded', async () => {
    toggleSection('auto')
    gateMocks.displayedItems.value = [clarify('run-x', 'clarify', '登录'), clarify('run-y', 'clarify', '另一个')]
    const wrapper = mountNav(true)
    await flushPromises()
    armSidebarHandoff('run-x', 'clarify')
    apiMocks.listRuns.mockResolvedValue({
      items: [
        run('run-x', '登录', 'running', auto('impl', '实现')),
        run('run-y', '另一个', 'waiting_human'),
      ],
      total: 2,
    })
    gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.runId !== 'run-x')
    await flushPromises()
    expect(wrapper.get('[data-testid="nav-auto-process"] .sidebar-section-count').text()).toBe('1')
    expect(wrapper.get('[data-testid="nav-pending-approvals"] .sidebar-section-count').text()).toBe('1')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBe('in')
    expect(wrapper.get('[data-key="run-x"]').attributes('data-flight-landing')).toBe('heading')
    expect(wrapper.get('[data-testid="nav-auto-heading"]').attributes('data-flight-target')).toBe('true')
    expect(wrapper.get('[data-testid="nav-auto-rows"]').isVisible()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  function layoutBox(left: number, top: number, width: number, height: number): DOMRect {
    return {
      x: left,
      y: top,
      left,
      top,
      width,
      height,
      right: left + width,
      bottom: top + height,
      toJSON() { return this },
    } as DOMRect
  }

  /** happy-dom reports empty boxes, so give rows a layout and step animation frames by hand. */
  function stubFlightGeometry() {
    const original = Element.prototype.getBoundingClientRect
    const spy = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function () {
      const el = this as HTMLElement
      const testId = el.dataset?.testid
      if (testId === 'nav-flight-ghost') {
        const top = Number.parseFloat(el.style.top) || 0
        const left = Number.parseFloat(el.style.left) || 0
        const width = Number.parseFloat(el.style.width) || 0
        const height = Number.parseFloat(el.style.height) || 0
        const match = /translate3d\(\s*([-\d.]+)px,\s*([-\d.]+)px/.exec(el.style.transform || '')
        const dx = match ? Number(match[1]) : 0
        const dy = match ? Number(match[2]) : 0
        return layoutBox(left + dx, top + dy, width, height)
      }
      if (testId === 'nav-pending-row' && el.dataset.kind !== 'solo') {
        if (el.dataset.section === 'auto') return layoutBox(40, 344, 220, 32)
        if (el.dataset.section === 'running') return layoutBox(40, 174, 220, 32)
      }
      if (testId === 'nav-auto-heading') return layoutBox(12, 312, 220, 28)
      if (testId === 'nav-running-heading') return layoutBox(12, 140, 220, 28)
      return original.call(this)
    })
    const frames: Array<{ id: number; cb: FrameRequestCallback; cancelled: boolean }> = []
    let seq = 1
    const prevRaf = window.requestAnimationFrame.bind(window)
    const prevCancel = window.cancelAnimationFrame.bind(window)
    const request = (cb: FrameRequestCallback) => {
      const id = seq++
      frames.push({ id, cb, cancelled: false })
      return id
    }
    const cancel = (id: number) => {
      const frame = frames.find((item) => item.id === id)
      if (frame) frame.cancelled = true
    }
    window.requestAnimationFrame = request as typeof window.requestAnimationFrame
    window.cancelAnimationFrame = cancel as typeof window.cancelAnimationFrame
    return {
      pump() {
        const batch = frames.splice(0).filter((frame) => !frame.cancelled)
        for (const frame of batch) frame.cb(0)
      },
      restore() {
        spy.mockRestore()
        window.requestAnimationFrame = prevRaf
        window.cancelAnimationFrame = prevCancel
      },
    }
  }

  async function ghostAfterMove() {
    await flushPromises()
    await nextTick()
    const ghost = document.querySelector<HTMLElement>('[data-testid="nav-flight-ghost"]')
    expect(ghost).toBeTruthy()
    return ghost!
  }

  it('moves the ghost from the painted origin to the destination', async () => {
    const geometry = stubFlightGeometry()
    try {
      gateMocks.displayedItems.value = [clarify('run-x', 'clarify', '登录'), clarify('run-y', 'clarify', '排队')]
      const wrapper = mountNav(true)
      await flushPromises()
      armSidebarHandoff('run-x', 'clarify')
      apiMocks.listRuns.mockResolvedValue({
        items: [
          run('run-x', '登录', 'running', auto('impl', '实现')),
          run('run-y', '排队', 'waiting_human'),
        ],
        total: 2,
      })
      gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.runId !== 'run-x')
      const ghost = await ghostAfterMove()
      const origin = ghost.getBoundingClientRect()
      expect(origin.y).toBe(174)
      expect(ghost.style.transform).toBe('translate3d(0px, 0px, 0px)')
      expect(ghost.style.transition).toBe('none')
      geometry.pump()
      expect(ghost.getBoundingClientRect().y).toBe(174)
      geometry.pump()
      const landed = ghost.getBoundingClientRect()
      expect(landed.y).toBe(344)
      expect(landed.y).not.toBe(origin.y)
      expect(ghost.style.transition).toContain(`${SIDEBAR_FLIGHT_MS}ms`)
      expect(SIDEBAR_FLIGHT_MS).toBeLessThanOrEqual(600)
      expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBe('in')
      wrapper.unmount()
    } finally {
      geometry.restore()
      vi.useRealTimers()
    }
  })

  it('moves the ghost back onto the running row', async () => {
    const wrapper = await show([run('run-x', '登录', 'running', auto('impl', '实现'))])
    const geometry = stubFlightGeometry()
    try {
      apiMocks.listRuns.mockResolvedValue({
        items: [run('run-x', '登录', 'waiting_human', { id: 'gate', name: '审批', iteration: 1, kind: 'gate', mode: 'manual', state: 'waiting_human' })],
        total: 1,
      })
      routeState.path = '/board'
      const ghost = await ghostAfterMove()
      expect(ghost.getBoundingClientRect().y).toBe(344)
      geometry.pump()
      expect(ghost.getBoundingClientRect().y).toBe(344)
      geometry.pump()
      expect(ghost.getBoundingClientRect().y).toBe(174)
      expect(ghost.style.transition).toContain(`${SIDEBAR_FLIGHT_MS}ms`)
      expect(wrapper.get('[data-key="run-x"]').attributes('data-flight')).toBe('back')
      wrapper.unmount()
    } finally {
      geometry.restore()
      vi.useRealTimers()
    }
  })

  it('moves the ghost onto the folded heading', async () => {
    const geometry = stubFlightGeometry()
    try {
      toggleSection('auto')
      gateMocks.displayedItems.value = [clarify('run-x', 'clarify', '登录'), clarify('run-y', 'clarify', '另一个')]
      const wrapper = mountNav(true)
      await flushPromises()
      armSidebarHandoff('run-x', 'clarify')
      apiMocks.listRuns.mockResolvedValue({
        items: [
          run('run-x', '登录', 'running', auto('impl', '实现')),
          run('run-y', '另一个', 'waiting_human'),
        ],
        total: 2,
      })
      gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.runId !== 'run-x')
      const ghost = await ghostAfterMove()
      expect(ghost.getBoundingClientRect().y).toBe(174)
      geometry.pump()
      expect(ghost.getBoundingClientRect().y).toBe(174)
      geometry.pump()
      const landed = ghost.getBoundingClientRect()
      expect(landed.x).toBe(12)
      expect(landed.y).toBe(312)
      expect(ghost.getAttribute('data-landing')).toBe('heading')
      expect(wrapper.get('[data-testid="nav-auto-process"] .sidebar-section-count').text()).toBe('1')
      expect(wrapper.get('[data-testid="nav-auto-rows"]').isVisible()).toBe(false)
      wrapper.unmount()
    } finally {
      geometry.restore()
      vi.useRealTimers()
    }
  })

  it('keeps pin, rename, cancel and share on an automatic row', async () => {
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    gateMocks.displayedItems.value = [{
      type: 'gate', nodeType: 'human_gate', runId: 'run-a', nodeId: 'g', title: '人工门禁', workflowName: 'w', runTitle: '自动甲',
    }]
    const wrapper = await show([
      run('run-a', '自动甲', 'running', auto('impl', '实现')),
      run('run-b', '自动乙', 'running', auto('test', '测试')),
      run('run-c', '等人', 'waiting_human'),
    ])
    const autoKeys = () => wrapper.get('[data-testid="nav-auto-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))
    expect(autoKeys()).toEqual(['run-a', 'run-b'])
    expect(wrapper.get('[data-testid="nav-running-rows"]').findAll('[data-testid="nav-pending-row"]').map((r) => r.attributes('data-key'))).toEqual(['run-c'])

    const open = (key: string) => {
      wrapper.get(`[data-key="${key}"]`).element.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 40 }),
      )
    }
    open('run-a')
    await nextTick()
    expect([...document.querySelectorAll('[data-testid="nav-pending-menu"] [role="menuitem"]')].map((el) => el.getAttribute('data-testid'))).toEqual([
      'nav-pending-menu-pin',
      'nav-pending-menu-rename',
      'nav-pending-menu-share',
      'nav-pending-menu-cancel',
    ])
    ;(document.querySelector('[data-testid="nav-pending-menu-share"]') as HTMLButtonElement).click()
    await nextTick()
    expect(routerMocks.push).toHaveBeenLastCalledWith({ path: '/gates', query: { run: 'run-a', node: 'g', share: '1' } })

    open('run-b')
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(autoKeys()).toEqual(['run-b', 'run-a'])
    expect(wrapper.get('[data-key="run-b"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="nav-running-rows"]').text()).toContain('等人')

    open('run-b')
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-rename"]') as HTMLButtonElement).click()
    await nextTick()
    apiMocks.updateRunTitle.mockResolvedValueOnce({ id: 'run-b', title: '新名字' })
    const input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    await input.setValue('新名字')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(apiMocks.updateRunTitle).toHaveBeenCalledWith('run-b', '新名字')

    open('run-a')
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-cancel"]') as HTMLButtonElement).click()
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-cancel-confirm"]')).toBeTruthy()
    ;(document.querySelector('[data-testid="nav-pending-cancel-dismiss"]') as HTMLButtonElement).click()
    wrapper.unmount()
    vi.useRealTimers()
  })
})
