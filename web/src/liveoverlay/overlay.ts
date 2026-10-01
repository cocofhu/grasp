import { createAnnotations } from './annotations'
import { describeElement, type LiveElement } from './describe'
import { fmt, strings, type Strings } from './i18n'
import { applyParam, paramDefault, scanWrappers, setVariantVisible, showVariant, type LiveParam, type Wrapper } from './scan'
import { OVERLAY_CSS, PAGE_CSS } from './styles'
import { dropView, getView, putView, type Mode, type SessionView } from './viewStore'

/** What preview-pick.js hands the overlay. */
export type HostOpts = {
  /** Post to the chat drawer; false when the drawer is not ready. */
  post: (msg: Record<string, unknown>) => boolean
  theme: () => string
  notice: (text: string, ok?: boolean) => void
  /** Turn off preview-pick's own element picking. */
  stopPick: () => void
  /** Turn preview-pick's element picking back on (the pick bar's "Select" segment). */
  startPick?: () => void
  /** Hand a picked element to the chat drawer as a plain pick. */
  sendToChat: (el: Element) => void
  /** Re-render preview-pick's bar (pick / eye state). */
  changed: () => void
  isOwnUi: (el: Element) => boolean
  /** Grasp UI language (`zh-CN` | `en`); falls back to the browser language. */
  lang?: string
}

export type LiveOverlay = {
  onDrawer: (msg: unknown) => void
  /** Show the action card for an element picked with preview-pick's Pick. */
  offer: (el: Element) => void
  startInsert: () => void
  cancelPick: () => void
  isInserting: () => boolean
  /** Show or hide the pick bar (select / insert switch and whole-page input). */
  setPickMode: (on: boolean) => void
  isPickMode: () => boolean
  setLang: (lang: string) => void
  hasCandidates: () => boolean
  setPeek: (on: boolean) => void
  toggleHidden: () => void
  isHidden: () => boolean
  setEnabled: (on: boolean) => void
  dispose: () => void
}

type Session = {
  sid: string
  mode: string
  state: string
  selector?: string
  summary?: string
  prompt?: string
  url?: string
  error?: string
  selected?: number
  retryAccept?: boolean
  mountAutoReported?: boolean
  variants?: Array<{ n: number; label?: string }>
  updatedAt?: string
}

type PickKind = 'replace' | 'insert'

type Panel = {
  kind: PickKind
  /** `choose` is the chat-or-design card; `design` is the generate form. */
  stage: 'choose' | 'design'
  el: Element
  desc: LiveElement
  action: string
  prompt: string
  notes: string
  count: number
  mode: Mode
  position: 'before' | 'after'
  marksOpen: boolean
}

export const LIVE_MSG = 'grasp-embed:live'
export const LIVE_ACK = 'grasp-embed:live-ack'
export const LIVE_SESSIONS = 'grasp-embed:live-sessions'
export const LIVE_CMD = 'grasp-embed:live-cmd'

const ACTIONS = ['bolder', 'quieter', 'polish', 'typeset', 'colorize', 'layout', 'distill', 'adapt', 'animate', 'delight', 'overdrive', 'freeform']
const BUSY = new Set(['generating', 'refining', 'accepting', 'discarding'])
const OPEN = new Set(['generating', 'ready', 'refining', 'accepting', 'discarding', 'failed'])
const MOUNT_GRACE_MS = 5000

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string)
}

function newSid(): string {
  const rnd = Math.random().toString(36).slice(2, 10)
  return `lv${Date.now().toString(36)}${rnd}`.slice(0, 32)
}

function pathOf(url: string | undefined): string {
  if (!url) return ''
  try {
    return new URL(url, location.href).pathname
  } catch {
    return ''
  }
}

function isEditable(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null
  if (!el || !el.tagName) return false
  return el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)
}

/** Union of visible child boxes (display:contents wrappers have none). */
export function wrapperRect(w: Wrapper): DOMRect | null {
  let l = Infinity
  let t = Infinity
  let r = -Infinity
  let b = -Infinity
  const els = [w.original, ...w.variants.map((v) => v.el)].filter((e): e is HTMLElement => !!e && !e.hidden)
  for (const e of els) {
    const rc = e.getBoundingClientRect()
    if (!rc.width && !rc.height) continue
    l = Math.min(l, rc.left)
    t = Math.min(t, rc.top)
    r = Math.max(r, rc.right)
    b = Math.max(b, rc.bottom)
  }
  if (l === Infinity) return null
  return new DOMRect(l, t, r - l, b - t)
}

/** Theme matching the page background, or '' when the page paints none. */
export function pageTheme(): 'light' | 'dark' | '' {
  for (const el of [document.body, document.documentElement]) {
    if (!el) continue
    const m = getComputedStyle(el).backgroundColor.match(/rgba?\(([^)]*)\)/)
    if (!m) continue
    const [r, g, b, a = 1] = m[1].split(/[\s,/]+/).filter(Boolean).map(Number)
    if (!a) continue
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255 < 0.5 ? 'dark' : 'light'
  }
  return ''
}

