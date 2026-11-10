// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import workspaceSnapshots from '@/locales/zh-CN/workspaceSnapshots.json'
import { REVIEW_RECOVERY_TIMEOUT_MS } from '@/lib/chat/reviewSessionState'

const mocks = vi.hoisted(() => ({ chat: vi.fn(), getRun: vi.fn(), chain: vi.fn(), nodeEvents: vi.fn(), continueWorkflow: vi.fn() }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, workflowChat: mocks.chat, getRun: mocks.getRun, runConversationChain: mocks.chain, nodeEvents: mocks.nodeEvents, continueWorkflow: mocks.continueWorkflow } }
})
import RunView from './RunView.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'

const wrappers: ReturnType<typeof mount>[] = []
const sockets = vi.fn()
const savedChain = (id = 'old', status = 'completed') => ({
  runId: id, status, segments: [
    { nodeId: 'clarify', iteration: 1, kind: 'clarify', status: 'completed', name: '需求澄清', startedAt: '2026-11-01T00:00:00Z', endedAt: '2026-11-01T00:00:03Z', turns: [
      { role: 'human', text: `${id} 原始要求`, at: '2026-11-01T00:00:00Z' },
      { role: 'agent', text: `${id} 已确认需求`, at: '2026-11-01T00:00:03Z' },
    ] },
    { nodeId: 'impl', iteration: 1, kind: 'agent', status, name: '实现', startedAt: '2026-11-01T00:00:04Z', endedAt: '2026-11-01T00:00:09Z', eventCount: 2 },
    { nodeId: 'clarify', iteration: 2, kind: 'review', status, name: '复核', startedAt: '2026-11-01T00:00:10Z', endedAt: '2026-11-01T00:00:13Z', turns: [
      { role: 'agent', text: `${id} 第二轮复核结果`, at: '2026-11-01T00:00:13Z' },
    ] },
  ],
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
async function render(path = '/runs/old') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/runs/:id', component: RunView }] })
  await router.push(path)
  const wrapper = mount(RunView, { global: {
    plugins: [router, createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...workspaceSnapshots } } })],
    // Keep RunChatView, RunLiveView, ReviewComposer and the history renderer
    // real: stubbing RunLiveView concealed the self-route loading regression.
    stubs: {
      ReviewShell: { template: '<div><slot name="stage" /><slot name="sidebar" /></div>' },
      ReactArtifactStage: true, SoloSessionPane: true, SoloCreateModal: true, Icon: true,
      RunDetailView: { template: '<div data-testid="canvas" />' },
    },
    directives: { 'hover-ink': {} },
  } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('WebSocket', class { constructor() { sockets() } })
  mocks.chat.mockImplementation(async (id: string) => ({ soloId: '', title: `${id} 历史任务`, status: 'completed' }))
  mocks.getRun.mockImplementation(async (id: string) => ({ id, status: 'completed', nodes: [] }))
  mocks.chain.mockImplementation(async (id: string) => savedChain(id))
  mocks.nodeEvents.mockResolvedValue({ events: [{ kind: 'message', text: '保留的自动执行正文' }], live: false })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('terminal Run chat history with the real live host', () => {
  it.each([
    ['completed', '运行已完成'], ['failed', '运行失败'], ['cancelled', '运行已取消'],
  ])('renders every saved visit in place for %s without starting a session', async (status, note) => {
    mocks.getRun.mockResolvedValue({ id: 'old', status, nodes: [] })
    mocks.chain.mockResolvedValue(savedChain('old', status))
    const { wrapper, router } = await render()
    expect(wrapper.text()).not.toContain('加载运行…')
    expect(wrapper.get('[data-testid="clarify-done-note"]').text()).toBe(note)
    expect(wrapper.text()).toContain('old 原始要求')
    expect(wrapper.text()).toContain('old 已确认需求')
    expect(wrapper.text()).toContain('old 第二轮复核结果')
    expect(mocks.nodeEvents).not.toHaveBeenCalled()
    const auto = wrapper.get('[data-testid="clarify-prior-segment"][data-kind="auto"]')
    ;(auto.element as HTMLDetailsElement).open = true
    await auto.trigger('toggle'); await flushPromises()
    expect(wrapper.text()).toContain('保留的自动执行正文')
    expect(mocks.nodeEvents).toHaveBeenCalledWith('old', 'impl', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(router.currentRoute.value.fullPath).toBe('/runs/old')
    expect(wrapper.find('[data-testid="canvas"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="chat-composer"]').exists()).toBe(false)
    expect(wrapper.getComponent(ReactArtifactStage).props('editable')).toBe(false)
    expect(wrapper.getComponent(ReviewComposer).props('active')).toBe(false)
    expect(mocks.continueWorkflow).not.toHaveBeenCalled()
    expect(sockets).not.toHaveBeenCalled()
  })

  it.each(['paused', 'queued'])('keeps a %s run without a current node on its conversation instead of loading forever', async status => {
    vi.stubGlobal('WebSocket', class { constructor() { sockets() } close() {} })
    mocks.chat.mockResolvedValue({ soloId: '', title: 'old 历史任务', status })
    mocks.getRun.mockResolvedValue({ id: 'old', status, nodes: [] })
    mocks.chain.mockResolvedValue(savedChain('old', 'completed'))
    const { wrapper, router } = await render()
    expect(wrapper.text()).not.toContain('加载运行…')
    expect(wrapper.text()).toContain('old 已确认需求')
    expect(wrapper.find('[data-testid="clarify-done-note"]').exists()).toBe(false)
    expect(router.currentRoute.value.fullPath).toBe('/runs/old')
    expect(wrapper.find('[data-testid="canvas"]').exists()).toBe(false)
  })

  it('explains a failed workspace save on the conversation instead of a bare pause', async () => {
    vi.stubGlobal('WebSocket', class { constructor() { sockets() } close() {} })
    mocks.chat.mockResolvedValue({ soloId: '', title: 'old 历史任务', status: 'paused' })
    mocks.getRun.mockResolvedValue({ id: 'old', status: 'paused', nodes: [{ id: 'code', type: 'agent', label: '编码' }], workspaceSave: { checkpointId: 'cp', nodeId: 'code', state: 'failed', reason: 'snapshot_failed', detail: 'git_unsupported', action: 'retry' } })
    mocks.chain.mockResolvedValue(savedChain('old', 'completed'))
    const { wrapper } = await render()
    const banner = wrapper.get('[data-testid="workspace-save-banner"]')
    expect(banner.attributes('data-state')).toBe('failed')
    expect(banner.text()).toContain('「编码」已完成')
    expect(wrapper.find('[data-testid="workspace-save-retry"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('old 已确认需求')
  })

  it('shows the saved terminal status for a genuinely empty chain', async () => {
    mocks.chain.mockResolvedValue({ runId: 'old', status: 'completed', segments: [] })
    const { wrapper, router } = await render()
    expect(wrapper.get('[data-testid="clarify-done-note"]').text()).toBe('运行已完成')
    expect(wrapper.text()).not.toContain('加载运行…')
    expect(wrapper.find('[data-testid="clarify-prior-segment"]').exists()).toBe(false)
    expect(mocks.nodeEvents).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/runs/old')
  })

  it.each(['run', 'chain'])('keeps a failed %s read retryable instead of treating history as empty', async source => {
    const failed = source === 'run' ? mocks.getRun : mocks.chain
    failed.mockRejectedValueOnce(Object.assign(new Error('unavailable'), { status: 503 }))
    const { wrapper } = await render()
    expect(wrapper.find('[data-testid="run-live-retry"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-done-note"]').exists()).toBe(false)
    await wrapper.get('[data-testid="run-live-retry"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="run-live-retry"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('old 第二轮复核结果')
    expect(wrapper.get('[data-testid="clarify-done-note"]').text()).toBe('运行已完成')
  })

  it('releases a timed-out history read, retries, and ignores its late response', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const first = deferred<ReturnType<typeof savedChain>>()
    let oldSignal!: AbortSignal
    mocks.chain.mockImplementationOnce((_id: string, init: { signal: AbortSignal }) => { oldSignal = init.signal; return first.promise })
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('加载运行…')
    await vi.advanceTimersByTimeAsync(REVIEW_RECOVERY_TIMEOUT_MS); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.find('[data-testid="run-live-retry"]').exists()).toBe(true)
    await wrapper.get('[data-testid="run-live-retry"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('old 已确认需求')
    first.resolve(savedChain('late-private-history')); await flushPromises()
    expect(wrapper.text()).not.toContain('late-private-history')
    expect(wrapper.get('[data-testid="clarify-done-note"]').text()).toBe('运行已完成')
  })

  it('ignores an old chain after selecting another historical Run', async () => {
    const first = deferred<ReturnType<typeof savedChain>>()
    mocks.chain.mockImplementationOnce(() => first.promise)
    const { wrapper, router } = await render()
    await router.push('/runs/new'); await flushPromises()
    expect(wrapper.text()).toContain('new 已确认需求')
    first.resolve(savedChain('old')); await flushPromises()
    expect(wrapper.text()).not.toContain('old 已确认需求')
    expect(router.currentRoute.value.fullPath).toBe('/runs/new')
  })

  it('rejects a chain that identifies a different Run instead of publishing its history', async () => {
    mocks.chain.mockResolvedValueOnce(savedChain('foreign'))
    const { wrapper } = await render()
    expect(wrapper.find('[data-testid="run-live-retry"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('foreign 原始要求')
    expect(wrapper.find('[data-testid="clarify-done-note"]').exists()).toBe(false)
  })

  it('uses the existing snapshot fallback only when the chain endpoint is absent', async () => {
    mocks.chain.mockRejectedValueOnce(Object.assign(new Error('not_found'), { status: 404 }))
    mocks.getRun.mockResolvedValue({ id: 'old', status: 'completed', nodes: [],
      nodeRuns: { clarify: { nodeId: 'clarify', iteration: 1, status: 'completed', startedAt: '2026-11-01T00:00:00Z' } },
      clarifyByNode: { clarify: { turns: [{ role: 'agent', text: '旧快照保存的答复', at: '2026-11-01T00:00:03Z' }] } },
    })
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('旧快照保存的答复')
    expect(wrapper.get('[data-testid="clarify-done-note"]').text()).toBe('运行已完成')
  })
})
