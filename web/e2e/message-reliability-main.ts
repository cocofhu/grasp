import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { initClarifyTab } from '../src/lib/inbox/clarifyDraftStash'
import { usePmLeaderChat } from '../src/lib/pm/usePmLeaderChat'
import type { ClarifyTurn } from '../src/lib/shared/types'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import AgentChatTester from '../src/components/agent/AgentChatTester.vue'

initLocale()
setTheme('light')
void initClarifyTab()

type RawFrame = Record<string, unknown>
type HarnessWindow = Window & {
  __reliabilitySockets: MockWS[]
  __studioEvents: RawFrame[]
  __pmExternal: boolean
}

function json(data: unknown) {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

class MockWS {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = MockWS.CONNECTING
  onopen: (() => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    sockets.push(this)
    queueMicrotask(() => {
      if (this.readyState !== MockWS.CONNECTING) return
      this.readyState = MockWS.OPEN
      this.onopen?.()
    })
  }

  send() {}

  emit(frame: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(frame) }))
  }

  close() {
    if (this.readyState === MockWS.CLOSED) return
    this.readyState = MockWS.CLOSED
    this.onclose?.()
  }
}

const sockets: MockWS[] = []
const harness = window as HarnessWindow
harness.__reliabilitySockets = sockets
harness.__studioEvents = [
  { type: 'prompt_begin', opId: 'srv-1', promptText: '继续验证' },
  { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial' } } },
]
harness.__pmExternal = false
window.WebSocket = MockWS as unknown as typeof WebSocket

const oldPmMessages = [
  { id: 'u-old', role: 'user', content: '旧问题', status: 'ok', createdAt: '2026-01-01T00:00:00Z' },
  { id: 'a-old', role: 'assistant', content: '旧回答', status: 'ok', createdAt: '2026-01-01T00:00:01Z' },
]
const externalPmMessages = [
  ...oldPmMessages,
  { id: 'u-new', role: 'user', content: '外部问题', status: 'ok', createdAt: '2026-01-01T00:00:02Z' },
  { id: 'a-new', role: 'assistant', content: '断线期间的最终回答', status: 'ok', createdAt: '2026-01-01T00:00:03Z' },
]

window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const method = (init?.method || 'GET').toUpperCase()
  if (url.includes('/workspace/pm/threads') && !url.includes('/messages') && !url.includes('/draft') && method === 'GET') {
    return json({ items: [{ id: 'thr-race', title: '可靠性', agentAvailable: true }] })
  }
  if (url.includes('/workspace/pm/threads/thr-race/messages') && method === 'GET') {
    return json({ items: harness.__pmExternal ? externalPmMessages : oldPmMessages, hasMore: false })
  }
  if (url.includes('/sandboxes/7/eventlog')) {
    // 留出一个帧泵窗口，确保测试能向 restoreHistory 注入 held frames。
    await new Promise((resolve) => setTimeout(resolve, 30))
    return json({ events: harness.__studioEvents, nextCursor: '', hasMore: false })
  }
  if (/\/sandboxes\/7(?:\?|$)/.test(url) && method === 'GET') {
    return json({ id: 7, status: 'running', name: 'race-sandbox' })
  }
  if (url.includes('/draft')) return json({ draft: null, live: false, hasFinal: false })
  return json({})
}

type ClarifyExpose = {
  applyReviewFrame: (frame: RawFrame) => void
  applyAcpEvents: (events: { kind: string; text: string }[]) => void
}

const hosts = ['inbox', 'public', 'embed'] as const
const clarifyTurns = Object.fromEntries(hosts.map((host) => [host, ref<ClarifyTurn[]>([
  { role: 'human', text: '继续', at: '2026-01-01T00:00:00Z' },
  { role: 'agent', text: '上一轮回答', at: '2026-01-01T00:00:01Z' },
])])) as Record<(typeof hosts)[number], ReturnType<typeof ref<ClarifyTurn[]>>>
const clarifyRefs = Object.fromEntries(hosts.map((host) => [host, ref<ClarifyExpose | null>(null)])) as Record<
  (typeof hosts)[number],
  ReturnType<typeof ref<ClarifyExpose | null>>
>

async function startSameTextRound() {
  for (const host of hosts) {
    const chat = clarifyRefs[host].value
    chat?.applyReviewFrame({ event: 'queue_state', nodeId: 'clarify', waiting: 0, items: [], busy: true })
    chat?.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { id: `next-${host}`, text: '继续' } })
    chat?.applyAcpEvents([{ kind: 'message', text: '新同文回答' }])
  }
  await nextTick()
}

const App = defineComponent({
  name: 'MessageReliabilityHarness',
  setup() {
    const pm = usePmLeaderChat({ binding: null }, () => {})
    return () => h('main', { 'data-testid': 'message-reliability-root', class: 'space-y-6 p-4' }, [
      h('section', { class: 'space-y-3' }, [
        h('button', { 'data-testid': 'sim-same-text', type: 'button', onClick: () => void startSameTextRound() }, '同文下一轮'),
        ...hosts.map((host) => h('div', { 'data-testid': `clarify-host-${host}`, class: 'h-64 border border-line' }, [
          h(ClarifyChat, {
            ref: clarifyRefs[host],
            runId: `run-${host}`,
            nodeId: 'clarify',
            iteration: 1,
            turns: clarifyTurns[host].value,
            done: false,
            active: true,
            reviewMode: false,
            hideFinish: true,
            onSend: () => {},
          }),
        ])),
      ]),
      h('section', { 'data-testid': 'studio-race', class: 'h-96 border border-line' }, [
        h(AgentChatTester, { profile: 'cursor', attachId: 7, embedded: true }),
      ]),
      h('section', { 'data-testid': 'pm-race' }, [
        h('div', { 'data-testid': 'pm-message-count' }, String(pm.messages.value.length)),
        ...pm.messages.value.map((message) => h('p', {
          key: message.id,
          'data-testid': 'pm-reconciled-message',
          'data-message-id': message.id,
        }, message.content)),
      ]),
    ])
  },
})

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { template: '<div />' } }],
})
createApp(App).use(router).use(i18n).mount('#app')
