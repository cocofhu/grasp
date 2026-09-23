// @vitest-environment happy-dom
import { defineComponent, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import { __resetSidebarHiddenForTests, setSidebarHidden, sidebarHidden } from '@/lib/shared/sidebarHidden'

const routeState = { path: '/', meta: {} as Record<string, unknown> }
const isMobileRef = ref(false)

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: isMobileRef }),
}))

vi.mock('@/lib/composables/useShutdownState', () => ({
  drainToast: { visible: false, text: '' },
  formatGrace: () => '',
  isDraining: () => false,
  isOffline: () => false,
  shutdownState: { mode: 'normal', graceRemainingSeconds: 0, message: '', checked: true },
  startShutdownPolling: vi.fn(),
  stopShutdownPolling: vi.fn(),
}))

vi.mock('@/lib/run/useWorkflowRunLaunch', async () => {
  const { ref } = await import('vue')
  return {
    useWorkflowRunLaunch: () => ({
      open: ref(false),
      target: ref(null),
      runFields: ref([]),
      runInputs: ref({}),
      runImages: ref({}),
      draftRestored: ref(false),
      openLaunch: vi.fn(),
      closeLaunch: vi.fn(),
      saveRunDraftClick: vi.fn(),
      onStarted: vi.fn(),
    }),
  }
})

import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import AppShell from './AppShell.vue'

function mountShell(opts?: { stubSidebar?: boolean; attachToBody?: boolean }) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...shell } },
  })
  return mount(AppShell, {
    attachTo: opts?.attachToBody ? document.body : undefined,
    slots: { default: '<div data-testid="main">内容</div>' },
    global: {
      plugins: [i18n],
      stubs: {
        AppSidebar: opts?.stubSidebar === false
          ? false
          : defineComponent({
              template:
                '<aside data-testid="sidebar" class="w-[232px] shrink-0" />',
            }),
        AppSidebarNav: { template: '<nav data-testid="shell-nav" />' },
        BrandLogo: true,
        Icon: true,
        RunLaunchModal: true,
        ServiceCommitBadge: true,
        ShellChromeControls: {
          template: '<div data-testid="shell-chrome-controls" />',
        },
        Teleport: true,
        Transition: false,
      },
    },
  })
}

