import { reactive, type InjectionKey } from 'vue'
import type { LiveMark, LivePoint } from '../../liveoverlay/annotations'

/**
 * Live variants: the preview page asks the parked preview-capable agent for N
 * variants of a picked element; the agent writes them into source and HMR
 * shows them. These helpers are the drawer side of that protocol. Message
 * names must match preview-pick.js / the live overlay bundle.
 */

/** Page → drawer: a Live request (`op`) or a view update (`op: 'state'`). */
export const EMBED_LIVE_MESSAGE = 'grasp-embed:live'
/** Drawer → page: `{enabled}` once the drawer knows whether Live is on. */
export const EMBED_LIVE_CAPS_MESSAGE = 'grasp-embed:live-caps'
/** Drawer → page: current sessions (after ready, and on every change). */
export const EMBED_LIVE_SESSIONS_MESSAGE = 'grasp-embed:live-sessions'
/** Drawer → page: answer to one Live request `{reqId, ok, error?, session?}`. */
export const EMBED_LIVE_ACK_MESSAGE = 'grasp-embed:live-ack'
/** Drawer → page: a command from the chat card (goto / compare / inplace / accept / discard). */
export const EMBED_LIVE_CMD_MESSAGE = 'grasp-embed:live-cmd'

export type LiveState =
  | 'generating'
  | 'ready'
  | 'refining'
  | 'accepting'
  | 'discarding'
  | 'accepted'
  | 'discarded'
  | 'failed'
  | 'done'

export type LiveOp = 'generate' | 'insert' | 'steer' | 'refine' | 'accept' | 'discard' | 'mount_failed'

export const LIVE_OPS: readonly LiveOp[] = ['generate', 'insert', 'steer', 'refine', 'accept', 'discard', 'mount_failed']

export const LIVE_ACTIONS = ['bolder', 'quieter', 'polish', 'typeset', 'colorize', 'layout', 'distill', 'adapt', 'animate', 'delight', 'overdrive', 'freeform'] as const

export type LiveVariant = { n: number; label?: string }

export type LiveSession = {
  sid: string
  mode: 'replace' | 'insert' | 'steer' | string
  action?: string
  prompt?: string
  count?: number
  selector?: string
  summary?: string
  url?: string
  state: LiveState | string
  file?: string
  variants?: LiveVariant[]
  selected?: number
  retryAccept?: boolean
  mountAutoReported?: boolean
  error?: string
  createdAt?: string
  updatedAt?: string
}

export type LiveRef = { sid: string; op: LiveOp | string; variant?: number; prompt?: string; generated?: boolean }

export type LiveElement = {
  selector: string
  tagName?: string
  id?: string
  classes?: string[]
  text?: string
  outerHTML?: string
  styles?: Record<string, string>
}

/** One Live request as the server takes it (POST reply `live`). */
export type LiveEvent = {
  op: LiveOp
  sid: string
  scope?: 'page'
  action?: string
  prompt?: string
  count?: number
  element?: LiveElement
  url?: string
  position?: 'before' | 'after'
  variant?: number
  params?: Record<string, unknown>
  notes?: string[]
  marks?: LiveMark[]
  error?: string
  /** mount_failed detected by the page rather than sent by the person. */
  auto?: boolean
}

export type LiveCtx = { sid: string; current: number; params?: Record<string, unknown> }

export type LiveView = { current: number; mode: 'inplace' | 'compare'; params?: Record<string, unknown> }

export type LiveCmd = 'goto' | 'compare' | 'inplace' | 'accept' | 'discard' | 'retry' | 'retry-accept'

/** A page message: either a request to forward (`event`) or a view update. */
export type EmbedLiveMessage =
  | { kind: 'request'; reqId: string; event: LiveEvent }
  | { kind: 'state'; sid: string; view: LiveView }

const OPEN_STATES = new Set(['generating', 'ready', 'refining', 'accepting', 'discarding', 'failed'])

export function isLiveOpen(state: string | undefined): boolean {
  return !!state && OPEN_STATES.has(state)
}

/** True while the agent is working on the session (no page actions). */
export function isLiveBusy(state: string | undefined): boolean {
  return state === 'generating' || state === 'refining' || state === 'accepting' || state === 'discarding'
}

function str(v: unknown, max = 4000): string | undefined {
  return typeof v === 'string' ? v.slice(0, max) : undefined
}

function int(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isInteger(v) ? v : undefined
}

