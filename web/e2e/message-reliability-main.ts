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
import { ingestDialogue } from '../src/lib/chat/dialogueStore'
import type { DialogueState, DialogueTurn } from '../src/lib/chat/dialogueState'
import { dialogueFrame, endedTurn, queuedTurn, runningTurn, type DialogueSpec, type RowInput } from '../src/test/dialogueKit'
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

const hosts = ['inbox', 'public', 'embed'] as const
type Host = (typeof hosts)[number]
const clarifyTurns = Object.fromEntries(hosts.map((host) => [host, ref<ClarifyTurn[]>([
  { role: 'human', text: '继续', at: '2026-01-01T00:00:00Z' },
  { role: 'agent', text: '上一轮回答', at: '2026-01-01T00:00:01Z' },
])])) as Record<Host, ReturnType<typeof ref<ClarifyTurn[]>>>
/** Sends each host's chat emitted (clientMessageId by text). */
const sentIds: Record<Host, Map<string, string>> = { inbox: new Map(), public: new Map(), embed: new Map() }

/** One host's dialogue state, as the platform pushes it (epoch = server process). */
function frame(host: Host, spec: DialogueSpec, epoch = 1, seq?: number): DialogueState {
  const f = dialogueFrame(`run-${host}`, 'clarify', spec)
  return { ...f, epoch, seq: seq ?? f.seq }
}
const push = (f: DialogueState) => ingestDialogue(f)

const earlier: RowInput[] = [
  { role: 'human', text: '继续', at: '2026-01-01T00:00:00Z', turnId: 'prev' },
  { role: 'agent', text: '上一轮回答', at: '2026-01-01T00:00:01Z', turnId: 'prev' },
]
async function startSameTextRound() {
  for (const host of hosts) {
    const id = `next-${host}`
    push(frame(host, {
      busy: true,
      rows: [...earlier, { role: 'human', text: '继续', at: '2026-01-01T00:00:02Z', turnId: id },
        { role: 'agent', text: '新同文回答', at: '2026-01-01T00:00:03Z', turnId: id, streaming: true }],
      turns: [runningTurn(id, { text: '继续' })],
    }))
  }
  await nextTick()
}

const activeStats = { id: 'active-stats', text: '回复信息改成统计信息' }
const sealedReply = '已把回复信息改成统计信息。'
const activeThought = '继续检查统计信息的展示效果。'
const statsHistory: RowInput[] = [
  { role: 'human', text: '之前的请求', at: '2026-10-28T13:30:00Z', turnId: 'before' },
  { role: 'agent', text: '之前的回复', at: '2026-10-28T13:30:38Z', turnId: 'before' },
  { role: 'human', text: activeStats.text, at: '2026-10-28T13:31:00Z', turnId: activeStats.id },
]
/** The running reply in two segments: a handed-off message, then an open thought. */
function segmentedRows(tail = ''): RowInput[] {
  return [...statsHistory,
    { role: 'agent', text: sealedReply, at: '2026-10-28T13:31:10Z', turnId: activeStats.id, handoff: true, phase: 'running' },
    { role: 'agent', text: tail, thought: activeThought, at: '2026-10-28T13:31:10Z', turnId: activeStats.id, streaming: true },
  ]
}
function queuedDark(): DialogueTurn[] {
  const text = '暗色主题感觉不太行'
  const clientMessageId = sentIds.inbox.get(text)
  return [queuedTurn('queued-dark', 0, { text, clientMessageId }, { clientMessageId })]
}
async function startSegmentedReply() {
  push(frame('inbox', { busy: true, rows: segmentedRows(), turns: [runningTurn(activeStats.id, activeStats)] }))
}
async function persistSealedSegment() {
  // The same request still owns an open thought segment while the next
  // accepted user waits behind it.
  push(frame('inbox', { busy: true, rows: segmentedRows(), turns: [runningTurn(activeStats.id, activeStats), ...queuedDark()] }))
  await nextTick()
}
function continueSegment() {
  push(frame('inbox', {
    busy: true, rows: segmentedRows('统计信息检查还在继续，无需刷新。'), turns: [runningTurn(activeStats.id, activeStats), ...queuedDark()],
  }))
}

