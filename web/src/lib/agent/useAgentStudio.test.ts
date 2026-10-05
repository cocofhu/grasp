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
  getAgentsOrg: vi.fn(),
  listProjects: vi.fn(),
  saveAgentsOrg: vi.fn(),
  patchAgentProject: vi.fn(),
  saveAgent: vi.fn(),
  getAgent: vi.fn(),
  exportAgent: vi.fn(),
  renameAgent: vi.fn(),
  deleteAgent: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgents: mocks.listAgents,
      getAgentsOrg: mocks.getAgentsOrg,
      listProjects: mocks.listProjects,
      saveAgentsOrg: mocks.saveAgentsOrg,
      patchAgentProject: mocks.patchAgentProject,
      saveAgent: mocks.saveAgent,
      getAgent: mocks.getAgent,
      exportAgent: mocks.exportAgent,
      renameAgent: mocks.renameAgent,
      deleteAgent: mocks.deleteAgent,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/agent/useAgentImport', () => ({
  useAgentImport: () => ({
    importFileInput: { value: null },
    showImportDiscardConfirm: { value: false },
    triggerImport: vi.fn(),
    onImportFile: vi.fn(),
    confirmImportDiscard: vi.fn(),
    cancelImportDiscard: vi.fn(),
  }),
}))

import { useAgentStudio } from './useAgentStudio'

class MockResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
  constructor(_cb: ResizeObserverCallback) {}
}

async function withAgentStudio(path = '/agents?agent=agent-a&tab=files') {
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
    mocks.listAgents.mockReset()
    mocks.getAgentsOrg.mockReset()
    mocks.listProjects.mockReset()

    mocks.listAgents.mockResolvedValue([
      { name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' },
      { name: 'agent-b', projectId: '', acpBackend: 'cursor' },
    ])
    mocks.getAgentsOrg.mockResolvedValue({
      revision: 1,
      groups: [{ id: 'g1', name: 'Default', parentId: '', agentNames: ['agent-a'] }],
      agents: { 'agent-a': { groupId: 'g1' } },
    })
    mocks.listProjects.mockResolvedValue([{ id: 'proj-1', name: 'Proj 1' }])
    mocks.saveAgentsOrg.mockResolvedValue({
      revision: 2,
      groups: [{ id: 'g1', name: 'Default', parentId: '', agentNames: ['agent-a'] }],
      agents: { 'agent-a': { groupId: 'g1' } },
    })
    mocks.patchAgentProject.mockResolvedValue({})
    mocks.saveAgent.mockResolvedValue({ name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.getAgent.mockResolvedValue({ name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.exportAgent.mockResolvedValue(new Blob(['x']))
    mocks.renameAgent.mockResolvedValue({ name: 'agent-z', projectId: 'proj-1', acpBackend: 'cursor' })
    mocks.deleteAgent.mockResolvedValue({ status: 'ok' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads agents/org on mount and selects query agent', async () => {
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
    studio.onSidebarRenameBlocked('agent-a')
    studio.gotoManageFromBlocked()
    studio.closeRenameBlocked()
    studio.openOrgSheet()
    studio.toggleOrgSheetNode('g1')
    studio.orgSheetPadStyle(2)
    studio.closeOrgSheet()
    studio.onDataSubTab('memory')
    studio.requestStudioTab('mcp')
    studio.leaveConfirmCancel()
    studio.openSettingsInFiles()
    studio.discardUnsavedChanges()
    studio.clearManageSearch()
    studio.closeAssignModals()
    studio.openCreateRootGroup()
    studio.openCreateChildGroup('g1')
    studio.openRenameGroup('g1')
    studio.confirmDeleteGroup('g1')
    await studio.reloadOrg()
    await studio.refreshAgentsList()
    studio.showToast('ok')
    studio.chooseAgent('agent-b')
    studio.chooseAgentFromSheet('agent-a')
    studio.resetOrgFromBaseline()
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

  it('switching to capabilities then back to files stays clean', async () => {
    const { studio, app } = await withAgentStudio()
    await flushPromises()
    expect(studio.agentDirty.value).toBe(false)
    studio.requestStudioTab('capabilities')
    await nextTick()
    expect(studio.tab.value).toBe('capabilities')
    expect(studio.agentDirty.value).toBe(false)
    studio.requestStudioTab('files')
    await nextTick()
    expect(studio.agentDirty.value).toBe(false)
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
    expect(studio.agentDirty.value).toBe(false)
    studio.requestStudioTab('capabilities')
    studio.draft.value!.capabilities!.review = true
    expect(studio.agentDirty.value).toBe(true)
    const saved = await studio.save()
    expect(saved).toBe(true)
    expect(mocks.saveAgent.mock.calls.at(-1)?.[0]).toMatchObject({
      capabilities: { interaction: 'auto', review: true, reads: ['*'] },
    })
    expect(studio.agentDirty.value).toBe(false)

    studio.draft.value!.capabilities!.review = false
    expect(studio.agentDirty.value).toBe(true)
    studio.discardUnsavedChanges()
    await nextTick()
    expect(studio.draft.value!.capabilities!.review).toBe(true)
    expect(studio.agentDirty.value).toBe(false)
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
