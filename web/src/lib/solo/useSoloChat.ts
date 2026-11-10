import { computed, onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { MessageReference, SoloMessage, SoloSession } from '@/lib/api/apiTypes'
import type { ChatExecution, ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { createReviewStateCursor } from '@/lib/chat/sessionQueue'
import { applyAcp, emptyAgentTurn, turnsFromMessages, type SoloTurn } from './acpTurn'
import { soloErrorText } from './soloErrors'
import { createSoloModelSettings } from './soloModelSettings'
import { createSoloQueueOrder } from './soloQueueOrder'
import { soloHistoryTiming, type SoloChatDiagnostic } from './soloDiagnostics'
import { useSoloSessions } from './useSoloSessions'
import { newSoloRequestID, readSoloOutbox, removeSoloOutbox, saveSoloOutbox, soloOutboxScope, type SoloMessageOutbox, type SoloTurnRequest } from './soloOutbox'

export type SoloQueueItem = { id: string; clientMessageId?: string; text: string; images: ClarifyImage[]; annotations?: ReactAnnotation[]; references?: MessageReference[]; local?: boolean }
export type SoloPhase = '' | 'preparing' | 'running'
export type SoloLink = 'idle' | 'connecting' | 'open' | 'reconnecting'

const SANDBOX_POLL_MS = 2500
// Bridge liveness is not forwarded, so a long tool call can be silent: a quiet
// busy turn only triggers a read, and resubscribes when the server says idle.
const STALL_CHECK_MS = 30_000

function parseFrame(data: unknown): any {
  if (typeof data !== 'string') return null
  try {
    return JSON.parse(data)
  } catch {
    return null
  }
}

function queueItem(raw: any): SoloQueueItem {
  return {
    id: String(raw?.id ?? ''),
    ...(typeof raw?.clientMessageId === 'string' ? { clientMessageId: raw.clientMessageId } : {}),
    text: String(raw?.text ?? ''),
    images: Array.isArray(raw?.images) ? raw.images : [],
    ...(Array.isArray(raw?.annotations) && raw.annotations.length ? { annotations: raw.annotations } : {}),
    ...(Array.isArray(raw?.references) && raw.references.length ? { references: raw.references } : {}),
  }
}

/**
 * Live state of one Solo session: transcript from persisted messages plus the
 * active turn streamed over the session WebSocket (which replays the running
 * turn on reconnect, so a dropped socket only pauses the view).
 */
export type SoloChatTransport = Pick<typeof api, 'getSoloSession' | 'listSoloMessages' | 'sendSoloTurn' | 'cancelSoloTurn' | 'soloChatWsUrl'
  | 'setSoloSessionModel' | 'setSoloSessionReasoningEffort' | 'removeSoloQueued' | 'reorderSoloQueue'> & {
  listSoloMessagePage?: typeof api.listSoloMessagePage
  listSoloMessageEvents?: typeof api.listSoloMessageEvents
  soloChatProtocols?: string[]
  reportSoloDiagnostics?: typeof api.reportSoloDiagnostics
}

export function useSoloChat(sessionId: Ref<string>, transport: SoloChatTransport = api) {
  const { t, te, locale } = useI18n()
  const label = (key: string) => {
    const k = `pages.agentChatTester.tools.${key}`
    return te(k) ? t(k) : ''
  }
  const toolFallback = () => t('pages.agentChatTester.toolFallback')
  const sessions = useSoloSessions()

  const session = shallowRef<SoloSession | null>(null)
  const turns = ref<SoloTurn[]>([])
  const queued = ref<SoloQueueItem[]>([])
  const busy = ref(false)
  const sessionStateCursor = createReviewStateCursor()
  const phase = ref<SoloPhase>('')
  const link = ref<SoloLink>('idle')
  const loading = ref(false)
  const historyReady = ref(false)
  const loadError = ref('')
  const actionError = ref('')
  const pendingConfirmation = ref<string[]>([])
  const retryingSend = ref(false)
  const hasEarlier = ref(false)
  const loadingEarlier = ref(false)
  const earlierError = ref('')
  const processPages = ref<Record<string, { next: number; hasMore: boolean; loading: boolean; error: string; legacyIncomplete: boolean }>>({})
  const historyMessages = new Map<string, SoloMessage>()
  let historyOrder: string[] = []
  const processEvents = new Map<string, unknown[]>()
  let beforeMessage: string | undefined
  let earlierLoaded = false

  let ws: WebSocket | null = null
  let held: string[] | null = null
  const activeId = ref('')
  const executions = new Map<string, ChatExecution>()
  let disposed = false
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let stallTimer: ReturnType<typeof setTimeout> | undefined
  let frameCount = 0
  let refetchSeq = 0
  // lastSeq 是缺口检测高水位（含快照）；ackedSeq 仅确认已处理的真实帧，供重连回放。
  let lastSeq = -1
  let ackedSeq = -1
  let streamGeneration: number | undefined
  let journalVersion = 0
  let resyncs = 0
  let sessionGeneration = 0
  // 真实帧按序号和事件类型去重；订阅快照不进入此表，避免吞掉同序号的开场帧。
  const seenSeq = new Map<number, Set<string>>()
  type LocalSend = {
    localId: string
    serverId?: string
    text: string
    images: ClarifyImage[]
    references: MessageReference[]
    annotations: ReactAnnotation[]
    started: boolean
    request?: SoloTurnRequest
    outboxScope?: string
  }
  // The local identity survives HTTP, queue acknowledgement and turn_begin.
  // A server identity can be assigned once; repeated text is never a turn id.
  const openSends: LocalSend[] = []
  const knownServerIds = new Set<string>()
  const missingQueuedIds = new Set<string>()
  let persistedHistory: SoloTurn[] = []
  let chatEpoch = 0
  const outboxReady = ref(false)
  const diagnosticViewID = newSoloRequestID().slice('local-'.length)
  let diagnosticConnection = 0
  let diagnosticReceived = 0
  let diagnosticFrameStart = 0
  let diagnosticLastReceived = -1
  let diagnosticHead = -1
  let diagnosticRequest = 0
  let diagnosticCaughtUp = false
  let connectingTimer: ReturnType<typeof setTimeout> | undefined
  const historyTimers = new Map<number, ReturnType<typeof setTimeout>>()

  function diagnostic(event: SoloChatDiagnostic['event'], extra: Partial<SoloChatDiagnostic> = {}) {
    if (!transport.reportSoloDiagnostics || !sessionId.value) return
    // Do not include texts, native frames, attachment URLs, errors or tokens.
    const row: SoloChatDiagnostic = {
      viewId: diagnosticViewID, event, connection: diagnosticConnection,
      received: diagnosticReceived, processed: frameCount - diagnosticFrameStart,
      buffered: held?.length ?? 0, pendingHistory: historyTimers.size,
      lastReceived: diagnosticLastReceived, acknowledged: ackedSeq, highWatermark: diagnosticHead,
      historyReady: historyReady.value, busy: busy.value, serverBusy: !!session.value?.busy,
      turns: turns.value.length, agentTurns: turns.value.filter(turn => turn.role === 'agent').length,
      liveParts: activeTurn.value?.steps.length ?? 0, liveTextChars: activeTurn.value?.text.length ?? 0,
      ...extra,
    }
    try { void transport.reportSoloDiagnostics(sessionId.value, row).catch(() => {}) } catch { /* diagnostics cannot affect the chat */ }
  }

  const reconnect = createWsReconnectController({
    connect: () => openWs(),
    shouldReconnect: () => !disposed && !!sessionId.value,
  })

  const status = computed(() => session.value?.status ?? 'starting')
  const closed = computed(() => status.value === 'closed')
  const sessionOpen = computed(() => !!session.value && (status.value === 'starting' || status.value === 'running'))
  // Establish the old message identities before accepting a new send. An old
  // same-text row from a delayed initial read must never acknowledge it.
  const canSend = computed(() => sessionOpen.value && historyReady.value && outboxReady.value && !pendingConfirmation.value.length && session.value?.sandbox?.desiredState !== 'stopped')
  // An explicit current model wins, including empty (primary). Old sessions
  // omit the flag and keep the opening model.
  const currentModelId = computed(() => {
    const row = session.value
    if (!row) return ''
    if (row.currentModelSet) return row.currentModelId || ''
    return row.initialModelId || ''
  })
  const currentConfigurationId = computed(() => session.value?.currentConfigurationId || session.value?.initialConfigurationId || '')
  const currentReasoningEffort = computed(() => session.value?.reasoningEffort || '')
  const activeTurn = computed(() => (activeId.value ? turns.value.find((x) => x.id === `live:${activeId.value}`) ?? null : null))

  const isCurrent = (id: string, epoch: number) => !disposed && epoch === chatEpoch && id === sessionId.value
  const settings = createSoloModelSettings({
    sessionId,
    session,
    sessionOpen,
    actionError,
    transport,
    disposed: () => disposed,
    sessionGeneration: () => sessionGeneration,
    patchSession,
    errorText: (error) => soloErrorText(error, t, te, locale.value),
  })
  const queueOrder = createSoloQueueOrder({ sessionId, queued, transport, isCurrent, epoch: () => chatEpoch })
  const displayQueued = queueOrder.displayQueued

  function setSession(v: SoloSession) {
    // The embed transport is bound to the preview run ID by its bearer.
    if (!transport.soloChatProtocols && v.id !== sessionId.value) return
    const next = settings.mergeFetched(v)
    session.value = next
    sessions.upsert(next)
    schedulePoll()
  }

  function applyResumeSnapshot(frame: Record<string, any>) {
    if (settings.pending()) return
    const patch: Partial<SoloSession> = {}
    if (typeof frame.status === 'string' && frame.status) {
      patch.status = frame.status as SoloSession['status']
      patch.error = typeof frame.error === 'string' ? frame.error : ''
    }
    if (typeof frame.sandboxId === 'number') patch.sandboxId = frame.sandboxId
    if (typeof frame.currentModelSet === 'boolean') {
      patch.currentModelSet = frame.currentModelSet
      if (typeof frame.currentModelId === 'string') patch.currentModelId = frame.currentModelId
    }
    if (typeof frame.currentConfigurationId === 'string') patch.currentConfigurationId = frame.currentConfigurationId
    if (typeof frame.backend === 'string') patch.backend = frame.backend
    if (typeof frame.reasoningEffort === 'string') patch.reasoningEffort = frame.reasoningEffort
    if (typeof frame.modelNotice === 'string') patch.modelNotice = frame.modelNotice
    if (!Object.keys(patch).length) return
    patchSession(patch)
    // Invalidate getSoloSession calls that started before this subscribe snapshot.
    // Otherwise a slower row can setSession over the status and model just applied.
    sessionGeneration++
    if (session.value) settings.confirm(session.value)
    if (frame.modelNotice) showModelNotice(String(frame.modelNotice), typeof frame.error === 'string' ? frame.error : '')
    if (frame.status === 'closed' || frame.status === 'error') void refetchMessages()
  }

  function patchSession(patch: Partial<SoloSession>) {
    const cur = session.value
    if (!cur) return
    const next = { ...cur, ...patch }
    session.value = next
    sessions.upsert(next)
  }

  /** Polls while the sandbox boots, and again while a running session has no live socket. */
  function schedulePoll() {
    if (pollTimer) clearTimeout(pollTimer)
    pollTimer = undefined
    if (disposed) return
    const st = session.value?.status
    const boot = st === 'starting'
    const catchUp = st === 'running' && (link.value !== 'open' || !historyReady.value)
    if (!boot && !catchUp) return
    pollTimer = setTimeout(() => {
      pollTimer = undefined
      if (session.value?.status === 'starting') void refreshSession()
      else void refetchMessages()
      schedulePoll()
    }, SANDBOX_POLL_MS)
  }

  async function refreshSession() {
    const id = sessionId.value
    if (!id || settings.pending()) return
    const generation = sessionGeneration
    const sequence = settings.sequence()
    try {
      const row = await transport.getSoloSession(id)
      if (generation === sessionGeneration && sequence === settings.sequence()) setSession(row)
    } catch {
      schedulePoll()
    }
  }

  const sendKey = (send: LocalSend) => send.serverId || send.localId

  function samePayload(send: LocalSend, item: SoloQueueItem): boolean {
    const refs = item.references || []
    if (refs.length && (refs.length !== send.references.length || refs.some((ref, i) => ref.target !== send.references[i]!.target))) return false
    const annotations = item.annotations || []
    if (annotations.length !== send.annotations.length || annotations.some((pick, i) => pick.selector !== send.annotations[i]?.selector || pick.url !== send.annotations[i]?.url)) return false
    return send.text === item.text && send.images.length === item.images.length && send.images.every((image, i) => {
      const other = item.images[i]!
      if (image.mimeType !== other.mimeType) return false
      if (image.name && other.name && image.name !== other.name) return false
      if (image.sizeBytes && other.sizeBytes && image.sizeBytes !== other.sizeBytes) return false
      const imageRef = image.ref || (image.data?.startsWith('blob:') ? image.data : '')
      const otherRef = other.ref || (other.data?.startsWith('blob:') ? other.data : '')
      if (imageRef && otherRef) return imageRef === otherRef
      if (!imageRef && !otherRef && image.data && other.data) return image.data === other.data
      // Inline uploads become opaque blob references on the server.
      return true
    })
  }

  /** Bind the first unacknowledged matching send, once, including image-only sends. */
  function confirmLocalSend(send: LocalSend) {
    if (send.outboxScope) {
      const scope = send.outboxScope
      // Removal failure keeps the same identity available for reconciliation
      // after reload. It never authorizes a fresh request or automatic replay.
      void removeSoloOutbox(scope, send.localId).then(() => { send.outboxScope = undefined }).catch(() => {})
    }
    pendingConfirmation.value = pendingConfirmation.value.filter(id => id !== send.localId)
    if (!pendingConfirmation.value.length && actionError.value === t('pages.solo.sendUnconfirmed')) actionError.value = ''
  }

  function adoptLocalSend(item: SoloQueueItem): LocalSend | undefined {
    if (!item.id) return undefined
    const bound = openSends.find((send) => send.serverId === item.id || send.localId === item.id)
    if (bound) {
      confirmLocalSend(bound)
      return bound
    }
    const exact = item.clientMessageId ? openSends.find(row => !row.serverId && row.localId === item.clientMessageId) : undefined
    if (knownServerIds.has(item.id) && !exact) return undefined
    const send = item.clientMessageId ? exact : openSends.find((row) => !row.serverId && !row.outboxScope && samePayload(row, item))
    knownServerIds.add(item.id)
    if (!send) return undefined
    const old = send.localId
    send.serverId = item.id
    confirmLocalSend(send)
    for (const row of turns.value) {
      if (row.id === `u:${old}`) row.id = `u:${item.id}`
      if (row.id === `live:${old}`) row.id = `live:${item.id}`
    }
    if (activeId.value === old) activeId.value = item.id
    // HTTP acceptance can precede queue_state. Keep an accepted request
    // visible under its server identity until that authoritative snapshot.
    queued.value = queued.value.map(row => row.id === old ? { ...row, id: item.id, clientMessageId: send.localId, local: false } : row)
    return send
  }

  function forgetSend(send: LocalSend) {
    pendingConfirmation.value = pendingConfirmation.value.filter(id => id !== send.localId)
    const key = sendKey(send)
    const index = openSends.indexOf(send)
    if (index >= 0) openSends.splice(index, 1)
    missingQueuedIds.delete(key)
    turns.value = turns.value.filter((turn) => turn.id !== `u:${key}` && turn.id !== `live:${key}`)
    queued.value = queued.value.filter((item) => item.id !== key && item.id !== send.localId)
    if (activeId.value === key) {
      activeId.value = ''
      phase.value = ''
    }
  }

  function dropQueuedSends() {
    for (const send of [...openSends]) {
      if (!send.started) forgetSend(send)
    }
  }

  function historyCoversLive(saved: SoloTurn | undefined, live: SoloTurn | undefined): boolean {
    if (!saved || saved.role !== 'agent') return false
    if (!live) return true
    if (saved.historyMessageId && !live.streaming) return true
    if (!saved.text.trim().startsWith(live.text.trim())) return false
    if (live.error && !(saved.error || '').includes(live.error)) return false
    if (live.status && live.status !== saved.status) return false
    const thoughts = (turn: SoloTurn) => turn.steps.flatMap((step) => step.kind === 'thought' ? [step.text] : []).join('')
    if (!thoughts(saved).startsWith(thoughts(live))) return false
    if (live.plan.length && !saved.plan.length) return false
    if (live.files?.some(file => !saved.files?.some(other => other.artifactId === file.artifactId && other.revision === file.revision))) return false
    return live.tools.every((tool) => saved.tools.some((other) => other.id === tool.id &&
      (!['completed', 'failed'].includes(tool.status) || other.status === tool.status) &&
      (!tool.input || (other.input || '').startsWith(tool.input)) &&
      (!tool.output || (other.output || '').startsWith(tool.output))))
  }

  function applyMessages(messages: SoloMessage[], older = false) {
    if (transport.listSoloMessagePage) {
      // Preserve each server window's order, including legacy rows with equal
      // timestamps. Re-sorting by IDs can move an answer before its question.
      const incomingIds = new Set(messages.map(message => message.id))
      const common = messages.find(message => historyMessages.has(message.id))
      if (messages.length) {
        if (common) {
          const pivot = historyOrder.indexOf(common.id)
          historyOrder = [...historyOrder.slice(0, pivot).filter(id => !incomingIds.has(id)), ...messages.map(message => message.id), ...historyOrder.slice(pivot).filter(id => !incomingIds.has(id))]
        } else {
          historyOrder = older ? [...messages.map(message => message.id), ...historyOrder] : [...historyOrder, ...messages.map(message => message.id)]
        }
      }
      for (const message of messages) historyMessages.set(message.id, message)
      messages = historyOrder.map(id => historyMessages.get(id)!)
    }
    const clientIds = new Map(messages.map(message => [message.id, message.clientMessageId]))
    const incoming = turnsFromMessages(messages.map(message => processEvents.has(message.id) ? { ...message, events: processEvents.get(message.id) } : message), label, toolFallback())
    for (const turn of incoming) {
      if (!turn.historyMessageId) continue
      const page = processPages.value[turn.historyMessageId]
      // A reply-only journal still owns the elapsed fold the reader just opened.
      turn.processComplete = page?.hasMore === false
    }
    // Messages are append-only. A shorter HTTP snapshot must not erase rows
    // already reconciled from an earlier response.
    if (incoming.length >= persistedHistory.length) persistedHistory = incoming
    const history = persistedHistory.slice()
    for (const user of history) {
      if (user.role !== 'user') continue
      const send = adoptLocalSend({ id: user.id.slice(2), clientMessageId: clientIds.get(user.id.slice(2)), text: user.text, images: user.images || [], annotations: user.annotations })
      if (send) send.started = true
    }
    // A queue item can be removed in another viewer. Confirm its absence
    // against a history read started after the queue frame, since a missing
    // queue item may also have completed while this socket was disconnected.
    for (const id of [...missingQueuedIds]) {
      const send = openSends.find((row) => row.serverId === id)
      if (send && !send.started && !history.some((row) => row.id === `u:${id}`)) forgetSend(send)
      missingQueuedIds.delete(id)
    }

    const replyIndex = (key: string) => {
      const explicit = history.findIndex(turn => turn.role === 'agent' && turn.replyTo === key)
      if (explicit >= 0) return explicit
      const user = history.findIndex(turn => turn.id === `u:${key}`)
      const next = history[user + 1]
      return user >= 0 && next?.role === 'agent' && !next.replyTo ? user + 1 : -1
    }
    const mergePair = (key: string) => {
      const user = turns.value.find((turn) => turn.id === `u:${key}`)
      const agent = turns.value.find((turn) => turn.id === `live:${key}`)
      const index = history.findIndex((turn) => turn.id === `u:${key}`)
      let savedIndex = replyIndex(key)
      if (index < 0) {
        if (user) {
          if (savedIndex >= 0) { history.splice(savedIndex, 0, user); savedIndex++ }
          else history.push(user)
        }
      }
      if (agent) {
        const saved = history[savedIndex]
        if (saved?.role === 'agent' && saved.files?.length) {
          const files = [...(agent.files ?? [])]
          for (const file of saved.files) {
            if (!files.some(other => other.artifactId === file.artifactId && other.revision === file.revision)) files.push(file)
          }
          agent.files = files
        }
        if (savedIndex >= 0) history.splice(savedIndex, 1, agent)
        else {
          const userIndex = history.findIndex(turn => turn.id === `u:${key}`)
          history.splice(userIndex >= 0 ? userIndex + 1 : history.length, 0, agent)
        }
      }
    }

    const retained = new Set<string>()
    for (const send of [...openSends]) {
      const key = sendKey(send)
      const live = turns.value.find((turn) => turn.id === `live:${key}`)
      // Keep an active stream until its terminal/idle frame: history may be
      // persisted before the last output frames have reached this browser.
      if (activeId.value !== key && historyCoversLive(history[replyIndex(key)], live)) {
        openSends.splice(openSends.indexOf(send), 1)
      } else {
        retained.add(key)
      }
    }
    if (activeId.value) retained.add(activeId.value)
    // Use transcript order, including turns started in another viewer, so an
    // active remote turn always remains before locally queued sends.
    for (const turn of turns.value) {
      if (turn.role !== 'user') continue
      const key = turn.id.slice(2)
      if (retained.delete(key)) mergePair(key)
    }
    for (const key of retained) mergePair(key)
    turns.value = history
    historyReady.value = true
    schedulePoll()
  }

  async function readHistory(id: string) {
    const epoch = chatEpoch
    const request = ++diagnosticRequest
    const started = performance.now()
    historyTimers.set(request, setTimeout(() => {
      if (isCurrent(id, epoch)) diagnostic('history_slow', { request, elapsedMs: Math.round(performance.now() - started) })
    }, 5000))
    try {
      let messages: SoloMessage[]
      if (!transport.listSoloMessagePage) messages = await transport.listSoloMessages(id)
      else {
        const page = await transport.listSoloMessagePage(id)
        if (id === sessionId.value && epoch === chatEpoch && !earlierLoaded) {
          beforeMessage = page.before
          hasEarlier.value = page.hasMore
        }
        messages = page.items
      }
      if (isCurrent(id, epoch)) diagnostic('history_done', { request, elapsedMs: Math.round(performance.now() - started), pendingHistory: historyTimers.size - 1, httpStatus: 200, ...soloHistoryTiming(id, started) })
      return messages
    } catch (error: any) {
      if (isCurrent(id, epoch)) diagnostic('history_failed', { request, elapsedMs: Math.round(performance.now() - started), pendingHistory: historyTimers.size - 1, httpStatus: Number.isInteger(error?.status) && error.status >= 100 && error.status <= 599 ? error.status : 0 })
      throw error
    } finally {
      clearTimeout(historyTimers.get(request))
      historyTimers.delete(request)
    }
  }

  async function loadEarlier() {
    const id = sessionId.value
    const epoch = chatEpoch
    if (!transport.listSoloMessagePage || !beforeMessage || loadingEarlier.value) return
    loadingEarlier.value = true
    earlierError.value = ''
    try {
      const page = await transport.listSoloMessagePage(id, beforeMessage)
      if (disposed || id !== sessionId.value || epoch !== chatEpoch) return
      applyMessages(page.items, true)
      earlierLoaded = true
      beforeMessage = page.before
      hasEarlier.value = page.hasMore
    } catch (error: any) {
      if (id === sessionId.value && epoch === chatEpoch) earlierError.value = String(error?.message || error)
    } finally {
      if (id === sessionId.value && epoch === chatEpoch) loadingEarlier.value = false
    }
  }

  async function loadProcess(turn: SoloTurn) {
    const id = sessionId.value
    const epoch = chatEpoch
    const messageId = turn.historyMessageId
    if (!messageId || !transport.listSoloMessageEvents) return
    const previous = processPages.value[messageId]
    if (previous?.loading || previous?.hasMore === false) return
    const state = { next: previous?.next ?? -1, hasMore: true, loading: true, error: '', legacyIncomplete: previous?.legacyIncomplete ?? false }
    processPages.value[messageId] = state
    try {
      const page = await transport.listSoloMessageEvents(id, messageId, state.next)
      if (disposed || id !== sessionId.value || epoch !== chatEpoch) return
      if (!Number.isSafeInteger(page.next) || !Number.isSafeInteger(page.head) || page.next > page.head || page.hasMore && page.next <= state.next) throw new Error(t('pages.solo.chat.journalGap'))
      const events = [...(processEvents.get(messageId) ?? []), ...page.items.filter(entry => entry.type === 'acp').map(entry => entry.data)]
      const expected = historyMessages.get(messageId)?.eventLog?.count
      if (!page.hasMore && expected !== undefined && events.length !== expected) throw new Error(t('pages.solo.chat.journalGap'))
      processEvents.set(messageId, events)
      processPages.value[messageId] = { next: page.next, hasMore: page.hasMore, loading: false, error: '', legacyIncomplete: !!page.legacyIncomplete }
      applyMessages([])
    } catch (error: any) {
      if (id === sessionId.value && epoch === chatEpoch) processPages.value[messageId] = { ...state, loading: false, error: String(error?.message || error) }
    }
  }

  async function refetchMessages() {
    const id = sessionId.value
    const epoch = chatEpoch
    const seq = ++refetchSeq
    try {
      const messages = await readHistory(id)
      if (!disposed && epoch === chatEpoch && seq === refetchSeq && id === sessionId.value) applyMessages(messages)
    } catch {
      /* keep the current transcript */
    }
  }

  function showModelNotice(code: string, fallback = '') {
    if (!code) return
    actionError.value = soloErrorText({ code, message: fallback || code }, t, te, locale.value)
  }

  function endActive(error?: string, interrupted?: boolean) {
    const turn = activeTurn.value
    if (turn) {
      turn.streaming = false
      turn.endedAt = new Date().toISOString()
      if (error) turn.error = error
      if (interrupted) turn.status = 'stopped'
    }
    executions.delete(activeId.value)
    activeId.value = ''
    phase.value = ''
  }

  function applyExecution(frame: any) {
    const id = String(frame.userMsgId || frame.activeItem?.id || activeId.value || '')
    if (!id) return
    if (frame.execution && typeof frame.execution === 'object' && typeof frame.execution.agentName === 'string' && typeof frame.execution.backend === 'string') executions.set(id, { ...frame.execution })
    const execution = executions.get(id)
    if (!execution) return
    const turn = turns.value.find(row => row.id === `live:${id}`)
    if (!turn) return
    turn.execution = execution
    if (execution.startedAt) turn.startedAt = execution.startedAt
  }

  function beginTurn(frame: any) {
    const item = queueItem(frame.item)
    const id = item.id || String(frame.userMsgId ?? '')
    const send = adoptLocalSend({ ...item, id })
    if (send) send.started = true
    queued.value = queued.value.filter((q) => q.id !== id)
    if (activeId.value && activeId.value !== id) endActive()
    const existing = turns.value.find((x) => x.id === `live:${id}`)
    if (existing) {
      activeId.value = id
      const shown = turns.value.find((x) => x.id === `u:${id}`)
      // The server copy carries the send-time access check; prefer it over the optimistic one.
      if (shown && item.references?.length) shown.references = item.references
      if (shown && item.annotations?.length) shown.annotations = item.annotations
      if (!shown) {
        const started = existing.startedAt || new Date().toISOString()
        const user = {
          id: `u:${id}`,
          role: 'user' as const,
          text: item.text,
          tools: [],
          steps: [],
          plan: [],
          streaming: false,
          images: item.images.length ? item.images : undefined,
          references: item.references,
          annotations: item.annotations,
          createdAt: started,
        }
        const idx = turns.value.findIndex((x) => x.id === existing.id)
        const next = turns.value.slice()
        next.splice(idx < 0 ? next.length : idx, 0, user)
        turns.value = next
      }
      existing.streaming = true
      busy.value = true
      if (!phase.value) phase.value = 'preparing'
      return
    }
    const started = new Date().toISOString()
    activeId.value = id
    const next = turns.value.filter((x) => x.id !== `u:${id}` && x.id !== `live:${id}`)
    const firstPending = next.findIndex((turn) => openSends.some((row) => !row.started && turn.id === `u:${sendKey(row)}`))
    next.splice(firstPending < 0 ? next.length : firstPending, 0,
      { id: `u:${id}`, role: 'user', text: item.text, tools: [], steps: [], plan: [], streaming: false, images: item.images.length ? item.images : undefined, references: item.references, annotations: item.annotations, createdAt: started },
      emptyAgentTurn(`live:${id}`, true, started),
    )
    turns.value = next
    busy.value = true
    if (!phase.value) phase.value = 'preparing'
  }

  /** Output arrived without turn_begin. Attach it to the transcript's current user turn. */
  function adoptTurnForOutput() {
    const queuedItem = queued.value.find((q) => !q.local) ?? queued.value[0]
    let userId = ''
    for (let i = turns.value.length - 1; i >= 0; i--) {
      const row = turns.value[i]
      if (row?.role === 'user') {
        userId = row.id.startsWith('u:') ? row.id.slice(2) : row.id
        break
      }
    }
    const id = queuedItem?.id || userId
    if (!id) {
      const started = new Date().toISOString()
      const live = emptyAgentTurn('live:pending', true, started)
      turns.value = [...turns.value, live]
      activeId.value = 'pending'
      busy.value = true
      return live
    }
    const existing = turns.value.find((x) => x.id === `live:${id}`)
    if (existing) {
      existing.streaming = true
      activeId.value = id
      busy.value = true
      return existing
    }
    const fromQueue = queuedItem && queuedItem.id === id ? queuedItem : undefined
    const fromUser = turns.value.find((x) => x.id === `u:${id}`)
    beginTurn({
      item: {
        id,
        text: fromQueue?.text || fromUser?.text || '',
        images: fromQueue?.images || fromUser?.images || [],
      },
      userMsgId: id,
    })
    return turns.value.find((x) => x.id === `live:${id}`) ?? null
  }

  function noteSeq(frame: any, subscriptionSnapshot: boolean): 'ok' | 'dup' | 'gap' {
    const seq = frame?.seq
    if (typeof seq !== 'number' || seq < 0) return 'ok'
    // 快照只宣告服务端高水位，不代表其前的真实帧均已处理，也不构成序号缺口。
    if (subscriptionSnapshot) {
      if (seq > lastSeq) lastSeq = seq
      return 'ok'
    }
    const kind = `${String(frame?.type ?? '')}:${String(frame?.event ?? '')}`
    const seenKinds = seenSeq.get(seq)
    if (seenKinds?.has(kind)) return 'dup'
    if (lastSeq >= 0 && seq > lastSeq + 1) {
      if (!journalVersion && resyncs >= 3) {
        seenSeq.set(seq, new Set([kind]))
        lastSeq = seq
        return 'ok'
      }
      return 'gap'
    }
    if (seenKinds) seenKinds.add(kind)
    else seenSeq.set(seq, new Set([kind]))
    if (seq === lastSeq + 1 || lastSeq < 0) resyncs = 0
    if (seq > lastSeq) lastSeq = seq
    if (seenSeq.size > 4096) {
      for (const n of seenSeq.keys()) {
        if (n < lastSeq - 2048) seenSeq.delete(n)
      }
    }
    return 'ok'
  }

  function requestResync() {
    if (disposed) return
    if (resyncs >= 3 && journalVersion) {
      loadError.value = t('pages.solo.chat.journalGap')
      closeWs()
      link.value = 'reconnecting'
      schedulePoll()
      return
    }
    if (resyncs >= 3) return
    resyncs += 1
    resubscribe()
  }

  function resubscribe() {
    const sock = ws
    ws = null
    held = null
    link.value = 'reconnecting'
    sock?.close()
    void refetchMessages()
    schedulePoll()
    reconnect.onClose()
  }

  function scheduleStallCheck() {
    if (stallTimer) clearTimeout(stallTimer)
    stallTimer = undefined
    if (disposed || !busy.value || link.value !== 'open') return
    const frames = frameCount
    stallTimer = setTimeout(() => {
      stallTimer = undefined
      if (frames === frameCount) void checkStall()
      else scheduleStallCheck()
    }, STALL_CHECK_MS)
  }

  async function checkStall() {
    const id = sessionId.value
    const generation = sessionGeneration
    const frames = frameCount
    const sock = ws
    try {
      const row = await transport.getSoloSession(id)
      if (disposed || generation !== sessionGeneration || ws !== sock || frames !== frameCount) return
      if (!row.busy && busy.value) {
        resubscribe()
        return
      }
    } catch {
      /* check again after the next quiet window */
    }
    scheduleStallCheck()
  }

  function onFrame(raw: string, firstFrame = false) {
    frameCount++
    const frame = parseFrame(raw)
    if (!frame || typeof frame !== 'object') return
    // Subscribe 保证首帧是 queue_state 快照；之后的同名帧才是真实流事件。
    const subscriptionSnapshot = firstFrame && frame.type === 'session' && frame.event === 'queue_state'
    const stateSnapshot = frame.type === 'session' && frame.event === 'queue_state' && frame.stateSnapshot === true
    if (subscriptionSnapshot) {
      sessionStateCursor.reset()
      journalVersion = frame.journalVersion === 1 ? 1 : 0
      if (journalVersion && frame.cursorReset) {
        lastSeq = ackedSeq = -1
        seenSeq.clear()
        endActive()
        void refetchMessages()
      }
      if (Number.isSafeInteger(frame.sessionGeneration) && frame.sessionGeneration >= 0) {
        if (!journalVersion && streamGeneration !== undefined && streamGeneration !== frame.sessionGeneration) {
          // A recreated server stream starts its sequence at zero. Same-stream
          // reconnects retain their deduplication and replay acknowledgements.
          lastSeq = -1
          ackedSeq = -1
          seenSeq.clear()
          resyncs = 0
        }
        streamGeneration = frame.sessionGeneration
      }
    }
    const seqState = stateSnapshot && !subscriptionSnapshot ? 'ok' : noteSeq(frame, subscriptionSnapshot)
    if (seqState === 'dup') return
    if (seqState === 'gap') {
      diagnostic('sequence_gap')
      requestResync()
      return
    }
    const seq = frame.seq
    if (!subscriptionSnapshot && !stateSnapshot && typeof seq === 'number' && seq >= 0 && seq > ackedSeq) ackedSeq = seq
    if (frame.type === 'acp') {
      // A reconnect may include the tail of an already completed turn before
      // the current one. Its result is loaded from history under its own ID.
      if (journalVersion && (!frame.userMsgId || frame.userMsgId !== activeId.value)) return
      const turn = activeTurn.value ?? adoptTurnForOutput()
      const send = openSends.find((row) => sendKey(row) === activeId.value)
      if (send) send.started = true
      if (turn) applyAcp(frame.data, turn, label, toolFallback())
      return
    }
    if (frame.type !== 'session') return
    if (!sessionStateCursor.acceptServer(frame)) return
    if (Number.isSafeInteger(frame.sessionGeneration) && frame.sessionGeneration >= 0) streamGeneration = frame.sessionGeneration
    switch (frame.event) {
      case 'queue_state': {
        const serverItems: SoloQueueItem[] = Array.isArray(frame.items) ? frame.items.map(queueItem) : []
        if (frame.activeItem) {
          // The active item precedes queued items, including identical prompts.
          // Rebuild it even when replay has no turn_begin after our cursor.
          beginTurn({ item: frame.activeItem })
        }
        for (const item of serverItems) adoptLocalSend(item)
        if (frame.userMsgId) {
          const send = openSends.find((row) => row.serverId === String(frame.userMsgId))
          if (send) send.started = true
        }
        const serverActiveId = String(frame.activeItem?.id || frame.userMsgId || '')
        for (const send of openSends) {
          if (!send.serverId || send.started) continue
          if (serverItems.some((item) => item.id === send.serverId) || serverActiveId === send.serverId) missingQueuedIds.delete(send.serverId)
          else if (!frame.busy || serverActiveId) missingQueuedIds.add(send.serverId)
        }
        const locals = queued.value.filter((item) => item.local && openSends.some((send) => send.localId === item.id && !send.serverId))
        queued.value = [...serverItems, ...locals]
        queueOrder.clear()
        busy.value = !!frame.busy
        patchSession({ busy: busy.value, waiting: queued.value.length })
        if (subscriptionSnapshot || stateSnapshot) applyResumeSnapshot(frame)
        applyExecution(frame)
        if (frame.busy && frame.phase) phase.value = frame.phase
        if (!frame.busy && (activeId.value || openSends.length)) {
          if (activeId.value) endActive()
          void refetchMessages()
        } else if (missingQueuedIds.size) void refetchMessages()
        break
      }
      case 'turn_begin':
        beginTurn(frame)
        applyExecution(frame)
        patchSession({ busy: true, waiting: queued.value.length })
        break
      case 'phase':
        phase.value = frame.phase || ''
        break
      case 'artifact': {
        if (!frame.userMsgId || frame.userMsgId !== activeId.value) break
        const turn = activeTurn.value
        if (turn && frame.userMsgId === activeId.value && frame.file?.artifactId && frame.file?.revision > 0) {
          const files = turn.files ?? []
          if (!files.some(file => file.artifactId === frame.file.artifactId && file.revision === frame.file.revision)) turn.files = [...files, frame.file]
        }
        break
      }
      case 'execution':
        applyExecution(frame)
        break
      case 'turn_done':
        applyExecution(frame)
        endActive(undefined, !!frame.interrupted)
        busy.value = queued.value.length > 0
        patchSession({ busy: busy.value, waiting: queued.value.length })
        void refetchMessages()
        break
      case 'error':
        applyExecution(frame)
        endActive(frame.message || t('pages.solo.chat.turnFailed'))
        busy.value = queued.value.length > 0
        patchSession({ busy: busy.value, waiting: queued.value.length })
        void refetchMessages()
        break
      case 'model': {
        // Ordinary acknowledgements can arrive before a newer queued choice.
        // A runtime rollback is authoritative and invalidates older HTTP replies.
        if (settings.pending() && !frame.modelNotice) break
        if (frame.modelNotice) settings.invalidate()
        const patch: Partial<SoloSession> = { modelNotice: typeof frame.modelNotice === 'string' ? frame.modelNotice : '' }
        if (typeof frame.currentModelId === 'string') { patch.currentModelId = frame.currentModelId; patch.currentModelSet = true }
        if (typeof frame.currentConfigurationId === 'string') patch.currentConfigurationId = frame.currentConfigurationId
        if (typeof frame.backend === 'string') patch.backend = frame.backend
        if (typeof frame.reasoningEffort === 'string') patch.reasoningEffort = frame.reasoningEffort
        if (Array.isArray(frame.reasoningEfforts)) patch.reasoningEfforts = frame.reasoningEfforts.filter((value: unknown): value is string => typeof value === 'string')
        patchSession(patch)
        // A saved choice from another tab supersedes metadata reads already in flight.
        sessionGeneration++
        if (session.value) settings.confirm(session.value)
        if (frame.modelNotice) showModelNotice(String(frame.modelNotice), String(frame.error || ''))
        break
      }
      case 'history_changed':
        void refetchMessages()
        break
      case 'status': {
        const cur = session.value
        if (cur) {
          patchSession({ status: frame.status, error: frame.error || '', sandboxId: frame.sandboxId || cur.sandboxId })
        }
        void refreshSession()
        if (frame.status === 'closed' || frame.status === 'error') void refetchMessages()
        break
      }
    }
  }

  function openWs() {
    closeWs(false)
    const id = sessionId.value
    if (!id || disposed) return
    diagnosticConnection++
    diagnosticReceived = 0
    diagnosticFrameStart = frameCount
    diagnosticLastReceived = diagnosticHead = -1
    diagnosticCaughtUp = false
    link.value = link.value === 'idle' ? 'connecting' : 'reconnecting'
    const cursor = ackedSeq >= 0 ? ackedSeq : undefined
    const address = journalVersion ? transport.soloChatWsUrl(id, cursor, journalVersion) : transport.soloChatWsUrl(id, cursor)
    let socketAddress = address
    if (transport.reportSoloDiagnostics) {
      try {
        const traced = new URL(address)
        traced.searchParams.set('view', diagnosticViewID)
        traced.searchParams.set('connection', String(diagnosticConnection))
        socketAddress = traced.toString()
      } catch { /* preserve the transport's address */ }
    }
    const sock = new WebSocket(socketAddress, transport.soloChatProtocols)
    ws = sock
    held = []
    diagnostic('ws_connecting')
    connectingTimer = setTimeout(() => {
      if (ws === sock && sock.readyState !== WebSocket.OPEN) diagnostic('ws_waiting', { elapsedMs: 5000 })
    }, 5000)
    let firstFrame = true
    const receive = (raw: string) => {
      const first = firstFrame
      firstFrame = false
      try { onFrame(raw, first) } catch (error) { diagnostic('frame_failed'); throw error }
      if (!diagnosticCaughtUp && diagnosticHead >= 0 && ackedSeq >= diagnosticHead) {
        diagnosticCaughtUp = true
        diagnostic('replay_caught_up')
      }
    }
    sock.onopen = async () => {
      if (ws !== sock) return
      clearTimeout(connectingTimer)
      reconnect.markOpened()
      link.value = 'open'
      schedulePoll()
      // KeepNone status/model frames are not replayed; refresh the session row
      // and also accept the subscribe snapshot below.
      void refreshSession()
      try {
        const messages = await readHistory(id)
        if (ws === sock) applyMessages(messages)
      } catch {
        /* live frames still render */
      }
      if (ws !== sock) return
      const pending = held ?? []
      held = null
      for (const raw of pending) receive(raw)
      diagnostic('replay_drained')
    }
    sock.onmessage = (ev) => {
      if (ws !== sock) return
      diagnosticReceived++
      const metadata = parseFrame(ev.data)
      if (Number.isSafeInteger(metadata?.seq)) diagnosticLastReceived = metadata.seq
      if (metadata?.stateSnapshot === true && Number.isSafeInteger(metadata.highWatermark)) diagnosticHead = metadata.highWatermark
      if (diagnosticReceived === 1) diagnostic('ws_snapshot')
      if (held) held.push(ev.data)
      else receive(ev.data)
    }
    sock.onclose = (event) => {
      if (ws !== sock) return
      diagnostic('ws_closed', { closeCode: event?.code || 0 })
      clearTimeout(connectingTimer)
      ws = null
      held = null
      if (disposed) return
      link.value = 'reconnecting'
      void refetchMessages()
      schedulePoll()
      reconnect.onClose()
    }
  }

  function closeWs(intentional = true) {
    clearTimeout(connectingTimer)
    const sock = ws
    ws = null
    held = null
    if (intentional) reconnect.markIntentionalClose()
    sock?.close()
  }

  async function load() {
    for (const timer of historyTimers.values()) clearTimeout(timer)
    historyTimers.clear()
    executions.clear()
    const id = sessionId.value
    chatEpoch++
    const generation = ++sessionGeneration
    settings.reset()
    closeWs()
    reconnect.reset()
    sessionStateCursor.reset()
    turns.value = []
    queued.value = []
    queueOrder.clear()
    openSends.splice(0, openSends.length)
    pendingConfirmation.value = []
    knownServerIds.clear()
    missingQueuedIds.clear()
    persistedHistory = []
    historyMessages.clear()
    historyOrder = []
    processEvents.clear()
    processPages.value = {}
    hasEarlier.value = false
    loadingEarlier.value = false
    earlierError.value = ''
    beforeMessage = undefined
    earlierLoaded = false
    historyReady.value = false
    busy.value = false
    phase.value = ''
    activeId.value = ''
    lastSeq = -1
    ackedSeq = -1
    streamGeneration = undefined
    journalVersion = 0
    resyncs = 0
    seenSeq.clear()
    link.value = 'idle'
    loadError.value = ''
    actionError.value = ''
    session.value = null
    outboxReady.value = false
    if (!id) return
    loading.value = true
    try {
      const v = await transport.getSoloSession(id)
      if (id !== sessionId.value || generation !== sessionGeneration || disposed) return
      setSession(v)
      try {
        const scope = soloOutboxScope(v.userId, 'message', v.id)
        const saved = await readSoloOutbox(scope)
        if (id !== sessionId.value || generation !== sessionGeneration || disposed) return
        for (const record of saved) {
          if (record.kind !== 'message' || record.request.clientMessageId !== record.id) continue
          const request = record.request
          const references: MessageReference[] = (request.references || []).map((reference, index) => ({ ...reference, id: `${record.id}:${index}`, title: reference.target, createdAt: record.createdAt }))
          const send: LocalSend = { localId: record.id, text: request.text, images: request.images || [], references, annotations: request.annotations || [], started: false, request, outboxScope: scope }
          displayLocalSend(send, record.createdAt, true)
          pendingConfirmation.value.push(send.localId)
        }
        outboxReady.value = true
        if (pendingConfirmation.value.length) actionError.value = t('pages.solo.outboxRestored')
      } catch {
        actionError.value = t('pages.solo.outboxUnavailable')
      }
      if (v.modelNotice) showModelNotice(v.modelNotice)
      openWs()
    } catch (e: any) {
      if (id === sessionId.value && generation === sessionGeneration) loadError.value = String(e?.message || e)
    } finally {
      if (id === sessionId.value && generation === sessionGeneration) loading.value = false
    }
  }

  function displayLocalSend(send: LocalSend, started: string, queuedOnly = false) {
    const local: SoloQueueItem = { id: send.localId, clientMessageId: send.localId, text: send.text, images: send.images, references: send.references, annotations: send.annotations, local: true }
    openSends.push(send)
    // A send behind a running turn stays in the queue until its turn_begin,
    // matching the persisted transcript a reload shows.
    if (!activeId.value && !queuedOnly) {
      turns.value = [
        ...turns.value,
        {
          id: `u:${local.id}`,
          role: 'user',
          text: send.text,
          tools: [],
          steps: [],
          plan: [],
          streaming: false,
          images: send.images.length ? send.images : undefined,
          references: send.references,
          annotations: send.annotations,
          createdAt: started,
        },
        emptyAgentTurn(`live:${local.id}`, true, started),
      ]
      activeId.value = local.id
      if (!phase.value) phase.value = 'preparing'
    }
    queued.value = [...queued.value, local]
    patchSession({ waiting: queued.value.length, busy: true })
  }

  async function send(text: string, images: ClarifyImage[] = [], references: MessageReference[] = [], annotations: ReactAnnotation[] = []) {
    const id = sessionId.value
    const epoch = chatEpoch
    if (!id || !session.value || disposed || !canSend.value || (!text.trim() && !images.length && !references.length && !annotations.length)) return false
    actionError.value = ''
    const body = text.trim()
    const scope = soloOutboxScope(session.value.userId, 'message', session.value.id)
    const localId = newSoloRequestID()
    const request: SoloTurnRequest = JSON.parse(JSON.stringify({
        text: body, images, clientMessageId: localId,
        ...(references.length ? { references: references.map(({ kind, target }) => ({ kind, target })) } : {}),
        ...(annotations.length ? { annotations } : {}),
      }))
    const record: SoloMessageOutbox = { scope, id: localId, kind: 'message', request, createdAt: new Date().toISOString() }
    const send: LocalSend = { localId, text: body, images: request.images || [], references: JSON.parse(JSON.stringify(references)), annotations: request.annotations || [], started: false, request, outboxScope: scope }
    displayLocalSend(send, record.createdAt)
    try {
      await saveSoloOutbox(record)
    } catch {
      if (!disposed && epoch === chatEpoch) {
        forgetSend(send)
        patchSession({ waiting: queued.value.length, busy: queued.value.length > 0 || !!activeId.value })
        actionError.value = t('pages.solo.outboxUnavailable')
      }
      return false
    }
    if (!isCurrent(id, epoch)) return false
    try {
      const receipt = await transport.sendSoloTurn(id, request)
      if (!disposed && epoch === chatEpoch && id === sessionId.value && receipt.turnId && receipt.clientMessageId === send.localId) {
        adoptLocalSend({ id: receipt.turnId, clientMessageId: receipt.clientMessageId, text: body, images, annotations, references })
      }
      if (!send.serverId) throw new Error(t('pages.solo.invalidReceipt'))
      return !disposed && epoch === chatEpoch && id === sessionId.value
    } catch (e: any) {
      if (!isCurrent(id, epoch)) return false
      // The socket can confirm acceptance before the HTTP connection fails.
      if (send.serverId) return true
      if (!openSends.includes(send)) return false
      if (!e?.status || e.status >= 500) {
        pendingConfirmation.value = [...new Set([...pendingConfirmation.value, send.localId])]
        actionError.value = t('pages.solo.sendUnconfirmed')
        return false
      }
      try { await removeSoloOutbox(scope, send.localId) } catch { /* preserve the original identity if local storage is unavailable */ }
      forgetSend(send)
      patchSession({ waiting: queued.value.length, busy: queued.value.length > 0 || !!activeId.value })
      actionError.value = String(e?.message || e)
      return false
    }
  }

  async function retryPendingSend() {
    if (retryingSend.value || !sessionId.value || !sessionOpen.value || session.value?.sandbox?.desiredState === 'stopped') return false
    const id = sessionId.value, epoch = chatEpoch
    retryingSend.value = true
    try {
      for (const key of [...pendingConfirmation.value]) {
        const send = openSends.find(row => row.localId === key)
        if (!send?.request) continue
        const receipt = await transport.sendSoloTurn(id, send.request)
        if (disposed || epoch !== chatEpoch || sessionId.value !== id) return false
        if (receipt.turnId && receipt.clientMessageId === send.localId) {
          adoptLocalSend({ id: receipt.turnId, clientMessageId: receipt.clientMessageId, text: send.text, images: send.images, annotations: send.annotations, references: send.references })
        }
        if (!send.serverId) throw new Error(t('pages.solo.invalidReceipt'))
      }
      if (!pendingConfirmation.value.length) actionError.value = ''
      return !pendingConfirmation.value.length
    } catch (e: any) {
      if (!disposed && epoch === chatEpoch && sessionId.value === id) actionError.value = String(e?.message || e)
      return false
    } finally {
      retryingSend.value = false
    }
  }

  async function stopLocalRetry() {
    if (retryingSend.value) return
    for (const key of [...pendingConfirmation.value]) {
      const send = openSends.find(row => row.localId === key)
      if (!send?.outboxScope) continue
      try { await removeSoloOutbox(send.outboxScope, send.localId) } catch {
        actionError.value = t('pages.solo.outboxUnavailable')
        return
      }
      // An acknowledgement can arrive while IndexedDB commits the removal.
      // Stopping local retries never removes an already accepted server item.
      if (send.serverId) continue
      forgetSend(send)
    }
    actionError.value = ''
  }

  async function cancel() {
    const id = sessionId.value
    const epoch = chatEpoch
    if (!id) return
    if (!transport.soloChatProtocols && ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'cancel' }))
    else await transport.cancelSoloTurn(id).catch(() => undefined)
    if (!isCurrent(id, epoch)) return
    dropQueuedSends()
    queued.value = []
    patchSession({ waiting: 0, busy: false })
  }

  /** True only when the server removed the item for this still-current session. */
  async function dropQueued(turnId: string): Promise<boolean> {
    const id = sessionId.value
    const epoch = chatEpoch
    const before = queued.value
    queued.value = before.filter((q) => q.id !== turnId)
    let ok = false
    try {
      await transport.removeSoloQueued(id, turnId)
      if (!isCurrent(id, epoch)) return false
      const send = openSends.find((row) => sendKey(row) === turnId)
      if (send && !send.started) forgetSend(send)
      ok = true
    } catch {
      if (!isCurrent(id, epoch)) return false
      const removed = before.find((row) => row.id === turnId)
      if (removed && activeId.value !== turnId && !queued.value.some((row) => row.id === turnId)) {
        const next = queued.value.slice()
        next.splice(Math.min(before.indexOf(removed), next.length), 0, removed)
        queued.value = next
      }
    }
    patchSession({ waiting: queued.value.length, busy: busy.value || queued.value.length > 0 })
    return ok
  }

  async function removeQueued(turnId: string) {
    await dropQueued(turnId)
  }

  /**
   * Withdraw a queued item for editing. Returns its content only once the
   * server removed it; an item that already began is never handed back.
   */
  async function takeQueued(turnId: string): Promise<SoloQueueItem | null> {
    const target = queued.value.find((q) => q.id === turnId)
    if (!target || target.local) return null
    const copy: SoloQueueItem = {
      id: target.id,
      text: target.text,
      images: target.images.map((image) => ({ ...image })),
      ...(target.annotations?.length ? { annotations: target.annotations.map(pick => ({ ...pick })) } : {}),
      ...(target.references?.length ? { references: target.references.map((ref) => ({ ...ref })) } : {}),
    }
    return (await dropQueued(turnId)) ? copy : null
  }

  watch(sessionId, () => void load(), { immediate: true })
  watch([busy, link], scheduleStallCheck)

  onBeforeUnmount(() => {
    diagnostic('ws_closed')
    disposed = true
    for (const timer of historyTimers.values()) clearTimeout(timer)
    historyTimers.clear()
    if (pollTimer) clearTimeout(pollTimer)
    if (stallTimer) clearTimeout(stallTimer)
    closeWs()
  })

  return {
    session,
    turns,
    queued,
    displayQueued,
    busy,
    phase,
    link,
    loading,
    loadError,
    actionError,
    pendingConfirmation,
    stopLocalRetry,
    retryingSend,
    retryPendingSend,
    hasEarlier,
    loadingEarlier,
    earlierError,
    loadEarlier,
    processPages,
    loadProcess,
    status,
    closed,
    canSend,
    currentModelId,
    currentConfigurationId,
    currentReasoningEffort,
    modelChangePending: settings.modelChangePending,
    load,
    send,
    setCurrentModel: settings.setCurrentModel,
    setCurrentReasoningEffort: settings.setCurrentReasoningEffort,
    cancel,
    removeQueued,
    takeQueued,
    reorderQueued: queueOrder.reorderQueued,
    refreshSession,
  }
}
