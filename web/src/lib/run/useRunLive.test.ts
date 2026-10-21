// @vitest-environment happy-dom
import { computed, defineComponent, h, ref, watch } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'

const mocks = vi.hoisted(() => ({ getRun: vi.fn(), nodeEvents: vi.fn(), takeoverNode: vi.fn(), runConversationChain: vi.fn(), reactReply: vi.fn(), show: vi.fn(), error: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getRun: mocks.getRun, nodeEvents: mocks.nodeEvents, takeoverNode: mocks.takeoverNode, runConversationChain: mocks.runConversationChain, reactReply: mocks.reactReply, runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events` } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ show: mocks.show, error: mocks.error }) }))

import { useRunLive, type LiveChatSurface } from './useRunLive'

class FakeSocket {
  static last: FakeSocket | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) { FakeSocket.last = this }
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
  const surface: LiveChatSurface = { applyReviewFrame: vi.fn(), applyAcpEvents: vi.fn(), releaseConfirmPending: vi.fn() }
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

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('WebSocket', FakeSocket)
  mocks.getRun.mockResolvedValue(autoRun)
  mocks.nodeEvents.mockResolvedValue({ events: [{ kind: 'agent_message', text: '已有输出' }] })
  mocks.runConversationChain.mockRejectedValue(Object.assign(new Error('until_not_found'), { status: 400 }))
  mocks.reactReply.mockResolvedValue({})
})
afterEach(() => vi.unstubAllGlobals())

describe('useRunLive (auto node → Stop → chat)', () => {
  it('confirm before the chat is open does not send and releases the spinner', async () => {
    const { live, surface, wrapper } = await setup()
    await live().confirm()
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(surface.releaseConfirmPending).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('seeds a busy turn, replays the snapshot, then streams acp frames read-only', async () => {
    const { surface, wrapper } = await setup()
    expect(surface.applyReviewFrame).toHaveBeenNthCalledWith(1, expect.objectContaining({ event: 'queue_state', busy: true, nodeId: 'impl' }))
    expect(surface.applyReviewFrame).toHaveBeenNthCalledWith(2, expect.objectContaining({ event: 'turn_begin', nodeId: 'impl' }))
    expect(surface.applyAcpEvents).toHaveBeenCalledWith([{ kind: 'agent_message', text: '已有输出' }], 'impl')
    expect(FakeSocket.last?.url).toBe('ws://test/api/runs/r1/events')
    FakeSocket.last!.send({ type: 'acp', nodeId: 'impl', events: [{ kind: 'agent_message', text: '新输出' }] })
    expect(surface.applyAcpEvents).toHaveBeenLastCalledWith([{ kind: 'agent_message', text: '新输出' }], 'impl')
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
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=impl')
    wrapper.unmount()
  })

  it('already_manual (a concurrent takeover won) is success: no error, straight into the chat', async () => {
    const { live, router, wrapper } = await setup()
    mocks.takeoverNode.mockResolvedValue({ runId: 'r1', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'waiting_human', status: 'already_manual' })
    await live().takeover()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=impl')
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
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=impl')
    wrapper.unmount()
  })

  // User rule: once the user is looking at a node, the page never jumps by itself.
  it.each([
    ['a gate', { id: 'r1', status: 'waiting_human', currentNode: { ...cur('gate', 'gate', 'manual', 'waiting_human'), name: '上线审批' } }, { kind: 'gate', label: '上线审批' }],
    ['the end of the run', { id: 'r1', status: 'succeeded' }, { kind: 'run', status: 'succeeded' }],
    ['a failed run', { id: 'r1', status: 'failed' }, { kind: 'run', status: 'failed' }],
  ])('stays on the page when the node on screen finishes (run moved to %s): no navigation, shown as done', async (_label, next, endState) => {
    const { live, router, surface, wrapper } = await setup()
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
    expect(surface.applyReviewFrame).toHaveBeenLastCalledWith({ event: 'turn_done', nodeId: 'impl' })
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
  it('auto → auto in place: the next node is seeded once on the re-keyed surface, early frames replayed there, never on the old one', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    await router.push('/runs/r1/live?node=impl')
    const mk = (): LiveChatSurface => ({ applyReviewFrame: vi.fn(), applyAcpEvents: vi.fn() })
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
    const next = surfaces[0]
    // The old surface never sees 验证.
    expect(first.applyReviewFrame).not.toHaveBeenCalledWith(expect.objectContaining({ nodeId: 'verify' }))
    expect(first.applyAcpEvents).not.toHaveBeenCalledWith(expect.anything(), 'verify')
    // The new one is seeded exactly once and gets the early frame.
    const begins = (next.applyReviewFrame as ReturnType<typeof vi.fn>).mock.calls.filter(([f]) => f.event === 'turn_begin')
    expect(begins).toHaveLength(1)
    expect(next.applyAcpEvents).toHaveBeenCalledWith(early, 'verify')
    // Later frames stream into the same turn.
    const later = [...early, { t: 0, kind: 'tool_call', title: '运行测试', status: 'completed' }]
    FakeSocket.last!.send({ type: 'acp', nodeId: 'verify', events: later, busy: true })
    expect(next.applyAcpEvents).toHaveBeenLastCalledWith(later, 'verify')
    expect((next.applyReviewFrame as ReturnType<typeof vi.fn>).mock.calls.filter(([f]) => f.event === 'turn_begin')).toHaveLength(1)
    // ?node= follows the node on screen (replace, same page); later polls: no re-seed, no new turn.
    expect(replace).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/runs/r1/live?node=verify')
    await live.reload()
    await flushPromises()
    await live.reload()
    await flushPromises()
    expect(surfaces).toHaveLength(1)
    expect((next.applyReviewFrame as ReturnType<typeof vi.fn>).mock.calls.filter(([f]) => f.event === 'turn_begin')).toHaveLength(1)
    // Run ends: one turn_done on the new surface, nothing busy left, no navigation.
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'completed' })
    await live.reload()
    await flushPromises()
    expect(live.ended.value).toBe(true)
    expect(next.applyReviewFrame).toHaveBeenLastCalledWith({ event: 'turn_done', nodeId: 'verify' })
    expect(replace).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  // #850: the conversation continues in place with the run's next node.
  it('next auto node: continues in place (earlier nodes folded above, next stream), no navigation', async () => {
    const { live, router, surface, wrapper } = await setup()
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
    expect((surface.applyReviewFrame as ReturnType<typeof vi.fn>).mock.calls.filter(([f]) => f.event === 'turn_begin' && f.nodeId === 'test')).toHaveLength(1)
    expect(live().nodeId.value).toBe('test')
    expect(live().mode.value).toBe('observe')
    expect(live().ended.value).toBe(false)
    expect(live().priorSegments.value.map((s) => s.label)).toEqual(['实现'])
    expect(live().task.value).toBe('跑单元测试')
    // The next node's stream seeds the (re-keyed) surface.
    expect(surface.applyReviewFrame).toHaveBeenCalledWith({ event: 'turn_begin', nodeId: 'test', item: { text: '' } })
    wrapper.unmount()
  })

  it('next node waiting for a person: its chat continues in place — reply and 确认并流转 here, still no navigation', async () => {
    const { live, router, surface, wrapper } = await setup()
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
    expect(surface.applyReviewFrame).toHaveBeenLastCalledWith({ type: 'review', event: 'turn_begin', nodeId: 'accept' })
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
      const surface: LiveChatSurface = { applyReviewFrame: vi.fn(), applyAcpEvents: vi.fn() }
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
    expect(again.surface.applyReviewFrame).toHaveBeenCalledWith(expect.objectContaining({ event: 'turn_begin', nodeId: 'test' }))
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
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=impl')
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
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=impl')
    expect(mocks.getRun.mock.calls.length).toBe(before)
    wrapper.unmount()
  })

  it('ignores nodeRuns: a stale running nodeRun with no currentNode is not observed as auto', async () => {
    mocks.getRun.mockResolvedValue({ id: 'r1', status: 'waiting_human', currentNode: cur('gate', 'gate', 'manual', 'waiting_human'), nodeRuns: { impl: { status: 'running', iteration: 1 } } })
    const { router, wrapper } = await setup()
    expect(router.currentRoute.value.fullPath).toBe('/gates?run=r1&node=gate')
    wrapper.unmount()
  })
})