describe('AppShell (no topbar + floating ball)', () => {
  beforeEach(() => {
    __resetSidebarHiddenForTests()
    isMobileRef.value = false
    routeState.path = '/'
    routeState.meta = {}
    vi.useFakeTimers()
  })
  afterEach(() => {
    __resetSidebarHiddenForTests()
    vi.useRealTimers()
  })

  it('renders sidebar without AppTopbar (g1.1)', () => {
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="topbar"]').exists()).toBe(false)
    expect(wrapper.find('header').exists()).toBe(false)
    expect(wrapper.find('[data-testid="main"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="floating-nav-ball"]').exists()).toBe(true)
    expect(wrapper.find('[aria-live="polite"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not render desktop-nav-open or edge-open (g1.1 / g2.1)', () => {
    setSidebarHidden(true)
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="desktop-nav-open"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="desktop-nav-edge-open"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="floating-nav-ball-wrap"]').attributes('data-exiting')).toBe(
      'false',
    )
    wrapper.unmount()
  })

  it('full pages no longer use edge-open or pl-11 (g2.1)', () => {
    routeState.meta = { full: true }
    setSidebarHidden(true)
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="desktop-nav-edge-open"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="app-full-main"]').classes()).not.toContain('pl-11')
    expect(wrapper.find('[data-testid="floating-nav-ball"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('click floating ball pins sidebar immediately, ball exits in parallel (g1.1 / g2.2)', async () => {
    setSidebarHidden(true)
    const wrapper = mountShell()
    const ball = wrapper.find('[data-testid="floating-nav-ball"]')
    expect(ball.attributes('aria-label')).toBe('打开导航')
    await ball.trigger('click')
    expect(sidebarHidden.value).toBe(false)
    expect(wrapper.find('[data-testid="floating-nav-ball-wrap"]').attributes('data-exiting')).toBe(
      'true',
    )
    await vi.advanceTimersByTimeAsync(200)
    await nextTick()
    expect(sidebarHidden.value).toBe(false)
    expect(wrapper.find('[data-testid="floating-nav-ball-wrap"]').attributes('data-exiting')).toBe(
      'false',
    )
    wrapper.unmount()
  })

  it('moves focus to desktop-nav-hide after pinning from the ball (g2.3)', async () => {
    setSidebarHidden(true)
    const wrapper = mountShell({ stubSidebar: false, attachToBody: true })
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    expect(sidebarHidden.value).toBe(false)
    await nextTick()
    await nextTick()
    expect(document.activeElement).toBe(wrapper.find('[data-testid="desktop-nav-hide"]').element)
    wrapper.unmount()
  })

  it('hover on floating ball does not expand sidebar (g2.2)', async () => {
    setSidebarHidden(true)
    const wrapper = mountShell()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('mouseenter')
    await nextTick()
    expect(sidebarHidden.value).toBe(true)
    wrapper.unmount()
  })

  it('mobile click opens drawer instead of pinning (g2.4)', async () => {
    isMobileRef.value = true
    setSidebarHidden(true)
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="mobile-nav-drawer"]').exists()).toBe(false)
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    await nextTick()
    expect(sidebarHidden.value).toBe(true)
    expect(wrapper.find('[data-testid="mobile-nav-drawer"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-chrome-controls"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps height chain classes (g5.2 fill lock)', () => {
    const wrapper = mountShell()
    expect(wrapper.find('.h-screen').exists()).toBe(true)
    const main = wrapper.find('main')
    expect(main.classes()).toContain('min-h-0')
    expect(main.classes()).toContain('flex-1')
    wrapper.unmount()
  })

  it('workspace shell uses dotted canvas and sidebar padding (plan g1.3)', () => {
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="app-shell-workspace"]').exists()).toBe(true)
    expect(wrapper.find('.app-shell-dotgrid').exists()).toBe(true)
    const slot = wrapper.find('[data-testid="app-shell-sidebar-slot"]')
    expect(slot.classes()).toContain('py-[14px]')
    expect(slot.classes()).toContain('pl-[14px]')
    wrapper.unmount()
  })

  it('only reduces workspace top padding (compact layout g1.1)', () => {
    const wrapper = mountShell()
    const workspace = wrapper.find('[data-testid="app-shell-workspace"]')
    const content = workspace.find('.scroll-area > div')
    expect(content.classes()).toEqual(
      expect.arrayContaining(['px-4', 'pt-2', 'pb-4', 'md:px-6', 'md:pt-3', 'md:pb-6']),
    )
    expect(content.classes()).not.toEqual(expect.arrayContaining(['py-4', 'md:py-6']))
    wrapper.unmount()
  })

  it('full pages keep dotted canvas and sidebar pad (plan g1.2 / g1.3)', () => {
    routeState.meta = { full: true }
    const wrapper = mountShell()
    expect(wrapper.find('[data-testid="app-shell-full"]').exists()).toBe(true)
    expect(wrapper.find('.app-shell-dotgrid').exists()).toBe(true)
    const slot = wrapper.find('[data-testid="app-shell-sidebar-slot"]')
    expect(slot.classes()).toContain('py-[14px]')
    expect(slot.classes()).toContain('pl-[14px]')
    wrapper.unmount()
  })

  // plan_coverage: g2.1 — workspace ⇄ full share identical shell classes → no in-place flip
  it('workspace and full shells render the same canvas + sidebar pad (plan g2.1)', () => {
    const workspace = mountShell()
    routeState.meta = { full: true }
    const fullShell = mountShell()
    const workspaceRoot = workspace.find('[data-testid="app-shell-workspace"]')
    const fullRoot = fullShell.find('[data-testid="app-shell-full"]')
    expect(fullRoot.classes().sort()).toEqual(workspaceRoot.classes().sort())
    expect(fullRoot.classes()).toContain('app-shell-dotgrid')
    const workspaceSlot = workspace.find('[data-testid="app-shell-sidebar-slot"]')
    const fullSlot = fullShell.find('[data-testid="app-shell-sidebar-slot"]')
    expect(fullSlot.classes().sort()).toEqual(workspaceSlot.classes().sort())
    expect(fullSlot.classes()).toContain('py-[14px]')
    expect(fullSlot.classes()).toContain('pl-[14px]')
    workspace.unmount()
    fullShell.unmount()
  })

  it('mobile drawer stays md:hidden (g3.2 source lock)', () => {
    const src = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), 'AppShell.vue'),
      'utf8',
    )
    expect(src).toMatch(/bg-black\/50 md:hidden/)
    expect(src).toMatch(/app-sidebar-card[\s\S]*?md:hidden|md:hidden[\s\S]*?data-testid="mobile-nav-drawer"/)
    const aside = src.match(/<aside[\s\S]*?data-testid="mobile-nav-drawer"[\s\S]*?>/)?.[0]
    expect(aside).toBeTruthy()
    expect(aside!).toMatch(/\bapp-sidebar-card\b/)
    expect(aside!).toMatch(/\bmd:hidden\b/)
    expect(aside!).not.toMatch(/\binset-y-0\b/)
    expect(src).not.toMatch(/<AppTopbar/)
  })

  it('desktop hide uses width 0 not a mobile drawer (g2.2 / g3.2)', async () => {
    const wrapper = mountShell({ stubSidebar: false })
    const aside = wrapper.find('[data-testid="app-desktop-sidebar"]')
    expect(aside.exists()).toBe(true)
    await wrapper.find('[data-testid="desktop-nav-hide"]').trigger('click')
    expect((aside.element as HTMLElement).style.display).not.toBe('none')
    expect(aside.classes()).toContain('w-0')
    expect(aside.classes()).toContain('border-r-0')
    expect(wrapper.find('[data-testid="shell-nav"]').exists()).toBe(true)
    expect(aside.classes()).toContain('hidden')
    expect(aside.classes()).toContain('md:flex')
    wrapper.unmount()
  })
})
