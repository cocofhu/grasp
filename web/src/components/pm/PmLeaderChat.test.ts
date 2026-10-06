// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { extractAgentMessageDelta } from '@/lib/run/acpUnpack'
import PmLeaderChat from './PmLeaderChat.vue'

const apiMocks = vi.hoisted(() => ({
  listPmThreads: vi.fn(),
  createPmThread: vi.fn(),
  listPmMessages: vi.fn(),
  startPmTurn: vi.fn(),
  cancelPmTurn: vi.fn(),
  sandboxChatWsUrl: vi.fn(() => 'ws://test/sandbox/1'),
  pmThreadChatWsUrl: vi.fn(() => 'ws://test/pm/thr-new/chat'),
  patchPmMessage: vi.fn(),
  deletePmThread: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listPmThreads: apiMocks.listPmThreads,
      createPmThread: apiMocks.createPmThread,
      listPmMessages: apiMocks.listPmMessages,
      startPmTurn: apiMocks.startPmTurn,
      cancelPmTurn: apiMocks.cancelPmTurn,
      sandboxChatWsUrl: apiMocks.sandboxChatWsUrl,
      pmThreadChatWsUrl: apiMocks.pmThreadChatWsUrl,
      patchPmMessage: apiMocks.patchPmMessage,
      deletePmThread: apiMocks.deletePmThread,
    },
  }
})

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: MockWebSocket[] = []
  constructor() {
    MockWebSocket.instances.push(this)
  }
  readyState = MockWebSocket.OPEN
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  send = vi.fn()
  close = vi.fn()
  addEventListener = vi.fn()
  removeEventListener = vi.fn()
}

function lastSocket(): MockWebSocket {
  const socket = MockWebSocket.instances[MockWebSocket.instances.length - 1]
  if (!socket) throw new Error('no websocket opened')
  return socket
}

function serverFrame(socket: MockWebSocket, frame: unknown) {
  socket.onmessage?.(new MessageEvent('message', { data: JSON.stringify(frame) }))
}

function chunkFrame(text: string) {
  return {
    type: 'acp',
    data: {
      op: 'event',
      data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text } } },
    },
  }
}

function mountChat(
  binding: {
    enabled: boolean
    agentAvailable: boolean
    agentConfigRef: string
    aclNote: string
  } = {
    enabled: true,
    agentAvailable: true,
    agentConfigRef: 'agent-1',
    aclNote: '',
  },
) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(PmLeaderChat, {
    props: {
      projectId: 'proj-1',
      binding,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, CitationCard: true },
    },
  })
}

describe('PmLeaderChat first-send without thread', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
    apiMocks.createPmThread.mockResolvedValue({ id: 'thr-new', title: '新会话' })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    apiMocks.startPmTurn.mockImplementation(async (_pid: string, _tid: string, body: { content?: string }) => ({
      message: { id: 'msg-u1', role: 'user', content: body.content || '', status: 'ok' },
      waiting: 1,
    }))
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('creates a thread and appends the first user message (no silent abort)', async () => {
    const wrapper = mountChat()
    await flushPromises()

    const suggestion = wrapper.findAll('button').find((b) => b.text().includes('项目整体进度如何？'))
    expect(suggestion).toBeTruthy()
    await suggestion!.trigger('click')
    await flushPromises()

    expect(apiMocks.createPmThread).toHaveBeenCalledWith('proj-1')
    expect(apiMocks.startPmTurn).toHaveBeenCalledWith(
      'proj-1',
      'thr-new',
      expect.objectContaining({
        content: '项目整体进度如何？',
      }),
    )
    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(true)
    expect(wrapper.find('.typing-dots').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('正在生成')
    expect(wrapper.text()).toContain('项目整体进度如何？')
    expect(wrapper.find('[data-testid="pm-idle-suggestions"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('first send from the composer also works with no active thread', async () => {
    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    expect(textarea.exists()).toBe(true)
    await textarea.setValue('本周有哪些风险？')
    await flushPromises()

    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    expect(sendBtn).toBeTruthy()
    await sendBtn!.trigger('click')
    await flushPromises()

    expect(apiMocks.createPmThread).toHaveBeenCalledTimes(1)
    expect(apiMocks.startPmTurn).toHaveBeenCalledWith(
      'proj-1',
      'thr-new',
      expect.objectContaining({
        content: '本周有哪些风险？',
      }),
    )
    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('本周有哪些风险？')
    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('')
    wrapper.unmount()
  })

  it('disables new-thread and other-thread switch while busy after first send', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        { id: 'thr-a', title: '会话A' },
        { id: 'thr-b', title: '会话B' },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })

    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('忙态禁切换')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    const newBtn = wrapper.findAll('button').find((b) => b.text() === '新建')
    expect(newBtn).toBeTruthy()
    expect(newBtn!.attributes('disabled')).toBeDefined()

    const btnA = wrapper.findAll('aside button').find((b) => b.text().includes('会话A'))
    const btnB = wrapper.findAll('aside button').find((b) => b.text().includes('会话B'))
    expect(btnA).toBeTruthy()
    expect(btnB).toBeTruthy()
    expect(btnA!.attributes('disabled')).toBeUndefined()
    expect(btnB!.attributes('disabled')).toBeDefined()

    wrapper.unmount()
  })
})

