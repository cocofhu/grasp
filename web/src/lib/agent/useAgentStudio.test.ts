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
  listProjects: vi.fn(),
  saveAgent: vi.fn(),
  getAgent: vi.fn(),
  exportAgent: vi.fn(),
  renameAgent: vi.fn(),
  deleteAgent: vi.fn(),
  exportProjectAgents: vi.fn(),
  triggerProjectImport: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgents: mocks.listAgents,
      listProjects: mocks.listProjects,
      saveAgent: mocks.saveAgent,
      getAgent: mocks.getAgent,
      exportAgent: mocks.exportAgent,
      renameAgent: mocks.renameAgent,
      deleteAgent: mocks.deleteAgent,
      exportProjectAgents: mocks.exportProjectAgents,
    },
  }
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
      triggerImport: mocks.triggerProjectImport,
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
      { name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' },
      { name: 'agent-b', projectId: 'proj-2', acpBackend: 'cursor' },
    ])
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: 'Proj 1' },
      { id: 'proj-2', name: 'Proj 2' },
    ])
    mocks.saveAgent.mockResolvedValue({ name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.getAgent.mockResolvedValue({ name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.exportAgent.mockResolvedValue(new Blob(['x']))
    mocks.renameAgent.mockResolvedValue({ name: 'agent-z', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.deleteAgent.mockResolvedValue({ status: 'ok' })
    mocks.exportProjectAgents.mockResolvedValue({ blob: new Blob(['zip']), filename: 'Proj 1-agents.zip' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads agents/projects on mount and selects query agent', async () => {
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
    studio.openAgentManage('agent-a')
    studio.closeAgentManage()
    studio.openProjectSheet()
    studio.closeProjectSheet()
    studio.onDataSubTab('memory')
    studio.requestStudioTab('mcp')
    studio.leaveConfirmCancel()
    studio.openSettingsInFiles()
    studio.discardUnsavedChanges()
    studio.clearManageSearch()
    await studio.refreshAgentsList()
    studio.showToast('ok')
    studio.chooseAgent('agent-b')
    studio.chooseAgentFromSheet('agent-a')
    studio.onWizardCreated({ name: 'agent-c', projectId: 'proj-1', acpBackend: 'cursor' } as never)
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

  it('builds project tree nodes and selects agents only from child keys', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    expect(studio.treeNodes.value.map((n) => [n.id, n.children?.map((c) => c.id)])).toEqual([
      ['proj-1', ['agent-a']],
      ['proj-2', ['agent-b']],
    ])
    expect(studio.activeTreeKey.value).toBe('c:proj-1:agent-a')

    studio.onTreeSelect('p:proj-2')
    await flushPromises()
    expect(studio.activeName.value).toBe('agent-a')

    studio.onTreeSelect('c:proj-2:agent-b')
    await flushPromises()
    expect(studio.activeName.value).toBe('agent-b')
    expect(studio.activeTreeKey.value).toBe('c:proj-2:agent-b')
    app.unmount()
  })

  it('header import asks for a target project, project row import goes straight in', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()

    studio.triggerImport()
    expect(studio.showImportProjectPick.value).toBe(true)
    expect(studio.importProjectId.value).toBe('proj-1')
    studio.importProjectId.value = 'proj-2'
    studio.confirmImportProjectPick()
    expect(studio.showImportProjectPick.value).toBe(false)
    expect(mocks.triggerProjectImport).toHaveBeenLastCalledWith('proj-2')

    studio.triggerImport()
    studio.cancelImportProjectPick()
    expect(mocks.triggerProjectImport).toHaveBeenCalledTimes(1)

    studio.onImportProject('proj-1')
    expect(mocks.triggerProjectImport).toHaveBeenLastCalledWith('proj-1')
    app.unmount()
  })

  it('header import without projects only toasts', async () => {
    mocks.listProjects.mockResolvedValue([])
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.triggerImport()
    expect(studio.showImportProjectPick.value).toBe(false)
    expect(studio.toastMsg.value).toBe(pages.pages.agentStudio.project.noProjects)
    expect(mocks.triggerProjectImport).not.toHaveBeenCalled()
    app.unmount()
  })

  it('prefills the create wizard project from the row, else the current agent project', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.openCreateAgent('proj-2')
    expect(studio.showCreateWizard.value).toBe(true)
    expect(studio.createAgentProjectId.value).toBe('proj-2')
    studio.openCreateAgent()
    expect(studio.createAgentProjectId.value).toBe('proj-1')
    app.unmount()
  })

  it('exports a project bundle after the secrets confirmation', async () => {
    const createObjectURL = vi.fn(() => 'blob:x')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
    const { studio, app } = await withAgentStudio()
    await flushPromises()

    studio.onExportProject('proj-1')
    expect(studio.showBundleSecrets.value).toBe(true)
    expect(mocks.exportProjectAgents).not.toHaveBeenCalled()
    await studio.confirmBundleSecrets()
    expect(mocks.exportProjectAgents).toHaveBeenCalledWith('proj-1')
    expect(createObjectURL).toHaveBeenCalled()
    expect(studio.showBundleSecrets.value).toBe(false)
    app.unmount()
  })

  it('refuses to save an agent without a home project', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    studio.draft.value!.projectId = ''
    expect(await studio.save()).toBe(false)
    expect(mocks.saveAgent).not.toHaveBeenCalled()
    expect(studio.error.value).toBe(pages.pages.agentStudio.project.required)
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
        projectId: 'proj-1',
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
        projectId: 'proj-1',
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
