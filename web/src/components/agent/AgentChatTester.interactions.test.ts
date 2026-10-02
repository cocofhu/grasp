// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentChatTester from './AgentChatTester.vue'

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
    props: { profile: 'cursor', homeProjectId: ' project-1 ', ...props },
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
  socket!.onmessage?.(new MessageEvent('message', { data: JSON.stringify({ type, ...extra }) }))
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
      projectId: 'project-1',
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
    expect(vm.status).toBe('error')
    expect(vm.errorMsg).toBe('quota')
    w.unmount()

    const missing = mountTester()
    await (missing.vm as any).start()
    expect((missing.vm as any).status).toBe('error')
    expect((missing.vm as any).errorMsg).toBeTruthy()
    missing.unmount()
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
    frame('queue_state')
    frame('turn_begin')
    frame('acp', { data: { type: 'session_update', update: { sessionUpdate: 'agentMessageChunk', content: { text: 'Hello' } } } })
    frame('acp', { data: { type: 'session_update', update: { session_update: 'agent-thought-chunk', content: [{ text: 'think' }] } } })
    frame('acp', { data: { type: 'session_update', update: { type: 'plan', entries: [{ title: 'step', state: 'completed' }, {}] } } })
    frame('acp', { data: { type: 'session_update', update: { kind: 'tool_call', id: 't1', name: 'read_file', status: 'in_progress' } } })
    frame('acp', { data: { type: 'session_update', update: { kind: 'toolcall_update', id: 't1', title: 'Read done', state: 'completed' } } })
    await flushPromises()
    expect(vm.turns[1]).toMatchObject({ text: 'Hello', thought: 'think', streaming: true })
    expect(vm.turns[1].plan).toEqual([{ content: 'step', status: 'completed' }])
    expect(vm.turns[1].tools[0]).toMatchObject({ id: 't1', title: 'Read Done', status: 'completed' })
    frame('turn_done')
    expect(vm.status).toBe('ready')

    vm.input = 'again'
    vm.send()
    frame('turn_begin')
    frame('error', { message: 'agent failed' })
    expect(vm.turns.at(-1).error).toBe('agent failed')
    frame('error')
    expect(vm.errorMsg).toBeTruthy()

    vm.queued = [{ text: 'queued', images: [] }]
    vm.cancel()
    expect(vm.queued).toEqual([])
    expect(socket!.send).toHaveBeenLastCalledWith(JSON.stringify({ type: 'cancel' }))

    socket!.onerror?.()
    expect(vm.status).toBe('error')
    socket!.onclose?.()
    expect(vm.status).toBe('error')
    vm.status = 'ready'
    socket!.onclose?.()
    expect(vm.status).toBe('closed')
    w.unmount()
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
    expect(vm.contentText(null)).toBe('')
    expect(vm.contentText({ parts: ['a', { text: 'b' }] })).toBe('ab')
    expect(vm.contentText({ nope: true })).toBe('')
    expect(vm.normalizeKind('toolCall-Update')).toBe('tool_call_update')
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
    expect(vm.unwrapFrame(null)).toBeNull()
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
})