describe('PmLeaderChat ACP + hydrate', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
    apiMocks.createPmThread.mockResolvedValue({ id: 'thr-new', title: '新会话' })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    apiMocks.startPmTurn.mockImplementation(async (_pid: string, _tid: string, body: { content?: string }) => ({
      message: { id: 'msg-u1', role: 'user', content: body.content || '', status: 'ok' },
      waiting: 1,
    }))
    apiMocks.patchPmMessage.mockResolvedValue({ id: 'u1', status: 'failed', failKind: 'unknown' })
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('extractAgentMessageDelta accumulates nested op:event chunks (false-empty fix)', () => {
    const nested = {
      op: 'event',
      data: {
        type: 'session_update',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: '进度正常' },
        },
      },
    }
    expect(extractAgentMessageDelta(nested)?.text).toBe('进度正常')
  })

  it('error frame with failKind=empty shows the server-persisted empty card', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '?', status: 'ok' }],
    })
    apiMocks.startPmTurn.mockResolvedValue({
      message: { id: 'u1', role: 'user', content: '?', status: 'ok' },
      waiting: 1,
    })

    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('again')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    apiMocks.listPmMessages.mockResolvedValueOnce({
      items: [{ id: 'u1', role: 'user', content: '?', status: 'failed', failKind: 'empty' }],
    })
    serverFrame(lastSocket(), { type: 'session', event: 'error', failKind: 'empty', message: 'empty reply', userMsgId: 'u1' })
    await flushPromises()

    expect(wrapper.text()).toContain('空回复')
    expect(wrapper.text()).toContain('本轮未产出有效回复')
    expect(wrapper.text()).toContain('重试')
    expect(apiMocks.patchPmMessage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reconnect replays the active turn without duplicating stream text', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '进度？', status: 'ok' }],
    })

    const wrapper = mountChat()
    await flushPromises()

    const first = lastSocket()
    serverFrame(first, { type: 'session', event: 'queue_state', busy: true, waiting: 0, userMsgId: 'u1', phase: 'running' })
    serverFrame(first, { type: 'session', event: 'turn_begin', userMsgId: 'u1' })
    serverFrame(first, chunkFrame('Hello'))
    await flushPromises()
    expect(wrapper.text()).toContain('Hello')

    first.onclose?.()
    document.dispatchEvent(new Event('visibilitychange'))
    const second = lastSocket()
    expect(second).not.toBe(first)
    serverFrame(second, { type: 'session', event: 'queue_state', busy: true, waiting: 0, userMsgId: 'u1', phase: 'running' })
    serverFrame(second, { type: 'session', event: 'turn_begin', userMsgId: 'u1' })
    serverFrame(second, chunkFrame('Hello'))
    serverFrame(second, chunkFrame('!'))
    await flushPromises()

    expect(wrapper.text()).toContain('Hello!')
    expect(wrapper.text()).not.toMatch(/HelloHello/)
    expect(wrapper.text()).not.toContain('连接中断')
    wrapper.unmount()
  })

  it('server-failed turn renders its fail card with Retry on load', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: 'q', status: 'failed', failKind: 'empty' }],
    })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.text()).toContain('空回复')
    expect(wrapper.text()).toContain('重试')
    expect(apiMocks.patchPmMessage).not.toHaveBeenCalled()
    expect(apiMocks.pmThreadChatWsUrl).toHaveBeenCalledWith('proj-1', 'thr-1')
    wrapper.unmount()
  })

  it('refresh mid-turn restores the live bubble from the snapshot and replay', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: 'q', status: 'ok' }],
    })

    const wrapper = mountChat()
    await flushPromises()

    const socket = lastSocket()
    serverFrame(socket, { type: 'session', event: 'queue_state', busy: true, waiting: 0, userMsgId: 'u1', phase: 'running' })
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-stream-resuming-meta"]').exists()).toBe(true)

    serverFrame(socket, { type: 'session', event: 'turn_begin', userMsgId: 'u1' })
    serverFrame(socket, chunkFrame('partial…'))
    await flushPromises()

    expect(wrapper.text()).toContain('partial…')
    expect(apiMocks.patchPmMessage).not.toHaveBeenCalled()
    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('a turn interrupted by a server restart shows the interrupted card', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: 'q', status: 'failed', failKind: 'interrupted' }],
    })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.text()).toContain('服务重启中断')
    expect(wrapper.text()).toContain('重试')
    expect(wrapper.text()).not.toContain('连接中断')
    expect(wrapper.find('textarea').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('socket errors and drops never fail the running turn', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })

    const wrapper = mountChat()
    await flushPromises()

    await wrapper.find('textarea').setValue('进度？')
    await wrapper.findAll('button').find((b) => b.text() === '发送')!.trigger('click')
    await flushPromises()
    const socket = lastSocket()
    serverFrame(socket, { type: 'session', event: 'turn_begin', userMsgId: 'msg-u1' })
    serverFrame(socket, chunkFrame('写到一半'))
    await flushPromises()

    socket.onerror?.()
    socket.onclose?.()
    await flushPromises()

    expect(apiMocks.patchPmMessage).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('连接中断')
    expect(wrapper.text()).toContain('写到一半')
    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('missed turn end settles from the idle snapshot after reconnect', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })

    const wrapper = mountChat()
    await flushPromises()

    await wrapper.find('textarea').setValue('进度？')
    await wrapper.findAll('button').find((b) => b.text() === '发送')!.trigger('click')
    await flushPromises()
    const first = lastSocket()
    serverFrame(first, { type: 'session', event: 'turn_begin', userMsgId: 'msg-u1' })
    await flushPromises()

    first.onclose?.()
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        { id: 'msg-u1', role: 'user', content: '进度？', status: 'ok' },
        { id: 'a1', role: 'assistant', content: '一切正常' },
      ],
    })
    document.dispatchEvent(new Event('visibilitychange'))
    serverFrame(lastSocket(), { type: 'session', event: 'queue_state', busy: false, waiting: 0 })
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('一切正常')
    wrapper.unmount()
  })

  it('Retry clears the failed partial and re-queues the same message (S4)', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '项目整体进度如何？', status: 'ok' }],
    })
    apiMocks.startPmTurn.mockResolvedValue({
      message: { id: 'u1', role: 'user', content: '项目整体进度如何？', status: 'ok' },
      waiting: 1,
    })

    const wrapper = mountChat()
    await flushPromises()

    const socket = lastSocket()
    serverFrame(socket, { type: 'session', event: 'turn_begin', userMsgId: 'u1' })
    serverFrame(socket, chunkFrame('半成品内容'))
    await flushPromises()
    apiMocks.listPmMessages.mockResolvedValueOnce({
      items: [{ id: 'u1', role: 'user', content: '项目整体进度如何？', status: 'failed', failKind: 'sandbox' }],
    })
    serverFrame(socket, { type: 'session', event: 'error', failKind: 'sandbox', message: 'timeout', userMsgId: 'u1' })
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-failed-partial"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('半成品内容')

    await wrapper.find('[data-testid="pm-fail-retry"]').trigger('click')
    await flushPromises()

    expect(apiMocks.startPmTurn).toHaveBeenCalledWith('proj-1', 'thr-1', { retryOf: 'u1' })
    expect(wrapper.find('[data-testid="pm-failed-partial"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('半成品内容')
    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('Stop asks the server to cancel and shows the stopped card from its frame', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    apiMocks.cancelPmTurn.mockResolvedValue({ ok: true })

    const wrapper = mountChat()
    await flushPromises()

    await wrapper.find('textarea').setValue('进度？')
    await wrapper.findAll('button').find((b) => b.text() === '发送')!.trigger('click')
    await flushPromises()
    serverFrame(lastSocket(), { type: 'session', event: 'turn_begin', userMsgId: 'msg-u1' })
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('停止'))!.trigger('click')
    await flushPromises()
    expect(apiMocks.cancelPmTurn).toHaveBeenCalledWith('proj-1', 'thr-1')

    apiMocks.listPmMessages.mockResolvedValueOnce({
      items: [{ id: 'msg-u1', role: 'user', content: '进度？', status: 'failed', failKind: 'stopped' }],
    })
    serverFrame(lastSocket(), { type: 'session', event: 'turn_done', interrupted: true, failKind: 'stopped', userMsgId: 'msg-u1' })
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('已停止')
    wrapper.unmount()
  })
})

