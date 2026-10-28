// @vitest-environment happy-dom
import { computed, defineComponent, h, shallowRef, watch } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'

const mocks = vi.hoisted(() => ({
  getRun: vi.fn(), nodeEvents: vi.fn(), takeoverNode: vi.fn(),
  runConversationChain: vi.fn(), reactReply: vi.fn(), show: vi.fn(), error: vi.fn(),
  armSidebarHandoff: vi.fn(), clearSidebarHandoff: vi.fn(),
}))
vi.mock('@/lib/api/api', () => ({ api: {
  getRun: mocks.getRun, nodeEvents: mocks.nodeEvents, takeoverNode: mocks.takeoverNode,
  runConversationChain: mocks.runConversationChain, reactReply: mocks.reactReply,
  runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events`,
} }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ show: mocks.show, error: mocks.error }) }))
vi.mock('@/lib/inbox/sidebarHandoff', async (original) => ({
  ...await original<typeof import('@/lib/inbox/sidebarHandoff')>(),
  armSidebarHandoff: mocks.armSidebarHandoff,
  clearSidebarHandoff: mocks.clearSidebarHandoff,
}))

import { useRunLive, type LiveChatSurface } from './useRunLive'

class FakeSocket {
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  close() { this.onclose?.() }
}

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: Error) => void
  const promise = new Promise((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

const currentNode = (id: string, kind: string, mode: string, state: string) => ({
  id, name: id, iteration: 1, kind, mode, state,
})
const autoRun = (id: string) => ({ id, status: 'running', currentNode: currentNode('impl', 'agent', 'auto', 'running') })
const chatRun = (id: string) => ({
  id, status: 'waiting_human', currentNode: currentNode('accept', 'clarify', 'manual', 'waiting_human'),
  clarifyByNode: { accept: { nodeId: 'accept', iteration: 1, turns: [], done: false } },
})
const wrappers: ReturnType<typeof mount>[] = []

async function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/runs/:id/:rest(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/runs/A/live?node=impl')
  const phases = new Map<string, 'auto' | 'chat'>()
  mocks.getRun.mockImplementation(async (id: string) => phases.get(id) === 'chat' ? chatRun(id) : autoRun(id))
  const newSurface = (): LiveChatSurface => ({
    applyReviewFrame: vi.fn(), applyAcpEvents: vi.fn(), releaseConfirmPending: vi.fn(),
  })
  const surface = shallowRef<LiveChatSurface | null>(newSurface())
  let live!: ReturnType<typeof useRunLive>
  const Harness = defineComponent({
    setup() {
      const runId = computed(() => String(router.currentRoute.value.params.id || ''))
      // A new run mounts a new chat surface, as the live view's key does.
      watch(runId, () => { surface.value = newSurface() }, { flush: 'sync' })
      live = useRunLive(runId, computed(() => String(router.currentRoute.value.query.node || '')), surface)
      return () => h('div')
    },
  })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })
  const wrapper = mount(Harness, { global: { plugins: [router, i18n] } })
  wrappers.push(wrapper)
  await flushPromises()

  async function continueChat() {
    const id = String(router.currentRoute.value.params.id)
    phases.set(id, 'chat')
    await live.reload()
    await flushPromises()
    expect(live.mode.value).toBe('chat')
    expect(live.nodeId.value).toBe('accept')
  }
  async function switchRun(id: string, chat = false) {
    phases.set(id, 'auto')
    await router.push(`/runs/${id}/live?node=impl`)
    await flushPromises()
    expect(live.run.value?.id).toBe(id)
    if (chat) await continueChat()
  }
  return { live, router, surface, switchRun, continueChat }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('WebSocket', FakeSocket)
  mocks.nodeEvents.mockResolvedValue({ events: [] })
  mocks.runConversationChain.mockRejectedValue(Object.assign(new Error('until_not_found'), { status: 400 }))
  mocks.reactReply.mockResolvedValue({})
})
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('useRunLive action responses belong to the originating visit', () => {
  it('a late takeover success from A cannot navigate B or replace B’s pending stop', async () => {
    const { live, router, switchRun } = await setup()
    const a = deferred(), b = deferred()
    mocks.takeoverNode.mockImplementationOnce(() => a.promise).mockImplementationOnce(() => b.promise)
    const pendingA = live.takeover()
    await switchRun('B')
    const pendingB = live.takeover()
    expect(live.stopping.value).toBe(true)
    const replace = vi.spyOn(router, 'replace')
    const loads = mocks.getRun.mock.calls.length

    a.resolve({ runId: 'A', nodeId: 'impl', iteration: 1, state: 'waiting_human', status: 'taken_over' })
    await pendingA
    await flushPromises()

    expect(replace).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/runs/B/live?node=impl')
    expect(live.run.value?.id).toBe('B')
    expect(live.stopping.value).toBe(true)
    expect(mocks.getRun).toHaveBeenCalledTimes(loads)
    expect(mocks.error).not.toHaveBeenCalled()
    b.resolve({ runId: 'B', nodeId: 'impl', iteration: 1, state: 'stopping', status: 'taken_over' })
    await pendingB
  })

  it('an old send failure remains stale after A → B → A, without a toast or a refresh', async () => {
    const { live, switchRun, continueChat } = await setup()
    await continueChat()
    const a = deferred()
    mocks.reactReply.mockImplementationOnce(() => a.promise)
    const pendingA = live.send('A visit one reply')
    expect(mocks.reactReply).toHaveBeenCalledWith('A', 'accept', 'A visit one reply', [], false, [])
    await switchRun('B', true)
    await switchRun('A', true)
    const loads = mocks.getRun.mock.calls.length

    a.reject(new Error('old A request failed'))
    await pendingA
    await flushPromises()

    expect(mocks.error).not.toHaveBeenCalled()
    expect(mocks.show).not.toHaveBeenCalled()
    expect(mocks.getRun).toHaveBeenCalledTimes(loads)
    expect(live.confirmError.value).toBeNull()
    expect(live.handoff.value).toBeNull()
    expect(live.mode.value).toBe('chat')
  })

  it.each(['success', 'failure'] as const)('a late A confirm %s cannot settle or release B’s own confirmation on the same node', async (outcome) => {
    const { live, router, surface, switchRun, continueChat } = await setup()
    await continueChat()
    const a = deferred(), b = deferred()
    mocks.reactReply.mockImplementationOnce(() => a.promise).mockImplementationOnce(() => b.promise)
    const pendingA = live.confirm()
    await switchRun('B', true)
    const surfaceB = surface.value!
    const pendingB = live.confirm()
    const handoffB = live.handoff.value
    expect(handoffB?.phase).toBe('pending')
    const loads = mocks.getRun.mock.calls.length
    const replace = vi.spyOn(router, 'replace')

    if (outcome === 'success') a.resolve({})
    else a.reject(new Error('old A confirmation failed'))
    await pendingA
    await flushPromises()

    expect(live.handoff.value).toBe(handoffB)
    expect(live.handoff.value?.phase).toBe('pending')
    expect(live.confirmError.value).toBeNull()
    expect(surfaceB.releaseConfirmPending).not.toHaveBeenCalled()
    expect(mocks.clearSidebarHandoff).not.toHaveBeenCalledWith('B')
    expect(mocks.getRun).toHaveBeenCalledTimes(loads)
    expect(mocks.error).not.toHaveBeenCalled()
    expect(replace).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/runs/B/live?node=accept')

    b.resolve({})
    await pendingB
    await flushPromises()
    expect(live.handoff.value?.phase).toBe('done')
  })
})
