// @vitest-environment happy-dom
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  const signedInWorkspace = { name: 'Workspace', description: '', variables: [] }
  return { signedInWorkspace, workspace: ref<typeof signedInWorkspace | null>(signedInWorkspace), entries: ref<{ workflowId: string; favoritedAt: number }[]>([]), listWorkflowCatalog: vi.fn(), listRuns: vi.fn(), getWorkflow: vi.fn(), openLaunch: vi.fn(), error: vi.fn() }
})
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, loading: ref(false), ensureLoaded: async () => mocks.workspace.value }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ error: mocks.error }) }))
vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({ useWorkflowRunLaunch: () => ({ openLaunch: mocks.openLaunch }) }))
vi.mock('@/lib/run/useWorkflowFavorites', () => ({ useWorkflowFavorites: () => ({ entries: mocks.entries, isFavorite: (id: string) => mocks.entries.value.some((item) => item.workflowId === id), toggleFavorite: (id: string) => { mocks.entries.value = mocks.entries.value.some((item) => item.workflowId === id) ? mocks.entries.value.filter((item) => item.workflowId !== id) : [{ workflowId: id, favoritedAt: Date.now() }, ...mocks.entries.value] } }) }))
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import { workflowCatalogRevision } from '@/lib/api/workflowCatalogEvents'
const catalog = Array.from({ length: 14 }, (_, index) => ({ id: `wf-${index}`, name: `流程 ${index}`, description: `说明 ${index}`, status: index % 2 ? 'draft' : 'published', version: 2, publishedVersion: 1, updatedAt: `2026-10-${String(20 - index).padStart(2, '0')}` }))
async function setup(path = '/workflows/wf-3/edit') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common } } })
  const wrapper = mount(SidebarWorkspaceLists, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.workspace.value = mocks.signedInWorkspace
  mocks.entries.value = []
  mocks.listWorkflowCatalog.mockResolvedValue(catalog)
  mocks.listRuns.mockResolvedValue({ items: [{ id: 'r1', title: '最近一次运行', workflowName: '工作流' }], total: 1 })
  mocks.openLaunch.mockResolvedValue(undefined)
})

describe('complete workspace workflow directory', () => {
  it('lists more than six/eight workflows directly without sidebar search', async () => {
    const { wrapper } = await setup()
    expect(wrapper.findAll('[data-testid="sidebar-workflow"]')).toHaveLength(14)
    expect(wrapper.find('input[type="search"]').exists()).toBe(false)
    expect(wrapper.get('[aria-current="page"]').attributes('href')).toBe('/workflows/wf-3/edit')
    expect(wrapper.text()).toContain('草稿')
    expect(wrapper.get('[data-testid="sidebar-recent-run"]').text()).toBe('最近一次运行')
    expect(mocks.listWorkflowCatalog).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })
    expect(mocks.getWorkflow).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('preserves personal pinned ordering while retaining every unpinned workflow', async () => {
    mocks.entries.value = [{ workflowId: 'wf-12', favoritedAt: 2 }, { workflowId: 'wf-8', favoritedAt: 1 }]
    const { wrapper } = await setup()
    const rows = () => wrapper.findAll('[data-testid="sidebar-workflow"]')
    expect(rows()[0].text()).toContain('流程 12')
    expect(rows()[1].text()).toContain('流程 8')
    await rows()[0].find('[aria-pressed]').trigger('click')
    expect(rows()[0].text()).toContain('流程 8')
    expect(rows()).toHaveLength(14)
    wrapper.unmount()
  })
  it('loads graph details only for an explicit manual run and uses the saved draft head', async () => {
    const head = { ...catalog[3], nodes: [{ id: 'head-input' }], edges: [], publishedSnapshot: { nodes: [{ id: 'published-input' }] } }
    mocks.getWorkflow.mockResolvedValue(head)
    const { wrapper } = await setup()
    await wrapper.findAll('[data-testid="sidebar-workflow-run"]')[3].trigger('click')
    await flushPromises()
    expect(mocks.getWorkflow).toHaveBeenCalledWith('wf-3')
    expect(mocks.openLaunch).toHaveBeenCalledWith(head)
    expect(wrapper.emitted('navigate')).toHaveLength(1)
    wrapper.unmount()
  })
  it('keeps a refresh failure distinct from empty and retries without losing recents', async () => {
    mocks.listWorkflowCatalog.mockRejectedValueOnce(new Error('offline'))
    const { wrapper } = await setup()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('.workspace-empty').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="sidebar-recent-run"]')).toHaveLength(1)
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="sidebar-workflow"]')).toHaveLength(14)
    mocks.listWorkflowCatalog.mockRejectedValueOnce(new Error('refresh failed'))
    workflowCatalogRevision.value++
    await flushPromises()
    expect(wrapper.findAll('[data-testid="sidebar-workflow"]')).toHaveLength(14)
    wrapper.unmount()
  })
  it('rejects stale catalog results after signing out', async () => {
    let done!: (value: typeof catalog) => void
    mocks.listWorkflowCatalog.mockReturnValueOnce(new Promise((resolve) => { done = resolve }))
    const { wrapper } = await setup()
    const signal = mocks.listWorkflowCatalog.mock.calls[0][0].signal as AbortSignal
    mocks.workspace.value = null
    await flushPromises()
    done(catalog)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.findAll('[data-testid="sidebar-workflow"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="sidebar-recent-run"]')).toHaveLength(0)
    wrapper.unmount()
  })
})
