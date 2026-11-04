// @vitest-environment happy-dom
import { shallowMount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/en/common.json'
import pages from '@/locales/en/pages.json'
import type { PreviewChatHistory } from '@/lib/inbox/appPreviewShare'

const client = vi.hoisted(() => ({
  session: vi.fn(), history: vi.fn(), getModel: vi.fn(), connectMain: vi.fn(), connectGuest: vi.fn(),
  modelCatalog: vi.fn(), setModel: vi.fn(), searchReferences: vi.fn(), image: vi.fn(), send: vi.fn(),
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
function mountChat() {
  const w = shallowMount(SharedAppPreviewChat, {
    props: { token: 'shared-app' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { ...common, ...pages } } })] },
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
  client.connectMain.mockImplementation(() => {
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

  it('resets the delay only after receiving a usable snapshot', async () => {
    mountChat()
    await flushPromises()
    sockets[0]!.close()
    await vi.advanceTimersByTimeAsync(1_000)
    sockets[1]!.close()
    await vi.advanceTimersByTimeAsync(2_000)
    sockets[2]!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
    sockets[2]!.close()
    await vi.advanceTimersByTimeAsync(1_000)
    expect(sockets).toHaveLength(4)
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
    mountChat()
    await flushPromises()
    sockets[0]!.receive({ type: 'snapshot', history: { rows: [], busy: false } })
    sockets[0]!.receive({ type: 'error', status: 409 })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(client.connectMain).toHaveBeenCalledTimes(1)
    expect(client.getModel).not.toHaveBeenCalled()
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
