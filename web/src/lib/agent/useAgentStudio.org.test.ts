// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Agent, AgentOrg } from '@/lib/api/api'

const isMobile = ref(false)

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  getAgentsOrg: vi.fn(),
  listProjects: vi.fn(),
  saveAgentsOrg: vi.fn(),
  patchAgentProject: vi.fn(),
  saveAgent: vi.fn(),
  getAgent: vi.fn(),
  exportAgent: vi.fn(),
  exportOrgFolder: vi.fn(),
  renameAgent: vi.fn(),
  deleteAgent: vi.fn(),
  scanOrgSensitiveKeys: vi.fn(),
  stripOrgSensitiveKeys: vi.fn(),
  importAgent: vi.fn(),
  importOrgFolder: vi.fn(),
  toastSuccess: vi.fn(),
  toastWarn: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    warn: mocks.toastWarn,
    error: mocks.toastError,
    info: vi.fn(),
  }),
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
      exportOrgFolder: mocks.exportOrgFolder,
      renameAgent: mocks.renameAgent,
      deleteAgent: mocks.deleteAgent,
      scanOrgSensitiveKeys: mocks.scanOrgSensitiveKeys,
      stripOrgSensitiveKeys: mocks.stripOrgSensitiveKeys,
      importAgent: mocks.importAgent,
      importOrgFolder: mocks.importOrgFolder,
    },
  }
})

import { useAgentStudio } from './useAgentStudio'

class MockResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
  constructor(_cb: ResizeObserverCallback) {}
}

const agentA = (): Agent =>
  ({ name: 'agent-a', projectId: 'proj-1', acpBackend: 'cursor' }) as unknown as Agent
const agentB = (): Agent =>
  ({ name: 'agent-b', projectId: '', acpBackend: 'cursor' }) as unknown as Agent
const agentC = (): Agent =>
  ({ name: 'agent-c', projectId: 'proj-2', acpBackend: 'cursor' }) as unknown as Agent

const orgFixture = (): AgentOrg =>
  ({
    revision: 1,
    groups: [
      { id: 'g1', name: '交付组' },
      { id: 'g2', name: '子组', parentGroupId: 'g1' },
      { id: 'g3', name: '空组' },
    ],
    agents: { 'agent-a': { groupIds: ['g1'] }, 'agent-b': { groupIds: ['g2'] } },
  }) as unknown as AgentOrg

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
  await flushPromises()
  await nextTick()
  return { studio, app, router }
}

/** Files-panel stand-in: the studio calls into it from watchers, not just handlers. */
function panelStub(extra: Record<string, unknown> = {}) {
  return {
    closeExplorerMore: vi.fn(),
    resetForSelect: vi.fn(),
    openPathOrCreate: vi.fn(),
    snapshot: vi.fn(() => ({ path: '', openPaths: [] as string[] })),
    restoreAfterDiscard: vi.fn(),
    ...extra,
  } as never
}

/** Element with a fixed overflow geometry (happy-dom reports zeros). */
function overflowEl(scrollWidth: number, clientWidth: number, scrollLeft = 0): HTMLElement {
  const el = document.createElement('div')
  Object.defineProperty(el, 'scrollWidth', { value: scrollWidth, configurable: true })
  Object.defineProperty(el, 'clientWidth', { value: clientWidth, configurable: true })
  Object.defineProperty(el, 'scrollLeft', { value: scrollLeft, configurable: true, writable: true })
  el.getBoundingClientRect = () =>
    ({ x: 0, y: 0, top: 10, left: 0, right: 100, bottom: 30, width: 100, height: 20 }) as DOMRect
  return el
}

