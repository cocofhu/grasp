// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createApp, defineComponent, nextTick } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Workflow } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  toastWarn: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  listWorkflows: vi.fn(),
  listProjects: vi.fn(),
  patchWorkflowHomeVisibility: vi.fn(),
  startRun: vi.fn(),
  getRun: vi.fn(),
  reactReply: vi.fn(),
  readStoredProjectId: vi.fn(() => 'proj-1'),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push }),
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
      patchWorkflowHomeVisibility: mocks.patchWorkflowHomeVisibility,
      startRun: mocks.startRun,
      getRun: mocks.getRun,
      reactReply: mocks.reactReply,
    },
  }
})

import {
  useHomeApproveChat,
  HOME_PIPELINE_MEMORY_KEY,
  HOME_PRIORITY_MEMORY_KEY,
  HOME_COMPOSER_DRAFT_DEBOUNCE_MS,
  parseRunPriority,
  resolveHomeProjectName,
} from './useHomeApproveChat'
import {
  HOME_COMPOSER_DRAFT_KEY,
  __resetHomeComposerDraftMigrationForTests,
  saveHomeComposerDraft,
  loadHomeComposerDraft,
} from './homeComposerDraft'
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
    { id: 'ap', type: 'approve', label: '澄清', position: { x: 0, y: 0 }, config: {} },
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
    { id: 'r', type: 'react', label: '实现', position: { x: 0, y: 0 }, config: {} },
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
    mocks.push.mockReset()
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
    __resetHomeComposerDraftMigrationForTests()
    localStorage.removeItem(HOME_PIPELINE_MEMORY_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
  })

  afterEach(() => {
    vi.useRealTimers()
    __resetDraftIdbForTests()
    __resetHomeComposerDraftMigrationForTests()
    localStorage.removeItem(HOME_PIPELINE_MEMORY_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
  })

  it('filters to published approve-first pipelines', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap'])
    expect(chat.selected.value?.id).toBe('wf-ap')
  })

  // plan g2.2 — load listProjects in parallel and attach projectName
  it('resolves projectName from listProjects in parallel with listWorkflows', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(mocks.listProjects).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(chat.pipelines.value[0]?.projectName).toBe('综合项目组')
  })

  it('falls back to projectId when the project list has no matching name', async () => {
    mocks.listProjects.mockResolvedValue([])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value[0]?.projectName).toBe('proj-1')
  })

  it('leaves projectName empty when the workflow has no projectId', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, projectId: undefined }, reactWf])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value[0]?.projectName).toBe('')
  })

  it('still loads workflows when listProjects fails', async () => {
    mocks.listProjects.mockRejectedValue(new Error('projects down'))
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.loadError.value).toBeNull()
    expect(chat.pipelines.value[0]?.id).toBe('wf-ap')
    expect(chat.pipelines.value[0]?.projectName).toBe('proj-1')
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
    await chat.load()
    expect(mocks.listWorkflows).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const arg = mocks.listWorkflows.mock.calls[0]?.[0] || {}
    expect(arg.projectId).toBeUndefined()
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])
    expect(chat.projectId.value).toBe('proj-1')
  })

  // plan g2.3 — remember last selection; fall back when memory is stale
  it('defaults to remembered pipeline and falls back when memory is gone', async () => {
    localStorage.setItem(HOME_PIPELINE_MEMORY_KEY, 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')

    mocks.listWorkflows.mockResolvedValue([approveWf])
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-ap')
  })

  it('selectPipeline updates selection memory and project context', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    chat.selectPipeline('wf-lite')
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(chat.projectId.value).toBe('proj-2')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-lite')
  })

  it('hides a pipeline optimistically and falls back to the next visible pipeline', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()

    await chat.hidePipelineFromHome(approveWf)

    expect(mocks.patchWorkflowHomeVisibility).toHaveBeenCalledWith('wf-ap', false)
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-lite'])
    expect(chat.selectedId.value).toBe('wf-lite')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-lite')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已更新首页可见')
  })

  it('rolls back the card and selection when hiding fails', async () => {
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    mocks.patchWorkflowHomeVisibility.mockRejectedValue(new Error('patch failed'))
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()

    await chat.hidePipelineFromHome(approveWf)

    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-lite'])
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-ap')
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
    await chat.load()
    chat.draft.value = '紧急需求'
    chat.selectPriority('high')
    await chat.send()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'high', [], {
      title: '紧急需求',
      firstMessage: { text: '紧急需求', images: [] },
    })
  })

  it('starts a run carrying the first message, then opens inbox', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    chat.draft.value = '  把登录做清楚  '
    await chat.send()
    await nextTick()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '把登录做清楚',
      firstMessage: { text: '把登录做清楚', images: [] },
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
    await chat.load()
    chat.selectPipeline('wf-lite')
    chat.draft.value = '跨项目提交'
    await chat.send()
    await nextTick()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/gates',
      query: { run: 'run-1', node: 'ap', projectId: 'proj-2' },
    })
  })

  it('goGates omits projectId when the workflow has none (plan g1.1 / g2.3)', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, projectId: undefined }, reactWf])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    chat.draft.value = '无项目流水线'
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
    await result.load()
    result.draft.value = '卸载后仍要跳转'
    const sendPromise = result.send()
    app.unmount()
    await sendPromise
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '卸载后仍要跳转',
      firstMessage: { text: '卸载后仍要跳转', images: [] },
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

  // plan g2.4 — draft pipeline beats lastPipeline memory
  it('prefers draft pipeline over HOME_PIPELINE_MEMORY_KEY', async () => {
    localStorage.setItem(HOME_PIPELINE_MEMORY_KEY, 'wf-lite')
    await saveHomeComposerDraft('草稿管道优先', [], 'wf-ap')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(chat.draft.value).toBe('草稿管道优先')
  })

  // plan g2.4 — unavailable draft pipeline still restores text/attachments
  it('restores text when draft pipeline is unavailable', async () => {
    await saveHomeComposerDraft('管道已下线', [{ data: btoa('x'), mimeType: 'image/png' }], 'wf-gone')
    localStorage.setItem(HOME_PIPELINE_MEMORY_KEY, 'wf-lite')
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
  it('debounces auto-save of text, attachments, and pipeline', async () => {
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
    chat.selectPipeline('wf-lite')
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

  // plan g2.4 — empty pipeline list must not drop draft preference before load
  it('keeps draft pipeline preference while pipeline list is still empty', async () => {
    await saveHomeComposerDraft('等列表', [], 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([approveWf, approveWfB])
    const chat = withSetup(() => useHomeApproveChat())
    await flushPromises()
    // Before load: preference from hydrate must survive empty pipelines watch.
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

  // plan g3.1 / g3.3 — showOnHome=false is hidden even when published approve-first
  it('hides published approve-first pipelines when showOnHome is false or missing', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, id: 'wf-off', showOnHome: false },
      { ...approveWf, id: 'wf-missing', showOnHome: undefined },
      approveWf,
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap'])
  })

  // plan g3.3 — draft / non-approve-first stay hidden even when showOnHome is true
  it('still hides draft and non-approve-first pipelines when showOnHome is true', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, id: 'wf-draft', status: 'draft' as const, showOnHome: true },
      { ...reactWf, showOnHome: true },
      approveWf,
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap'])
  })

  // plan g3.1 / g3.3 — remembered hidden id falls back to first visible
  it('falls back when remembered pipeline is no longer showOnHome', async () => {
    localStorage.setItem(HOME_PIPELINE_MEMORY_KEY, 'wf-lite')
    mocks.listWorkflows.mockResolvedValue([
      approveWf,
      { ...approveWfB, showOnHome: false },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.selectedId.value).toBe('wf-ap')
    expect(chat.selected.value?.id).toBe('wf-ap')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-ap')
  })

  // plan g3.3 — all hidden → empty pipelines (empty state / disabled select)
  it('yields an empty pipeline list when every candidate is hidden', async () => {
    mocks.listWorkflows.mockResolvedValue([
      { ...approveWf, showOnHome: false },
      { ...approveWfB, showOnHome: false },
    ])
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value).toHaveLength(0)
    expect(chat.selected.value).toBeNull()
  })

  // plan g2.1 / g2.2 — from-baseline success reloads and selects the new card
  it('reloadAfterCreate loads the new pipeline and selects it', async () => {
    const created = { ...approveWf, id: 'wf-new', name: '首页新建', showOnHome: true }
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap'])
    mocks.listWorkflows.mockResolvedValue([approveWf, created])
    await chat.reloadAfterCreate('wf-new')
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap', 'wf-new'])
    expect(chat.selectedId.value).toBe('wf-new')
    expect(localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)).toBe('wf-new')
  })

  // plan g2.2 — reload failure must not wipe existing home cards
  it('reloadAfterCreate keeps the previous list when reload fails', async () => {
    const chat = withSetup(() => useHomeApproveChat())
    await chat.load()
    mocks.listWorkflows.mockRejectedValue(new Error('reload failed'))
    await chat.reloadAfterCreate('wf-new')
    expect(chat.pipelines.value.map((w) => w.id)).toEqual(['wf-ap'])
    expect(chat.selectedId.value).toBe('wf-ap')
  })
})
