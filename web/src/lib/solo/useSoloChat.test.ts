// @vitest-environment happy-dom
import { createApp, defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  getSoloSession: vi.fn(),
  listSoloMessages: vi.fn(),
  sendSoloTurn: vi.fn(),
  cancelSoloTurn: vi.fn(),
  closeSoloSession: vi.fn(),
  renameSoloSession: vi.fn(),
  removeSoloQueued: vi.fn(),
  setSoloSessionModel: vi.fn(),
  setSoloSessionReasoningEffort: vi.fn(),
  soloChatWsUrl: vi.fn(() => 'ws://example.test/solo'),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...mocks } }
})

import { useSoloChat } from './useSoloChat'
import { modelRouteValue } from '@/lib/agent/modelBindings'

class FakeSocket {
  static all: FakeSocket[] = []
  static OPEN = 1
  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(public url: string) {
    FakeSocket.all.push(this)
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  emit(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) })
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = 3
  }
}

const session = (over: Record<string, unknown> = {}) => ({
  id: 's1',
  userId: 'u',
  title: 'T',
  agentName: 'coder',
  status: 'running',
  restarts: 0,
  busy: false,
  waiting: 0,
  createdAt: '',
  updatedAt: '',
  ...over,
})

function mountChat() {
  let chat!: ReturnType<typeof useSoloChat>
  const id = ref('s1')
  const app = createApp(
    defineComponent({
      setup() {
        chat = useSoloChat(id)
        return () => null
      },
    }),
  )
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages } } }))
  app.mount(document.createElement('div'))
  return { chat, app, id }
}

const acp = (text: string) => ({ type: 'acp', data: { op: 'event', data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text } } } } })

