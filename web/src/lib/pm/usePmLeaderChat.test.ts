// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  listPmThreads: vi.fn(),
  createPmThread: vi.fn(),
  deletePmThread: vi.fn(),
  listPmMessages: vi.fn(),
  patchPmMessage: vi.fn(),
  pmThreadChatWsUrl: vi.fn(() => 'ws://example.test/pm'),
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
      pmThreadChatWsUrl: mocks.pmThreadChatWsUrl,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

import { usePmLeaderChat } from './usePmLeaderChat'

const THREAD = {
  id: 'th-1',
  projectId: 'proj-1',
  userId: 'u1',
  agentName: 'pm',
  kind: 'pm',
  title: '会话',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function withChat() {
  let chat!: ReturnType<typeof usePmLeaderChat>
  const emit = vi.fn()
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      chat = usePmLeaderChat(
        { projectId: 'proj-1', binding: { enabled: true, agentConfigRef: 'pm', agentAvailable: true, aclNote: '' } },
        emit,
      )
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { chat, app, emit }
}

describe('usePmLeaderChat', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'WebSocket',
      class {
        onmessage = null
        onclose = null
        close() {}
      },
    )
    mocks.listPmThreads.mockResolvedValue({ items: [THREAD] })
    mocks.listPmMessages.mockResolvedValue({ items: [], hasMore: false })
    mocks.createPmThread.mockResolvedValue({ ...THREAD, id: 'th-2', title: '新会话' })
    mocks.deletePmThread.mockResolvedValue({ status: 'ok' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads threads and can start a new thread', async () => {
    const { chat, app } = withChat()
    await flushPromises()
    await nextTick()
    expect(chat.threads.value.length).toBeGreaterThan(0)
    expect(chat.loading.value).toBe(false)
    chat.relTime(THREAD.createdAt)
    chat.renderMarkdown('**x**')
    chat.attachmentDisplayName({ mimeType: 'image/png', dataUrl: 'data:image/png;base64,xx' }, 0)
    await chat.newThread()
    await flushPromises()
    chat.closeWs()
    app.unmount()
  })
})
