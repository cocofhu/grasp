// @vitest-environment happy-dom
import { shallowMount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/en/common.json'
import pages from '@/locales/en/pages.json'
import type { PreviewChatHistory } from '@/lib/inbox/appPreviewShare'
import { EMBED_IMAGE_PREVIEW_KEY } from '@/lib/inbox/embedImagePreview'

const client = vi.hoisted(() => ({
  session: vi.fn(), history: vi.fn(), getModel: vi.fn(), connectMain: vi.fn(), connectGuest: vi.fn(),
  modelCatalog: vi.fn(), setModel: vi.fn(), searchReferences: vi.fn(), image: vi.fn(), send: vi.fn(), cancelMain: vi.fn(),
}))
vi.mock('@/lib/inbox/appPreviewShare', () => ({ publicAppPreviewClient: () => client }))

import SharedAppPreviewChat from './SharedAppPreviewChat.vue'

class Socket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3
  readyState = Socket.CONNECTING
  onmessage?: (event: { data: string }) => void
  onclose?: () => void
  onerror?: () => void
  onopen?: () => void
  send = vi.fn()
  open() {
    this.readyState = Socket.OPEN
    this.onopen?.()
  }
  close() {
    if (this.readyState === Socket.CLOSED) return
    this.readyState = Socket.CLOSED
    this.onclose?.()
  }
  receive(frame: object) {
    this.readyState = Socket.OPEN
    this.onmessage?.({ data: JSON.stringify(frame) })
  }
}

