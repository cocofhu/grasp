// @vitest-environment happy-dom
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import type { Project, Run } from '@/lib/shared/types'

const api = vi.hoisted(() => ({ listProjects: vi.fn(), listRuns: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api }))

const messages = { nav: {
  projects: '项目', recent: '最近', workspaceLoading: '正在加载…',
  workspaceNoProjects: '还没有项目', workspaceNoRuns: '还没有运行',
  workspaceLoadFailed: '暂时无法加载', workspaceRetry: '重试',
  viewAllProjects: '查看全部项目', viewAllRuns: '查看全部运行',
  expandProject: '展开项目 {name}', collapseProject: '收起项目 {name}',
  projectActions: '项目 {name} 的操作', viewProject: '查看项目', viewProjectRuns: '查看项目运行',
  newProjectRequest: '为 {name} 新建需求',
} }

const project = (id: string): Project => ({ id, name: `项目 ${id}`, description: '', variables: [] })
const run = (id: string, title = `运行 ${id}`): Run => ({
  id, title, workflowId: 'wf-1', workflowName: '工作流名称', status: 'completed',
  trigger: 'manual', startedAt: '2026-10-20T00:00:00Z', durationSec: 0,
  progress: 100, nodeRuns: {}, artifacts: [],
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

const wrappers: ReturnType<typeof mount>[] = []
async function mountLists(path = '/dashboard') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: ['/dashboard', '/projects', '/projects/:id', '/runs', '/runs/:id', '/gates']
      .map((routePath) => ({ path: routePath, component: { template: '<div />' } })),
  })
  await router.push(path)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': messages } })
  const wrapper = mount(SidebarWorkspaceLists, { attachTo: document.body, global: { plugins: [router, i18n] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  vi.resetAllMocks()
  api.listProjects.mockResolvedValue([])
  api.listRuns.mockResolvedValue({ items: [] })
})
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  document.body.innerHTML = ''
})