export function createOverlay(opts: HostOpts, initialStrings?: Strings): LiveOverlay {
  let lang = opts.lang || (typeof navigator !== 'undefined' ? navigator.language || '' : '')
  let T = initialStrings ?? strings(lang)
  let enabled = false
  let pickMode = false
  let hidden = false
  let peek = false
  let inserting = false
  let hoverEl: Element | null = null
  let panel: Panel | null = null
  let steerText = ''
  let seq = 0
  let raf = 0
  let focusSid = ''
  const sessions = new Map<string, Session>()
  const views = new Map<string, SessionView>()
  const postedViews = new Map<string, string>()
  const pending = new Map<string, { sid: string; op: string; previousState?: string }>()
  const mountWatch = new Map<string, { key: string; since: number; sent: boolean; error?: string }>()
  let wrappers: Wrapper[] = []

  const host = document.createElement('grasp-live-overlay')
  host.setAttribute('data-grasp-live-overlay', '')
  host.setAttribute('data-page-agent-not-interactive', '')
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML =
    `<style>${OVERLAY_CSS}</style><div class="root">` +
    '<div data-layer="frames"></div><div data-layer="switchers"></div>' +
    '<div data-layer="annotations"></div><div data-layer="panel"></div><div data-layer="dock"></div></div>'
  const root = shadow.querySelector('.root') as HTMLElement
  const layer = (name: string) => shadow.querySelector(`[data-layer="${name}"]`) as HTMLElement
  const annotations = createAnnotations(layer('annotations'), T, () => renderPanel())
  const pageStyle = document.createElement('style')
  pageStyle.setAttribute('data-grasp-live-overlay', '')
  pageStyle.textContent = PAGE_CSS

  function mount() {
    if (!host.isConnected) (document.body || document.documentElement).appendChild(host)
    if (!pageStyle.isConnected) (document.head || document.documentElement).appendChild(pageStyle)
  }

  // ---------- state helpers ----------

  function viewOf(sid: string, w?: Wrapper): SessionView {
    let v = views.get(sid)
    if (!v) {
      v = getView(sid) || { current: w?.variants[0]?.n ?? 1, mode: 'inplace', params: {} }
      views.set(sid, v)
    }
    if (w && w.variants.length && !w.variants.some((x) => x.n === v!.current) && v.current !== 0) v.current = w.variants[0].n
    return v
  }

  function setView(sid: string, patch: Partial<SessionView>) {
    const v = { ...viewOf(sid), ...patch }
    views.set(sid, v)
    putView(sid, v)
    applyWrappers()
    syncViews()
    renderSwitchers()
  }

  function selectedParams(w: Wrapper | undefined, v: SessionView): Record<string, string> | undefined {
    const variant = w?.variants.find((x) => x.n === v.current)
    const vals = v.params[String(v.current)] || {}
    const params: Record<string, string> = {}
    for (const p of variant?.params || []) {
      const val = vals[p.id] ?? paramDefault(p)
      params[p.id] = p.kind === 'range' ? `${val}${p.unit || ''}` : String(val)
    }
    return Object.keys(params).length ? params : undefined
  }

  /** Publish the actual mounted view, including after reload, reconnect and HMR. */
  function syncViews() {
    for (const s of sessions.values()) {
      if (s.mode === 'steer') continue
      const w = wrappers.find((x) => x.sid === s.sid)
      const v = viewOf(s.sid, w)
      const current = OPEN.has(s.state) && w?.variants.some((x) => x.n === v.current) ? v.current : 0
      const params = current ? selectedParams(w, v) : undefined
      const message = { type: LIVE_MSG, op: 'state', sid: s.sid, current, mode: v.mode, ...(params ? { params } : {}) }
      const signature = JSON.stringify(message)
      if (postedViews.get(s.sid) !== signature && opts.post(message)) postedViews.set(s.sid, signature)
    }
  }

  function upsert(raw: unknown) {
    if (!raw || typeof raw !== 'object') return
    const s = raw as Session
    if (typeof s.sid !== 'string' || typeof s.state !== 'string') return
    const prev = sessions.get(s.sid)
    if (prev?.updatedAt && s.updatedAt && prev.updatedAt > s.updatedAt) return
    sessions.set(s.sid, { ...s })
    if (!OPEN.has(s.state)) {
      views.delete(s.sid)
      dropView(s.sid)
    }
  }

  function request(op: string, sid: string, extra: Record<string, unknown> = {}): boolean {
    const reqId = `${sid}-${++seq}`
    const ok = opts.post({ type: LIVE_MSG, op, sid, reqId, ...extra })
    if (!ok) {
      opts.notice(T.noDrawer)
      return false
    }
    pending.set(reqId, { sid, op, previousState: sessions.get(sid)?.state })
    return true
  }

  function isBusy(sid: string): boolean {
    for (const p of pending.values()) if (p.sid === sid) return true
    return BUSY.has(sessions.get(sid)?.state || '')
  }

  function openSessions(): Session[] {
    return [...sessions.values()].filter((s) => s.mode !== 'steer' && OPEN.has(s.state))
  }

  // ---------- page wrappers ----------

  function applyWrappers() {
    for (const w of wrappers) {
      const v = viewOf(w.sid, w)
      if (peek) {
        if (w.el.hasAttribute('data-grasp-compare')) w.el.removeAttribute('data-grasp-compare')
        showVariant(w, 0)
        continue
      }
      if (v.mode === 'compare' && !hidden) {
        w.el.setAttribute('data-grasp-compare', '')
        if (w.original) setVariantVisible(w.original, true)
        for (const x of w.variants) setVariantVisible(x.el, true)
      } else {
        w.el.removeAttribute('data-grasp-compare')
        showVariant(w, v.current)
      }
      for (const x of w.variants) {
        const vals = v.params[String(x.n)] || {}
        for (const p of x.params) applyParam(x.el, p, vals[p.id] ?? paramDefault(p))
      }
    }
  }

  function rescan() {
    const had = wrappers.length > 0
    wrappers = scanWrappers()
    if (!wrappers.length) hidden = false
    applyWrappers()
    syncViews()
    checkMounts()
    renderSwitchers()
    renderDock()
    layout()
    if (had !== wrappers.length > 0) opts.changed()
  }

  /** Wrapper still missing after the grace period; shown as a page hint. */
  function mountMissing(): Array<{ sid: string; error: string }> {
    const out: Array<{ sid: string; error: string }> = []
    for (const [sid, w] of mountWatch) if (w.error) out.push({ sid, error: w.error })
    return out
  }

  function checkMounts() {
    const now = Date.now()
    const before = JSON.stringify(mountMissing())
    for (const s of sessions.values()) {
      if (s.state !== 'ready' || s.mode === 'steer' || pathOf(s.url) !== location.pathname) {
        mountWatch.delete(s.sid)
        continue
      }
      const w = wrappers.find((x) => x.sid === s.sid)
      const missing = (s.variants || []).filter((variant) => !w?.variants.some((mounted) => mounted.n === variant.n))
      const key = s.updatedAt || s.state
      const cur = mountWatch.get(s.sid)
      if (w && w.variants.length && !missing.length) {
        mountWatch.delete(s.sid)
        continue
      }
      if (!cur || cur.key !== key) {
        mountWatch.set(s.sid, { key, since: now, sent: false })
        continue
      }
      if (now - cur.since < MOUNT_GRACE_MS) continue
      cur.error = !w
        ? `no [data-grasp-live="${s.sid}"] on ${location.pathname}`
        : missing.length
          ? `reported variants not rendered: ${missing.map((variant) => variant.n).join(', ')}`
          : 'wrapper has no variants (data-grasp-variant ≥ 1)'
      // Only one automatic report per attempt (the server enforces it across
      // tabs and reloads); later misses wait for the person on the page hint.
      if (!cur.sent && !s.mountAutoReported && document.visibilityState === 'visible') {
        cur.sent = true
        request('mount_failed', s.sid, { error: cur.error, auto: true })
      }
    }
    if (JSON.stringify(mountMissing()) !== before) renderDock()
  }

  function reportMount(sid: string) {
    const miss = mountMissing().find((m) => m.sid === sid)
    if (!miss || isBusy(sid)) return
    if (request('mount_failed', sid, { error: miss.error })) renderDock()
  }

  // ---------- rendering ----------

  function themeClass() {
    root.className = (pageTheme() || opts.theme()) === 'light' ? 'root light' : 'root'
  }

  /** Status hints and the pick bar, stacked just above preview-pick's bar. */
  function renderDock() {
    themeClass()
    const dock = layer('dock')
    const focused = shadow.activeElement as HTMLInputElement | null
    const restore = focused?.dataset?.input === 'steer' ? { start: focused.selectionStart, end: focused.selectionEnd } : null
    if (!enabled) {
      dock.innerHTML = ''
      return
    }
    const hint = hintHtml()
    const bar = pickMode ? pickBarHtml() : ''
    dock.innerHTML = hint || bar ? `<div class="dock">${hint}${bar}</div>` : ''
    if (restore) {
      const input = dock.querySelector<HTMLInputElement>('[data-input="steer"]')
      if (input) {
        input.focus()
        try {
          input.setSelectionRange(restore.start, restore.end)
        } catch {
          // Some input types have no selection.
        }
      }
    }
  }

  function hintHtml(): string {
    const away = openSessions().filter((s) => !wrappers.some((w) => w.sid === s.sid) && pathOf(s.url) && pathOf(s.url) !== location.pathname)
    const failures = [...sessions.values()].filter((s) => s.state === 'failed' && !wrappers.some((w) => w.sid === s.sid))
    const notMounted = mountMissing().filter((m) => sessions.get(m.sid)?.state === 'ready')
    return failures.length
      ? `<div class="hint" role="status">${failures.map((s) =>
          `<div>${esc(T.failed)}${s.error ? `: ${esc(s.error)}` : ''} ` +
          (s.mode === 'steer' ? `<button type="button" data-act="retry" data-sid="${esc(s.sid)}">${esc(T.retry)}</button>` : '') +
          (canRetryAdoption(s) ? `<button type="button" data-act="retry-accept" data-sid="${esc(s.sid)}">${esc(T.retryAccept)}</button>` : '') +
          `<button type="button" data-act="discard" data-sid="${esc(s.sid)}">${esc(s.mode === 'steer' ? T.dismissSteer : T.discard)}</button>` +
          (s.mode === 'steer' ? `<div>${esc(T.steerPartial)}</div>` : '') + '</div>').join('')}</div>`
      : notMounted.length
      ? `<div class="hint" role="status">${notMounted.map((m) =>
          `<div>${esc(T.notMounted)} ` +
          `<button type="button" data-act="reload">${esc(T.reloadPage)}</button>` +
          (isBusy(m.sid) ? '' : `<button type="button" data-act="report-mount" data-sid="${esc(m.sid)}">${esc(T.reportMount)}</button>`) +
          '</div>').join('')}</div>`
      : away.length
      ? `<div class="hint" role="status">${esc(fmt(T.pending, { n: away.length, path: pathOf(away[0].url) }))} ` +
        `<button type="button" data-act="goto-url" data-sid="${esc(away[0].sid)}">→</button></div>`
      : ''
  }

  /** Select / insert switch plus the whole-page input, shown while Pick is on. */
  function pickBarHtml(): string {
    const hasMic = !!(window as unknown as { SpeechRecognition?: unknown; webkitSpeechRecognition?: unknown }).SpeechRecognition ||
      !!(window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition
    return (
      `<div class="pickbar" role="group" aria-label="${esc(T.pickMode)}">` +
      `<div class="row"><div class="seg" role="group" aria-label="${esc(T.pickMode)}">` +
      `<button type="button" data-act="mode-select" aria-pressed="${!inserting}">${esc(T.modeSelect)}</button>` +
      `<button type="button" data-act="mode-insert" aria-pressed="${inserting}">${esc(T.modeInsert)}</button></div>` +
      `<span class="label pickhint" role="status">${esc(inserting ? T.insertPicking : T.pickHint)}</span></div>` +
      `<div class="steer"><input type="text" data-input="steer" placeholder="${esc(T.steer)}" aria-label="${esc(T.steer)}" value="${esc(steerText)}" />` +
      (hasMic ? `<button type="button" data-act="mic" title="${esc(T.mic)}" aria-label="${esc(T.mic)}">🎤</button>` : '') +
      `<button type="button" data-act="steer" aria-label="${esc(T.steerSend)}">↵</button></div>` +
      '</div>'
    )
  }

  /** Why the element cannot get design candidates right now, if it cannot. */
  function designBlocked(p: Panel): string {
    if (p.kind === 'replace' && (p.el.closest('[data-grasp-live]') || openSessions().length)) return T.openOther
    return ''
  }

  function targetHtml(p: Panel): string {
    return `<div class="target" title="${esc(p.desc.selector)}">${esc(p.desc.tagName)}${p.desc.text ? ` · ${esc(p.desc.text.slice(0, 40))}` : ''}</div>`
  }

  function renderPanel() {
    const el = layer('panel')
    annotations.setTarget(enabled && panel?.stage === 'design' ? panel.el : null)
    if (!panel || !enabled) {
      el.innerHTML = ''
      return
    }
    const p = panel
    if (p.stage === 'choose') {
      const blocked = designBlocked(p)
      el.innerHTML =
        '<div class="panel choose" role="dialog" aria-modal="false">' +
        targetHtml(p) +
        '<div class="row">' +
        `<button type="button" class="chip" data-act="to-chat">${esc(T.toChat)}</button>` +
        `<button type="button" class="chip go" data-act="to-design"${blocked ? ' disabled' : ''}>${esc(T.toDesign)}</button>` +
        `<button type="button" data-act="cancel-panel" title="${esc(T.cancel)}" aria-label="${esc(T.cancel)}">✕</button>` +
        '</div>' +
        (blocked ? `<div class="label" role="status">${esc(blocked)}</div>` : '') +
        '</div>'
      positionPanel()
      return
    }
    const seg = (act: string, label: string, items: Array<{ v: string; text: string; on: boolean }>) =>
      `<div class="field"><span class="label">${esc(label)}</span><div class="seg" role="group" aria-label="${esc(label)}">` +
      items.map((it) => `<button type="button" data-act="${act}" data-v="${esc(it.v)}" aria-pressed="${it.on}">${esc(it.text)}</button>`).join('') +
      '</div></div>'
    // Insert only ever uses the freeform action, so it gets a position switch instead of action chips.
    const head =
      p.kind === 'insert'
        ? seg('pos', T.position, (['before', 'after'] as const).map((v) => ({ v, text: v === 'before' ? T.before : T.after, on: p.position === v })))
        : `<div class="chips" role="group">${ACTIONS
            .map((a) => `<button type="button" class="chip" data-act="action" data-v="${a}" aria-pressed="${p.action === a}">${esc(T.actions[a] || a)}</button>`)
            .join('')}</div>`
    const needPrompt = p.kind === 'insert' || p.action === 'freeform'
    const open = marksExpanded(p)
    const n = annotations.count + (p.notes.trim() ? 1 : 0)
    const marks =
      `<div class="marks"><button type="button" class="disclosure" data-act="marks-toggle" aria-expanded="${open}">` +
      `<span aria-hidden="true">${open ? '▾' : '▸'}</span> ${esc(T.marks)}${n ? ` (${n})` : ''}</button>` +
      (open
        ? `<textarea data-input="notes" rows="1" placeholder="${esc(T.notes)}" aria-label="${esc(T.notes)}">${esc(p.notes)}</textarea>` +
          `<div class="row"><button type="button" class="chip" data-act="mark-draw" aria-pressed="${annotations.mode === 'draw'}"${annotations.count >= 8 ? ' disabled' : ''}>${esc(T.markDraw)}</button>` +
          `<button type="button" class="chip" data-act="mark-note" aria-pressed="${annotations.mode === 'note'}"${annotations.count >= 8 ? ' disabled' : ''}>${esc(T.markNote)}</button>` +
          (annotations.count
            ? `<button type="button" class="link" data-act="mark-undo">${esc(T.markUndo)}</button>` +
              `<button type="button" class="link" data-act="mark-clear">${esc(T.markClear)}</button>`
            : '') +
          '</div>' +
          (annotations.mode ? `<div class="label" role="status">${esc(T.markHint)}</div>` : '')
        : '') +
      '</div>'
    el.innerHTML =
      '<div class="panel" role="dialog" aria-modal="false">' +
      targetHtml(p) +
      head +
      `<textarea data-input="prompt" rows="2" placeholder="${esc(needPrompt ? T.promptRequired : T.prompt)}" aria-label="${esc(T.prompt)}">${esc(p.prompt)}</textarea>` +
      marks +
      seg('count', T.count, [2, 3, 4].map((c) => ({ v: String(c), text: String(c), on: p.count === c }))) +
      seg('pmode', T.display, (['inplace', 'compare'] as Mode[]).map((m) => ({ v: m, text: m === 'compare' ? T.compare : T.inplace, on: p.mode === m }))) +
      `<div class="row foot"><button type="button" data-act="cancel-panel">${esc(T.cancel)}</button>` +
      `<button type="button" class="go" data-act="go">${esc(T.go)}</button></div>` +
      '</div>'
    positionPanel()
  }

  function marksExpanded(p: Panel): boolean {
    return p.marksOpen || !!annotations.mode
  }

  function positionPanel() {
    annotations.layout()
    const box = layer('panel').firstElementChild as HTMLElement | null
    if (!box || !panel) return
    const r = panel.el.getBoundingClientRect()
    const w = Math.min(320, window.innerWidth - 32)
    let left = Math.min(Math.max(16, r.left), window.innerWidth - w - 16)
    let top = r.bottom + 8
    const h = box.offsetHeight || 260
    if (top + h > window.innerHeight - 72) top = Math.max(16, r.top - h - 8)
    if (!Number.isFinite(left)) left = 16
    box.style.left = `${left}px`
    box.style.top = `${top}px`
  }

  function stateLabel(state: string): string {
    return ({ generating: T.generating, refining: T.refining, accepting: T.accepting, discarding: T.discarding } as Record<string, string>)[state] || ''
  }

  function renderSwitchers() {
    const layerEl = layer('switchers')
    const frames = layer('frames')
    if (!enabled || hidden || peek) {
      layerEl.innerHTML = ''
      frames.innerHTML = ''
      return
    }
    let html = ''
    let frameHtml = ''
    for (const w of wrappers) {
      const s = sessions.get(w.sid)
      const v = viewOf(w.sid, w)
      const known = !!s
      const busy = isBusy(w.sid)
      const idx = w.variants.findIndex((x) => x.n === v.current)
      const cur = w.variants[idx]
      if (busy) frameHtml += `<div class="shimmer" data-shimmer="${esc(w.sid)}"></div>`
      const status = s?.state === 'failed'
        ? `<span class="err" title="${esc(s.error || '')}">${esc(T.failed)}${s.error ? `: ${esc(s.error)}` : ''}</span>`
        : busy
          ? `<span class="state" role="status">${esc(stateLabel(s?.state || 'generating') || T.generating)}</span>`
          : ''
      if (v.mode === 'compare') {
        const items = [...(w.original ? [{ n: 0, label: T.original }] : []), ...w.variants.map((x) => ({ n: x.n, label: x.label }))]
        for (const it of items) {
          const sel = it.n === v.current
          const text = it.n ? `${it.n}${it.label ? ` · ${it.label}` : ''}` : T.original
          frameHtml += `<div class="cframe${sel ? ' sel' : ''}" data-cframe="${esc(w.sid)}" data-n="${it.n}"></div>`
          html +=
            `<button type="button" class="tag${sel ? ' sel' : ''}" data-tag="${esc(w.sid)}" data-act="select" data-sid="${esc(w.sid)}" data-n="${it.n}" ` +
            `aria-pressed="${sel}" title="${esc(text)}">${esc(text)}</button>`
        }
        const backN = cur?.n ?? w.variants[0]?.n ?? 0
        html +=
          `<div class="sw" data-sw="${esc(w.sid)}" data-compare role="group" aria-label="${esc(T.comparing)}">` +
          `<span class="count">${esc(T.comparing)}</span>` +
          `<span class="lab">${esc(cur ? fmt(T.selected, { n: cur.label ? `${cur.n} · ${cur.label}` : cur.n }) : T.selectedOriginal)}</span>` +
          status +
          '<span class="sep" aria-hidden="true"></span>' +
          `<button type="button" data-act="inplace" data-sid="${esc(w.sid)}" data-n="${backN}">${esc(T.backInPlace)}</button>` +
          `<button type="button" data-act="discard" data-sid="${esc(w.sid)}" title="${esc(known ? T.discard : T.viewOnly)}"${busy || !known ? ' disabled' : ''}>${esc(T.discardAll)}</button>` +
          (canRetryAdoption(s) && !busy
            ? `<button type="button" class="accept" data-act="retry-accept" data-sid="${esc(w.sid)}">${esc(T.retryAccept)}</button>`
            : `<button type="button" class="accept" data-act="accept" data-sid="${esc(w.sid)}" data-n="${cur?.n ?? ''}"${busy || !known || !cur || s?.state !== 'ready' ? ' disabled' : ''}>` +
              `${esc(cur ? fmt(T.acceptN, { n: cur.n }) : T.accept)}</button>`) +
          '</div>'
        continue
      }
      frameHtml += `<div class="frame" data-frame="${esc(w.sid)}"></div>`
      html +=
        `<div class="sw" data-sw="${esc(w.sid)}" role="group" aria-label="${esc(T.title)}">` +
        `<button type="button" data-act="prev" data-sid="${esc(w.sid)}" aria-label="${esc(T.prev)}"${busy || w.variants.length < 2 ? ' disabled' : ''}>‹</button>` +
        `<span class="count" aria-live="polite">${idx + 1 || 0} / ${w.variants.length}</span>` +
        (cur?.label ? `<span class="lab">${esc(cur.label)}</span>` : '') +
        `<button type="button" data-act="next" data-sid="${esc(w.sid)}" aria-label="${esc(T.next)}"${busy || w.variants.length < 2 ? ' disabled' : ''}>›</button>` +
        status +
        '<span class="sep" aria-hidden="true"></span>' +
        `<button type="button" data-act="compare" data-sid="${esc(w.sid)}"${busy ? ' disabled' : ''}>${esc(T.sideBySide)}</button>` +
        `<button type="button" data-act="discard" data-sid="${esc(w.sid)}" aria-label="${esc(T.discard)}" title="${esc(known ? T.discard : T.viewOnly)}"${busy || !known ? ' disabled' : ''}>✕</button>` +
        `<button type="button" class="accept" data-act="accept" data-sid="${esc(w.sid)}" data-n="${cur?.n ?? ''}"${busy || !known || !cur || s?.state !== 'ready' ? ' disabled' : ''}>${esc(T.accept)}</button>` +
        (canRetryAdoption(s) && !busy ? `<button type="button" class="accept" data-act="retry-accept" data-sid="${esc(w.sid)}">${esc(T.retryAccept)}</button>` : '') +
        '</div>'
      if (cur && cur.params.length && !busy) html += paramsRow(w.sid, cur.n, cur.params, v)
    }
    // Sessions still generating whose wrapper is not in the page yet: shimmer the picked element.
    for (const s of sessions.values()) {
      if (s.mode === 'steer' || !BUSY.has(s.state) || wrappers.some((w) => w.sid === s.sid)) continue
      if (s.url && pathOf(s.url) !== location.pathname) continue
      frameHtml += `<div class="shimmer" data-shimmer-sel="${esc(s.sid)}"></div>`
      html +=
        `<div class="sw" data-sw-sel="${esc(s.sid)}" role="status"><span class="state">${esc(stateLabel(s.state) || T.generating)}</span>` +
        (s.state === 'generating'
          ? `<button type="button" data-act="discard" data-sid="${esc(s.sid)}">${esc(T.cancel)}</button>`
          : '') +
        '</div>'
    }
    layerEl.innerHTML = html
    frames.innerHTML = frameHtml
    layout()
  }

  function paramsRow(sid: string, n: number, params: LiveParam[], v: SessionView): string {
    const vals = v.params[String(n)] || {}
    const cells = params.map((p) => {
      const val = vals[p.id] ?? paramDefault(p)
      const label = `<span class="label">${esc(p.label || p.id)}</span>`
      const key = `data-param="${esc(sid)}|${n}|${esc(p.id)}"`
      if (p.kind === 'range') {
        return `${label}<input type="range" ${key} min="${p.min}" max="${p.max}" step="${p.step ?? 1}" value="${esc(String(val))}" aria-label="${esc(p.label || p.id)}" />`
      }
      if (p.kind === 'toggle') {
        return `${label}<input type="checkbox" ${key} ${val === 'on' ? 'checked' : ''} aria-label="${esc(p.label || p.id)}" />`
      }
      return (
        label +
        (p.options || [])
          .map((o) => `<button type="button" class="chip" ${key} data-v="${esc(o.value)}" aria-pressed="${String(val) === o.value}">${esc(o.label || o.value)}</button>`)
          .join('')
      )
    })
    return `<div class="params" data-params="${esc(sid)}">${cells.join('')}</div>`
  }

  function place(el: HTMLElement, rect: DOMRect | null, below = true) {
    if (!rect) {
      el.style.display = 'none'
      return
    }
    el.style.display = ''
    const w = el.offsetWidth || 240
    const h = el.offsetHeight || 36
    let left = rect.left + rect.width / 2 - w / 2
    left = Math.min(Math.max(8, left), window.innerWidth - w - 8)
    let top = below ? rect.bottom + 10 : rect.top - h - 6
    top = Math.min(Math.max(8, top), window.innerHeight - h - 72)
    el.style.left = `${left}px`
    el.style.top = `${top}px`
  }

  function box(el: HTMLElement, rect: DOMRect | null, pad = 3) {
    if (!rect) {
      el.style.display = 'none'
      return
    }
    el.style.display = ''
    el.style.left = `${rect.left - pad}px`
    el.style.top = `${rect.top - pad}px`
    el.style.width = `${rect.width + pad * 2}px`
    el.style.height = `${rect.height + pad * 2}px`
  }

  function selectorRect(sel: string | undefined): DOMRect | null {
    if (!sel) return null
    try {
      const el = document.querySelector(sel)
      return el ? el.getBoundingClientRect() : null
    } catch {
      return null
    }
  }

  function layout() {
    raf = 0
    for (const w of wrappers) {
      const rect = wrapperRect(w)
      const frame = shadow.querySelector<HTMLElement>(`[data-frame="${w.sid}"]`)
      if (frame) box(frame, rect)
      const shimmer = shadow.querySelector<HTMLElement>(`[data-shimmer="${w.sid}"]`)
      if (shimmer) box(shimmer, rect, 0)
      const sw = shadow.querySelector<HTMLElement>(`[data-sw="${w.sid}"]`)
      if (sw) place(sw, rect)
      const params = shadow.querySelector<HTMLElement>(`[data-params="${w.sid}"]`)
      if (params && sw && rect) {
        const r2 = new DOMRect(rect.left, rect.bottom + 10 + (sw.offsetHeight || 36), rect.width, 0)
        place(params, r2)
      }
      const candidateRect = (n: number) => {
        const el = n === 0 ? w.original : w.variants.find((x) => x.n === n)?.el
        const rc = el?.getBoundingClientRect()
        return rc && (rc.width || rc.height) ? rc : null
      }
      shadow.querySelectorAll<HTMLElement>(`[data-cframe="${w.sid}"]`).forEach((f) => box(f, candidateRect(Number(f.dataset.n)), 2))
      shadow.querySelectorAll<HTMLElement>(`[data-tag="${w.sid}"]`).forEach((b) => {
        const rc = candidateRect(Number(b.dataset.n))
        if (!rc || rc.bottom < 0 || rc.top > window.innerHeight) {
          b.style.display = 'none'
          return
        }
        b.style.display = ''
        b.style.left = `${Math.max(8, rc.left + 6)}px`
        b.style.top = `${Math.max(8, rc.top + 6)}px`
      })
    }
    for (const s of sessions.values()) {
      const sh = shadow.querySelector<HTMLElement>(`[data-shimmer-sel="${s.sid}"]`)
      const sw = shadow.querySelector<HTMLElement>(`[data-sw-sel="${s.sid}"]`)
      if (!sh && !sw) continue
      const rect = selectorRect(s.selector)
      if (sh) box(sh, rect, 0)
      if (sw) place(sw, rect)
    }
    positionPanel()
  }

  function scheduleLayout() {
    if (!raf) raf = requestAnimationFrame(layout)
  }

  function renderAll() {
    mount()
    renderDock()
    renderPanel()
    renderSwitchers()
    opts.changed()
  }

  // ---------- picking ----------

  function setInserting(on: boolean) {
    const was = inserting
    inserting = on
    if (on) {
      opts.stopPick()
      panel = null
      renderPanel()
    }
    clearHover()
    if (on) document.documentElement.setAttribute('data-grasp-live-picking', '')
    else document.documentElement.removeAttribute('data-grasp-live-picking')
    renderDock()
    if (was !== on) opts.changed()
  }

  /** Leave pick mode entirely: no insert picking and no pick bar. */
  function endPickMode() {
    const was = pickMode || inserting
    pickMode = false
    setInserting(false)
    renderDock()
    if (was) opts.changed()
  }

  function setPickMode(on: boolean) {
    if (!on || !enabled) {
      endPickMode()
      return
    }
    if (pickMode) return
    pickMode = true
    renderDock()
    opts.changed()
  }

  function newPanel(kind: PickKind, el: Element): Panel {
    return {
      kind,
      stage: kind === 'insert' ? 'design' : 'choose',
      el,
      desc: describeElement(el),
      action: kind === 'insert' ? 'freeform' : 'bolder',
      prompt: '',
      notes: '',
      count: 3,
      mode: 'inplace',
      position: 'after',
      marksOpen: false,
    }
  }

  function offer(el: Element) {
    if (!enabled) {
      opts.sendToChat(el)
      return
    }
    endPickMode()
    mount()
    panel = newPanel('replace', el)
    renderPanel()
  }

  function clearHover() {
    if (hoverEl) hoverEl.removeAttribute('data-grasp-live-hover')
    hoverEl = null
  }

  function pickable(t: EventTarget | null): Element | null {
    const el = t as Element | null
    if (!el || el.nodeType !== 1) return null
    if (el === host || host.contains(el) || opts.isOwnUi(el)) return null
    if (el.closest('[data-grasp-live-overlay],[data-grasp-preview-pick]')) return null
    if (el === document.documentElement || el === document.body) return null
    return el
  }

  /** Clicks inside either shadow root report inner nodes that `closest()` cannot see past. */
  function ownPath(path: EventTarget[]): boolean {
    return path.some((n) => n === host || (n instanceof Element && opts.isOwnUi(n)))
  }

  function onMove(ev: MouseEvent) {
    if (!inserting) return
    const path = ev.composedPath ? ev.composedPath() : []
    const el = ownPath(path) ? null : pickable(path[0] ?? ev.target)
    if (el === hoverEl) return
    clearHover()
    if (el) {
      hoverEl = el
      el.setAttribute('data-grasp-live-hover', '')
    }
  }

  function selectCompared(target: EventTarget | null | undefined) {
    const el = target as Element | null
    if (!enabled || hidden || peek || !el || el.nodeType !== 1) return
    for (const w of wrappers) {
      const v = viewOf(w.sid, w)
      if (v.mode !== 'compare') continue
      const n = w.original?.contains(el) ? 0 : w.variants.find((x) => x.el.contains(el))?.n
      if (n === undefined) continue
      focusSid = w.sid
      if (n !== v.current) setView(w.sid, { current: n })
      return
    }
  }

  function onClick(ev: MouseEvent) {
    const path = ev.composedPath ? ev.composedPath() : []
    if (ownPath(path)) return
    if (!inserting) {
      selectCompared(path[0] ?? ev.target)
      return
    }
    const el = pickable(path[0] ?? ev.target)
    if (!el) return
    ev.preventDefault()
    ev.stopPropagation()
    clearHover()
    endPickMode()
    panel = newPanel('insert', el)
    renderPanel()
  }

  function onKeydown(ev: KeyboardEvent) {
    if (!enabled) return
    if (ev.key === 'Escape') {
      if (inserting || pickMode) {
        endPickMode()
        opts.stopPick()
        ev.stopPropagation()
      } else if (panel) {
        panel = null
        renderPanel()
      } else if (!hidden && !peek) {
        const w = wrappers.find((x) => viewOf(x.sid, x).mode === 'compare')
        if (w) {
          exitCompare(w.sid)
          ev.stopPropagation()
        }
      }
      return
    }
    if (hidden) return
    const inShadow = shadow.activeElement !== null
    if (isEditable(ev.target) && !inShadow) return
    if (isEditable(shadow.activeElement)) return
    const sid = focusSid && wrappers.some((w) => w.sid === focusSid) ? focusSid : wrappers[0]?.sid
    if (!sid) return
    if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      step(sid, ev.key === 'ArrowLeft' ? -1 : 1)
      ev.preventDefault()
    } else if (ev.key === 'Enter' && inShadow) {
      const w = wrappers.find((x) => x.sid === sid)
      const v = viewOf(sid, w)
      if (w && sessions.get(sid)?.state === 'ready' && v.current > 0) accept(sid, v.current)
    }
  }

  // ---------- actions ----------

  function go() {
    if (!panel) return
    const p = panel
    const rect = p.el.getBoundingClientRect()
    if (!p.el.isConnected || !rect.width || !rect.height) {
      opts.notice(T.targetChanged)
      panel = null
      renderPanel()
      return
    }
    const needPrompt = p.kind === 'insert' || p.action === 'freeform'
    const marks = annotations.snapshot()
    if (needPrompt && !p.prompt.trim() && !p.notes.trim() && !marks.some((m) => m.text?.trim())) {
      opts.notice(T.promptRequired)
      return
    }
    const sid = newSid()
    const notes = p.notes.trim() ? [p.notes.trim()] : undefined
    const ok = request(p.kind === 'insert' ? 'insert' : 'generate', sid, {
      action: p.action,
      prompt: p.prompt.trim() || undefined,
      count: p.count,
      element: p.desc,
      url: location.href,
      position: p.kind === 'insert' ? p.position : undefined,
      notes,
      marks: marks.length ? marks : undefined,
    })
    if (!ok) return
    const v: SessionView = { current: 1, mode: p.mode, params: {} }
    views.set(sid, v)
    putView(sid, v)
    sessions.set(sid, { sid, mode: p.kind, state: 'generating', selector: p.desc.selector, url: location.href })
    syncViews()
    focusSid = sid
    panel = null
    opts.notice(T.sent, true)
    renderAll()
  }

  function steer() {
    const text = steerText.trim()
    if (!text) return
    const sid = newSid()
    if (request('steer', sid, { prompt: text, url: location.href })) {
      sessions.set(sid, { sid, mode: 'steer', state: 'generating', prompt: text, url: location.href })
      steerText = ''
      opts.stopPick()
      endPickMode()
      opts.notice(T.sent, true)
      renderDock()
      opts.changed()
    }
  }

  function step(sid: string, d: number) {
    const w = wrappers.find((x) => x.sid === sid)
    if (!w || !w.variants.length || isBusy(sid)) return
    const v = viewOf(sid, w)
    const i = Math.max(0, w.variants.findIndex((x) => x.n === v.current))
    const next = w.variants[(i + d + w.variants.length) % w.variants.length]
    focusSid = sid
    setView(sid, { current: next.n, mode: 'inplace' })
  }

  /** Back to in-place, keeping the selected variant (the original falls back to the first). */
  function exitCompare(sid: string) {
    const w = wrappers.find((x) => x.sid === sid)
    const v = viewOf(sid, w)
    const current = v.current || w?.variants[0]?.n || v.current
    setView(sid, { mode: 'inplace', current })
  }

  function accept(sid: string, n: number) {
    if (isBusy(sid) || !n || sessions.get(sid)?.state !== 'ready') return
    const w = wrappers.find((x) => x.sid === sid)
    if (!w?.variants.some((x) => x.n === n)) return
    const params = selectedParams(w, { ...viewOf(sid, w), current: n })
    if (request('accept', sid, { variant: n, params })) {
      const s = sessions.get(sid)
      if (s) s.state = 'accepting'
      renderSwitchers()
      renderDock()
    }
  }

  function discard(sid: string) {
    if (!sessions.has(sid)) {
      opts.notice(T.viewOnly)
      return
    }
    if (pendingFor(sid, 'discard')) return
    if (request('discard', sid)) {
      const s = sessions.get(sid)
      if (s) s.state = 'discarding'
      renderSwitchers()
      renderDock()
    }
  }

  function retry(sid: string) {
    const s = sessions.get(sid)
    if (!s || s.mode !== 'steer' || s.state !== 'failed' || isBusy(sid)) return
    if (request('steer', sid, { prompt: s.prompt, url: s.url })) {
      s.state = 'generating'
      renderAll()
    }
  }

  function canRetryAdoption(s: Session | undefined): boolean {
    return !!s && s.state === 'failed' && s.retryAccept === true && Number.isInteger(s.selected) && (s.selected ?? 0) > 0
  }

  function retryAdoption(sid: string) {
    const s = sessions.get(sid)
    if (!s || !canRetryAdoption(s) || isBusy(sid)) return
    // Recovery reuses the server's persisted selection and final params, even after HMR removed the wrapper.
    if (request('accept', sid, { variant: s.selected })) {
      s.state = 'accepting'
      renderAll()
    }
  }

  function pendingFor(sid: string, op: string): boolean {
    for (const p of pending.values()) if (p.sid === sid && p.op === op) return true
    return false
  }

  function setParam(key: string, value: string | number) {
    const [sid, nStr, id] = key.split('|')
    const v = viewOf(sid)
    const params = { ...v.params, [nStr]: { ...(v.params[nStr] || {}), [id]: value } }
    views.set(sid, { ...v, params })
    putView(sid, { ...v, params })
    applyWrappers()
    syncViews()
  }

  function startMic() {
    const W = window as unknown as { SpeechRecognition?: new () => SpeechRec; webkitSpeechRecognition?: new () => SpeechRec }
    const Ctor = W.SpeechRecognition || W.webkitSpeechRecognition
    if (!Ctor) return
    const rec = new Ctor()
    rec.lang = /^zh/i.test(lang) ? 'zh-CN' : 'en-US'
    rec.interimResults = false
    rec.onresult = (e) => {
      const said = e.results?.[0]?.[0]?.transcript || ''
      if (said) {
        steerText = (steerText ? `${steerText} ` : '') + said
        renderDock()
      }
    }
    try {
      rec.start()
    } catch {
      // Already listening or blocked.
    }
  }

  // ---------- events ----------

  function setPeek(on: boolean) {
    if (peek === on) return
    peek = on
    applyWrappers()
    renderSwitchers()
  }

  function toggleHidden() {
    hidden = !hidden
    applyWrappers()
    renderSwitchers()
    renderDock()
    opts.changed()
  }

  function setLang(next: string) {
    if (!next || next === lang) return
    lang = next
    T = strings(next)
    annotations.setStrings(T)
    renderDock()
    renderPanel()
    renderSwitchers()
  }

  shadow.addEventListener('click', (ev) => {
    const t = (ev.target as Element).closest?.('[data-act],[data-param]') as HTMLElement | null
    if (!t) return
    ev.preventDefault()
    ev.stopPropagation()
    const act = t.dataset.act || ''
    const sid = t.dataset.sid || ''
    const n = Number(t.dataset.n || 0)
    if (t.dataset.param && t.tagName === 'BUTTON') {
      setParam(t.dataset.param, t.dataset.v || '')
      renderSwitchers()
      return
    }
    if (sid) focusSid = sid
    switch (act) {
      case 'to-chat':
        if (panel) {
          opts.sendToChat(panel.el)
          panel = null
          renderPanel()
        }
        break
      case 'to-design':
        if (panel && !designBlocked(panel)) {
          panel.stage = 'design'
          renderPanel()
        }
        break
      case 'mic':
        startMic()
        break
      case 'mode-select':
        if (inserting) {
          setInserting(false)
          opts.startPick?.()
        }
        break
      case 'mode-insert':
        if (!inserting) setInserting(true)
        break
      case 'marks-toggle':
        if (panel) {
          panel.marksOpen = !marksExpanded(panel)
          if (!panel.marksOpen && annotations.mode) annotations.setMode(null)
          renderPanel()
        }
        break
      case 'steer':
        steer()
        break
      case 'goto-url': {
        const s = sessions.get(sid)
        if (s?.url) location.assign(s.url)
        break
      }
      case 'action':
      case 'count':
      case 'pmode':
      case 'pos':
        if (panel) {
          const v = t.dataset.v || ''
          if (act === 'action') panel.action = v
          if (act === 'count') panel.count = Number(v) || 3
          if (act === 'pmode') panel.mode = v === 'compare' ? 'compare' : 'inplace'
          if (act === 'pos') panel.position = v === 'before' ? 'before' : 'after'
          renderPanel()
        }
        break
      case 'mark-draw':
      case 'mark-note':
        annotations.setMode(act === 'mark-draw' ? 'draw' : 'note')
        renderPanel()
        break
      case 'mark-undo':
        annotations.undo()
        renderPanel()
        break
      case 'mark-clear':
        annotations.clear()
        renderPanel()
        break
      case 'cancel-panel':
        panel = null
        renderPanel()
        break
      case 'go':
        go()
        break
      case 'prev':
        step(sid, -1)
        break
      case 'next':
        step(sid, 1)
        break
      case 'compare':
        setView(sid, { mode: 'compare' })
        break
      case 'inplace':
        setView(sid, { mode: 'inplace', ...(n ? { current: n } : {}) })
        break
      case 'select':
        setView(sid, { current: n })
        break
      case 'accept':
        accept(sid, n)
        break
      case 'discard':
        discard(sid)
        break
      case 'retry':
        retry(sid)
        break
      case 'retry-accept':
        retryAdoption(sid)
        break
      case 'reload':
        location.reload()
        break
      case 'report-mount':
        reportMount(sid)
        break
    }
  })

  shadow.addEventListener('input', (ev) => {
    const t = ev.target as HTMLInputElement | HTMLTextAreaElement
    const key = t.dataset.input
    if (key === 'steer') steerText = t.value
    else if (key === 'prompt' && panel) panel.prompt = t.value
    else if (key === 'notes' && panel) panel.notes = t.value
    else if (t.dataset.param) {
      const input = t as HTMLInputElement
      setParam(t.dataset.param, input.type === 'checkbox' ? (input.checked ? 'on' : 'off') : Number(input.value))
    }
  })
  shadow.addEventListener('change', (ev) => {
    const t = ev.target as HTMLInputElement
    if (t.dataset.param && t.type === 'checkbox') setParam(t.dataset.param, t.checked ? 'on' : 'off')
  })
  shadow.addEventListener('keydown', (ev) => {
    const t = ev.target as HTMLElement
    if ((ev as KeyboardEvent).key === 'Enter' && t.dataset?.input === 'steer') {
      ev.preventDefault()
      steer()
    }
  })

  const observer = new MutationObserver((records) => {
    if (!enabled) return
    const relevant = records.some((r) => {
      const n = r.target as Node
      return !(n === host || host.contains(n) || n === pageStyle)
    })
    if (relevant) scheduleRescan()
  })
  let rescanTimer = 0
  function scheduleRescan() {
    if (rescanTimer) return
    rescanTimer = window.setTimeout(() => {
      rescanTimer = 0
      rescan()
    }, 60)
  }
  const mountTimer = window.setInterval(() => {
    if (enabled && sessions.size) {
      checkMounts()
      renderSwitchers()
    }
  }, 1000)

  document.addEventListener('mousemove', onMove, true)
  document.addEventListener('click', onClick, true)
  document.addEventListener('keydown', onKeydown, true)
  window.addEventListener('scroll', scheduleLayout, true)
  window.addEventListener('resize', scheduleLayout)
  window.addEventListener('popstate', scheduleRescan)

  // ---------- drawer ----------

  function onDrawer(msg: unknown) {
    if (!msg || typeof msg !== 'object') return
    const m = msg as Record<string, unknown>
    if (m.type === LIVE_SESSIONS) {
      if (m.replace === true) {
        sessions.clear()
        postedViews.clear()
      }
      if (Array.isArray(m.sessions)) for (const s of m.sessions) upsert(s)
      // A refresh lands here with the page's own view restored from sessionStorage.
      for (const s of sessions.values()) if (OPEN.has(s.state)) viewOf(s.sid)
      rescan()
      opts.changed()
      return
    }
    if (m.type === LIVE_ACK) {
      const reqId = typeof m.reqId === 'string' ? m.reqId : ''
      const p = pending.get(reqId)
      pending.delete(reqId)
      if (m.session) upsert(m.session)
      if (m.ok !== true) {
        opts.notice(typeof m.error === 'string' && m.error ? m.error : T.failed)
        if (p && (p.op === 'generate' || p.op === 'insert') && !m.session) sessions.delete(p.sid)
        if (p && (p.op === 'accept' || p.op === 'discard')) {
          const s = sessions.get(p.sid)
          if (s && (s.state === 'accepting' || s.state === 'discarding') && !m.session) s.state = p.previousState || 'ready'
        }
        if (p?.op === 'steer' && !m.session) {
          const s = sessions.get(p.sid)
          if (s) {
            s.state = 'failed'
            s.error = typeof m.error === 'string' ? m.error : T.failed
          }
        }
      }
      rescan()
      return
    }
    if (m.type === LIVE_CMD) {
      const sid = typeof m.sid === 'string' ? m.sid : ''
      const n = typeof m.variant === 'number' ? m.variant : 0
      if (!sid) return
      focusSid = sid
      switch (m.cmd) {
        case 'goto':
          if (n) setView(sid, { current: n, mode: 'inplace' })
          break
        case 'compare':
          setView(sid, { mode: 'compare' })
          break
        case 'inplace':
          exitCompare(sid)
          break
        case 'accept':
          accept(sid, n || viewOf(sid).current)
          break
        case 'discard':
          discard(sid)
          break
        case 'retry':
          retry(sid)
          break
        case 'retry-accept':
          retryAdoption(sid)
          break
      }
    }
  }

  function setEnabled(on: boolean) {
    enabled = on
    postedViews.clear()
    if (on) {
      mount()
      observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-grasp-live', 'data-grasp-variant', 'data-grasp-params', 'data-grasp-variant-label'] })
      rescan()
    } else {
      observer.disconnect()
      pickMode = false
      setInserting(false)
      panel = null
      renderAll()
      layer('switchers').innerHTML = ''
      layer('frames').innerHTML = ''
    }
    opts.changed()
  }

  return {
    onDrawer,
    offer,
    startInsert: () => {
      if (!enabled) return
      pickMode = true
      setInserting(true)
    },
    cancelPick: () => {
      if (inserting) setInserting(false)
    },
    isInserting: () => inserting,
    setPickMode,
    isPickMode: () => pickMode,
    setLang,
    hasCandidates: () => enabled && wrappers.length > 0,
    setPeek,
    toggleHidden,
    isHidden: () => hidden,
    setEnabled,
    dispose() {
      observer.disconnect()
      clearInterval(mountTimer)
      if (rescanTimer) clearTimeout(rescanTimer)
      document.removeEventListener('mousemove', onMove, true)
      document.removeEventListener('click', onClick, true)
      document.removeEventListener('keydown', onKeydown, true)
      window.removeEventListener('scroll', scheduleLayout, true)
      window.removeEventListener('resize', scheduleLayout)
      window.removeEventListener('popstate', scheduleRescan)
      setInserting(false)
      host.remove()
      pageStyle.remove()
    },
  }
}

type SpeechRec = {
  lang: string
  interimResults: boolean
  onresult: ((e: { results?: ArrayLike<ArrayLike<{ transcript?: string }>> }) => void) | null
  start: () => void
}