describe('PmLeaderChat loading states S1–S5', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.patchPmMessage.mockResolvedValue({ id: 'u1', status: 'failed', failKind: 'connection' })
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('S1: mount with history does not flash emptyHint or suggestions', async () => {
    let resolveMessages: (v: { items: unknown[] }) => void = () => {}
    const messagesPromise = new Promise<{ items: unknown[] }>((r) => {
      resolveMessages = r
    })
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: '历史会话' }] })
    apiMocks.listPmMessages.mockReturnValue(messagesPromise)

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('正在加载历史对话')
    expect(wrapper.text()).not.toContain('询问项目整体进度')
    expect(wrapper.text()).not.toContain('项目整体进度如何？')

    resolveMessages({
      items: [
        { id: 'u1', role: 'user', content: 'hello', status: 'ok', createdAt: '2026-01-01T00:00:00Z' },
        { id: 'a1', role: 'assistant', content: 'world', createdAt: '2026-01-01T00:01:00Z' },
      ],
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('hello')
    expect(wrapper.text()).toContain('world')
    expect(wrapper.find('[data-testid="pm-idle-suggestions"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('项目整体进度如何？')
    // chips are column-level siblings (pl-[38px]), not nested inside bubble-wrap
    const chips = wrapper.find('[data-testid="pm-idle-suggestions"]')
    expect(chips.classes()).toContain('pl-[38px]')
    expect(chips.element.parentElement?.classList.contains('max-w-[85%]')).toBe(false)
    wrapper.unmount()
  })

  it('idle suggestions hidden when last message is failed user (not assistant)', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        { id: 'u0', role: 'user', content: 'prev', status: 'ok', createdAt: '2026-01-01T00:00:00Z' },
        { id: 'a0', role: 'assistant', content: 'earlier reply', createdAt: '2026-01-01T00:01:00Z' },
        { id: 'u1', role: 'user', content: 'q', status: 'failed', failKind: 'connection', createdAt: '2026-01-01T00:02:00Z' },
      ],
    })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.text()).toContain('earlier reply')
    expect(wrapper.find('[data-testid="pm-fail-retry"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-idle-suggestions"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('copy full text copies assistant .md innerText and toasts success', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        { id: 'u1', role: 'user', content: 'hello', status: 'ok', createdAt: '2026-01-01T00:00:00Z' },
        { id: 'a1', role: 'assistant', content: '**bold** reply', createdAt: '2026-01-01T00:01:00Z' },
      ],
    })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    const wrapper = mountChat()
    await flushPromises()

    const copyBtn = wrapper.findAll('button').find((b) => b.text().includes('复制全文'))
    expect(copyBtn).toBeTruthy()
    await copyBtn!.trigger('click')
    await flushPromises()

    expect(writeText).toHaveBeenCalled()
    const copied = writeText.mock.calls[0][0] as string
    expect(copied.length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('S2: switching threads clears old messages and shows loadingHistory', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        { id: 'thr-a', title: '会话A' },
        { id: 'thr-b', title: '会话B' },
      ],
    })
    apiMocks.listPmMessages.mockImplementation(async (_p, tid) => {
      if (tid === 'thr-a') {
        return { items: [{ id: 'u1', role: 'user', content: 'A-only', status: 'ok' }] }
      }
      await new Promise((r) => setTimeout(r, 30))
      return { items: [{ id: 'u2', role: 'user', content: 'B-only', status: 'ok' }] }
    })

    const wrapper = mountChat()
    await flushPromises()
    expect(wrapper.text()).toContain('A-only')

    const btnB = wrapper.findAll('aside button').find((b) => b.text().includes('会话B'))
    expect(btnB).toBeTruthy()
    await btnB!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('A-only')
    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(true)

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('B-only')
    })
    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('S4: listPmMessages reject shows EmptyState and retry succeeds', async () => {
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce({
        items: [{ id: 'u1', role: 'user', content: 'recovered', status: 'ok' }],
      })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-load-failed"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.text()).not.toContain('询问项目整体进度')

    const retryBtn = wrapper.find('[data-testid="pm-messages-load-failed"] button')
    expect(retryBtn.exists()).toBe(true)
    await retryBtn.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-load-failed"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('recovered')
    wrapper.unmount()
  })
})