describe('useSoloChat', () => {
  beforeEach(() => {
    FakeSocket.all = []
    vi.stubGlobal('WebSocket', FakeSocket)
    Object.values(mocks).forEach((m) => m.mockReset())
    mocks.soloChatWsUrl.mockImplementation((_id: string, after?: number) =>
      `ws://example.test/solo${after != null && after >= 0 ? `?after=${after}` : ''}`,
    )
    mocks.getSoloSession.mockResolvedValue(session())
    mocks.listSoloMessages.mockResolvedValue([])
  })
  afterEach(() => vi.unstubAllGlobals())

  it('streams a turn from turn_begin to turn_done and then reloads history', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()

    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [{ id: 'q1', text: 'next', images: [] }] })
    expect(chat.queued.value.map((q) => q.id)).toEqual(['q1'])

    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'q1', text: 'next', images: [] }, userMsgId: 'q1' })
    ws.emit({ type: 'session', event: 'phase', phase: 'running' })
    ws.emit(acp('Hel'))
    ws.emit(acp('lo'))
    expect(chat.queued.value).toEqual([])
    expect(chat.phase.value).toBe('running')
    expect(chat.turns.value.map((x) => x.id)).toEqual(['u:q1', 'live:q1'])
    expect(chat.turns.value[1]!.text).toBe('Hello')
    expect(chat.turns.value[1]!.streaming).toBe(true)

    mocks.listSoloMessages.mockResolvedValue([
      { id: 'q1', sessionId: 's1', role: 'user', content: 'next', createdAt: '' },
      { id: 'a1', sessionId: 's1', role: 'assistant', content: 'Hello', status: 'ok', createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'turn_done', interrupted: false })
    expect(chat.turns.value[1]!.streaming).toBe(false)
    await flushPromises()
    expect(chat.turns.value.map((x) => x.id)).toEqual(['u:q1', 'a:a1'])
    app.unmount()
  })

  it('replaces the persisted user message when a reconnect replays the active turn', async () => {
    mocks.listSoloMessages.mockResolvedValue([{ id: 'q1', sessionId: 's1', role: 'user', content: 'hi', createdAt: '' }])
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    // Frames arriving before history loads are held, then replayed in order.
    ws.open()
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [], phase: 'preparing' })
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'q1', text: 'hi', images: [] } })
    await flushPromises()
    expect(chat.turns.value.map((x) => x.id)).toEqual(['u:q1', 'live:q1'])
    expect(chat.phase.value).toBe('preparing')
    expect(chat.busy.value).toBe(true)
    app.unmount()
  })

  it('applies status frames and blocks sending once closed', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    expect(chat.canSend.value).toBe(true)
    mocks.getSoloSession.mockResolvedValue(session({ status: 'closed' }))
    ws.emit({ type: 'session', event: 'status', status: 'closed', error: '' })
    expect(chat.closed.value).toBe(true)
    expect(chat.canSend.value).toBe(false)
    expect(await chat.send('x')).toBe(false)
    expect(mocks.sendSoloTurn).not.toHaveBeenCalled()
    app.unmount()
  })

  it('shows a local queue item until the server confirms, and rolls back on failure', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    FakeSocket.all[0]!.open()
    await flushPromises()
    mocks.sendSoloTurn.mockRejectedValueOnce(new Error('排队已满'))
    const pending = chat.send('hello')
    expect(chat.queued.value).toHaveLength(1)
    expect(chat.queued.value[0]!.local).toBe(true)
    expect(await pending).toBe(false)
    expect(chat.queued.value).toEqual([])
    expect(chat.actionError.value).toBe('排队已满')
    app.unmount()
  })

  it('sends cancel over the socket', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    await chat.cancel()
    expect(ws.sent).toEqual([JSON.stringify({ type: 'cancel' })])
    app.unmount()
  })

  it('keeps an old session on its opening model and switches the saved model for later turns', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ initialModelId: 'aux' }))
    const { chat, app } = mountChat()
    await flushPromises()
    expect(chat.currentModelId.value).toBe('aux')

    mocks.getSoloSession.mockResolvedValue(session({ initialModelId: 'aux', currentModelSet: true, currentModelId: 'aux' }))
    mocks.setSoloSessionModel.mockResolvedValue(session({ initialModelId: 'aux', currentModelSet: true, currentModelId: 'aux2' }))
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'q1', text: 'hi', images: [] }, userMsgId: 'q1' })
    const pending = chat.setCurrentModel('aux2')
    expect(chat.currentModelId.value).toBe('aux2')
    expect(chat.modelSwitchNotice.value).toContain('从下一条开始使用新模型')
    expect(await pending).toBe(true)
    expect(mocks.setSoloSessionModel).toHaveBeenCalledWith('s1', 'aux2')

    ws.emit({ type: 'session', event: 'turn_done', interrupted: false })
    expect(chat.modelSwitchNotice.value).toBe('')
    app.unmount()
  })

  it('restores the previous model when saving the selection fails', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux', initialModelId: 'primary' }))
    mocks.setSoloSessionModel.mockRejectedValueOnce(Object.assign(new Error('nope'), { code: 'model_not_in_configuration' }))
    const { chat, app } = mountChat()
    await flushPromises()
    expect(await chat.setCurrentModel('other')).toBe(false)
    expect(chat.currentModelId.value).toBe('aux')
    expect(chat.actionError.value).toContain('不在该 Agent 的模型配置中')
    expect(chat.modelSwitchNotice.value).toBe('')
    app.unmount()
  })

  it('rolls the selector back when the sandbox rejects the switch', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux2' }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({
      type: 'session',
      event: 'model',
      currentModelId: 'aux',
      currentModelSet: true,
      modelNotice: 'model_switch_failed',
      error: '切换模型失败，已改回原来的模型',
    })
    expect(chat.currentModelId.value).toBe('aux')
    expect(chat.actionError.value).toContain('恢复原来的模型')
    app.unmount()
  })
  it('changes the effort optimistically during a reply and keeps the model unchanged', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'low', reasoningEfforts: ['low', 'high'] }))
    let resolve!: (value: ReturnType<typeof session>) => void
    mocks.setSoloSessionReasoningEffort.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [] })
    const pending = chat.setCurrentReasoningEffort('high')
    expect(chat.currentReasoningEffort.value).toBe('high')
    expect(chat.currentModelId.value).toBe('aux')
    expect(chat.modelChangePending.value).toBe(false)
    expect(chat.modelSwitchNotice.value).not.toBe('')
    await flushPromises()
    expect(mocks.setSoloSessionReasoningEffort).toHaveBeenCalledWith('s1', 'high')
    expect(mocks.setSoloSessionModel).not.toHaveBeenCalled()
    resolve(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'high', reasoningEfforts: ['low', 'high'] }))
    expect(await pending).toBe(true)
    expect(chat.currentModelId.value).toBe('aux')
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
    expect(chat.modelSwitchNotice.value).toBe('')
    app.unmount()
  })

  it('does not let background metadata reads overwrite a pending effort selection', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ reasoningEffort: 'low' }))
    let resolve!: (value: ReturnType<typeof session>) => void
    mocks.setSoloSessionReasoningEffort.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { chat, app } = mountChat()
    await flushPromises()
    const pending = chat.setCurrentReasoningEffort('high')
    await flushPromises()
    await chat.refreshSession()
    expect(mocks.getSoloSession).toHaveBeenCalledTimes(1)
    resolve(session({ reasoningEffort: 'high' }))
    expect(await pending).toBe(true)
    expect(chat.currentReasoningEffort.value).toBe('high')
    app.unmount()
  })

  it('serializes slider choices and rolls a failed latest choice back to the last confirmed effort', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'low' }))
    let resolveFirst!: (value: ReturnType<typeof session>) => void
    let rejectSecond!: (error: Error) => void
    mocks.setSoloSessionReasoningEffort
      .mockImplementationOnce(() => new Promise(done => { resolveFirst = done }))
      .mockImplementationOnce(() => new Promise((_, reject) => { rejectSecond = reject }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    const first = chat.setCurrentReasoningEffort('medium')
    const second = chat.setCurrentReasoningEffort('high')
    expect(chat.currentReasoningEffort.value).toBe('high')
    await flushPromises()
    expect(mocks.setSoloSessionReasoningEffort).toHaveBeenCalledTimes(1)
    resolveFirst(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'medium' }))
    expect(await first).toBe(true)
    await flushPromises()
    expect(mocks.setSoloSessionReasoningEffort.mock.calls.map(call => call[1])).toEqual(['medium', 'high'])
    expect(chat.currentReasoningEffort.value).toBe('high')
    ws.emit({ type: 'session', event: 'model', currentModelId: 'aux', reasoningEffort: 'medium' })
    expect(chat.currentReasoningEffort.value).toBe('high')
    rejectSecond(Object.assign(new Error('unsupported effort'), { code: 'reasoning_effort_unavailable' }))
    expect(await second).toBe(false)
    expect(chat.currentReasoningEffort.value).toBe('medium')
    expect(chat.currentModelId.value).toBe('aux')
    expect(chat.actionError.value).not.toBe('')
    app.unmount()
  })

  it('queues effort writes after model writes without losing the latest optimistic choice', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'old', reasoningEffort: 'low', reasoningEfforts: ['low', 'high'] }))
    let resolveModel!: (value: ReturnType<typeof session>) => void
    mocks.setSoloSessionModel.mockImplementationOnce(() => new Promise(done => { resolveModel = done }))
    mocks.setSoloSessionReasoningEffort.mockResolvedValueOnce(session({ currentModelSet: true, currentModelId: 'new', reasoningEffort: 'high', reasoningEfforts: ['high'] }))
    const { chat, app } = mountChat()
    await flushPromises()
    const modelRequest = chat.setCurrentModel('new')
    const effortRequest = chat.setCurrentReasoningEffort('high')
    expect(chat.currentModelId.value).toBe('new')
    expect(chat.modelChangePending.value).toBe(true)
    expect(chat.session.value?.reasoningEfforts).toEqual([])
    await flushPromises()
    expect(mocks.setSoloSessionReasoningEffort).not.toHaveBeenCalled()
    resolveModel(session({ currentModelSet: true, currentModelId: 'new', reasoningEffort: 'low', reasoningEfforts: ['high'] }))
    expect(await modelRequest).toBe(true)
    expect(await effortRequest).toBe(true)
    expect(chat.currentReasoningEffort.value).toBe('high')
    expect(chat.currentModelId.value).toBe('new')
    expect(chat.modelChangePending.value).toBe(false)
    app.unmount()
  })

  it.each([false, true])('ignores an old session effort response after navigation (failure: %s)', async (fails) => {
    mocks.getSoloSession.mockResolvedValueOnce(session({ reasoningEffort: 'low' }))
      .mockResolvedValueOnce(session({ id: 's2', reasoningEffort: 'medium' }))
    let resolve!: (value: ReturnType<typeof session>) => void
    let reject!: (error: Error) => void
    mocks.setSoloSessionReasoningEffort.mockImplementationOnce(() => new Promise((done, fail) => { resolve = done; reject = fail }))
    const { chat, app, id } = mountChat()
    await flushPromises()
    const pending = chat.setCurrentReasoningEffort('high')
    await flushPromises()
    id.value = 's2'
    await flushPromises()
    if (fails) reject(new Error('old request failed'))
    else resolve(session({ reasoningEffort: 'high' }))
    expect(await pending).toBe(false)
    expect(chat.session.value?.id).toBe('s2')
    expect(chat.currentReasoningEffort.value).toBe('medium')
    expect(chat.actionError.value).toBe('')
    app.unmount()
  })

  it('keeps a runtime effort rollback authoritative over a late HTTP response', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'low' }))
    let resolve!: (value: ReturnType<typeof session>) => void
    mocks.setSoloSessionReasoningEffort.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    const pending = chat.setCurrentReasoningEffort('high')
    await flushPromises()
    ws.emit({ type: 'session', event: 'model', currentModelId: 'aux', reasoningEffort: 'low', reasoningEfforts: ['low', 'medium'], modelNotice: 'model_switch_failed', error: 'switch rolled back' })
    expect(chat.currentReasoningEffort.value).toBe('low')
    resolve(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'high' }))
    expect(await pending).toBe(false)
    expect(chat.currentReasoningEffort.value).toBe('low')
    expect(chat.session.value?.reasoningEfforts).toEqual(['low', 'medium'])
    expect(chat.actionError.value).not.toBe('')
    app.unmount()
  })

  it('applies effort-only WebSocket updates without changing the model', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'low' }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'model', reasoningEffort: 'high', reasoningEfforts: ['low', 'high'] })
    expect(chat.currentModelId.value).toBe('aux')
    expect(chat.currentReasoningEffort.value).toBe('high')
    app.unmount()
  })

  it('writes a second turn and output that arrives before turn_begin', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()

    ws.emit({ type: 'session', event: 'turn_begin', seq: 1, item: { id: 'q1', text: 'first', images: [] } })
    ws.emit({ ...acp('One'), seq: 2 })
    ws.emit({ type: 'session', event: 'turn_done', seq: 3 })
    expect(chat.turns.value[1]!.text).toBe('One')
    expect(chat.turns.value[1]!.streaming).toBe(false)

    ws.emit({ type: 'session', event: 'queue_state', seq: 4, busy: true, items: [{ id: 'q2', text: 'second', images: [] }] })
    ws.emit({ ...acp('Two'), seq: 5 })
    expect(chat.turns.value.some((t) => t.id === 'live:q2' && t.text === 'Two' && t.streaming)).toBe(true)
    expect(chat.turns.value.filter((t) => t.text === 'Two')).toHaveLength(1)

    ws.emit({ type: 'session', event: 'turn_begin', seq: 6, item: { id: 'q2', text: 'second', images: [] } })
    expect(chat.turns.value.find((t) => t.id === 'live:q2')!.text).toBe('Two')
    app.unmount()
  })

  it('replays turn_begin when the subscription snapshot has the same sequence', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()

    // 订阅快照复用最新真实事件的 seq，不能因此吞掉随后的真实回放开场帧。
    const item = { id: 'q1', text: '刚发送的消息', images: [] }
    ws.emit({ type: 'session', event: 'queue_state', seq: 7, busy: true, items: [], activeItem: item })
    ws.emit({ type: 'session', event: 'turn_begin', seq: 7, item, userMsgId: 'q1' })
    ws.emit({ type: 'session', event: 'turn_begin', seq: 7, item, userMsgId: 'q1' })

    expect(chat.turns.value.map((turn) => turn.id)).toEqual(['u:q1', 'live:q1'])
    expect(chat.turns.value[0]!.text).toBe('刚发送的消息')
    app.unmount()
  })

  it('replays the opening frame when a disconnect follows its same-sequence snapshot', async () => {
    vi.useFakeTimers()
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()

    const item = { id: 'q1', text: '断线前发送的消息', images: [] }
    ws.emit({ type: 'session', event: 'queue_state', seq: 7, busy: true, items: [], activeItem: item })
    // 快照可以推进缺口检测高水位，但同 seq 开场未处理，不能进入重连确认游标。
    ws.onclose?.()
    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    expect(again).not.toBe(ws)
    expect(again.url).not.toContain('after=')
    again.open()
    await flushPromises()

    // 与服务端一致：快照照常发送，回放只包含 seq > after 的真实帧。
    const after = Number(new URL(again.url).searchParams.get('after') ?? -1)
    again.emit({ type: 'session', event: 'queue_state', seq: 7, busy: true, items: [], activeItem: item })
    if (7 > after) again.emit({ type: 'session', event: 'turn_begin', seq: 7, item, userMsgId: 'q1' })
    again.emit({ ...acp('回复'), seq: 8 })

    expect(chat.turns.value.map((turn) => turn.id)).toEqual(['u:q1', 'live:q1'])
    expect(chat.turns.value[0]!.text).toBe('断线前发送的消息')
    expect(chat.turns.value[1]!.text).toBe('回复')
    vi.useRealTimers()
    app.unmount()
  })

  it('resumes unacknowledged replay across legal KeepNone sequence holes', async () => {
    vi.useFakeTimers()
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()

    const item = { id: 'q1', text: '跨空洞恢复', images: [] }
    ws.emit({ type: 'session', event: 'queue_state', seq: 5, busy: true, items: [], activeItem: item })
    ws.emit({ type: 'session', event: 'turn_begin', seq: 2, item, userMsgId: 'q1' })
    // seq 3 是不进入回放的 KeepNone 帧；确认游标只能到已处理的 seq 2。
    ws.onclose?.()
    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    expect(again.url).toContain('after=2')
    again.open()
    await flushPromises()

    const after = Number(new URL(again.url).searchParams.get('after') ?? -1)
    again.emit({ type: 'session', event: 'queue_state', seq: 5, busy: true, items: [], activeItem: item })
    const replay = [
      { type: 'session', event: 'phase', seq: 4, phase: 'running' },
      { ...acp('完整恢复'), seq: 5 },
    ]
    for (const frame of replay) {
      if (frame.seq > after) again.emit(frame)
    }

    expect(chat.turns.value.map((turn) => turn.id)).toEqual(['u:q1', 'live:q1'])
    expect(chat.turns.value[1]!.text).toBe('完整恢复')
    expect(chat.phase.value).toBe('running')
    expect(FakeSocket.all).toHaveLength(2)
    vi.useRealTimers()
    app.unmount()
  })

  it('restores KeepNone session and model state from the subscribe snapshot', async () => {
    let releaseStale: (row: ReturnType<typeof session>) => void = () => {}
    mocks.getSoloSession
      .mockResolvedValueOnce(session({ status: 'running', currentModelSet: true, currentModelId: 'old' }))
      .mockImplementationOnce(() => new Promise((resolve) => { releaseStale = resolve }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    ws.emit({
      type: 'session',
      event: 'queue_state',
      busy: false,
      items: [],
      status: 'closed',
      error: '会话已关闭',
      sandboxId: 9,
      currentModelId: 'model-b',
      currentModelSet: true,
      reasoningEffort: 'high',
      modelNotice: 'rolled',
    })
    await flushPromises()
    expect(chat.session.value?.status).toBe('closed')
    expect(chat.session.value?.error).toBe('会话已关闭')
    expect(chat.session.value?.sandboxId).toBe(9)
    expect(chat.session.value?.currentModelId).toBe('model-b')
    expect(chat.session.value?.currentModelSet).toBe(true)
    expect(chat.session.value?.reasoningEffort).toBe('high')
    expect(chat.session.value?.modelNotice).toBe('rolled')
    expect(mocks.getSoloSession).toHaveBeenCalledTimes(2)
    releaseStale(session({ status: 'running', currentModelSet: true, currentModelId: 'old' }))
    await flushPromises()
    expect(chat.session.value?.status).toBe('closed')
    expect(chat.session.value?.error).toBe('会话已关闭')
    expect(chat.session.value?.sandboxId).toBe(9)
    expect(chat.session.value?.currentModelId).toBe('model-b')
    expect(chat.session.value?.currentModelSet).toBe(true)
    expect(chat.session.value?.reasoningEffort).toBe('high')
    expect(chat.session.value?.modelNotice).toBe('rolled')
    app.unmount()
  })

  it('reconnects after the socket closes and does not draw the same frame twice', async () => {
    vi.useFakeTimers()
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'turn_begin', seq: 1, item: { id: 'q1', text: 'hi', images: [] } })
    ws.emit({ ...acp('Hello'), seq: 2 })
    expect(chat.turns.value[1]!.text).toBe('Hello')

    ws.onclose?.()
    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    expect(again).not.toBe(ws)
    expect(again.url).toContain('after=2')
    again.open()
    await flushPromises()
    again.emit({ type: 'session', event: 'queue_state', seq: 2, busy: true, items: [], activeItem: { id: 'q1', text: 'hi', images: [] } })
    again.emit({ ...acp('Hello'), seq: 2 })
    again.emit({ ...acp('!'), seq: 3 })
    expect(chat.turns.value.find((t) => t.id === 'live:q1')!.text).toBe('Hello!')
    vi.useRealTimers()
    app.unmount()
  })

  it('settles a missing terminal frame from persisted history after reconnect idle', async () => {
    vi.useFakeTimers()
    mocks.listSoloMessages.mockResolvedValueOnce([])
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'turn_begin', seq: 1, item: { id: 'q1', text: 'hi', images: [] } })
    ws.emit({ ...acp('partial'), seq: 2 })
    expect(chat.turns.value[1]?.streaming).toBe(true)

    mocks.listSoloMessages.mockResolvedValue([
      { id: 'q1', sessionId: 's1', role: 'user', content: 'hi', createdAt: '' },
      { id: 'a1', sessionId: 's1', role: 'assistant', content: 'final reply', status: 'ok', createdAt: '' },
    ])
    // The server closes an undeliverable subscription; no later seq or done arrives.
    ws.onclose?.()
    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    again.open()
    await flushPromises()
    again.emit({ type: 'session', event: 'queue_state', seq: 2, busy: false, items: [] })
    await flushPromises()

    expect(chat.turns.value.map((turn) => turn.id)).toEqual(['u:q1', 'a:a1'])
    expect(chat.turns.value[1]?.text).toBe('final reply')
    expect(chat.turns.value.some((turn) => turn.streaming)).toBe(false)
    expect(chat.busy.value).toBe(false)
    vi.useRealTimers()
    app.unmount()
  })

  it('fills a sequence hole by replaying after the last frame', async () => {
    vi.useFakeTimers()
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    ws.emit({ type: 'session', event: 'turn_begin', seq: 1, item: { id: 'q1', text: 'hi', images: [] } })
    ws.emit({ ...acp('A'), seq: 2 })
    ws.emit({ ...acp('SKIP'), seq: 5 })
    expect(chat.turns.value[1]!.text).toBe('A')

    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    expect(again.url).toContain('after=2')
    again.open()
    await flushPromises()
    // 重连快照先到且只推高水位，不能把它自身当成需要重连的序号缺口。
    again.emit({ type: 'session', event: 'queue_state', seq: 5, busy: true, items: [], activeItem: { id: 'q1', text: 'hi', images: [] } })
    again.emit({ ...acp('B'), seq: 3 })
    again.emit({ ...acp('C'), seq: 4 })
    again.emit({ ...acp('D'), seq: 5 })
    expect(chat.turns.value.find((t) => t.id === 'live:q1')!.text).toBe('ABCD')
    expect(chat.turns.value.filter((t) => t.role === 'agent')).toHaveLength(1)
    vi.useRealTimers()
    app.unmount()
  })
  it('switches the same model through another group and restores the group on runtime rollback', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-a', backend: 'codex' }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open(); await flushPromises()
    mocks.setSoloSessionModel.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-b', backend: 'opencode' }))
    const pending = chat.setCurrentModel(modelRouteValue('account-b', 'same/model'))
    expect(chat.currentConfigurationId.value).toBe('account-b')
    expect(await pending).toBe(true)
    expect(mocks.setSoloSessionModel).toHaveBeenCalledWith('s1', 'same/model', undefined, 'account-b')
    expect(chat.session.value?.backend).toBe('opencode')
    ws.emit({ type: 'session', event: 'model', currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-a', backend: 'codex', modelNotice: 'model_switch_failed' })
    expect(chat.currentConfigurationId.value).toBe('account-a')
    expect(chat.session.value?.backend).toBe('codex')
    app.unmount()
  })

  it('keeps recorded reply execution and usage when the next-turn selection changes', async () => {
    const execution = { agentName: 'Original', backend: 'opencode', configurationId: 'old-group', modelId: 'old/model', startedAt: '2026-10-28T08:00:00Z' }
    const usage = { inputTokens: 0, outputTokens: 12, cacheReadTokens: 34, cacheWriteTokens: 0 }
    mocks.listSoloMessages.mockResolvedValue([{ id: 'old', sessionId: 's1', role: 'assistant', content: 'Recorded reply', createdAt: '2026-10-28T08:00:25Z', durationMs: 25000, execution, usage, usageByModel: { 'old/model': usage } }])
    const { chat, app } = mountChat()
    await flushPromises()
    FakeSocket.all[0]!.open()
    await flushPromises()
    mocks.setSoloSessionModel.mockResolvedValue(session({ currentModelSet: true, currentConfigurationId: 'new-group', currentModelId: 'new/model', backend: 'codex' }))
    expect(await chat.setCurrentModel(modelRouteValue('new-group', 'new/model'))).toBe(true)
    expect(chat.turns.value[0]).toMatchObject({ execution, usage, durationMs: 25000, startedAt: execution.startedAt, usageByModel: { 'old/model': usage } })
    app.unmount()
  })

  it('retains an execution snapshot from the reconnect head before turn_begin is replayed', async () => {
    const execution = { agentName: 'Actual', backend: 'opencode', configurationId: 'recorded', modelId: 'recorded/model', startedAt: '2026-10-28T08:00:00Z' }
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open(); await flushPromises()
    ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'q1', activeItem: { id: 'q1', text: 'Continue' }, items: [], execution })
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'q1', text: 'Continue' } })
    const reply = () => chat.turns.value.find(turn => turn.id === 'live:q1')
    expect(reply()?.execution).toEqual(execution)
    expect(reply()?.startedAt).toBe(execution.startedAt)
    ws.emit({ type: 'session', event: 'execution', userMsgId: 'another-turn', execution: { ...execution, modelId: 'another/model' } })
    expect(reply()?.execution?.modelId).toBe('recorded/model')
    app.unmount()
  })

  it('keeps another tab’s qualified selection when an older metadata request finishes during a turn', async () => {
    const before = session({ currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-a', backend: 'codex', reasoningEffort: 'low' })
    mocks.getSoloSession.mockResolvedValue(before)
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open(); await flushPromises()
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [] })
    let resolve!: (value: ReturnType<typeof session>) => void
    mocks.getSoloSession.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const background = chat.refreshSession()

    ws.emit({ type: 'session', event: 'model', currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-b', backend: 'opencode', reasoningEffort: 'high', reasoningEfforts: ['low', 'high'], modelNotice: '', pendingChange: 'model' })
    expect(chat.currentConfigurationId.value).toBe('account-b')
    expect(chat.currentReasoningEffort.value).toBe('high')
    expect(chat.busy.value).toBe(true)
    expect(chat.modelSwitchNotice.value).toContain('下一条')
    resolve(before)
    await background

    expect(chat.currentConfigurationId.value).toBe('account-b')
    expect(chat.currentModelId.value).toBe('same/model')
    expect(chat.session.value?.backend).toBe('opencode')
    expect(chat.currentReasoningEffort.value).toBe('high')
    expect(chat.busy.value).toBe(true)
    expect(mocks.setSoloSessionModel).not.toHaveBeenCalled()
    app.unmount()
  })

  it.each(['model', 'effort'] as const)('keeps the next-turn %s hint when its save notification arrives after HTTP success', async kind => {
    const before = session({ currentModelSet: true, currentModelId: 'aux', currentConfigurationId: 'account-a', reasoningEffort: 'low' })
    const saved = session({ ...before, ...(kind === 'model' ? { currentModelId: 'aux2' } : { reasoningEffort: 'high' }) })
    mocks.getSoloSession.mockResolvedValue(before)
    mocks.setSoloSessionModel.mockResolvedValue(saved)
    mocks.setSoloSessionReasoningEffort.mockResolvedValue(saved)
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open(); await flushPromises()
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [] })
    expect(await (kind === 'model' ? chat.setCurrentModel('aux2') : chat.setCurrentReasoningEffort('high'))).toBe(true)
    const hint = chat.modelSwitchNotice.value
    expect(hint).toContain('下一条')

    ws.emit({ type: 'session', event: 'model', currentConfigurationId: saved.currentConfigurationId, currentModelId: saved.currentModelId, reasoningEffort: saved.reasoningEffort, pendingChange: kind })
    expect(chat.modelSwitchNotice.value).toBe(hint)
    expect(chat.busy.value).toBe(true)
    ws.emit({ type: 'session', event: 'model', currentConfigurationId: saved.currentConfigurationId, currentModelId: saved.currentModelId, reasoningEffort: saved.reasoningEffort, pendingChange: '' })
    expect(chat.modelSwitchNotice.value).toBe('')
    app.unmount()
  })

})
