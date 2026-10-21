// @vitest-environment happy-dom
import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'

const mocks = vi.hoisted(() => ({ getRun: vi.fn(), nodeEvents: vi.fn(), takeoverNode: vi.fn(), show: vi.fn(), error: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getRun: mocks.getRun, nodeEvents: mocks.nodeEvents, takeoverNode: mocks.takeoverNode, runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events` } }))
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
  const surface: LiveChatSurface = { applyReviewFrame: vi.fn(), applyAcpEvents: vi.fn() }
  let live!: ReturnType<typeof useRunLive>
  const Harness = defineComponent({
    setup() {
      live = useRunLive(ref('r1'), ref(String(router.currentRoute.value.query.node || '')), ref(surface))
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
})
afterEach(() => vi.unstubAllGlobals())

describe('useRunLive (auto node → Stop → chat)', () => {
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
    ['the next auto node', { id: 'r1', status: 'running', currentNode: cur('test', 'agent', 'auto', 'running') }],
    ['the next node parked for a person', { id: 'r1', status: 'waiting_human', currentNode: cur('gate', 'gate', 'manual', 'waiting_human') }],
    ['the end of the run', { id: 'r1', status: 'succeeded' }],
  ])('stays on the page when the node on screen finishes (run moved to %s): no navigation, shown as done', async (_label, next) => {
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
    // Later frames and polls no longer route the page either.
    const calls = mocks.getRun.mock.calls.length
    FakeSocket.last?.send({ type: 'snapshot', run: next })
    await live().reload()
    await flushPromises()
    expect(replace).not.toHaveBeenCalled()
    expect(mocks.getRun.mock.calls.length).toBe(calls)
    wrapper.unmount()
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
