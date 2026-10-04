<script setup lang="ts">
import { ref, reactive, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import ReposEditor, { type RepoRow } from '@/components/ui/ReposEditor.vue'
import AcpStatusPill from '@/components/run/AcpStatusPill.vue'
import AgentToolGroup from '@/components/run/AgentToolGroup.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import StreamMarkdown from '@/components/run/StreamMarkdown.vue'
import { renderMarkdown, renderMarkdownBlocks, type MarkdownBlockCache } from '@/lib/shared/markdown'
import { api, type CreateAgentTestPayload, type SandboxView } from '@/lib/api/api'
import {
  SITE_ATTACH_MAX_BYTES,
  SITE_ATTACH_MAX_MIB,
  attachmentDisplayName,
  filesFromClipboard,
  findOversizedAttachments,
  formatSelectRejectMessage,
  formatSendRejectMessage,
  inferImageMimeFromUrl,
  isImageAttachment,
  readFilesAsAttachments,
} from '@/lib/shared/attachments'
import { chatImageSrc } from '@/lib/shared/compositeText'
import { contentText, flattenUpdate, normalizeKind, unwrapFrame } from '@/lib/run/acpUnpack'
import type { AgentPart } from '@/lib/shared/types'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'

// `attachId` attaches to an existing sandbox (skips the create flow) — used by
// the sandbox console's ACP tab. `embedded` hides the internal header/controls
// when a host page already provides its own chrome.
const props = defineProps<{
  profile: string
  attachId?: number
  embedded?: boolean
  /** Agent home project id from Studio; empty = unbound (no task-scheduler). */
  homeProjectId?: string
  /**
   * Required override for starting a test sandbox (project shared-config dialogue test).
   * Omitted when attaching to an existing sandbox via attachId.
   */
  createTest?: (profile: string, payload: CreateAgentTestPayload) => Promise<SandboxView>
}>()

const { t, te } = useI18n()
const blockCache: MarkdownBlockCache = new Map()
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

type Tool = { id: string; title: string; status: string; summary?: string; input?: string; output?: string }
/** Arrival order of the reply: text runs, or an index into Turn.tools. */
type Step = { kind: 'thought' | 'message'; text: string } | { kind: 'tool'; tool: number }
type ImageAtt = { data: string; mimeType: string; url: string; name?: string }
type QueueItem = { id: string; text: string; images: ImageAtt[] }
type Turn = {
  role: 'user' | 'agent'
  text: string
  thought: string
  tools: Tool[]
  steps?: Step[]
  plan: { content: string; status: string }[]
  streaming: boolean
  images?: ImageAtt[]
  error?: string
}

const sandbox = ref<SandboxView | null>(null)
const turns = ref<Turn[]>([])
const input = ref('')
const status = ref<'idle' | 'starting' | 'ready' | 'thinking' | 'closed' | 'error'>('idle')
const errorMsg = ref('')
type LaunchMode = 'empty' | 'clone'
const launchMode = ref<LaunchMode>('empty')
const repos = ref<RepoRow[]>([{ name: '', url: '', branch: '' }])
const scroller = ref<HTMLElement | null>(null)
const attachments = ref<ImageAtt[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
// Not-yet-started sends as last reported by the server (plus optimistic local
// sends). Items live ONLY in the bottom queue panel — no conversation bubble is
// created until the server starts the turn (turn_begin). This avoids showing
// "queued" bubbles prematurely in the transcript.
const queued = ref<QueueItem[]>([])
// True while rebuilding a reused sandbox's prior transcript, so the UI can show
// a "loading history" indicator instead of a blank (seemingly frozen) panel.
const restoring = ref(false)
const loadingEarlier = ref(false)
const historyHasMore = ref(false)
const historyCursor = ref('')
let rawFrames: any[] = []
let activeIdx = -1
let activeItemId = ''
let ws: WebSocket | null = null
let wsSandboxId = 0
let wsAttempt = 0
let wsReconnectTimer: number | undefined
// Frames that arrive before the reused sandbox's history is restored.
let heldFrames: string[] | null = null
let disposed = false
// Local copies of sent items so turn_begin can show their previews.
const sentItems = new Map<string, QueueItem>()

const statusLabel = computed(() => ({
  idle: t('pages.agentChatTester.status.idle'),
  starting: t('pages.agentChatTester.status.starting'),
  ready: t('pages.agentChatTester.status.ready'),
  thinking: t('pages.agentChatTester.status.thinking'),
  closed: t('pages.agentChatTester.status.closed'),
  error: t('pages.agentChatTester.status.error'),
}))

// ACP busy/idle for the breathing indicator: busy while the agent processes a
// turn (thinking), connected-but-idle when the session is up and waiting.
const acpBusy = computed(() => status.value === 'thinking')
const acpConnected = computed(() => status.value === 'ready' || status.value === 'thinking')

const validRepos = computed(() => repos.value.filter((r) => r.name.trim() && r.url.trim()))
const isHomeProjectBound = computed(() => !!props.homeProjectId?.trim())
const canStart = computed(
  () => launchMode.value === 'empty' || validRepos.value.length > 0,
)

watch(launchMode, (mode) => {
  if (mode === 'clone' && repos.value.length === 0) {
    repos.value = [{ name: '', url: '', branch: '' }]
  }
})

// While the sandbox boots we drive stages from real status when known
// (pulling → create → runtime → ACP), falling back to a timed cycle.
const STARTING_STEPS = computed(() => [
  { title: t('pages.agentChatTester.stepPulling.title'), hint: t('pages.agentChatTester.stepPulling.hint') },
  { title: t('pages.agentChatTester.stepSandbox.title'), hint: t('pages.agentChatTester.stepSandbox.hint') },
  { title: t('pages.agentChatTester.stepRuntime.title'), hint: t('pages.agentChatTester.stepRuntime.hint') },
  { title: t('pages.agentChatTester.stepAcp.title'), hint: t('pages.agentChatTester.stepAcp.hint') },
  { title: t('pages.agentChatTester.stepReady.title'), hint: t('pages.agentChatTester.stepReady.hint') },
])
const startStep = ref(0)
let startTimer: number | undefined

watch(
  status,
  (s) => {
    if (s === 'starting') {
      startStep.value = sandbox.value?.status === 'pulling' ? 0 : 1
      if (startTimer) clearInterval(startTimer)
      if (sandbox.value?.status === 'pulling') {
        startTimer = undefined
        return
      }
      startTimer = window.setInterval(() => {
        // Advance but hold on the last step so it doesn't loop back to "准备容器".
        if (startStep.value < STARTING_STEPS.value.length - 1) startStep.value++
      }, 2600)
    } else if (startTimer) {
      clearInterval(startTimer)
      startTimer = undefined
    }
  },
)

async function start() {
  if (status.value === 'starting' || !canStart.value) return
  if (!props.createTest) {
    status.value = 'error'
    errorMsg.value = t('pages.agentChatTester.missingCreateTest')
    return
  }
  status.value = 'starting'
  errorMsg.value = ''
  turns.value = []
  try {
    const payload =
      launchMode.value === 'clone'
        ? {
            ...(props.homeProjectId?.trim() ? { projectId: props.homeProjectId.trim() } : {}),
            repos: validRepos.value.map((r) => ({
              name: r.name.trim(),
              url: r.url.trim(),
              ...(r.branch.trim() ? { branch: r.branch.trim() } : {}),
            })),
          }
        : {
            ...(props.homeProjectId?.trim() ? { projectId: props.homeProjectId.trim() } : {}),
          }
    sandbox.value = await props.createTest(props.profile, payload)
    await waitReady(sandbox.value.id)
  } catch (e: any) {
    status.value = 'error'
    errorMsg.value = String(e?.message || e)
  }
}

// Attach to an already-created sandbox (console ACP tab): skip the repo/create
// step and go straight to waiting for readiness + opening the chat WS.
async function attach(id: number) {
  status.value = 'starting'
  errorMsg.value = ''
  await waitReady(id)
}

onMounted(() => {
  if (props.attachId) void attach(props.attachId)
})

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

async function waitReady(id: number) {
  // Align with gateway FinalizeTimeout / create timeout (~20m) for cold pulls (g3.4).
  const deadline = Date.now() + 20 * 60 * 1000
  while (Date.now() < deadline) {
    // Bail if the user reset/destroyed the session while we were waiting.
    if (status.value !== 'starting') return
    let v: SandboxView
    try {
      v = await api.getSandbox(id)
    } catch {
      await sleep(2000)
      continue
    }
    sandbox.value = v
    if (v.status === 'pulling') {
      startStep.value = 0
      if (startTimer) {
        clearInterval(startTimer)
        startTimer = undefined
      }
      await sleep(2000)
      continue
    }
    if (v.status === 'creating' && startStep.value === 0) {
      startStep.value = 1
      if (!startTimer) {
        startTimer = window.setInterval(() => {
          if (startStep.value < STARTING_STEPS.value.length - 1) startStep.value++
        }, 2600)
      }
    }
    if (v.status === 'running') {
      openWs(id)
      return
    }
    if (v.status === 'error') {
      status.value = 'error'
      errorMsg.value = v.error || t('pages.agentChatTester.sandboxStartFailed')
      return
    }
    await sleep(2000)
  }
  status.value = 'error'
  errorMsg.value = t('pages.agentChatTester.sandboxTimeout')
}

function openWs(id: number) {
  closeWs()
  wsSandboxId = id
  const sock = new WebSocket(api.sandboxChatWsUrl(id))
  ws = sock
  heldFrames = []
  sock.onopen = () => {
    if (ws !== sock) return
    wsAttempt = 0
    status.value = 'ready'
    errorMsg.value = ''
    // Reused sandbox: rebuild the prior transcript so the context is visible,
    // like the sandbox console page. Live frames wait until it is in place.
    void restoreHistory(id).finally(() => {
      if (ws === sock) flushHeldFrames()
    })
  }
  sock.onmessage = (ev) => {
    if (ws !== sock) return
    if (heldFrames) heldFrames.push(ev.data)
    else onFrame(ev.data)
  }
  sock.onclose = () => {
    if (ws !== sock) return
    ws = null
    if (status.value !== 'error') status.value = 'closed'
    scheduleReconnect()
  }
  sock.onerror = () => {
    if (ws !== sock) return
    status.value = 'error'
    errorMsg.value = t('pages.agentChatTester.wsFailed')
  }
}

function closeWs() {
  if (wsReconnectTimer !== undefined) {
    clearTimeout(wsReconnectTimer)
    wsReconnectTimer = undefined
  }
  const sock = ws
  ws = null
  heldFrames = null
  sock?.close()
}

// The turn keeps running on the server while the socket is down; reconnecting
// replays it, so a dropped connection only pauses the view.
function scheduleReconnect() {
  if (disposed || !wsSandboxId || wsReconnectTimer !== undefined) return
  const delay = Math.min(15_000, 1000 * 2 ** wsAttempt)
  wsAttempt++
  wsReconnectTimer = window.setTimeout(() => {
    wsReconnectTimer = undefined
    if (!disposed && wsSandboxId && !ws) openWs(wsSandboxId)
  }, delay)
}

function flushHeldFrames() {
  const held = heldFrames ?? []
  heldFrames = null
  const head = parseFrame(held[0])
  const active = head?.event === 'queue_state' && head.busy ? head.activeItem : null
  if (active && activeIdx < 0) dropRestoredActiveTurn(String(active.text ?? ''))
  for (const raw of held) onFrame(raw)
}

// The event log already holds the running turn's prompt and partial reply; drop
// them so the replayed frames rebuild that turn once.
function dropRestoredActiveTurn(text: string) {
  for (let i = turns.value.length - 1; i >= 0; i--) {
    const turn = turns.value[i]
    if (turn?.role !== 'user') continue
    if (turn.text === text.trim()) turns.value = turns.value.slice(0, i)
    return
  }
}

// canSend allows firing even while a turn is running — extra messages are
// queued on the server (single worker drains them serially).
const canSend = () =>
  !!ws && ws.readyState === WebSocket.OPEN && (status.value === 'ready' || status.value === 'thinking')

function newItemId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

function send() {
  const text = input.value.trim()
  if ((!text && attachments.value.length === 0) || !canSend()) return
  const imgs = attachments.value.slice()
  const over = findOversizedAttachments(imgs)
  if (over.length) {
    errorMsg.value = formatSendRejectMessage(
      over.map((im, i) => attachmentDisplayName(im, i)),
      SITE_ATTACH_MAX_MIB,
    )
    return
  }
  // Enqueue only — the bubble is created on turn_begin (see onFrame), so extra
  // sends show up in the bottom queue panel instead of as premature bubbles.
  const item: QueueItem = { id: newItemId(), text, images: imgs }
  sentItems.set(item.id, item)
  queued.value.push(item)
  input.value = ''
  attachments.value = []
  status.value = 'thinking'
  ws!.send(
    JSON.stringify({
      type: 'chat',
      id: item.id,
      content: text,
      images: imgs.map((a) => ({
        data: a.data,
        mimeType: a.mimeType,
        ...(a.name ? { name: a.name } : {}),
      })),
    }),
  )
  scrollDown()
}

function cancel() {
  ws?.send(JSON.stringify({ type: 'cancel' }))
  // Drop everything not yet started; the running turn still gets its turn_done.
  queued.value = []
  refreshStatus()
}

function refreshStatus() {
  status.value = queued.value.length === 0 && activeIdx < 0 ? 'ready' : 'thinking'
}

function parseFrame(data: string | undefined): any {
  if (typeof data !== 'string') return null
  try {
    return JSON.parse(data)
  } catch {
    return null
  }
}

function serverItem(raw: any): QueueItem {
  const id = String(raw?.id ?? '')
  const local = id ? sentItems.get(id) : undefined
  if (local) return local
  const images: ImageAtt[] = Array.isArray(raw?.images)
    ? raw.images
        .filter((im: any) => im && typeof im.data === 'string')
        .map((im: any) => ({
          data: im.data,
          mimeType: String(im.mimeType || ''),
          url: im.mimeType ? `data:${im.mimeType};base64,${im.data}` : '',
          ...(im.name ? { name: String(im.name) } : {}),
        }))
    : []
  return { id, text: String(raw?.text ?? ''), images }
}

function applyQueueState(frame: any) {
  queued.value = Array.isArray(frame.items) ? frame.items.map(serverItem) : []
  if (!frame.busy && activeIdx >= 0) {
    // The turn ended while we were away.
    const turn = turns.value[activeIdx]
    if (turn) turn.streaming = false
    activeIdx = -1
    activeItemId = ''
  }
  if (frame.busy) status.value = 'thinking'
  else refreshStatus()
}

function beginTurn(frame: any) {
  const item = serverItem(frame.item)
  queued.value = queued.value.filter((q) => q.id !== item.id)
  if (item.id && item.id === activeItemId && activeIdx >= 0) {
    // Replay of the turn we were already showing: rebuild its reply.
    turns.value[activeIdx] = { role: 'agent', text: '', thought: '', tools: [], plan: [], streaming: true }
    status.value = 'thinking'
    return
  }
  if (activeIdx >= 0) {
    const prev = turns.value[activeIdx]
    if (prev) prev.streaming = false
  }
  turns.value.push({
    role: 'user',
    text: item.text,
    thought: '',
    tools: [],
    plan: [],
    streaming: false,
    images: item.images.length ? item.images : undefined,
  })
  activeIdx =
    turns.value.push({ role: 'agent', text: '', thought: '', tools: [], plan: [], streaming: true }) - 1
  activeItemId = item.id
  status.value = 'thinking'
}

function endTurn(error?: string) {
  const turn = activeIdx >= 0 ? turns.value[activeIdx] : null
  if (turn) {
    turn.streaming = false
    if (error !== undefined) turn.error = error
  } else if (error !== undefined) {
    errorMsg.value = error
  }
  if (activeItemId) sentItems.delete(activeItemId)
  activeIdx = -1
  activeItemId = ''
  refreshStatus()
}

function onFrame(data: string) {
  const frame = parseFrame(data)
  if (!frame || typeof frame !== 'object') return
  if (frame.type === 'acp') {
    const turn = activeIdx >= 0 ? turns.value[activeIdx] : null
    if (turn) applyAcp(frame.data, turn)
    scrollDown()
    return
  }
  if (frame.type !== 'session') return
  switch (frame.event) {
    case 'queue_state':
      applyQueueState(frame)
      break
    case 'turn_begin':
      beginTurn(frame)
      break
    case 'turn_done':
      endTurn()
      break
    case 'error':
      endTurn(frame.message || t('pages.agentChatTester.execError'))
      break
  }
  scrollDown()
}

// --- file attachments (any type; 50 MiB select/send gate) -----------------
function addFiles(files: ArrayLike<File> | null | undefined) {
  if (!files) return
  const { rejected } = readFilesAsAttachments(files, {
    maxBytes: SITE_ATTACH_MAX_BYTES,
    onRead: (att) => attachments.value.push(att),
  })
  if (rejected.length) {
    errorMsg.value = formatSelectRejectMessage(rejected, SITE_ATTACH_MAX_MIB)
  }
}
function onPickFiles(e: Event) {
  addFiles((e.target as HTMLInputElement).files)
  if (fileInput.value) fileInput.value.value = ''
}
function onPaste(e: ClipboardEvent) {
  const picked = filesFromClipboard(e)
  if (picked.length) {
    e.preventDefault()
    addFiles(picked)
  }
}
function removeAttachment(i: number) {
  attachments.value.splice(i, 1)
}

function rebuildTurnsFromFrames(events: any[]): Turn[] {
  const rebuilt: Turn[] = []
  let agent: Turn | null = null
  const newAgent = () => {
    agent = { role: 'agent', text: '', thought: '', tools: [], plan: [], streaming: false }
    rebuilt.push(agent)
  }
  for (const raw of events) {
    const ev = unwrapFrame(raw)
    if (!ev || typeof ev !== 'object') continue
    if (ev.type === 'prompt_begin') {
      const text = String(ev.promptText ?? ev.text ?? '').trim()
      const urls: string[] = Array.isArray(ev.imageURLs) ? ev.imageURLs.filter((u: any) => typeof u === 'string' && u) : []
      if (text || urls.length) {
        rebuilt.push({
          role: 'user',
          text,
          thought: '',
          tools: [],
          plan: [],
          streaming: false,
          images: urls.length
            ? urls.map((u) => ({ url: u, data: '', mimeType: inferImageMimeFromUrl(u) }))
            : undefined,
        })
        newAgent()
      }
    } else if (ev.type === 'session_update' && ev.update) {
      if (!agent) newAgent()
      applyAcp(ev, agent!)
    }
  }
  const last = rebuilt[rebuilt.length - 1]
  if (last && last.role === 'agent' && !last.text && !last.thought && !last.tools.length && !last.plan.length) {
    rebuilt.pop()
  }
  return rebuilt
}

// restoreHistory rebuilds the transcript from the sandbox's raw event log so a
// reopened (reused) sandbox shows its prior conversation. prompt_begin frames
// carry the original user prompt (+ image data URLs); session_update frames are
// folded into the following agent turn via the same applyAcp used live.
async function restoreHistory(id: number) {
  if (turns.value.length > 0) return
  restoring.value = true
  try {
    const r = await api.sandboxEventLog(id, { limit: 20 })
    if ('hasMore' in r) {
      const paged = r as { events: any[]; nextCursor: string; hasMore: boolean }
      rawFrames = paged.events || []
      historyHasMore.value = paged.hasMore
      historyCursor.value = paged.nextCursor || ''
    } else {
      rawFrames = r.events || []
      historyHasMore.value = false
      historyCursor.value = ''
    }
  } catch {
    return
  } finally {
    restoring.value = false
  }
  if (!rawFrames.length || turns.value.length > 0) return
  const rebuilt = rebuildTurnsFromFrames(rawFrames)
  if (rebuilt.length) {
    turns.value = rebuilt
    scrollDown()
  }
}

async function loadEarlierHistory() {
  if (!sandbox.value || !historyHasMore.value || loadingEarlier.value) return
  loadingEarlier.value = true
  try {
    const r = await api.sandboxEventLog(sandbox.value.id, {
      cursor: historyCursor.value,
      limit: 20,
    })
    if (!('hasMore' in r) || !r.events?.length) return
    const paged = r as { events: any[]; nextCursor: string; hasMore: boolean }
    rawFrames = [...paged.events, ...rawFrames]
    historyHasMore.value = paged.hasMore
    historyCursor.value = paged.nextCursor || ''
    turns.value = rebuildTurnsFromFrames(rawFrames)
  } catch {
    /* keep current transcript */
  } finally {
    loadingEarlier.value = false
  }
}

// applyAcp tolerantly parses a cursor-acp event frame ({op:"event",data:{type,update}}).
function applyAcp(envelope: any, turn: Turn) {
  const ev = envelope?.data ?? envelope
  if (!ev || ev.type !== 'session_update' || !ev.update) return
  const u = flattenUpdate(ev.update)
  const kind = normalizeKind(u.sessionUpdate || u.session_update || u.type || u.kind || '')
  if (kind === 'agent_message_chunk') {
    const s = contentText(u.content)
    turn.text += s
    addStep(turn, 'message', s)
  } else if (kind === 'agent_thought_chunk') {
    const s = contentText(u.content)
    turn.thought += s
    addStep(turn, 'thought', s)
  }
  else if (kind === 'plan') turn.plan = planEntries(u)
  else if (isToolKind(kind)) applyTool(u, turn)
}

function addStep(turn: Turn, kind: 'thought' | 'message', s: string) {
  if (!s) return
  const steps = (turn.steps ??= [])
  const last = steps[steps.length - 1]
  if (last && last.kind === kind) last.text += s
  else steps.push({ kind, text: s })
}

/** Turn steps as timeline parts; tool steps read the live tool row. */
function partsOf(turn: Turn): AgentPart[] {
  return (turn.steps || []).map((st) => {
    if (st.kind !== 'tool') return { kind: st.kind, text: st.text }
    const tool = turn.tools[st.tool]
    return tool ? { kind: 'tool', title: tool.title, status: tool.status, summary: tool.summary, input: tool.input, output: tool.output } : { kind: 'tool', title: '' }
  })
}

const SUMMARY_KEYS = ['command', 'cmd', 'file_path', 'filePath', 'path', 'target_file', 'url', 'uri', 'pattern', 'glob_pattern', 'query', 'search_term', 'name', 'description']
const TOOL_DETAIL_MAX = 2000
const SECRET_KEY_RE = /token|secret|password|passwd|api[_-]?key|authorization|cookie|credential/i

function toolSummary(input: any): string {
  if (!input || typeof input !== 'object') return ''
  for (const k of SUMMARY_KEYS) {
    const v = input[k]
    const s = typeof v === 'string' ? v : Array.isArray(v) ? v.filter((x) => typeof x === 'string').join(' ') : ''
    if (s.trim()) return s.replace(/\s+/g, ' ').trim().slice(0, 160)
  }
  return ''
}

function toolDetail(v: any, output: boolean): string {
  if (v === undefined || v === null) return ''
  let s = ''
  if (typeof v === 'string') s = v
  else if (output && typeof v === 'object' && !Array.isArray(v)) s = [v.stdout, v.stderr].filter((x) => typeof x === 'string' && x).join('\n') || (typeof v.output === 'string' ? v.output : '')
  if (!s && output && (Array.isArray(v) || v.content)) s = contentText(Array.isArray(v) ? v : v.content)
  if (!s) s = JSON.stringify(v, (k, x) => (k && SECRET_KEY_RE.test(k) && typeof x !== 'object' ? '****' : x), 2)
  s = s.trim()
  if (!s || s === '{}' || s === '[]') return ''
  return s.length > TOOL_DETAIL_MAX ? `${s.slice(0, TOOL_DETAIL_MAX)}\n…(truncated)` : s
}

function isToolKind(k: string): boolean {
  return k.includes('tool_call') || k.includes('toolcall')
}
function field(o: any, ...keys: string[]): string {
  for (const k of keys) if (typeof o[k] === 'string' && o[k]) return o[k]
  return ''
}
function planEntries(u: any): { content: string; status: string }[] {
  const raw = u.entries || u.steps || []
  if (!Array.isArray(raw)) return []
  return raw
    .map((e: any) => ({ content: field(e, 'content', 'title', 'text'), status: field(e, 'status', 'state') || 'pending' }))
    .filter((e: any) => e.content)
}
// Common tool names → localized labels (rest gets Title-Cased).
function humanizeTool(s: string): string {
  const raw = String(s || '').trim()
  if (!raw || /^tool_[0-9a-f-]{8,}$/i.test(raw)) return ''
  if (/[\u4e00-\u9fff]/.test(raw)) return raw
  const key = raw.toLowerCase().replace(/\s+/g, '_').replace(/-/g, '_')
  const i18nKey = `pages.agentChatTester.tools.${key}`
  if (te(i18nKey)) return t(i18nKey)
  return raw
    .replace(/_/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

function applyTool(u: any, turn: Turn) {
  // Some agents nest the tool details under toolCall/tool/call instead of the
  // top level — check both so we don't miss the title/kind.
  const tc = (u.toolCall || u.tool || u.call || {}) as any
  const id = field(u, 'toolCallId', 'tool_call_id', 'id', 'callId') || field(tc, 'toolCallId', 'id', 'callId')
  // Prefer an explicit title/name; fall back to the ACP tool "kind" (read/edit/…)
  // so a call still reads as e.g. "Read" instead of the ugly id.
  const title = humanizeTool(
    field(u, 'title', 'name', 'toolName', 'tool_name', 'kind') || field(tc, 'title', 'name', 'toolName', 'kind'),
  )
  const stat = field(u, 'status', 'state') || field(tc, 'status', 'state')
  const rawIn = u.rawInput ?? u.raw_input ?? u.input ?? tc.rawInput ?? tc.input
  const rawOut = u.rawOutput ?? u.raw_output ?? u.output ?? tc.rawOutput ?? tc.output
  const existing = turn.tools.find((t) => id && t.id === id)
  const tool = existing ?? { id, title: title || t('pages.agentChatTester.toolFallback'), status: stat || 'pending' }
  if (existing) {
    if (title) existing.title = title
    if (stat) existing.status = stat
  } else {
    turn.tools.push(tool)
    ;(turn.steps ??= []).push({ kind: 'tool', tool: turn.tools.length - 1 })
  }
  if (rawIn !== undefined) {
    tool.summary = toolSummary(rawIn) || undefined
    tool.input = toolDetail(rawIn, false) || undefined
  }
  if (rawOut !== undefined) tool.output = toolDetail(rawOut, true) || undefined
}

function scrollDown() {
  nextTick(() => {
    if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
  })
}

function reset() {
  closeWs()
  wsSandboxId = 0
  wsAttempt = 0
  sentItems.clear()
  activeItemId = ''
  sandbox.value = null
  turns.value = []
  attachments.value = []
  queued.value = []
  restoring.value = false
  activeIdx = -1
  status.value = 'idle'
}

// Two-step destroy: clicking "结束并销毁" opens a confirm modal first.
const confirmDestroy = ref(false)
async function destroy() {
  confirmDestroy.value = false
  const id = sandbox.value?.id
  reset()
  if (id) {
    try {
      await api.destroySandbox(id)
    } catch {
      /* sweeper will reclaim */
    }
  }
}

// Tearing down the WS on profile switch / unmount keeps the sandbox alive
// (the TTL sweeper reclaims it; it's also visible in the 沙箱 page).
watch(
  () => props.profile,
  () => reset(),
)
onBeforeUnmount(() => {
  disposed = true
  closeWs()
  if (startTimer) clearInterval(startTimer)
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <!-- header / controls -->
    <div v-if="!embedded" class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4">
      <AcpStatusPill v-if="acpConnected" :busy="acpBusy" connected />
      <span
        v-else
        class="chip"
        :class="status === 'error' ? 'border-err/30 text-err' : 'border-line text-txt3'"
      >{{ statusLabel[status] }}</span>
      <span v-if="sandbox" class="text-[11px] font-mono text-txt3">{{ sandbox.name }}</span>
      <div class="ml-auto flex items-center gap-2">
        <RouterLink
          v-if="sandbox"
          :to="`/sandboxes/${sandbox.id}/console`"
          class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong"
        ><Icon name="terminal" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('pages.agentChatTester.console') }}</RouterLink>
        <AppButton v-if="status === 'idle' || status === 'closed' || status === 'error'" size="sm" variant="primary" icon="play" :disabled="!canStart" @click="start">{{ t('pages.agentChatTester.startTest') }}</AppButton>
        <AppButton v-else size="sm" variant="danger" icon="trash" @click="confirmDestroy = true">{{ t('pages.agentChatTester.destroy') }}</AppButton>
      </div>
    </div>

    <!-- embedded (sandbox console ACP tab): the host hides our header, so surface
         a slim busy/idle strip here too -->
    <div v-if="embedded && acpConnected" class="flex items-center gap-2 border-b border-line px-3 py-1.5">
      <AcpStatusPill :busy="acpBusy" connected />
      <span class="text-[11px] font-medium text-txt3">{{ t('pages.agentChatTester.acpSession') }}</span>
    </div>

    <!-- idle: mode cards + repos editor -->
    <div v-if="status === 'idle'" class="flex flex-1 flex-col items-center justify-center gap-5 p-6">
      <div
        v-if="!isHomeProjectBound"
        class="flex w-full max-w-lg items-start gap-2.5 rounded-md border border-warn/40 bg-warn/10 px-3 py-2.5 text-left"
        role="alert"
      >
        <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-warn" />
        <div>
          <p class="text-[12.5px] font-semibold text-warn">{{ t('pages.agentChatTester.projectRequired.title') }}</p>
          <p class="mt-0.5 text-[12px] leading-relaxed text-txt2">{{ t('pages.agentChatTester.projectRequired.desc') }}</p>
        </div>
      </div>

      <div class="max-w-lg text-center">
        <Icon name="robot" :size="32" class="mx-auto text-txt3" />
        <p class="mt-3 text-[13px] text-txt3">
          {{ t('pages.agentChatTester.idleHint', { profile }) }}
        </p>
      </div>

      <div class="w-full max-w-lg">
        <div class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-txt3">
          {{ t('pages.agentChatTester.mode.sectionLabel') }}
        </div>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2" role="radiogroup" :aria-label="t('pages.agentChatTester.mode.sectionLabel')">
          <button
            type="button"
            role="radio"
            :aria-checked="launchMode === 'empty'"
            class="relative rounded-md border p-4 text-left transition"
            :class="launchMode === 'empty' ? 'border-accent bg-accent/10 shadow-[0_0_0_1px_rgba(123,97,255,0.3)]' : 'border-line bg-surface hover:border-line-strong'"
            @click="launchMode = 'empty'"
          >
            <span
              class="absolute right-3 top-3 flex h-4 w-4 items-center justify-center rounded-full border"
              :class="launchMode === 'empty' ? 'border-accent bg-accent' : 'border-line-strong'"
            >
              <span v-if="launchMode === 'empty'" class="h-1.5 w-1.5 rounded-full bg-white" />
            </span>
            <div class="flex items-start gap-2.5 pr-6">
              <span class="rounded-full flex h-8 w-8 shrink-0 items-center justify-center border border-line bg-elevated text-txt2" :class="launchMode === 'empty' ? 'border-accent/35 bg-accent/15 text-accent-2' : ''">
                <Icon name="folder" :size="16" />
              </span>
              <div>
                <div class="text-[13px] font-semibold text-txt">{{ t('pages.agentChatTester.mode.empty.title') }}</div>
                <div class="mt-0.5 text-[11.5px] leading-relaxed text-txt3">{{ t('pages.agentChatTester.mode.empty.desc') }}</div>
              </div>
            </div>
          </button>
          <button
            type="button"
            role="radio"
            :aria-checked="launchMode === 'clone'"
            class="relative rounded-md border p-4 text-left transition"
            :class="launchMode === 'clone' ? 'border-accent bg-accent/10 shadow-[0_0_0_1px_rgba(123,97,255,0.3)]' : 'border-line bg-surface hover:border-line-strong'"
            @click="launchMode = 'clone'"
          >
            <span
              class="absolute right-3 top-3 flex h-4 w-4 items-center justify-center rounded-full border"
              :class="launchMode === 'clone' ? 'border-accent bg-accent' : 'border-line-strong'"
            >
              <span v-if="launchMode === 'clone'" class="h-1.5 w-1.5 rounded-full bg-white" />
            </span>
            <div class="flex items-start gap-2.5 pr-6">
              <span class="rounded-full flex h-8 w-8 shrink-0 items-center justify-center border border-line bg-elevated text-txt2" :class="launchMode === 'clone' ? 'border-accent/35 bg-accent/15 text-accent-2' : ''">
                <Icon name="git" :size="16" />
              </span>
              <div>
                <div class="text-[13px] font-semibold text-txt">{{ t('pages.agentChatTester.mode.clone.title') }}</div>
                <div class="mt-0.5 text-[11.5px] leading-relaxed text-txt3">{{ t('pages.agentChatTester.mode.clone.desc') }}</div>
              </div>
            </div>
          </button>
        </div>
      </div>

      <div v-if="launchMode === 'clone'" class="w-full max-w-lg space-y-2">
        <div class="text-[12px] font-medium text-txt2">{{ t('pages.agentChatTester.repos.sectionLabel') }}</div>
        <p class="text-[11.5px] text-txt3">{{ t('pages.agentChatTester.repos.hint') }}</p>
        <ReposEditor v-model:repos="repos" :min-rows="1" i18n-prefix="pages.agentChatTester.repos" />
      </div>

      <AppButton variant="primary" icon="play" :disabled="!canStart" @click="start">{{ t('pages.agentChatTester.startTest') }}</AppButton>
      <p v-if="errorMsg" class="text-[12px] text-err">{{ errorMsg }}</p>
    </div>

    <!-- starting: full-panel loader so it never looks frozen -->
    <div v-else-if="status === 'starting'" class="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
      <Icon name="spinner" :size="30" class="animate-spin text-accent" />
      <Transition name="startfade" mode="out-in">
        <p :key="startStep" class="text-[13px] text-txt2">{{ STARTING_STEPS[startStep].title }}</p>
      </Transition>
      <Transition name="startfade" mode="out-in">
        <p :key="startStep" class="max-w-md text-[12px] text-txt3">{{ STARTING_STEPS[startStep].hint }}</p>
      </Transition>
      <div class="mt-1 flex items-center gap-1.5">
        <span
          v-for="(_, i) in STARTING_STEPS"
          :key="i"
          class="h-1.5 rounded-full transition-all duration-300"
          :class="i === startStep ? 'w-5 bg-accent' : i < startStep ? 'w-1.5 bg-accent/50' : 'w-1.5 bg-line'"
        />
      </div>
      <p v-if="errorMsg" class="text-[12px] text-err">{{ errorMsg }}</p>
    </div>

    <!-- conversation -->
    <div v-else ref="scroller" class="scroll-area min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
      <div v-if="historyHasMore" class="text-center">
        <button
          type="button"
          class="text-xs text-accent-2 transition hover:text-accent disabled:opacity-40"
          :disabled="loadingEarlier"
          @click="loadEarlierHistory"
        >
          {{ t('common.pagination.loadEarlier') }}
        </button>
      </div>
      <div v-if="errorMsg" class="rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">{{ errorMsg }}</div>
      <div v-if="restoring" class="flex items-center justify-center gap-2 rounded-md border border-line bg-base/60 px-3 py-2 text-[12px] text-txt3">
        <Icon name="spinner" :size="13" class="animate-spin text-accent-2" />{{ t('pages.agentChatTester.loadingHistory') }}
      </div>

      <div v-for="(turn, i) in turns" :key="i" class="flex" :class="turn.role === 'user' ? 'justify-end' : 'justify-start'">
        <div v-if="turn.role === 'user'" class="max-w-[80%] space-y-1.5">
          <div v-if="turn.images && turn.images.length" class="flex flex-wrap justify-end gap-1.5">
            <template v-for="(im, ii) in turn.images" :key="ii">
              <ChatImageThumb
                v-if="isImageAttachment(im)"
                mode="previewable"
                size="md"
                thumb-class="rounded-md"
                :src="chatImageSrc(im)"
                :label="attachmentDisplayName(im, ii)"
                :alt="attachmentDisplayName(im, ii)"
                test-id="tester-history-image-thumb"
                @preview="openChatImagePreview(chatImageSrc(im), attachmentDisplayName(im, ii))"
              />
              <div
                v-else
                class="rounded-md flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                data-testid="tester-history-file-chip"
                :title="attachmentDisplayName(im, ii)"
              >
                <span class="shrink-0 text-[10px] font-medium uppercase tracking-wide text-info">DOC</span>
                <span class="min-w-0 truncate text-[12px] text-txt">{{ attachmentDisplayName(im, ii) }}</span>
              </div>
            </template>
          </div>
          <div v-if="turn.text" class="rounded-lg rounded-br-sm bg-accent px-3 py-2 text-[13px] leading-6 text-white">
            {{ turn.text }}
          </div>
        </div>
        <div v-else class="max-w-[88%] space-y-2">
          <AgentTimeline
            v-if="turn.steps?.length"
            :parts="partsOf(turn)"
            :streaming="turn.streaming"
            :completed="!turn.streaming && !turn.error"
            :interrupted="!turn.streaming && !!turn.error"
            message-test-id="tester-agent-message"
          />
          <!-- thought -->
          <details v-if="turn.thought && !turn.steps?.length" class="rounded-md border border-line bg-base/60 text-[11.5px] text-txt3">
            <summary class="cursor-pointer select-none px-2.5 py-1.5 text-txt3 hover:text-txt2"><Icon name="sparkles" :size="11" class="-mt-0.5 mr-1 inline text-accent-2" />{{ t('pages.agentChatTester.thought') }}</summary>
            <div class="whitespace-pre-wrap px-2.5 pb-2 font-mono leading-5">{{ turn.thought }}</div>
          </details>
          <!-- plan -->
          <div v-if="turn.plan.length" class="rounded-md border border-line bg-base/60 p-2 text-[11.5px]">
            <div class="mb-1 text-txt3">{{ t('pages.agentChatTester.plan') }}</div>
            <div v-for="(p, pi) in turn.plan" :key="pi" class="flex items-center gap-1.5 text-txt2">
              <Icon :name="p.status === 'completed' ? 'check' : 'clock'" :size="11" :class="p.status === 'completed' ? 'text-ok' : 'text-txt3'" />
              <span :class="p.status === 'completed' ? 'line-through text-txt3' : ''">{{ p.content }}</span>
            </div>
          </div>
          <AgentToolGroup v-if="turn.tools.length && !turn.steps?.length" :tools="turn.tools" :busy="turn.streaming" />
          <!-- narration -->
          <div v-if="turn.text && !turn.steps?.length" class="md rounded-lg rounded-bl-sm border border-line bg-surface px-3 py-2 text-[13px] leading-6 text-txt">
            <StreamMarkdown v-if="turn.streaming" :blocks="renderMarkdownBlocks(turn.text, blockCache)" />
            <span v-else v-html="renderMarkdown(turn.text)" />
          </div>
          <div v-else-if="turn.streaming && !turn.thought && !turn.tools.length && !turn.text" class="rounded-lg border border-line bg-surface px-3 py-2 text-[13px] text-txt3">
            <Icon name="spinner" :size="13" class="mr-1 inline animate-spin" />{{ t('pages.agentChatTester.generating') }}
          </div>
          <div v-if="turn.error" class="rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">{{ turn.error }}</div>
        </div>
      </div>
    </div>

    <!-- pending-send queue panel (mirrors the sandbox UI: queued items show
         here at the bottom instead of as premature transcript bubbles) -->
    <div v-if="status !== 'idle' && queued.length" class="border-t border-line bg-base/40 px-3 py-2">
      <div class="mb-1 flex items-center gap-1.5 text-[11px] text-txt3">
        <Icon name="clock" :size="11" />{{ t('pages.agentChatTester.queue', { n: queued.length }) }}
      </div>
      <div class="space-y-1">
        <div
          v-for="(q, qi) in queued"
          :key="q.id || qi"
          class="flex items-center gap-2 rounded border border-line bg-surface px-2 py-1 text-[12px] text-txt2"
        >
          <span class="flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-line text-[9px] text-txt3">{{ qi + 1 }}</span>
          <span v-if="q.images.length" class="shrink-0 text-[10.5px] text-txt3">{{ t('pages.agentChatTester.imagesCount', { n: q.images.length }) }}</span>
          <span class="truncate">{{ q.text || (q.images.length ? t('pages.agentChatTester.imagesOnly') : t('pages.agentChatTester.empty')) }}</span>
        </div>
      </div>
    </div>

    <!-- composer -->
    <div v-if="status !== 'idle'" class="border-t border-line p-3">
      <!-- pending attachments -->
      <div v-if="attachments.length" class="mb-2 flex flex-wrap gap-1.5">
        <div v-for="(im, ii) in attachments" :key="ii" class="relative">
          <ChatImageThumb
            v-if="isImageAttachment(im)"
            mode="previewable"
            size="sm"
            thumb-class="rounded-md"
            :src="chatImageSrc(im)"
            :label="attachmentDisplayName(im, ii)"
            :alt="attachmentDisplayName(im, ii)"
            test-id="tester-draft-image-thumb"
            @preview="openChatImagePreview(chatImageSrc(im), attachmentDisplayName(im, ii))"
          />
          <div
            v-else
            class="rounded-lg flex h-14 max-w-[160px] items-center gap-1.5 border border-line bg-elevated px-2"
            data-testid="tester-pending-file-chip"
            :title="attachmentDisplayName(im, ii)"
          >
            <span class="shrink-0 text-[9px] font-medium uppercase text-info">DOC</span>
            <span class="min-w-0 truncate text-[11px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
          </div>
          <button
            class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-err text-white"
            @click.stop="removeAttachment(ii)"
          ><Icon name="close" :size="9" /></button>
        </div>
      </div>
      <div class="flex items-end gap-2">
        <input ref="fileInput" type="file" multiple class="hidden" @change="onPickFiles" />
        <AppButton
          size="sm"
          variant="outline"
          icon="paperclip"
          :disabled="status !== 'ready' && status !== 'thinking'"
          @click="fileInput?.click()"
        >{{ t('pages.agentChatTester.images') }}</AppButton>
        <textarea
          v-model="input"
          rows="2"
          :disabled="status !== 'ready' && status !== 'thinking'"
          :placeholder="t('pages.agentChatTester.inputPlaceholder')"
          class="scroll-area max-h-32 min-h-[40px] flex-1 resize-none rounded-md border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent disabled:opacity-50"
          @keydown.enter.exact.prevent="send"
          @paste="onPaste"
        />
        <AppButton v-if="status === 'thinking'" size="sm" variant="outline" icon="close" @click="cancel">{{ t('pages.agentChatTester.cancel') }}</AppButton>
        <AppButton
          size="sm"
          variant="primary"
          icon="send"
          :disabled="(!input.trim() && !attachments.length) || (status !== 'ready' && status !== 'thinking')"
          @click="send"
        >{{ t('pages.agentChatTester.send') }}</AppButton>
      </div>
    </div>

    <ChatImagePreviewModal
      :open="!!imagePreview"
      :src="imagePreview?.src || ''"
      :label="imagePreview?.label || ''"
      test-id-prefix="tester-image-preview"
      @close="closeChatImagePreview"
    />

    <AppModal :open="confirmDestroy" :title="t('pages.agentChatTester.destroyTitle')" :width="420" @close="confirmDestroy = false">
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.agentChatTester.destroyWarning') }}
        </div>
        <p>{{ t('pages.agentChatTester.destroyConfirm') }}</p>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="confirmDestroy = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" @click="destroy">{{ t('common.buttons.confirmDestroy') }}</AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.startfade-enter-active,
.startfade-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.startfade-enter-from {
  opacity: 0;
  transform: translateY(4px);
}
.startfade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
