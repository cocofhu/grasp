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
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    updateRunTitle: (...args: unknown[]) => apiMocks.updateRunTitle(...args),
  },
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
import { useAuth } from '@/lib/composables/useAuth'
import { useToast } from '@/lib/composables/useToast'
import AppSidebarNav from './AppSidebarNav.vue'

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
  routeState.path = '/dashboard'
  routeState.query = {}
  routeState.meta = {}
  localStorage.clear()
  useAuth().clearUser()
  apiMocks.updateRunTitle.mockReset()
  const toast = useToast()
  for (const item of [...toast.toasts.value]) toast.dismiss(item.id)
  vi.useFakeTimers()
})

function mountNav(attach = false) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...nav } },
  })
  return mount(AppSidebarNav, {
    attachTo: attach ? document.body : undefined,
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
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/settings"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/dashboard"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/gates"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/dashboard"]').text()).toContain('开始')
    expect(wrapper.text()).not.toContain('待办')
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(false)
    const workspace = wrapper.find('[data-testid="nav-workspace-chrome"]')
    expect(workspace.exists()).toBe(true)
    expect(workspace.findAll('a.nav-item').map((l) => l.attributes('data-to'))).toEqual([
      '/dashboard',
      '/projects',
      '/runs',
      '/settings',
    ])
    expect(workspace.find('[data-to="/projects"]').text()).toContain('项目')
    expect(wrapper.text()).not.toContain('配置')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('hides quick-workflows section after the last favorite is removed', async () => {
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
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(true)

    await wrapper.find('[data-testid="nav-quick-workflow-unfavorite"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
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
    expect(wrapper.find('[data-testid="nav-quick-workflow-item"]').text()).toContain('夜间回归')
    expect(wrapper.text()).toContain('草稿')

    await wrapper.find('[data-testid="nav-quick-workflow-unfavorite"]').trigger('click')
    expect(favMocks.unfavorite).toHaveBeenCalledWith('wf-1', { name: '夜间回归' })
    expect(launchMocks.openLaunch).not.toHaveBeenCalled()

    await wrapper.find('[data-testid="nav-quick-workflow-item"]').trigger('click')
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
    const handles = wrapper.findAll('[data-testid="nav-quick-workflow-drag-handle"]')
    expect(handles).toHaveLength(2)
    await wrapper.find('[data-testid="nav-quick-workflow-item"]').trigger('click')
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
    expect(wrapper.find('[data-testid="nav-quick-workflow-drag-handle"]').exists()).toBe(false)
    await wrapper.find('[data-testid="nav-quick-workflow-item"]').trigger('click')
    expect(favMocks.getFavoriteWorkflow).toHaveBeenCalledWith('wf-1')
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
    const categoryLinks = wrapper
      .find('[data-testid="nav-settings-chrome"]')
      .findAll('a.nav-item')
      .filter((l) => l.attributes('data-testid') !== 'nav-back-home')
    expect(categoryLinks[0].attributes('data-to')).toBe('/notifications')
    expect(categoryLinks[0].text()).toContain('通知')
    expect(categoryLinks.map((l) => l.attributes('data-to'))).not.toContain('/projects')
    expect(wrapper.text()).not.toContain('项目')
    expect(wrapper.find('[data-to="/notifications"]').exists()).toBe(true)
    expect(wrapper.find('[data-to="/runs"]').exists()).toBe(false)
    expect(wrapper.find('[data-to="/stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-integrations"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('返回首页')
    expect(wrapper.text()).toContain('通用')
    expect(wrapper.find('[data-to="/agents"]').text()).toContain('智能体')
    expect(wrapper.text()).not.toContain('待办')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps workspace chrome and highlights projects on project routes', async () => {
    routeState.path = '/projects'
    const list = mountNav()
    await flushPromises()
    expect(list.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(list.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(list.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    expect(list.text()).not.toContain('返回首页')
    expect(list.find('[data-to="/projects"]').classes()).toContain('active')
    list.unmount()

    routeState.path = '/projects/abc'
    routeState.query = { tab: 'agents' }
    const wrapper = mountNav()
    await flushPromises()
    expect(wrapper.find('[data-testid="nav-workspace-chrome"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings-chrome"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-back-home"]').exists()).toBe(false)
    const projectLink = wrapper.find('[data-to="/projects"]')
    expect(projectLink.classes()).toContain('active')
    expect(wrapper.find('[data-to="/runs"]').classes()).not.toContain('active')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('highlights general only on exact /settings (plan g2.2)', async () => {
    routeState.path = '/settings'
    const general = mountNav()
    await flushPromises()
    const links = general.findAll('[data-to="/settings"]')
    const generalLink = links.find((l) => l.text().includes('通用'))
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
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(true)

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
    expect(wrapper.find('[data-testid="nav-quick-workflows"]').exists()).toBe(true)
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
    const runs = chrome.find('[data-to="/runs"]').element as HTMLElement
    expect(dashboard.classList.contains('hover-ink-host')).toBe(true)
    expect((dashboard.firstElementChild as HTMLElement | null)?.classList.contains('hover-ink')).toBe(true)

    routeState.path = '/runs'
    await flushPromises()
    await nextTick()

    const nextActive = chrome.find('a.nav-item.active').element as HTMLElement
    expect(nextActive).toBe(runs)
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
        projectId: 'p1',
        projectName: 'checkout',
        status: 'published',
      },
    ]
    const wrapper = mountNav()
    await flushPromises()

    const pending = wrapper.get('[data-testid="nav-pending-approvals"]')
    expect(pending.text()).toContain('待审批')
    expect(wrapper.find('[data-testid="nav-inbox-records"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-record-row"]').exists()).toBe(false)
    const quick = wrapper.get('[data-testid="nav-quick-workflows"]')
    expect(quick.element.compareDocumentPosition(pending.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    const rows = pending.findAll('[data-testid="nav-pending-row"]')
    expect(rows.map((row) => row.text())).toEqual(['发布前确认', '需求规格', '代码评审'])
    expect(rows[0].find('[data-testid="inbox-motion-spin"]').exists()).toBe(false)
    expect(rows[0].find('[data-testid="inbox-motion-pulse"]').exists()).toBe(false)
    expect(rows[1].attributes('data-motion')).toBe('spin')
    expect(rows[1].find('[data-testid="inbox-motion-spin"]').exists()).toBe(true)
    expect(rows[2].attributes('data-motion')).toBe('pulse')
    expect(rows[2].find('[data-testid="inbox-motion-pulse"]').exists()).toBe(true)
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
    await nextTick()
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

    expectRowStart(rows[0])
    expectRowStart(rows[1], 'inbox-motion-spin')
    expectRowStart(rows[2], 'inbox-motion-spin')
    expect(rows[2].get('.nav-row-title').text()).toBe(longLabel)
    expectRowStart(rows[3], 'inbox-motion-pulse')

    expect(rows[0].classes()).not.toContain('active')
    gateMocks.activeInboxKey.value = 'run-1:g1'
    routeState.path = '/gates'
    await nextTick()
    expect(rows[0].classes()).toContain('active')
    expectRowStart(rows[0])

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
    expect(wrapper.text()).not.toContain('待审批')
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
      'Run #abc',
      'Run #json',
      'approving',
    ])
    expect(rows[0].text()).not.toContain('需求澄清')
    expect(rows[4].text()).not.toContain('[')
    expect(rows[0].attributes('title')).toBe('把登录做清楚 · 需求澄清')
    expect(rows[2].attributes('title')).toBe('Run #abc · 需求澄清')
    expect(rows[4].attributes('title')).toBe('approving · 人工门禁')
    expect(rows[0].attributes('data-motion')).toBe('spin')
    expect(rows[0].find('[data-testid="inbox-motion-spin"]').exists()).toBe(true)
    await rows[1].trigger('click')
    expect(routerMocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-2', node: 'clarify' } })
    expect(enNav.nav.pendingPin).toBe('Pin')
    expect(enNav.nav.pendingUnpin).toBe('Unpin')
    expect(enNav.nav.pendingRename).toBe('Rename')
    expect(enNav.nav.pendingNameEmpty).toBe('Name cannot be empty')
    expect(enNav.nav.pendingNameTooLong).toBe('80 characters maximum')
    expect(enNav.nav.pendingRenameFailed).toBe('Save failed')
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
    expect(sourceOrder()).toEqual(['run-e:e', 'run-b:b', 'run-c:c', 'run-b:other'])

    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 })
    wrapper.get('[data-key="run-c:c"]').element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    await nextTick()
    const menu = document.querySelector('[data-testid="nav-pending-menu"]')
    expect(menu).toBeTruthy()
    expect(menu?.textContent).toContain('置顶')
    expect(menu?.textContent).toContain('重命名')
    expect(wrapper.find('[data-testid="nav-pending-rename-input"]').exists()).toBe(false)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-e:e', 'run-b:b', 'run-c:c', 'run-b:other'])

    wrapper.get('[data-key="run-c:c"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-e:e', 'run-b:b', 'run-c:c', 'run-b:other'])

    const stored = gateMocks.displayedItems.value.map((it) => `${it.runId}:${it.nodeId}`)
    wrapper.get('[data-key="run-c:c"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    expect(sourceOrder()).toEqual(['run-c:c', 'run-e:e', 'run-b:b', 'run-b:other'])
    expect(wrapper.get('[data-key="run-c:c"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(true)
    expect(wrapper.get('[data-key="run-b:other"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(false)
    expect(gateMocks.displayedItems.value.map((it) => `${it.runId}:${it.nodeId}`)).toEqual(stored)

    wrapper.get('[data-key="run-b:b"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('置顶')
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(sourceOrder()).toEqual(['run-b:b', 'run-c:c', 'run-e:e', 'run-b:other'])

    wrapper.get('[data-key="run-b:b"]').element.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 30 }),
    )
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')?.textContent).toContain('取消置顶')
    ;(document.querySelector('[data-testid="nav-pending-menu-pin"]') as HTMLButtonElement).click()
    await nextTick()
    expect(sourceOrder()).toEqual(['run-c:c', 'run-e:e', 'run-b:b', 'run-b:other'])
    expect(wrapper.get('[data-key="run-b:b"]').find('[data-testid="nav-pending-pin"]').exists()).toBe(false)

    wrapper.unmount()
    const again = mountNav(true)
    await flushPromises()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-c:c',
      'run-e:e',
      'run-b:b',
      'run-b:other',
    ])
    useAuth().setUser({ username: 'bob', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-e:e',
      'run-b:b',
      'run-c:c',
      'run-b:other',
    ])
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-d', nodeId: 'd', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '更新的一行' },
      { type: 'clarify', runId: 'run-b', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第二条' },
    ]
    gateMocks.sidebarExtras.value = []
    await nextTick()
    expect(again.find('[data-testid="nav-pending-row"]').attributes('data-key')).not.toBe('run-c:c')
    gateMocks.displayedItems.value = [
      { type: 'clarify', runId: 'run-d', nodeId: 'd', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '更新的一行' },
      { type: 'clarify', runId: 'run-b', nodeId: 'b', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第二条' },
      { type: 'clarify', runId: 'run-c', nodeId: 'c', label: '需求澄清', kind: 'clarify', workflowName: 'w', runTitle: '第三条' },
    ]
    await nextTick()
    expect(again.findAll('[data-testid="nav-pending-row"]').map((row) => row.attributes('data-key'))).toEqual([
      'run-c:c',
      'run-d:d',
      'run-b:b',
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

    await wrapper.get('[data-key="run-1:a"]').trigger('keydown', { key: 'F2' })
    await nextTick()
    expect(document.querySelector('[data-testid="nav-pending-menu"]')).toBeNull()
    let input = wrapper.get('[data-testid="nav-pending-rename-input"]')
    expect((input.element as HTMLInputElement).value).toBe('把登录做清楚')
    expect(wrapper.get('[data-key="run-1:a"]').find('[data-testid="inbox-motion-spin"]').exists()).toBe(true)
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
    expect(wrapper.get('[data-key="run-1:a"]').get('.nav-row-title').text()).toBe('把登录做清楚')
    expect(apiMocks.updateRunTitle).not.toHaveBeenCalled()

    await wrapper.get('[data-key="run-empty:e"]').trigger('keydown', { key: 'F2' })
    await nextTick()
    expect((wrapper.get('[data-testid="nav-pending-rename-input"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-testid="nav-pending-rename-input"]').trigger('keydown', { key: 'Escape' })

    wrapper.get('[data-key="run-1:a"]').element.dispatchEvent(
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
    expect(wrapper.findAll('.nav-row-title').map((node) => node.text()).filter((text) => text === '字'.repeat(80))).toHaveLength(2)
    expect(wrapper.get('[data-key="run-2:a"]').get('.nav-row-title').text()).toBe('补充空状态文案')

    wrapper.get('[data-key="run-2:a"]').element.dispatchEvent(
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

    wrapper.get('[data-key="run-2:a"]').element.dispatchEvent(
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
    expect(wrapper.get('[data-key="run-2:a"]').get('.nav-row-title').text()).toBe('空状态短名')

    wrapper.get('[data-key="run-2:a"]').element.dispatchEvent(
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
    expect(wrapper.get('[data-key="run-2:a"]').find('.nav-row-title').exists()).toBe(false)

    gateMocks.displayedItems.value = gateMocks.displayedItems.value.filter((it) => it.nodeId !== 'a' || it.runId !== 'run-2')
    await nextTick()
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
})
