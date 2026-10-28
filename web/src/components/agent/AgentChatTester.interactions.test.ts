// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentChatTester from './AgentChatTester.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'

const mocks = vi.hoisted(() => ({
  getSandbox: vi.fn(),
  wsUrl: vi.fn(() => 'ws://chat'),
  eventLog: vi.fn(),
  destroy: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSandbox: mocks.getSandbox,
      sandboxChatWsUrl: mocks.wsUrl,
      sandboxEventLog: mocks.eventLog,
      destroySandbox: mocks.destroy,
    },
  }
})

let socket: SocketStub | null
class SocketStub {
  static OPEN = 1
  readyState = 1
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  send = vi.fn()
  close = vi.fn()
  constructor() {
    socket = this
  }
}

const ModalStub = {
  props: ['open', 'title'],
  emits: ['close'],
  template: '<div v-if="open" class="modal"><h2>{{title}}</h2><slot/><slot name="footer"/></div>',
}

function mountTester(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentChatTester, {
    props: { profile: 'cursor', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        AppModal: ModalStub,
        ReposEditor: { template: '<div data-testid="repos"/>' },
        AcpStatusPill: { props: ['busy'], template: '<div data-testid="acp">{{busy}}</div>' },
        ChatImageThumb: {
          props: ['src', 'label', 'testId'],
          emits: ['preview'],
          template: '<button :data-testid="testId" @click="$emit(\'preview\')">{{label}}</button>',
        },
        ChatImagePreviewModal: {
          props: ['open', 'label'],
          emits: ['close'],
          template: '<div v-if="open" data-testid="preview">{{label}}<button @click="$emit(\'close\')">close</button></div>',
        },
      },
    },
  })
}

function frame(type: string, extra: Record<string, unknown> = {}) {
  const body = type === 'acp' ? { type, ...extra } : { type: 'session', event: type, ...extra }
  socket!.onmessage?.(new MessageEvent('message', { data: JSON.stringify(body) }))
}