describe('PmLeaderChat loading states S3/S5 and race boundaries', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    apiMocks.startPmTurn.mockImplementation(async (_pid: string, _tid: string, body: { content?: string }) => ({
      message: { id: 'msg-u1', role: 'user', content: body.content || '', status: 'ok' },
      waiting: 1,
    }))
    apiMocks.patchPmMessage.mockResolvedValue({ id: 'u1', status: 'failed', failKind: 'unknown' })
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('S3: turn_done keeps partial and finalizing meta until refetch completes', async () => {
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '?', status: 'ok' }],
    })

    let resolveRefetch: (v: { items: unknown[] }) => void = () => {}
    const refetchPromise = new Promise<{ items: unknown[] }>((r) => {
      resolveRefetch = r
    })

    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('进度？')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    const chunk = (text: string) => ({
      op: 'event',
      data: {
        type: 'session_update',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text },
        },
      },
    })

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'acp', data: chunk('Partial '), seq: 0 }),
      }),
    )
    await flushPromises()
    expect(wrapper.text()).toContain('Partial ')

    apiMocks.listPmMessages.mockReturnValueOnce(refetchPromise)
    socket!.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify({ type: 'session', event: 'turn_done', interrupted: false }) }),
    )
    await flushPromises()

    expect(wrapper.text()).toContain('Partial ')
    expect(wrapper.find('[data-testid="pm-stream-finalizing-meta"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('正在保存回复')

    resolveRefetch({
      items: [
        { id: 'u1', role: 'user', content: '进度？', status: 'ok' },
        { id: 'a1', role: 'assistant', content: 'Partial answer done' },
      ],
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Partial answer done')
    wrapper.unmount()
  })

  it('S1: messagesLoading does not show Stop button or turn busy hint', async () => {
    let resolveMessages: (v: { items: unknown[] }) => void = () => {}
    const messagesPromise = new Promise<{ items: unknown[] }>((r) => {
      resolveMessages = r
    })
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: '历史会话' }] })
    apiMocks.listPmMessages.mockReturnValue(messagesPromise)

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('停止')
    expect(wrapper.text()).not.toContain('正在处理本轮')

    resolveMessages({ items: [{ id: 'u1', role: 'user', content: 'hello', status: 'ok' }] })
    await flushPromises()
    wrapper.unmount()
  })

  it('S3: turn_done refetch failure keeps partial and allows retry', async () => {
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '?', status: 'ok' }],
    })

    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('进度？')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({
          type: 'acp',
          data: {
            op: 'event',
            data: {
              type: 'session_update',
              update: {
                sessionUpdate: 'agent_message_chunk',
                content: { type: 'text', text: 'Partial ' },
              },
            },
          },
          seq: 0,
        }),
      }),
    )
    await flushPromises()

    apiMocks.listPmMessages.mockRejectedValueOnce(new Error('network down'))
    socket!.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify({ type: 'session', event: 'turn_done', interrupted: false }) }),
    )
    await flushPromises()

    expect(wrapper.text()).toContain('Partial ')
    expect(wrapper.find('[data-testid="pm-stream-finalizing-meta"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-finalizing-retry"]').exists()).toBe(true)

    apiMocks.listPmMessages.mockResolvedValueOnce({
      items: [
        { id: 'u1', role: 'user', content: '进度？', status: 'ok' },
        { id: 'a1', role: 'assistant', content: 'Partial answer done' },
      ],
    })
    await wrapper.find('[data-testid="pm-finalizing-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-stream-bubble"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Partial answer done')
    wrapper.unmount()
  })

  it('finalizing takes priority over messagesLoading pane', async () => {
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'u1', role: 'user', content: '?', status: 'ok' }],
    })

    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('x')
    await wrapper.findAll('button').find((b) => b.text() === '发送')!.trigger('click')
    await flushPromises()

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({
          type: 'acp',
          data: {
            op: 'event',
            data: {
              type: 'session_update',
              update: {
                sessionUpdate: 'agent_message_chunk',
                content: { type: 'text', text: 'keep' },
              },
            },
          },
          seq: 0,
        }),
      }),
    )
    await flushPromises()

    let resolveSlowRefetch: (v: { items: unknown[] }) => void = () => {}
    apiMocks.listPmMessages.mockReturnValueOnce(
      new Promise((r) => {
        resolveSlowRefetch = r
      }),
    )
    socket!.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify({ type: 'session', event: 'turn_done', interrupted: false }) }),
    )
    await flushPromises()

    expect(wrapper.find('[data-testid="pm-messages-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-stream-finalizing-meta"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('keep')

    resolveSlowRefetch({
      items: [
        { id: 'u1', role: 'user', content: 'x', status: 'ok' },
        { id: 'a1', role: 'assistant', content: 'done' },
      ],
    })
    await flushPromises()
    wrapper.unmount()
  })

  it('rapid thread switch drops stale listPmMessages response', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        { id: 'thr-a', title: 'A' },
        { id: 'thr-b', title: 'B' },
      ],
    })

    const pending: Record<string, { resolve: (v: { items: unknown[] }) => void }> = {}
    apiMocks.listPmMessages.mockImplementation((_p, tid) => {
      return new Promise((resolve) => {
        pending[tid] = { resolve }
      })
    })

    const wrapper = mountChat()
    await flushPromises()

    const btnB = wrapper.findAll('aside button').find((b) => b.text().includes('B'))
    await btnB!.trigger('click')
    await flushPromises()

    pending['thr-a']?.resolve({ items: [{ id: 'stale', role: 'user', content: 'STALE', status: 'ok' }] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('STALE')

    pending['thr-b']?.resolve({ items: [{ id: 'fresh', role: 'user', content: 'FRESH', status: 'ok' }] })
    await flushPromises()
    expect(wrapper.text()).toContain('FRESH')
    wrapper.unmount()
  })
})

