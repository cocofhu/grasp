// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  saveAgent: vi.fn(),
  getAgent: vi.fn(),
  exportAgent: vi.fn(),
  renameAgent: vi.fn(),
  deleteAgent: vi.fn(),
  exportWorkspaceAgents: vi.fn(),
  triggerImport: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgents: mocks.listAgents,
      saveAgent: mocks.saveAgent,
      getAgent: mocks.getAgent,
      exportAgent: mocks.exportAgent,
      renameAgent: mocks.renameAgent,
      deleteAgent: mocks.deleteAgent,
      exportWorkspaceAgents: mocks.exportWorkspaceAgents,
    },
  }
})

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  const workspace = ref({ name: 'Workspace', description: '', variables: [] })
  return { useWorkspaceContext: () => ({ workspace, ensureLoaded: async () => workspace.value, reload: async () => workspace.value }) }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/agent/useAgentImport', async () => {
  const { ref } = await import('vue')
  return {
    useAgentImport: () => ({
      fileInput: ref(null),
      showDiscardConfirm: ref(false),
      showConflict: ref(false),
      showImportError: ref(false),
      importError: ref(''),
      conflictName: ref(''),
      conflictAction: ref('rename'),
      renameValue: ref(''),
      renameError: ref(''),
      showBatchConflict: ref(false),
      batchConflictNames: ref([]),
      triggerImport: mocks.triggerImport,
      onDiscardCancel: vi.fn(),
      onDiscardConfirm: vi.fn(),
      handleFileChange: vi.fn(),
      selectConflict: vi.fn(),
      closeConflict: vi.fn(),
      confirmConflict: vi.fn(),
      closeBatchConflict: vi.fn(),
      confirmBatchRename: vi.fn(),
      confirmBatchOverwrite: vi.fn(),
    }),
  }
})

import { useAgentStudio } from './useAgentStudio'

class MockResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
  constructor(_cb: ResizeObserverCallback) {}
}