describe('AgentChatTester interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    socket = null
    vi.stubGlobal('WebSocket', SocketStub)
    mocks.getSandbox.mockResolvedValue({ id: 7, name: 'sandbox', status: 'running' })
    mocks.eventLog.mockResolvedValue({ events: [] })
    mocks.destroy.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('validates launch modes, builds clone payload, and handles startup failures', async () => {
    const create = vi.fn().mockResolvedValue({ id: 7, name: 'sandbox', status: 'starting' })
    const w = mountTester({ createTest: create })
    const vm = w.vm as any
    vm.launchMode = 'clone'
    vm.repos = [{ name: ' repo ', url: ' https://repo ', branch: ' main ' }, { name: '', url: 'x', branch: '' }]
    await vm.start()
    expect(create).toHaveBeenCalledWith('cursor', {
      repos: [{ name: 'repo', url: 'https://repo', branch: 'main' }],
    })
    expect(socket).toBeTruthy()
    socket!.onopen?.()
    await flushPromises()
    expect(vm.status).toBe('ready')

    vm.reset()
    create.mockRejectedValueOnce(new Error('quota'))
    vm.launchMode = 'empty'
    await vm.start()
    expect(create).toHaveBeenLastCalledWith('cursor', {})
    expect(vm.status).toBe('error')
    expect(vm.errorMsg).toBe('quota')
    w.unmount()

    const missing = mountTester()
    await (missing.vm as any).start()
    expect((missing.vm as any).status).toBe('error')
    expect((missing.vm as any).errorMsg).toBeTruthy()
    missing.unmount()
  })

  it('shows tool summary and masked details in arrival order', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(3)
    socket!.onopen?.()
    await flushPromises()
    vm.input = 'go'
    vm.send()
    frame('turn_begin', { item: { id: vm.queued[0].id } })
    const up = (update: Record<string, unknown>) => frame('acp', { data: { type: 'session_update', update } })
    up({ sessionUpdate: 'agent_thought_chunk', content: { text: 'plan' } })
    up({ sessionUpdate: 'tool_call', toolCallId: 'a', title: 'Shell', status: 'in_progress', rawInput: { command: 'curl  -sS\n http://x', env: { API_KEY: 'k1' } } })
    up({ sessionUpdate: 'tool_call_update', toolCallId: 'a', status: 'completed', rawOutput: { stdout: 'ok', stderr: 'warn' } })
    up({ sessionUpdate: 'tool_call', toolCallId: 'b', title: 'Read', rawInput: 'a.ts', rawOutput: { content: [{ text: 'body' }] } })
    up({ sessionUpdate: 'agent_message_chunk', content: { text: 'found ' } })
    up({ sessionUpdate: 'agent_message_chunk', content: { text: 'it' } })
    up({ sessionUpdate: 'tool_call', toolCallId: 'c', title: 'Noop', rawInput: {}, rawOutput: [] })
    await flushPromises()
    const turn = vm.turns[vm.turns.length - 1]
    expect(turn.steps.map((s: any) => s.kind)).toEqual(['thought', 'tool', 'tool', 'message', 'tool'])
    expect(turn.steps[3].text).toBe('found it')
    expect(turn.tools[0]).toMatchObject({ summary: 'curl -sS http://x', output: 'ok\nwarn' })
    expect(turn.tools[0].input).toContain('"API_KEY": "****"')
    expect(turn.tools[0].input).not.toContain('k1')
    expect(turn.tools[1]).toMatchObject({ input: 'a.ts', output: 'body' })
    expect(turn.tools[1].summary).toBeUndefined()
    expect(turn.tools[2].input).toBeUndefined()
    expect(turn.tools[2].output).toBeUndefined()
    const tl = w.get('[data-testid="agent-timeline"]')
    expect(Array.from(tl.element.children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'agent-timeline-thought', 'agent-tool-group', 'tester-agent-message', 'agent-tool-group',
    ])
    expect(w.find('[data-testid="agent-chat-generating"]').exists()).toBe(false)
  })

  it.each(['turn_done', 'error'])('keeps turn activity after completed tools until %s', async (terminal) => {
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(3)
    socket!.onopen?.()
    await flushPromises()
    frame('turn_begin', { item: { id: 'activity-turn', text: 'go' } })
    await flushPromises()
    expect(w.get('[data-testid="tester-agent-activity"]').attributes('role')).toBe('status')

    const up = (update: Record<string, unknown>) => frame('acp', { data: { type: 'session_update', update } })
    up({ sessionUpdate: 'agent_thought_chunk', content: { text: 'plan' } })
    await flushPromises()
    expect(w.get('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('idle')
    expect(w.findAll('[data-testid="thinking-lamp"]')).toHaveLength(1)

    up({ sessionUpdate: 'tool_call', toolCallId: 'read', title: 'Read', status: 'in_progress' })
    up({ sessionUpdate: 'tool_call_update', toolCallId: 'read', status: 'completed' })
    await flushPromises()
    expect(w.get('[data-testid="agent-tool-group"]').attributes('data-state')).toBe('done')
    expect(w.get('[data-testid="tester-agent-activity"]').text()).toContain('正在生成')

    up({ sessionUpdate: 'agent_message_chunk', content: { text: 'working' } })
    await flushPromises()
    expect(w.get('[data-testid="tester-agent-message"]').text()).toContain('working')
    expect(w.find('[data-testid="tester-agent-activity"]').exists()).toBe(true)

    frame(terminal, terminal === 'error' ? { message: 'agent failed' } : {})
    await flushPromises()
    expect(w.find('[data-testid="tester-agent-activity"]').exists()).toBe(false)
    expect(w.find('[data-testid="thinking-lamp"]').exists()).toBe(false)
    if (terminal === 'error') expect(w.text()).toContain('agent failed')
    w.unmount()
  })

  it('folds a finished reply into one elapsed line', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-20T00:00:00.000Z'))
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(3)
    socket!.onopen?.()
    await flushPromises()
    vm.input = 'go'
    vm.send()
    frame('turn_begin', { item: { id: vm.queued[0].id } })
    const up = (update: Record<string, unknown>) => frame('acp', { data: { type: 'session_update', update } })
    up({ sessionUpdate: 'agent_thought_chunk', content: { text: 'plan' } })
    up({ sessionUpdate: 'agent_message_chunk', content: { text: 'done' } })
    vi.setSystemTime(new Date('2026-10-20T00:03:34.000Z'))
    frame('turn_done')
    await flushPromises()
    const btn = w.get('[data-testid="agent-timeline-elapsed"]')
    expect(btn.text()).toContain('用时 3m 34s')
    expect(w.get('[data-testid="tester-agent-message"]').text()).toContain('done')
    expect(w.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
    await btn.trigger('click')
    expect(w.get('[data-testid="agent-timeline-thought"]').text()).toContain('plan')
    expect(w.text()).not.toContain('已完成')
    vi.useRealTimers()
    w.unmount()
  })

  it('queues sends, applies live ACP frames, cancels, and handles socket events', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(3)
    socket!.onopen?.()
    await flushPromises()

    vm.input = ' hello '
    vm.attachments = [{ data: 'abc', mimeType: 'image/png', url: 'data:image/png;base64,abc', name: 'pic.png' }]
    vm.send()
    expect(socket!.send).toHaveBeenCalled()
    expect(vm.queued).toHaveLength(1)
    expect(vm.status).toBe('thinking')
    const sentId = vm.queued[0].id
    expect(JSON.parse(socket!.send.mock.calls[0][0])).toMatchObject({ type: 'chat', id: sentId, content: 'hello' })
    frame('queue_state', { busy: true, waiting: 1, items: [{ id: sentId, text: 'hello', images: [] }] })
    expect(vm.queued).toHaveLength(1)
    frame('turn_begin', { item: { id: sentId, text: 'hello' } })
    expect(vm.queued).toHaveLength(0)
    expect(vm.turns[0].images[0].name).toBe('pic.png')
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agentMessageChunk', content: { text: 'Hello' } } } })
    frame('acp', { data: { type: 'session_update', update: { session_update: 'agent-thought-chunk', content: [{ text: 'think' }] } } })
    frame('acp', { data: { type: 'session_update', update: { type: 'plan', entries: [{ title: 'step', state: 'completed' }, {}] } } })
    frame('acp', { data: { type: 'session_update', update: { kind: 'tool_call', id: 't1', name: 'read_file', status: 'in_progress' } } })
    frame('acp', { data: { type: 'session_update', update: { kind: 'toolcall_update', id: 't1', title: 'Read done', state: 'completed' } } })
    await flushPromises()
    expect(vm.turns[1]).toMatchObject({ text: 'Hello', thought: 'think', streaming: true })
    expect(vm.turns[1].plan).toEqual([{ content: 'step', status: 'completed' }])
    expect(vm.turns[1].tools[0]).toMatchObject({ id: 't1', title: 'Read Done', status: 'completed' })
    expect(w.find('[data-testid="agent-tool-group"]').attributes('data-state')).toBe('done')
    expect(w.find('[data-testid="agent-tool-group-names"]').text()).toBe('Read Done')
    expect(w.find('[data-testid="tester-agent-message"]').text()).toContain('Hello')
    const steps = w.find('[data-testid="agent-timeline"]').findAll('[data-testid="tester-agent-message"], [data-testid="agent-timeline-thought"], [data-testid="agent-tool-group"]')
    expect(steps.map((s) => s.attributes('data-testid'))).toEqual(['tester-agent-message', 'agent-timeline-thought', 'agent-tool-group'])
    frame('turn_done')
    expect(vm.status).toBe('ready')
    await flushPromises()
    expect(w.find('[data-testid="stream-md"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(true)
    // Settled turns keep the last message under the elapsed rule, even when tools follow it.
    expect(w.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-tool-group"]').exists()).toBe(false)
    const folded = w.get('[data-testid="tester-agent-message"]')
    expect(folded.text()).toContain('Hello')
    const rule = w.get('[data-testid="agent-timeline-rule"]').element
    expect(rule.compareDocumentPosition(folded.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    expect(w.findAll('[data-testid="tester-agent-message"]')).toHaveLength(1)
    expect(w.get('[data-testid="agent-timeline-work"]').text()).toContain('think')

    vm.input = 'again'
    vm.send()
    frame('turn_begin', { item: { id: vm.queued[0].id } })
    frame('error', { message: 'agent failed' })
    expect(vm.turns.at(-1).error).toBe('agent failed')
    frame('error')
    expect(vm.errorMsg).toBeTruthy()

    vm.queued = [{ text: 'queued', images: [] }]
    vm.cancel()
    expect(vm.queued).toEqual([])
    expect(socket!.send).toHaveBeenLastCalledWith(JSON.stringify({ type: 'cancel' }))

    vi.useFakeTimers()
    socket!.onerror?.()
    expect(vm.status).toBe('error')
    const dropped = socket!
    dropped.onclose?.()
    expect(vm.status).toBe('error')
    vi.advanceTimersByTime(1000)
    expect(socket).not.toBe(dropped)
    socket!.onopen?.()
    expect(vm.status).toBe('ready')
    socket!.onclose?.()
    expect(vm.status).toBe('closed')
    const waiting = socket
    w.unmount()
    vi.advanceTimersByTime(30_000)
    expect(socket).toBe(waiting)
  })

  it('ignores invalid sends/frames and rejects oversized attachments', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.send()
    vm.onFrame('not-json')
    expect(vm.turns).toEqual([])

    vm.status = 'starting'
    vm.openWs(1)
    socket!.onopen?.()
    vm.attachments = [{
      data: 'x',
      mimeType: 'application/octet-stream',
      url: '',
      name: 'huge.bin',
      size: 999999999,
    }]
    // Attachment size validation uses decoded data size, so a large base64 payload is required.
    vm.attachments = [{ data: 'a'.repeat(70 * 1024 * 1024), mimeType: 'application/octet-stream', url: '', name: 'huge.bin' }]
    vm.send()
    expect(vm.errorMsg).toContain('huge.bin')
    expect(socket!.send).not.toHaveBeenCalled()

    const turn = { role: 'agent', text: '', thought: '', tools: [], plan: [], streaming: true }
    vm.applyAcp({}, turn)
    vm.applyAcp({ type: 'other' }, turn)
    expect(vm.humanizeTool('工具')).toBe('工具')
    expect(vm.humanizeTool('tool_12345678')).toBe('')
    expect(vm.planEntries({ steps: 'bad' })).toEqual([])
    w.unmount()
  })

  it('reads files, removes drafts, and handles selection errors', async () => {
    class Reader {
      result = 'data:text/plain;base64,SGk='
      onload: (() => void) | null = null
      readAsDataURL() { queueMicrotask(() => this.onload?.()) }
    }
    vi.stubGlobal('FileReader', Reader)
    const w = mountTester()
    const vm = w.vm as any
    vm.addFiles(null)
    const good = new File(['hi'], 'note.txt', { type: '' })
    const huge = new File(['x'], 'huge.dat')
    Object.defineProperty(huge, 'size', { value: 60 * 1024 * 1024 })
    vm.addFiles([good, huge] as any)
    await flushPromises()
    expect(vm.attachments[0]).toMatchObject({ name: 'note.txt', mimeType: 'application/octet-stream', data: 'SGk=' })
    expect(vm.errorMsg).toContain('huge.dat')
    vm.removeAttachment(0)
    expect(vm.attachments).toEqual([])

    vm.status = 'ready'
    await flushPromises()
    const input = w.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [good] })
    await input.trigger('change')
    await flushPromises()
    expect((input.element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('pastes clipboard files with data URL and keeps prior selection errors', async () => {
    class Reader {
      result = ''
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type};base64,UA==`
        queueMicrotask(() => this.onload?.())
      }
    }
    class Transfer {
      files: File[] = []
      items = { add: (f: File) => this.files.push(f) }
    }
    vi.stubGlobal('FileReader', Reader)
    vi.stubGlobal('DataTransfer', Transfer)
    const w = mountTester()
    const vm = w.vm as any

    const huge = new File(['x'], 'huge.dat')
    Object.defineProperty(huge, 'size', { value: 60 * 1024 * 1024 })
    vm.addFiles([huge] as any)
    expect(vm.errorMsg).toContain('huge.dat')

    const textOnly = vi.fn()
    vm.onPaste({ preventDefault: textOnly, clipboardData: { items: [{ kind: 'string', getAsFile: () => null }] } })
    vm.onPaste({ preventDefault: textOnly, clipboardData: null })
    expect(textOnly).not.toHaveBeenCalled()

    const file = new File(['p'], 'shot.png', { type: 'image/png' })
    const preventDefault = vi.fn()
    vm.onPaste({
      preventDefault,
      clipboardData: { items: [{ kind: 'string', getAsFile: () => null }, { kind: 'file', getAsFile: () => file }] },
    })
    await flushPromises()
    expect(preventDefault).toHaveBeenCalled()
    expect(vm.attachments).toEqual([
      { data: 'UA==', mimeType: 'image/png', url: 'data:image/png;base64,UA==', name: 'shot.png' },
    ])
    expect(vm.errorMsg).toContain('huge.dat')
    w.unmount()
  })

  it('keeps provider errors visible after a live turn ends', async () => {
    const w = mountTester({ attachId: 7 })
    try {
      await flushPromises()
      socket!.onopen?.()
      await flushPromises()
      frame('turn_begin', { item: { id: 'failed', text: 'hi', images: [] } })
      frame('acp', { data: { op: 'event', data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: 'Partial reply' } } } })
      frame('acp', { data: { op: 'event', data: { type: 'error_text', text: 'Quota exhausted' } } })
      frame('acp', { data: { op: 'event', data: { type: 'prompt_done', stopReason: 'failed' } } })
      frame('turn_done')
      await flushPromises()
      expect((w.vm as any).turns[1]).toMatchObject({ text: 'Partial reply', error: 'Quota exhausted', streaming: false })
      expect(w.text()).toContain('Quota exhausted')
    } finally {
      w.unmount()
    }
  })

  it.each(['failed', 'refusal'])('restores an otherwise empty provider failure from %s history', async (stopReason) => {
    mocks.eventLog.mockResolvedValueOnce({ events: [
      { op: 'event', data: { type: 'prompt_begin', text: 'hi' } },
      { op: 'event', data: { type: 'prompt_done', stopReason } },
    ] })
    const w = mountTester()
    try {
      await (w.vm as any).restoreHistory(7)
      ;(w.vm as any).sandbox = { id: 7, status: 'running' }
      ;(w.vm as any).status = 'ready'
      await flushPromises()
      expect((w.vm as any).turns).toHaveLength(2)
      expect((w.vm as any).turns[1]).toMatchObject({ error: pages.pages.agentChatTester.execError, streaming: false })
      expect(w.text()).toContain(pages.pages.agentChatTester.execError)
    } finally {
      w.unmount()
    }
  })

  it('restores paged history, loads earlier events, and tolerates failures', async () => {
    const newer = [
      { op: 'event', data: { type: 'prompt_begin', promptText: 'new', imageURLs: ['https://x/a.png', '', 1] } },
      { type: 'session_update', update: { type: 'agent_message_chunk', content: 'answer' } },
    ]
    mocks.eventLog.mockResolvedValueOnce({ events: newer, hasMore: true, nextCursor: 'c1' })
    const w = mountTester()
    const vm = w.vm as any
    await vm.restoreHistory(9)
    expect(vm.turns[0].text).toBe('new')
    expect(vm.turns[1].text).toBe('answer')
    expect(vm.historyHasMore).toBe(true)

    mocks.eventLog.mockResolvedValueOnce({
      events: [{ type: 'prompt_begin', text: 'old' }],
      hasMore: false,
      nextCursor: '',
    })
    vm.sandbox = { id: 9 }
    await vm.loadEarlierHistory()
    expect(mocks.eventLog).toHaveBeenLastCalledWith(9, { cursor: 'c1', limit: 20 })
    expect(vm.turns[0].text).toBe('old')

    mocks.eventLog.mockRejectedValueOnce(new Error('offline'))
    vm.historyHasMore = true
    await vm.loadEarlierHistory()
    expect(vm.loadingEarlier).toBe(false)
    vm.turns = []
    mocks.eventLog.mockRejectedValueOnce(new Error('offline'))
    await vm.restoreHistory(9)
    expect(vm.restoring).toBe(false)

    expect(vm.rebuildTurnsFromFrames([{ type: 'session_update', update: { type: 'agent_thought_chunk', content: '' } }])).toEqual([])
    const wrapped = vm.rebuildTurnsFromFrames([
      { op: 'event', data: { type: 'prompt_begin', text: 'q' } },
      { op: 'event', data: { type: 'session_update', update: { sessionUpdate: 'agentThoughtChunk', content: { parts: ['a', { text: 'b' }] } } } },
      { op: 'event', data: { type: 'session_update', update: { kind: 'tool_call', id: 't9', title: 'run', status: 'pending' } } },
      { op: 'event', data: { type: 'session_update', update: { session_update: 'agent_message_chunk', content: ['x', { text: 'y' }] } } },
    ])
    expect(wrapped).toHaveLength(2)
    expect(wrapped[1]).toMatchObject({ role: 'agent', thought: 'ab', text: 'xy' })
    expect(wrapped[1].tools[0]).toMatchObject({ id: 't9', status: 'pending' })
    w.unmount()
  })

  it('previews only the last streamed message block across intervening tools and thoughts', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.beginTurn({ item: { id: 'question', text: '检查需求' } })
    const update = (data: Record<string, unknown>) => vm.onFrame(JSON.stringify({ type: 'acp', data: { type: 'session_update', update: data } }))
    update({ type: 'agent_message_chunk', content: '先检查已有产物。' })
    update({ type: 'tool_call', toolCallId: 'read', title: '读取文件' })
    update({ type: 'agent_message_chunk', content: '**最终结论**。\n\n请补充' })
    await flushPromises()
    const rail = w.getComponent(ChatMessageRail)
    const id = rail.props('items')[0]!.id
    expect(rail.props('items')[0]!.preview).toBe('**最终结论**。\n\n请补充')
    update({ type: 'agent_message_chunk', content: '现象与期望。' })
    update({ type: 'agent_thought_chunk', content: '内部过程' })
    await flushPromises()
    expect(rail.props('items')[0]!).toEqual({ id, title: '检查需求', preview: '**最终结论**。\n\n请补充现象与期望。' })
    vm.beginTurn({ item: { id: 'next', text: '另一个问题' } })
    await flushPromises()
    expect(rail.props('items')[0]!.preview).toContain('最终结论')
    expect(rail.props('items')[1]!.preview).toBeUndefined()
    w.unmount()
  })

  it('keeps question anchors and the streaming reply when earlier history is prepended', async () => {
    mocks.eventLog.mockResolvedValueOnce({
      events: [
        { type: 'prompt_begin', opId: 'saved-question', text: 'same question' },
        { type: 'session_update', update: { type: 'agent_message_chunk', content: 'saved answer' } },
      ], hasMore: true, nextCursor: 'older',
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'ready'
    vm.sandbox = { id: 7 }
    await vm.restoreHistory(7)
    vm.beginTurn({ item: { id: 'live-question', text: 'live question' } })
    vm.onFrame(JSON.stringify({ type: 'acp', data: { type: 'session_update', update: { type: 'agent_message_chunk', content: 'live answer' } } }))
    await flushPromises()
    const originalIds = vm.turns.map((turn: any) => turn.id)
    const reply = vm.turns.at(-1)
    const scroller = w.get('[data-testid="tester-transcript"]').element as HTMLElement
    Object.defineProperties(scroller, {
      scrollHeight: { configurable: true, get: () => vm.turns.length * 100 },
      clientHeight: { configurable: true, value: 100 },
    })
    scroller.scrollTop = 120
    mocks.eventLog.mockResolvedValueOnce({
      events: [
        { type: 'prompt_begin', text: 'same question' },
        { type: 'session_update', update: { type: 'agent_message_chunk', content: 'first old answer' } },
        { type: 'prompt_begin', text: 'same question' },
        { type: 'session_update', update: { type: 'agent_message_chunk', content: 'second old answer' } },
      ], hasMore: false, nextCursor: '',
    })
    await vm.loadEarlierHistory()
    expect(vm.turns.slice(-4).map((turn: any) => turn.id)).toEqual(originalIds)
    expect(vm.turns.at(-1)).toBe(reply)
    expect(scroller.scrollTop).toBe(520)
    const questions = vm.turns.filter((turn: any) => turn.role === 'user')
    expect(new Set(questions.map((turn: any) => turn.id)).size).toBe(4)
    expect(questions.at(-1).id).toBe('question:live-question')
    expect(w.findAll('[data-chat-message-id]').map(el => el.attributes('data-chat-message-id'))).toEqual(questions.map((turn: any) => turn.id))
    const rail = w.getComponent(ChatMessageRail)
    expect(rail.props('items').map(item => item.id)).toEqual(questions.map((turn: any) => turn.id))
    expect(rail.props('items').at(-1)?.preview).toBe('live answer')

    rail.vm.$emit('navigate', questions[0].id)
    vm.onFrame(JSON.stringify({ type: 'acp', data: { type: 'session_update', update: { type: 'agent_message_chunk', content: ' continued' } } }))
    await flushPromises()
    expect(reply.text).toBe('live answer continued')
    expect(scroller.scrollTop).toBe(520)
    expect(vm.followBottom).toBe(false)
    // First open establishes the sandbox identity; the second is a real reconnect.
    vm.openWs(7)
    socket!.onopen?.()
    await flushPromises()
    vm.openWs(7)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.turns.at(-1)).toBe(reply)
    expect(vm.activeIdx).toBe(vm.turns.length - 1)
    expect(() => vm.beginTurn({ item: { id: 'live-question', text: 'live question' } })).not.toThrow()
    expect(vm.turns.filter((turn: any) => turn.id === 'question:live-question')).toHaveLength(1)

    const earlierIds = vm.turns.slice(0, -2).map((turn: any) => turn.id)
    mocks.eventLog.mockResolvedValueOnce({
      events: [
        { type: 'prompt_begin', opId: 'live-question', promptText: 'live question' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'live final' } } },
        { type: 'prompt_done' },
      ],
      hasMore: true,
      nextCursor: 'older',
    })
    vm.openWs(7)
    socket!.onopen?.()
    frame('queue_state', { busy: false, items: [] })
    await flushPromises()
    expect(vm.turns.slice(0, -2).map((turn: any) => turn.id)).toEqual(earlierIds)
    expect(vm.turns.slice(-2).map((turn: any) => turn.text)).toEqual(['live question', 'live final'])
    expect(vm.activeIdx).toBe(-1)
    vm.input = 'next question'
    vm.send()
    await flushPromises()
    expect(vm.followBottom).toBe(true)
    expect(scroller.scrollTop).toBe(scroller.scrollHeight)
    w.unmount()
  })

  it('resets and destroys a sandbox even when deletion fails', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(11)
    vm.sandbox = { id: 11, name: 'sb' }
    vm.confirmDestroy = true
    mocks.destroy.mockRejectedValueOnce(new Error('gone'))
    await vm.destroy()
    expect(mocks.destroy).toHaveBeenCalledWith(11)
    expect(vm.status).toBe('idle')
    expect(vm.confirmDestroy).toBe(false)

    vm.openWs(12)
    await w.setProps({ profile: 'claude' })
    expect(vm.status).toBe('idle')
    w.unmount()
  })

  it('reuses a restored active turn when its replay arrives after history', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'srv-late', promptText: 'live' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'par' } } },
      ],
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.turns.map((t: any) => t.text)).toEqual(['live', 'par'])

    frame('queue_state', { busy: true, activeItem: { id: 'srv-late', text: 'live' }, items: [] })
    frame('turn_begin', { item: { id: 'srv-late', text: 'live' } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial' } } } })
    await flushPromises()
    expect(vm.turns.map((t: any) => t.text)).toEqual(['live', 'partial'])
    expect(vm.turns.filter((t: any) => t.id === 'question:srv-late')).toHaveLength(1)
    expect(vm.turns.at(-1).streaming).toBe(true)
    w.unmount()
  })

  it('keeps a richer live partial only while restored history is still active', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'active', promptText: 'live' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'par' } } },
      ],
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.turns = [
      { id: 'question:active', role: 'user', text: 'live', thought: '', tools: [], plan: [], streaming: false },
      { id: 'question:active:reply', role: 'agent', text: 'partial longer', thought: '', tools: [], plan: [], streaming: true },
    ]
    vm.activeIdx = 1
    vm.activeItemId = 'active'
    vm.wsSandboxId = 5
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.turns.at(-1)).toMatchObject({ text: 'partial longer', streaming: true })

    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'active', promptText: 'live' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'done' } } },
        { type: 'prompt_done' },
      ],
    })
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.turns.at(-1)).toMatchObject({ text: 'done', streaming: false })
    w.unmount()
  })

  it('reconciles a legacy active prompt that has no reply or opId yet', async () => {
    mocks.eventLog.mockResolvedValue({ events: [{ type: 'prompt_begin', promptText: 'live' }] })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.turns = [
      { id: 'question:legacy', role: 'user', text: 'live', thought: '', tools: [], plan: [], streaming: false },
      { id: 'question:legacy:reply', role: 'agent', text: '', thought: '', tools: [], plan: [], streaming: true },
    ]
    vm.activeIdx = 1
    vm.activeItemId = 'legacy'
    vm.wsSandboxId = 5
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.turns.map((t: any) => t.role)).toEqual(['user', 'agent'])
    expect(vm.turns.filter((t: any) => t.role === 'user' && t.text === 'live')).toHaveLength(1)
    expect(vm.activeIdx).toBe(1)

    frame('turn_begin', { item: { id: 'legacy', text: 'live' } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial' } } } })
    expect(vm.turns.map((t: any) => t.text)).toEqual(['live', 'partial'])
    w.unmount()
  })

  it('replays a running turn after reconnecting without duplicating it', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', promptText: 'old' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'done' } } },
        { type: 'prompt_begin', opId: 'srv-1', promptText: 'live' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'par' } } },
      ],
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: true, waiting: 1, activeItem: { id: 'srv-1', text: 'live' }, items: [{ id: 'srv-2', text: 'next', images: [{ data: 'QQ==', mimeType: 'image/png' }] }] })
    frame('turn_begin', { item: { id: 'srv-1', text: 'live', images: [] } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial' } } } })
    await flushPromises()
    expect(vm.turns.map((t: any) => t.text)).toEqual(['old', 'done', 'live', 'partial'])
    expect(vm.status).toBe('thinking')
    expect(vm.queued[0]).toMatchObject({ id: 'srv-2', text: 'next' })
    expect(vm.queued[0].images[0].url).toBe('data:image/png;base64,QQ==')

    // Reconnect mid-turn: the replay rebuilds the same reply in place.
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: true, waiting: 0, activeItem: { id: 'srv-1', text: 'live' }, items: [] })
    frame('turn_begin', { item: { id: 'srv-1', text: 'live' } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial more' } } } })
    await flushPromises()
    expect(vm.turns.map((t: any) => t.text)).toEqual(['old', 'done', 'live', 'partial more'])

    // The turn finished while disconnected: idle must reconcile the event log,
    // not merely stop the old partial bubble's streaming caret.
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', promptText: 'old' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'done' } } },
        { type: 'prompt_begin', opId: 'srv-1', promptText: 'live' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial more final' } } },
        { type: 'prompt_done', stopReason: 'end_turn' },
      ],
    })
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: false, waiting: 0, items: [] })
    await flushPromises()
    expect(vm.turns.map((t: any) => t.text)).toEqual(['old', 'done', 'live', 'partial more final'])
    expect(vm.turns.filter((t: any) => t.id === 'question:srv-1')).toHaveLength(1)
    expect(vm.turns.at(-1).streaming).toBe(false)
    expect(vm.status).toBe('ready')
    frame('error', { message: '消息队列已满，请稍候' })
    expect(vm.errorMsg).toBe('消息队列已满，请稍候')
    socket!.onmessage?.(new MessageEvent('message', { data: JSON.stringify({ type: 'other' }) }))
    w.unmount()
  })

  it.each(['turn_done', 'error'])('keeps %s final when a delayed cancel queue frame arrives afterward', async (terminal) => {
    const w = mountTester()
    const vm = w.vm as any
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    frame('turn_begin', { sessionGeneration: 8, revision: 3, item: { id: 'A', text: 'first' } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial answer' } } } })
    frame(terminal, { sessionGeneration: 8, revision: 5, item: { id: 'A' }, ...(terminal === 'error' ? { message: 'stopped' } : {}) })
    // Cancel captured this busy state before A ended, but its publisher was delayed.
    frame('queue_state', { sessionGeneration: 8, revision: 4, busy: true, waiting: 1, activeItem: { id: 'A', text: 'first' }, items: [{ id: 'A', text: 'first' }] })
    frame('turn_begin', { sessionGeneration: 8, revision: 3, item: { id: 'A', text: 'first' } })
    await flushPromises()
    expect(vm.status).toBe('ready')
    expect(vm.activeIdx).toBe(-1)
    expect(vm.queued).toEqual([])
    expect(vm.turns.map((turn: any) => turn.text)).toEqual(['first', 'partial answer'])
    expect(vm.turns.at(-1).streaming).toBe(false)

    // A new live turn must still start after the stale frames were rejected.
    frame('turn_begin', { sessionGeneration: 8, revision: 6, item: { id: 'B', text: 'second' } })
    expect(vm.status).toBe('thinking')
    expect(vm.activeItemId).toBe('B')
    w.unmount()
  })

  it('restores a fresh subscription head after a server restart without requiring its older replay begin', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    frame('turn_begin', { sessionGeneration: 8, revision: 3, item: { id: 'A', text: 'first' } })
    frame('turn_done', { sessionGeneration: 8, revision: 4, item: { id: 'A' } })

    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { sessionGeneration: 1, revision: 6, busy: true, waiting: 0, activeItem: { id: 'B', text: 'second' }, items: [] })
    frame('turn_begin', { sessionGeneration: 1, revision: 3, item: { id: 'B', text: 'second' } })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'running after restart' } } } })
    await flushPromises()
    expect(vm.activeItemId).toBe('B')
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B')).toHaveLength(1)
    expect(vm.turns.at(-1).text).toBe('running after restart')
    expect(vm.status).toBe('thinking')
    frame('turn_done', { sessionGeneration: 1, revision: 7, item: { id: 'B' } })
    await flushPromises()
    expect(vm.status).toBe('ready')
    w.unmount()
  })

  it('shows an unversioned queue-control refusal without stopping the active versioned turn', async () => {
    const w = mountTester()
    const vm = w.vm as any
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    frame('queue_state', { sessionGeneration: 8, revision: 3, busy: true, waiting: 0, activeItem: { id: 'A', text: 'first' }, items: [] })
    frame('error', { message: 'cannot remove active item' })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'still running' } } } })
    await flushPromises()
    expect(vm.errorMsg).toBe('cannot remove active item')
    expect(vm.status).toBe('thinking')
    expect(vm.activeItemId).toBe('A')
    expect(vm.turns.at(-1)).toMatchObject({ text: 'still running', streaming: true })
    expect(vm.turns.at(-1).error).toBeUndefined()
    frame('turn_done', { sessionGeneration: 8, revision: 4, item: { id: 'A' } })
    expect(vm.status).toBe('ready')
    w.unmount()
  })

  it('keeps a completed same-text turn when the event log lags the new busy turn', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'old', promptText: '继续' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: '旧回复' } } },
        { type: 'prompt_done' },
      ],
      hasMore: false,
      nextCursor: '',
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.wsSandboxId = 5
    vm.turns = [
      { id: 'question:old', role: 'user', text: '继续', thought: '', tools: [], plan: [], streaming: false },
      { id: 'question:old:reply', role: 'agent', text: '旧回复', thought: '', tools: [], plan: [], streaming: false },
    ]
    vm.activeIdx = -1
    vm.activeItemId = ''
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: true, activeItem: { id: 'new', text: '继续' }, items: [] })
    frame('turn_begin', { item: { id: 'new', text: '继续' } })
    await flushPromises()
    expect(vm.turns.filter((turn: any) => turn.role === 'user').map((turn: any) => turn.id)).toEqual(['question:old', 'question:new'])
    expect(vm.turns.some((turn: any) => turn.id === 'question:old:reply' && turn.text === '旧回复')).toBe(true)
    w.unmount()
  })

  it('does not duplicate B when A finishes and B starts while disconnected', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'A', promptText: '问题 A' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'A 最终回复' } } },
        { type: 'prompt_done' },
        { type: 'prompt_begin', opId: 'B', promptText: '问题 B' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'B 部分回复' } } },
      ],
      hasMore: false,
      nextCursor: '',
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.wsSandboxId = 5
    vm.turns = [
      { id: 'question:A', role: 'user', text: '问题 A', thought: '', tools: [], plan: [], streaming: false },
      { id: 'question:A:reply', role: 'agent', text: 'A 部分', thought: '', tools: [], plan: [], streaming: true },
    ]
    vm.activeIdx = 1
    vm.activeItemId = 'A'
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: true, activeItem: { id: 'B', text: '问题 B' }, items: [] })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: '续写' } } } })
    await flushPromises()
    expect(vm.turns.filter((turn: any) => turn.id === 'question:A')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:A:reply' && turn.text === 'A 最终回复')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B')).toHaveLength(1)
    expect(vm.turns.find((turn: any) => turn.id === 'question:B:reply')?.text).toContain('B 部分回复')
    expect(vm.turns.find((turn: any) => turn.id === 'question:B:reply')?.text).toContain('续写')
    expect(vm.activeItemId).toBe('B')

    frame('turn_begin', { item: { id: 'B', text: '问题 B' } })
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B:reply')).toHaveLength(1)
    expect(vm.turns.find((turn: any) => turn.id === 'question:B:reply')?.text).toContain('B 部分回复')
    expect(vm.turns.find((turn: any) => turn.id === 'question:B:reply')?.text).toContain('续写')
    expect(vm.activeItemId).toBe('B')
    w.unmount()
  })

  it('keeps the event-log prefix and ACP when turn_begin shares the reconnect flush', async () => {
    mocks.eventLog.mockResolvedValue({
      events: [
        { type: 'prompt_begin', opId: 'A', promptText: '问题 A' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'A 最终回复' } } },
        { type: 'prompt_done' },
        { type: 'prompt_begin', opId: 'B', promptText: '问题 B' },
        { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'B 部分回复' } } },
      ],
      hasMore: false,
      nextCursor: '',
    })
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.wsSandboxId = 5
    vm.turns = [
      { id: 'question:A', role: 'user', text: '问题 A', thought: '', tools: [], plan: [], streaming: false },
      { id: 'question:A:reply', role: 'agent', text: 'A 部分', thought: '', tools: [], plan: [], streaming: true },
    ]
    vm.activeIdx = 1
    vm.activeItemId = 'A'
    vm.openWs(5)
    socket!.onopen?.()
    frame('queue_state', { busy: true, activeItem: { id: 'B', text: '问题 B' }, items: [] })
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: '续写' } } } })
    frame('turn_begin', { item: { id: 'B', text: '问题 B' } })
    await flushPromises()
    const reply = vm.turns.find((turn: any) => turn.id === 'question:B:reply')
    expect(reply?.text).toContain('B 部分回复')
    expect(reply?.text).toContain('续写')
    expect(reply?.text).not.toBe('')
    expect(vm.turns.filter((turn: any) => turn.id === 'question:A')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:A:reply' && turn.text === 'A 最终回复')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B')).toHaveLength(1)
    expect(vm.turns.filter((turn: any) => turn.id === 'question:B:reply')).toHaveLength(1)
    expect(vm.activeItemId).toBe('B')
    w.unmount()
  })

  it('clears restoring when history fails and ignores earlier pages during restore', async () => {
    mocks.eventLog.mockRejectedValueOnce(new Error('log down'))
    const w = mountTester()
    const vm = w.vm as any
    vm.status = 'starting'
    vm.wsSandboxId = 5
    vm.turns = [
      { id: 'question:old', role: 'user', text: '继续', thought: '', tools: [], plan: [], streaming: false },
    ]
    vm.openWs(5)
    socket!.onopen?.()
    await flushPromises()
    expect(vm.restoring).toBe(false)

    let release!: (value: unknown) => void
    mocks.eventLog.mockImplementation((_id: number, opts?: { cursor?: string }) => {
      if (opts?.cursor) return Promise.resolve({ events: [], hasMore: false, nextCursor: '' })
      return new Promise((resolve) => { release = resolve })
    })
    vm.historyHasMore = true
    vm.historyCursor = 'c1'
    vm.sandbox = { id: 5 }
    vm.openWs(5)
    socket!.onopen?.()
    const earlier = vm.loadEarlierHistory()
    expect(mocks.eventLog.mock.calls.some((call: unknown[]) => (call[1] as { cursor?: string } | undefined)?.cursor)).toBe(false)
    release({
      events: [
        { type: 'prompt_begin', opId: 'old', promptText: '继续' },
        { type: 'prompt_done' },
      ],
      hasMore: true,
      nextCursor: 'c2',
    })
    await earlier
    await flushPromises()
    expect(vm.restoring).toBe(false)
    expect(vm.loadingEarlier).toBe(false)
    w.unmount()
  })
})