describe('PmLeaderChat i18n locale switch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockRejectedValue(new Error('fail'))
  })

  it('shows en loading/error strings without raw keys', async () => {
    const enPages = await import('@/locales/en/pages.json')
    const enCommon = await import('@/locales/en/common.json')
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon.default, ...enPages.default } },
    })
    const wrapper = mount(PmLeaderChat, {
      props: { projectId: 'proj-1', binding: { enabled: true, agentAvailable: true, agentConfigRef: 'a', aclNote: '' } },
      global: { plugins: [i18n], stubs: { Icon: true, CitationCard: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load messages')
    expect(wrapper.text()).not.toContain('pages.projectDetail.pm.loadFailed')
    wrapper.unmount()
  })
})

describe('PmLeaderChat settings entry', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
  })

  it('emits openSettings from enabled gear+settings button', async () => {
    const wrapper = mountChat()
    await flushPromises()
    const btn = wrapper.find('[data-testid="pm-chat-open-settings"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('设置')
    await btn.trigger('click')
    expect(wrapper.emitted('openSettings')).toHaveLength(1)
    wrapper.unmount()
  })

  it('emits openSettings from disabled empty-state CTA', async () => {
    const wrapper = mountChat({
      enabled: false,
      agentAvailable: false,
      agentConfigRef: '',
      aclNote: '',
    })
    await flushPromises()
    expect(wrapper.text()).toContain('需先在本页设置中绑定')
    const go = wrapper.findAll('button').find((b) => b.text().includes('前往设置'))
    expect(go).toBeTruthy()
    await go!.trigger('click')
    expect(wrapper.emitted('openSettings')).toHaveLength(1)
    wrapper.unmount()
  })
})