let sockets: Socket[]
const wrappers: ReturnType<typeof shallowMount>[] = []
function mountChat(provide: Record<string | symbol, unknown> = {}) {
  const w = shallowMount(SharedAppPreviewChat, {
    props: { token: 'shared-app' },
    global: { provide,
      stubs: { ChatComposer: { name: 'ChatComposer', props: ['draft'], emits: ['send'], template: '<div><slot name="actions" /></div>' } },
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { ...common, ...pages } } })] },
  })
  wrappers.push(w)
  return w
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('WebSocket', Socket)
  vi.resetAllMocks()
  sockets = []
  client.session.mockResolvedValue({ ticket: 'ticket', chatMode: 'main' })
  client.history.mockResolvedValue({ rows: [], busy: false })
  client.getModel.mockResolvedValue({})
  client.cancelMain.mockResolvedValue({ ok: true })
  client.connectMain.mockImplementation(() => {
    const socket = new Socket()
    sockets.push(socket)
    return socket
  })
  client.connectGuest.mockImplementation(() => {
    const socket = new Socket()
    sockets.push(socket)
    return socket
  })
})
afterEach(() => {
  for (const w of wrappers.splice(0)) w.unmount()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('shared main chat recovery', () => {
  it('stops the main conversation without discarding a draft or assuming it has already stopped', async () => {
    let finish!: (value: object) => void
    client.cancelMain.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const w = mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], busy: true } })
    await flushPromises()
    const composer = w.getComponent({ name: 'ChatComposer' })
    composer.props('draft').input.value = 'Keep this draft'
    const stop = () => w.findAllComponents({ name: 'ComposerPrimaryAction' }).find(button => button.props('kind') === 'stop')
    expect(stop()!.props('disabled')).toBe(false)
    stop()!.vm.$emit('click', 'stop')
    await flushPromises()
    expect(client.cancelMain).toHaveBeenCalledOnce()
    expect(stop()!.props('disabled')).toBe(true)
    finish({ ok: true })
    await flushPromises()
    expect(stop()).toBeDefined() // The authoritative stream still reports busy.
    expect(composer.props('draft').input.value).toBe('Keep this draft')
    sockets[0]!.receive({ type: 'tail', start: 0, rows: [], queue: [], busy: false })
    await flushPromises()
    expect(stop()).toBeUndefined()
    expect(composer.props('draft').input.value).toBe('Keep this draft')
  })

  it('keeps a failed main stop retryable', async () => {
    client.cancelMain.mockRejectedValueOnce({ status: 503 })
    const w = mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], busy: true } })
    await flushPromises()
    const stop = w.findAllComponents({ name: 'ComposerPrimaryAction' }).find(button => button.props('kind') === 'stop')!
    stop.vm.$emit('click', 'stop')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('Could not stop the reply')
    expect(stop.props('disabled')).toBe(false)
  })

  it('cancels only the current independent turn, preserves partial content and can send again on the same socket', async () => {
    client.session.mockResolvedValue({ ticket: 'guest', chatMode: 'isolated' })
    const w = mountChat()
    await flushPromises()
    const socket = sockets[0]!
    socket.open()
    const composer = w.getComponent({ name: 'ChatComposer' })
    composer.props('draft').input.value = 'First request'
    composer.vm.$emit('send')
    await flushPromises()
    const request = JSON.parse(socket.send.mock.calls[0]![0])
    socket.receive({ type: 'reply', text: 'Partial reply' })
    const stop = () => w.findAllComponents({ name: 'ComposerPrimaryAction' }).find(button => button.props('kind') === 'stop')
    await flushPromises()
    composer.props('draft').input.value = 'Next request'
    stop()!.vm.$emit('click', 'stop')
    await flushPromises()
    expect(JSON.parse(socket.send.mock.calls[1]![0])).toEqual({ type: 'cancel', id: request.id })
    expect(stop()!.props('disabled')).toBe(true)
    socket.receive({ type: 'done', text: 'Partial reply', interrupted: true, failed: true })
    await flushPromises()
    expect(stop()).toBeUndefined()
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.getComponent({ name: 'AgentTimeline' }).props()).toMatchObject({ text: 'Partial reply', interrupted: true, streaming: false })
    expect(composer.props('draft').input.value).toBe('Next request')
    composer.vm.$emit('send')
    await flushPromises()
    const next = JSON.parse(socket.send.mock.calls[2]![0])
    expect(next.text).toBe('Next request')
    expect(next.id).not.toBe(request.id)
    expect(client.connectGuest).toHaveBeenCalledOnce()
  })

  it('synchronizes the pending queue across seeds, token tails, reorders and removals', async () => {
    let finishSeed!: (history: PreviewChatHistory) => void
    client.history.mockReturnValueOnce(new Promise<PreviewChatHistory>(resolve => { finishSeed = resolve }))
    const w = mountChat()
    await flushPromises()
    const queue = [{ id: 'q1', text: 'First', images: [{ mimeType: 'image/png' }] }, { id: 'q2', text: 'Second' }]
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], queue, busy: true } })
    await flushPromises()
    const panel = () => w.getComponent({ name: 'PendingSendQueuePanel' })
    expect(panel().props('items').map((item: { id: string }) => item.id)).toEqual(['q1', 'q2'])
    expect(panel().props('items')[0].images).toHaveLength(1)
    expect(panel().props('readonly')).toBe(true)
    sockets[0]!.receive({ type: 'tail', start: 0, rows: [], busy: true })
    await flushPromises()
    expect(panel().props('items')).toHaveLength(2)
    sockets[0]!.receive({ type: 'tail', start: 0, rows: [], queue: [...queue].reverse(), busy: true })
    finishSeed({ rows: [], queue: [], busy: false })
    await flushPromises()
    expect(panel().props('items').map((item: { id: string }) => item.id)).toEqual(['q2', 'q1'])
    sockets[0]!.receive({ type: 'tail', start: 0, rows: [], queue: [], busy: false })
    await flushPromises()
    expect(w.findComponent({ name: 'PendingSendQueuePanel' }).exists()).toBe(false)
  })

  it('passes recorded reply statistics through without using current model settings', async () => {
    const recorded = { id: 'a', role: 'assistant', text: 'Historical reply', at: '2026-11-04T07:00:34Z',
      execution: { agentName: 'Recorded Agent', backend: 'opencode', modelId: 'old-model' },
      usage: { inputTokens: 123, outputTokens: 45, cacheReadTokens: 0, cacheWriteTokens: 0 }, durationMs: 34000 }
    client.history.mockResolvedValue({ rows: [recorded], busy: false, model: { currentModelId: 'new-model' } })
    const w = mountChat()
    await flushPromises()
    expect(w.getComponent({ name: 'AgentTimeline' }).props('metadata')).toEqual(recorded)
  })

  it('opens attachments through the host preview instead of a modal inside the chat frame', async () => {
    const host = { open: vi.fn(), close: vi.fn() }
    client.image.mockResolvedValue(new Blob(['picture'], { type: 'image/png' }))
    client.history.mockResolvedValue({ rows: [{ id: 'u', role: 'user', text: 'Image', images: [{ mimeType: 'image/png', name: 'Screenshot.png' }] }], busy: false })
    const w = mountChat({ [EMBED_IMAGE_PREVIEW_KEY as symbol]: host })
    await flushPromises()
    w.getComponent({ name: 'ChatAttachments' }).vm.$emit('preview', 'blob:picture', 'Screenshot.png')
    await flushPromises()
    expect(host.open).toHaveBeenCalledWith('blob:picture', 'Screenshot.png')
    expect(w.getComponent({ name: 'ChatImagePreviewModal' }).props('open')).toBe(false)
  })

  it('backs off failed connections up to thirty seconds without blocking ticket renewal', async () => {
    mountChat()
    await flushPromises()
    for (const delay of [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]) {
      const count = sockets.length
      sockets.at(-1)!.close()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(delay - 1)
      expect(sockets).toHaveLength(count)
      await vi.advanceTimersByTimeAsync(1)
      expect(sockets).toHaveLength(count + 1)
    }
    expect(client.session).toHaveBeenCalledTimes(2)
  })

  it('keeps backing off when each snapshot is immediately followed by a stream error', async () => {
    mountChat()
    await flushPromises()
    for (const delay of [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]) {
      const count = sockets.length
      sockets.at(-1)!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
      sockets.at(-1)!.receive({ type: 'error', status: 503 })
      await flushPromises()
      await vi.advanceTimersByTimeAsync(delay - 1)
      expect(sockets).toHaveLength(count)
      await vi.advanceTimersByTimeAsync(1)
      expect(sockets).toHaveLength(count + 1)
    }
  })

  it('resets the delay only after five healthy seconds, including across streamed updates', async () => {
    mountChat()
    await flushPromises()
    sockets[0]!.close()
    await vi.advanceTimersByTimeAsync(1_000)
    sockets[1]!.close()
    await vi.advanceTimersByTimeAsync(2_000)
    sockets[2]!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
    await vi.advanceTimersByTimeAsync(4_999)
    sockets[2]!.close()
    await vi.advanceTimersByTimeAsync(3_999)
    expect(sockets).toHaveLength(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets).toHaveLength(4)
    sockets[3]!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
    await vi.advanceTimersByTimeAsync(2_500)
    sockets[3]!.receive({ type: 'tail', start: 0, rows: [], busy: false })
    await vi.advanceTimersByTimeAsync(2_500)
    sockets[3]!.close()
    await vi.advanceTimersByTimeAsync(999)
    expect(sockets).toHaveLength(4)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets).toHaveLength(5)
  })

  it('does not count time before a usable snapshot as a healthy connection', async () => {
    mountChat()
    await flushPromises()
    sockets[0]!.close()
    await vi.advanceTimersByTimeAsync(1_000)
    sockets[1]!.receive({ type: 'keepalive' })
    await vi.advanceTimersByTimeAsync(10_000)
    sockets[1]!.close()
    await vi.advanceTimersByTimeAsync(1_999)
    expect(sockets).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets).toHaveLength(3)
  })

  it.each([409, 410])('stops reconnecting and polling after HTTP %s', async status => {
    client.history.mockRejectedValueOnce({ status })
    mountChat()
    await flushPromises()
    expect(sockets[0]!.readyState).toBe(Socket.CLOSED)
    await vi.advanceTimersByTimeAsync(120_000)
    expect(client.connectMain).toHaveBeenCalledTimes(1)
    expect(client.history).toHaveBeenCalledTimes(1)
  })

  it('stops on a terminal stream error without waiting for another HTTP poll', async () => {
    const w = mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], queue: [{ id: 'queued', text: 'Waiting' }], busy: true } })
    sockets[0]!.receive({ type: 'error', status: 409 })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(client.connectMain).toHaveBeenCalledTimes(1)
    expect(client.getModel).not.toHaveBeenCalled()
    expect(w.findComponent({ name: 'PendingSendQueuePanel' }).exists()).toBe(false)
  })

  it('keeps streamed tails ahead of a delayed HTTP seed and supports truncation', async () => {
    let resolve!: (history: PreviewChatHistory) => void
    client.history.mockReturnValueOnce(new Promise<PreviewChatHistory>(done => { resolve = done }))
    const w = mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [
      { id: 'u', role: 'user', text: 'Question' }, { id: 'a', role: 'assistant', text: 'Partial', streaming: true },
    ], busy: true } })
    sockets[0]!.receive({ type: 'tail', start: 1, rows: [{ id: 'a', role: 'assistant', text: 'Complete' }], busy: false })
    resolve({ rows: [], busy: false })
    await flushPromises()
    expect(w.get('chat-user-message-stub').attributes('text')).toBe('Question')
    expect(w.get('agent-timeline-stub').attributes('text')).toBe('Complete')
    sockets[0]!.receive({ type: 'tail', start: 1, rows: [], busy: false })
    await flushPromises()
    expect(w.find('agent-timeline-stub').exists()).toBe(false)
    expect(w.get('chat-user-message-stub').attributes('text')).toBe('Question')
  })

  it('reconnects for a full snapshot if a tail cannot be applied', async () => {
    mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
    sockets[0]!.receive({ type: 'tail', start: 20, rows: [], busy: false })
    await vi.advanceTimersByTimeAsync(1_000)
    expect(client.connectMain).toHaveBeenCalledTimes(2)
  })
})