describe('SidebarWorkspaceLists', () => {
  it('limits real rows and fetches project runs only on expansion, caching one accordion at a time', async () => {
    api.listProjects.mockResolvedValue(Array.from({ length: 8 }, (_, i) => project(`p${i}`)))
    api.listRuns.mockImplementation((params: { projectId?: string }) => Promise.resolve({
      items: params.projectId === 'p0'
        ? [run('p0-first'), run('p0-second'), run('p0-third')]
        : params.projectId ? [] : Array.from({ length: 8 }, (_, i) => run(`r${i}`)),
    }))
    const { wrapper } = await mountLists()
    expect(wrapper.findAll('[data-testid="sidebar-project"]')).toHaveLength(6)
    expect(wrapper.findAll('[data-testid="sidebar-recent-run"]')).toHaveLength(6)
    expect(wrapper.findAll('[data-testid="sidebar-project-run"]')).toHaveLength(0)
    expect(api.listProjects).toHaveBeenCalledTimes(1)
    expect(api.listRuns).toHaveBeenCalledTimes(1)
    expect(api.listRuns).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 20, order: 'desc' }))

    const rows = wrapper.findAll('[data-testid="sidebar-project"]')
    await rows[0]!.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).toHaveBeenLastCalledWith(expect.objectContaining({ projectId: 'p0', pageSize: 2 }))
    expect(rows[0]!.findAll('[data-testid="sidebar-project-run"]')).toHaveLength(2)
    expect(wrapper.emitted('navigate')).toBeUndefined()
    await rows[0]!.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await rows[0]!.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).toHaveBeenCalledTimes(2)
    expect(rows[0]!.get('[data-testid="sidebar-project-toggle"]').attributes('aria-expanded')).toBe('true')

    await rows[1]!.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(rows[0]!.get('[data-testid="sidebar-project-toggle"]').attributes('aria-expanded')).toBe('false')
    expect(rows[1]!.text()).toContain('还没有运行')
    expect(wrapper.findAll('[data-testid="sidebar-project-run"]')).toHaveLength(0)
  })

  it('keeps query-based active states, safe titles and navigation events', async () => {
    api.listProjects.mockResolvedValue([project('p1')])
    api.listRuns.mockResolvedValue({ items: [run('r1', '{"secret":"raw JSON"}')] })
    const { wrapper, router } = await mountLists('/gates?run=r1&node=clarify')
    const recent = wrapper.get('[data-testid="sidebar-recent-run"]')
    expect(recent.attributes('aria-current')).toBe('page')
    expect(recent.text()).toBe('工作流名称')
    expect(recent.attributes('title')).toBe('工作流名称')
    expect(wrapper.text()).not.toContain('secret')

    const projectLink = wrapper.get('[data-testid="sidebar-project-link"]')
    expect(projectLink.attributes('href')).toBe('/projects/p1')
    await projectLink.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/projects/p1')
    expect(projectLink.attributes('aria-current')).toBe('page')
    expect(wrapper.emitted('navigate')).toHaveLength(1)
    expect(api.listProjects).toHaveBeenCalledTimes(2)
    expect(api.listRuns).toHaveBeenCalledTimes(2)

    await router.push('/runs?projectId=p1')
    await flushPromises()
    expect(projectLink.attributes('aria-current')).toBe('page')
    await wrapper.get('[data-testid="sidebar-recent-run"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/runs/r1')
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').attributes('aria-current')).toBe('page')
    expect(wrapper.emitted('navigate')).toHaveLength(2)
  })

  it('keeps successful data when the other endpoint fails and recovers to a genuine empty state', async () => {
    api.listProjects.mockRejectedValueOnce(new Error('offline'))
    api.listRuns.mockResolvedValue({ items: [run('r1')] })
    const { wrapper } = await mountLists()
    const projectSection = wrapper.get('[data-testid="sidebar-projects"]')
    expect(projectSection.text()).toContain('暂时无法加载')
    expect(projectSection.text()).not.toContain('还没有项目')
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('运行 r1')
    await projectSection.get('button').trigger('click')
    await flushPromises()
    expect(projectSection.text()).toContain('还没有项目')
    expect(projectSection.text()).not.toContain('暂时无法加载')
    expect(wrapper.get('[data-testid="sidebar-workspace-lists"]').attributes('aria-busy')).toBe('false')
  })

  it('retries a failed project independently without treating failure as no runs', async () => {
    api.listProjects.mockResolvedValue([project('p1')])
    api.listRuns.mockImplementation((params: { projectId?: string }) => params.projectId
      ? Promise.reject(new Error('offline')) : Promise.resolve({ items: [] }))
    const { wrapper } = await mountLists()
    const row = wrapper.get('[data-testid="sidebar-project"]')
    await row.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(row.text()).toContain('暂时无法加载')
    expect(row.text()).not.toContain('还没有运行')
    api.listRuns.mockResolvedValueOnce({ items: [run('project-run')] })
    await row.get('.workspace-list-retry').trigger('click')
    await flushPromises()
    expect(row.get('[data-testid="sidebar-project-run"]').text()).toBe('运行 project-run')
    expect(api.listProjects).toHaveBeenCalledTimes(1)
    expect(api.listRuns).toHaveBeenCalledTimes(3)
  })

  it('updates recent and cached project titles together without another API request', async () => {
    api.listProjects.mockResolvedValue([project('p1')])
    api.listRuns.mockResolvedValue({ items: [run('shared', '原来的运行名')] })
    const { wrapper } = await mountLists()
    await wrapper.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-project-run"]').text()).toBe('原来的运行名')
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('原来的运行名')

    await wrapper.setProps({ runTitles: { shared: '重命名后的运行' } })
    for (const testId of ['sidebar-project-run', 'sidebar-recent-run']) {
      const row = wrapper.get(`[data-testid="${testId}"]`)
      expect(row.text()).toBe('重命名后的运行')
      expect(row.attributes('title')).toBe('重命名后的运行')
    }
    await wrapper.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await wrapper.get('[data-testid="sidebar-project-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-project-run"]').text()).toBe('重命名后的运行')
    expect(api.listProjects).toHaveBeenCalledTimes(1)
    expect(api.listRuns).toHaveBeenCalledTimes(2)
  })

  it('refreshes only the open project on navigation and invalidates collapsed route caches', async () => {
    api.listProjects.mockResolvedValue([project('p1'), project('p2')])
    api.listRuns.mockImplementation((params: { projectId?: string }) => Promise.resolve({
      items: params.projectId ? [run(`${params.projectId}-old`)] : [],
    }))
    const { wrapper, router } = await mountLists()
    const toggle = (index: number) => wrapper.findAll('[data-testid="sidebar-project"]')[index]!.get('[data-testid="sidebar-project-toggle"]')
    await toggle(1).trigger('click')
    await flushPromises()
    await toggle(0).trigger('click')
    await flushPromises()
    const refreshed = deferred<{ items: Run[] }>()
    api.listRuns.mockImplementation((params: { projectId?: string }) => params.projectId === 'p1'
      ? refreshed.promise : Promise.resolve({ items: [] }))

    await router.push('/runs')
    await flushPromises()
    const openProject = wrapper.findAll('[data-testid="sidebar-project"]')[0]!
    expect(openProject.text()).toContain('正在加载')
    expect(openProject.text()).not.toContain('还没有运行')
    expect(api.listRuns.mock.calls.filter(([params]) => params.projectId === 'p1')).toHaveLength(2)
    expect(api.listRuns.mock.calls.filter(([params]) => params.projectId === 'p2')).toHaveLength(1)
    refreshed.resolve({ items: [run('p1-new')] })
    await flushPromises()
    expect(openProject.get('[data-testid="sidebar-project-run"]').text()).toBe('运行 p1-new')

    api.listRuns.mockResolvedValueOnce({ items: [run('p2-new')] })
    await toggle(1).trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-project-run"]').text()).toBe('运行 p2-new')
    expect(api.listRuns.mock.calls.filter(([params]) => params.projectId === 'p2')).toHaveLength(2)
  })

  it('opens real project destinations and composes a request in the selected project', async () => {
    api.listProjects.mockResolvedValue([project('p1')])
    const { wrapper, router } = await mountLists()
    const compose = wrapper.get('[data-testid="sidebar-project-compose"]')
    const composeUrl = new URL(compose.attributes('href'), 'http://localhost')
    expect(composeUrl.pathname).toBe('/dashboard')
    expect(composeUrl.searchParams.get('projectId')).toBe('p1')
    await compose.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.projectId).toBe('p1')
    expect(wrapper.emitted('navigate')).toHaveLength(1)

    const trigger = wrapper.get('[data-testid="sidebar-project-menu-trigger"]')
    await trigger.trigger('click')
    await flushPromises()
    let menu = new DOMWrapper(document.querySelector('[data-testid="sidebar-project-menu"]')!)
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-controls')).toBe(menu.attributes('id'))
    expect(menu.findAll('[role="menuitem"]')).toHaveLength(2)
    expect(menu.get('[data-testid="sidebar-project-menu-open"]').attributes('href')).toBe('/projects/p1')
    await menu.get('[data-testid="sidebar-project-menu-open"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/projects/p1')
    expect(document.querySelector('[data-testid="sidebar-project-menu"]')).toBeNull()

    await trigger.trigger('click')
    await flushPromises()
    menu = new DOMWrapper(document.querySelector('[data-testid="sidebar-project-menu"]')!)
    await menu.get('[data-testid="sidebar-project-menu-runs"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/runs')
    expect(router.currentRoute.value.query.projectId).toBe('p1')
    expect(wrapper.emitted('navigate')).toHaveLength(3)
    expect(wrapper.get('[data-testid="sidebar-project-toggle"]').attributes('aria-expanded')).toBe('false')
  })

  it('keeps the menu in the viewport and supports arrow keys, Escape, Tab and outside dismissal', async () => {
    api.listProjects.mockResolvedValue([project('p1')])
    const { wrapper } = await mountLists()
    const trigger = wrapper.get('[data-testid="sidebar-project-menu-trigger"]')
    trigger.element.getBoundingClientRect = () => new DOMRect(window.innerWidth - 20, window.innerHeight - 20, 24, 24)
    await trigger.trigger('click')
    await flushPromises()
    let menu = new DOMWrapper(document.querySelector('[data-testid="sidebar-project-menu"]')!)
    const panel = menu.element as HTMLElement
    expect(Number.parseFloat(panel.style.left)).toBeGreaterThanOrEqual(8)
    expect(Number.parseFloat(panel.style.left) + 210).toBeLessThanOrEqual(window.innerWidth - 8)
    expect(Number.parseFloat(panel.style.top) + 80).toBeLessThanOrEqual(window.innerHeight - 8)
    const items = menu.findAll('[role="menuitem"]')
    expect(document.activeElement).toBe(items[0]!.element)
    await items[0]!.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[1]!.element)
    await items[1]!.trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(items[0]!.element)
    await items[0]!.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(document.querySelector('[data-testid="sidebar-project-menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await flushPromises()
    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="sidebar-project-menu"]')).toBeNull()

    await trigger.trigger('click')
    await flushPromises()
    menu = new DOMWrapper(document.querySelector('[data-testid="sidebar-project-menu"]')!)
    await menu.get('[role="menuitem"]').trigger('keydown', { key: 'Tab' })
    await flushPromises()
    expect(document.querySelector('[data-testid="sidebar-project-menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    expect(wrapper.emitted('navigate')).toBeUndefined()
  })

  it('aborts stale route requests and ignores late responses after refresh and unmount', async () => {
    const oldProjects = deferred<Project[]>()
    const oldRuns = deferred<{ items: Run[] }>()
    api.listProjects.mockReturnValueOnce(oldProjects.promise).mockResolvedValue([project('new')])
    api.listRuns.mockReturnValueOnce(oldRuns.promise).mockResolvedValue({ items: [run('new')] })
    const { wrapper, router } = await mountLists()
    expect(wrapper.text()).toContain('正在加载')
    const firstSignal = api.listRuns.mock.calls[0]![0].signal as AbortSignal
    await router.push('/runs')
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    oldProjects.resolve([project('old')])
    oldRuns.resolve({ items: [run('old')] })
    await flushPromises()
    expect(wrapper.text()).toContain('项目 new')
    expect(wrapper.text()).not.toContain('项目 old')
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('运行 new')

    const late = deferred<{ items: Run[] }>()
    api.listRuns.mockReturnValueOnce(late.promise)
    await router.push('/gates')
    await flushPromises()
    const lastSignal = api.listRuns.mock.lastCall![0].signal as AbortSignal
    wrapper.unmount()
    expect(lastSignal.aborted).toBe(true)
    late.resolve({ items: [run('late')] })
    await flushPromises()
  })
})