describe('PmLeaderChat stick-to-bottom', () => {
  const originalWS = globalThis.WebSocket

  function mockScrollerMetrics(
    el: HTMLElement,
    scrollHeight: number,
    clientHeight: number,
    scrollTop?: number,
  ) {
    let top = scrollTop ?? scrollHeight - clientHeight
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => scrollHeight })
    Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => clientHeight })
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
    return {
      getScrollTop: () => top,
      setScrollTop: (v: number) => {
        top = v
      },
      syncStickFromScroll: () => {
        el.dispatchEvent(new Event('scroll'))
      },
    }
  }

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: 't' }] })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    apiMocks.startPmTurn.mockImplementation(async (_pid: string, _tid: string, body: { content?: string }) => ({
      message: { id: 'msg-u1', role: 'user', content: body.content || '', status: 'ok' },
      waiting: 1,
    }))
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('handleAcp and refetchAfterTurnDone do not scroll when user is away from bottom', async () => {
    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    const metrics = mockScrollerMetrics(scrollerEl, 1000, 200, 100)

    const textarea = wrapper.find('textarea')
    await textarea.setValue('进度？')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    metrics.setScrollTop(100)
    metrics.syncStickFromScroll()
    await flushPromises()
    const awayTop = metrics.getScrollTop()

    const chunk = (text: string) => ({
      op: 'event',
      data: {
        type: 'session_update',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text },
        },
      },
    })

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'acp', data: chunk('Hello'), seq: 0 }),
      }),
    )
    await flushPromises()
    expect(metrics.getScrollTop()).toBe(awayTop)

    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        { id: 'u1', role: 'user', content: '进度？', status: 'ok' },
        { id: 'a1', role: 'assistant', content: 'Hello', status: 'ok' },
      ],
    })
    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'session', event: 'turn_done', interrupted: false }),
      }),
    )
    await flushPromises()
    expect(metrics.getScrollTop()).toBe(awayTop)
    wrapper.unmount()
  })

  it('send and loadMessages force scroll to bottom', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        { id: 'thr-1', title: '会话1' },
        { id: 'thr-2', title: '会话2' },
      ],
    })
    apiMocks.listPmMessages.mockImplementation(async (_pid: string, tid: string) => ({
      items: [{ id: `u-${tid}`, role: 'user', content: `msg-${tid}`, status: 'ok' }],
    }))

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    mockScrollerMetrics(scrollerEl, 1000, 200, 100)

    const btn2 = wrapper.findAll('aside button').find((b) => b.text().includes('会话2'))
    expect(btn2).toBeTruthy()
    await btn2!.trigger('click')
    await flushPromises()
    expect(scrollerEl.scrollTop).toBe(scrollerEl.scrollHeight)

    mockScrollerMetrics(scrollerEl, 1000, 200, 100)
    const textarea = wrapper.find('textarea')
    await textarea.setValue('强制滚底')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()
    expect(scrollerEl.scrollTop).toBe(scrollerEl.scrollHeight)
    wrapper.unmount()
  })

  it('QQ channel threads show dual QQ tags, readonly bar, no delete/send', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-qq',
          title: '产品评审同步',
          userId: 'qq:guild:ch1',
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
        {
          id: 'thr-web',
          title: '对接发布流程',
          userId: 'alice',
          projectId: 'proj-1',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [{ id: 'm1', role: 'user', content: '[来自 Alice] 下周评审还开吗？', status: 'ok' }],
    })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.findAll('[data-testid="pm-qq-tag"]').length).toBe(1)
    const channelBtn = wrapper.find('[data-channel="1"]')
    expect(channelBtn.exists()).toBe(true)
    expect(channelBtn.find('[data-testid="pm-thread-delete"]').exists()).toBe(false)
    expect(wrapper.find('[data-channel="0"] [data-testid="pm-thread-delete"]').exists()).toBe(true)

    // Active defaults to first (channel): header tag + readonly composer.
    expect(wrapper.find('[data-testid="pm-qq-tag-header"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('渠道会话不可在 Web 改写')
    expect(wrapper.text()).toContain('渠道会话在 Web 只读：不可发送、不可删除')
    expect(wrapper.text()).toContain('禁止当普通 Web 线程编辑或删除')
    expect(wrapper.text()).toContain('可点击缩略图查看大图')
    expect(apiMocks.startPmTurn).not.toHaveBeenCalled()

    // Web thread has send composer and no QQ header tag.
    await wrapper.find('[data-channel="0"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-qq-tag-header"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('wecom channel threads show synthetic id, unspoken badge, and readonly editor (g3.2/g5.3)', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-wecom',
          title: '',
          userId: 'wecom:c2c:zhangsan',
          unspoken: true,
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.find('[data-channel="1"]').text()).toContain('wecom:c2c:zhangsan')
    expect(wrapper.find('[data-testid="pm-unspoken-tag"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-chat-title"]').text()).toContain('wecom:c2c:zhangsan')
    expect(wrapper.find('[data-testid="pm-channel-subtitle"]').text()).toContain('来自企业微信 Channel')
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').text()).toContain('来自企业微信，请在企业微信侧回复')
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('Feishu channel threads show cyan badge, readonly, no delete/send', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-fs',
          title: '飞书私聊',
          userId: 'feishu:c2c:oc_zhang',
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    const wrapper = mountChat()
    await flushPromises()
    const tag = wrapper.find('[data-testid="pm-qq-tag"]')
    expect(tag.exists()).toBe(true)
    expect(tag.text()).toBe('飞书')
    expect(tag.attributes('data-channel-kind')).toBe('feishu')
    expect(wrapper.find('[data-testid="pm-thread-delete"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('来自飞书，请在飞书侧回复')
    expect(wrapper.text()).toContain('不可当普通 Web 线程编辑或删除')
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('DingTalk channel threads show badge and readonly source copy', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-dt',
          title: '钉钉群',
          userId: 'dingtalk:group:cid_review',
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })
    const wrapper = mountChat()
    await flushPromises()
    const tag = wrapper.find('[data-testid="pm-qq-tag"]')
    expect(tag.exists()).toBe(true)
    expect(tag.text()).toBe('钉钉')
    expect(tag.attributes('data-channel-kind')).toBe('dingtalk')
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('来自钉钉，请在钉钉侧回复')
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders yellow channel hint for system+channel source only (g5.4)', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-qq',
          title: '你好',
          userId: 'qq:c2c:u1',
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        { id: 'u1', role: 'user', content: '这是现场照片', status: 'ok', source: 'channel' },
        {
          id: 'h1',
          role: 'system',
          source: 'channel',
          content: '收到 1 张图片，但下载失败，未能展示。',
          status: 'ok',
        },
        { id: 's-other', role: 'system', source: 'cron', content: '不该露出的系统消息', status: 'ok' },
      ],
    })

    const wrapper = mountChat()
    await flushPromises()
    const hint = wrapper.find('[data-testid="pm-channel-hint"]')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toContain('渠道提示')
    expect(hint.text()).toContain('收到 1 张图片，但下载失败，未能展示。')
    expect(hint.classes().join(' ')).toMatch(/border-warn/)
    expect(wrapper.text()).not.toContain('不该露出的系统消息')
    wrapper.unmount()
  })

  it('channel context menu opens detail with untitled placeholder', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-qq-empty',
          title: '   ',
          userId: 'qq:c2c:u1',
          projectId: 'proj-1',
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T12:00:00Z',
        },
        {
          id: 'thr-web',
          title: 'Web',
          userId: 'alice',
          projectId: 'proj-1',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T12:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: [] })

    const wrapper = mountChat()
    await flushPromises()

    expect(wrapper.find('[data-channel="1"]').text()).toContain('qq:c2c:u1')
    expect(wrapper.find('[data-testid="pm-chat-title"]').text()).toContain('qq:c2c:u1')

    await wrapper.find('[data-channel="0"]').trigger('contextmenu')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-channel-ctx-menu"]').exists()).toBe(false)

    await wrapper.find('[data-channel="1"]').trigger('contextmenu')
    await flushPromises()
    const menu = wrapper.find('[data-testid="pm-channel-ctx-menu"]')
    expect(menu.exists()).toBe(true)
    expect(menu.text()).toContain('查看详情')
    expect(menu.text()).not.toContain('删除')

    await wrapper.find('[data-testid="pm-channel-ctx-detail"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-channel-detail-title"]').text()).toBe('qq:c2c:u1')
    expect(wrapper.find('[data-testid="pm-channel-detail-source"]').text()).toContain('来自 QQ Channel')
    wrapper.unmount()
  })

  it('scrolling back within 48px restores stick and following acp scrolls', async () => {
    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    mockScrollerMetrics(scrollerEl, 1000, 200, 100)

    const textarea = wrapper.find('textarea')
    await textarea.setValue('恢复跟随')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    mockScrollerMetrics(scrollerEl, 1000, 200, 100)
    scrollerEl.dispatchEvent(new Event('scroll'))
    await flushPromises()

    const metrics = mockScrollerMetrics(scrollerEl, 1000, 200, 760)
    metrics.syncStickFromScroll()
    await flushPromises()

    mockScrollerMetrics(scrollerEl, 1200, 200, 760)

    const chunk = (text: string) => ({
      op: 'event',
      data: {
        type: 'session_update',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text },
        },
      },
    })

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'acp', data: chunk('More'), seq: 0 }),
      }),
    )
    await flushPromises()

    expect(scrollerEl.scrollTop).toBe(scrollerEl.scrollHeight)
    wrapper.unmount()
  })
})

