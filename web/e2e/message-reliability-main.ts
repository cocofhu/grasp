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
  applyAcpEvents: (events: { kind: string; text?: string }[]) => void
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

const activeStats = { id: 'active-stats', text: '回复信息改成统计信息' }
const queuedDark = { id: 'queued-dark', text: '暗色主题感觉不太行' }
const sealedReply = '已把回复信息改成统计信息。'
const activeThought = '继续检查统计信息的展示效果。'
const segmentedEvents = [
  { kind: 'message', text: sealedReply },
  { kind: 'segment' },
  { kind: 'thought', text: activeThought },
]
function statsHistory(): ClarifyTurn[] {
  return [
    { role: 'human', text: '之前的请求', at: '2026-10-28T13:30:00Z' },
    { role: 'agent', text: '之前的回复', at: '2026-10-28T13:30:38Z' },
    { role: 'human', text: activeStats.text, at: '2026-10-28T13:31:00Z' },
  ]
}
async function startSegmentedReply() {
  clarifyTurns.inbox.value = statsHistory()
  await nextTick()
  const chat = clarifyRefs.inbox.value
  chat?.applyReviewFrame({ event: 'queue_state', nodeId: 'clarify', busy: true, waiting: 0, items: [], activeItem: activeStats })
  chat?.applyAcpEvents(segmentedEvents)
}
async function persistSealedSegment() {
  clarifyRefs.inbox.value?.applyReviewFrame({ event: 'queue_state', nodeId: 'clarify', busy: true, waiting: 1, items: [queuedDark], activeItem: activeStats })
  // Only the first assistant segment is sealed. The same request still owns
  // an open thought segment while the next accepted user waits behind it.
  clarifyTurns.inbox.value = [...statsHistory(), { role: 'agent', text: sealedReply, at: '2026-10-28T13:31:10Z', handoff: true }]
  await nextTick()
}

const concurrentItems = ['a', 'b', 'c'].map((id) => ({
  id: `concurrent-${id}`, text: '继续检查', clientMessageId: `10000000-0000-4000-8000-00000000000${id === 'a' ? 1 : id === 'b' ? 2 : 3}`,
}))
const nextGenerationItem = { id: 'new-session-active', text: '继续检查', clientMessageId: '20000000-0000-4000-8000-000000000001' }
function versionedFrame(event: string, revision: number, extra: RawFrame, sessionGeneration = 7) {
  clarifyRefs.inbox.value?.applyReviewFrame({ event, nodeId: 'clarify', sessionGeneration, revision, ...extra })
}
async function finishConcurrentRounds() {
  for (const [index, item] of concurrentItems.entries()) {
    const revision = index === 0 ? 2 : index === 1 ? 6 : 8
    versionedFrame('turn_begin', revision, { item })
    clarifyRefs.inbox.value?.applyAcpEvents([{ kind: 'message', text: `并发回合 ${index + 1} 已完成。` }])
    const waiting = concurrentItems.slice(index + 1)
    versionedFrame('turn_done', index === 0 ? 5 : index === 1 ? 7 : 9, {
      item, waiting: waiting.length, items: waiting, busy: false,
    })
  }
  await nextTick()
}
function deliverDelayedQueue() {
  // B's enqueue captured A active + B waiting. Its publish was delayed until
  // all three requests finished; arrival order must not overwrite revision 9.
  versionedFrame('queue_state', 3, {
    busy: true, waiting: 1, activeItem: concurrentItems[0], items: [concurrentItems[1]],
  })
}
function startNewGeneration() {
  versionedFrame('queue_state', 1, { busy: true, waiting: 0, items: [], activeItem: nextGenerationItem }, 8)
  clarifyRefs.inbox.value?.applyAcpEvents([{ kind: 'message', text: '新会话正在继续检查。' }])
}
function deliverRetiredFrames() {
  deliverDelayedQueue()
  versionedFrame('turn_done', 999, { item: concurrentItems[2], busy: false, waiting: 0, items: [] })
  clarifyRefs.inbox.value?.applyAcpEvents([{ kind: 'message', text: '新会话继续输出，旧快照没有打断。' }])
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
        h('button', { 'data-testid': 'sim-continue-segment', type: 'button', onClick: () => clarifyRefs.inbox.value?.applyAcpEvents([...segmentedEvents, { kind: 'message', text: '统计信息检查还在继续，无需刷新。' }]) }, '继续当前回复'),
        ...hosts.map((host) => h('div', { 'data-testid': `clarify-host-${host}`, class: 'h-64 border border-line', style: segmentedScenario && host === 'inbox' ? { height: '650px' } : undefined }, [
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
