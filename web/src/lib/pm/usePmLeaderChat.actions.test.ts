// @vitest-environment happy-dom
import { createApp, defineComponent, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const shared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref: hoistedRef } = require('vue') as typeof import('vue')
  return { isMobile: hoistedRef(false) }
})

const mocks = vi.hoisted(() => ({
  listPmThreads: vi.fn(),
  createPmThread: vi.fn(),
  deletePmThread: vi.fn(),
  listPmMessages: vi.fn(),
  patchPmMessage: vi.fn(),
  startPmTurn: vi.fn(),
  cancelPmTurn: vi.fn(),
  pmThreadChatWsUrl: vi.fn(() => 'ws://example.test/pm'),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listPmThreads: mocks.listPmThreads,
      createPmThread: mocks.createPmThread,
      deletePmThread: mocks.deletePmThread,
      listPmMessages: mocks.listPmMessages,
      patchPmMessage: mocks.patchPmMessage,
      startPmTurn: mocks.startPmTurn,
      cancelPmTurn: mocks.cancelPmTurn,
      pmThreadChatWsUrl: mocks.pmThreadChatWsUrl,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warn: vi.fn(),
    info: vi.fn(),
  }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: shared.isMobile }),
}))

import { usePmLeaderChat } from './usePmLeaderChat'

const thread = (over: Record<string, unknown> = {}) => ({
  id: 'th-1',
  projectId: 'proj-1',
  userId: 'u1',
  agentName: 'pm',
  kind: 'pm',
  title: '会话',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...over,
})

const message = (id: string, role: 'user' | 'assistant' | 'system' = 'user', over = {}) => ({
  id,
  threadId: 'th-1',
  role,
  content: id,
  createdAt: '2026-01-01T00:00:00Z',
  ...over,
})

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: MockWebSocket[] = []
  readyState = MockWebSocket.OPEN
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  sent: string[] = []
  listeners = new Map<string, Set<EventListener>>()
  constructor(public url: string) {
    MockWebSocket.instances.push(this)
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
  addEventListener(type: string, fn: EventListener) {
    const set = this.listeners.get(type) ?? new Set()
    set.add(fn)
    this.listeners.set(type, set)
  }
  removeEventListener(type: string, fn: EventListener) {
    this.listeners.get(type)?.delete(fn)
  }
  fire(type: string) {
    for (const fn of this.listeners.get(type) ?? []) fn(new Event(type))
  }
  frame(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }
}

