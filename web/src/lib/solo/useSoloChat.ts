import { computed, onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { SoloMessage, SoloSession } from '@/lib/api/apiTypes'
import type { ClarifyImage } from '@/lib/shared/types'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { applyAcp, emptyAgentTurn, turnsFromMessages, type SoloTurn } from './acpTurn'
import { soloErrorText } from './soloErrors'
import { useSoloSessions } from './useSoloSessions'

export type SoloQueueItem = { id: string; text: string; images: ClarifyImage[]; local?: boolean }
export type SoloPhase = '' | 'preparing' | 'running'
export type SoloLink = 'idle' | 'connecting' | 'open' | 'reconnecting'

const SANDBOX_POLL_MS = 2500

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
    text: String(raw?.text ?? ''),
    images: Array.isArray(raw?.images) ? raw.images : [],
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
  const phase = ref<SoloPhase>('')
  const link = ref<SoloLink>('idle')
  const loading = ref(false)
  const loadError = ref('')
  const actionError = ref('')

  let ws: WebSocket | null = null
  let held: string[] | null = null
  const activeId = ref('')
  let disposed = false
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let refetchSeq = 0
  // lastSeq 是缺口检测高水位（含快照）；ackedSeq 仅确认已处理的真实帧，供重连回放。
  let lastSeq = -1
  let ackedSeq = -1
  let resyncs = 0
  let sessionGeneration = 0
  let settingsEpoch = 0
  let settingSequence = 0
  let pendingSettings = 0
  let pendingModels = 0
  let settingsQueue: Promise<void> = Promise.resolve()
  type ModelSettings = Pick<SoloSession, 'currentModelId' | 'currentModelSet' | 'modelNotice' | 'reasoningEffort' | 'reasoningEfforts'>
  let confirmedSettings: ModelSettings | null = null
  const modelChangePending = ref(false)
  // 真实帧按序号和事件类型去重；订阅快照不进入此表，避免吞掉同序号的开场帧。
  const seenSeq = new Map<number, Set<string>>()
  /** Accepted sends still missing from history. baseline is how many same-text user rows already existed. */
  const openSends: { key: string; text: string; baseline: number }[] = []

  const reconnect = createWsReconnectController({
    connect: () => openWs(),
    shouldReconnect: () => !disposed && !!sessionId.value,
  })

  const status = computed(() => session.value?.status ?? 'starting')
  const closed = computed(() => status.value === 'closed')
  const canSend = computed(() => !!session.value && (status.value === 'starting' || status.value === 'running'))
  // An explicit current model wins, including empty (primary). Old sessions
  // omit the flag and keep the opening model.
  const currentModelId = computed(() => {
    const row = session.value
    if (!row) return ''
    if (row.currentModelSet) return row.currentModelId || ''
    return row.initialModelId || ''
  })
  const currentReasoningEffort = computed(() => session.value?.reasoningEffort || '')
  const modelSwitchNotice = ref('')
  const activeTurn = computed(() => (activeId.value ? turns.value.find((x) => x.id === `live:${activeId.value}`) ?? null : null))

  function modelSettings(row: SoloSession): ModelSettings {
    return { currentModelId: row.currentModelId, currentModelSet: row.currentModelSet, modelNotice: row.modelNotice, reasoningEffort: row.reasoningEffort, reasoningEfforts: row.reasoningEfforts ? [...row.reasoningEfforts] : undefined }
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
    const catchUp = st === 'running' && link.value !== 'open'
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

  function historySettles(
    history: SoloTurn[],
    send: { text: string; baseline: number },
    liveAgent: SoloTurn | undefined,
  ): boolean {
    let seen = 0
    for (let i = 0; i < history.length; i++) {
      const row = history[i]
      if (!row || row.role !== 'user' || row.text !== send.text) continue
      if (seen !== send.baseline) {
        seen++
        continue
      }
      const next = history[i + 1]
      if (!next || next.role === 'user') return false
      return (next.text || '').length >= (liveAgent?.text || '').length
    }
    return false
  }

  /** Rows for accepted sends that this snapshot does not contain yet. */
  function rowsToKeep(history: SoloTurn[]): SoloTurn[] {
    const kept: SoloTurn[] = []
    for (let i = openSends.length - 1; i >= 0; i--) {
      const send = openSends[i]!
      const user = turns.value.find((turn) => turn.id === `u:${send.key}`)
      const agent = turns.value.find((turn) => turn.id === `live:${send.key}`)
      if (historySettles(history, send, agent)) {
        openSends.splice(i, 1)
        if (activeId.value === send.key) {
          activeId.value = ''
          phase.value = ''
        }
        continue
      }
      const seen = history.filter((turn) => turn.role === 'user' && turn.text === send.text).length
      if (user && seen <= send.baseline) kept.push(user)
      if (agent) kept.push(agent)
    }
    return kept
  }

  function applyMessages(messages: SoloMessage[]) {
    const history = turnsFromMessages(messages, label, toolFallback())
    const kept = rowsToKeep(history)
    const active = activeId.value
    if (!active) {
      const ids = new Set(kept.map((turn) => turn.id))
      turns.value = [...history.filter((turn) => !ids.has(turn.id)), ...kept]
      return
    }
    const live = turns.value.filter((turn) => turn.id === `u:${active}` || turn.id === `live:${active}`)
    const extras = kept.filter((turn) => !live.some((row) => row.id === turn.id))
    const drop = new Set([...live, ...extras].map((turn) => turn.id))
    turns.value = [...history.filter((turn) => !drop.has(turn.id) && turn.id !== `u:${active}`), ...live, ...extras]
  }

  async function refetchMessages() {
    const id = sessionId.value
    const seq = ++refetchSeq
    try {
      const messages = await api.listSoloMessages(id)
      if (seq === refetchSeq && id === sessionId.value) applyMessages(messages)
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
    activeId.value = ''
    phase.value = ''
  }

  /** Point the accepted local row at the server id so turn_begin does not add a second one. */
  function adoptLocalSend(serverId: string, text: string) {
    if (!serverId || !text) return
    const send = openSends.find((row) => row.text === text && row.key !== serverId)
    if (!send) return
    const old = send.key
    send.key = serverId
    for (const row of turns.value) {
      if (row.id === `u:${old}`) row.id = `u:${serverId}`
      if (row.id === `live:${old}`) row.id = `live:${serverId}`
    }
    if (activeId.value === old) activeId.value = serverId
    queued.value = queued.value.filter((item) => item.id !== old)
  }

  function beginTurn(frame: any) {
    const item = queueItem(frame.item)
    const id = item.id || String(frame.userMsgId ?? '')
    adoptLocalSend(id, item.text)
    queued.value = queued.value.filter((q) => q.id !== id && !q.local)
    if (activeId.value && activeId.value !== id) endActive()
    const existing = turns.value.find((x) => x.id === `live:${id}`)
    const hasOutput = !!existing && (!!existing.text || existing.steps.length > 0 || existing.tools.length > 0)
    if (hasOutput && existing) {
      activeId.value = id
      if (!turns.value.some((x) => x.id === `u:${id}`)) {
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
    turns.value = [
      ...turns.value.filter((x) => x.id !== `u:${id}` && x.id !== `live:${id}`),
      { id: `u:${id}`, role: 'user', text: item.text, tools: [], steps: [], plan: [], streaming: false, images: item.images.length ? item.images : undefined, createdAt: started },
      emptyAgentTurn(`live:${id}`, true, started),
    ]
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
    const sock = ws
    ws = null
    held = null
    link.value = 'reconnecting'
    sock?.close()
    void refetchMessages()
    schedulePoll()
    reconnect.onClose()
  }

  function onFrame(raw: string, firstFrame = false) {
    const frame = parseFrame(raw)
    if (!frame || typeof frame !== 'object') return
    // Subscribe 保证首帧是 queue_state 快照；之后的同名帧才是真实流事件。
    const subscriptionSnapshot = firstFrame && frame.type === 'session' && frame.event === 'queue_state'
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
      if (turn) applyAcp(frame.data, turn, label, toolFallback())
      return
    }
    if (frame.type !== 'session') return
    switch (frame.event) {
      case 'queue_state': {
        const serverItems: SoloQueueItem[] = Array.isArray(frame.items) ? frame.items.map(queueItem) : []
        const locals = queued.value.filter((item) => item.local && !serverItems.some((server) => server.id === item.id))
        queued.value = [...serverItems, ...locals]
        busy.value = !!frame.busy
        patchSession({ busy: busy.value, waiting: queued.value.length })
        if (subscriptionSnapshot) applyResumeSnapshot(frame)
        if (frame.busy && frame.phase) phase.value = frame.phase
        if (!frame.busy && (activeId.value || openSends.length)) {
          if (activeId.value) endActive()
          void refetchMessages()
        }
        break
      }
      case 'turn_begin':
        beginTurn(frame)
        patchSession({ busy: true, waiting: queued.value.length })
        break
      case 'phase':
        phase.value = frame.phase || ''
        break
      case 'turn_done':
        endActive(undefined, !!frame.interrupted)
        busy.value = queued.value.length > 0
        modelSwitchNotice.value = ''
        patchSession({ busy: busy.value, waiting: queued.value.length })
        void refetchMessages()
        break
      case 'error':
        modelSwitchNotice.value = ''
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
        if (typeof frame.reasoningEffort === 'string') patch.reasoningEffort = frame.reasoningEffort
        if (Array.isArray(frame.reasoningEfforts)) patch.reasoningEfforts = frame.reasoningEfforts.filter((value: unknown): value is string => typeof value === 'string')
        patchSession(patch)
        if (session.value) confirmedSettings = modelSettings(session.value)
        modelSwitchNotice.value = ''
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
    const id = sessionId.value
    const generation = ++sessionGeneration
    settingsEpoch++
    settingSequence++
    pendingSettings = 0
    pendingModels = 0
    confirmedSettings = null
    modelChangePending.value = false
    modelSwitchNotice.value = ''
    settingsQueue = Promise.resolve()
    closeWs()
    reconnect.reset()
    turns.value = []
    queued.value = []
    openSends.splice(0, openSends.length)
    busy.value = false
    phase.value = ''
    activeId.value = ''
    lastSeq = -1
    ackedSeq = -1
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

  async function send(text: string, images: ClarifyImage[] = []) {
    const id = sessionId.value
    if (!id || !canSend.value || (!text.trim() && !images.length)) return false
    actionError.value = ''
    const body = text.trim()
    const local: SoloQueueItem = { id: newLocalId(), text: body, images, local: true }
    const baseline = turns.value.filter((turn) => turn.role === 'user' && turn.text === body).length
    openSends.push({ key: local.id, text: body, baseline })
    const started = new Date().toISOString()
    const makeActive = !activeId.value
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
        createdAt: started,
      },
      emptyAgentTurn(`live:${local.id}`, makeActive, started),
    ]
    if (makeActive) {
      activeId.value = local.id
      if (!phase.value) phase.value = 'preparing'
    }
    queued.value = [...queued.value, local]
    patchSession({ waiting: queued.value.length, busy: true })
    try {
      await api.sendSoloTurn(id, { text: body, images })
      return true
    } catch (e: any) {
      queued.value = queued.value.filter((q) => q.id !== local.id)
      const openAt = openSends.findIndex((row) => row.key === local.id)
      if (openAt >= 0) openSends.splice(openAt, 1)
      turns.value = turns.value.filter((turn) => turn.id !== `u:${local.id}` && turn.id !== `live:${local.id}`)
      if (activeId.value === local.id) {
        activeId.value = ''
        phase.value = ''
      }
      patchSession({ waiting: queued.value.length, busy: queued.value.length > 0 || !!activeId.value })
      actionError.value = String(e?.message || e)
      return false
    }
  }

  async function saveModelSetting(kind: 'model' | 'effort', value: string): Promise<boolean> {
    const id = sessionId.value
    const cur = session.value
    if (!id || !cur || !canSend.value || disposed) return false
    const next = value.trim()
    if (kind === 'model' ? cur.currentModelSet && (cur.currentModelId || '') === next : (cur.reasoningEffort || '') === next) return true
    const generation = sessionGeneration
    const epoch = settingsEpoch
    const sequence = ++settingSequence
    confirmedSettings ||= modelSettings(cur)
    pendingSettings++
    if (kind === 'model') { pendingModels++; modelChangePending.value = true }
    actionError.value = ''
    const deferNotice = busy.value
    patchSession(kind === 'model' ? { currentModelId: next, currentModelSet: true, reasoningEffort: '', reasoningEfforts: [], modelNotice: '' } : { reasoningEffort: next, modelNotice: '' })
    modelSwitchNotice.value = deferNotice ? t(kind === 'model' ? 'pages.solo.modelSelect.pending' : 'pages.solo.modelSelect.effortPending') : ''
    const isCurrentSession = () => !disposed && id === sessionId.value && generation === sessionGeneration && epoch === settingsEpoch
    const latest = () => isCurrentSession() && sequence === settingSequence
    const request = settingsQueue.then(async () => {
      try {
        if (!isCurrentSession()) return false
        if (!canSend.value) {
          if (latest() && confirmedSettings) { patchSession(confirmedSettings); modelSwitchNotice.value = '' }
          return false
        }
        const row = kind === 'model' ? await api.setSoloSessionModel(id, next) : await api.setSoloSessionReasoningEffort(id, next)
        if (!isCurrentSession() || row.id !== id) return false
        confirmedSettings = modelSettings(row)
        if (latest()) {
          patchSession(modelSettings(row))
          if (!(deferNotice && busy.value)) modelSwitchNotice.value = ''
        }
        return true
      } catch (error) {
        if (latest()) {
          if (confirmedSettings) patchSession(confirmedSettings)
          modelSwitchNotice.value = ''
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
    if (!id) return
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'cancel' }))
    else await api.cancelSoloTurn(id).catch(() => undefined)
    queued.value = []
    patchSession({ waiting: 0, busy: false })
  }

  async function removeQueued(turnId: string) {
    const id = sessionId.value
    const before = queued.value
    queued.value = before.filter((q) => q.id !== turnId)
    try {
      await api.removeSoloQueued(id, turnId)
    } catch {
      queued.value = before
    }
    patchSession({ waiting: queued.value.length, busy: busy.value || queued.value.length > 0 })
  }

  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    actionError.value = ''
    try {
      return await fn()
    } catch (e: any) {
      actionError.value = String(e?.message || e)
      return undefined
    }
  }

  async function close() {
    const v = await run(() => api.closeSoloSession(sessionId.value))
    if (v) {
      setSession(v)
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

  watch(busy, (now, was) => {
    if (was && !now) modelSwitchNotice.value = ''
  })

  watch(sessionId, () => void load(), { immediate: true })

  onBeforeUnmount(() => {
    disposed = true
    if (pollTimer) clearTimeout(pollTimer)
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
    currentReasoningEffort,
    modelChangePending,
    modelSwitchNotice,
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