const concurrentItems = ['a', 'b', 'c'].map((id) => ({
  id: `concurrent-${id}`, text: '继续检查', clientMessageId: `10000000-0000-4000-8000-00000000000${id === 'a' ? 1 : id === 'b' ? 2 : 3}`,
}))
const nextGenerationItem = { id: 'new-session-active', text: '继续检查', clientMessageId: '20000000-0000-4000-8000-000000000001' }
const roundRows = (n: number): RowInput[] => concurrentItems.slice(0, n).flatMap((item, i): RowInput[] => [
  { role: 'human', text: item.text, at: `2026-01-01T00:0${i}:00Z`, turnId: item.id, clientMessageId: item.clientMessageId },
  { role: 'agent', text: `并发回合 ${i + 1} 已完成。`, at: `2026-01-01T00:0${i}:30Z`, turnId: item.id },
])
/** A's first state (B waiting), whose publish is delayed until after all three finish. */
let delayed: DialogueState | null = null
async function finishConcurrentRounds() {
  for (const [index, item] of concurrentItems.entries()) {
    const waiting = concurrentItems.slice(index + 1).map((w, i) => queuedTurn(w.id, i, w, { clientMessageId: w.clientMessageId }))
    const running = frame('inbox', {
      busy: true,
      rows: [...roundRows(index), { role: 'human', text: item.text, at: '2026-01-01T00:00:00Z', turnId: item.id, clientMessageId: item.clientMessageId },
        { role: 'agent', text: `并发回合 ${index + 1} 已完成。`, at: '2026-01-01T00:00:00Z', turnId: item.id, streaming: true }],
      turns: [runningTurn(item.id, item, { clientMessageId: item.clientMessageId }), ...waiting],
    })
    if (index === 0) delayed = running
    else push(running)
    push(frame('inbox', {
      rows: roundRows(index + 1),
      turns: [endedTurn(item.id, 'ok', { clientMessageId: item.clientMessageId }), ...waiting],
    }))
  }
  await nextTick()
}
function deliverDelayedQueue() {
  if (delayed) push(delayed)
}
const newGenerationRows = (text: string): RowInput[] => [...roundRows(3),
  { role: 'human', text: nextGenerationItem.text, at: '2026-01-01T00:05:00Z', turnId: nextGenerationItem.id, clientMessageId: nextGenerationItem.clientMessageId },
  { role: 'agent', text, at: '2026-01-01T00:05:01Z', turnId: nextGenerationItem.id, streaming: true },
]
function startNewGeneration() {
  push(frame('inbox', {
    busy: true, rows: newGenerationRows('新会话正在继续检查。'),
    turns: [runningTurn(nextGenerationItem.id, nextGenerationItem, { clientMessageId: nextGenerationItem.clientMessageId })],
  }, 2, 1))
}
function deliverRetiredFrames() {
  deliverDelayedQueue()
  // The previous server process's last state: far higher seq, older epoch.
  push(frame('inbox', { rows: roundRows(3), turns: [] }, 1, 999))
  push(frame('inbox', {
    busy: true, rows: newGenerationRows('新会话继续输出，旧快照没有打断。'),
    turns: [runningTurn(nextGenerationItem.id, nextGenerationItem, { clientMessageId: nextGenerationItem.clientMessageId })],
  }, 2, 2))
}

const segmentedScenario = new URLSearchParams(location.search).has('sealed-segment')

const App = defineComponent({
  name: 'MessageReliabilityHarness',
  setup() {
    const pm = usePmLeaderChat({ binding: null }, () => {})
    return () => h('main', { 'data-testid': 'message-reliability-root', class: 'space-y-6 p-4' }, [
      h('section', { class: 'space-y-3' }, [
        h('button', { 'data-testid': 'sim-concurrent-complete', type: 'button', onClick: () => void finishConcurrentRounds() }, '完成三个并发回合'),
        h('button', { 'data-testid': 'sim-delayed-queue', type: 'button', onClick: deliverDelayedQueue }, '送达延迟队列快照'),
        h('button', { 'data-testid': 'sim-new-generation', type: 'button', onClick: startNewGeneration }, '启动新会话'),
        h('button', { 'data-testid': 'sim-retired-frames', type: 'button', onClick: deliverRetiredFrames }, '送达已退休会话帧'),
        h('button', { 'data-testid': 'sim-same-text', type: 'button', onClick: () => void startSameTextRound() }, '同文下一轮'),
        h('button', { 'data-testid': 'sim-segmented-reply', type: 'button', onClick: () => void startSegmentedReply() }, '分段回复'),
        h('button', { 'data-testid': 'sim-persist-segment', type: 'button', onClick: () => void persistSealedSegment() }, '持久化已封口片段'),
        h('button', { 'data-testid': 'sim-continue-segment', type: 'button', onClick: continueSegment }, '继续当前回复'),
        ...hosts.map((host) => h('div', { 'data-testid': `clarify-host-${host}`, class: 'h-64 border border-line', style: segmentedScenario && host === 'inbox' ? { height: '650px' } : undefined }, [
          h(ClarifyChat, {
            runId: `run-${host}`,
            nodeId: 'clarify',
            iteration: 1,
            turns: clarifyTurns[host].value,
            done: false,
            active: true,
            reviewMode: false,
            hideFinish: true,
            onSend: (text: string, _images: unknown, _annotations: unknown, clientMessageId: string) => {
              sentIds[host].set(text, clientMessageId)
            },
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
