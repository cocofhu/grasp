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
  reorderSoloQueue: vi.fn(),
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

  it.each(['turn_done', 'error'] as const)('rejects an older busy queue snapshot after %s', async (event) => {
    const { chat, app } = mountChat()
    await flushPromises()
    const socket = FakeSocket.all[0]!
    socket.open()
    await flushPromises()
    const item = { id: 'active', text: 'active', images: [] }
    socket.emit({ type: 'session', event: 'queue_state', seq: 0, sessionGeneration: 7, revision: 2, busy: true, activeItem: item, items: [] })
    socket.emit({ type: 'session', event, seq: 1, sessionGeneration: 7, revision: 4, message: 'stopped' })
    await flushPromises()
    expect(chat.busy.value).toBe(false)
    // Cancel captured busy before the native cancellation completed the turn.
    socket.emit({ type: 'session', event: 'queue_state', seq: 2, sessionGeneration: 7, revision: 3, busy: true, activeItem: item, items: [] })
    expect(chat.busy.value).toBe(false)
    expect(chat.queued.value).toEqual([])
    expect(chat.turns.value.some(turn => turn.streaming)).toBe(false)
    app.unmount()
  })

  describe('quiet busy turn', () => {
    const item = { id: 'active', text: 'active', images: [] }
    async function busyChat() {
      vi.useFakeTimers()
      const mounted = mountChat()
      await flushPromises()
      const socket = FakeSocket.all[0]!
      socket.open()
      await flushPromises()
      socket.emit({ type: 'session', event: 'queue_state', seq: 0, sessionGeneration: 1, revision: 1, busy: true, activeItem: item, items: [] })
      await flushPromises()
      expect(mounted.chat.busy.value).toBe(true)
      mocks.getSoloSession.mockClear()
      return { ...mounted, socket }
    }
    afterEach(() => vi.useRealTimers())

    it('resubscribes when the server is idle after a missed turn_done', async () => {
      const { chat, app, socket } = await busyChat()
      mocks.getSoloSession.mockResolvedValue(session({ busy: false }))
      await vi.advanceTimersByTimeAsync(30_000)
      expect(mocks.getSoloSession).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(1500)
      const next = FakeSocket.all.at(-1)!
      expect(next).not.toBe(socket)
      next.open()
      await flushPromises()
      next.emit({ type: 'session', event: 'queue_state', seq: 2, sessionGeneration: 1, revision: 3, busy: false, items: [] })
      await flushPromises()
      expect(chat.busy.value).toBe(false)
      expect(chat.turns.value.some(turn => turn.streaming)).toBe(false)
      app.unmount()
    })

    it('keeps a long silent turn on its socket while the server is still busy', async () => {
      const { chat, app } = await busyChat()
      mocks.getSoloSession.mockResolvedValue(session({ busy: true }))
      await vi.advanceTimersByTimeAsync(30_000)
      await vi.advanceTimersByTimeAsync(30_000)
      expect(mocks.getSoloSession).toHaveBeenCalledTimes(2)
      expect(FakeSocket.all).toHaveLength(1)
      expect(chat.busy.value).toBe(true)
      app.unmount()
    })

    it('discards an idle read when a live frame arrives during it', async () => {
      const { chat, app, socket } = await busyChat()
      let resolve!: (row: unknown) => void
      mocks.getSoloSession.mockReturnValueOnce(new Promise(r => { resolve = r }))
      await vi.advanceTimersByTimeAsync(30_000)
      socket.emit({ ...acp('still working'), seq: 1 })
      resolve(session({ busy: false }))
      await vi.advanceTimersByTimeAsync(1500)
      expect(FakeSocket.all).toHaveLength(1)
      expect(chat.busy.value).toBe(true)
      app.unmount()
    })
  })

  it('rebases state and replay sequence on a new subscription to a recreated server stream', async () => {
    vi.useFakeTimers()
    const { chat, app } = mountChat()
    try {
      await flushPromises()
      const first = FakeSocket.all[0]!
      first.open()
      await flushPromises()
      first.emit({ type: 'session', event: 'queue_state', seq: 0, sessionGeneration: 9, revision: 20, busy: false, items: [] })
      first.emit({ type: 'session', event: 'turn_begin', seq: 1, sessionGeneration: 9, revision: 21, item: { id: 'old', text: 'old', images: [] } })
      first.emit({ ...acp('old output'), seq: 2 })
      first.emit({ type: 'session', event: 'turn_done', seq: 3, sessionGeneration: 9, revision: 22 })
      first.onclose?.()
      await vi.advanceTimersByTimeAsync(1500)
      const next = FakeSocket.all.at(-1)!
      expect(next).not.toBe(first)
      next.open()
      await flushPromises()
      const item = { id: 'new', text: 'new', images: [] }
      next.emit({ type: 'session', event: 'queue_state', seq: 0, sessionGeneration: 1, revision: 1, busy: true, activeItem: item, items: [] })
      next.emit({ type: 'session', event: 'turn_begin', seq: 1, sessionGeneration: 1, revision: 2, item })
      // This collides with the prior stream's ACP sequence number.
      next.emit({ ...acp('new output'), seq: 2 })
      expect(chat.busy.value).toBe(true)
      expect(chat.turns.value.find(turn => turn.id === 'live:new')?.text).toBe('new output')
    } finally {
      app.unmount()
      vi.useRealTimers()
    }
  })

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
    expect(chat.turns.value.some((turn) => turn.text === 'hello')).toBe(false)
    app.unmount()
  })

  it('shows an accepted message before turn_begin and keeps it when history is short', async () => {
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    mocks.sendSoloTurn.mockResolvedValue(undefined)
    const pending = chat.send('你好')
    expect(chat.turns.value.some((turn) => turn.role === 'user' && turn.text === '你好')).toBe(true)
    expect(await pending).toBe(true)

    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [] })
    ws.emit({
      type: 'session',
      event: 'turn_begin',
      item: { id: 'srv', text: '你好', images: [] },
      userMsgId: 'srv',
    })
    expect(chat.turns.value.filter((turn) => turn.role === 'user' && turn.text === '你好')).toHaveLength(1)

    mocks.listSoloMessages.mockResolvedValue([])
    ws.emit({ type: 'session', event: 'turn_done' })
    await flushPromises()
    expect(chat.turns.value.some((turn) => turn.role === 'user' && turn.text === '你好')).toBe(true)

    mocks.listSoloMessages.mockResolvedValue([
      { id: 'srv', sessionId: 's1', role: 'user', content: '你好', createdAt: '' },
      { id: 'a1', sessionId: 's1', role: 'assistant', content: '收到', status: 'ok', createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
    expect(chat.turns.value.filter((turn) => turn.role === 'user' && turn.text === '你好')).toHaveLength(1)
    expect(chat.turns.value.some((turn) => turn.text === '收到')).toBe(true)
    app.unmount()
  })

  async function openChat() {
    const context = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    await flushPromises()
    mocks.sendSoloTurn.mockResolvedValue({ waiting: 1 })
    return { ...context, ws }
  }

  it('replaces a local queued item with its server acknowledgement once', async () => {
    const { chat, app, ws } = await openChat()
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
    await chat.send('queued')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [{ id: 'server-queued', text: 'queued', images: [] }] })
    expect(chat.queued.value.map(row => row.id)).toEqual(['server-queued'])
    expect(chat.session.value?.waiting).toBe(1)
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [{ id: 'server-queued', text: 'queued', images: [] }] })
    expect(chat.queued.value.map(row => row.id)).toEqual(['server-queued'])
    expect(chat.turns.value.filter(row => row.role === 'user' && row.text === 'queued')).toHaveLength(0)
    app.unmount()
  })

  it('keeps a send behind a running turn out of the transcript while the turn streams', async () => {
    const { chat, app, ws } = await openChat()
    const first = { id: 'srv-1', text: 'first', images: [] }
    const second = { id: 'srv-2', text: 'second', images: [] }
    await chat.send('first')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [first] })
    ws.emit({ type: 'session', event: 'turn_begin', item: first, activeItem: first })
    ws.emit(acp('a1 '))
    await chat.send('second')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [second], activeItem: first, phase: 'running', userMsgId: 'srv-1' })
    ws.emit(acp('a2'))
    expect(chat.turns.value.map(row => row.id)).toEqual(['u:srv-1', 'live:srv-1'])
    expect(chat.turns.value[1]).toMatchObject({ text: 'a1 a2', streaming: true })
    expect(chat.queued.value.map(row => row.id)).toEqual(['srv-2'])
    app.unmount()
  })

  it('keeps repeated queued prompts and their replies separate before history returns', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('continue')
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'server-1', text: 'continue', images: [] } })
    ws.emit(acp('first reply'))
    await chat.send('continue')
    ws.emit({ type: 'session', event: 'turn_done' })
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'server-2', text: 'continue', images: [] } })
    ws.emit(acp('second reply'))
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.id)).toEqual(['u:server-1', 'u:server-2'])
    expect(chat.turns.value.filter(row => row.role === 'agent').map(row => row.text)).toEqual(['first reply', 'second reply'])
    // The first turn's HTTP history response may arrive while the next streams.
    await flushPromises()
    expect(chat.turns.value.filter(row => row.role === 'agent').map(row => row.text)).toEqual(['first reply', 'second reply'])
    app.unmount()
  })

  it('adopts image-only sends and preserves their order through queue and opening frames', async () => {
    const { chat, app, ws } = await openChat()
    const first = [{ mimeType: 'image/png', data: 'first-image', name: 'first.png' }]
    const second = [{ mimeType: 'image/png', data: 'second-image', name: 'second.png' }]
    await chat.send('', first)
    await chat.send('', second)
    const item1 = { id: 'image-1', text: '', images: [{ mimeType: 'image/png', ref: 'blob:first', name: 'first.png' }] }
    const item2 = { id: 'image-2', text: '', images: [{ mimeType: 'image/png', ref: 'blob:second', name: 'second.png' }] }
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [item1, item2] })
    ws.emit({ type: 'session', event: 'turn_begin', item: item1 })
    ws.emit(acp('first image reply'))
    ws.emit({ type: 'session', event: 'turn_done' })
    ws.emit({ type: 'session', event: 'turn_begin', item: item2 })
    ws.emit(acp('second image reply'))
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.id)).toEqual(['u:image-1', 'u:image-2'])
    expect(chat.turns.value.filter(row => row.role === 'agent').map(row => row.text)).toEqual(['first image reply', 'second image reply'])
    expect(chat.queued.value).toEqual([])
    app.unmount()
  })

  it('sends reference-only messages and carries references through queue and opening frames', async () => {
    const { chat, app, ws } = await openChat()
    mocks.sendSoloTurn.mockResolvedValue(undefined)
    const ref = { kind: 'github_pr' as const, target: 'acme/web#7', title: 'Fix' }
    expect(await chat.send('', [], [ref])).toBe(true)
    expect(mocks.sendSoloTurn).toHaveBeenLastCalledWith('s1', expect.objectContaining({ references: [{ kind: 'github_pr', target: 'acme/web#7' }] }))
    const pinned = { ...ref, id: 'ref-1', access: 'ok' }
    const item = { id: 'r1', text: '', references: [pinned] }
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [item] })
    ws.emit({ type: 'session', event: 'turn_begin', item })
    const user = chat.turns.value.find(row => row.role === 'user')
    expect(user?.references).toEqual([pinned])
    app.unmount()
  })

  it('keeps sends behind a running turn in the queue, in send order, until they begin', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('one')
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'server-one', text: 'one', images: [] } })
    ws.emit(acp('first answer'))
    await chat.send('two')
    await chat.send('three')
    mocks.listSoloMessages.mockResolvedValue([
      { id: 'server-one', sessionId: 's1', role: 'user', content: 'one', createdAt: '' },
      { id: 'answer-one', sessionId: 's1', role: 'assistant', content: 'first answer', createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'turn_done' })
    await flushPromises()
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.text)).toEqual(['one'])
    expect(chat.queued.value.map(row => row.text)).toEqual(['two', 'three'])
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [{ id: 'server-two', text: 'two', images: [] }, { id: 'server-three', text: 'three', images: [] }] })
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'server-two', text: 'two', images: [] } })
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.text)).toEqual(['one', 'two'])
    expect(chat.queued.value.map(row => row.text)).toEqual(['three'])
    app.unmount()
  })

  it.each(['cancel', 'remove'] as const)('cleans pending transcript rows after queue %s', async action => {
    const { chat, app, ws } = await openChat()
    mocks.removeSoloQueued.mockResolvedValue({ ok: true })
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
    ws.emit(acp('first partial reply'))
    await chat.send('do not execute')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'server-queued', text: 'do not execute', images: [] }] })
    if (action === 'cancel') await chat.cancel()
    else await chat.removeQueued('server-queued')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [] })
    ws.emit({ type: 'session', event: 'turn_done', interrupted: action === 'cancel' })
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
    expect(chat.turns.value.some(row => row.text === 'do not execute')).toBe(false)
    expect(chat.queued.value).toEqual([])
    app.unmount()
  })

  it('keeps accepted rows when the HTTP send fails after socket acknowledgement', async () => {
    const { chat, app, ws } = await openChat()
    let reject!: (error: Error) => void
    mocks.sendSoloTurn.mockImplementationOnce(() => new Promise((_, fail) => { reject = fail }))
    const pending = chat.send('accepted')
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'confirmed', text: 'accepted', images: [] } })
    ws.emit(acp('reply'))
    reject(new Error('connection lost after acceptance'))
    expect(await pending).toBe(true)
    expect(chat.turns.value.map(row => row.id)).toEqual(['u:confirmed', 'live:confirmed'])
    expect(chat.turns.value[1]?.text).toBe('reply')
    expect(chat.actionError.value).toBe('')
    app.unmount()
  })

  it.each([false, true])('ignores a pending send after navigating away and back (failure: %s)', async fails => {
    const { chat, app, id } = await openChat()
    let resolve!: (value: { waiting: number }) => void
    let reject!: (error: Error) => void
    mocks.sendSoloTurn.mockImplementationOnce(() => new Promise((done, fail) => { resolve = done; reject = fail }))
    const pending = chat.send('old draft')
    mocks.getSoloSession.mockResolvedValueOnce(session({ id: 's2' })).mockResolvedValue(session())
    id.value = 's2'
    await flushPromises()
    id.value = 's1'
    await flushPromises()
    if (fails) reject(new Error('old request failed'))
    else resolve({ waiting: 1 })
    expect(await pending).toBe(false)
    expect(chat.turns.value).toEqual([])
    expect(chat.queued.value).toEqual([])
    expect(chat.actionError.value).toBe('')
    app.unmount()
  })

  it.each([false, true])('cleans a remotely removed acknowledged queue item (idle: %s)', async idle => {
    const { chat, app, ws } = await openChat()
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
    await chat.send('removed elsewhere')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'remote-removed', text: 'removed elsewhere', images: [] }] })
    ws.emit({ type: 'session', event: 'queue_state', busy: !idle, userMsgId: idle ? '' : 'active', items: [] })
    await flushPromises()
    expect(chat.turns.value.some(row => row.text === 'removed elsewhere')).toBe(false)
    expect(chat.queued.value).toEqual([])
    app.unmount()
  })

  it('reconciles a completed queued turn when its opening and terminal frames were missed', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('completed while disconnected')
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [{ id: 'completed', text: 'completed while disconnected', images: [] }] })
    mocks.listSoloMessages.mockResolvedValue([
      { id: 'completed', sessionId: 's1', role: 'user', content: 'completed while disconnected', createdAt: '' },
      { id: 'answer', sessionId: 's1', role: 'assistant', content: 'saved reply', createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
    expect(chat.turns.value.map(row => row.id)).toEqual(['u:completed', 'a:answer'])
    app.unmount()
  })

  it('keeps an active remote turn before local pending sends after a history refresh', async () => {
    const { chat, app, ws } = await openChat()
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'remote-active', text: 'first', images: [] } })
    ws.emit(acp('first partial reply'))
    await chat.send('later')
    ws.onclose?.()
    await flushPromises()
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.text)).toEqual(['first'])
    expect(chat.turns.value[1]?.text).toBe('first partial reply')
    expect(chat.queued.value.map(row => row.text)).toEqual(['later'])
    app.unmount()
  })

  it('preserves live tool output until persisted history contains it', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('run tool')
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'tools', text: 'run tool', images: [] } })
    ws.emit(acp('reply'))
    const tool = { op: 'event', data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'tool-1', title: 'Read', status: 'completed', rawOutput: 'important result' } } }
    ws.emit({ type: 'acp', data: tool })
    mocks.listSoloMessages.mockResolvedValue([
      { id: 'tools', sessionId: 's1', role: 'user', content: 'run tool', createdAt: '' },
      { id: 'answer', sessionId: 's1', role: 'assistant', content: 'reply', createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'turn_done' })
    await flushPromises()
    expect(chat.turns.value.filter(row => row.role === 'agent')).toHaveLength(1)
    expect(chat.turns.value[1]?.tools[0]?.output).toBe('important result')
    mocks.listSoloMessages.mockResolvedValue([
      { id: 'tools', sessionId: 's1', role: 'user', content: 'run tool', createdAt: '' },
      { id: 'answer', sessionId: 's1', role: 'assistant', content: 'reply', events: [acp('reply').data, tool], createdAt: '' },
    ])
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
    expect(chat.turns.value.map(row => row.id)).toEqual(['u:tools', 'a:answer'])
    expect(chat.turns.value[1]?.tools[0]?.output).toBe('important result')
    app.unmount()
  })

  it('keeps already streamed output when cancel arrives before the opening frame', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('started without opening frame')
    ws.emit(acp('partial reply'))
    await chat.cancel()
    ws.emit({ type: 'session', event: 'turn_done', interrupted: true })
    await flushPromises()
    expect(chat.turns.value.some(row => row.text === 'started without opening frame')).toBe(true)
    expect(chat.turns.value.find(row => row.role === 'agent')?.text).toBe('partial reply')
    app.unmount()
  })

  it('loads old message identities before accepting a same-text send', async () => {
    let releaseHistory!: (messages: unknown[]) => void
    mocks.listSoloMessages.mockImplementationOnce(() => new Promise(resolve => { releaseHistory = resolve }))
    const { chat, app } = mountChat()
    await flushPromises()
    const ws = FakeSocket.all[0]!
    ws.open()
    expect(chat.canSend.value).toBe(false)
    expect(await chat.send('continue')).toBe(false)
    expect(mocks.sendSoloTurn).not.toHaveBeenCalled()
    releaseHistory([
      { id: 'old-user', sessionId: 's1', role: 'user', content: 'continue', createdAt: '' },
      { id: 'old-answer', sessionId: 's1', role: 'assistant', content: 'old reply', createdAt: '' },
    ])
    await flushPromises()
    expect(chat.canSend.value).toBe(true)
    await chat.send('continue')
    ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'new-user', text: 'continue', images: [] } })
    ws.emit(acp('new reply'))
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.id)).toEqual(['u:old-user', 'u:new-user'])
    expect(chat.turns.value.filter(row => row.role === 'agent').map(row => row.text)).toEqual(['old reply', 'new reply'])
    app.unmount()
  })

  it('retries a failed initial history read before enabling the composer', async () => {
    vi.useFakeTimers()
    mocks.listSoloMessages.mockRejectedValueOnce(new Error('temporary history failure')).mockResolvedValue([])
    const { chat, app } = mountChat()
    await flushPromises()
    FakeSocket.all[0]!.open()
    await flushPromises()
    expect(chat.canSend.value).toBe(false)
    await vi.advanceTimersByTimeAsync(2500)
    expect(chat.canSend.value).toBe(true)
    expect(mocks.listSoloMessages).toHaveBeenCalledTimes(2)
    app.unmount()
    vi.useRealTimers()
  })

  it('adopts the active repeated prompt before its queued twin on a resume snapshot', async () => {
    const { chat, app, ws } = await openChat()
    await chat.send('continue')
    await chat.send('continue')
    ws.emit({
      type: 'session', event: 'queue_state', busy: true,
      activeItem: { id: 'active-same', text: 'continue', images: [] },
      items: [{ id: 'queued-same', text: 'continue', images: [] }],
    })
    ws.emit(acp('first reply'))
    expect(chat.turns.value.filter(row => row.role === 'user').map(row => row.id)).toEqual(['u:active-same'])
    expect(chat.turns.value[1]?.text).toBe('first reply')
    expect(chat.queued.value.map(row => row.id)).toEqual(['queued-same'])
    app.unmount()
  })

  it('restores an active item ahead of queued items when turn_begin is outside replay', async () => {
    const { chat, app, ws } = await openChat()
    ws.emit({
      type: 'session', event: 'queue_state', busy: true,
      activeItem: { id: 'active-resumed', text: 'first', images: [] },
      items: [{ id: 'later', text: 'second', images: [] }],
    })
    ws.emit(acp('first reply'))
    expect(chat.turns.value.map(row => row.id)).toEqual(['u:active-resumed', 'live:active-resumed'])
    expect(chat.turns.value[1]?.text).toBe('first reply')
    expect(chat.queued.value.map(row => row.id)).toEqual(['later'])
    app.unmount()
  })

  it('adopts blob-in-data image acknowledgements without replacing the inline preview', async () => {
    const { chat, app, ws } = await openChat()
    const images = [{ mimeType: 'image/png', data: 'aW1hZ2UtYnl0ZXM=', name: 'preview.png' }]
    await chat.send('', images)
    const item = { id: 'blob-in-data', text: '', images: [{ mimeType: 'image/png', data: 'blob:waiting-asset', name: 'preview.png' }] }
    ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [item] })
    expect(chat.queued.value.map(row => row.id)).toEqual(['blob-in-data'])
    ws.emit({ type: 'session', event: 'turn_begin', item })
    const users = chat.turns.value.filter(row => row.role === 'user')
    expect(users).toHaveLength(1)
    expect(users[0]?.id).toBe('u:blob-in-data')
    expect(users[0]?.images).toEqual(images)
    expect(chat.queued.value).toEqual([])
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
    expect(await pending).toBe(true)
    expect(mocks.setSoloSessionModel).toHaveBeenCalledWith('s1', 'aux2')

    ws.emit({ type: 'session', event: 'turn_done', interrupted: false })
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
    await flushPromises()
    expect(mocks.setSoloSessionReasoningEffort).toHaveBeenCalledWith('s1', 'high')
    expect(mocks.setSoloSessionModel).not.toHaveBeenCalled()
    resolve(session({ currentModelSet: true, currentModelId: 'aux', reasoningEffort: 'high', reasoningEfforts: ['low', 'high'] }))
    expect(await pending).toBe(true)
    expect(chat.currentModelId.value).toBe('aux')
    ws.emit({ type: 'session', event: 'queue_state', busy: false, items: [] })
    await flushPromises()
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

  it('writes the new model highest effort when a switch clears the saved level', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'old', reasoningEffort: 'low', reasoningEfforts: ['low', 'high'] }))
    mocks.setSoloSessionModel.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'gemini-4-argon', reasoningEffort: '', reasoningEfforts: ['low', 'medium', 'high'] }))
    mocks.setSoloSessionReasoningEffort.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'gemini-4-argon', reasoningEffort: 'high', reasoningEfforts: ['low', 'medium', 'high'] }))
    const { chat, app } = mountChat()
    await flushPromises()
    expect(await chat.setCurrentModel('gemini-4-argon')).toBe(true)
    expect(mocks.setSoloSessionModel).toHaveBeenCalledWith('s1', 'gemini-4-argon')
    expect(mocks.setSoloSessionReasoningEffort).toHaveBeenCalledWith('s1', 'high')
    expect(chat.currentReasoningEffort.value).toBe('high')
    app.unmount()
  })

  it('does not raise a lower effort that the new model still accepts', async () => {
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'old', reasoningEffort: 'low', reasoningEfforts: ['low', 'medium', 'high'] }))
    mocks.setSoloSessionModel.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'gemini-4-argon', reasoningEffort: 'low', reasoningEfforts: ['low', 'medium', 'high'] }))
    const { chat, app } = mountChat()
    await flushPromises()
    expect(await chat.setCurrentModel('gemini-4-argon')).toBe(true)
    expect(mocks.setSoloSessionReasoningEffort).not.toHaveBeenCalled()
    expect(chat.currentReasoningEffort.value).toBe('low')
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
    ws.emit({ type: 'session', event: 'turn_begin', seq: 1, sessionGeneration: 3, revision: 2, item: { id: 'q1', text: 'hi', images: [] } })
    ws.emit({ ...acp('Hello'), seq: 2 })
    expect(chat.turns.value[1]!.text).toBe('Hello')

    ws.onclose?.()
    await vi.advanceTimersByTimeAsync(1500)
    const again = FakeSocket.all.at(-1)!
    expect(again).not.toBe(ws)
    expect(again.url).toContain('after=2')
    again.open()
    await flushPromises()
    again.emit({ type: 'session', event: 'queue_state', seq: 2, sessionGeneration: 3, revision: 2, busy: true, items: [], activeItem: { id: 'q1', text: 'hi', images: [] } })
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

  it('allows a qualified model choice while initial history still blocks sending', async () => {
    let releaseHistory!: (messages: unknown[]) => void
    mocks.listSoloMessages.mockImplementationOnce(() => new Promise(resolve => { releaseHistory = resolve }))
    mocks.getSoloSession.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-a', backend: 'codex' }))
    mocks.setSoloSessionModel.mockResolvedValue(session({ currentModelSet: true, currentModelId: 'same/model', currentConfigurationId: 'account-b', backend: 'opencode' }))
    const { chat, app } = mountChat()
    await flushPromises()
    FakeSocket.all[0]!.open()
    await flushPromises()

    expect(chat.canSend.value).toBe(false)
    expect(await chat.setCurrentModel(modelRouteValue('account-b', 'same/model'))).toBe(true)
    expect(mocks.setSoloSessionModel).toHaveBeenCalledWith('s1', 'same/model', undefined, 'account-b')
    expect(chat.currentConfigurationId.value).toBe('account-b')
    expect(chat.session.value?.backend).toBe('opencode')
    expect(await chat.send('continue')).toBe(false)
    expect(mocks.sendSoloTurn).not.toHaveBeenCalled()

    releaseHistory([])
    await flushPromises()
    expect(chat.canSend.value).toBe(true)
    expect(chat.currentConfigurationId.value).toBe('account-b')
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

  it.each(['model', 'effort'] as const)('keeps the saved %s selection when its notification arrives during the current turn', async kind => {
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

    ws.emit({ type: 'session', event: 'model', currentConfigurationId: saved.currentConfigurationId, currentModelId: saved.currentModelId, reasoningEffort: saved.reasoningEffort, pendingChange: kind })
    expect(chat.currentModelId.value).toBe(saved.currentModelId)
    expect(chat.currentReasoningEffort.value).toBe(saved.reasoningEffort)
    expect(chat.busy.value).toBe(true)
    ws.emit({ type: 'session', event: 'model', currentConfigurationId: saved.currentConfigurationId, currentModelId: saved.currentModelId, reasoningEffort: saved.reasoningEffort, pendingChange: '' })
    app.unmount()
  })

  describe('queue reorder and withdraw', () => {
    const qa = { id: 'qa', text: 'alpha', images: [] }
    const qb = { id: 'qb', text: 'beta', images: [] }
    const qc = { id: 'qc', text: 'gamma', images: [] }
    const shownIds = (chat: ReturnType<typeof useSoloChat>) => chat.displayQueued.value.map(row => row.id)
    function pending<T = unknown>() {
      let resolve!: (value: T) => void
      let reject!: (error: Error) => void
      const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
      return { promise, resolve, reject }
    }

    it('attributes output before turn_begin to the server queue head while a drag is unconfirmed', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      const put = pending()
      mocks.reorderSoloQueue.mockReturnValueOnce(put.promise)
      void chat.reorderQueued(1, 0)
      expect(mocks.reorderSoloQueue).toHaveBeenCalledWith('s1', ['qb', 'qa'])
      expect(shownIds(chat)).toEqual(['qb', 'qa'])
      expect(chat.queued.value.map(row => row.id)).toEqual(['qa', 'qb'])
      expect(chat.session.value?.waiting).toBe(2)
      // Queued sends stay out of the transcript until they begin (#1054).
      expect(chat.turns.value.filter(row => row.role === 'user')).toEqual([])
      ws.emit(acp('reply'))
      expect(chat.turns.value.find(row => row.id === 'live:qa')?.text).toBe('reply')
      expect(chat.turns.value.some(row => row.id === 'live:qb' || row.id === 'u:qb')).toBe(false)
      put.resolve({ ok: true })
      await flushPromises()
      app.unmount()
    })

    it('shows the latest queue_state, not a pre-drag snapshot, when the reorder fails', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb, qc] })
      const put = pending()
      mocks.reorderSoloQueue.mockReturnValueOnce(put.promise)
      const reorder = chat.reorderQueued(2, 0)
      expect(shownIds(chat)).toEqual(['qc', 'qa', 'qb'])
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qb, qc] })
      put.reject(new Error('item count mismatch'))
      await reorder
      expect(shownIds(chat)).toEqual(['qb', 'qc'])
      expect(chat.queued.value.map(row => row.id)).toEqual(['qb', 'qc'])
      expect(chat.actionError.value).toBe('')
      app.unmount()
    })

    it('drops the display order when the reorder fails without a newer frame', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      mocks.reorderSoloQueue.mockRejectedValueOnce(new Error('item count mismatch'))
      await chat.reorderQueued(1, 0)
      expect(shownIds(chat)).toEqual(['qa', 'qb'])
      app.unmount()
    })

    it('lets every queue_state replace the display order', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      mocks.reorderSoloQueue.mockResolvedValue({ ok: true })
      await chat.reorderQueued(1, 0)
      expect(shownIds(chat)).toEqual(['qb', 'qa'])
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qb, qa] })
      expect(shownIds(chat)).toEqual(['qb', 'qa'])
      // Another viewer moved them back.
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      expect(shownIds(chat)).toEqual(['qa', 'qb'])
      app.unmount()
    })

    it('keeps unacknowledged local sends at the tail and out of the submitted order', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [qa, qb] })
      mocks.sendSoloTurn.mockReturnValueOnce(new Promise(() => {}))
      void chat.send('local text')
      const local = chat.displayQueued.value[2]!
      expect(local.local).toBe(true)
      mocks.reorderSoloQueue.mockResolvedValue({ ok: true })
      await chat.reorderQueued(2, 0)
      await chat.reorderQueued(0, 2)
      expect(mocks.reorderSoloQueue).not.toHaveBeenCalled()
      await chat.reorderQueued(1, 0)
      expect(mocks.reorderSoloQueue).toHaveBeenCalledWith('s1', ['qb', 'qa'])
      expect(shownIds(chat)).toEqual(['qb', 'qa', local.id])
      expect(chat.session.value?.waiting).toBe(3)
      app.unmount()
    })

    it('withdraws once when the removal queue_state precedes the HTTP response', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
      const image = { mimeType: 'image/png', data: 'img', name: 'a.png' }
      const ref = { kind: 'github_issue' as const, target: 'acme/web#3', title: 'Bug' }
      await chat.send('edit me', [image], [ref])
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'srv-e', text: 'edit me', images: [image], references: [ref] }] })
      const del = pending()
      mocks.removeSoloQueued.mockReturnValueOnce(del.promise)
      const taking = chat.takeQueued('srv-e')
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [] })
      await flushPromises()
      del.resolve({ ok: true })
      expect(await taking).toEqual({ id: 'srv-e', text: 'edit me', images: [image], references: [ref] })
      expect(mocks.removeSoloQueued).toHaveBeenCalledTimes(1)
      expect(chat.queued.value).toEqual([])
      expect(chat.turns.value.some(row => row.text === 'edit me')).toBe(false)
      expect(chat.actionError.value).toBe('')
      app.unmount()
    })

    it('does not hand back an item that began while it was being withdrawn', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      const del = pending()
      mocks.removeSoloQueued.mockReturnValueOnce(del.promise)
      const taking = chat.takeQueued('qa')
      expect(shownIds(chat)).toEqual(['qb'])
      ws.emit({ type: 'session', event: 'turn_begin', item: qa })
      ws.emit(acp('started'))
      del.reject(new Error('cannot remove active item'))
      expect(await taking).toBeNull()
      expect(chat.queued.value.map(row => row.id)).toEqual(['qb'])
      expect(chat.turns.value.map(row => row.id)).toEqual(['u:qa', 'live:qa'])
      expect(chat.turns.value[1]?.text).toBe('started')
      app.unmount()
    })

    it('puts a failed withdrawal back in place and returns nothing', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      mocks.removeSoloQueued.mockRejectedValueOnce(new Error('排队消息不存在'))
      expect(await chat.takeQueued('qa')).toBeNull()
      expect(chat.queued.value.map(row => row.id)).toEqual(['qa', 'qb'])
      expect(await chat.takeQueued('missing')).toBeNull()
      expect(mocks.removeSoloQueued).toHaveBeenCalledTimes(1)
      app.unmount()
    })

    it('never binds a resend of withdrawn content to the old server message', async () => {
      const { chat, app, ws } = await openChat()
      ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'active', text: 'first', images: [] } })
      await chat.send('again')
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'srv-old', text: 'again', images: [] }] })
      mocks.removeSoloQueued.mockResolvedValue({ ok: true })
      const taken = await chat.takeQueued('srv-old')
      expect(taken?.text).toBe('again')
      await chat.send(taken!.text, taken!.images, taken!.references)
      // A late frame still naming the withdrawn item must not acknowledge the resend.
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'srv-old', text: 'again', images: [] }] })
      ws.emit({ type: 'session', event: 'queue_state', busy: true, userMsgId: 'active', items: [{ id: 'srv-new', text: 'again', images: [] }] })
      expect(chat.queued.value.map(row => row.id)).toEqual(['srv-new'])
      ws.emit({ type: 'session', event: 'turn_done' })
      ws.emit({ type: 'session', event: 'turn_begin', item: { id: 'srv-new', text: 'again', images: [] } })
      ws.emit(acp('new reply'))
      const users = chat.turns.value.filter(row => row.role === 'user' && row.text === 'again')
      expect(users.map(row => row.id)).toEqual(['u:srv-new'])
      expect(chat.turns.value.find(row => row.id === 'live:srv-new')?.text).toBe('new reply')
      app.unmount()
    })

    it('ignores reorder and withdrawal results from a session the user left', async () => {
      const { chat, app, id } = await openChat()
      FakeSocket.all[0]!.emit({ type: 'session', event: 'queue_state', busy: true, items: [qa, qb] })
      const put = pending()
      const del = pending()
      mocks.reorderSoloQueue.mockReturnValueOnce(put.promise)
      mocks.removeSoloQueued.mockReturnValueOnce(del.promise)
      void chat.reorderQueued(1, 0)
      const taking = chat.takeQueued('qa')
      mocks.getSoloSession.mockResolvedValue(session({ id: 's2' }))
      id.value = 's2'
      await flushPromises()
      const next = FakeSocket.all.at(-1)!
      next.open()
      await flushPromises()
      next.emit({ type: 'session', event: 'queue_state', busy: true, items: [{ id: 'x1', text: 'one', images: [] }, { id: 'x2', text: 'two', images: [] }] })
      mocks.reorderSoloQueue.mockReturnValueOnce(new Promise(() => {}))
      void chat.reorderQueued(1, 0)
      put.reject(new Error('old session'))
      del.resolve({ ok: true })
      expect(await taking).toBeNull()
      await flushPromises()
      expect(shownIds(chat)).toEqual(['x2', 'x1'])
      expect(chat.queued.value.map(row => row.id)).toEqual(['x1', 'x2'])
      app.unmount()
    })
  })
})