function parseParamValues(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const out: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(value).slice(0, 6)) {
    if (!/^[a-z][a-z0-9-]{0,31}$/.test(key)) continue
    if (typeof val === 'string') out[key] = val.slice(0, 120)
    else if ((typeof val === 'number' && Number.isFinite(val)) || typeof val === 'boolean') out[key] = val
  }
  return Object.keys(out).length ? out : undefined
}

function parseMarks(value: unknown): LiveMark[] | null {
  if (!Array.isArray(value) || value.length > 8) return null
  const marks: LiveMark[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') return null
    const m = item as Record<string, unknown>
    if (m.kind !== 'draw' && m.kind !== 'note') return null
    if (!Array.isArray(m.points) || m.points.length > 80 || m.points.length < (m.kind === 'draw' ? 2 : 1) || (m.kind === 'note' && m.points.length !== 1)) return null
    const points: LivePoint[] = []
    for (const point of m.points) {
      if (!point || typeof point !== 'object') return null
      const p = point as Record<string, unknown>
      if (typeof p.x !== 'number' || typeof p.y !== 'number' || !Number.isFinite(p.x) || !Number.isFinite(p.y) || p.x < 0 || p.x > 1 || p.y < 0 || p.y > 1) return null
      points.push({ x: p.x, y: p.y })
    }
    const mark: LiveMark = { kind: m.kind, points }
    const text = str(m.text, 300)
    if (text) mark.text = text
    if (m.targets !== undefined) {
      if (!Array.isArray(m.targets) || m.targets.length > 4) return null
      mark.targets = []
      for (const entry of m.targets) {
        if (!entry || typeof entry !== 'object') return null
        const target = entry as Record<string, unknown>
        const selector = str(target.selector, 1024)
        if (!selector) return null
        mark.targets.push({ selector, ...(str(target.text, 120) ? { text: str(target.text, 120) } : {}) })
      }
    }
    marks.push(mark)
  }
  return marks
}

const SID_RE = /^[A-Za-z0-9_-]{6,64}$/

export function validLiveSid(sid: unknown): sid is string {
  return typeof sid === 'string' && SID_RE.test(sid)
}

function parseElement(v: unknown): LiveElement | undefined {
  if (!v || typeof v !== 'object') return undefined
  const e = v as Record<string, unknown>
  const selector = str(e.selector, 1024)
  if (!selector?.trim()) return undefined
  const out: LiveElement = { selector }
  const tag = str(e.tagName, 32)
  if (tag) out.tagName = tag
  const id = str(e.id, 128)
  if (id) out.id = id
  if (Array.isArray(e.classes)) out.classes = e.classes.filter((c): c is string => typeof c === 'string').slice(0, 12)
  const text = str(e.text, 400)
  if (text) out.text = text
  const html = str(e.outerHTML, 4096)
  if (html) out.outerHTML = html
  if (e.styles && typeof e.styles === 'object' && !Array.isArray(e.styles)) {
    const styles: Record<string, string> = {}
    for (const [k, val] of Object.entries(e.styles as Record<string, unknown>).slice(0, 16)) {
      if (typeof val === 'string') styles[k] = val.slice(0, 120)
    }
    out.styles = styles
  }
  return out
}

/** Validate a Live message from the preview page. */
export function parseEmbedLiveMessage(data: unknown): EmbedLiveMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  if (m.type !== EMBED_LIVE_MESSAGE || !validLiveSid(m.sid)) return null
  if (m.op === 'state') {
    const current = int(m.current) ?? 0
    const view: LiveView = { current, mode: m.mode === 'compare' ? 'compare' : 'inplace' }
    const params = parseParamValues(m.params)
    if (params) view.params = params
    return { kind: 'state', sid: m.sid, view }
  }
  if (typeof m.op !== 'string' || !(LIVE_OPS as readonly string[]).includes(m.op)) return null
  const reqId = str(m.reqId, 64) || ''
  const ev: LiveEvent = { op: m.op as LiveOp, sid: m.sid }
  if (m.scope !== undefined) {
    if (m.scope !== 'page' || ev.op !== 'generate') return null
    ev.scope = 'page'
  }
  const action = str(m.action, 32)
  if (action) ev.action = action
  const prompt = str(m.prompt, 2000)
  if (prompt) ev.prompt = prompt
  const count = int(m.count)
  if (count !== undefined) ev.count = count
  const variant = int(m.variant)
  if (variant !== undefined) ev.variant = variant
  const url = str(m.url, 2048)
  if (url) ev.url = url
  if (m.position === 'before' || m.position === 'after') ev.position = m.position
  const el = parseElement(m.element)
  if (el) ev.element = el
  if (m.params && typeof m.params === 'object' && !Array.isArray(m.params)) ev.params = m.params as Record<string, unknown>
  if (Array.isArray(m.notes)) ev.notes = m.notes.filter((n): n is string => typeof n === 'string').slice(0, 10)
  if (m.marks !== undefined) {
    const marks = parseMarks(m.marks)
    if (!marks) return null
    ev.marks = marks
  }
  const error = str(m.error, 2000)
  if (error) ev.error = error
  if (m.auto === true && ev.op === 'mount_failed') ev.auto = true
  return { kind: 'request', reqId, event: ev }
}

