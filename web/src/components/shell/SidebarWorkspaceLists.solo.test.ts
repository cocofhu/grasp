// @vitest-environment happy-dom
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { SoloSession } from '@/lib/api/apiTypes'

const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  const signedInWorkspace = { name: 'Workspace', description: '', variables: [] }
  return { signedInWorkspace, workspace: ref<typeof signedInWorkspace | null>(signedInWorkspace), listRuns: vi.fn(), listSoloSessions: vi.fn() }
})
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, loading: ref(false), ensureLoaded: async () => mocks.workspace.value }) }))
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { __resetSidebarSectionsForTests } from '@/lib/shared/sidebarSections'

const untitled = pages.pages.solo.untitled
async function setup(path = '/') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common, ...pages } } })
  const wrapper = mount(SidebarWorkspaceLists, { attachTo: document.body, global: { plugins: [router, i18n], directives: { 'hover-ink': vHoverInk } } })
  await flushPromises()
  return { wrapper, router }
}

const run = (id: string, status: string, lastActivityAt: string) => ({ id, title: `运行 ${id}`, workflowName: 'wf', status, lastActivityAt })
const solo = (id: string, status: SoloSession['status'], at: Partial<SoloSession> = {}): SoloSession =>
  ({ id, userId: 'u', title: `会话 ${id}`, agentName: 'a', status, restarts: 0, createdAt: '2026-11-01T00:00:00Z', updatedAt: '2026-11-01T00:00:00Z', busy: false, waiting: 0, ...at })
/** History row identities in render order: run ids and `solo:<id>`. */
const order = (w: Awaited<ReturnType<typeof setup>>['wrapper']) =>
  w.findAll('[data-testid="sidebar-recent-run"], [data-testid="sidebar-recent-solo"]').map((x) =>
    x.attributes('data-testid') === 'sidebar-recent-solo' ? `solo:${x.attributes('data-solo-id')}` : x.attributes('data-run-id'))

async function seedSolos(list: SoloSession[]) {
  mocks.listSoloSessions.mockResolvedValue({ sessions: list, maxSandboxes: 5 })
  await useSoloSessions().refresh()
}

beforeEach(async () => {
  vi.clearAllMocks()
  __resetSidebarSectionsForTests()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  mocks.workspace.value = mocks.signedInWorkspace
  mocks.listRuns.mockResolvedValue({
    items: [run('r3', 'completed', '2026-11-03T10:00:00Z'), run('r1', 'failed', '2026-11-03T08:00:00Z')],
    total: 2,
  })
  await seedSolos([
    solo('live', 'running', { updatedAt: '2026-11-03T11:00:00Z' }),
    solo('closed', 'closed', { closedAt: '2026-11-03T09:00:00Z' }),
    solo('broken', 'error', { title: '', updatedAt: '2026-11-03T07:00:00Z' }),
  ])
})
afterEach(() => { vi.useRealTimers(); document.body.replaceChildren() })

describe('sidebar 历史执行 lists ended Solo sessions', () => {
  it('merges closed and error sessions with runs by time, with end marks and /solo links', async () => {
    const { wrapper } = await setup('/')
    expect(order(wrapper)).toEqual(['r3', 'solo:closed', 'r1', 'solo:broken'])
    expect(wrapper.find('[data-solo-id="live"]').exists()).toBe(false)

    const closed = wrapper.get('[data-solo-id="closed"]')
    expect(closed.attributes('href')).toBe('/solo/closed')
    expect(closed.attributes('data-status')).toBe('closed')
    expect(closed.classes()).toContain('workspace-recent-row')
    expect(closed.get('.workspace-list-title').text()).toBe('会话 closed')
    const cancelled = closed.get('[data-testid="sidebar-recent-ended"]')
    expect(cancelled.element.tagName.toLowerCase()).toBe('svg')
    expect(cancelled.classes()).toContain('is-cancelled')
    expect(cancelled.attributes('title')).toBe('已取消')

    const broken = wrapper.get('[data-solo-id="broken"]')
    expect(broken.attributes('href')).toBe('/solo/broken')
    expect(broken.get('.workspace-list-title').text()).toBe(untitled)
    const failed = broken.get('[data-testid="sidebar-recent-ended"]')
    expect(failed.attributes('data-icon')).toBe('alert')
    expect(failed.classes()).toContain('is-failed')
    expect(failed.attributes('title')).toBe('失败')
    wrapper.unmount()
  })

  it('marks the open session row and navigates to it', async () => {
    const { wrapper, router } = await setup('/solo/closed')
    const closed = wrapper.get('[data-solo-id="closed"]')
    expect(closed.classes()).toContain('is-active')
    expect(closed.attributes('aria-current')).toBe('page')
    expect(wrapper.get('[data-solo-id="broken"]').classes()).not.toContain('is-active')
    await wrapper.get('[data-solo-id="broken"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/solo/broken')
    expect(wrapper.emitted('navigate')).toHaveLength(1)
    expect(wrapper.get('[data-solo-id="broken"]').classes()).toContain('is-active')
    wrapper.unmount()
  })

  it('shows Solo history instead of the empty state when there are no finished runs', async () => {
    mocks.listRuns.mockResolvedValue({ items: [], total: 0 })
    const { wrapper } = await setup('/')
    expect(order(wrapper)).toEqual(['solo:closed', 'solo:broken'])
    expect(wrapper.text()).not.toContain(nav.nav.workspaceNoRuns)
    await seedSolos([solo('live', 'running')])
    await flushPromises()
    expect(order(wrapper)).toEqual([])
    expect(wrapper.text()).toContain(nav.nav.workspaceNoRuns)
    wrapper.unmount()
  })

  it('follows the shared store: cancel adds the row, restart and delete take it away', async () => {
    const { wrapper } = await setup('/')
    const store = useSoloSessions()
    store.upsert(solo('live', 'closed', { closedAt: '2026-11-03T12:00:00Z' }))
    await flushPromises()
    expect(order(wrapper)).toEqual(['solo:live', 'r3', 'solo:closed', 'r1', 'solo:broken'])
    store.upsert(solo('live', 'starting', { closedAt: '2026-11-03T12:00:00Z' }))
    await flushPromises()
    expect(order(wrapper)).toEqual(['r3', 'solo:closed', 'r1', 'solo:broken'])
    store.remove('closed')
    await flushPromises()
    expect(order(wrapper)).toEqual(['r3', 'r1', 'solo:broken'])
    expect(mocks.listRuns).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('holds a new Solo row while the pointer is on the list, then inserts it', async () => {
    const { wrapper } = await setup('/')
    const list = wrapper.get('[data-testid="sidebar-recent-list"]').element as HTMLElement
    list.dispatchEvent(new Event('pointerenter'))
    useSoloSessions().upsert(solo('live', 'closed', { closedAt: '2026-11-03T12:00:00Z' }))
    await flushPromises()
    expect(order(wrapper)).toEqual(['r3', 'solo:closed', 'r1', 'solo:broken'])
    list.dispatchEvent(new Event('pointerleave'))
    await flushPromises()
    expect(order(wrapper)).toEqual(['solo:live', 'r3', 'solo:closed', 'r1', 'solo:broken'])
    wrapper.unmount()
  })
})
