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
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], emits: ['click'], template: '<a :data-to="typeof to === \'string\' ? to : to.path" @click="$emit(\'click\')"><slot/></a>' },
}))
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    count: ref(3), unread: ref(5), items: ref<any[]>([]), mobile: ref(false),
    peek: vi.fn(), refresh: vi.fn(), hydrate: vi.fn(), unfavorite: vi.fn(),
    getWorkflow: vi.fn(), reorder: vi.fn(), launch: vi.fn(), toastError: vi.fn(),
    displayedItems: ref<any[]>([]),
    sidebarExtras: ref<any[]>([]),
    startFailedKeys: ref<string[]>([]),
    activeInboxKey: ref<string | null>(null),
  }
})
vi.mock('@/lib/inbox/usePendingGates', () => ({ usePendingGates: () => ({
  count: mocks.count, peek: mocks.peek, refresh: mocks.refresh,
  displayedItems: mocks.displayedItems,
  sidebarExtras: mocks.sidebarExtras, startFailedKeys: mocks.startFailedKeys,
  activeInboxKey: mocks.activeInboxKey,
  itemKey: (it: { runId: string; nodeId: string }) => `${it.runId}:${it.nodeId}`,
}) }))
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
  { workflowId: 'w1', name: 'One', status: 'published', favoritedAt: 2 },
  { workflowId: 'w2', name: 'Two', status: 'draft', favoritedAt: 1 },
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
    route.path = '/runs'
    await w.vm.$nextTick()
    expect(mocks.refresh).toHaveBeenCalledWith({ source: 'mount' })
    w.unmount()
  })

})