describe('useAgentStudio org and dialogs', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('ResizeObserver', MockResizeObserver)
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:agent')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    isMobile.value = false
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()

    mocks.listAgents.mockResolvedValue([agentA(), agentB()])
    mocks.getAgentsOrg.mockResolvedValue(orgFixture())
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: 'Proj 1' },
      { id: 'proj-2', name: 'Proj 2' },
    ])
    mocks.saveAgentsOrg.mockImplementation(async (payload: AgentOrg) => ({
      ...payload,
      revision: (payload.revision || 0) + 1,
    }))
    mocks.patchAgentProject.mockResolvedValue({})
    mocks.saveAgent.mockResolvedValue(agentA())
    mocks.getAgent.mockResolvedValue(agentA())
    mocks.exportAgent.mockResolvedValue(new Blob(['zip']))
    mocks.exportOrgFolder.mockResolvedValue({ blob: new Blob(['zip']), filename: '交付组.zip' })
    mocks.renameAgent.mockResolvedValue({ ...agentA(), name: 'agent-z', updatedWorkflowCount: 2 })
    mocks.deleteAgent.mockResolvedValue({ status: 'ok' })
    mocks.scanOrgSensitiveKeys.mockResolvedValue({
      keys: [
        { key: 'OPENAI_API_KEY', agentCount: 2 },
        { key: 'GITHUB_TOKEN', agentCount: 1 },
      ],
    })
    mocks.stripOrgSensitiveKeys.mockResolvedValue({
      cleared: 2,
      failed: [],
      strippedKeys: ['OPENAI_API_KEY'],
      agentNames: ['agent-a'],
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('classifies studio load failures as denied or retryable', async () => {
    mocks.listAgents.mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }))
    const denied = await withAgentStudio()
    expect(denied.studio.loadDenied.value).toBe(true)
    expect(denied.studio.loadFailed.value).toBe(false)
    expect(denied.studio.agents.value).toEqual([])
    denied.app.unmount()

    mocks.listAgents.mockRejectedValue(Object.assign(new Error('boom'), { status: 500 }))
    const failed = await withAgentStudio()
    expect(failed.studio.loadFailed.value).toBe(true)
    expect(failed.studio.loadDenied.value).toBe(false)

    // A stale-but-present list is kept instead of blanking the studio.
    failed.studio.agents.value = [agentA()]
    await failed.studio.load()
    expect(failed.studio.agents.value).toHaveLength(1)
    expect(failed.studio.showRefreshProgress.value).toBe(false)
    failed.app.unmount()
  })

  it('applies the deep-link data sub-tab and mirrors state back into the query', async () => {
    const { studio, app, router } = await withAgentStudio('/agents?agent=agent-b&tab=data&sub=jobs')
    expect(studio.activeName.value).toBe('agent-b')
    expect(studio.tab.value).toBe('data')
    expect(studio.dataSubTab.value).toBe('jobs')

    studio.onDataSubTab('context')
    await flushPromises()
    expect(router.currentRoute.value.query.sub).toBe('context')

    studio.requestStudioTab('mcp')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('mcp')
    // Re-requesting the current tab is a no-op.
    studio.requestStudioTab('mcp')
    expect(studio.tab.value).toBe('mcp')

    studio.requestStudioTab('files')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBeUndefined()
    // Nothing changed → no extra replace.
    studio.syncStudioQuery()

    expect(studio.studioTabs.value.map((t) => t.k)).toEqual(['files', 'mcp', 'env', 'capabilities', 'data', 'meta', 'test'])
    expect(studio.studioTabLabel.value).toBeTruthy()

    app.unmount()
  })

  it('runs the group project-assign flow end to end', async () => {
    const { studio, app } = await withAgentStudio()

    // Unknown group / empty group / no projects all bail out early.
    studio.onAssignProject('nope')
    expect(studio.showAssignPick.value).toBe(false)
    studio.onAssignProject('g3')
    expect(studio.showAssignPick.value).toBe(false)

    const savedProjects = studio.projects.value
    studio.projects.value = []
    studio.onAssignProject('g1')
    expect(studio.showAssignPick.value).toBe(false)
    studio.projects.value = savedProjects

    studio.onAssignProject('g1')
    expect(studio.showAssignPick.value).toBe(true)
    expect(studio.assignMembers.value).toContain('agent-a')
    expect(studio.assignMemberList.value).toContain('agent-a')
    expect(studio.assignTargetLabel.value).toBe('Proj 1')

    // No target selected → refuse to advance.
    studio.assignTargetId.value = '  '
    studio.onAssignPickNext()
    expect(studio.showAssignCover.value).toBe(false)

    // Everyone already on the target project → informative close.
    studio.assignTargetId.value = 'proj-1'
    studio.assignMembers.value = ['agent-a']
    studio.onAssignPickNext()
    expect(studio.showAssignPick.value).toBe(false)

    // Members bound elsewhere → cover confirmation, then cancel.
    studio.agents.value = [agentA(), agentB(), agentC()]
    studio.onAssignProject('g1')
    studio.assignMembers.value = ['agent-a', 'agent-c']
    studio.assignTargetId.value = 'proj-1'
    studio.onAssignPickNext()
    expect(studio.showAssignCover.value).toBe(true)
    expect(studio.assignAffectedList.value).toContain('agent-c')
    studio.cancelAssignCover()
    expect(studio.showAssignCover.value).toBe(false)

    // Dirty draft binding for the active agent → draft confirmation step.
    studio.onAssignProject('g1')
    studio.assignMembers.value = ['agent-a']
    studio.assignTargetId.value = 'proj-2'
    studio.draft.value!.projectId = 'proj-9'
    studio.maybeAssignDraftThenApply()
    expect(studio.showAssignDraft.value).toBe(true)
    studio.keepAssignDraft()
    expect(studio.showAssignDraft.value).toBe(false)

    app.unmount()
  })

  it('applies assignment results and syncs the active draft binding', async () => {
    const { studio, app } = await withAgentStudio()
    studio.assignTargetId.value = 'proj-2'
    studio.assignMembers.value = ['agent-a', 'agent-b']

    mocks.listAgents.mockResolvedValueOnce([{ ...agentA(), projectId: 'proj-2' }, agentB()])
    await studio.applyAssign(true)
    expect(mocks.patchAgentProject).toHaveBeenCalledTimes(2)
    expect(studio.assignOkCount.value).toBe(2)
    expect(studio.draft.value!.projectId).toBe('proj-2')
    expect(studio.agentDirty.value).toBe(false)

    // Partial failure reports both counts and skips the draft sync.
    mocks.patchAgentProject.mockRejectedValueOnce(new Error('bound elsewhere'))
    mocks.listAgents.mockRejectedValueOnce(new Error('offline'))
    studio.assignTargetId.value = 'proj-1'
    await studio.applyAssign(true)
    expect(studio.assignFail.value).toHaveLength(1)
    expect(studio.assignOkCount.value).toBe(1)
    expect(studio.toastMsg.value).toBeTruthy()

    // Re-entrancy guard and empty target.
    studio.assignApplying.value = true
    mocks.patchAgentProject.mockClear()
    await studio.applyAssign(false)
    expect(mocks.patchAgentProject).not.toHaveBeenCalled()
    studio.assignApplying.value = false
    studio.assignTargetId.value = ''
    await studio.applyAssign(false)
    expect(mocks.patchAgentProject).not.toHaveBeenCalled()

    // Malformed snapshot must not throw while syncing the draft binding.
    studio.originalJson.value = 'not-json'
    studio.syncDraftProjectId('proj-2')
    expect(studio.draft.value!.projectId).toBe('proj-2')
    studio.draft.value = null
    studio.syncDraftProjectId('proj-1')

    app.unmount()
  })

  it('persists org edits and reloads the authoritative org on conflict', async () => {
    const { studio, app } = await withAgentStudio()

    expect(await studio.persistOrg({ ...studio.org.value, groups: [{ id: 'g9', name: 'New' }] } as AgentOrg)).toBe(true)
    expect(studio.orgDirty.value).toBe(false)

    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('revision conflict'))
    mocks.getAgentsOrg.mockResolvedValueOnce(orgFixture())
    expect(await studio.persistOrg(studio.org.value)).toBe(false)
    expect(studio.error.value).toBe('revision conflict')

    // Reload of the authoritative org may also fail; the error stays visible.
    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('revision conflict'))
    mocks.getAgentsOrg.mockRejectedValueOnce(new Error('offline'))
    expect(await studio.persistOrg(studio.org.value)).toBe(false)

    // reloadOrg normalizes a bare payload and reports failures.
    mocks.getAgentsOrg.mockResolvedValueOnce({ revision: 3 } as AgentOrg)
    await studio.reloadOrg()
    expect(studio.org.value.groups).toEqual([])
    mocks.getAgentsOrg.mockResolvedValueOnce(null as unknown as AgentOrg)
    await studio.reloadOrg()
    mocks.getAgentsOrg.mockRejectedValueOnce(new Error('org down'))
    await studio.reloadOrg()
    expect(studio.error.value).toBe('org down')

    app.unmount()
  })

  it('creates, renames, moves and deletes org groups through the prompt dialogs', async () => {
    const { studio, app } = await withAgentStudio()

    studio.openCreateRootGroup()
    expect(studio.promptCanSubmit.value).toBe(false)
    studio.promptValue.value = '  '
    studio.refreshPromptFeedback()
    expect(studio.promptError.value).toBe('')
    studio.promptValue.value = '新组'
    studio.refreshPromptFeedback()
    expect(studio.promptCanSubmit.value).toBe(true)
    await studio.promptOk()
    expect(studio.promptCfg.value).toBeNull()
    expect(mocks.saveAgentsOrg).toHaveBeenCalled()

    studio.openCreateChildGroup('g1')
    studio.promptValue.value = '子子组'
    await studio.promptOk()
    expect(studio.promptCfg.value).toBeNull()

    studio.openRenameGroup('missing')
    expect(studio.promptCfg.value).toBeNull()
    studio.openRenameGroup('g1')
    expect(studio.promptValue.value).toBe('交付组')
    // Empty name fails validation and keeps the dialog open.
    studio.promptValue.value = ''
    await studio.promptOk()
    expect(studio.promptCfg.value).not.toBeNull()
    expect(studio.promptError.value).toBeTruthy()
    // Backend rejection surfaces inside the dialog.
    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('name taken'))
    studio.promptValue.value = '改名组'
    await studio.promptOk()
    expect(studio.promptCfg.value).not.toBeNull()
    expect(studio.promptError.value).toBeTruthy()

    studio.promptCfg.value = null
    await studio.promptOk()

    // Cycles are rejected before hitting the API (g2 still lives under g1).
    mocks.saveAgentsOrg.mockClear()
    await studio.onMoveGroup('g1', 'g2')
    expect(mocks.saveAgentsOrg).not.toHaveBeenCalled()
    expect(studio.error.value).toBeTruthy()

    await studio.onMoveGroup('g2', '')
    expect(mocks.saveAgentsOrg).toHaveBeenCalled()
    await studio.onMoveAgent('agent-a', 'g1', 'g2')
    await studio.onRemoveFromGroup('agent-a', 'g2')
    expect(studio.toastMsg.value).toBeTruthy()

    studio.confirmDeleteGroup('missing')
    expect(studio.confirmCfg.value).toBeNull()
    studio.confirmDeleteGroup('g2')
    expect(studio.confirmCfg.value?.danger).toBe(true)
    await studio.confirmOk()
    expect(studio.confirmCfg.value).toBeNull()
    expect((studio.org.value.groups || []).some((g) => g.id === 'g2')).toBe(false)

    studio.confirmCfg.value = { title: 't', message: 'm', ok: () => { throw new Error('nope') } }
    await studio.confirmOk()
    expect(studio.error.value).toBe('nope')
    await studio.confirmOk()

    app.unmount()
  })

  it('renames and deletes agents with cascade feedback', async () => {
    const { studio, app } = await withAgentStudio()

    studio.openRenameAgent('agent-a')
    expect(studio.promptValue.value).toBe('agent-a')
    studio.promptValue.value = ''
    studio.refreshPromptFeedback()
    expect(studio.promptCanSubmit.value).toBe(false)
    studio.promptValue.value = 'bad name'
    studio.refreshPromptFeedback()
    expect(studio.promptError.value).toBeTruthy()
    studio.promptValue.value = 'agent-b'
    studio.refreshPromptFeedback()
    expect(studio.promptError.value).toBeTruthy()
    studio.promptValue.value = 'agent-z'
    studio.refreshPromptFeedback()
    expect(studio.promptOkMsg.value).toBeTruthy()

    studio.manageFocusAgent.value = 'agent-a'
    mocks.listAgents.mockResolvedValue([{ ...agentA(), name: 'agent-z' }, agentB()])
    await studio.promptOk()
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('agent-a', 'agent-z')
    expect(studio.manageFocusAgent.value).toBe('agent-z')
    expect(studio.activeName.value).toBe('agent-z')
    expect(studio.toastMsg.value).toBeTruthy()

    // Renaming to the same name short-circuits.
    mocks.renameAgent.mockClear()
    studio.openRenameAgent('agent-z')
    studio.promptValue.value = 'agent-z'
    await studio.promptOk()
    expect(mocks.renameAgent).not.toHaveBeenCalled()

    // Rename without workflow references uses the plain success toast.
    mocks.renameAgent.mockResolvedValueOnce({ ...agentA(), name: 'agent-y' })
    studio.openRenameAgent('agent-z')
    studio.promptValue.value = 'agent-y'
    await studio.promptOk()
    await flushPromises()
    expect(studio.agents.value.some((a) => a.name === 'agent-y')).toBe(true)

    studio.confirmDeleteAgent('agent-y')
    studio.activeName.value = 'agent-y'
    await studio.confirmOk()
    expect(mocks.deleteAgent).toHaveBeenCalledWith('agent-y')
    expect(studio.activeName.value).toBe('')
    expect(studio.draft.value).toBeNull()

    app.unmount()
  })

  it('guards agent switches and tab leaves while the draft is dirty', async () => {
    const { studio, app } = await withAgentStudio()

    studio.chooseAgent('agent-a')
    expect(studio.confirmCfg.value).toBeNull()

    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    expect(studio.dirty.value).toBe(true)
    expect(studio.justSaved.value).toBe(false)

    studio.chooseAgent('agent-b')
    expect(studio.confirmCfg.value).not.toBeNull()
    await studio.confirmOk()
    expect(studio.activeName.value).toBe('agent-b')

    // Sheet switching: same agent just closes the sheet.
    studio.openOrgSheet()
    studio.chooseAgentFromSheet('agent-b')
    expect(studio.showOrgSheet.value).toBe(false)

    studio.openOrgSheet()
    studio.chooseAgentFromSheet('agent-a')
    expect(studio.activeName.value).toBe('agent-a')
    expect(studio.showOrgSheet.value).toBe(false)

    // Dirty sheet switch → save/discard confirmation.
    studio.draft.value!.env = [{ k: 'B', v: '2' }]
    await nextTick()
    studio.openOrgSheet()
    studio.chooseAgentFromSheet('agent-b')
    expect(studio.leaveConfirmCfg.value).not.toBeNull()
    await studio.leaveConfirmSave()
    expect(studio.leaveConfirmCfg.value).toBeNull()
    expect(studio.activeName.value).toBe('agent-b')

    studio.draft.value!.env = [{ k: 'C', v: '3' }]
    await nextTick()
    studio.openOrgSheet()
    studio.chooseAgentFromSheet('agent-a')
    studio.leaveConfirmDiscard()
    expect(studio.leaveConfirmCfg.value).toBeNull()
    expect(studio.activeName.value).toBe('agent-a')
    studio.leaveConfirmDiscard()
    await studio.leaveConfirmSave()

    studio.openManageFromSheet()
    expect(studio.showAgentManage.value).toBe(true)

    app.unmount()
  })

  it('confirms leaving a dirty mobile file editor before switching tabs', async () => {
    isMobile.value = true
    const { studio, app } = await withAgentStudio()
    studio.filesPanelRef.value = panelStub({ filesStep: ref('edit') })
    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()

    studio.requestStudioTab('mcp')
    expect(studio.leaveConfirmCfg.value).not.toBeNull()
    expect(studio.tab.value).toBe('files')
    await studio.leaveConfirmSave()
    expect(studio.tab.value).toBe('mcp')
    expect(studio.justSaved.value).toBe(true)

    // Save failure keeps the confirmation open.
    studio.requestStudioTab('files')
    studio.draft.value!.env = [{ k: 'B', v: '2' }]
    await nextTick()
    studio.requestStudioTab('env')
    mocks.saveAgent.mockRejectedValueOnce(new Error('save failed'))
    await studio.leaveConfirmSave()
    expect(studio.leaveConfirmCfg.value).not.toBeNull()
    expect(studio.tab.value).toBe('files')

    // onSave throwing is reported on the studio error banner.
    studio.leaveConfirmCfg.value = {
      title: 't',
      message: 'm',
      saveText: 's',
      discardText: 'd',
      onSave: () => { throw new Error('throwing save') },
      onDiscard: () => {},
    }
    await studio.leaveConfirmSave()
    expect(studio.error.value).toBe('throwing save')

    studio.requestStudioTab('files')
    studio.draft.value!.env = [{ k: 'C', v: '3' }]
    await nextTick()
    studio.requestStudioTab('meta')
    studio.leaveConfirmDiscard()
    expect(studio.tab.value).toBe('meta')

    // Leaving mobile closes the org sheet and re-measures the chrome.
    studio.openOrgSheet()
    isMobile.value = false
    await nextTick()
    await flushPromises()
    expect(studio.showOrgSheet.value).toBe(false)

    app.unmount()
  })

  it('saves the draft with and without a reason', async () => {
    const { studio, app } = await withAgentStudio()

    expect(await studio.save()).toBe(false)
    studio.promptSave()
    expect(studio.showSaveReasonModal.value).toBe(false)

    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    studio.promptSave()
    expect(studio.showSaveReasonModal.value).toBe(true)
    expect(studio.saveReason.value).toBeTruthy()

    expect(await studio.confirmSaveWithReason()).toBe(true)
    expect(mocks.saveAgent).toHaveBeenCalledWith(expect.objectContaining({ name: 'agent-a' }), {
      reason: studio.saveReason.value.trim(),
    })
    expect(studio.showSaveReasonModal.value).toBe(false)
    expect(studio.historyRefreshKey.value).toBe(1)

    // Org-only dirt saves through persistOrg; its failure fails the save.
    studio.org.value = { ...studio.org.value, groups: [...(studio.org.value.groups || []), { id: 'g8', name: 'X' }] }
    await nextTick()
    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('org conflict'))
    expect(await studio.save()).toBe(false)

    studio.org.value = { ...studio.org.value, groups: [...(studio.org.value.groups || []), { id: 'g7', name: 'Y' }] }
    await nextTick()
    expect(await studio.save()).toBe(true)

    studio.draft.value!.env = [{ k: 'B', v: '2' }]
    await nextTick()
    mocks.saveAgent.mockRejectedValueOnce(new Error('write denied'))
    expect(await studio.save()).toBe(false)
    expect(studio.error.value).toBe('write denied')

    // Reloading from the server clears the dirty draft.
    mocks.getAgent.mockResolvedValueOnce({ ...agentA(), acpBackend: 'cursor' })
    await studio.reloadAgentFromServer('agent-a')
    expect(studio.agentDirty.value).toBe(false)
    mocks.getAgent.mockRejectedValueOnce(new Error('agent gone'))
    await studio.reloadAgentFromServer('agent-a')
    expect(studio.error.value).toBe('agent gone')
    await studio.reloadAgentFromServer('missing-agent')

    app.unmount()
  })

  it('discards unsaved changes back to the last snapshot', async () => {
    const { studio, app } = await withAgentStudio()
    const snapshot = vi.fn(() => ({ path: 'AGENTS.md', openPaths: ['AGENTS.md'] }))
    const restoreAfterDiscard = vi.fn()
    studio.filesPanelRef.value = panelStub({ snapshot, restoreAfterDiscard })

    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    studio.discardUnsavedChanges()
    await nextTick()
    expect(studio.agentDirty.value).toBe(false)
    expect(restoreAfterDiscard).toHaveBeenCalledWith({ path: 'AGENTS.md', openPaths: ['AGENTS.md'] })

    // No panel exposed and an unknown active agent are both tolerated.
    studio.filesPanelRef.value = null
    studio.activeName.value = 'ghost'
    studio.discardUnsavedChanges()

    app.unmount()
  })

  it('exports a single agent and an org folder', async () => {
    const { studio, app } = await withAgentStudio()

    await studio.doExport('agent-a')
    expect(mocks.exportAgent).toHaveBeenCalledWith('agent-a')
    expect(studio.exporting.value).toBe(false)
    mocks.exportAgent.mockRejectedValueOnce(new Error('export failed'))
    await studio.doExport('agent-a')
    expect(studio.error.value).toBe('export failed')

    mocks.exportAgent.mockClear()
    studio.triggerExport()
    await flushPromises()
    expect(mocks.exportAgent).toHaveBeenCalled()

    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    studio.triggerExport()
    expect(studio.showUnsavedExport.value).toBe(true)
    studio.cancelUnsavedExport()
    expect(studio.showUnsavedExport.value).toBe(false)

    studio.triggerExport()
    await studio.discardAndExport()
    expect(studio.showUnsavedExport.value).toBe(false)

    studio.draft.value!.env = [{ k: 'B', v: '2' }]
    await nextTick()
    studio.triggerExport()
    await studio.saveThenExport()
    expect(studio.showUnsavedExport.value).toBe(false)

    // Save failure aborts the export.
    studio.draft.value!.env = [{ k: 'C', v: '3' }]
    await nextTick()
    mocks.saveAgent.mockRejectedValueOnce(new Error('nope'))
    await studio.saveThenExport()
    expect(studio.error.value).toBe('nope')

    studio.exporting.value = true
    mocks.exportAgent.mockClear()
    studio.triggerExport()
    expect(mocks.exportAgent).not.toHaveBeenCalled()
    studio.exporting.value = false
    studio.activeName.value = ''
    studio.triggerExport()
    expect(mocks.exportAgent).not.toHaveBeenCalled()
    await studio.discardAndExport()

    app.unmount()
  })

  it('exports a group folder after resolving unsaved work', async () => {
    const { studio, app } = await withAgentStudio()

    await studio.onExportGroup('g1')
    expect(studio.showFolderSecrets.value).toBe(true)
    expect(studio.pendingFolderExportGroupId.value).toBe('g1')
    await studio.confirmFolderSecrets()
    expect(mocks.exportOrgFolder).toHaveBeenCalledWith('g1')
    expect(studio.toastMsg.value).toBeTruthy()

    studio.cancelFolderSecrets()
    expect(studio.pendingFolderExportGroupId.value).toBe('')
    await studio.confirmFolderSecrets()

    mocks.exportOrgFolder.mockRejectedValueOnce(new Error('folder export failed'))
    await studio.onExportGroup('g1')
    await studio.confirmFolderSecrets()
    expect(studio.error.value).toBe('folder export failed')

    // Dirty agent inside the exported subtree asks first.
    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    await studio.onExportGroup('g1')
    expect(studio.showUnsavedExport.value).toBe(true)
    await studio.discardAndExport()
    expect(studio.showFolderSecrets.value).toBe(true)
    studio.cancelFolderSecrets()

    studio.draft.value!.env = [{ k: 'B', v: '2' }]
    await nextTick()
    await studio.onExportGroup('g1')
    await studio.saveThenExport()
    expect(studio.showFolderSecrets.value).toBe(true)
    studio.cancelFolderSecrets()

    // Org dirt is flushed first; a failed flush aborts the export.
    studio.org.value = { ...studio.org.value, groups: [...(studio.org.value.groups || []), { id: 'g6', name: 'Z' }] }
    await nextTick()
    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('org conflict'))
    mocks.exportOrgFolder.mockClear()
    await studio.onExportGroup('g1')
    expect(mocks.exportOrgFolder).not.toHaveBeenCalled()

    studio.exporting.value = true
    await studio.onExportGroup('g1')
    studio.exporting.value = false

    studio.onImportGroup('g1')
    await flushPromises()

    app.unmount()
  })

  it('scans and strips sensitive keys for a group', async () => {
    const { studio, app } = await withAgentStudio()

    await studio.onClearSensitiveConfig('g1')
    expect(studio.showClearSensitive.value).toBe(true)
    expect(studio.clearSensitiveHits.value).toHaveLength(2)
    expect(studio.clearSensitiveSelectedCount.value).toBe(2)
    expect(studio.clearSensitiveGroupName.value).toBe('交付组')
    expect(studio.clearSensitiveAgentCount.value).toBeGreaterThan(0)

    expect(studio.isClearSensitiveKeySelected('OPENAI_API_KEY')).toBe(true)
    studio.toggleClearSensitiveKey('OPENAI_API_KEY', false)
    expect(studio.isClearSensitiveKeySelected('OPENAI_API_KEY')).toBe(false)
    studio.toggleClearSensitiveKey('OPENAI_API_KEY', true)
    studio.clearAllClearSensitiveKeys()
    expect(studio.clearSensitiveSelectedCount.value).toBe(0)
    // Nothing selected → no request.
    await studio.confirmClearSensitive()
    expect(mocks.stripOrgSensitiveKeys).not.toHaveBeenCalled()
    studio.selectAllClearSensitiveKeys()
    expect(studio.clearSensitiveSelectedCount.value).toBe(2)

    await studio.confirmClearSensitive()
    expect(mocks.stripOrgSensitiveKeys).toHaveBeenCalledWith('g1', ['OPENAI_API_KEY', 'GITHUB_TOKEN'])
    expect(studio.showClearSensitive.value).toBe(false)

    // Partial failure keeps the numbers in the toast.
    mocks.stripOrgSensitiveKeys.mockResolvedValueOnce({
      cleared: 1,
      failed: ['agent-b'],
      strippedKeys: [],
      agentNames: [],
    })
    await studio.onClearSensitiveConfig('g1')
    await studio.confirmClearSensitive()
    expect(studio.toastMsg.value).toContain('agent-b')

    mocks.stripOrgSensitiveKeys.mockRejectedValueOnce(new Error('strip failed'))
    await studio.onClearSensitiveConfig('g1')
    await studio.confirmClearSensitive()
    expect(studio.error.value).toBe('strip failed')
    studio.cancelClearSensitive()
    expect(studio.clearSensitiveHits.value).toEqual([])

    // Empty scan informs instead of opening the modal.
    mocks.scanOrgSensitiveKeys.mockResolvedValueOnce({ keys: [] })
    await studio.onClearSensitiveConfig('g1')
    expect(studio.showClearSensitive.value).toBe(false)

    mocks.scanOrgSensitiveKeys.mockRejectedValueOnce(new Error('scan failed'))
    await studio.onClearSensitiveConfig('g1')
    expect(studio.error.value).toBe('scan failed')

    studio.clearSensitiveBusy.value = true
    mocks.scanOrgSensitiveKeys.mockClear()
    await studio.onClearSensitiveConfig('g1')
    expect(mocks.scanOrgSensitiveKeys).not.toHaveBeenCalled()
    await studio.confirmClearSensitive()
    studio.clearSensitiveBusy.value = false

    // Org dirt is flushed first; a failed flush aborts the scan.
    studio.org.value = { ...studio.org.value, groups: [...(studio.org.value.groups || []), { id: 'g5', name: 'W' }] }
    await nextTick()
    mocks.saveAgentsOrg.mockRejectedValueOnce(new Error('org conflict'))
    await studio.onClearSensitiveConfig('g1')
    expect(mocks.scanOrgSensitiveKeys).not.toHaveBeenCalled()

    app.unmount()
  })

  it('bootstraps a team and refreshes the roster', async () => {
    const { studio, app } = await withAgentStudio()

    studio.onTeamBootstrapStarted({ id: 'sess-1' } as never)
    await flushPromises()
    expect(studio.teamBootstrapSessionId.value).toBe('sess-1')

    mocks.listAgents.mockResolvedValue([agentA(), agentB(), { ...agentC(), name: '项目经理' }])
    await studio.onTeamBootstrapRefresh()
    expect(studio.agents.value.some((a) => a.name === '项目经理')).toBe(true)

    studio.onTeamBootstrapSelectPm('agent-b')
    expect(studio.activeName.value).toBe('agent-b')
    studio.onTeamBootstrapOpenPm('agent-a')
    expect(studio.teamBootstrapSessionId.value).toBe('')

    studio.onTeamBootstrapDone()
    await flushPromises()
    expect(studio.activeName.value).toBe('项目经理')

    mocks.listAgents.mockRejectedValueOnce(new Error('offline'))
    await studio.refreshAgentsList()

    studio.onWizardCreated({ ...agentC(), name: 'agent-new' } as never)
    expect(studio.activeName.value).toBe('agent-new')
    expect(studio.toastMsg.value).toBeTruthy()

    app.unmount()
  })

  it('filters the agent management list and highlights matches', async () => {
    const { studio, app } = await withAgentStudio()

    expect(studio.filteredManageNames.value).toEqual(['agent-a', 'agent-b'])
    expect(studio.manageSearchActive.value).toBe(false)
    studio.manageSearch.value = ' AGENT-B '
    expect(studio.manageSearchActive.value).toBe(true)
    expect(studio.filteredManageNames.value).toEqual(['agent-b'])
    expect(studio.manageNameHighlight('agent-b')).toEqual({ before: '', hit: 'agent-b', after: '' })
    expect(studio.manageNameHighlight('agent-a')).toEqual({ before: 'agent-a', hit: '', after: '' })
    studio.manageSearch.value = 'gent'
    expect(studio.manageNameHighlight('agent-a')).toEqual({ before: 'a', hit: 'gent', after: '-a' })
    studio.clearManageSearch()
    expect(studio.manageSearch.value).toBe('')
    expect(studio.manageNameHighlight('agent-a').hit).toBe('')

    app.unmount()
  })

  it('drives the org sheet tree and collapse memory', async () => {
    const { studio, app } = await withAgentStudio()

    expect(studio.orgSheetRows.value.length).toBeGreaterThan(0)
    const before = new Set(studio.orgSheetCollapsed.value)
    studio.toggleOrgSheetNode('g1')
    expect(studio.orgSheetCollapsed.value.has('g1')).toBe(!before.has('g1'))
    studio.toggleOrgSheetNode('g1')
    expect(studio.orgSheetCollapsed.value.has('g1')).toBe(before.has('g1'))
    expect(studio.orgSheetPadStyle(3)).toEqual({ paddingLeft: '48px' })

    // Group-list changes re-run the collapse merge.
    studio.org.value = {
      ...studio.org.value,
      groups: [...(studio.org.value.groups || []), { id: 'g4', name: '新组' }],
    }
    await nextTick()
    expect(studio.orgSheetCollapsed.value).toBeInstanceOf(Set)

    // Focusing an agent expands its ancestors.
    studio.manageFocusAgent.value = 'agent-b'
    await nextTick()
    expect(studio.orgSheetCollapsed.value.has('g2')).toBe(false)

    app.unmount()
  })

  it('measures the agent name tooltip and tab-strip fades', async () => {
    isMobile.value = true
    const { studio, app } = await withAgentStudio()
    const closeExplorerMore = vi.fn()
    studio.filesPanelRef.value = panelStub({ closeExplorerMore })

    studio.measureAgentNameTruncation()
    expect(studio.agentNameTruncated.value).toBe(false)

    studio.agentNameEl.value = overflowEl(400, 200)
    studio.measureAgentNameTruncation()
    expect(studio.agentNameTruncated.value).toBe(true)

    // Tip is only placed while open.
    studio.placeFullNameTip()
    expect(studio.fullNameTipStyle.value).toEqual({})
    studio.onAgentNameClick()
    expect(studio.showFullNameTip.value).toBe(true)
    await nextTick()
    expect(studio.fullNameTipStyle.value.top).toBe('38px')
    expect(closeExplorerMore).toHaveBeenCalled()

    studio.onChromeReposition()
    studio.onChromeKeydown(new KeyboardEvent('keydown', { key: 'a' }))
    expect(studio.showFullNameTip.value).toBe(true)
    studio.onChromeKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(studio.showFullNameTip.value).toBe(false)
    studio.onChromeKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))

    studio.onAgentNameClick()
    expect(studio.showFullNameTip.value).toBe(true)
    // A name that now fits is not a tooltip trigger, so the click is ignored.
    studio.agentNameEl.value = overflowEl(100, 400)
    studio.onAgentNameClick()
    expect(studio.agentNameTruncated.value).toBe(false)
    expect(studio.showFullNameTip.value).toBe(true)
    studio.closeFullNameTip()
    studio.agentNameEl.value = null
    studio.placeFullNameTip()

    studio.tabStripEl.value = overflowEl(600, 300, 10)
    await nextTick()
    studio.syncTabFade()
    expect(studio.tabFadeLeft.value).toBe(true)
    expect(studio.tabFadeRight.value).toBe(true)

    isMobile.value = false
    studio.syncTabFade()
    expect(studio.tabFadeLeft.value).toBe(false)
    expect(studio.tabFadeRight.value).toBe(false)

    studio.tabStripEl.value = null
    studio.bindTabStripObserver()
    studio.syncTabFade()

    app.unmount()
  })

  it('skips the tab-strip observer when ResizeObserver is unavailable', async () => {
    const { studio, app } = await withAgentStudio()
    vi.stubGlobal('ResizeObserver', undefined)
    studio.tabStripEl.value = overflowEl(600, 300)
    studio.bindTabStripObserver()
    app.unmount()
  })

  it('remembers the collapsed sidebar across mounts', async () => {
    const { studio, app } = await withAgentStudio()
    expect(studio.agentListCollapsed.value).toBe(false)
    studio.toggleAgentListCollapsed()
    expect(studio.agentListCollapsed.value).toBe(true)
    expect(studio.readCollapsedState(studio.AGENT_LIST_COLLAPSED_KEY)).toBe(true)
    expect(studio.cardGridStyle.value.gridTemplateColumns).toContain('28px')
    studio.toggleAgentListCollapsed()
    expect(studio.cardGridStyle.value.gridTemplateColumns).toContain('280px')
    expect(studio.readCollapsedState('never-written')).toBe(false)
    app.unmount()

    isMobile.value = true
    const remount = await withAgentStudio()
    expect(remount.studio.cardGridStyle.value).toEqual({ gridTemplateColumns: '1fr' })
    remount.app.unmount()
  })

  it('survives a storage backend that throws', async () => {
    const getItem = vi.spyOn(localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const setItem = vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const { studio, app } = await withAgentStudio()
    expect(studio.agentListCollapsed.value).toBe(false)
    studio.toggleAgentListCollapsed()
    expect(studio.agentListCollapsed.value).toBe(true)
    getItem.mockRestore()
    setItem.mockRestore()
    app.unmount()
  })

  it('opens the settings file and the agent management focus row', async () => {
    const { studio, app } = await withAgentStudio()
    const openPathOrCreate = vi.fn()
    studio.filesPanelRef.value = panelStub({ openPathOrCreate })

    studio.tab.value = 'meta'
    studio.openSettingsInFiles()
    await nextTick()
    expect(studio.tab.value).toBe('files')
    expect(openPathOrCreate).toHaveBeenCalled()

    studio.draft.value = null
    studio.tab.value = 'meta'
    studio.openSettingsInFiles()
    expect(studio.tab.value).toBe('meta')

    const row = document.createElement('div')
    row.setAttribute('data-manage-agent', 'agent-b')
    row.scrollIntoView = vi.fn()
    document.body.appendChild(row)
    studio.openAgentManage('agent-b')
    await nextTick()
    expect(row.scrollIntoView).toHaveBeenCalled()
    studio.closeAgentManage()
    expect(studio.manageFocusAgent.value).toBe('')
    row.remove()

    studio.onSidebarRenameBlocked('agent-a')
    expect(studio.renameBlockedTarget.value).toBe('agent-a')
    studio.gotoManageFromBlocked()
    expect(studio.showRenameBlocked.value).toBe(false)
    expect(studio.manageFocusAgent.value).toBe('agent-a')
    studio.closeRenameBlocked()

    app.unmount()
  })

  it('reports the saved project binding independent of the draft', async () => {
    const { studio, app } = await withAgentStudio()
    expect(studio.savedProjectId.value).toBe('proj-1')
    expect(studio.isProjectBound.value).toBe(true)
    expect(studio.draftBindingDirty.value).toBe(false)
    studio.draft.value!.projectId = 'proj-2'
    await nextTick()
    expect(studio.draftBindingDirty.value).toBe(true)
    expect(studio.projectNameById('proj-2')).toBe('Proj 2')
    expect(studio.projectNameById('ghost')).toBe('ghost')

    studio.select('agent-b')
    expect(studio.savedProjectId.value).toBe('')
    expect(studio.isProjectBound.value).toBe(false)
    studio.select('missing')
    expect(studio.activeName.value).toBe('agent-b')

    // A corrupted baseline leaves the current org untouched.
    studio.orgBaseline.value = 'not-json'
    const groups = studio.org.value.groups
    studio.resetOrgFromBaseline()
    expect(studio.org.value.groups).toBe(groups)

    app.unmount()
  })

  it('routes agent zip import through the shared import composable', async () => {
    const { studio, app } = await withAgentStudio()
    const click = vi.fn()
    studio.importFileInput.value = { click, value: '', files: null } as never

    studio.triggerImport()
    await flushPromises()
    expect(click).toHaveBeenCalled()

    studio.draft.value!.env = [{ k: 'A', v: '1' }]
    await nextTick()
    studio.triggerImport()
    await flushPromises()
    expect(studio.showImportDiscardConfirm.value).toBe(true)
    studio.onImportDiscardCancel()
    expect(studio.showImportDiscardConfirm.value).toBe(false)
    studio.triggerImport()
    await flushPromises()
    studio.onImportDiscardConfirm()
    expect(studio.showImportDiscardConfirm.value).toBe(false)

    // Non-zip uploads are rejected before any parsing.
    const input = document.createElement('input')
    input.type = 'file'
    Object.defineProperty(input, 'files', {
      value: [new File(['x'], 'notes.txt', { type: 'text/plain' })],
      configurable: true,
    })
    await studio.onImportFileChange({ target: input } as unknown as Event)
    expect(studio.showImportErrorModal.value).toBe(true)
    expect(studio.importErrorMessage.value).toBeTruthy()

    studio.selectImportConflict('overwrite')
    expect(studio.importConflictAction.value).toBe('overwrite')
    studio.closeImportConflict()
    await studio.confirmImportConflict()
    studio.selectImportConflict('cancel')
    await studio.confirmImportConflict()
    studio.closeBatchConflict()
    expect(studio.batchConflictNames.value).toEqual([])

    app.unmount()
  })
})