describe('PmLeaderChat tail window + lazyload', () => {
  const originalWS = globalThis.WebSocket

  function mockScrollerMetrics(
    el: HTMLElement,
    scrollHeight: number,
    clientHeight: number,
    scrollTop?: number,
  ) {
    let top = scrollTop ?? scrollHeight - clientHeight
    let height = scrollHeight
    Object.defineProperty(el, 'scrollHeight', {
      configurable: true,
      get: () => height,
      set: (v: number) => {
        height = v
      },
    })
    Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => clientHeight })
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
    return {
      getScrollTop: () => top,
      setScrollTop: (v: number) => {
        top = v
      },
      setScrollHeight: (v: number) => {
        height = v
      },
      syncStickFromScroll: () => {
        el.dispatchEvent(new Event('scroll'))
      },
    }
  }

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.startPmTurn.mockImplementation(async (_pid: string, _tid: string, body: { content?: string }) => ({
      message: { id: 'msg-u1', role: 'user', content: body.content || '', status: 'ok' },
      waiting: 1,
    }))
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('non-Channel first load requests limit=20 tail window and shows scroll-up tip', async () => {
    const tail = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i + 5}`,
      role: i % 2 === 0 ? 'user' : 'assistant',
      content: `msg-${i + 5}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items: tail, hasMore: true })

    const wrapper = mountChat()
    await flushPromises()

    expect(apiMocks.listPmMessages).toHaveBeenCalledWith('proj-1', 'thr-long', { limit: 20 })
    expect(wrapper.findAll('[data-msg-id]').length).toBe(20)
    const tip = wrapper.find('[data-testid="pm-history-tip"]')
    expect(tip.exists()).toBe(true)
    expect(tip.text()).toContain('上滑加载更早')
    wrapper.unmount()
  })

  it('Channel first load uses the newest window and shows the lazyload tip', async () => {
    const items = Array.from({ length: 20 }, (_, i) => ({
      id: `c-${i}`,
      role: 'user',
      content: `qq-${i}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-qq',
          title: 'QQ会话',
          userId: 'qq:guild:1',
          projectId: 'proj-1',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items, hasMore: true })

    const wrapper = mountChat()
    await flushPromises()

    expect(apiMocks.listPmMessages).toHaveBeenCalledWith('proj-1', 'thr-qq', { limit: 20 })
    expect(wrapper.find('[data-testid="pm-history-tip"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-msg-id]').length).toBe(20)
    wrapper.unmount()
  })

  it('scroll near top lazyloads earlier page with before=oldest id', async () => {
    const tail = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i + 20}`,
      role: 'user',
      content: `t-${i + 20}`,
      status: 'ok',
    }))
    const earlier = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i}`,
      role: 'user',
      content: `e-${i}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
    })
    apiMocks.listPmMessages
      .mockResolvedValueOnce({ items: tail, hasMore: true })
      .mockResolvedValueOnce({ items: earlier, hasMore: false })

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    const metrics = mockScrollerMetrics(scrollerEl, 2000, 400, 40)
    metrics.syncStickFromScroll()
    await flushPromises()

    expect(apiMocks.listPmMessages).toHaveBeenLastCalledWith('proj-1', 'thr-long', {
      limit: 20,
      before: 'm-20',
    })
    expect(wrapper.findAll('[data-msg-id]').length).toBe(40)
    expect(wrapper.find('[data-testid="pm-history-tip"]').text()).toContain('已到最早')
    wrapper.unmount()
  })

  it('short session hasMore=false does not request earlier on scroll-top', async () => {
    const items = Array.from({ length: 8 }, (_, i) => ({
      id: `s-${i}`,
      role: 'user',
      content: `s-${i}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [{ id: 'thr-short', title: '短会话', userId: 'alice' }],
    })
    apiMocks.listPmMessages.mockResolvedValue({ items, hasMore: false })

    const wrapper = mountChat()
    await flushPromises()
    expect(apiMocks.listPmMessages).toHaveBeenCalledTimes(1)

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    const metrics = mockScrollerMetrics(scrollerEl, 800, 400, 0)
    metrics.syncStickFromScroll()
    await flushPromises()

    expect(apiMocks.listPmMessages).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="pm-history-tip"]').text()).toContain('已到最早')
    wrapper.unmount()
  })

  it('refetchAfterTurnDone merges by id and keeps prepended prefix', async () => {
    let socket: MockWebSocket | null = null
    class CaptureWS extends MockWebSocket {
      constructor() {
        super()
        socket = this
      }
    }
    // @ts-expect-error test stub
    globalThis.WebSocket = CaptureWS

    const older = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i}`,
      role: 'user',
      content: `old-${i}`,
      status: 'ok',
    }))
    const tail = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i + 20}`,
      role: i % 2 === 0 ? 'user' : 'assistant',
      content: `tail-${i + 20}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
    })
    apiMocks.listPmMessages
      .mockResolvedValueOnce({ items: tail, hasMore: true })
      .mockResolvedValueOnce({ items: older, hasMore: false })
      .mockResolvedValueOnce({
        items: [
          ...tail.slice(-19),
          { id: 'm-39', role: 'assistant', content: 'updated-tail', status: 'ok' },
          { id: 'm-40', role: 'assistant', content: 'turn-result', status: 'ok' },
        ],
        hasMore: true,
      })

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    mockScrollerMetrics(scrollerEl, 2000, 400, 30).syncStickFromScroll()
    await flushPromises()
    expect(wrapper.findAll('[data-msg-id]').length).toBe(40)

    const textarea = wrapper.find('textarea')
    await textarea.setValue('继续')
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === '发送')
    await sendBtn!.trigger('click')
    await flushPromises()

    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'session', event: 'turn_done', interrupted: false }),
      }),
    )
    await flushPromises()

    // Prefix m-0..m-19 must remain; new turn message appears; no collapse to 20.
    expect(wrapper.find('[data-msg-id="m-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-msg-id="m-40"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-msg-id]').length).toBeGreaterThan(20)
    const mergeCalls = apiMocks.listPmMessages.mock.calls
    const mergeCall = mergeCalls[mergeCalls.length - 1]
    expect(mergeCall?.[2]).toEqual({ limit: 20 })
    wrapper.unmount()
  })

  it('lazyload failure keeps messages and retries on next top scroll', async () => {
    const tail = Array.from({ length: 20 }, (_, i) => ({
      id: `m-${i + 20}`,
      role: 'user',
      content: `t-${i + 20}`,
      status: 'ok',
    }))
    const earlier = Array.from({ length: 10 }, (_, i) => ({
      id: `m-${i + 10}`,
      role: 'user',
      content: `e-${i + 10}`,
      status: 'ok',
    }))
    apiMocks.listPmThreads.mockResolvedValue({
      items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
    })
    apiMocks.listPmMessages
      .mockResolvedValueOnce({ items: tail, hasMore: true })
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce({ items: earlier, hasMore: true })

    const wrapper = mountChat()
    await flushPromises()

    const scrollerEl = wrapper.find('[data-testid="pm-message-scroller"]').element as HTMLElement
    const metrics = mockScrollerMetrics(scrollerEl, 2000, 400, 20)
    metrics.syncStickFromScroll()
    await flushPromises()

    expect(wrapper.findAll('[data-msg-id]').length).toBe(20)
    expect(wrapper.find('[data-testid="pm-history-tip"]').text()).toContain('再次滚到顶部可重试')

    // Scroll away then back to top to retry.
    metrics.setScrollTop(200)
    metrics.syncStickFromScroll()
    await flushPromises()
    metrics.setScrollTop(10)
    metrics.syncStickFromScroll()
    await flushPromises()

    expect(wrapper.findAll('[data-msg-id]').length).toBe(30)
    expect(wrapper.find('[data-msg-id="m-10"]').exists()).toBe(true)
    wrapper.unmount()
  })
})

