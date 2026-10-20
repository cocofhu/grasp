// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createApp, defineComponent, nextTick, shallowRef } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Workflow } from '@/lib/shared/types'

let currentRoute = shallowRef({ path: '/dashboard', query: {} as Record<string, string | string[] | null> })

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  toastWarn: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  listWorkflows: vi.fn(),
  listProjects: vi.fn(),
  listAgents: vi.fn(async () => [
    { name: 'clarifier', capabilities: { interaction: 'clarify', tools: ['ask_question'] } },
    { name: 'worker', capabilities: { interaction: 'auto' } },
  ]),
  patchWorkflowHomeVisibility: vi.fn(),
  startRun: vi.fn(),
  getRun: vi.fn(),
  reactReply: vi.fn(),
  readStoredProjectId: vi.fn(() => 'proj-1'),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push, replace: mocks.replace, currentRoute }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    warn: mocks.toastWarn,
    error: mocks.toastError,
    success: mocks.toastSuccess,
  }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  readStoredProjectId: () => mocks.readStoredProjectId(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkflows: mocks.listWorkflows,
      listProjects: mocks.listProjects,
      listAgents: mocks.listAgents,
      patchWorkflowHomeVisibility: mocks.patchWorkflowHomeVisibility,
      startRun: mocks.startRun,
      getRun: mocks.getRun,
      reactReply: mocks.reactReply,
    },
  }
})

import {
  useHomeApproveChat,
  HOME_WORKFLOW_MEMORY_KEY,
  HOME_PRIORITY_MEMORY_KEY,
  HOME_COMPOSER_DRAFT_DEBOUNCE_MS,
  parseRunPriority,
  resolveHomeProjectName,
} from './useHomeApproveChat'
import {
  HOME_COMPOSER_DRAFT_KEY,
  saveHomeComposerDraft,
  loadHomeComposerDraft,
} from './homeComposerDraft'
import { peekHomeApproveHandoff } from './homeApproveHandoff'
import {
  __resetDraftIdbForTests,
  __setDraftIdbBackendForTests,
  createMemoryDraftIdb,
} from './draftIdb'

