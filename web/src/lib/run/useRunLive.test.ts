// @vitest-environment happy-dom
import { computed, defineComponent, h, ref, watch } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import { REVIEW_RECOVERY_TIMEOUT_MS } from '@/lib/chat/reviewSessionState'
import { clearDialogues, dialogueKey, getDialogue } from '@/lib/chat/dialogueStore'
import { dialogueFrame, endedTurn, runningTurn } from '@/test/dialogueKit'

const mocks = vi.hoisted(() => ({ getRun: vi.fn(), nodeEvents: vi.fn(), takeoverNode: vi.fn(), runConversationChain: vi.fn(), reactReply: vi.fn(), show: vi.fn(), error: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getRun: mocks.getRun, nodeEvents: mocks.nodeEvents, takeoverNode: mocks.takeoverNode, runConversationChain: mocks.runConversationChain, reactReply: mocks.reactReply, runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events` } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ show: mocks.show, error: mocks.error }) }))

import { LIVE_POLL_MS, useRunLive, type LiveChatSurface } from './useRunLive'

class FakeSocket {
  static last: FakeSocket | null = null
  static instances: FakeSocket[] = []
  static throwNext = false
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) {
    if (FakeSocket.throwNext) {
      FakeSocket.throwNext = false
      throw new Error('connection failed')
    }
    FakeSocket.last = this
    FakeSocket.instances.push(this)
  }
  close() { this.onclose?.() }
  send(frame: unknown) { this.onmessage?.({ data: JSON.stringify(frame) }) }
}

const cur = (id: string, kind: string, mode: string, state: string) => ({ id, name: id, iteration: 1, kind, mode, state })
const autoRun = { id: 'r1', status: 'running', currentNode: cur('impl', 'agent', 'auto', 'running') }
const stoppingRun = { id: 'r1', status: 'running', currentNode: cur('impl', 'takeover', 'manual', 'stopping') }
const takenRun = { id: 'r1', status: 'waiting_human', currentNode: cur('impl', 'takeover', 'manual', 'waiting_human') }

async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  await router.push('/runs/r1/live?node=impl')
  const surface: LiveChatSurface = { releaseConfirmPending: vi.fn() }
  let live!: ReturnType<typeof useRunLive>
  const Harness = defineComponent({
    setup() {
      // Like RunLiveView: ?node= is read reactively from the route.
      live = useRunLive(ref('r1'), computed(() => String(router.currentRoute.value.query.node || '')), ref(surface))
      return () => h('div')
    },
  })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })
  const wrapper = mount(Harness, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router, surface, live: () => live }
}

function deferred<T = any>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

async function setupRouteReuse() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/runs/:id/live', component: { template: '<div />' } }] })
  await router.push('/runs/a/live?node=impl')
  const makeSurface = (): LiveChatSurface => ({ releaseConfirmPending: vi.fn() })
  const chat = ref<LiveChatSurface | null>(makeSurface())
  const surfaces = [chat.value!]
  let live!: ReturnType<typeof useRunLive>
  const Harness = defineComponent({
    setup() {
      const id = computed(() => String(router.currentRoute.value.params.id || ''))
      live = useRunLive(id, computed(() => String(router.currentRoute.value.query.node || '')), chat)
      // RunLiveView reuses its controller but re-keys the child by run/node.
      watch(id, () => { chat.value = makeSurface(); surfaces.push(chat.value!) }, { flush: 'post' })
      return () => h('div')
    },
  })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })
  const wrapper = mount(Harness, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router, surfaces, live }
}

const routeRun = (id: string) => ({ ...autoRun, id, title: id, currentNode: { ...cur('impl', 'agent', 'auto', 'running'), task: `task ${id}` } })
const routeChain = (id: string, task: string) => ({ runId: id, status: 'running', currentNode: cur('impl', 'agent', 'auto', 'running'), segments: [{ nodeId: 'impl', iteration: 1, name: 'impl', kind: 'agent', mode: 'auto', status: 'running', task }] })

beforeEach(() => {
  vi.clearAllMocks()
  FakeSocket.instances = []
  FakeSocket.throwNext = false
  vi.stubGlobal('WebSocket', FakeSocket)
  mocks.getRun.mockResolvedValue(autoRun)
  mocks.nodeEvents.mockResolvedValue({ events: [{ kind: 'agent_message', text: '已有输出' }] })
  mocks.runConversationChain.mockRejectedValue(Object.assign(new Error('until_not_found'), { status: 400 }))
  mocks.reactReply.mockResolvedValue({})
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useRunLive (auto node → Stop → chat)', () => {
  it('shows a retryable error when its initial GET ignores abort and rejects the late response after retry', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const initial = deferred()
    let signal!: AbortSignal
    mocks.getRun.mockImplementationOnce((_id: string, options: { signal: AbortSignal }) => {
      signal = options.signal
      return initial.promise
    })
    const { live, wrapper } = await setup()
    try {
      expect(live().loading.value).toBe(true)
      await vi.advanceTimersByTimeAsync(REVIEW_RECOVERY_TIMEOUT_MS)
      await flushPromises()
      expect(signal.aborted).toBe(true)
      expect(live().loading.value).toBe(false)
      expect(live().loadError.value).toBe(true)
      expect(live().run.value).toBeNull()
      await live().reload()
      await flushPromises()
      expect(live().run.value?.id).toBe('r1')
      expect(live().loadError.value).toBe(false)
      initial.resolve({ id: 'r1', status: 'succeeded' })
      await flushPromises()
      expect(live().run.value?.status).toBe('running')
      expect(live().ended.value).toBe(false)
    } finally {
      wrapper.unmount()
      hidden.mockRestore()
    }
  })

  it('ignores the old run socket while the newly selected run HTTP load is pending', async () => {
    const bRun = deferred()
    mocks.getRun.mockImplementation(id => id === 'b' ? bRun.promise : Promise.resolve(routeRun(id)))
    const { router, wrapper } = await setupRouteReuse()
    try {
      const old = FakeSocket.last!
      await router.push('/runs/b/live?node=impl')
      await flushPromises()
      old.send({ type: 'acp', runId: 'a', nodeId: 'impl', events: [{ kind: 'message', text: 'late A output' }] })
    } finally { wrapper.unmount() }
  })

  it('ignores an earlier visit conversation chain after switching A to B and back to A', async () => {
    const firstA = deferred()
    let aChains = 0
    mocks.getRun.mockImplementation(id => Promise.resolve(routeRun(id)))
    mocks.runConversationChain.mockImplementation(id => id === 'a' && ++aChains === 1 ? firstA.promise : Promise.resolve(routeChain(id, `fresh ${id}`)))
    const { live, router, wrapper } = await setupRouteReuse()
    try {
      await router.push('/runs/b/live?node=impl')
      await flushPromises()
      await router.push('/runs/a/live?node=impl')
      await flushPromises()
      expect(live.task.value).toBe('fresh a')
      firstA.resolve(routeChain('a', 'obsolete a task'))
      await flushPromises()
      expect(live.task.value).toBe('fresh a')
    } finally { wrapper.unmount() }
  })

  it('keeps one poll after repeated route switches and removes all timers on unmount', async () => {
    vi.useFakeTimers()
    mocks.getRun.mockImplementation(id => Promise.resolve(routeRun(id)))
    const { router, wrapper } = await setupRouteReuse()
    try {
      await router.push('/runs/b/live?node=impl')
      await flushPromises()
      await router.push('/runs/a/live?node=impl')
      await flushPromises()
      const before = mocks.getRun.mock.calls.length
      await vi.advanceTimersByTimeAsync(LIVE_POLL_MS)
      await flushPromises()
      expect(mocks.getRun).toHaveBeenCalledTimes(before + 1)
    } finally { wrapper.unmount() }
    expect(vi.getTimerCount()).toBe(0)
  })

  it('ignores completed-event history from an earlier visit after A to B to A ends again', async () => {
    const oldTerminal = deferred()
    const currentEvents = [{ kind: 'message', text: 'current completed reply' }]
    let aReads = 0
    mocks.getRun.mockImplementation(id => Promise.resolve(routeRun(id)))
    mocks.nodeEvents.mockImplementation(id => {
      if (id !== 'a') return Promise.resolve({ events: [] })
      aReads++
      if (aReads === 2) return oldTerminal.promise
      return Promise.resolve({ events: aReads === 4 ? currentEvents : [] })
    })
    const { live, router, wrapper } = await setupRouteReuse()
    try {
      const completed = { ...routeRun('a'), status: 'completed', currentNode: undefined }
      live.onFrame({ type: 'snapshot', run: completed })
      expect(live.ended.value).toBe(true)
      await router.push('/runs/b/live?node=impl')
      await flushPromises()
      await router.push('/runs/a/live?node=impl')
      await flushPromises()
      live.onFrame({ type: 'snapshot', run: completed })
      await flushPromises()
      expect(live.turns.value.at(-1)?.text).toBe('current completed reply')
      oldTerminal.resolve({ events: [{ kind: 'message', text: 'obsolete completed reply' }] })
      await flushPromises()
      expect(live.turns.value.at(-1)?.text).toBe('current completed reply')
    } finally { wrapper.unmount() }
  })

  it('loads the initial conversation chain for the selected iteration above one', async () => {
    mocks.getRun.mockResolvedValue({ ...autoRun, currentNode: { ...cur('impl', 'agent', 'auto', 'running'), iteration: 2 } })
    mocks.nodeEvents.mockResolvedValue({ events: [] })
    mocks.runConversationChain.mockResolvedValue({
      runId: 'r1', status: 'running', currentNode: { ...cur('impl', 'agent', 'auto', 'running'), iteration: 2 },
      segments: [{ nodeId: 'impl', iteration: 2, name: 'second implementation visit', kind: 'agent', mode: 'auto', status: 'running', task: 'task for the second iteration' }],
    })
    const { live, wrapper } = await setup()
    try {
      expect(live().iteration.value).toBe(2)
      expect(live().task.value).toBe('task for the second iteration')
      expect(live().nodeLabel.value).toBe('second implementation visit')
    } finally { wrapper.unmount() }
  })

  it('confirm before the chat is open does not send and releases the spinner', async () => {
    const { live, surface, wrapper } = await setup()
    await live().confirm()
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(surface.releaseConfirmPending).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('takeover → stopping, then WS node_takeover waiting_human opens the chat', async () => {
    const { live, router, wrapper } = await setup()
    mocks.takeoverNode.mockResolvedValue({ runId: 'r1', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'stopping', status: 'taken_over' })
    await live().takeover()
    expect(mocks.takeoverNode).toHaveBeenCalledWith('r1', 'impl', 1)
    expect(live().stopping.value).toBe(true)
    await live().takeover()
    expect(mocks.takeoverNode).toHaveBeenCalledTimes(1)
    FakeSocket.last!.send({ type: 'node_takeover', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'waiting_human' })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/impl')
    wrapper.unmount()
  })

  it('already_manual (a concurrent takeover won) is success: no error, straight into the chat', async () => {
    const { live, router, wrapper } = await setup()
    mocks.takeoverNode.mockResolvedValue({ runId: 'r1', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'waiting_human', status: 'already_manual' })
    await live().takeover()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/impl')
    wrapper.unmount()
  })

  it('a lost race (node_finishing) is a notice plus refresh, not an error', async () => {
    const { live, wrapper } = await setup()
    mocks.takeoverNode.mockRejectedValue(Object.assign(new Error('node_finishing'), { status: 409 }))
    const before = mocks.getRun.mock.calls.length
    await live().takeover()
    await flushPromises()
    expect(mocks.show).toHaveBeenCalledWith('节点正在结束，已刷新')
    expect(mocks.error).not.toHaveBeenCalled()
    expect(mocks.getRun.mock.calls.length).toBeGreaterThan(before)
    expect(live().stopping.value).toBe(false)
    wrapper.unmount()
  })

  it('other failures toast an error and re-enable Stop', async () => {
    const { live, wrapper } = await setup()
    mocks.takeoverNode.mockRejectedValueOnce(Object.assign(new Error('server_halting'), { status: 409 }))
    await live().takeover()
    expect(mocks.error).toHaveBeenLastCalledWith('服务正在停机，请稍后重试')
    mocks.takeoverNode.mockRejectedValueOnce(new Error('network down'))
    await live().takeover()
    expect(mocks.error).toHaveBeenLastCalledWith('停止失败：network down')
    expect(live().stopping.value).toBe(false)
    wrapper.unmount()
  })

  it('opening a node that is still stopping shows the spinner; once waiting it redirects', async () => {
    mocks.getRun.mockResolvedValue(stoppingRun)
    const { live, router, wrapper } = await setup()
    expect(live().stopping.value).toBe(true)
    mocks.getRun.mockResolvedValue(takenRun)
    await live().reload()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/impl')
    wrapper.unmount()
  })

  // User rule: once the user is looking at a node, the page never jumps by itself.
  it.each([
    ['a gate', { id: 'r1', status: 'waiting_human', currentNode: { ...cur('gate', 'gate', 'manual', 'waiting_human'), name: '上线审批' } }, { kind: 'gate', label: '上线审批' }],
    ['the end of the run', { id: 'r1', status: 'succeeded' }, { kind: 'run', status: 'succeeded' }],
    ['a failed run', { id: 'r1', status: 'failed' }, { kind: 'run', status: 'failed' }],
  ])('stays on the page when the node on screen finishes (run moved to %s): no navigation, shown as done', async (_label, next, endState) => {
    const { live, router, wrapper } = await setup()
    const replace = vi.spyOn(router, 'replace')
    const push = vi.spyOn(router, 'push')
    mocks.getRun.mockResolvedValue(next)
    await live().reload()
    await flushPromises()
    expect(replace).not.toHaveBeenCalled()
    expect(push).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=impl')
    expect(live().ended.value).toBe(true)
    expect(live().nodeId.value).toBe('impl')
    // Suggestion 3: the specific state (gate waiting by name / how the run ended).
    expect(live().endState.value).toEqual(endState)
    // Later frames and polls no longer route the page either.
    const calls = mocks.getRun.mock.calls.length
    FakeSocket.last?.send({ type: 'snapshot', run: next })
    await live().reload()
    await flushPromises()
    expect(replace).not.toHaveBeenCalled()
    expect(mocks.getRun.mock.calls.length).toBe(calls)
    wrapper.unmount()
  })

  // GraspReview B-850-1: 实现 → 验证 (auto → auto). RunLiveView re-keys the surface per node.
  it('auto → auto in place: the next node mounts one re-keyed surface and the route follows it, no navigation', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    await router.push('/runs/r1/live?node=impl')
    const mk = (): LiveChatSurface => ({ releaseConfirmPending: vi.fn() })
    const first = mk()
    const surfaces: LiveChatSurface[] = []
    const chatRef = ref<LiveChatSurface | null>(first)
    let live!: ReturnType<typeof useRunLive>
    const Harness = defineComponent({
      setup() {
        live = useRunLive(ref('r1'), computed(() => String(router.currentRoute.value.query.node || '')), chatRef)
        // Like :key="surfaceKey": a new node mounts a new surface on the next render.
        watch(live.nodeId, (_n, old) => {
          if (!old) return // first node: the surface that is already mounted
          const s = mk()
          surfaces.push(s)
          chatRef.value = s
        })
        return () => h('div')
      },
    })
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })
    const wrapper = mount(Harness, { global: { plugins: [router, i18n] } })
    await flushPromises()
    const replace = vi.spyOn(router, 'replace')
    mocks.nodeEvents.mockResolvedValue({ events: [] })
    // Server order: node_enter, then 验证's first acp frames, then the page learns 验证 is current.
    FakeSocket.last!.send({ type: 'node_enter', nodeId: 'verify', iteration: 1, kind: 'agent', mode: 'auto' })
    const early = [{ t: 0, kind: 'message', text: '开始验证' }]
    FakeSocket.last!.send({ type: 'acp', nodeId: 'verify', events: early, busy: true })
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'running', currentNode: cur('verify', 'agent', 'auto', 'running') })
    await live.reload()
    await flushPromises()
    expect(live.nodeId.value).toBe('verify')
    expect(surfaces).toHaveLength(1)
    // Later frames stream into the same turn.
    const later = [...early, { t: 0, kind: 'tool_call', title: '运行测试', status: 'completed' }]
    FakeSocket.last!.send({ type: 'acp', nodeId: 'verify', events: later, busy: true })
    // ?node= follows the node on screen (replace, same page); later polls: no re-seed, no new turn.
    expect(replace).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=verify')
    await live.reload()
    await flushPromises()
    await live.reload()
    await flushPromises()
    expect(surfaces).toHaveLength(1)
    // Run ends: one turn_done on the new surface, nothing busy left, no navigation.
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'completed' })
    await live.reload()
    await flushPromises()
    expect(live.ended.value).toBe(true)
    expect(replace).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('feeds the chat from dialogue frames and from the run dialogue on reload', async () => {
    mocks.getRun.mockResolvedValue(autoRun)
    mocks.nodeEvents.mockResolvedValue({ events: [] })
    const { live, wrapper } = await setup()
    const text = () => getDialogue(dialogueKey('r1', 'impl'))?.rows.map((r) => r.text)
    FakeSocket.last!.send(dialogueFrame('r1', 'impl', {
      busy: true,
      rows: [{ role: 'agent', text: '正在实现', at: '1', turnId: 't1', streaming: true }],
      turns: [runningTurn('t1')],
    }))
    expect(text()).toEqual(['正在实现'])
    mocks.getRun.mockResolvedValue({
      ...autoRun,
      dialogueByNode: {
        impl: dialogueFrame('r1', 'impl', { rows: [{ role: 'agent', text: '实现完成', at: '1', turnId: 't1' }], turns: [endedTurn('t1', 'ok')] }),
      },
    })
    await live().reload()
    await flushPromises()
    expect(text()).toEqual(['实现完成'])
    wrapper.unmount()
    clearDialogues()
  })

  // #850: the conversation continues in place with the run's next node.
  it('next auto node: continues in place (earlier nodes folded above, next stream), no navigation', async () => {
    const { live, router, wrapper } = await setup()
    const replace = vi.spyOn(router, 'replace')
    mocks.runConversationChain.mockResolvedValue({
      runId: 'r1',
      status: 'running',
      currentNode: { id: 'test', name: '测试', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' },
      segments: [
        { nodeId: 'impl', iteration: 1, name: '实现', kind: 'agent', mode: 'auto', status: 'completed', startedAt: '2026-10-21T05:00:00+08:00', endedAt: '2026-10-21T05:00:20+08:00', eventCount: 4 },
        { nodeId: 'test', iteration: 1, name: '测试', kind: 'agent', mode: 'auto', status: 'running', startedAt: '2026-10-21T05:00:21+08:00', task: '跑单元测试' },
      ],
    })
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'running', currentNode: cur('test', 'agent', 'auto', 'running') })
    const push = vi.spyOn(router, 'push')
    const gets = mocks.getRun.mock.calls.length
    await live().reload()
    await flushPromises()
    // ?node= now names the node on screen (refresh lands there): replace, no push.
    expect(replace).toHaveBeenCalledExactlyOnceWith({ path: '/runs/r1/live', query: { node: 'test' } })
    expect(push).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=test')
    // The URL change is not a new page: no reload, no reset of the continued conversation.
    expect(mocks.getRun.mock.calls.length).toBe(gets + 1)
    expect(live().nodeId.value).toBe('test')
    expect(live().mode.value).toBe('observe')
    expect(live().ended.value).toBe(false)
    expect(live().priorSegments.value.map((s) => s.label)).toEqual(['实现'])
    expect(live().task.value).toBe('跑单元测试')
    // The next node's stream seeds the (re-keyed) surface.
    wrapper.unmount()
  })

  it('next node waiting for a person: its chat continues in place — reply and 确认并流转 here, still no navigation', async () => {
    const { live, router, wrapper } = await setup()
    const replace = vi.spyOn(router, 'replace')
    const turns = [{ role: 'agent', text: '验收前想确认一下', at: '2026-10-21T05:01:00+08:00' }]
    const chatRun = { id: 'r1', status: 'waiting_human', currentNode: { ...cur('accept', 'clarify', 'manual', 'waiting_human'), name: '验收测试', task: '编写验收用例' }, clarifyByNode: { accept: { nodeId: 'accept', iteration: 1, turns, done: false } } }
    mocks.getRun.mockResolvedValue(chatRun)
    await live().reload()
    await flushPromises()
    expect(replace).toHaveBeenCalledExactlyOnceWith({ path: '/runs/r1/live', query: { node: 'accept' } })
    expect(live().mode.value).toBe('chat')
    expect(live().chatKind.value).toBe('clarify')
    expect(live().nodeId.value).toBe('accept')
    expect(live().nodeLabel.value).toBe('验收测试')
    expect(live().task.value).toBe('编写验收用例')
    expect(live().turns.value).toEqual(turns)
    // Review frames of this node drive the chat; turn_done refreshes the transcript.
    FakeSocket.last!.send({ type: 'review', event: 'turn_begin', nodeId: 'accept' })
    await live().send('空数据只导出表头', [], [])
    expect(mocks.reactReply).toHaveBeenCalledWith('r1', 'accept', '空数据只导出表头', [], false, [])
    await live().confirm()
    expect(mocks.reactReply).toHaveBeenLastCalledWith('r1', 'accept', expect.any(String), [], true, [])
    expect(live().handoff.value?.phase).toBe('done')
    expect(live().composerLocked.value).toBe(true)
    // Run ends after the confirm: the page ends in place.
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'succeeded' })
    await live().reload()
    await flushPromises()
    expect(live().ended.value).toBe(true)
    expect(replace).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=accept')
    wrapper.unmount()
  })

  it('polls the authoritative queue and persisted reply when turn_done was lost on a healthy socket', async () => {
    vi.useFakeTimers()
    const { live, wrapper } = await setup()
    const chatRun = { id: 'r1', status: 'waiting_human', currentNode: cur('accept', 'review', 'manual', 'busy') }
    mocks.getRun.mockResolvedValue(chatRun)
    await live().reload()
    await flushPromises()
    const socket = FakeSocket.last!
    socket.send({ type: 'review', event: 'turn_begin', nodeId: 'accept', sessionGeneration: 3, revision: 1,
      item: { id: 'q1', text: '继续' } })
    socket.send({ type: 'review', event: 'queue_state', nodeId: 'accept', sessionGeneration: 3, revision: 2,
      busy: true, waiting: 1, items: [{ id: 'q2', text: '你应该只修改前端' }] })
    const finalTurns = [{ role: 'agent', text: '已经完成两条消息', at: '2026-10-29T01:00:00Z' }]
    mocks.getRun.mockResolvedValue({ ...chatRun, reviewSessionGeneration: 3,
      clarifyByNode: { accept: { nodeId: 'accept', turns: finalTurns, done: false } } })
    await vi.advanceTimersByTimeAsync(LIVE_POLL_MS)
    await flushPromises()
    expect(live().turns.value).toEqual(finalTurns)
    expect(FakeSocket.last).toBe(socket)
    socket.send({ type: 'review', event: 'queue_state', nodeId: 'accept', sessionGeneration: 3, revision: 3,
      busy: true, waiting: 1, items: [{ id: 'q2', text: 'retired queue' }] })
    wrapper.unmount()
  })

  it('streams ACP into a continued chat and reconnects while its previous turn is still busy', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const { live, wrapper } = await setup()
    const chatRun = { id: 'r1', status: 'waiting_human', currentNode: cur('accept', 'review', 'manual', 'busy') }
    mocks.getRun.mockResolvedValue(chatRun)
    await live().reload()
    await flushPromises()
    const first = FakeSocket.last!
    first.onopen?.()
    first.send({ type: 'review', event: 'turn_begin', nodeId: 'accept', item: { id: 'q1', text: '继续' } })
    const partial = [{ kind: 'message', text: '正在继续', t: 0 }]
    first.send({ type: 'acp', nodeId: 'accept', events: partial, busy: true })
    first.close()
    await vi.advanceTimersByTimeAsync(800)
    const replacement = FakeSocket.last!
    expect(replacement).not.toBe(first)
    replacement.onopen?.()
    const recovered = [{ kind: 'message', text: '正在继续，补齐断线期间内容', t: 0 }]
    mocks.nodeEvents.mockResolvedValue({ events: recovered, live: true })
    replacement.send({ type: 'snapshot', run: { ...chatRun, reactSessions: { accept: { busy: true, waiting: 0, items: [], activeItem: { id: 'q1', text: '继续' } } } } })
    await flushPromises()
    expect(live().ended.value).toBe(false)

    // turn_done was lost while offline. The authoritative reconnect snapshot
    // restores its persisted reply and usage without another page load.
    const usage = { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheWriteTokens: 0 }
    const finalTurns = [{ role: 'agent', text: '已经完成', at: '2026-10-28T01:00:00Z', usage }]
    replacement.send({ type: 'snapshot', run: { ...chatRun, currentNode: cur('accept', 'review', 'manual', 'waiting_human'), reactSessions: {}, clarifyByNode: { accept: { nodeId: 'accept', turns: finalTurns, done: false } } } })
    await flushPromises()
    expect(live().turns.value).toEqual(finalTurns)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(70_000)
    expect(FakeSocket.instances).toHaveLength(2)
  })

  it('keeps retrying constructor and handshake failures and repairs a socket that never closes', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const { wrapper } = await setup()
    FakeSocket.last!.onopen?.()
    FakeSocket.throwNext = true
    FakeSocket.last!.close()
    await vi.advanceTimersByTimeAsync(800)
    expect(FakeSocket.instances).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1600)
    expect(FakeSocket.instances).toHaveLength(2)
    FakeSocket.last!.close() // handshake failed before onopen
    await vi.advanceTimersByTimeAsync(3200)
    expect(FakeSocket.instances).toHaveLength(3)
    const stalled = FakeSocket.last!
    stalled.onopen?.()
    // Native close is never delivered on a half-open connection.
    await vi.advanceTimersByTimeAsync(60_800)
    const recovered = FakeSocket.last!
    expect(recovered).not.toBe(stalled)
    const events = [{ kind: 'message', t: 0, text: '断线期间的新输出' }]
    mocks.nodeEvents.mockResolvedValue({ events, live: true })
    recovered.onopen?.()
    recovered.send({ type: 'snapshot', run: autoRun })
    await flushPromises()
    wrapper.unmount()
  })

  it('uses application heartbeats to keep a healthy quiet socket open', async () => {
    vi.useFakeTimers()
    const { wrapper } = await setup()
    const socket = FakeSocket.last!
    socket.onopen?.()
    for (let i = 0; i < 4; i++) {
      await vi.advanceTimersByTimeAsync(25_000)
      socket.send({ type: 'heartbeat' })
    }
    expect(FakeSocket.instances).toHaveLength(1)
    wrapper.unmount()
  })

  it('ignores an older HTTP snapshot that crosses a live review turn', async () => {
    const { live, wrapper } = await setup()
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('accept', 'review', 'manual', 'waiting_human') })
    await live().reload()
    await flushPromises()
    let resolve!: (value: unknown) => void
    mocks.getRun.mockImplementationOnce(() => new Promise((done) => { resolve = done }))
    const pending = live().reload()
    FakeSocket.last!.send({ type: 'review', event: 'turn_begin', nodeId: 'accept' })
    resolve({ id: 'r1', status: 'succeeded' })
    await pending
    expect(live().ended.value).toBe(false)
    wrapper.unmount()
  })

  it('opens an entering auto node on the conversation and does not leave for run detail', async () => {
    mocks.getRun.mockResolvedValue({
      id: 'r1',
      status: 'waiting_human',
      title: '当前时间数据与上一格',
      currentNode: { ...cur('impl', 'agent', 'auto', 'entering'), name: '调研' },
    })
    const { live, router, wrapper } = await setup()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=impl')
    expect(live().ended.value).toBe(false)
    expect(live().nodeId.value).toBe('impl')
    expect(live().followTail.value).toEqual(expect.objectContaining({ kind: 'waiting', label: '调研' }))
    mocks.getRun.mockResolvedValue({
      id: 'r1',
      status: 'running',
      currentNode: { ...cur('impl', 'agent', 'auto', 'running'), name: '调研', task: '对齐当前时间' },
    })
    await live().reload()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=impl')
    expect(live().ended.value).toBe(false)
    expect(live().mode.value).toBe('observe')
    expect(live().nodeId.value).toBe('impl')
    expect(live().followTail.value).toBeNull()
    wrapper.unmount()
  })

  it('still leaves the conversation when the run is already finished, failed, or cancelled', async () => {
    for (const status of ['completed', 'failed', 'cancelled'] as const) {
      mocks.getRun.mockResolvedValue({ id: 'r1', status })
      const { router, wrapper } = await setup()
      expect(router.currentRoute.value.fullPath).toBe('/runs/r1')
      wrapper.unmount()
    }
  })

  it('waits for an entering successor while the run still reports waiting_human', async () => {
    const { live, wrapper } = await setup()
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('verify', 'agent', 'auto', 'entering') })
    await live().reload()
    expect(live().ended.value).toBe(false)
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'running', currentNode: cur('verify', 'agent', 'auto', 'running') })
    await live().reload()
    await flushPromises()
    expect(live().nodeId.value).toBe('verify')
    expect(live().ended.value).toBe(false)
    wrapper.unmount()
  })

  it('restores completed per-turn usage from persisted events instead of assigning execution totals to a reply', async () => {
    const { live, wrapper } = await setup()
    const usage = { inputTokens: 1000, outputTokens: 500, cacheReadTokens: 0, cacheWriteTokens: 0 }
    const events = [
      { kind: 'prompt', t: 0, text: '实现功能', at: '2026-10-28T01:00:00Z' },
      { kind: 'message', t: 1, text: '完成第一步' },
      { kind: 'segment', t: 2 },
      { kind: 'message', t: 3, text: '实现已完成' },
      { kind: 'turn_end', t: 4, at: '2026-10-28T01:00:04Z', usage },
    ]
    mocks.nodeEvents.mockResolvedValue({ events, live: false })
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'succeeded', nodeExecutions: { impl: [{ nodeId: 'impl', iteration: 1, status: 'succeeded', events, usage: { ...usage, inputTokens: 9000 } }] } })
    await live().reload()
    await flushPromises()
    expect(live().turns.value).toEqual([
      expect.objectContaining({ role: 'agent', text: '完成第一步' }),
      expect.objectContaining({ role: 'agent', text: '实现已完成', usage }),
    ])
    expect(live().turns.value[0].usage).toBeUndefined()
    wrapper.unmount()
  })

  it('pending 确认并流转 does not lock the composer; success still locks while following', async () => {
    const { live, wrapper } = await setup()
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('accept', 'review', 'manual', 'waiting_human'), clarifyByNode: { accept: { nodeId: 'accept', iteration: 1, turns: [], done: false } } })
    await live().reload()
    await flushPromises()
    let release: (value?: unknown) => void = () => {}
    mocks.reactReply.mockImplementationOnce(() => new Promise((resolve) => { release = resolve }))
    const finishing = live().confirm()
    await flushPromises()
    expect(live().handoff.value?.phase).toBe('pending')
    expect(live().composerLocked.value).toBe(false)
    release({})
    await finishing
    expect(live().handoff.value?.phase).toBe('done')
    expect(live().composerLocked.value).toBe(true)
    wrapper.unmount()
  })

  it('a rejected 确认并流转 in the continued chat shows the error and unlocks the composer', async () => {
    const { live, surface, wrapper } = await setup()
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('accept', 'review', 'manual', 'waiting_human'), clarifyByNode: { accept: { nodeId: 'accept', iteration: 1, turns: [], done: false } } })
    await live().reload()
    await flushPromises()
    mocks.reactReply.mockRejectedValueOnce(new Error('产物契约未满足'))
    await live().confirm()
    expect(live().confirmError.value).toBe('产物契约未满足')
    expect(live().handoff.value).toBeNull()
    expect(live().composerLocked.value).toBe(false)
    expect(surface.releaseConfirmPending).toHaveBeenCalledTimes(1)
    mocks.reactReply.mockRejectedValueOnce(new Error('产物契约未满足'))
    await live().confirm()
    expect(live().confirmError.value).toBe('产物契约未满足')
    expect(live().handoff.value).toBeNull()
    expect(surface.releaseConfirmPending).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('confirm passes automatic, not automatic, or unknown, and node_enter fills unknown', async () => {
    const nodes = [
      { id: 'accept', type: 'agent', label: '验收', caps: { interaction: 'clarify', tools: ['ask_question'] } },
      { id: 'impl', type: 'agent', label: '实现', caps: { interaction: 'auto' } },
      { id: 'gate', type: 'human_gate', label: '审批' },
      { id: 'out', type: 'output', label: '输出' },
    ]
    const chat = {
      id: 'r1',
      status: 'waiting_human',
      currentNode: cur('accept', 'clarify', 'manual', 'waiting_human'),
      clarifyByNode: { accept: { nodeId: 'accept', iteration: 1, turns: [], done: false } },
      nodes,
    }
    async function confirmWith(edges: unknown[]) {
      mocks.getRun.mockResolvedValue(autoRun)
      const { live, wrapper } = await setup()
      mocks.getRun.mockResolvedValue({ ...chat, edges })
      await live().reload()
      await flushPromises()
      await live().confirm()
      await flushPromises()
      return { live, wrapper }
    }
    const auto = await confirmWith([{ id: 'e1', source: 'accept', target: 'impl' }])
    expect(auto.live().handoff.value).toMatchObject({ phase: 'done', targetAuto: true })
    auto.wrapper.unmount()

    const human = await confirmWith([{ id: 'e1', source: 'accept', target: 'gate' }])
    expect(human.live().handoff.value).toMatchObject({ phase: 'done', targetAuto: false })
    human.wrapper.unmount()

    const unknown = await confirmWith([
      { id: 'e1', source: 'accept', target: 'impl', when: 'vars.ready' },
      { id: 'e2', source: 'accept', target: 'gate' },
    ])
    expect(unknown.live().handoff.value?.phase).toBe('done')
    expect(unknown.live().handoff.value?.targetAuto).toBeUndefined()
    FakeSocket.last!.send({ type: 'node_enter', nodeId: 'out', name: '输出', mode: 'auto' })
    expect(unknown.live().handoff.value?.targetAuto).toBeUndefined()
    FakeSocket.last!.send({ type: 'node_enter', nodeId: 'impl', name: '实现', mode: 'auto' })
    expect(unknown.live().handoff.value?.targetAuto).toBe(true)
    unknown.wrapper.unmount()
  })

  it('a refresh after an in-place continue opens the node on screen, without re-routing', async () => {
    const { live, router, wrapper } = await setup()
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'running', currentNode: cur('test', 'agent', 'auto', 'running') })
    await live().reload()
    await flushPromises()
    const url = router.currentRoute.value.fullPath
    expect(url).toBe('/runs/r1/live?node=test')
    wrapper.unmount()
    // Reload the page at that URL.
    const replace = vi.spyOn(router, 'replace')
    const again = await (async () => {
      const r2 = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
      await r2.push(url)
      const surface: LiveChatSurface = { releaseConfirmPending: vi.fn() }
      let l!: ReturnType<typeof useRunLive>
      const H = defineComponent({ setup() { l = useRunLive(ref('r1'), computed(() => String(r2.currentRoute.value.query.node || '')), ref(surface)); return () => h('div') } })
      const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })
      const w = mount(H, { global: { plugins: [r2, i18n] } })
      await flushPromises()
      return { l, r2, w, surface }
    })()
    expect(again.l.nodeId.value).toBe('test')
    expect(again.l.mode.value).toBe('observe')
    expect(again.r2.currentRoute.value.fullPath).toBe('/runs/r1/live?node=test')
    expect(replace).not.toHaveBeenCalled()
    again.w.unmount()
  })

  it('still routes while opening: a stale ?node= follows to the node the run is on now', async () => {
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'running', currentNode: cur('test', 'agent', 'auto', 'running') })
    const { router, wrapper } = await setup()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=test')
    wrapper.unmount()
  })

  it('opens with the node task from currentNode.task', async () => {
    mocks.getRun.mockResolvedValue({ ...autoRun, currentNode: { ...autoRun.currentNode, task: '  实现登录页改版  ' } })
    mocks.nodeEvents.mockResolvedValue({ events: [], task: '另一个' })
    const { live, wrapper } = await setup()
    expect(live().task.value).toBe('实现登录页改版')
    wrapper.unmount()
  })

  it('falls back to the node events task, and shows none when neither is sent', async () => {
    mocks.nodeEvents.mockResolvedValue({ events: [], task: '部署 ••• 到 预发' })
    const a = await setup()
    expect(a.live().task.value).toBe('部署 ••• 到 预发')
    a.wrapper.unmount()
    mocks.nodeEvents.mockResolvedValue({ events: [] })
    const b = await setup()
    expect(b.live().task.value).toBe('')
    b.wrapper.unmount()
  })

  it('already_manual while the auto call still unwinds waits like taken_over, without an error', async () => {
    const { live, router, wrapper } = await setup()
    mocks.takeoverNode.mockResolvedValue({ runId: 'r1', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'stopping', status: 'already_manual' })
    await live().takeover()
    expect(live().stopping.value).toBe(true)
    expect(mocks.error).not.toHaveBeenCalled()
    FakeSocket.last!.send({ type: 'node_takeover', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'waiting_human' })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/impl')
    wrapper.unmount()
  })

  it('409 run_not_running shows the re-read run status with the error toast, refreshes and ends in place', async () => {
    const { live, router, wrapper } = await setup()
    mocks.takeoverNode.mockRejectedValue(Object.assign(new Error('run_not_running'), { status: 409, current: { runStatus: 'cancelled' } }))
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'cancelled' })
    const before = mocks.getRun.mock.calls.length
    await live().takeover()
    await flushPromises()
    expect(mocks.error).toHaveBeenCalledWith('运行已不在执行中（已取消），已刷新')
    expect(mocks.show).not.toHaveBeenCalled()
    expect(mocks.getRun.mock.calls.length).toBeGreaterThan(before)
    expect(live().stopping.value).toBe(false)
    expect(live().ended.value).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=impl')
    wrapper.unmount()
  })

  it('routes on the WS snapshot frame currentNode without another GET', async () => {
    const { router, wrapper } = await setup()
    const before = mocks.getRun.mock.calls.length
    FakeSocket.last!.send({ type: 'snapshot', run: { ...takenRun, lastActivityAt: '2026-10-21T00:00:00Z' } })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/impl')
    expect(mocks.getRun.mock.calls.length).toBe(before)
    wrapper.unmount()
  })

  it('ignores nodeRuns: a stale running nodeRun with no currentNode is not observed as auto', async () => {
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('gate', 'gate', 'manual', 'waiting_human'), nodeRuns: { impl: { status: 'running', iteration: 1 } } })
    const { router, wrapper } = await setup()
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/nodes/gate')
    wrapper.unmount()
  })
})