const PreviewAppModalStub = {
  props: ['open', 'title', 'width'],
  emits: ['close'],
  template: `
    <div v-if="open" data-testid="pm-image-preview-modal">
      <div data-testid="pm-image-preview-title">{{ title }}</div>
      <button type="button" data-testid="pm-image-preview-close" @click="$emit('close')">×</button>
      <button type="button" data-testid="pm-image-preview-backdrop" @click="$emit('close')">backdrop</button>
      <slot />
    </div>
  `,
}

function mountChatForPreview() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(PmLeaderChat, {
    props: {
      projectId: 'proj-1',
      binding: {
        enabled: true,
        agentAvailable: true,
        agentConfigRef: 'agent-1',
        aclNote: '',
      },
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, CitationCard: true, AppModal: PreviewAppModalStub },
    },
  })
}

describe('PmLeaderChat image preview (f1/f2)', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.listPmThreads.mockResolvedValue({ items: [{ id: 'thr-1', title: '会话' }] })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        {
          id: 'u1',
          role: 'user',
          content: '看下这两张',
          status: 'ok',
          createdAt: '2026-08-09T00:00:00Z',
          images: [
            { data: 'AAAAhist', mimeType: 'image/png', name: '看板截图.png' },
            { data: 'BBBBhist', mimeType: 'image/png', name: '表单截图.png' },
            { data: 'DOCDATA', mimeType: 'application/pdf', name: '说明.docx' },
          ],
        },
      ],
    })
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('QQ channel thread thumbs stay previewable (g4.2)', async () => {
    apiMocks.listPmThreads.mockResolvedValue({
      items: [
        {
          id: 'thr-qq',
          title: '你好',
          userId: 'qq:c2c:u1',
          projectId: 'proj-1',
          createdAt: '2026-08-09T00:00:00Z',
          updatedAt: '2026-08-09T00:00:00Z',
        },
      ],
    })
    apiMocks.listPmMessages.mockResolvedValue({
      items: [
        {
          id: 'u1',
          role: 'user',
          content: '看图',
          status: 'ok',
          source: 'channel',
          createdAt: '2026-08-09T00:00:00Z',
          images: [{ data: 'WEBPB64', mimeType: 'image/webp', name: '70345B2BE' }],
        },
      ],
    })
    const wrapper = mountChatForPreview()
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-channel-readonly"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    const thumb = wrapper.find('[data-testid="pm-history-image-thumb"]')
    expect(thumb.exists()).toBe(true)
    expect(thumb.element.tagName).toBe('BUTTON')
    expect(thumb.text()).toContain('点击放大')
    await thumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-title"]').text()).toBe('图片预览 · 70345B2BE')
    expect(wrapper.find('[data-testid="pm-chat-send"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens history thumb preview, Esc stays, no gallery, no unavailable overlay', async () => {
    const wrapper = mountChatForPreview()
    await flushPromises()
    const thumbs = wrapper.findAll('[data-testid="pm-history-image-thumb"]')
    expect(thumbs).toHaveLength(2)
    expect(thumbs[0].text()).toContain('点击放大')
    expect(thumbs[0].text()).not.toContain('不可预览')
    expect(wrapper.find('[data-testid="pm-history-file-chip"]').exists()).toBe(true)

    await thumbs[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-title"]').text()).toBe('图片预览 · 表单截图.png')
    expect(wrapper.find('[data-testid="pm-image-preview-img"]').attributes('src')).toContain('BBBBhist')
    expect(wrapper.find('[data-testid="pm-image-preview-prev"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-image-preview-next"]').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-modal"]').exists()).toBe(true)

    await wrapper.find('[data-testid="pm-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('draft preview closes without dropping attachment; load-failed still closes', async () => {
    const wrapper = mountChatForPreview()
    await flushPromises()

    class MockFR {
      result = 'data:image/png;base64,DRAFTPM'
      onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => unknown) | null = null
      readAsDataURL() {
        queueMicrotask(() => this.onload?.call(this as unknown as FileReader, {} as ProgressEvent<FileReader>))
      }
    }
    vi.stubGlobal('FileReader', MockFR)

    const fileInput = wrapper.find('input[type="file"]')
    const file = new File(['x'], '待发截图.png', { type: 'image/png' })
    Object.defineProperty(fileInput.element, 'files', { configurable: true, value: [file] })
    await fileInput.trigger('change')
    await flushPromises()

    const draft = wrapper.find('[data-testid="pm-draft-image-thumb"]')
    expect(draft.exists()).toBe(true)
    expect(draft.text()).toContain('点击放大')
    expect(draft.text()).not.toContain('不可预览')
    await draft.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-title"]').text()).toBe('图片预览 · 待发截图.png')

    await wrapper.find('[data-testid="pm-image-preview-img"]').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-failed"]').text()).toContain('图片加载失败')
    await wrapper.find('[data-testid="pm-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="pm-image-preview-modal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-draft-image-thumb"]').exists()).toBe(true)
    vi.unstubAllGlobals()
    wrapper.unmount()
  })
})