/** Validate a session object from the server (frame / list / reply). */
export function parseLiveSession(v: unknown): LiveSession | null {
  if (!v || typeof v !== 'object') return null
  const s = v as Record<string, unknown>
  if (!validLiveSid(s.sid) || typeof s.state !== 'string') return null
  const variants = Array.isArray(s.variants)
    ? (s.variants as unknown[])
        .map((x) => (x && typeof x === 'object' ? (x as Record<string, unknown>) : null))
        .filter((x): x is Record<string, unknown> => !!x && typeof x.n === 'number')
        .map((x) => ({ n: x.n as number, label: str(x.label, 32) }))
    : undefined
  return {
    sid: s.sid,
    mode: str(s.mode, 16) || 'replace',
    action: str(s.action, 32),
    prompt: str(s.prompt, 2000),
    count: int(s.count),
    selector: str(s.selector, 1024),
    summary: str(s.summary, 200),
    url: str(s.url, 2048),
    state: s.state,
    file: str(s.file, 512),
    variants,
    selected: int(s.selected),
    retryAccept: s.retryAccept === true,
    mountAutoReported: s.mountAutoReported === true,
    error: str(s.error, 2000),
    createdAt: str(s.createdAt, 64),
    updatedAt: str(s.updatedAt, 64),
  }
}

export function parseLiveRef(v: unknown): LiveRef | undefined {
  if (!v || typeof v !== 'object') return undefined
  const r = v as Record<string, unknown>
  if (!validLiveSid(r.sid) || typeof r.op !== 'string') return undefined
  const out: LiveRef = { sid: r.sid, op: r.op }
  const variant = int(r.variant)
  if (variant) out.variant = variant
  const prompt = str(r.prompt, 200)
  if (prompt) out.prompt = prompt
  if (r.generated === true) out.generated = true
  return out
}

export type LiveStore = {
  sessions: Record<string, LiveSession>
  views: Record<string, LiveView>
  enabled: boolean
}

/** Reactive store of Live sessions and the page's view of each. */
export function createLiveStore() {
  const store = reactive<LiveStore>({ sessions: {}, views: {}, enabled: false })
  return {
    store,
    setEnabled(on: boolean) {
      store.enabled = on
    },
    apply(raw: unknown): LiveSession | null {
      const s = parseLiveSession(raw)
      if (!s) return null
      const prev = store.sessions[s.sid]
      if (prev?.updatedAt && s.updatedAt && prev.updatedAt > s.updatedAt) return prev
      store.sessions[s.sid] = s
      if (!isLiveOpen(s.state)) delete store.views[s.sid]
      return s
    },
    replaceAll(list: unknown): LiveSession[] {
      const next: Record<string, LiveSession> = {}
      if (Array.isArray(list)) {
        for (const raw of list) {
          const s = parseLiveSession(raw)
          if (s) next[s.sid] = s
        }
      }
      store.sessions = next
      for (const sid of Object.keys(store.views)) if (!isLiveOpen(next[sid]?.state)) delete store.views[sid]
      return Object.values(next)
    },
    setView(sid: string, view: LiveView) {
      store.views[sid] = view
    },
    /** The open session the person is looking at, for liveCtx on a message. */
    activeCtx(): LiveCtx | null {
      for (const s of Object.values(store.sessions)) {
        if (s.mode === 'steer' || !isLiveOpen(s.state)) continue
        const v = store.views[s.sid]
        if (v && v.current > 0) return { sid: s.sid, current: v.current, ...(v.params ? { params: { ...v.params } } : {}) }
      }
      return null
    },
    list(): LiveSession[] {
      return Object.values(store.sessions)
    },
  }
}

export type LiveStoreApi = ReturnType<typeof createLiveStore>

/** What a host (the preview drawer) provides so Live cards can show state and act. */
export type LiveCardHost = {
  store: LiveStore
  /** Card buttons drive the preview page (drawer only). */
  interactive: boolean
  command: (sid: string, cmd: LiveCmd, variant?: number) => void
}

export const LIVE_CARD_HOST: InjectionKey<LiveCardHost> = Symbol('live-card-host')
