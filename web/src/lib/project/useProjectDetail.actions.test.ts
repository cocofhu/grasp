// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Project, ProjectVariable, Workflow } from '@/lib/shared/types'

const shared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref: hoistedRef } = require('vue') as typeof import('vue')
  return { isMobile: hoistedRef(false), firstInstallCompletedAt: hoistedRef('') }
})
const isMobile = shared.isMobile
const firstInstallCompletedAt = shared.firstInstallCompletedAt

const mocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  listWorkflows: vi.fn(),
  listAgents: vi.fn(),
  getPmLeader: vi.fn(),
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  deleteWorkflow: vi.fn(),
  copyPreviewWorkflow: vi.fn(),
  patchWorkflowNotifyPolicy: vi.fn(),
  patchWorkflowHomeVisibility: vi.fn(),
  writeStoredProjectId: vi.fn(),
  mergeRunDraft: vi.fn(),
  saveRunDraft: vi.fn(),
  clearRunDraft: vi.fn(),
  openRetryOnboarding: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarn: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProject: mocks.getProject,
      listWorkflows: mocks.listWorkflows,
      listAgents: mocks.listAgents,
      getPmLeader: mocks.getPmLeader,
      updateProject: mocks.updateProject,
      deleteProject: mocks.deleteProject,
      deleteWorkflow: mocks.deleteWorkflow,
      copyPreviewWorkflow: mocks.copyPreviewWorkflow,
      patchWorkflowNotifyPolicy: mocks.patchWorkflowNotifyPolicy,
      patchWorkflowHomeVisibility: mocks.patchWorkflowHomeVisibility,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: shared.isMobile }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warn: mocks.toastWarn,
    info: vi.fn(),
  }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  writeStoredProjectId: (...args: unknown[]) => mocks.writeStoredProjectId(...args),
}))

vi.mock('@/lib/run/runDraft', () => ({
  mergeRunDraft: (...args: unknown[]) => mocks.mergeRunDraft(...args),
  saveRunDraft: (...args: unknown[]) => mocks.saveRunDraft(...args),
  clearRunDraft: (...args: unknown[]) => mocks.clearRunDraft(...args),
}))

vi.mock('@/lib/pm/firstInstall', () => ({
  firstInstallCompletedAt: shared.firstInstallCompletedAt,
  openRetryOnboarding: (id: string) => mocks.openRetryOnboarding(id),
}))

vi.mock('@/lib/run/useWorkflowImport', () => ({
  useWorkflowImport: () => ({
    fileInput: { value: null },
    triggerImport: vi.fn(),
    handleFileChange: vi.fn(),
  }),
}))

import { useProjectDetail } from './useProjectDetail'

const sampleProject = (over: Partial<Project> = {}): Project =>
  ({
    id: 'proj-a',
    name: 'Project A',
    description: 'desc',
    variables: [{ name: 'TOKEN', type: 'string', value: '****', secret: true }],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...over,
  }) as unknown as Project

const askWorkflow = (): Workflow =>
  ({
    id: 'wf-1',
    projectId: 'proj-a',
    name: 'WF',
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [
      {
        id: 'in',
        type: 'input',
        label: 'in',
        position: { x: 0, y: 0 },
        config: {
          variables: [
            { name: 'branch', ask: true, type: 'string', value: 'main', desc: 'branch' },
            { name: 'env', ask: true, type: 'select', options: 'dev,prod', value: null },
            { name: 'repos', ask: true, type: 'repos', value: [{ name: 'a' }] },
            { name: 'skipped', ask: false, type: 'string', value: 'x' },
            null,
          ],
        },
      },
    ],
    edges: [],
  }) as unknown as Workflow

