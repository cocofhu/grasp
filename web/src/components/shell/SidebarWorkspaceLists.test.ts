// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  const signedInWorkspace = { name: 'Workspace', description: '', variables: [] }
  return {
    signedInWorkspace,
    workspace: ref<typeof signedInWorkspace | null>(signedInWorkspace),
    entries: ref<{ workflowId: string; favoritedAt: number }[]>([]),
   
    listRuns: vi.fn(),
    getWorkflow: vi.fn(),
    openLaunch: vi.fn(),
    toggleFavorite: vi.fn(),
    error: vi.fn(),
  }
})
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, loading: ref(false), ensureLoaded: async () => mocks.workspace.value }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ error: mocks.error }) }))
vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({ useWorkflowRunLaunch: () => ({ openLaunch: mocks.openLaunch }) }))
vi.mock('@/lib/run/useWorkflowFavorites', () => ({ useWorkflowFavorites: () => ({ entries: mocks.entries, isFavorite: (id: string) => mocks.entries.value.some((item) => item.workflowId === id), toggleFavorite: mocks.toggleFavorite }) }))
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'

const shellDir = dirname(fileURLToPath(import.meta.url))
const listSrc = readFileSync(join(shellDir, 'SidebarWorkspaceLists.vue'), 'utf8')
import { readArtifactPanelOpen, resetConversationDrawers, writeArtifactPanelOpen } from '@/lib/chat/conversationDrawer'
import { __resetSidebarSectionsForTests } from '@/lib/shared/sidebarSections'

const recentRun = { id: 'r1', title: '最近一次运行', workflowName: '工作流' }
function expectNoWorkflowDirectory(wrapper: VueWrapper) {
  expect(wrapper.find('[data-testid="sidebar-workflows"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="sidebar-workflow"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="sidebar-workflow-list"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="sidebar-workflow-run"]').exists()).toBe(false)
  expect(wrapper.find('.workspace-catalog-skeleton').exists()).toBe(false)
  expect(wrapper.find('.workspace-empty').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('新建工作流')
  expect(wrapper.text()).not.toContain('置顶')
}
async function setup(path = '/dashboard') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common } } })
  const wrapper = mount(SidebarWorkspaceLists, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  resetConversationDrawers()
  __resetSidebarSectionsForTests()
  vi.clearAllMocks()
  mocks.workspace.value = mocks.signedInWorkspace
  mocks.entries.value = []
  mocks.listRuns.mockResolvedValue({ items: [recentRun], total: 1 })
  mocks.openLaunch.mockResolvedValue(undefined)
})