async function withAgentStudio(path = '/agents?agent=agent-a') {
  let studio!: ReturnType<typeof useAgentStudio>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/agents', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const Comp = defineComponent({
    setup() {
      studio = useAgentStudio()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  return { studio, app, router }
}

describe('useAgentStudio', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', MockResizeObserver)
    for (const fn of Object.values(mocks)) fn.mockReset()

    mocks.listAgents.mockResolvedValue([
      { name: 'agent-a', acpBackend: 'cursor' },
      { name: 'agent-b', acpBackend: 'cursor' },
    ])
    mocks.saveAgent.mockResolvedValue({ name: 'agent-a', acpBackend: 'cursor' })
    mocks.getAgent.mockResolvedValue({ name: 'agent-a', acpBackend: 'cursor' })
    mocks.exportAgent.mockResolvedValue(new Blob(['x']))
    mocks.renameAgent.mockResolvedValue({ name: 'agent-z', acpBackend: 'cursor' })
    mocks.deleteAgent.mockResolvedValue({ status: 'ok' })
    mocks.exportWorkspaceAgents.mockResolvedValue({ blob: new Blob(['zip']), filename: 'workspace-agents.zip' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads agents on mount and selects query agent', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    await nextTick()

    expect(studio.hasInitialLoaded.value).toBe(true)
    expect(studio.agents.value.length).toBe(2)
    expect(studio.activeName.value).toBe('agent-a')
    expect(studio.tab.value).toBe('files')

    studio.toggleAgentListCollapsed()
    studio.closeFullNameTip()
    studio.closeMobileChromeOverlays()

    window.dispatchEvent(new Event('resize'))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

    studio.openCreateAgent()
    studio.openCreateTeam()
    studio.openSidebarSheet()
    expect(studio.showSidebarSheet.value).toBe(true)
    studio.closeSidebarSheet()
    expect(studio.showSidebarSheet.value).toBe(false)
    studio.onDataSubTab('memory')
    studio.requestStudioTab('mcp')
    studio.leaveConfirmCancel()
    studio.openSettingsInFiles()
    studio.discardUnsavedChanges()
    await studio.refreshAgentsList()
    studio.showToast('ok')
    studio.chooseAgent('agent-b')
    studio.chooseAgentFromSheet('agent-a')
    studio.onWizardCreated({ name: 'agent-c', acpBackend: 'cursor' } as never)
    studio.onTeamBootstrapStarted({
      id: 'sess-1',
      status: 'running',
      events: [],
      resources: [],
      createdAt: '2026-01-01T00:00:00Z',
    })
    studio.onTeamBootstrapSelectPm('agent-a')
    studio.onTeamBootstrapOpenPm('agent-a')
    await studio.onTeamBootstrapRefresh()
    studio.onTeamBootstrapDone()
    await studio.load()
    await studio.save()
    await studio.select('agent-a')

    app.unmount()
  })

  it('locks canonical agents in the list and refuses rename or delete', async () => {
    mocks.listAgents.mockResolvedValue([
      { name: '需求澄清', acpBackend: 'cursor' },
      { name: '自建', acpBackend: 'cursor' },
    ])
    const { studio, app } = await withAgentStudio('/agents?agent=自建')
    await flushPromises()
    const locked = studio.treeNodes.value.find((row) => row.id === '需求澄清')
    const custom = studio.treeNodes.value.find((row) => row.id === '自建')
    expect(locked?.locked).toBe(true)
    expect(locked?.lockTitle).toContain('不能删除或重命名')
    expect(custom?.locked).toBeFalsy()
    studio.openRenameAgent('需求澄清')
    studio.confirmDeleteAgent('需求澄清')
    expect(studio.promptCfg.value).toBeNull()
    expect(studio.confirmCfg.value).toBeNull()
    expect(mocks.renameAgent).not.toHaveBeenCalled()
    expect(mocks.deleteAgent).not.toHaveBeenCalled()
    studio.openRenameAgent('自建')
    expect(studio.promptCfg.value?.title).toBeTruthy()
    app.unmount()
  })

  it('follows a rename only when the open agent is the one renamed', async () => {
    mocks.renameAgent.mockImplementation(async (_from: string, next: string) => ({ name: next, acpBackend: 'cursor', updatedWorkflowCount: 0 }))
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.draft.value!.files.push({ path: 'note.md', content: 'dirty' })
    expect(studio.dirty.value).toBe(true)

    studio.openRenameAgent('agent-b')
    expect(studio.promptCfg.value?.hint).toContain('工作流')
    expect(studio.promptCfg.value?.validate('')).toBeTruthy()
    expect(studio.promptCfg.value?.validate('bad name')).toBeTruthy()
    studio.promptValue.value = 'agent-b2'
    await studio.promptOk()
    await flushPromises()

    expect(mocks.renameAgent).toHaveBeenCalledWith('agent-b', 'agent-b2')
    expect(studio.activeName.value).toBe('agent-a')
    expect(studio.draft.value?.files.some((file) => file.path === 'note.md')).toBe(true)
    expect(studio.dirty.value).toBe(true)
    expect(studio.agents.value.map((agent) => agent.name)).toEqual(['agent-a', 'agent-b2'])
    expect(studio.confirmCfg.value).toBeNull()

    studio.openRenameAgent('agent-a')
    studio.promptValue.value = 'agent-z'
    await studio.promptOk()
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('agent-a', 'agent-z')
    expect(studio.activeName.value).toBe('agent-z')
    expect(studio.promptCfg.value).toBeNull()
    app.unmount()
  })

  it('keeps the rename dialog open and the list unchanged when rename fails', async () => {
    mocks.renameAgent.mockRejectedValueOnce(new Error('rename failed'))
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.openRenameAgent('agent-b')
    studio.promptValue.value = 'agent-b2'
    await studio.promptOk()
    await flushPromises()
    expect(studio.promptCfg.value).not.toBeNull()
    expect(studio.promptError.value).toContain('rename failed')
    expect(studio.activeName.value).toBe('agent-a')
    expect(studio.agents.value.map((agent) => agent.name)).toEqual(['agent-a', 'agent-b'])
    app.unmount()
  })

  it('clears selection after deleting the open agent and leaves another draft alone', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.draft.value!.files.push({ path: 'note.md', content: 'dirty' })
    studio.confirmDeleteAgent('agent-b')
    expect(studio.confirmCfg.value?.danger).toBe(true)
    expect(studio.confirmCfg.value?.message).toContain('agent-b')
    expect(studio.confirmCfg.value?.message).toContain('不可恢复')
    await studio.confirmOk()
    await flushPromises()
    expect(mocks.deleteAgent).toHaveBeenCalledWith('agent-b')
    expect(studio.activeName.value).toBe('agent-a')
    expect(studio.draft.value?.files.some((file) => file.path === 'note.md')).toBe(true)
    expect(studio.agents.value.map((agent) => agent.name)).toEqual(['agent-a'])

    studio.confirmDeleteAgent('agent-a')
    await studio.confirmOk()
    await flushPromises()
    expect(studio.activeName.value).toBe('')
    expect(studio.draft.value).toBeNull()
    expect(studio.agents.value).toEqual([])
    app.unmount()
  })

  it('lists agents directly and selects by their stable name', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    expect(studio.treeNodes.value.map((row) => row.id)).toEqual(['agent-a', 'agent-b'])
    expect(studio.activeTreeKey.value).toBe('agent-a')
    studio.onTreeSelect('agent-b')
    await flushPromises()
    expect(studio.activeName.value).toBe('agent-b')
    expect(studio.activeTreeKey.value).toBe('agent-b')
    app.unmount()
  })

  it('imports directly into the workspace', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.triggerImport()
    expect(mocks.triggerImport).toHaveBeenCalledTimes(1)
    expect(mocks.triggerImport).toHaveBeenLastCalledWith()
    studio.onImportWorkspace()
    expect(mocks.triggerImport).toHaveBeenCalledTimes(2)
    expect(mocks.triggerImport).toHaveBeenLastCalledWith()
    app.unmount()
  })

  it('creates in the workspace and keeps team creation available', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.openCreateAgent()
    expect(studio.showCreateWizard.value).toBe(true)
    studio.openCreateTeam()
    expect(studio.showTeamWizard.value).toBe(true)
    app.unmount()
  })

  it('exports a workspace bundle after the secrets confirmation', async () => {
    const createObjectURL = vi.fn(() => 'blob:x')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
    const { studio, app } = await withAgentStudio()
    await flushPromises()

    studio.onExportWorkspace()
    expect(studio.pendingBundleExport.value).toBe(true)
    expect(studio.showBundleSecrets.value).toBe(true)
    expect(mocks.exportWorkspaceAgents).not.toHaveBeenCalled()
    await studio.confirmBundleSecrets()
    expect(mocks.exportWorkspaceAgents).toHaveBeenCalledWith()
    expect(createObjectURL).toHaveBeenCalled()
    expect(studio.showBundleSecrets.value).toBe(false)
    expect(studio.pendingBundleExport.value).toBe(false)
    app.unmount()
  })

  it('switching to capabilities then back to files stays clean', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    expect(studio.dirty.value).toBe(false)
    studio.requestStudioTab('capabilities')
    await nextTick()
    expect(studio.tab.value).toBe('capabilities')
    expect(studio.dirty.value).toBe(false)
    studio.requestStudioTab('files')
    await nextTick()
    expect(studio.dirty.value).toBe(false)
    app.unmount()
  })

  it('editing capabilities marks dirty; save sends them and discard restores', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        name: 'agent-a',
        acpBackend: 'cursor',
        capabilities: { interaction: 'auto', reads: ['*'] },
      },
    ])
    mocks.saveAgent.mockImplementation(async (payload: { name: string }) => payload)
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    expect(studio.dirty.value).toBe(false)
    studio.requestStudioTab('capabilities')
    studio.draft.value!.capabilities!.review = true
    expect(studio.dirty.value).toBe(true)
    const saved = await studio.save()
    expect(saved).toBe(true)
    expect(mocks.saveAgent.mock.calls.at(-1)?.[0]).toMatchObject({
      capabilities: { interaction: 'auto', review: true, reads: ['*'] },
    })
    expect(studio.dirty.value).toBe(false)

    studio.draft.value!.capabilities!.review = false
    expect(studio.dirty.value).toBe(true)
    studio.discardUnsavedChanges()
    await nextTick()
    expect(studio.draft.value!.capabilities!.review).toBe(true)
    expect(studio.dirty.value).toBe(false)
    app.unmount()
  })

  it('save rejects invalid capabilities and opens the capabilities tab', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        name: 'agent-a',
        acpBackend: 'cursor',
        capabilities: { interaction: 'auto', reads: ['*'] },
      },
    ])
    mocks.saveAgent.mockReset()
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.requestStudioTab('files')
    studio.draft.value!.capabilities = { interaction: 'clarify', reads: ['*'] }
    expect(await studio.save()).toBe(false)
    expect(mocks.saveAgent).not.toHaveBeenCalled()
    expect(studio.tab.value).toBe('capabilities')
    app.unmount()
  })
})
