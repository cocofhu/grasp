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
  let activeId = ''
  let disposed = false
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let refetchSeq = 0

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
  const modelSwitchNotice = ref('')
  const activeTurn = computed(() => (activeId ? turns.value.find((x) => x.id === `live:${activeId}`) ?? null : null))

  function setSession(v: SoloSession) {
    if (v.id !== sessionId.value) return
    session.value = v
    sessions.upsert(v)
    schedulePoll()
  }

  function patchSession(patch: Partial<SoloSession>) {
    const cur = session.value
    if (!cur) return
    const next = { ...cur, ...patch }
    session.value = next
    sessions.upsert(next)
  }

  /** Polls while the sandbox boots so the startup card shows real stages. */
  function schedulePoll() {
    if (pollTimer) clearTimeout(pollTimer)
    pollTimer = undefined
    if (disposed || session.value?.status !== 'starting') return
    pollTimer = setTimeout(() => {
      pollTimer = undefined
      void refreshSession()
    }, SANDBOX_POLL_MS)
  }

  async function refreshSession() {
    const id = sessionId.value
    if (!id) return
    try {
      setSession(await api.getSoloSession(id))
    } catch {
      schedulePoll()
    }
  }

  function applyMessages(messages: SoloMessage[]) {
    const history = turnsFromMessages(messages, label, toolFallback())
    if (!activeId) {
      turns.value = history
      return
    }
    const live = turns.value.filter((x) => x.id === `u:${activeId}` || x.id === `live:${activeId}`)
    turns.value = [...history.filter((x) => x.id !== `u:${activeId}`), ...live]
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
    activeId = ''
    phase.value = ''
  }

  function beginTurn(frame: any) {
    const item = queueItem(frame.item)
    const id = item.id || String(frame.userMsgId ?? '')
    queued.value = queued.value.filter((q) => q.id !== id && !q.local)
    if (activeId && activeId !== id) endActive()
    const started = new Date().toISOString()
    turns.value = [
      ...turns.value.filter((x) => x.id !== `u:${id}` && x.id !== `live:${id}`),
      { id: `u:${id}`, role: 'user', text: item.text, tools: [], steps: [], plan: [], streaming: false, images: item.images.length ? item.images : undefined, createdAt: started },
      emptyAgentTurn(`live:${id}`, true, started),
    ]
    activeId = id
    busy.value = true
    if (!phase.value) phase.value = 'preparing'
  }

  function onFrame(raw: string) {
    const frame = parseFrame(raw)
    if (!frame || typeof frame !== 'object') return
    if (frame.type === 'acp') {
      const turn = activeTurn.value
      if (turn) applyAcp(frame.data, turn, label, toolFallback())
      return
    }
    if (frame.type !== 'session') return
    switch (frame.event) {
      case 'queue_state':
        queued.value = Array.isArray(frame.items) ? frame.items.map(queueItem) : []
        busy.value = !!frame.busy
        patchSession({ busy: busy.value, waiting: queued.value.length })
        if (frame.busy && frame.phase) phase.value = frame.phase
        if (!frame.busy && activeId) {
          endActive()
          void refetchMessages()
        }
        break
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
      case 'model':
        patchSession({
          currentModelId: typeof frame.currentModelId === 'string' ? frame.currentModelId : '',
          currentModelSet: true,
          modelNotice: typeof frame.modelNotice === 'string' ? frame.modelNotice : '',
        })
        modelSwitchNotice.value = ''
        if (frame.modelNotice) showModelNotice(String(frame.modelNotice), String(frame.error || ''))
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
    link.value = link.value === 'idle' ? 'connecting' : 'reconnecting'
    const sock = new WebSocket(api.soloChatWsUrl(id))
    ws = sock
    held = []
    sock.onopen = async () => {
      if (ws !== sock) return
      reconnect.markOpened()
      link.value = 'open'
      try {
        const messages = await api.listSoloMessages(id)
        if (ws === sock) applyMessages(messages)
      } catch {
        /* live frames still render */
      }
      if (ws !== sock) return
      const pending = held ?? []
      held = null
      for (const raw of pending) onFrame(raw)
    }
    sock.onmessage = (ev) => {
      if (ws !== sock) return
      if (held) held.push(ev.data)
      else onFrame(ev.data)
    }
    sock.onclose = () => {
      if (ws !== sock) return
      ws = null
      held = null
      if (disposed) return
      link.value = 'reconnecting'
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
    closeWs()
    reconnect.reset()
    turns.value = []
    queued.value = []
    busy.value = false
    phase.value = ''
    activeId = ''
    link.value = 'idle'
    loadError.value = ''
    actionError.value = ''
    session.value = null
    if (!id) return
    loading.value = true
    try {
      const v = await api.getSoloSession(id)
      if (id !== sessionId.value) return
      setSession(v)
      if (v.modelNotice) showModelNotice(v.modelNotice)
      openWs()
    } catch (e: any) {
      if (id === sessionId.value) loadError.value = String(e?.message || e)
    } finally {
      if (id === sessionId.value) loading.value = false
    }
  }

  function newLocalId() {
    return `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
  }

  async function send(text: string, images: ClarifyImage[] = []) {
    const id = sessionId.value
    if (!id || !canSend.value || (!text.trim() && !images.length)) return false
    actionError.value = ''
    const local: SoloQueueItem = { id: newLocalId(), text: text.trim(), images, local: true }
    queued.value = [...queued.value, local]
    patchSession({ waiting: queued.value.length, busy: true })
    try {
      await api.sendSoloTurn(id, { text: text.trim(), images })
      return true
    } catch (e: any) {
      queued.value = queued.value.filter((q) => q.id !== local.id)
      patchSession({ waiting: queued.value.length, busy: queued.value.length > 0 })
      actionError.value = String(e?.message || e)
      return false
    }
  }

  async function setCurrentModel(modelId: string) {
    const id = sessionId.value
    const cur = session.value
    if (!id || !cur || !canSend.value) return false
    const next = modelId.trim()
    if (cur.currentModelSet && (cur.currentModelId || '') === next) return true
    const prev = {
      currentModelId: cur.currentModelId,
      currentModelSet: cur.currentModelSet,
      modelNotice: cur.modelNotice,
    }
    actionError.value = ''
    const deferNotice = busy.value
    patchSession({ currentModelId: next, currentModelSet: true, modelNotice: '' })
    modelSwitchNotice.value = deferNotice ? t('pages.solo.modelSelect.pending') : ''
    try {
      const v = await api.setSoloSessionModel(id, next)
      if (id !== sessionId.value) return false
      setSession(v)
      if (!(deferNotice && busy.value)) modelSwitchNotice.value = ''
      return true
    } catch (e) {
      patchSession(prev)
      modelSwitchNotice.value = ''
      actionError.value = soloErrorText(e, t, te, locale.value)
      return false
    }
  }

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
    modelSwitchNotice,
    load,
    send,
    setCurrentModel,
    cancel,
    removeQueued,
    close,
    rename,
    refreshSession,
  }
}