async function withProjectDetail(path = '/projects/proj-a') {
  let detail!: ReturnType<typeof useProjectDetail>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages }, en: { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/agents', component: { template: '<div />' } },
      { path: '/projects', component: { template: '<div />' } },
      { path: '/projects/:id', component: { template: '<div />' } },
      { path: '/runs/:id', component: { template: '<div />' } },
      { path: '/workflows/new/edit', component: { template: '<div />' } },
      { path: '/workflows/:id/edit', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const Comp = defineComponent({
    setup() {
      detail = useProjectDetail()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  await flushPromises()
  await nextTick()
  return { detail, app, router, i18n }
}

describe('useProjectDetail actions', () => {
  beforeEach(() => {
    isMobile.value = false
    firstInstallCompletedAt.value = ''
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()

    mocks.getProject.mockResolvedValue(sampleProject())
    mocks.listWorkflows.mockResolvedValue([askWorkflow()])
    mocks.listAgents.mockResolvedValue([{ name: 'agent-a', projectId: 'proj-a' }])
    mocks.getPmLeader.mockResolvedValue({ agentConfigRef: 'pm-agent', channelId: 'c1' })
    mocks.updateProject.mockImplementation(async (_id: string, patch: Partial<Project>) => ({
      ...sampleProject(),
      ...patch,
    }))
    mocks.deleteProject.mockResolvedValue({ status: 'ok' })
    mocks.deleteWorkflow.mockResolvedValue({ status: 'ok' })
    mocks.copyPreviewWorkflow.mockResolvedValue({
      sourceId: 'wf-1',
      sourceName: 'WF',
      suggestedName: 'WF copy',
    })
    mocks.patchWorkflowNotifyPolicy.mockImplementation(async (_id: string, policy: unknown) => ({
      notifyPolicy: policy,
    }))
    mocks.patchWorkflowHomeVisibility.mockResolvedValue({ showOnHome: true })
    mocks.mergeRunDraft.mockImplementation(
      async (
        _id: string,
        seed: Record<string, string>,
        images: Record<string, unknown>,
      ) => ({ inputs: seed, images, restored: true }),
    )
    mocks.saveRunDraft.mockResolvedValue('ok')
    mocks.clearRunDraft.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('classifies project load failures by status', async () => {
    mocks.getProject.mockRejectedValue(Object.assign(new Error('denied'), { status: 403 }))
    const denied = await withProjectDetail()
    expect(denied.detail.loadDenied.value).toBe(true)
    expect(denied.detail.initialLoading.value).toBe(false)
    denied.app.unmount()

    mocks.getProject.mockRejectedValue(Object.assign(new Error('missing'), { status: 404 }))
    const missing = await withProjectDetail()
    expect(missing.detail.notFound.value).toBe(true)
    missing.app.unmount()

    mocks.getProject.mockRejectedValue(Object.assign(new Error('boom'), { status: 500 }))
    const failed = await withProjectDetail()
    expect(failed.detail.loadFailed.value).toBe(true)

    // A loaded project survives a later failing refresh.
    mocks.getProject.mockResolvedValue(sampleProject())
    await failed.detail.load()
    expect(failed.detail.project.value?.id).toBe('proj-a')
    mocks.getProject.mockRejectedValue(new Error('refresh down'))
    await failed.detail.load()
    expect(failed.detail.project.value?.id).toBe('proj-a')
    failed.app.unmount()
  })

  it('keeps the PM binding optional', async () => {
    mocks.getPmLeader.mockRejectedValue(new Error('no binding'))
    const { detail, app } = await withProjectDetail()
    await flushPromises()
    expect(detail.pmBinding.value).toBeNull()

    mocks.getPmLeader.mockResolvedValueOnce({ agentConfigRef: 'pm-agent' })
    await detail.loadPmBinding()
    expect(detail.pmBinding.value?.agentConfigRef).toBe('pm-agent')

    detail.onPmBindingChanged({ agentConfigRef: 'pm-2' } as never)
    expect(detail.pmView.value).toBe('chat')

    app.unmount()
  })

  it('guards leaving the requirement-drafts tab', async () => {
    const { detail, app, router } = await withProjectDetail('/projects/proj-a?tab=requirementDrafts')
    expect(detail.tab.value).toBe('requirementDrafts')

    // No panel / clean panel → leave freely.
    expect(await detail.confirmDraftsLeave()).toBe(true)
    detail.draftsPanelRef.value = { isDirty: false, requestLeave: vi.fn(async () => true) }
    expect(await detail.confirmDraftsLeave()).toBe(true)

    // Dirty panel refuses → tab unchanged and the query is restored.
    const requestLeave = vi.fn(async () => false)
    detail.draftsPanelRef.value = { isDirty: true, requestLeave }
    await detail.setTab('board')
    expect(requestLeave).toHaveBeenCalled()
    expect(detail.tab.value).toBe('requirementDrafts')

    await router.replace({ query: { tab: 'board' } })
    await flushPromises()
    expect(detail.tab.value).toBe('requirementDrafts')
    expect(router.currentRoute.value.query.tab).toBe('requirementDrafts')

    // Accepting the leave moves on.
    detail.draftsPanelRef.value = { isDirty: true, requestLeave: vi.fn(async () => true) }
    await router.replace({ query: { tab: 'notify' } })
    await flushPromises()
    expect(detail.tab.value).toBe('notify')

    app.unmount()
  })

  it('writes a default tab into the query when it is missing or unknown', async () => {
    const { detail, app, router } = await withProjectDetail('/projects/proj-a?tab=nonsense')
    expect(detail.tab.value).toBe('board')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('board')

    await detail.setTab('audit')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('audit')
    // Same tab twice does not push another entry.
    await detail.setTab('audit')
    expect(detail.tab.value).toBe('audit')
    expect(detail.auditForceDenied.value).toBe(false)

    app.unmount()
  })

  it('exposes the PM inline sub-view transitions', async () => {
    const { detail, app } = await withProjectDetail()
    detail.openPmSettings()
    expect(detail.pmView.value).toBe('settings')
    detail.backToPmChat()
    expect(detail.pmView.value).toBe('chat')
    expect(detail.pmRestoreMobileChat.value).toBe(true)

    detail.openNotifyChannelSettings()
    await flushPromises()
    expect(detail.tab.value).toBe('pmLeader')

    app.unmount()
  })

  it('saves project meta and validates the unknown-model alias', async () => {
    const { detail, app } = await withProjectDetail()

    detail.editName.value = '  Renamed  '
    detail.editUnknownModelDisplayName.value = 'Auto'
    await detail.saveMeta()
    expect(mocks.updateProject).toHaveBeenCalledWith('proj-a', {
      name: 'Renamed',
      description: 'desc',
      unknownModelDisplayName: 'Auto',
    })
    expect(mocks.toastSuccess).toHaveBeenCalled()

    detail.editUnknownModelDisplayName.value = 'x'.repeat(500)
    mocks.updateProject.mockClear()
    await detail.saveMeta()
    expect(detail.unknownModelDisplayNameError.value).toBeTruthy()
    expect(mocks.updateProject).not.toHaveBeenCalled()

    detail.clearUnknownModelDisplayName()
    await flushPromises()
    expect(detail.editUnknownModelDisplayName.value).toBe('')
    expect(detail.unknownModelDisplayNameError.value).toBe('')

    mocks.updateProject.mockRejectedValueOnce(new Error('save failed'))
    await detail.saveMeta()
    expect(mocks.toastError).toHaveBeenCalledWith('save failed')

    detail.project.value = null
    mocks.updateProject.mockClear()
    await detail.saveMeta()
    await detail.saveVars()
    expect(mocks.updateProject).not.toHaveBeenCalled()

    app.unmount()
  })

  it('edits and saves project variables', async () => {
    const { detail, app } = await withProjectDetail()
    expect(detail.varRows.value).toHaveLength(1)
    expect(detail.VAR_TYPES.value).toHaveLength(5)
    expect(detail.existingNames()).toEqual(['WF'])

    detail.addVarRow()
    const row = detail.varRows.value[1]!
    detail.onVarNameChange(row, 'REGION')
    expect(row.name).toBe('REGION')

    detail.onVarTypeChange(row, 'bool')
    expect(row.value).toBe(false)
    expect(detail.isBoolTrue(row)).toBe(false)
    detail.setBoolValue(row, true)
    detail.onVarTypeChange(row, 'bool')
    expect(detail.isBoolTrue(row)).toBe(true)

    // Switching bool→number coerces the boolean instead of dropping it.
    detail.onVarTypeChange(row, 'number')
    expect(row.value).toBe(1)
    detail.onVarValueInput(row, '42', true)
    expect(row.value).toBe(42)
    detail.onVarTypeChange(row, 'number')
    expect(row.value).toBe(42)

    detail.onVarTypeChange(row, 'select')
    expect(row.options).toBe('')
    row.options = 'a, b ,,c'
    expect(detail.selectOptions(row)).toEqual(['a', 'b', 'c'])
    detail.onVarValueInput(row, 'a')
    expect(row.value).toBe('a')

    detail.onVarTypeChange(row, 'string')
    row.value = null as unknown as string
    detail.onVarTypeChange(row, 'string')
    expect(row.value).toBe('')
    row.value = null as unknown as string
    detail.onVarTypeChange(row, 'select')
    expect(row.value).toBe('')

    // The **** mask is dropped when the secret flag or the name changes.
    const secret = detail.varRows.value[0] as ProjectVariable
    detail.onVarSecretChange(secret, false)
    expect(secret.value).toBe('')
    secret.value = detail.SECRET_MASK
    secret.secret = true
    detail.onVarSecretChange(secret, true)
    expect(secret.value).toBe(detail.SECRET_MASK)
    detail.onVarNameChange(secret, 'RENAMED')
    expect(secret.value).toBe('')

    await detail.saveVars()
    expect(mocks.updateProject).toHaveBeenCalledWith('proj-a', {
      variables: expect.arrayContaining([expect.objectContaining({ name: 'RENAMED' })]),
    })

    mocks.updateProject.mockRejectedValueOnce(new Error('vars failed'))
    await detail.saveVars()
    expect(mocks.toastError).toHaveBeenCalledWith('vars failed')

    detail.removeVarRow(1)
    expect(detail.varRows.value).toHaveLength(1)

    app.unmount()
  })

  it('derives run launch fields from the input node and restores a draft', async () => {
    const { detail, app } = await withProjectDetail()
    const wf = detail.workflows.value[0]!

    const fields = detail.askFields(wf)
    expect(fields.map((f) => f.key)).toEqual(['branch', 'env', 'repos'])
    expect(fields[0]!.type).toBe('text')
    expect(fields[2]!.default).toBe('[{"name":"a"}]')
    expect(detail.fieldOptions(fields[1]!)).toEqual(['dev', 'prod'])
    expect(detail.fieldOptions({ key: 'x' } as never)).toEqual([])

    await detail.openRun(wf)
    expect(detail.runTarget.value?.id).toBe('wf-1')
    expect(detail.runInputs.value.branch).toBe('main')
    expect(detail.runInputs.value.env).toBe('dev')
    expect(detail.draftRestored.value).toBe(true)

    await detail.saveRunDraftClick()
    expect(mocks.saveRunDraft).toHaveBeenCalled()
    expect(mocks.toastSuccess).toHaveBeenCalled()

    mocks.saveRunDraft.mockResolvedValueOnce('quota_exceeded')
    await detail.saveRunDraftClick()
    expect(mocks.toastWarn).toHaveBeenCalled()
    mocks.saveRunDraft.mockResolvedValueOnce('partial')
    await detail.saveRunDraftClick()
    mocks.saveRunDraft.mockResolvedValueOnce('error')
    await detail.saveRunDraftClick()
    expect(mocks.toastError).toHaveBeenCalled()

    detail.onRunStarted()
    expect(mocks.clearRunDraft).toHaveBeenCalledWith('wf-1')
    detail.onRunStayed()
    await flushPromises()

    detail.closeRunModal()
    expect(detail.runTarget.value).toBeNull()
    mocks.saveRunDraft.mockClear()
    await detail.saveRunDraftClick()
    expect(mocks.saveRunDraft).not.toHaveBeenCalled()
    detail.onRunStarted()

    app.unmount()
  })

  it('routes workflow row actions', async () => {
    const { detail, app, router } = await withProjectDetail()
    const wf = detail.workflows.value[0]!

    detail.newWorkflow()
    await flushPromises()
    expect(router.currentRoute.value.query.projectId).toBe('proj-a')

    detail.toggleMenu('wf-1')
    expect(detail.openMenuId.value).toBe('wf-1')
    detail.toggleMenu('wf-1')
    expect(detail.openMenuId.value).toBeNull()
    expect(detail.menuIdFor('wf-1')).toBe('pd-wf-more-menu-wf-1')

    detail.toggleMenu('wf-1')
    detail.openEdit(wf)
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/workflows/wf-1/edit')
    expect(detail.openMenuId.value).toBeNull()

    detail.onViewRun('run-9')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/run-9')

    detail.openExport(wf)
    expect(detail.exportTarget.value?.id).toBe('wf-1')

    app.unmount()
  })

  it('copies a workflow through the preview endpoint', async () => {
    const { detail, app } = await withProjectDetail()
    const wf = detail.workflows.value[0]!

    await detail.openCopy(wf)
    expect(detail.copyModal.value?.suggestedName).toBe('WF copy')
    expect(detail.copyPreviewLoading.value).toBeNull()

    detail.onCopied({ ...wf, id: 'wf-2', name: 'WF copy' })
    expect(detail.workflows.value[0]!.id).toBe('wf-2')
    expect(detail.copyModal.value).toBeNull()

    mocks.copyPreviewWorkflow.mockRejectedValueOnce(new Error('nope'))
    await detail.openCopy(wf)
    expect(mocks.toastError).toHaveBeenCalled()
    detail.closeCopyModal()

    app.unmount()
  })

  it('deletes a workflow and the project itself', async () => {
    const { detail, app, router } = await withProjectDetail()
    const wf = detail.workflows.value[0]!

    await detail.confirmDeleteWf()
    expect(mocks.deleteWorkflow).not.toHaveBeenCalled()

    detail.openDeleteWf(wf)
    expect(detail.deleteWfTarget.value?.id).toBe('wf-1')
    await detail.confirmDeleteWf()
    expect(detail.workflows.value).toHaveLength(0)
    expect(detail.deleteWfTarget.value).toBeNull()

    detail.openDeleteWf(wf)
    mocks.deleteWorkflow.mockRejectedValueOnce(new Error('in use'))
    await detail.confirmDeleteWf()
    expect(detail.deleteWfError.value).toBe('in use')

    await detail.confirmDelete()
    expect(mocks.deleteProject).toHaveBeenCalledWith('proj-a')
    expect(mocks.writeStoredProjectId).toHaveBeenCalledWith('')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/projects')

    mocks.deleteProject.mockRejectedValueOnce(new Error('has runs'))
    await detail.confirmDelete()
    expect(detail.deleteError.value).toBe('has runs')

    detail.project.value = null
    mocks.deleteProject.mockClear()
    await detail.confirmDelete()
    expect(mocks.deleteProject).not.toHaveBeenCalled()

    app.unmount()
  })

  it('persists per-workflow notify policies', async () => {
    const { detail, app } = await withProjectDetail()
    const wf = detail.workflows.value[0]!

    expect(detail.wfNotifyMode(wf)).toBe('inherit')
    expect(detail.wfNotifyHas(wf, 'failed')).toBe(false)

    await detail.setWorkflowNotifyMode(wf, 'custom')
    expect(mocks.patchWorkflowNotifyPolicy).toHaveBeenCalledWith('wf-1', {
      mode: 'custom',
      events: ['waiting_human', 'failed'],
    })
    let saved = detail.workflows.value[0]!
    expect(detail.wfNotifyMode(saved)).toBe('custom')
    expect(detail.wfNotifyHas(saved, 'failed')).toBe(true)
    expect(detail.savingNotifyWfId.value).toBeNull()

    // Custom mode with existing events keeps them.
    await detail.setWorkflowNotifyMode(detail.workflows.value[0]!, 'custom')
    expect(mocks.patchWorkflowNotifyPolicy).toHaveBeenLastCalledWith('wf-1', {
      mode: 'custom',
      events: ['waiting_human', 'failed'],
    })

    await detail.setWorkflowNotifyMode(detail.workflows.value[0]!, 'off')
    saved = detail.workflows.value[0]!
    expect(detail.wfNotifyMode(saved)).toBe('off')

    await detail.toggleWorkflowNotifyEvent(saved, 'completed')
    saved = detail.workflows.value[0]!
    expect(detail.wfNotifyHas(saved, 'completed')).toBe(true)
    await detail.toggleWorkflowNotifyEvent(saved, 'completed')
    saved = detail.workflows.value[0]!
    expect(detail.wfNotifyHas(saved, 'completed')).toBe(false)

    mocks.patchWorkflowNotifyPolicy.mockRejectedValueOnce(new Error('notify failed'))
    await detail.persistWorkflowNotify(saved, { mode: 'inherit', events: [] })
    expect(mocks.toastError).toHaveBeenCalledWith('notify failed')
    expect(detail.savingNotifyWfId.value).toBeNull()

    app.unmount()
  })

  it('short-circuits an unchanged home-visibility toggle', async () => {
    const { detail, app } = await withProjectDetail()
    const wf = detail.workflows.value[0]!
    await detail.toggleWorkflowShowOnHome(wf, false)
    expect(mocks.patchWorkflowHomeVisibility).not.toHaveBeenCalled()
    expect(detail.savingHomeWfId.value).toBeNull()

    mocks.patchWorkflowHomeVisibility.mockRejectedValueOnce('plain string failure')
    await detail.toggleWorkflowShowOnHome(wf, true)
    expect(detail.workflows.value[0]!.showOnHome).toBe(false)

    app.unmount()
  })

  it('refreshes after onboarding completes', async () => {
    mocks.getProject.mockResolvedValue(sampleProject({ id: 'proj-default' }))
    mocks.listWorkflows.mockResolvedValue([])
    mocks.listAgents.mockResolvedValue([])
    const { detail, app } = await withProjectDetail('/projects/proj-default')
    expect(detail.isOnboardingEmpty.value).toBe(true)

    detail.openOnboarding()
    expect(mocks.openRetryOnboarding).toHaveBeenCalledWith('proj-default')

    mocks.listWorkflows.mockResolvedValue([askWorkflow()])
    mocks.listAgents.mockResolvedValue([{ name: 'agent-a', projectId: 'proj-a' }])
    firstInstallCompletedAt.value = '2026-02-02T00:00:00Z'
    await flushPromises()
    expect(detail.workflows.value).toHaveLength(1)
    expect(detail.projectAgents.value).toHaveLength(1)
    expect(detail.isOnboardingEmpty.value).toBe(false)

    mocks.listAgents.mockRejectedValueOnce(new Error('offline'))
    await detail.refreshAfterOnboarding?.()

    // A failing workflow refresh keeps the current list.
    mocks.listWorkflows.mockRejectedValueOnce(new Error('offline'))
    await detail.reloadWorkflows()
    expect(detail.workflows.value).toHaveLength(1)
    expect(detail.wfRefreshing.value).toBe(false)
    expect(detail.showRefreshProgress.value).toBe(false)

    app.unmount()
  })

  it('reloads everything when the route project changes', async () => {
    const { detail, app, router } = await withProjectDetail()
    expect(detail.project.value?.id).toBe('proj-a')

    mocks.getProject.mockResolvedValue(sampleProject({ id: 'proj-b', name: 'Project B' }))
    await router.push('/projects/proj-b')
    await flushPromises()
    await nextTick()
    expect(detail.projectId.value).toBe('proj-b')
    expect(detail.project.value?.id).toBe('proj-b')
    expect(mocks.writeStoredProjectId).toHaveBeenCalledWith('proj-b')

    app.unmount()
  })

  it('closes the row menu on outside click, escape and scroll', async () => {
    const { detail, app } = await withProjectDetail()
    const inside = document.createElement('div')
    inside.setAttribute('data-wf-menu', '')
    document.body.appendChild(inside)

    detail.toggleMenu('wf-1')
    detail.onDocClick({ target: inside } as unknown as MouseEvent)
    expect(detail.openMenuId.value).toBe('wf-1')

    detail.onDocClick({ target: document.body } as unknown as MouseEvent)
    expect(detail.openMenuId.value).toBeNull()
    detail.onDocClick({ target: null } as unknown as MouseEvent)

    detail.toggleMenu('wf-1')
    detail.onKeydown(new KeyboardEvent('keydown', { key: 'a' }))
    expect(detail.openMenuId.value).toBe('wf-1')
    detail.onKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(detail.openMenuId.value).toBeNull()

    detail.onScrollClose()
    detail.toggleMenu('wf-1')
    detail.onScrollClose()
    expect(detail.openMenuId.value).toBeNull()

    inside.remove()
    app.unmount()
  })

  it('widens the favorite button for latin locales', async () => {
    const { detail, app, i18n } = await withProjectDetail()
    expect(detail.favoriteBtnMinWidth.value).toBe('4.75rem')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect(detail.favoriteBtnMinWidth.value).toBe('5.75rem')
    app.unmount()
  })
})