describe('sidebar workspace lists without a workflow directory', () => {
  it('shows recent runs and does not request or render the workflow directory', async () => {
    const items = Array.from({ length: 24 }, (_, index) => ({ id: `r${index}`, title: `运行 ${index}`, workflowName: '工作流' }))
    mocks.listRuns.mockResolvedValue({ items, total: items.length })
    const { wrapper } = await setup('/runs/r0')
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[data-testid="sidebar-recent"]').text()).toContain('历史执行')
    const heading = wrapper.get('[data-testid="sidebar-recent-toggle"]')
    expect(heading.classes()).toContain('grasp-status-host')
    const headingIcon = heading.get('[data-icon="history-back"]')
    expect(headingIcon.classes()).toContain('grasp-status')
    expect(headingIcon.attributes('width')).toBe('14')
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(rows).toHaveLength(20)
    expect(rows[0].text()).toBe('运行 0')
    expect(rows[19].attributes('data-run-id')).toBe('r19')
    expect(wrapper.find('[data-run-id="r20"]').exists()).toBe(false)
    expect(rows[0].attributes('href')).toBe('/runs/r0')
    expect(rows[0].classes()).toContain('is-active')
    expect(rows[0].attributes('aria-current')).toBe('page')
    expect(rows[0].find('[data-icon="history"]').exists()).toBe(false)
    expect(rows[0].element.firstElementChild).toBe(rows[0].get('.workspace-list-title').element)
    expect(rows[0].find('[data-testid="sidebar-recent-ended"]').exists()).toBe(false)
    expect(wrapper.find('[data-icon="chat"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-recent-type-icon"]').exists()).toBe(false)
    expect(mocks.listRuns).toHaveBeenCalledWith({ page: 1, pageSize: 20, status: 'completed,failed,cancelled', sort: 'last_activity', order: 'desc', signal: expect.any(AbortSignal) })
    expect(mocks.getWorkflow).not.toHaveBeenCalled()
    expect(mocks.openLaunch).not.toHaveBeenCalled()
    expect(mocks.toggleFavorite).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders every finished run when fewer than twenty are returned', async () => {
    const items = Array.from({ length: 8 }, (_, index) => ({ id: `r${index}`, title: `运行 ${index}`, workflowName: '工作流' }))
    mocks.listRuns.mockResolvedValue({ items, total: items.length })
    const { wrapper } = await setup()
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(rows).toHaveLength(items.length)
    expect(rows.map((row) => row.attributes('data-run-id'))).toEqual(items.map((item) => item.id))
    expect(wrapper.get('[data-testid="sidebar-recent"]').text()).not.toContain('还没有执行完的工作流')
    expect(mocks.listRuns).toHaveBeenCalledWith({ page: 1, pageSize: 20, status: 'completed,failed,cancelled', sort: 'last_activity', order: 'desc', signal: expect.any(AbortSignal) })
    wrapper.unmount()
  })

  it('renders all twenty finished runs when the page is exactly full', async () => {
    const items = Array.from({ length: 20 }, (_, index) => ({ id: `r${index}`, title: `运行 ${index}`, workflowName: '工作流' }))
    mocks.listRuns.mockResolvedValue({ items, total: items.length })
    const { wrapper } = await setup()
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(rows).toHaveLength(20)
    expect(rows[19].attributes('data-run-id')).toBe('r19')
    wrapper.unmount()
  })

  it('keeps the recent empty state without a workflow directory', async () => {
    mocks.listRuns.mockResolvedValue({ items: [], total: 0 })
    const { wrapper } = await setup()
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[data-testid="sidebar-recent"]').text()).toContain('还没有执行完的工作流')
    expect(wrapper.find('[data-testid="sidebar-recent-run"]').exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('retries a recent-run failure without revealing a workflow directory', async () => {
    mocks.listRuns.mockRejectedValueOnce(new Error('offline'))
    const { wrapper } = await setup()
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('暂时无法加载')
    expect(wrapper.text()).not.toContain('还没有执行完的工作流')
    expect(wrapper.find('[data-testid="sidebar-recent-run"]').exists()).toBe(false)
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('最近一次运行')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expectNoWorkflowDirectory(wrapper)
    expect(mocks.listRuns).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('discards stale recent runs after signing out and still skips the workflow directory', async () => {
    let done!: (value: { items: typeof recentRun[]; total: number }) => void
    mocks.listRuns.mockReturnValueOnce(new Promise((resolve) => { done = resolve }))
    const { wrapper } = await setup()
    const signal = mocks.listRuns.mock.calls[0][0].signal as AbortSignal
    mocks.workspace.value = null
    await flushPromises()
    done({ items: [recentRun], total: 1 })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.findAll('[data-testid="sidebar-recent-run"]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('does not rewrite the artifact panel switch when a recent run is opened', async () => {
    writeArtifactPanelOpen(true)
    const { wrapper } = await setup('/dashboard')
    await wrapper.get('[data-testid="sidebar-recent-run"]').trigger('click')
    expect(readArtifactPanelOpen()).toBe(true)
    writeArtifactPanelOpen(false)
    await wrapper.get('[data-testid="sidebar-recent-run"]').trigger('click')
    expect(readArtifactPanelOpen()).toBe(false)
    wrapper.unmount()
  })

  it('makes the whole history title one collapse button with no link to /runs', async () => {
    const { wrapper, router } = await setup('/dashboard')
    const title = wrapper.get('[data-icon-motion-span="history-title"]')
    const toggle = title.get('[data-testid="sidebar-recent-toggle"]')
    const body = wrapper.get('[data-testid="sidebar-recent-body"]')
    expect(wrapper.find('[data-testid="sidebar-recent-heading"]').exists()).toBe(false)
    expect(title.find('a').exists()).toBe(false)
    expect(title.find('a[href="/runs"]').exists()).toBe(false)
    expect(title.findAll('button')).toHaveLength(1)
    expect(toggle.classes()).toContain('grasp-status-host')
    expect(toggle.element.tagName).toBe('BUTTON')
    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.element.tabIndex).not.toBe(-1)
    expect(toggle.text()).toBe('历史执行')
    expect(toggle.find('[data-icon="history-back"]').exists()).toBe(true)
    expect(toggle.find('.workspace-list-chevron').exists()).toBe(true)
    expect(toggle.attributes('aria-label')).toBe('折叠历史执行')
    expect(body.element.parentElement).not.toBe(title.element)

    const style = listSrc.slice(listSrc.indexOf('<style scoped>'))
    expect(style).toMatch(/\.workspace-list-heading\s*\{[^}]*font-size:\s*11\.5px/)
    expect(style).toMatch(/\.workspace-list-heading\s*\{[^}]*font-weight:\s*600/)
    expect(style).toMatch(/\.workspace-list-heading\s*\{[^}]*transition:\s*color 160ms ease/)
    expect(style).toMatch(/\.workspace-list-heading:hover\s*\{[^}]*color:\s*rgb\(var\(--c-txt\)\)/)
    expect(style).toMatch(/\.workspace-list-heading:hover \.workspace-list-chevron\s*\{[^}]*opacity:\s*1/)
    expect(style).toMatch(/\.workspace-list-chevron\s*\{[^}]*opacity:\s*\.7/)
    expect(style).not.toMatch(/\.workspace-list-toggle/)
    expect(style).not.toMatch(/\.workspace-list-heading > a:first-child/)
    expect(style).toMatch(/\.workspace-list-label\s*\{[^}]*gap:\s*6px[^}]*min-height:\s*30px[^}]*border:\s*0[^}]*background:\s*transparent/)
    expect(style).toMatch(/a:focus-visible,\s*button:focus-visible/)
    expect(style).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*transition:\s*none/)

    expect((body.element as HTMLElement).style.display).not.toBe('none')
    await toggle.trigger('click')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(wrapper.emitted('navigate')).toBeFalsy()
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.get('.workspace-list-chevron').classes()).toContain('is-collapsed')
    expect((body.element as HTMLElement).style.display).toBe('none')

    expect(toggle.attributes('aria-label')).toBe('展开历史执行')

    await toggle.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(wrapper.emitted('navigate')).toBeFalsy()
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect((body.element as HTMLElement).style.display).not.toBe('none')
    wrapper.unmount()
  })

  it('collapses from a click on the title text or icon without navigating', async () => {
    const { wrapper, router } = await setup('/dashboard')
    const toggle = wrapper.get('[data-testid="sidebar-recent-toggle"]')
    const body = wrapper.get('[data-testid="sidebar-recent-body"]')

    await toggle.get('span').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(wrapper.emitted('navigate')).toBeFalsy()
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect((body.element as HTMLElement).style.display).toBe('none')

    await toggle.get('[data-icon="history-back"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(wrapper.emitted('navigate')).toBeFalsy()
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect((body.element as HTMLElement).style.display).not.toBe('none')
    wrapper.unmount()
  })

  it('uses the same history title in the desktop sidebar and the narrow drawer', () => {
    const sidebar = readFileSync(join(shellDir, 'AppSidebar.vue'), 'utf8')
    const shell = readFileSync(join(shellDir, 'AppShell.vue'), 'utf8')
    const nav = readFileSync(join(shellDir, 'AppSidebarNav.vue'), 'utf8')
    expect(sidebar).toMatch(/<AppSidebarNav[^>]*:workspace-lists="!isMobile"/)
    expect(shell).toMatch(/data-testid="mobile-nav-drawer"[\s\S]*?<AppSidebarNav workspace-lists/)
    expect(nav).toMatch(/<SidebarWorkspaceLists v-if="workspaceLists"/)
    expect(listSrc).toContain('data-icon-motion-span="history-title"')
    expect(listSrc).not.toContain('data-testid="sidebar-recent-heading"')
    expect(listSrc).toContain('data-testid="sidebar-recent-toggle"')
  })
})
