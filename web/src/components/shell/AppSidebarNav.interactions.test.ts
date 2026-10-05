// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nav from '@/locales/zh-CN/nav.json'

const route = { path: '/dashboard', query: {} as any, meta: {} as any }
vi.mock('vue-router', () => ({
  useRoute: () => route,
  RouterLink: { props: ['to'], emits: ['click'], template: '<a :data-to="typeof to === \'string\' ? to : to.path" @click="$emit(\'click\')"><slot/></a>' },
}))
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    count: ref(3), unread: ref(5), items: ref<any[]>([]), mobile: ref(false),
    peek: vi.fn(), refresh: vi.fn(), hydrate: vi.fn(), unfavorite: vi.fn(),
    getWorkflow: vi.fn(), reorder: vi.fn(), launch: vi.fn(), toastError: vi.fn(),
  }
})
vi.mock('@/lib/inbox/usePendingGates', () => ({ usePendingGates: () => ({ count: mocks.count, peek: mocks.peek, refresh: mocks.refresh }) }))
vi.mock('@/lib/run/useRunTerminalNotifications', () => ({ useRunTerminalNotifications: () => ({ unreadCount: mocks.unread }) }))
vi.mock('@/lib/run/useWorkflowFavorites', () => ({ useWorkflowFavorites: () => ({
  displayItems: mocks.items, hydrateDisplay: mocks.hydrate, unfavorite: mocks.unfavorite,
  getFavoriteWorkflow: mocks.getWorkflow, reorderFavorites: mocks.reorder,
}) }))
vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({ useWorkflowRunLaunch: () => ({ openLaunch: mocks.launch }) }))
vi.mock('@/lib/composables/useBreakpoint', () => ({ useBreakpoint: () => ({ isMobile: mocks.mobile }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ error: mocks.toastError }) }))
import AppSidebarNav from './AppSidebarNav.vue'

const favorites = [
  { workflowId: 'w1', name: 'One', projectId: 'p', projectName: 'P', status: 'published', favoritedAt: 2 },
  { workflowId: 'w2', name: 'Two', projectId: 'p', projectName: 'P', status: 'draft', favoritedAt: 1 },
]
function mountNav() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...nav } } })
  return mount(AppSidebarNav, { attachTo: document.body, global: { plugins: [i18n], stubs: { Icon: true } } })
}

describe('AppSidebarNav interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks(); vi.useFakeTimers()
    route.path = '/dashboard'; route.query = {}; route.meta = {}
    mocks.items.value = [...favorites]; mocks.mobile.value = false
    mocks.peek.mockResolvedValue(undefined); mocks.refresh.mockResolvedValue(undefined); mocks.hydrate.mockResolvedValue(undefined)
    mocks.getWorkflow.mockResolvedValue({ id: 'w1', name: 'One' })
  })
  afterEach(() => { document.body.classList.remove('quick-workflow-dragging'); vi.useRealTimers() })

  it('polls, reacts to navigation, emits navigation and handles missing/error workflows', async () => {
    const w = mountNav()
    await flushPromises()
    vi.advanceTimersByTime(15000)
    expect(mocks.peek).toHaveBeenCalledWith({ source: 'sidebar-poll' })
    await w.find('[data-to="/runs"]').trigger('click')
    expect(w.emitted('navigate')).toBeTruthy()
    mocks.getWorkflow.mockResolvedValueOnce(null)
    await (w.vm as any).onLaunch('missing')
    mocks.getWorkflow.mockRejectedValueOnce(new Error('offline'))
    await (w.vm as any).onLaunch('bad')
    expect(mocks.toastError).toHaveBeenCalled()
    route.path = '/runs'
    await w.vm.$nextTick()
    expect(mocks.refresh).toHaveBeenCalledWith({ source: 'mount' })
    w.unmount()
  })

  it('reorders favorites by pointer drag and supports cancellation/guards', async () => {
    const w = mountNav()
    await flushPromises()
    const rows = w.findAll('[data-sortable-row]')
    rows.forEach((row, i) => {
      Object.defineProperty(row.element, 'offsetHeight', { configurable: true, value: 40 })
      row.element.getBoundingClientRect = () => ({ top: i * 50, left: 10, width: 180, height: 40, right: 190, bottom: i * 50 + 40, x: 10, y: i * 50, toJSON() {} })
    })
    const handle = w.find('[data-testid="nav-quick-workflow-drag-handle"]')
    ;(handle.element as any).setPointerCapture = vi.fn()
    ;(handle.element as any).releasePointerCapture = vi.fn()
    await handle.trigger('pointerdown', { button: 0, pointerId: 1, clientX: 15, clientY: 5 })
    handle.element.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, clientX: 30, clientY: 90, bubbles: true }))
    await w.vm.$nextTick()
    expect(document.body.classList.contains('quick-workflow-dragging')).toBe(true)
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1 }))
    expect(mocks.reorder).toHaveBeenCalled()
    vi.advanceTimersByTime(0)
    mocks.mobile.value = true
    ;(w.vm as any).onHandlePointerDown('w1', new PointerEvent('pointerdown', { button: 0 }))
    expect(mocks.reorder).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('does not launch or unfavorite while click suppression is active', async () => {
    const w = mountNav()
    await flushPromises()
    const vm = w.vm as any
    vm.suppressQuickItemClick = true
    await vm.onLaunch('w1')
    vm.onUnfavorite('w1', 'One', new MouseEvent('click'))
    expect(mocks.getWorkflow).not.toHaveBeenCalled()
    expect(mocks.unfavorite).not.toHaveBeenCalled()
    expect(vm.badgeFor('/other')).toBe(0)
    expect(vm.isActive('/dashboard')).toBe(true)
    w.unmount()
  })
})