function withChat(over: Record<string, unknown> = {}) {
  let chat!: ReturnType<typeof usePmLeaderChat>
  const emit = vi.fn()
  const props = reactive({
    projectId: 'proj-1',
    binding: { enabled: true, agentConfigRef: 'pm', agentAvailable: true, aclNote: '' },
    ...over,
  })
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      chat = usePmLeaderChat(props as never, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { chat, app, emit, props }
}

describe('usePmLeaderChat actions', () => {
  beforeEach(() => {
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()
    shared.isMobile.value = false
    localStorage.clear()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    mocks.listPmThreads.mockResolvedValue({ items: [thread()] })
    mocks.listPmMessages.mockResolvedValue({ items: [], hasMore: false })
    mocks.createPmThread.mockResolvedValue(thread({ id: 'th-2', title: '新会话' }))
    mocks.deletePmThread.mockResolvedValue({ status: 'ok' })
    mocks.startPmTurn.mockImplementation(async (_p, _t, body) => ({
      message: message(body.retryOf || 'u-new', 'user', { content: body.content ?? 'retry', status: 'ok' }),
      waiting: 1,
    }))
    mocks.cancelPmTurn.mockResolvedValue({ ok: true })
    mocks.patchPmMessage.mockImplementation(async (_p, _t, id, patch) => message(id, 'user', patch))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('restores the stored thread and derives channel presentation', async () => {
    localStorage.setItem('pm-leader:active-thread:proj-1', 'ch')
    mocks.listPmThreads.mockResolvedValue({
      items: [
        thread(),
        thread({ id: 'ch', userId: 'feishu:c2c:peer-9', title: '', unspoken: true }),
      ],
    })
    const { chat, app } = withChat()
    await flushPromises()

    expect(chat.activeId.value).toBe('ch')
    expect(chat.activeIsChannel.value).toBe(true)
    expect(chat.channelTypeOf(chat.activeThread.value)).toBe('feishu')
    expect(chat.channelBadgeLabel(chat.activeThread.value)).toBeTruthy()
    expect(chat.channelBadgeClass(chat.activeThread.value)).toContain('cyan')
    expect(chat.channelReadonlyTitle(chat.activeThread.value)).toBeTruthy()
    expect(chat.channelReadonlyHint(chat.activeThread.value)).toBeTruthy()
    expect(chat.threadDisplayTitle(chat.activeThread.value)).toBe('feishu:c2c:peer-9')
    expect(chat.channelSourceLine(chat.activeThread.value)).toContain('peer-9')
    expect(chat.isChannelHint(message('h', 'system', { source: 'channel' }) as never)).toBe(true)

    const ev = { preventDefault: vi.fn(), stopPropagation: vi.fn(), clientX: 4, clientY: 8 }
    chat.openChannelCtx(ev as unknown as MouseEvent, chat.activeThread.value!)
    expect(chat.channelCtx.value?.threadId).toBe('ch')
    chat.openChannelDetail()
    expect(chat.channelDetailOpen.value).toBe(true)
    chat.closeChannelDetail()
    app.unmount()
  })

  it('loads earlier messages, preserves scroll position and exposes retry failure', async () => {
    mocks.listPmMessages
      .mockResolvedValueOnce({ items: [message('m2'), message('m3')], hasMore: true })
      .mockResolvedValueOnce({ items: [message('m1'), message('m2')], hasMore: false })
      .mockRejectedValueOnce(new Error('older down'))
    const { chat, app } = withChat()
    await flushPromises()
    const el = document.createElement('div')
    Object.defineProperties(el, {
      scrollTop: { value: 10, writable: true },
      scrollHeight: { value: 200, configurable: true },
      clientHeight: { value: 100, configurable: true },
    })
    chat.scroller.value = el
    await chat.loadEarlier()
    expect(chat.messages.value.map((m) => m.id)).toEqual(['m1', 'm2', 'm3'])
    expect(chat.hasMoreEarlier.value).toBe(false)

    chat.hasMoreEarlier.value = true
    await chat.loadEarlier()
    expect(chat.historyLoadFailed.value).toBe(true)
    expect(chat.historyLoading.value).toBe(false)
    chat.onScrollerScroll()
    expect(chat.stickToBottom.value).toBe(false)
    chat.scrollBottom(true)
    expect(el.scrollTop).toBe(200)
    app.unmount()
  })

  it('handles load, create, delete and localStorage failures', async () => {
    mocks.listPmThreads.mockRejectedValueOnce(new Error('threads down'))
    const { chat, app, emit } = withChat()
    await flushPromises()
    expect(mocks.toastError).toHaveBeenCalledWith('threads down')

    await chat.newThread()
    expect(chat.activeId.value).toBe('th-2')
    mocks.createPmThread.mockRejectedValueOnce(new Error('create down'))
    await chat.newThread()
    expect(mocks.toastError).toHaveBeenCalledWith('create down')

    mocks.deletePmThread.mockRejectedValueOnce(new Error('delete down'))
    await chat.removeThread('th-2')
    expect(mocks.toastError).toHaveBeenCalledWith('delete down')
    mocks.deletePmThread.mockResolvedValueOnce({})
    await chat.removeThread('th-2')
    expect(chat.activeId.value).toBe('')

    const disabled = withChat({ binding: null })
    await flushPromises()
    await disabled.chat.newThread()
    expect(disabled.emit).toHaveBeenCalledWith('openSettings')
    emit.mockClear()
    disabled.app.unmount()
    app.unmount()
  })

  it('copies assistant text and reports clipboard failures', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    const root = document.createElement('div')
    root.dataset.assistantBubble = ''
    const md = document.createElement('div')
    md.className = 'md'
    md.innerText = 'answer'
    root.appendChild(md)
    const button = document.createElement('button')
    root.appendChild(button)
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    await chat.copyAssistantText({ currentTarget: button } as unknown as Event)
    expect(writeText).toHaveBeenCalledWith('answer')
    expect(mocks.toastSuccess).toHaveBeenCalled()
    writeText.mockRejectedValueOnce(new Error('denied'))
    await chat.copyAssistantText({ currentTarget: button } as unknown as Event)
    expect(mocks.toastError).toHaveBeenCalled()
    app.unmount()
  })

  it('handles failed message loads and retries the active thread', async () => {
    mocks.listPmMessages.mockRejectedValueOnce(new Error('messages down'))
    const { chat, app } = withChat()
    await flushPromises()
    expect(chat.messagesLoadFailed.value).toBe(true)
    expect(mocks.toastError).toHaveBeenCalled()

    mocks.listPmMessages.mockResolvedValueOnce({ items: [message('ok')], hasMore: false })
    await chat.retryLoadMessages()
    expect(chat.messagesLoadFailed.value).toBe(false)
    expect(chat.messages.value[0]?.id).toBe('ok')
    chat.messagesLoading.value = true
    await chat.retryLoadMessages()
    app.unmount()
  })

  it('selects threads on mobile and honors busy navigation guards', async () => {
    shared.isMobile.value = true
    mocks.listPmThreads.mockResolvedValue({ items: [thread(), thread({ id: 'th-2' })] })
    const { chat, app, emit, props } = withChat({ restoreMobileChat: true })
    await flushPromises()
    expect(chat.mobileView.value).toBe('chat')
    expect(emit).toHaveBeenCalledWith('restoredMobileChat')
    chat.backToThreads()
    expect(chat.mobileView.value).toBe('threads')
    await chat.selectThread('th-1')
    expect(chat.mobileView.value).toBe('chat')
    await chat.selectThread('th-2')
    expect(chat.activeId.value).toBe('th-2')

    chat.sending.value = true
    chat.backToThreads()
    await chat.selectThread('th-1')
    expect(chat.activeId.value).toBe('th-2')
    chat.sending.value = false
    props.projectId = 'proj-2'
    await flushPromises()
    expect(mocks.listPmThreads).toHaveBeenCalledWith('proj-2')
    app.unmount()
  })

  it('covers computed view states and message merge helpers', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    expect(chat.mergeMessagesKeepPrefix([], [message('a') as never])).toHaveLength(1)
    expect(chat.mergeMessagesKeepPrefix([message('a') as never], [])).toHaveLength(1)
    expect(
      chat.mergeMessagesKeepPrefix(
        [message('a', 'user', { content: 'old' }) as never],
        [message('a', 'user', { content: 'new' }) as never, message('b') as never],
      ),
    ).toEqual(expect.arrayContaining([expect.objectContaining({ id: 'a', content: 'new' })]))
    chat.finalizing.value = true
    expect(chat.mainViewState.value).toBe('finalizing')
    expect(chat.busyHint.value).toBeTruthy()
    chat.finalizing.value = false
    chat.resuming.value = true
    expect(chat.mainViewState.value).toBe('resuming')
    chat.resuming.value = false
    chat.messagesLoading.value = true
    expect(chat.mainViewState.value).toBe('messagesLoading')
    chat.messagesLoading.value = false
    chat.messagesLoadFailed.value = true
    expect(chat.mainViewState.value).toBe('errorEmpty')
    expect(chat.failMeta('not-real').kind).toBe('unknown')

    for (const [kind, classPart] of [
      ['wecom', 'accent'],
      ['feishu', 'cyan'],
      ['dingtalk', 'blue'],
      ['qq', 'accent'],
    ]) {
      const th = thread({ userId: `${kind}:c2c:peer` }) as never
      expect(chat.channelBadgeLabel(th)).toBeTruthy()
      expect(chat.channelBadgeClass(th)).toContain(classPart)
      expect(chat.channelReadonlyTitle(th)).toBeTruthy()
      expect(chat.channelReadonlyHint(th)).toBeTruthy()
    }
    expect(chat.threadDisplayTitle(thread({ title: '  ' }) as never)).toBeTruthy()
    expect(chat.channelSourceLine(thread({ userId: 'qq:c2c:peer' }) as never)).toContain('peer')
    chat.messagesLoadFailed.value = false
    chat.messages.value = [message('u', 'user') as never, message('a', 'assistant') as never]
    expect(chat.showIdleSuggestions.value).toBe(true)
    chat.historyLoading.value = true
    expect(chat.historyTipText.value).toBeTruthy()
    expect(chat.historyTipClass.value).toContain('accent')
    chat.historyLoading.value = false
    chat.historyLoadFailed.value = true
    expect(chat.historyTipClass.value).toContain('err')
    chat.input.value = 'ready'
    expect(chat.canSend.value).toBe(true)
    chat.sending.value = true
    expect(chat.showStreamBubble.value).toBe(true)
    expect(chat.showStreamTypingDots.value).toBe(true)
    chat.sending.value = false
    chat.streaming.value = true
    chat.syncStreamText('working')
    expect(chat.busyHint.value).toBeTruthy()
    chat.streaming.value = false
    app.unmount()
  })

  it('queues a turn over HTTP and follows it on the thread socket', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    const socket = MockWebSocket.instances.at(-1)!
    expect(mocks.pmThreadChatWsUrl).toHaveBeenCalledWith('proj-1', 'th-1')

    chat.input.value = '进度？'
    await chat.send()
    await flushPromises()
    expect(mocks.startPmTurn).toHaveBeenCalledWith('proj-1', 'th-1', { content: '进度？', images: undefined })
    expect(chat.messages.value.map((m) => m.id)).toEqual(['u-new'])
    expect(chat.sending.value).toBe(true)
    expect(socket.sent).toEqual([])

    socket.frame({ type: 'session', event: 'phase', phase: 'pulling' })
    expect(chat.sandboxBootStatus.value).toBe('pulling')
    expect(chat.isPullingBoot.value).toBe(true)
    socket.frame({ type: 'session', event: 'turn_begin', userMsgId: 'u-new' })
    expect(chat.sending.value).toBe(false)
    expect(chat.streaming.value).toBe(true)
    socket.frame({
      type: 'acp',
      data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: '好' } } },
    })
    socket.frame({ type: 'acp' })
    socket.frame('not json{')
    socket.onmessage?.(new MessageEvent('message', { data: 'not json{' }))
    expect(chat.streamText.value).toBe('好')

    mocks.listPmMessages.mockResolvedValue({ items: [message('u-new'), message('a1', 'assistant')], hasMore: false })
    socket.frame({ type: 'session', event: 'turn_done', interrupted: false, userMsgId: 'u-new' })
    await flushPromises()
    expect(chat.streaming.value).toBe(false)
    expect(chat.finalizing.value).toBe(false)
    expect(chat.messages.value.map((m) => m.id)).toEqual(['u-new', 'a1'])
    app.unmount()
  })

  it('restores input and refreshes messages when the server rejects a turn', async () => {
    mocks.startPmTurn.mockRejectedValueOnce(new Error('PM 会话排队已满，请稍候'))
    const { chat, app } = withChat()
    await flushPromises()
    chat.input.value = '再问'
    mocks.listPmMessages.mockResolvedValue({
      items: [message('u-x', 'user', { status: 'failed', failKind: 'unknown' })],
      hasMore: false,
    })
    await chat.send()
    await flushPromises()
    expect(chat.input.value).toBe('再问')
    expect(chat.sending.value).toBe(false)
    expect(mocks.toastError).toHaveBeenCalledWith('PM 会话排队已满，请稍候')
    expect(chat.messages.value[0]?.failKind).toBe('unknown')
    app.unmount()
  })

  it('blocks sends while busy, on channel threads and when PM is unavailable', async () => {
    const { chat, app, emit, props } = withChat()
    await flushPromises()
    chat.sending.value = true
    chat.input.value = 'x'
    await chat.send()
    chat.sending.value = false
    expect(mocks.startPmTurn).not.toHaveBeenCalled()

    props.binding = { ...props.binding, enabled: false }
    await chat.send('建议')
    expect(emit).toHaveBeenCalledWith('openSettings')
    expect(mocks.startPmTurn).not.toHaveBeenCalled()
    app.unmount()
  })

  it('pulls in turns started elsewhere and settles error frames', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    const socket = MockWebSocket.instances.at(-1)!
    mocks.listPmMessages.mockResolvedValue({ items: [message('u-im')], hasMore: false })
    socket.frame({ type: 'session', event: 'queue_state', busy: true, waiting: 0, userMsgId: 'u-im', phase: 'running' })
    await flushPromises()
    expect(chat.activeUserMessageId.value).toBe('u-im')
    expect(chat.resuming.value).toBe(true)
    expect(chat.messages.value.map((m) => m.id)).toEqual(['u-im'])

    socket.frame({
      type: 'acp',
      data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: '半' } } },
    })
    socket.frame({ type: 'session', event: 'error', failKind: 'bogus', message: 'acp closed', userMsgId: 'u-im' })
    await flushPromises()
    expect(chat.streaming.value).toBe(false)
    expect(chat.failedPartialByUserMsgId.value['u-im']).toBe('半')
    expect(chat.messages.value[0]?.status).toBe('failed')
    expect(chat.messages.value[0]?.failKind).toBe('unknown')
    expect(mocks.toastError).toHaveBeenCalledWith('acp closed')
    expect(mocks.patchPmMessage).not.toHaveBeenCalled()
    app.unmount()
  })

  it('reconnects with backoff after a drop and stops following on unmount', async () => {
    vi.useFakeTimers()
    const { chat, app } = withChat()
    await vi.runOnlyPendingTimersAsync()
    await flushPromises()
    expect(MockWebSocket.instances).toHaveLength(1)
    MockWebSocket.instances[0]!.onclose?.(new CloseEvent('close'))
    expect(MockWebSocket.instances).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockWebSocket.instances).toHaveLength(2)
    MockWebSocket.instances[1]!.onclose?.(new CloseEvent('close'))
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockWebSocket.instances).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockWebSocket.instances).toHaveLength(3)

    expect(chat.busy.value).toBe(false)
    app.unmount()
    MockWebSocket.instances[2]!.onclose?.(new CloseEvent('close'))
    await vi.advanceTimersByTimeAsync(30_000)
    expect(MockWebSocket.instances).toHaveLength(3)
  })

  it('does not subscribe channel threads', async () => {
    mocks.listPmThreads.mockResolvedValue({ items: [thread({ id: 'ch', userId: 'feishu:c2c:peer-9' })] })
    const { app } = withChat()
    await flushPromises()
    expect(MockWebSocket.instances).toHaveLength(0)
    app.unmount()
  })

  it('stop cancels on the server and retry re-queues the failed message', async () => {
    mocks.listPmMessages.mockResolvedValue({
      items: [message('u1', 'user', { status: 'failed', failKind: 'sandbox' })],
      hasMore: false,
    })
    const { chat, app } = withChat()
    await flushPromises()

    await chat.stop()
    expect(mocks.cancelPmTurn).not.toHaveBeenCalled()

    await chat.retryTurn('missing')
    expect(mocks.startPmTurn).not.toHaveBeenCalled()
    await chat.retryTurn('u1')
    expect(mocks.startPmTurn).toHaveBeenCalledWith('proj-1', 'th-1', { retryOf: 'u1' })
    expect(chat.messages.value[0]?.status).toBe('ok')

    mocks.cancelPmTurn.mockRejectedValueOnce(new Error('cancel failed'))
    await chat.stop()
    expect(mocks.cancelPmTurn).toHaveBeenCalledWith('proj-1', 'th-1')
    expect(mocks.toastError).toHaveBeenCalledWith('cancel failed')

    chat.sending.value = false
    mocks.startPmTurn.mockRejectedValueOnce(new Error('该消息已在处理中'))
    await chat.retryTurn('u1')
    expect(chat.messages.value[0]?.status).toBe('ok')
    expect(chat.sending.value).toBe(false)
    app.unmount()
  })

  it('reports refetch failures after turn_done and retries them', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    const socket = MockWebSocket.instances.at(-1)!
    socket.frame({ type: 'session', event: 'turn_begin', userMsgId: 'u1' })
    mocks.listPmMessages.mockRejectedValueOnce(new Error('down'))
    socket.frame({ type: 'session', event: 'turn_done', interrupted: false })
    await flushPromises()
    expect(chat.finalizingRefetchFailed.value).toBe(true)
    expect(chat.finalizing.value).toBe(true)
    socket.frame({ type: 'session', event: 'turn_done', interrupted: false })
    await chat.refetchAfterTurnDone()
    await flushPromises()
    expect(chat.finalizing.value).toBe(false)
    app.unmount()
  })
})