const approveWf: Workflow = {
  id: 'wf-ap',
  name: '自我迭代PRO',
  description: '开发前澄清 + 计划',
  status: 'published',
  version: 1,
  updatedAt: '',
  needsRepo: false,
  projectId: 'proj-1',
  showOnHome: true,
  nodes: [
    { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
    { id: 'ap', type: 'agent', label: '澄清', position: { x: 0, y: 0 }, config: { agent_profile: 'clarifier' } },
    { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
  ],
  edges: [
    { id: 'e1', source: 'in', target: 'ap' },
    { id: 'e2', source: 'ap', target: 'out' },
  ],
}

const approveWfB: Workflow = {
  ...approveWf,
  id: 'wf-lite',
  name: '快速澄清 Lite',
  projectId: 'proj-2',
}

const reactWf: Workflow = {
  ...approveWf,
  id: 'wf-react',
  name: '实现流',
  nodes: [
    { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
    { id: 'r', type: 'agent', label: '实现', position: { x: 0, y: 0 }, config: { agent_profile: 'worker' } },
  ],
  edges: [{ id: 'e1', source: 'in', target: 'r' }],
}

function withSetup<T>(fn: () => T): T {
  let result!: T
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      result = fn()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return result
}

describe('useHomeApproveChat', () => {
  beforeEach(() => {
    currentRoute = shallowRef({ path: '/dashboard', query: {} })
    mocks.push.mockReset()
    mocks.replace.mockReset()
    mocks.replace.mockImplementation((to: typeof currentRoute.value) => {
      currentRoute.value = to
      return Promise.resolve()
    })
    mocks.toastWarn.mockReset()
    mocks.toastError.mockReset()
    mocks.toastSuccess.mockReset()
    mocks.listWorkflows.mockReset()
    mocks.listProjects.mockReset()
    mocks.patchWorkflowHomeVisibility.mockReset()
    mocks.startRun.mockReset()
    mocks.getRun.mockReset()
    mocks.reactReply.mockReset()
    mocks.readStoredProjectId.mockReturnValue('proj-1')
    mocks.listWorkflows.mockResolvedValue([approveWf, reactWf])
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: '综合项目组', description: '', variables: [] },
      { id: 'proj-2', name: 'SkillHub', description: '', variables: [] },
    ])
    mocks.patchWorkflowHomeVisibility.mockResolvedValue({ ...approveWf, showOnHome: false })
    mocks.startRun.mockResolvedValue({ id: 'run-1', status: 'queued' })
    mocks.getRun.mockResolvedValue({
      id: 'run-1',
      status: 'waiting_human',
      nodes: approveWf.nodes,
      nodeRuns: { ap: { nodeId: 'ap', status: 'waiting_human' } },
    })
    mocks.reactReply.mockResolvedValue({ status: 'ok' })
    __setDraftIdbBackendForTests(createMemoryDraftIdb())
    localStorage.removeItem(HOME_WORKFLOW_MEMORY_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
  })

  afterEach(() => {
    vi.useRealTimers()
    __resetDraftIdbForTests()
    localStorage.removeItem(HOME_WORKFLOW_MEMORY_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
  })

  it('filters to published clarify-first workflows', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
    expect(chat.selected.value?.id).toBe('wf-ap')
  })

  // plan g2.2 — load listProjects in parallel and attach projectName
  it('resolves projectName from listProjects in parallel with listWorkflows', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(mocks.listProjects).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(chat.homeWorkflows.value[0]?.projectName).toBe('综合项目组')
  })

  it('falls back to projectId when the project list has no matching name', async () => {
    mocks.listProjects.mockResolvedValue([])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value[0]?.projectName).toBe('proj-1')
  })

  it('leaves projectName empty when the workflow has no projectId', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, projectId: undefined }, reactWf])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value[0]?.projectName).toBe('')
  })

  it('still loads workflows when listProjects fails', async () => {
    mocks.listProjects.mockRejectedValue(new Error('projects down'))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.loadError.value).toBeNull()
    expect(chat.homeWorkflows.value[0]?.id).toBe('wf-ap')
    expect(chat.homeWorkflows.value[0]?.projectName).toBe('proj-1')
  })

  it('resolveHomeProjectName prefers mapped name over id', () => {
    const map = new Map([['proj-1', '综合项目组']])
    expect(resolveHomeProjectName('proj-1', map)).toBe('综合项目组')
    expect(resolveHomeProjectName('missing', map)).toBe('missing')
    expect(resolveHomeProjectName('', map)).toBe('')
    expect(resolveHomeProjectName(undefined, map)).toBe('')
  })

  // plan g2.1 — cross-project list without project gate
  it('loads workflows across projects without requiring a stored projectId', async () => {
    mocks.readStoredProjectId.mockReturnValue('')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB, reactWf])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(mocks.listWorkflows).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const arg = mocks.listWorkflows.mock.calls[0]?.[0] || {}
    expect(arg.projectId).toBeUndefined()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])
    expect(chat.projectId.value).toBe('proj-1')
  })

  it('uses an explicit project compose link ahead of memory and the restored workflow', async () => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-ap')
    await saveHomeComposerDraft('为选中的项目发起需求', [], 'wf-ap')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB, reactWf])

    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()

    expect(chat.selected.value?.id).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')
    expect(chat.draft.value).toBe('为选中的项目发起需求')
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])
    expect(mocks.listWorkflows.mock.calls[0]?.[0]).not.toHaveProperty('projectId')
    await chat.send()
    expect(mocks.startRun.mock.calls[0]?.[0]).toBe('wf-lite')
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-2' },
    })
  })

  it('applies same-page project query changes and still allows a manual cross-project choice', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    chat.draft.value = '保留未发送内容'
    const listCalls = mocks.listWorkflows.mock.calls.length

    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-lite')
    expect(chat.draft.value).toBe('保留未发送内容')
    expect(mocks.listWorkflows).toHaveBeenCalledTimes(listCalls)

    chat.selectWorkflow('wf-ap')
    await chat.load()
    expect(chat.selected.value?.id).toBe('wf-ap')
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])

    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    await nextTick()
    expect(chat.projectId.value).toBe('proj-1')
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-lite')
  })

  it('uses the latest project query when the workflow request is still loading', async () => {
    let resolveList!: (value: Workflow[]) => void
    mocks.listWorkflows.mockReturnValueOnce(new Promise<Workflow[]>((resolve) => { resolveList = resolve }))
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value).toBeNull()
    expect(mocks.push).not.toHaveBeenCalled()

    resolveList([approveWf, approveWfB])
    await flushPromises()
    expect(chat.selected.value?.id).toBe('wf-lite')
  })

  it('clears only projectId after a manual cross-project choice so the original compose link works again', async () => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1', from: 'sidebar' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    expect(chat.selected.value?.id).toBe('wf-ap')

    chat.selectWorkflow('wf-lite')
    await flushPromises()
    expect(mocks.replace).toHaveBeenCalledWith({ path: '/dashboard', query: { from: 'sidebar' } })
    expect(currentRoute.value.query).toEqual({ from: 'sidebar' })
    expect(chat.selected.value?.id).toBe('wf-lite')

    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-ap')
  })

  it('does not let a late composer draft overwrite a project chosen after workflows load', async () => {
    const backend = createMemoryDraftIdb()
    __setDraftIdbBackendForTests(backend)
    await saveHomeComposerDraft('恢复较慢的草稿', [{ data: btoa('image'), mimeType: 'image/png' }], 'wf-ap')
    const stored = await backend.getHome()
    let resolveDraft!: (value: typeof stored) => void
    const pendingDraft = new Promise<typeof stored>((resolve) => { resolveDraft = resolve })
    __setDraftIdbBackendForTests({ ...backend, getHome: () => pendingDraft })
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-lite')

    resolveDraft(stored)
    await flushPromises()
    expect(chat.draft.value).toBe('恢复较慢的草稿')
    expect(chat.attachments.value).toHaveLength(1)
    expect(chat.selected.value?.id).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')
  })

  it.each([
    { list: [] },
    { list: [approveWf, { ...approveWfB, showOnHome: false }] },
    { list: [approveWf, { ...approveWfB, status: 'draft' as const }] },
    { list: [approveWf, { ...reactWf, projectId: 'proj-2' }] },
  ])('returns to a project with no startable home workflow instead of choosing another project (%#)', async ({ list }) => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-ap')
    mocks.listWorkflows.mockResolvedValue(list)
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()

    expect(chat.selectedId.value).toBe('')
    expect(chat.selected.value).toBeNull()
    expect(chat.projectId.value).toBe('proj-2')
    expect(mocks.toastWarn).toHaveBeenCalledWith(pages.pages.dashboard.noWorkflows)
    expect(mocks.push).toHaveBeenCalledWith({ path: '/projects/proj-2' })
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-ap')
    await chat.load()
    expect(mocks.push).toHaveBeenCalledTimes(1)
    chat.draft.value = '不能发给其他项目'
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })

  it('keeps explicit project context and the draft when its last workflow is hidden', async () => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    let resolveHide!: (workflow: Workflow) => void
    mocks.patchWorkflowHomeVisibility.mockReturnValueOnce(new Promise<Workflow>((resolve) => { resolveHide = resolve }))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    chat.draft.value = '只交给项目一的草稿'
    chat.attachments.value = [{ data: btoa('image'), mimeType: 'image/png' }]

    const hiding = chat.hideWorkflowFromHome(approveWf)
    await nextTick()
    expect(chat.selected.value).toBeNull()
    expect(mocks.push).not.toHaveBeenCalled()
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()

    resolveHide({ ...approveWf, showOnHome: false })
    await hiding
    await flushPromises()
    expect(chat.selected.value).toBeNull()
    expect(chat.projectId.value).toBe('proj-1')
    expect(chat.draft.value).toBe('只交给项目一的草稿')
    expect(chat.attachments.value).toHaveLength(1)
    expect(mocks.toastWarn).toHaveBeenCalledWith(pages.pages.dashboard.noWorkflows)
    expect(mocks.push).toHaveBeenCalledWith({ path: '/projects/proj-1' })
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })

  it('does not fall back to another project when a reload removes the requested workflow', async () => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    chat.draft.value = '刷新也要保留的草稿'
    mocks.listWorkflows.mockResolvedValue([approveWfB])

    await chat.load()
    await flushPromises()
    expect(chat.homeWorkflows.value.map((workflow) => workflow.id)).toEqual(['wf-lite'])
    expect(chat.selected.value).toBeNull()
    expect(chat.selectedId.value).toBe('')
    expect(chat.projectId.value).toBe('proj-1')
    expect(chat.draft.value).toBe('刷新也要保留的草稿')
    expect(mocks.push).toHaveBeenCalledWith({ path: '/projects/proj-1' })
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })

  it('selects a remaining workflow from the explicit project instead of the first other-project card', async () => {
    const remaining = { ...approveWf, id: 'wf-project-one-second' }
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB, remaining])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()

    await chat.hideWorkflowFromHome(approveWf)
    await flushPromises()
    expect(chat.selected.value?.id).toBe(remaining.id)
    expect(chat.projectId.value).toBe('proj-1')
    expect(currentRoute.value.query.projectId).toBe('proj-1')
    expect(mocks.push).not.toHaveBeenCalled()
  })

  it('restores the requested workflow without redirecting when its optimistic hide fails', async () => {
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    mocks.patchWorkflowHomeVisibility.mockRejectedValueOnce(new Error('patch failed'))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    chat.draft.value = '隐藏失败时保留'

    await chat.hideWorkflowFromHome(approveWf)
    await flushPromises()
    expect(chat.selected.value?.id).toBe('wf-ap')
    expect(chat.projectId.value).toBe('proj-1')
    expect(chat.draft.value).toBe('隐藏失败时保留')
    expect(mocks.push).not.toHaveBeenCalled()
    expect(mocks.toastError).toHaveBeenCalledWith('patch failed')
  })

  // plan g2.3 — remember last selection; fall back when memory is stale
  it('defaults to remembered workflow and falls back when memory is gone', async () => {
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')

    mocks.listWorkflows.mockResolvedValue([approveWf])
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-ap')
  })

  it('selectWorkflow updates selection memory and project context', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.selectWorkflow('wf-lite')
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-lite')
  })

  it('hides a workflow optimistically and falls back to the next visible workflow', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()

    await chat.hideWorkflowFromHome(approveWf)

    expect(mocks.patchWorkflowHomeVisibility).toHaveBeenCalledWith('wf-ap', false)
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-lite'])
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-lite')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已更新首页可见')
  })

  it('rolls back the card and selection when hiding fails', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    mocks.patchWorkflowHomeVisibility.mockRejectedValue(new Error('patch failed'))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()

    await chat.hideWorkflowFromHome(approveWf)

    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-ap')
    expect(mocks.toastError).toHaveBeenCalledWith('patch failed')
  })

  it('parseRunPriority falls back to normal for empty or illegal values (plan g1.4)', () => {
    expect(parseRunPriority('high')).toBe('high')
    expect(parseRunPriority('low')).toBe('low')
    expect(parseRunPriority('normal')).toBe('normal')
    expect(parseRunPriority('')).toBe('normal')
    expect(parseRunPriority('urgent')).toBe('normal')
    expect(parseRunPriority(null)).toBe('normal')
  })

  it('restores last priority from localStorage and rejects illegal values (plan g1.4)', async () => {
    localStorage.setItem(HOME_PRIORITY_MEMORY_KEY, 'high')
    const highChat = withSetup(() => useHomeApproveChat())
    expect(highChat.launchPriority.value).toBe('high')

    localStorage.setItem(HOME_PRIORITY_MEMORY_KEY, 'urgent')
    const fallback = withSetup(() => useHomeApproveChat())
    expect(fallback.launchPriority.value).toBe('normal')
  })

  it('persists priority via selectPriority without writing Composer IndexedDB draft (plan g1.4)', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    chat.selectPriority('low')
    expect(localStorage.getItem(HOME_PRIORITY_MEMORY_KEY)).toBe('low')
    expect(chat.launchPriority.value).toBe('low')
  })

  it('passes the selected priority as startRun fourth argument (plan g2.1 / g3.3)', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '紧急需求'
    chat.selectPriority('high')
    await chat.send()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'high', [], {
      title: '紧急需求',
      firstMessage: { text: '紧急需求', images: [] },
      publishedSnapshot: true,
    })
  })

  it('starts a run carrying the first message, then opens inbox', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '  把登录做清楚  '
    await chat.send()
    await nextTick()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '把登录做清楚',
      firstMessage: { text: '把登录做清楚', images: [] },
      publishedSnapshot: true,
    })
    // The engine delivers the message once the approve node parks.
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-1' },
    })
  })

  it('carries attachments on the first message and navigates to inbox', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.attachments.value = [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }]
    await chat.send()
    await nextTick()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: 'shot.png',
      firstMessage: {
        text: '',
        images: [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }],
      },
      publishedSnapshot: true,
    })
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-1' },
    })
  })

  it('opens inbox immediately without polling for the Approve park', async () => {
    mocks.getRun.mockImplementation(() => new Promise(() => {}))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.attachments.value = [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }]
    chat.draft.value = '附图说明'
    void chat.send()
    await flushPromises()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-1' },
    })
    expect(mocks.getRun).not.toHaveBeenCalled()
    expect(mocks.reactReply).not.toHaveBeenCalled()
  })

  it('goGates writes the current workflow projectId dynamically (plan g1.1)', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.selectWorkflow('wf-lite')
    chat.draft.value = '跨项目提交'
    await chat.send()
    await nextTick()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-2' },
    })
  })

  it('keeps an in-flight run on its original workflow when a project compose link changes the selection', async () => {
    const otherWorkflow = {
      ...approveWfB,
      nodes: [approveWfB.nodes[0], { ...approveWfB.nodes[1], id: 'ap-b' }, approveWfB.nodes[2]],
      edges: [{ ...approveWfB.edges[0], target: 'ap-b' }, { ...approveWfB.edges[1], source: 'ap-b' }],
    }
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([approveWf, otherWorkflow])
    let resolveStart!: (result: { id: string; status: string }) => void
    mocks.startRun.mockReturnValueOnce(new Promise((resolve) => { resolveStart = resolve }))
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    const image = { data: btoa('original-image'), mimeType: 'image/png', name: 'project-one.png' }
    chat.draft.value = '项目一原始需求'
    chat.attachments.value = [image]
    const sending = chat.send()

    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-lite')
    expect(chat.draft.value).toBe('项目一原始需求')
    expect(chat.attachments.value).toEqual([image])
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '项目一原始需求',
      firstMessage: { text: '项目一原始需求', images: [image] },
      publishedSnapshot: true,
    })

    resolveStart({ id: 'run-original-one', status: 'queued' })
    await sending
    expect(mocks.push).toHaveBeenLastCalledWith({
      path: '/gates',
      query: { run: 'run-original-one', node: 'ap', projectId: 'proj-1' },
    })
    expect(peekHomeApproveHandoff()).toEqual({
      runId: 'run-original-one', nodeId: 'ap', text: '项目一原始需求', images: [image],
    })
    expect(chat.draft.value).toBe('')
    expect(chat.attachments.value).toEqual([])
  })

  it('uses the launch target for the handoff even if the dashboard now selects another project', async () => {
    const requiresTopic = {
      ...approveWf,
      nodes: [
        { ...approveWf.nodes[0], config: { variables: [{ name: 'topic', ask: true, required: true, type: 'text', value: '' }] } },
        approveWf.nodes[1], approveWf.nodes[2],
      ],
    }
    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-1' } }
    mocks.listWorkflows.mockResolvedValue([requiresTopic, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    const image = { data: btoa('launch-image'), mimeType: 'image/png' }
    chat.draft.value = '由项目一启动对话'
    chat.attachments.value = [image]
    await chat.send()
    expect(chat.launchTarget.value?.id).toBe('wf-ap')
    expect(mocks.startRun).not.toHaveBeenCalled()

    currentRoute.value = { path: '/dashboard', query: { projectId: 'proj-2' } }
    await nextTick()
    expect(chat.selected.value?.id).toBe('wf-lite')
    await chat.onLaunchStarted('run-launch-one')
    expect(mocks.push).toHaveBeenLastCalledWith({
      path: '/gates',
      query: { run: 'run-launch-one', node: 'ap', projectId: 'proj-1' },
    })
    expect(peekHomeApproveHandoff()).toEqual({
      runId: 'run-launch-one', nodeId: 'ap', text: '由项目一启动对话', images: [image],
    })
  })

  it('goGates omits projectId when the workflow has none (plan g1.1 / g2.3)', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, projectId: undefined }, reactWf])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '无项目工作流'
    await chat.send()
    await nextTick()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap' },
    })
    expect(mocks.push.mock.calls[0][0].query).not.toHaveProperty('projectId')
  })

  it('navigates even when the home view unmounts mid-send', async () => {
    let result!: ReturnType<typeof useHomeApproveChat>
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const Comp = defineComponent({
      setup() {
        result = useHomeApproveChat()
        return () => null
      },
    })
    const app = createApp(Comp)
    app.use(i18n)
    app.mount(document.createElement('div'))
    await flushPromises()
    await result.load()
    result.draft.value = '卸载后仍要跳转'
    const sendPromise = result.send()
    app.unmount()
    await sendPromise
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '卸载后仍要跳转',
      firstMessage: { text: '卸载后仍要跳转', images: [] },
      publishedSnapshot: true,
    })
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-1' },
    })
  })

  it('opens the launch modal when required ask fields are empty', async () => {
    const missing = {
      ...approveWf,
      nodes: [
        {
          id: 'in',
          type: 'input' as const,
          label: '开始',
          position: { x: 0, y: 0 },
          config: {
            variables: [{ name: 'repos', ask: true, required: true, type: 'repos', value: [] }],
          },
        },
        approveWf.nodes[1],
        approveWf.nodes[2],
      ],
    }
    mocks.listWorkflows.mockResolvedValue([missing])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '先澄清目标'
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()
    expect(chat.launchOpen.value).toBe(true)
    expect(mocks.toastWarn).toHaveBeenCalled()
  })

  // plan g2.1 — silent restore + toast when non-empty draft exists
  it('restores composer draft on mount and toasts once', async () => {
    const imgData = btoa('img')
    await saveHomeComposerDraft('未发送草稿', [{ data: imgData, mimeType: 'image/png', name: 'a.png' }], 'wf-ap')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.draft.value).toBe('未发送草稿')
    expect(chat.attachments.value).toEqual([{ data: imgData, mimeType: 'image/png', name: 'a.png' }])
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('草稿已恢复')
  })

  // plan g2.1 — no toast when no draft
  it('does not toast restore when there is no composer draft', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.draft.value).toBe('')
    expect(mocks.toastSuccess).not.toHaveBeenCalled()
  })

  // plan g2.4 — draft workflow beats lastWorkflow memory
  it('prefers draft workflow over HOME_WORKFLOW_MEMORY_KEY', async () => {
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-lite')
    await saveHomeComposerDraft('草稿管道优先', [], 'wf-ap')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(chat.draft.value).toBe('草稿管道优先')
  })

  // plan g2.4 — unavailable draft workflow still restores text/attachments
  it('restores text when draft workflow is unavailable', async () => {
    await saveHomeComposerDraft('管道已下线', [{ data: btoa('x'), mimeType: 'image/png' }], 'wf-gone')
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.draft.value).toBe('管道已下线')
    expect(chat.attachments.value).toHaveLength(1)
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('草稿已恢复')
  })

  // plan g2.2 — debounced auto-save
  it('debounces auto-save of text, attachments, and workflow', async () => {
    vi.useFakeTimers()
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = 'a'
    chat.draft.value = 'ab'
    chat.draft.value = 'abc'
    expect(await loadHomeComposerDraft()).toBeNull()
    await vi.advanceTimersByTimeAsync(HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
    await flushPromises()
    expect((await loadHomeComposerDraft())?.text).toBe('abc')
    chat.selectWorkflow('wf-lite')
    chat.attachments.value = [{ data: btoa('z'), mimeType: 'image/png' }]
    await vi.advanceTimersByTimeAsync(HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
    await flushPromises()
    const saved = await loadHomeComposerDraft()
    expect(saved?.pipelineId).toBe('wf-lite')
    expect(saved?.attachments?.[0]).toMatchObject({ data: btoa('z'), mimeType: 'image/png' })
  })

  // plan g2.2 — unmount flushes pending debounce so quick leave still persists
  it('flushes pending composer draft save on unmount', async () => {
    vi.useFakeTimers()
    let result!: ReturnType<typeof useHomeApproveChat>
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const Comp = defineComponent({
      setup() {
        result = useHomeApproveChat()
        return () => null
      },
    })
    const app = createApp(Comp)
    app.use(i18n)
    app.mount(document.createElement('div'))
    await flushPromises()
    await result.load()
    result.draft.value = '离开前未防抖落盘'
    expect(await loadHomeComposerDraft()).toBeNull()
    app.unmount()
    await flushPromises()
    expect((await loadHomeComposerDraft())?.text).toBe('离开前未防抖落盘')
  })

  // plan g2.4 — empty workflow list must not drop draft preference before load
  it('keeps draft workflow preference while workflow list is still empty', async () => {
    await saveHomeComposerDraft('等列表', [], 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    // Before load: preference from hydrate must survive empty workflows watch.
    expect(chat.draft.value).toBe('等列表')
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-lite')
  })

  // plan g2.3 — clear draft after successful startRun
  it('clears local composer draft after successful send', async () => {
    await saveHomeComposerDraft('will clear', [], 'wf-ap')
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.draft.value).toBe('will clear')
    await chat.send()
    await nextTick()
    await flushPromises()
    expect(await loadHomeComposerDraft()).toBeNull()
    expect(chat.draft.value).toBe('')
  })

  // plan g2.3 — keep draft when only opening launch modal (not started)
  it('keeps composer draft when send opens launch modal without starting', async () => {
    const missing = {
      ...approveWf,
      nodes: [
        {
          id: 'in',
          type: 'input' as const,
          label: '开始',
          position: { x: 0, y: 0 },
          config: {
            variables: [{ name: 'repos', ask: true, required: true, type: 'repos', value: [] }],
          },
        },
        approveWf.nodes[1],
        approveWf.nodes[2],
      ],
    }
    mocks.listWorkflows.mockResolvedValue([missing])
    vi.useFakeTimers()
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '仍要保留'
    await vi.advanceTimersByTimeAsync(HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
    await flushPromises()
    await chat.send()
    expect(chat.launchOpen.value).toBe(true)
    expect((await loadHomeComposerDraft())?.text).toBe('仍要保留')
  })

  // plan g2.3 — clear on onLaunchStarted
  it('clears composer draft when onLaunchStarted runs after RunLaunch', async () => {
    await saveHomeComposerDraft('launch clear', [], 'wf-ap')
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    await chat.onLaunchStarted('run-99')
    await flushPromises()
    expect(await loadHomeComposerDraft()).toBeNull()
    expect(chat.draft.value).toBe('')
  })

  // plan g3.2 — auto-save quota toast only once
  it('toasts draftTooLarge only once across consecutive auto-saves (plan g3.2)', async () => {
    const { __setDraftIdbBackendForTests: setBackend, createMemoryDraftIdb: mem } = await import('./draftIdb')
    const base = mem()
    setBackend({
      ...base,
      putHome: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    })
    vi.useFakeTimers()
    mocks.listWorkflows.mockResolvedValue([approveWf])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = 'a'
    chat.attachments.value = [{ data: btoa('x'), mimeType: 'image/png' }]
    await vi.advanceTimersByTimeAsync(HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
    await flushPromises()
    chat.draft.value = 'ab'
    await vi.advanceTimersByTimeAsync(HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
    await flushPromises()
    expect(mocks.toastWarn).toHaveBeenCalledTimes(1)
    expect(mocks.toastWarn).toHaveBeenCalledWith('草稿过大，请减少图片或文字')
  })

  // plan g3.1 / g3.3 — showOnHome=false is hidden even when published clarify-first
  it('hides published clarify-first workflows when showOnHome is false or missing', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, id: 'wf-off', showOnHome: false },
      { ...approveWf, id: 'wf-missing', showOnHome: undefined },
      approveWf,
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
  })

  // Never-published drafts and non-clarify-first heads stay off the start list.
  it('still hides never-published drafts and non-clarify-first workflows when showOnHome is true', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, id: 'wf-draft', status: 'draft' as const, showOnHome: true },
      { ...reactWf, showOnHome: true },
      approveWf,
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
  })

  // plan g3.1 / g3.3 — remembered hidden id falls back to first visible
  it('falls back when remembered workflow is no longer showOnHome', async () => {
    localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([
      approveWf,
      { ...approveWfB, showOnHome: false },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(chat.selected.value?.id).toBe('wf-ap')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-ap')
  })

  // plan g3.3 — all hidden → empty workflows (empty state / disabled select)
  it('yields an empty workflow list when every candidate is hidden', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, showOnHome: false },
      { ...approveWfB, showOnHome: false },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value).toHaveLength(0)
    expect(chat.selected.value).toBeNull()
  })

  // plan g2.1 / g2.2 — from-baseline success reloads and selects the new card
  it('reloadAfterCreate loads the new workflow and selects it', async () => {
    const created = { ...approveWf, id: 'wf-new', name: '首页新建', showOnHome: true }
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
    mocks.listWorkflows.mockResolvedValue([approveWf, created])
    await chat.reloadAfterCreate('wf-new')
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-new'])
    expect(chat.selectedId.value).toBe('wf-new')
    expect(localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)).toBe('wf-new')
  })

  // plan g2.2 — reload failure must not wipe existing home cards
  it('reloadAfterCreate keeps the previous list when reload fails', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    mocks.listWorkflows.mockRejectedValue(new Error('reload failed'))
    await chat.reloadAfterCreate('wf-new')
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
    expect(chat.selectedId.value).toBe('wf-ap')
  })

  it('keeps a card from the published snapshot after the head becomes a draft', async () => {
    const draftHead = {
      ...approveWf,
      name: '草稿名',
      description: '草稿说明',
      status: 'draft' as const,
      version: 2,
      publishedVersion: 1,
      showOnHome: true,
      nodes: reactWf.nodes,
      edges: reactWf.edges,
      publishedSnapshot: {
        version: 1,
        name: '已发布名',
        description: '已发布说明',
        nodes: approveWf.nodes,
        edges: approveWf.edges,
      },
    }
    const never = {
      ...approveWf,
      id: 'wf-never',
      name: '从未发布',
      status: 'draft' as const,
      version: 1,
      publishedVersion: 0,
      showOnHome: true,
    }
    mocks.listWorkflows.mockResolvedValue([draftHead, never, { ...approveWf, id: 'wf-off', showOnHome: false, publishedVersion: 1, publishedSnapshot: draftHead.publishedSnapshot }])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
    expect(chat.homeWorkflows.value[0]?.name).toBe('已发布名')
    expect(chat.homeWorkflows.value[0]?.description).toBe('已发布说明')
    expect(chat.unpublishedHomeWorkflows.value.map((w) => w.id)).toEqual(['wf-never'])
    chat.draft.value = '仍用已发布版本'
    await chat.send()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '仍用已发布版本',
      firstMessage: { text: '仍用已发布版本', images: [] },
      publishedSnapshot: true,
    })
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-1' },
    })
  })

  it('drops a home-visible draft when the published snapshot is not clarify-first', async () => {
    mocks.listWorkflows.mockResolvedValue([
      {
        ...approveWf,
        status: 'draft' as const,
        version: 2,
        publishedVersion: 1,
        showOnHome: true,
        publishedSnapshot: {
          version: 1,
          name: '已发布实现',
          description: '',
          nodes: reactWf.nodes,
          edges: reactWf.edges,
        },
      },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value).toHaveLength(0)
    expect(chat.unpublishedHomeWorkflows.value.map((w) => w.id)).toEqual(['wf-ap'])
  })

  it('asks for launch fields from the published snapshot, not the draft head', async () => {
    const snapNodes = [
      {
        id: 'in',
        type: 'input' as const,
        label: '开始',
        position: { x: 0, y: 0 },
        config: {
          variables: [{ name: 'topic', ask: true, required: true, type: 'text', value: '' }],
        },
      },
      approveWf.nodes[1],
      approveWf.nodes[2],
    ]
    mocks.listWorkflows.mockResolvedValue([
      {
        ...approveWf,
        name: '草稿名',
        status: 'draft' as const,
        version: 2,
        publishedVersion: 1,
        nodes: [
          {
            id: 'in',
            type: 'input' as const,
            label: '开始',
            position: { x: 0, y: 0 },
            config: {
              variables: [{ name: 'repos', ask: true, required: true, type: 'repos', value: [] }],
            },
          },
          approveWf.nodes[1],
          approveWf.nodes[2],
        ],
        publishedSnapshot: {
          version: 1,
          name: '已发布名',
          description: '已发布说明',
          nodes: snapNodes,
          edges: approveWf.edges,
        },
      },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    chat.draft.value = '需要主题'
    await chat.send()
    expect(mocks.startRun).not.toHaveBeenCalled()
    expect(chat.launchOpen.value).toBe(true)
    expect(chat.launchTarget.value?.name).toBe('已发布名')
    expect(chat.runFields.value.map((f) => f.key)).toEqual(['topic'])
  })

  it('switches the card to the newly published snapshot', async () => {
    const snap = {
      version: 1,
      name: '已发布名',
      description: '已发布说明',
      nodes: approveWf.nodes,
      edges: approveWf.edges,
    }
    mocks.listWorkflows.mockResolvedValue([
      {
        ...approveWf,
        name: '草稿名',
        description: '草稿说明',
        status: 'draft' as const,
        version: 2,
        publishedVersion: 1,
        publishedSnapshot: snap,
      },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.homeWorkflows.value[0]?.name).toBe('已发布名')
    mocks.listWorkflows.mockResolvedValue([
      {
        ...approveWf,
        name: '新版本',
        description: '新说明',
        status: 'published' as const,
        version: 2,
        publishedVersion: 2,
        publishedSnapshot: { ...snap, version: 2, name: '新版本', description: '新说明' },
      },
    ])
    await chat.load()
    expect(chat.homeWorkflows.value[0]?.name).toBe('新版本')
    expect(chat.homeWorkflows.value[0]?.description).toBe('新说明')
  })
})
