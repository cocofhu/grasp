// @vitest-environment happy-dom
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
    listWorkflowCatalog: vi.fn(),
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
import { readArtifactPanelOpen, resetConversationDrawers, writeArtifactPanelOpen } from '@/lib/chat/conversationDrawer'

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
  vi.clearAllMocks()
  mocks.workspace.value = mocks.signedInWorkspace
  mocks.entries.value = []
  mocks.listWorkflowCatalog.mockResolvedValue([])
  mocks.listRuns.mockResolvedValue({ items: [recentRun], total: 1 })
  mocks.openLaunch.mockResolvedValue(undefined)
})

describe('sidebar workspace lists without a workflow directory', () => {
  it('shows recent runs and does not request or render the workflow directory', async () => {
    const items = Array.from({ length: 8 }, (_, index) => ({ id: `r${index}`, title: `运行 ${index}`, workflowName: '工作流' }))
    mocks.listRuns.mockResolvedValue({ items, total: items.length })
    const { wrapper } = await setup('/runs/r0')
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[data-testid="sidebar-recent"]').text()).toContain('最近')
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(rows).toHaveLength(6)
    expect(rows[0].text()).toBe('运行 0')
    expect(rows[0].attributes('href')).toBe('/runs/r0')
    expect(rows[0].attributes('aria-current')).toBe('page')
    expect(mocks.listRuns).toHaveBeenCalledWith({ page: 1, pageSize: 6, sort: 'last_activity', order: 'desc', signal: expect.any(AbortSignal) })
    expect(mocks.listWorkflowCatalog).not.toHaveBeenCalled()
    expect(mocks.getWorkflow).not.toHaveBeenCalled()
    expect(mocks.openLaunch).not.toHaveBeenCalled()
    expect(mocks.toggleFavorite).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps the recent empty state without a workflow directory', async () => {
    mocks.listRuns.mockResolvedValue({ items: [], total: 0 })
    const { wrapper } = await setup()
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[data-testid="sidebar-recent"]').text()).toContain('还没有运行')
    expect(wrapper.find('[data-testid="sidebar-recent-run"]').exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(mocks.listWorkflowCatalog).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('retries a recent-run failure without revealing a workflow directory', async () => {
    mocks.listRuns.mockRejectedValueOnce(new Error('offline'))
    const { wrapper } = await setup()
    expectNoWorkflowDirectory(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('暂时无法加载')
    expect(wrapper.text()).not.toContain('还没有运行')
    expect(wrapper.find('[data-testid="sidebar-recent-run"]').exists()).toBe(false)
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('最近一次运行')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expectNoWorkflowDirectory(wrapper)
    expect(mocks.listWorkflowCatalog).not.toHaveBeenCalled()
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
    expect(mocks.listWorkflowCatalog).not.toHaveBeenCalled()
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
})
