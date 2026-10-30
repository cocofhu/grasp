import { parseModelRoute } from '@/lib/agent/modelBindings'
import { computed, onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { MessageReference, SoloMessage, SoloSession } from '@/lib/api/apiTypes'
import type { ChatExecution, ClarifyImage } from '@/lib/shared/types'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { createReviewStateCursor } from '@/lib/chat/sessionQueue'
import { applyAcp, emptyAgentTurn, turnsFromMessages, type SoloTurn } from './acpTurn'
import { soloErrorText } from './soloErrors'
import { useSoloSessions } from './useSoloSessions'

export type SoloQueueItem = { id: string; text: string; images: ClarifyImage[]; references?: MessageReference[]; local?: boolean }
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

function listedEfforts(efforts?: string[]) {
  return [...new Set((efforts || []).map(value => value.trim()).filter(Boolean))]
}

function queueItem(raw: any): SoloQueueItem {
  return {
    id: String(raw?.id ?? ''),
    text: String(raw?.text ?? ''),
    images: Array.isArray(raw?.images) ? raw.images : [],
    ...(Array.isArray(raw?.references) && raw.references.length ? { references: raw.references } : {}),
  }
}

/**
 * Live state of one Solo session: transcript from persisted messages plus the
 * active turn streamed over the session WebSocket (which replays the running
 * turn on reconnect, so a dropped socket only pauses the view).
 */
export function useSoloChat(sessionId: Ref<string>) {
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
  let resyncs = 0
  let sessionGeneration = 0
  let settingsEpoch = 0
  let settingSequence = 0
  let pendingSettings = 0
  let pendingModels = 0
  let settingsQueue: Promise<void> = Promise.resolve()
  type ModelSettings = Pick<SoloSession, 'currentModelId' | 'currentConfigurationId' | 'currentModelSet' | 'modelNotice' | 'reasoningEffort' | 'reasoningEfforts' | 'backend' | 'model'>
  let confirmedSettings: ModelSettings | null = null
  const modelChangePending = ref(false)
  // 真实帧按序号和事件类型去重；订阅快照不进入此表，避免吞掉同序号的开场帧。
  const seenSeq = new Map<number, Set<string>>()
  type LocalSend = {
    localId: string
    serverId?: string
    text: string
    images: ClarifyImage[]
    references: MessageReference[]
    started: boolean
  }
  // The local identity survives HTTP, queue acknowledgement and turn_begin.
  // A server identity can be assigned once; repeated text is never a turn id.
  const openSends: LocalSend[] = []
  const knownServerIds = new Set<string>()
  const missingQueuedIds = new Set<string>()
  let persistedHistory: SoloTurn[] = []
  let chatEpoch = 0

  const reconnect = createWsReconnectController({
    connect: () => openWs(),
    shouldReconnect: () => !disposed && !!sessionId.value,
  })

  const status = computed(() => session.value?.status ?? 'starting')
  const closed = computed(() => status.value === 'closed')
  const sessionOpen = computed(() => !!session.value && (status.value === 'starting' || status.value === 'running'))
  // Establish the old message identities before accepting a new send. An old
  // same-text row from a delayed initial read must never acknowledge it.
  const canSend = computed(() => sessionOpen.value && historyReady.value)
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

  function modelSettings(row: SoloSession): ModelSettings {
    return { currentConfigurationId: row.currentConfigurationId, backend: row.backend, model: row.model, currentModelId: row.currentModelId, currentModelSet: row.currentModelSet, modelNotice: row.modelNotice, reasoningEffort: row.reasoningEffort, reasoningEfforts: row.reasoningEfforts ? [...row.reasoningEfforts] : undefined }
  }

  function setSession(v: SoloSession) {
    if (v.id !== sessionId.value) return
    const preserveSelection = pendingSettings > 0 && session.value
    const next = preserveSelection ? { ...v, ...modelSettings(session.value!) } : v
    session.value = next
    if (!preserveSelection) confirmedSettings = modelSettings(v)
    sessions.upsert(next)
    schedulePoll()
  }

  function applyResumeSnapshot(frame: Record<string, any>) {
    if (pendingSettings) return
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
    if (session.value) confirmedSettings = modelSettings(session.value)
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
    if (!id || pendingSettings) return
    const generation = sessionGeneration
    const sequence = settingSequence
    try {
      const row = await api.getSoloSession(id)
      if (generation === sessionGeneration && sequence === settingSequence) setSession(row)
    } catch {
      schedulePoll()
    }
  }

  const sendKey = (send: LocalSend) => send.serverId || send.localId

  function samePayload(send: LocalSend, item: SoloQueueItem): boolean {
    const refs = item.references || []
    if (refs.length && (refs.length !== send.references.length || refs.some((ref, i) => ref.target !== send.references[i]!.target))) return false
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
  function adoptLocalSend(item: SoloQueueItem): LocalSend | undefined {
    if (!item.id) return undefined
    const bound = openSends.find((send) => send.serverId === item.id || send.localId === item.id)
    if (bound) return bound
    if (knownServerIds.has(item.id)) return undefined
    const send = openSends.find((row) => !row.serverId && samePayload(row, item))
    knownServerIds.add(item.id)
    if (!send) return undefined
    const old = send.localId
    send.serverId = item.id
    for (const row of turns.value) {
      if (row.id === `u:${old}`) row.id = `u:${item.id}`
      if (row.id === `live:${old}`) row.id = `live:${item.id}`
    }
    if (activeId.value === old) activeId.value = item.id
    queued.value = queued.value.filter((row) => row.id !== old)
    return send
  }

  function forgetSend(send: LocalSend) {
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
    if (!saved.text.trim().startsWith(live.text.trim())) return false
    if (live.error && !(saved.error || '').includes(live.error)) return false
    if (live.status && live.status !== saved.status) return false
    const thoughts = (turn: SoloTurn) => turn.steps.flatMap((step) => step.kind === 'thought' ? [step.text] : []).join('')
    if (!thoughts(saved).startsWith(thoughts(live))) return false
    if (live.plan.length && !saved.plan.length) return false
    return live.tools.every((tool) => saved.tools.some((other) => other.id === tool.id &&
      (!['completed', 'failed'].includes(tool.status) || other.status === tool.status) &&
      (!tool.input || (other.input || '').startsWith(tool.input)) &&
      (!tool.output || (other.output || '').startsWith(tool.output))))
  }

  function applyMessages(messages: SoloMessage[]) {
    const incoming = turnsFromMessages(messages, label, toolFallback())
    // Messages are append-only. A shorter HTTP snapshot must not erase rows
    // already reconciled from an earlier response.
    if (incoming.length >= persistedHistory.length) persistedHistory = incoming
    const history = persistedHistory.slice()
    for (const user of history) {
      if (user.role !== 'user') continue
      const send = adoptLocalSend({ id: user.id.slice(2), text: user.text, images: user.images || [] })
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

    const mergePair = (key: string) => {
      const user = turns.value.find((turn) => turn.id === `u:${key}`)
      const agent = turns.value.find((turn) => turn.id === `live:${key}`)
      const index = history.findIndex((turn) => turn.id === `u:${key}`)
      if (index < 0) {
        if (user) history.push(user)
        if (agent) history.push(agent)
      } else if (agent) {
        if (history[index + 1]?.role === 'agent') history.splice(index + 1, 1, agent)
        else history.splice(index + 1, 0, agent)
      }
    }

    const retained = new Set<string>()
    for (const send of [...openSends]) {
      const key = sendKey(send)
      const index = history.findIndex((turn) => turn.id === `u:${key}`)
      const live = turns.value.find((turn) => turn.id === `live:${key}`)
      // Keep an active stream until its terminal/idle frame: history may be
      // persisted before the last output frames have reached this browser.
      if (activeId.value !== key && index >= 0 && historyCoversLive(history[index + 1], live)) {
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

  async function refetchMessages() {
    const id = sessionId.value
    const epoch = chatEpoch
    const seq = ++refetchSeq
    try {
      const messages = await api.listSoloMessages(id)
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
      { id: `u:${id}`, role: 'user', text: item.text, tools: [], steps: [], plan: [], streaming: false, images: item.images.length ? item.images : undefined, references: item.references, createdAt: started },
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
      if (resyncs >= 3) {
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
    if (disposed || resyncs >= 3) return
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
      const row = await api.getSoloSession(id)
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
    if (subscriptionSnapshot) {
      sessionStateCursor.reset()
      if (Number.isSafeInteger(frame.sessionGeneration) && frame.sessionGeneration >= 0) {
        if (streamGeneration !== undefined && streamGeneration !== frame.sessionGeneration) {
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
    const seqState = noteSeq(frame, subscriptionSnapshot)
    if (seqState === 'dup') return
    if (seqState === 'gap') {
      requestResync()
      return
    }
    const seq = frame.seq
    if (!subscriptionSnapshot && typeof seq === 'number' && seq >= 0 && seq > ackedSeq) ackedSeq = seq
    if (frame.type === 'acp') {
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
        busy.value = !!frame.busy
        patchSession({ busy: busy.value, waiting: queued.value.length })
        if (subscriptionSnapshot) applyResumeSnapshot(frame)
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
        if (pendingSettings && !frame.modelNotice) break
        if (frame.modelNotice) {
          settingsEpoch++
          settingSequence++
          pendingSettings = 0
          pendingModels = 0
          modelChangePending.value = false
        }
        const patch: Partial<SoloSession> = { modelNotice: typeof frame.modelNotice === 'string' ? frame.modelNotice : '' }
        if (typeof frame.currentModelId === 'string') { patch.currentModelId = frame.currentModelId; patch.currentModelSet = true }
        if (typeof frame.currentConfigurationId === 'string') patch.currentConfigurationId = frame.currentConfigurationId
        if (typeof frame.backend === 'string') patch.backend = frame.backend
        if (typeof frame.reasoningEffort === 'string') patch.reasoningEffort = frame.reasoningEffort
        if (Array.isArray(frame.reasoningEfforts)) patch.reasoningEfforts = frame.reasoningEfforts.filter((value: unknown): value is string => typeof value === 'string')
        patchSession(patch)
        // A saved choice from another tab supersedes metadata reads already in flight.
        sessionGeneration++
        if (session.value) confirmedSettings = modelSettings(session.value)
        if (frame.modelNotice) showModelNotice(String(frame.modelNotice), String(frame.error || ''))
        break
      }
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
    link.value = link.value === 'idle' ? 'connecting' : 'reconnecting'
    const sock = new WebSocket(api.soloChatWsUrl(id, ackedSeq >= 0 ? ackedSeq : undefined))
    ws = sock
    held = []
    let firstFrame = true
    const receive = (raw: string) => {
      const first = firstFrame
      firstFrame = false
      onFrame(raw, first)
    }
    sock.onopen = async () => {
      if (ws !== sock) return
      reconnect.markOpened()
      link.value = 'open'
      schedulePoll()
      // KeepNone status/model frames are not replayed; refresh the session row
      // and also accept the subscribe snapshot below.
      void refreshSession()
      try {
        const messages = await api.listSoloMessages(id)
        if (ws === sock) applyMessages(messages)
      } catch {
        /* live frames still render */
      }
      if (ws !== sock) return
      const pending = held ?? []
      held = null
      for (const raw of pending) receive(raw)
    }
    sock.onmessage = (ev) => {
      if (ws !== sock) return
      if (held) held.push(ev.data)
      else receive(ev.data)
    }
    sock.onclose = () => {
      if (ws !== sock) return
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
    const sock = ws
    ws = null
    held = null
    if (intentional) reconnect.markIntentionalClose()
    sock?.close()
  }

  async function load() {
    executions.clear()
    const id = sessionId.value
    chatEpoch++
    const generation = ++sessionGeneration
    settingsEpoch++
    settingSequence++
    pendingSettings = 0
    pendingModels = 0
    confirmedSettings = null
    modelChangePending.value = false
    settingsQueue = Promise.resolve()
    closeWs()
    reconnect.reset()
    sessionStateCursor.reset()
    turns.value = []
    queued.value = []
    openSends.splice(0, openSends.length)
    knownServerIds.clear()
    missingQueuedIds.clear()
    persistedHistory = []
    historyReady.value = false
    busy.value = false
    phase.value = ''
    activeId.value = ''
    lastSeq = -1
    ackedSeq = -1
    streamGeneration = undefined
    resyncs = 0
    seenSeq.clear()
    link.value = 'idle'
    loadError.value = ''
    actionError.value = ''
    session.value = null
    if (!id) return
    loading.value = true
    try {
      const v = await api.getSoloSession(id)
      if (id !== sessionId.value || generation !== sessionGeneration || disposed) return
      setSession(v)
      if (v.modelNotice) showModelNotice(v.modelNotice)
      openWs()
    } catch (e: any) {
      if (id === sessionId.value && generation === sessionGeneration) loadError.value = String(e?.message || e)
    } finally {
      if (id === sessionId.value && generation === sessionGeneration) loading.value = false
    }
  }

  function newLocalId() {
    return `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
  }

  async function send(text: string, images: ClarifyImage[] = [], references: MessageReference[] = []) {
    const id = sessionId.value
    const epoch = chatEpoch
    if (!id || disposed || !canSend.value || (!text.trim() && !images.length && !references.length)) return false
    actionError.value = ''
    const body = text.trim()
    const refs = references.length ? references : undefined
    const local: SoloQueueItem = { id: newLocalId(), text: body, images, references: refs, local: true }
    const send: LocalSend = { localId: local.id, text: body, images, references, started: false }
    openSends.push(send)
    const started = new Date().toISOString()
    // A send behind a running turn stays in the queue until its turn_begin,
    // matching the persisted transcript a reload shows.
    if (!activeId.value) {
      turns.value = [
        ...turns.value,
        {
          id: `u:${local.id}`,
          role: 'user',
          text: body,
          tools: [],
          steps: [],
          plan: [],
          streaming: false,
          images: images.length ? images : undefined,
          references: refs,
          createdAt: started,
        },
        emptyAgentTurn(`live:${local.id}`, true, started),
      ]
      activeId.value = local.id
      if (!phase.value) phase.value = 'preparing'
    }
    queued.value = [...queued.value, local]
    patchSession({ waiting: queued.value.length, busy: true })
    try {
      await api.sendSoloTurn(id, { text: body, images, ...(refs ? { references: refs.map(({ kind, target }) => ({ kind, target })) } : {}) })
      return !disposed && epoch === chatEpoch && id === sessionId.value
    } catch (e: any) {
      if (disposed || epoch !== chatEpoch || id !== sessionId.value) return false
      // The socket can confirm acceptance before the HTTP connection fails.
      if (send.serverId) return true
      if (!openSends.includes(send)) return false
      forgetSend(send)
      patchSession({ waiting: queued.value.length, busy: queued.value.length > 0 || !!activeId.value })
      actionError.value = String(e?.message || e)
      return false
    }
  }

  async function saveModelSetting(kind: 'model' | 'effort', value: string): Promise<boolean> {
    const id = sessionId.value
    const cur = session.value
    if (!id || !cur || !sessionOpen.value || disposed) return false
    const route = kind === 'model' ? parseModelRoute(value.trim()) : undefined
    const next = route?.modelId ?? value.trim()
    const configurationId = route?.configurationId
    if (kind === 'model' ? cur.currentModelSet && (cur.currentModelId || '') === next && (configurationId === undefined || cur.currentConfigurationId === configurationId) : (cur.reasoningEffort || '') === next) return true
    const generation = sessionGeneration
    const epoch = settingsEpoch
    const sequence = ++settingSequence
    confirmedSettings ||= modelSettings(cur)
    pendingSettings++
    if (kind === 'model') { pendingModels++; modelChangePending.value = true }
    actionError.value = ''
    patchSession(kind === 'model' ? { currentModelId: next, ...(configurationId === undefined ? {} : { currentConfigurationId: configurationId }), currentModelSet: true, reasoningEffort: '', reasoningEfforts: [], modelNotice: '' } : { reasoningEffort: next, modelNotice: '' })
    const isCurrentSession = () => !disposed && id === sessionId.value && generation === sessionGeneration && epoch === settingsEpoch
    const latest = () => isCurrentSession() && sequence === settingSequence
    const request = settingsQueue.then(async () => {
      try {
        if (!isCurrentSession()) return false
        if (!sessionOpen.value) {
          if (latest() && confirmedSettings) patchSession(confirmedSettings)
          return false
        }
        let row = kind === 'model' ? configurationId === undefined ? await api.setSoloSessionModel(id, next) : await api.setSoloSessionModel(id, next, undefined, configurationId) : await api.setSoloSessionReasoningEffort(id, next)
        if (!isCurrentSession() || row.id !== id) return false
        // A model switch clears an effort the new model does not offer. The
        // selector shows that gap as the highest level, so persist that token.
        if (kind === 'model' && latest() && !row.reasoningEffort?.trim()) {
          const highest = listedEfforts(row.reasoningEfforts).at(-1)
          if (highest) {
            row = await api.setSoloSessionReasoningEffort(id, highest)
            if (!isCurrentSession() || row.id !== id) return false
          }
        }
        confirmedSettings = modelSettings(row)
        if (latest()) {
          patchSession(modelSettings(row))
        }
        return true
      } catch (error) {
        if (latest()) {
          if (confirmedSettings) patchSession(confirmedSettings)
          actionError.value = soloErrorText(error, t, te, locale.value)
        }
        return false
      } finally {
        if (isCurrentSession()) {
          pendingSettings = Math.max(0, pendingSettings - 1)
          if (kind === 'model') pendingModels = Math.max(0, pendingModels - 1)
          modelChangePending.value = pendingModels > 0
        }
      }
    })
    // Both controls share one queue, so an effort always follows earlier model
    // changes and rapid slider updates reach the server in selection order.
    settingsQueue = request.then(() => undefined)
    return request
  }

  function setCurrentModel(modelId: string) { return saveModelSetting('model', modelId) }
  function setCurrentReasoningEffort(effort: string) { return saveModelSetting('effort', effort) }

  async function cancel() {
    const id = sessionId.value
    const epoch = chatEpoch
    if (!id) return
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'cancel' }))
    else await api.cancelSoloTurn(id).catch(() => undefined)
    if (disposed || epoch !== chatEpoch || id !== sessionId.value) return
    dropQueuedSends()
    queued.value = []
    patchSession({ waiting: 0, busy: false })
  }

  async function removeQueued(turnId: string) {
    const id = sessionId.value
    const epoch = chatEpoch
    const before = queued.value
    queued.value = before.filter((q) => q.id !== turnId)
    try {
      await api.removeSoloQueued(id, turnId)
      if (disposed || epoch !== chatEpoch || id !== sessionId.value) return
      const send = openSends.find((row) => sendKey(row) === turnId)
      if (send && !send.started) forgetSend(send)
    } catch {
      if (disposed || epoch !== chatEpoch || id !== sessionId.value) return
      const removed = before.find((row) => row.id === turnId)
      if (removed && activeId.value !== turnId && !queued.value.some((row) => row.id === turnId)) {
        const next = queued.value.slice()
        next.splice(Math.min(before.indexOf(removed), next.length), 0, removed)
        queued.value = next
      }
    }
    patchSession({ waiting: queued.value.length, busy: busy.value || queued.value.length > 0 })
  }

  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    const epoch = chatEpoch
    actionError.value = ''
    try {
      const result = await fn()
      return !disposed && epoch === chatEpoch ? result : undefined
    } catch (e: any) {
      if (!disposed && epoch === chatEpoch) actionError.value = String(e?.message || e)
      return undefined
    }
  }

  async function close() {
    const v = await run(() => api.closeSoloSession(sessionId.value))
    if (v) {
      setSession(v)
      dropQueuedSends()
      endActive()
      queued.value = []
      busy.value = false
      await refetchMessages()
    }
    return !!v
  }

  async function rename(title: string) {
    const v = await run(() => api.renameSoloSession(sessionId.value, title))
    if (v && session.value) session.value = { ...session.value, title: v.title }
    return !!v
  }

  watch(sessionId, () => void load(), { immediate: true })
  watch([busy, link], scheduleStallCheck)

  onBeforeUnmount(() => {
    disposed = true
    if (pollTimer) clearTimeout(pollTimer)
    if (stallTimer) clearTimeout(stallTimer)
    closeWs()
  })

  return {
    session,
    turns,
    queued,
    busy,
    phase,
    link,
    loading,
    loadError,
    actionError,
    status,
    closed,
    canSend,
    currentModelId,
    currentConfigurationId,
    currentReasoningEffort,
    modelChangePending,
    load,
    send,
    setCurrentModel,
    setCurrentReasoningEffort,
    cancel,
    removeQueued,
    close,
    rename,
    refreshSession,
  }
}
