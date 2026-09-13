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
  RouterLink: {
    props: ['to'],
    template:
      '<a :href="typeof to === \'string\' ? to : (to.path || \'\')" :data-to="typeof to === \'string\' ? to : (to.path || \'\')" @click="$emit(\'click\')"><slot /></a>',
  },
}))

const gateMocks = vi.hoisted(() => ({
  peek: vi.fn(),
  refresh: vi.fn(),
  count: { value: 2 },
}))

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
        projectId: string
        projectName: string
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

import AppSidebarNav from './AppSidebarNav.vue'

beforeEach(() => {
  vi.clearAllMocks()
  favMocks.unfavorite.mockReset()
  gateMocks.peek.mockResolvedValue(undefined)
  gateMocks.refresh.mockResolvedValue(undefined)
  gateMocks.count.value = 2
  notifMocks.unreadCount.value = 0
  favMocks.displayItems.value = []
  favMocks.hydrateDisplay.mockResolvedValue(undefined)
  breakpointMocks.isMobile.value = false
  routeState.path = '/dashboard'
  routeState.query = {}
  routeState.meta = {}
  vi.useFakeTimers()
})

function mountNav() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...nav } },
  })
  return mount(AppSidebarNav, {
    global: {
      plugins: [i18n],
      stubs: { Icon: true },
      directives: { 'hover-ink': vHoverInk },
    },
  })
}

describe('AppSidebarNav', () => {
  it('renders nav links from sidebar config', async () => {
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('nav').exists()).toBe(true)
    expect(gateMocks.refresh).toHaveBeenCalled()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows the gates badge without exposing notifications in workspace chrome', async () => {
    notifMocks.unreadCount.value = 46
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-gates-badge"]').text()).toBe('2')
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
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides quick-pipelines section when there are no favorites', async () => {
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('快捷流水线')
    // Primary workspace nav still present (plan g1.1)
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/settings"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/dashboard"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/gates"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/dashboard"]').text()).toContain('开始')
    expect(wrapper.find('[data-to="/gates"]').text()).toContain('待办')
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/projects"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('配置')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides quick-pipelines section after the last favorite is removed', async () => {
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        projectId: 'p1',
        projectName: 'checkout-service',
        status: 'published',
      },
    ]
    favMocks.unfavorite.mockImplementation(() => {
      favMocks.displayItems.value = []
    })
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(true)

    await wrapper.find('[data-testid="nav-quick-pipeline-unfavorite"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(false)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('lists favorites and unfavorite does not open launch', async () => {
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 2,
        name: '夜间回归',
        projectId: 'p1',
        projectName: 'checkout-service',
        status: 'draft',
      },
    ]
    favMocks.getFavoriteWorkflow.mockResolvedValue({ id: 'wf-1', name: '夜间回归', nodes: [], edges: [] })
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-pipeline-item"]').text()).toContain('夜间回归')
    expect(wrapper.text()).toContain('草稿')

    await wrapper.find('[data-testid="nav-quick-pipeline-unfavorite"]').trigger('click')
    expect(favMocks.unfavorite).toHaveBeenCalledWith('wf-1', { name: '夜间回归' })
    expect(launchMocks.openLaunch).not.toHaveBeenCalled()

    await wrapper.find('[data-testid="nav-quick-pipeline-item"]').trigger('click')
    await flushPromises()
    expect(favMocks.getFavoriteWorkflow).toHaveBeenCalledWith('wf-1')
    expect(launchMocks.openLaunch).toHaveBeenCalled()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('shows desktop-only independent drag handles without changing the item click target', async () => {
    favMocks.displayItems.value = [
      { workflowId: 'wf-1', favoritedAt: 2, name: '夜间回归', projectId: 'p1', projectName: 'checkout', status: 'draft' },
      { workflowId: 'wf-2', favoritedAt: 1, name: '发布预检', projectId: 'p1', projectName: 'billing', status: 'published' },
    ]
    const wrapper = mountNav()
    await flushPromises()
    const handles = wrapper.findAll('[data-testid="nav-quick-pipeline-drag-handle"]')
    expect(handles).toHaveLength(2)
    await wrapper.find('[data-testid="nav-quick-pipeline-item"]').trigger('click')
    expect(favMocks.getFavoriteWorkflow).toHaveBeenCalledWith('wf-1')
    expect(favMocks.reorderFavorites).not.toHaveBeenCalled()
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides the handle on mobile while retaining quick-item actions', async () => {
    breakpointMocks.isMobile.value = true
    favMocks.displayItems.value = [
      { workflowId: 'wf-1', favoritedAt: 1, name: '夜间回归', projectId: 'p1', projectName: 'checkout', status: 'published' },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-pipeline-drag-handle"]').exists()).toBe(false)
    await wrapper.find('[data-testid="nav-quick-pipeline-item"]').trigger('click')
    expect(favMocks.getFavoriteWorkflow).toHaveBeenCalledWith('wf-1')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('settings chrome replaces workspace four items and hides quick pipelines (plan g2.1 / g1.2)', async () => {
    routeState.path = '/projects'
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        projectId: 'p1',
        projectName: 'checkout-service',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').attributes('data-to')).toBe('/dashboard')
    expect(wrapper.find('[data-to="/projects"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-integrations"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('返回首页')
    expect(wrapper.text()).toContain('通用')
    expect(wrapper.text()).toContain('平台规则')
    expect(wrapper.find('[data-to="/agents"]').text()).toContain('智能体')
    expect(wrapper.text()).not.toContain('待办')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('highlights projects for /projects/:id and general only on exact /settings (plan g2.2)', async () => {
    routeState.path = '/projects/abc'
    const wrapper = mountNav()
    await flushPromises()
    const projectLink = wrapper.find('[data-to="/projects"]')
    expect(projectLink.classes()).toContain('active')
    wrapper.unmount()

    routeState.path = '/settings'
    const general = mountNav()
    await flushPromises()
    const links = general.findAll('[data-to="/settings"]')
    const generalLink = links.find((l) => l.text().includes('通用'))
    expect(generalLink?.classes()).toContain('active')
    expect(general.find('[data-to="/settings/platform-rules"]').classes()).not.toContain('active')
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

  it('uses workspace chrome and highlights runs for the run list', async () => {
    routeState.path = '/runs'
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/runs"]').classes()).toContain('active')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('switches chrome after navigation and still renders the target (g2.2)', async () => {
    favMocks.displayItems.value = [
      {
        workflowId: 'wf-1',
        favoritedAt: 1,
        name: '夜间回归',
        projectId: 'p1',
        projectName: 'checkout-service',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(true)

    routeState.path = '/agents'
    await flushPromises()
    await nextTick()
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/agents"]').text()).toContain('智能体')

    routeState.path = '/dashboard'
    await flushPromises()
    await nextTick()
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-quick-pipelines"]').exists()).toBe(true)
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
  })
})
