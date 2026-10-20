// @vitest-environment happy-dom
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
const mocks = vi.hoisted(() => ({ listWorkflows: vi.fn(), getWorkflow: vi.fn(), createWorkflowFromBaseline: vi.fn(), copyPreviewWorkflow: vi.fn(), deleteWorkflow: vi.fn(), patchWorkflowHomeVisibility: vi.fn(), patchWorkflowNotifyPolicy: vi.fn(), error: vi.fn(), success: vi.fn() }))
const workspaceId = ref('original-id')
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspaceId, ensureLoaded: async () => workspaceId.value ? { id: workspaceId.value } : null }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ error: mocks.error, success: mocks.success, warn: vi.fn() }) }))
vi.mock('@/lib/composables/useBreakpoint', () => ({ useBreakpoint: () => ({ isMobile: ref(false) }) }))
vi.mock('@/lib/run/useWorkflowImport', () => ({ useWorkflowImport: () => ({ fileInput: ref(null), triggerImport: vi.fn(), handleFileChange: vi.fn() }) }))
vi.mock('@/lib/run/useWorkflowFavorites', () => ({ useWorkflowFavorites: () => ({ isFavorite: () => false, toggleFavorite: vi.fn() }) }))
import { useWorkflows } from './useWorkflows'
const item = { id: 'wf-one', projectId: 'original-id', name: 'One', description: '', status: 'draft' as const, version: 2, publishedVersion: 1, updatedAt: '', nodes: [], edges: [], showOnHome: false, notifyPolicy: { mode: 'inherit' as const, events: ['failed'] } }
async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push('/workflows')
  let logic!: ReturnType<typeof useWorkflows>
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(defineComponent({ setup() { logic = useWorkflows(); return () => null } }), { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { logic, router, wrapper }
}
beforeEach(() => { vi.clearAllMocks(); workspaceId.value = 'original-id'; mocks.listWorkflows.mockResolvedValue([{ ...item }]); mocks.getWorkflow.mockResolvedValue(item) })
describe('workspace workflow actions', () => {
  it('creates from baseline in the workspace without a project choice and opens scratch with a clean URL', async () => {
    const { logic, router, wrapper } = await setup()
    mocks.createWorkflowFromBaseline.mockResolvedValue({ ...item, id: 'new-id', name: 'New' })
    logic.openBaselineModal()
    logic.baselineName.value = ' New '
    logic.baselineRepos.value = [{ name: 'repo', url: 'https://example.com/repo.git', branch: 'main' }]
    await logic.createFromBaseline()
    expect(mocks.createWorkflowFromBaseline).toHaveBeenCalledWith('original-id', 'New', logic.baselineRepos.value)
    expect(logic.workflows.value.map((row) => row.id)).toEqual(['new-id', 'wf-one'])
    expect(logic.baselineModalOpen.value).toBe(false)
    logic.newWorkflow()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/workflows/new/edit')
    wrapper.unmount()
  })
  it('preserves copy naming preview and updates the directory without duplicate rows', async () => {
    const { logic, wrapper } = await setup()
    mocks.copyPreviewWorkflow.mockResolvedValue({ sourceId: 'wf-one', sourceName: 'One', suggestedName: 'One copy' })
    await logic.openCopy(item)
    expect(logic.copyModal.value?.suggestedName).toBe('One copy')
    const copy = { ...item, id: 'copied', name: 'One copy' }
    logic.onCopied(copy)
    logic.onCopied(copy)
    expect(logic.workflows.value.filter((row) => row.id === 'copied')).toHaveLength(1)
    logic.openExport(item)
    expect(logic.exportTarget.value?.id).toBe('wf-one')
    wrapper.unmount()
  })
  it('does not discard a workflow when deletion fails, and removes it only after successful deletion', async () => {
    const { logic, wrapper } = await setup()
    mocks.deleteWorkflow.mockRejectedValueOnce(new Error('Still in use')).mockResolvedValueOnce({ status: 'ok' })
    logic.openDeleteWf(item)
    await logic.confirmDeleteWf()
    expect(logic.deleteWfError.value).toBe('Still in use')
    expect(logic.workflows.value).toHaveLength(1)
    expect(logic.deleteWfTarget.value?.id).toBe('wf-one')
    await logic.confirmDeleteWf()
    expect(logic.workflows.value).toHaveLength(0)
    expect(logic.deleteWfTarget.value).toBeNull()
    wrapper.unmount()
  })
  it('rolls back a failed homepage visibility update and uses the narrow policy endpoint', async () => {
    const { logic, wrapper } = await setup()
    mocks.patchWorkflowHomeVisibility.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ ...item, showOnHome: true })
    await logic.toggleWorkflowShowOnHome(item, true)
    expect(logic.workflows.value[0].showOnHome).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith('offline')
    await logic.toggleWorkflowShowOnHome(item, true)
    expect(logic.workflows.value[0].showOnHome).toBe(true)
    expect(mocks.patchWorkflowHomeVisibility).toHaveBeenLastCalledWith('wf-one', true)
    wrapper.unmount()
  })
  it('changes notify inheritance and events without sending cached graph data', async () => {
    const { logic, wrapper } = await setup()
    mocks.patchWorkflowNotifyPolicy.mockImplementation(async (_id, notifyPolicy) => ({ ...item, notifyPolicy }))
    await logic.setWorkflowNotifyMode(item, 'custom')
    expect(mocks.patchWorkflowNotifyPolicy).toHaveBeenCalledWith('wf-one', { mode: 'custom', events: ['failed'] })
    await logic.toggleWorkflowNotifyEvent(logic.workflows.value[0], 'completed')
    expect(mocks.patchWorkflowNotifyPolicy).toHaveBeenLastCalledWith('wf-one', { mode: 'custom', events: ['failed', 'completed'] })
    expect(logic.workflows.value[0].notifyPolicy?.events).toEqual(['failed', 'completed'])
    wrapper.unmount()
  })
  it('does not open a run when fresh head loading fails', async () => {
    const { logic, wrapper } = await setup()
    mocks.getWorkflow.mockRejectedValueOnce(new Error('Workflow removed'))
    await logic.openRun(item)
    expect(logic.runTarget.value).toBeNull()
    expect(mocks.error).toHaveBeenCalledWith('Workflow removed')
    wrapper.unmount()
  })
  it('clears private launch inputs on logout and ignores pending actions from the previous account', async () => {
    const { logic, wrapper } = await setup()
    let resolveHead!: (value: typeof item) => void
    let resolveCopy!: (value: { sourceId: string; sourceName: string; suggestedName: string }) => void
    mocks.getWorkflow.mockReturnValueOnce(new Promise((resolve) => { resolveHead = resolve }))
    mocks.copyPreviewWorkflow.mockReturnValueOnce(new Promise((resolve) => { resolveCopy = resolve }))
    logic.runInputs.value = { token: 'private input' }
    logic.runImages.value = { upload: [{ dataUrl: 'private image' } as never] }
    logic.baselineName.value = 'private name'
    const launch = logic.openRun(item)
    const copy = logic.openCopy(item)
    workspaceId.value = ''
    expect(logic.runInputs.value).toEqual({})
    expect(logic.runImages.value).toEqual({})
    expect(logic.baselineName.value).toBe('')
    workspaceId.value = 'original-id'
    resolveHead(item)
    resolveCopy({ sourceId: item.id, sourceName: item.name, suggestedName: 'old account copy' })
    await Promise.all([launch, copy])
    expect(logic.runTarget.value).toBeNull()
    expect(logic.copyModal.value).toBeNull()
    expect(mocks.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
