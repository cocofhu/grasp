import { describeElement, type LiveElement } from './describe'
import { fmt, strings, type Strings } from './i18n'
import { applyParam, paramDefault, scanWrappers, showVariant, type LiveParam, type Wrapper } from './scan'
import { OVERLAY_CSS, PAGE_CSS } from './styles'
import { dropView, getView, putView, type Mode, type SessionView } from './viewStore'

/** What preview-pick.js hands the overlay. */
export type HostOpts = {
  /** Post to the chat drawer; false when the drawer is not ready. */
  post: (msg: Record<string, unknown>) => boolean
  theme: () => string
  notice: (text: string, ok?: boolean) => void
  /** Turn off preview-pick's own Pick mode. */
  stopPick: () => void
  /** Re-render preview-pick's bar (Live button state). */
  changed: () => void
  isOwnUi: (el: Element) => boolean
}

export type LiveOverlay = {
  onDrawer: (msg: unknown) => void
  toggle: () => void
  isOpen: () => boolean
  setEnabled: (on: boolean) => void
  dispose: () => void
}

type Session = {
  sid: string
  mode: string
  state: string
  selector?: string
  summary?: string
  url?: string
  error?: string
  variants?: Array<{ n: number; label?: string }>
  updatedAt?: string
}

type PickKind = 'replace' | 'insert'

type Panel = {
  kind: PickKind
  el: Element
  desc: LiveElement
  action: string
  prompt: string
  notes: string
  count: number
  mode: Mode
  position: 'before' | 'after'
}

export const LIVE_MSG = 'grasp-embed:live'
export const LIVE_ACK = 'grasp-embed:live-ack'
export const LIVE_SESSIONS = 'grasp-embed:live-sessions'
export const LIVE_CMD = 'grasp-embed:live-cmd'

const ACTIONS = ['bolder', 'quieter', 'polish', 'typeset', 'colorize', 'layout', 'distill', 'adapt', 'freeform']
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

export function createOverlay(opts: HostOpts, T: Strings = strings()): LiveOverlay {
  let enabled = false
  let open = false
  let hidden = false
  let peek = false
  let picking: PickKind | null = null
  let hoverEl: Element | null = null
  let panel: Panel | null = null
  let steerText = ''
  let seq = 0
  let raf = 0
  let focusSid = ''
  const sessions = new Map<string, Session>()
  const views = new Map<string, SessionView>()
  const pending = new Map<string, { sid: string; op: string }>()
  const mountWatch = new Map<string, { key: string; since: number; sent: boolean }>()
  let wrappers: Wrapper[] = []

  const host = document.createElement('grasp-live-overlay')
  host.setAttribute('data-grasp-live-overlay', '')
  host.setAttribute('data-page-agent-not-interactive', '')
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML =
    `<style>${OVERLAY_CSS}</style><div class="root">` +
    '<div data-layer="frames"></div><div data-layer="switchers"></div>' +
    '<div data-layer="panel"></div><div data-layer="hint"></div><div data-layer="bar"></div></div>'
  const root = shadow.querySelector('.root') as HTMLElement
  const layer = (name: string) => shadow.querySelector(`[data-layer="${name}"]`) as HTMLElement
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
    opts.post({ type: LIVE_MSG, op: 'state', sid, current: v.current, mode: v.mode })
    applyWrappers()
    renderSwitchers()
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
    pending.set(reqId, { sid, op })
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
        const wide = (wrapperRect(w)?.width ?? 0) > window.innerWidth * 0.6
        w.el.setAttribute('data-grasp-compare', wide ? 'stack' : 'grid')
        if (w.original) w.original.hidden = false
        for (const x of w.variants) x.el.hidden = false
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
    wrappers = scanWrappers()
    applyWrappers()
    checkMounts()
    renderSwitchers()
    renderBar()
    layout()
  }

  function checkMounts() {
    const now = Date.now()
    for (const s of sessions.values()) {
      if (s.state !== 'ready' || s.mode === 'steer' || pathOf(s.url) !== location.pathname) {
        mountWatch.delete(s.sid)
        continue
      }
      const w = wrappers.find((x) => x.sid === s.sid)
      const key = s.updatedAt || s.state
      const cur = mountWatch.get(s.sid)
      if (w && w.variants.length) {
        mountWatch.delete(s.sid)
        continue
      }
      if (!cur || cur.key !== key) {
        mountWatch.set(s.sid, { key, since: now, sent: false })
        continue
      }
      if (!cur.sent && now - cur.since >= MOUNT_GRACE_MS) {
        cur.sent = true
        const error = w ? 'wrapper has no variants (data-grasp-variant ≥ 1)' : `no [data-grasp-live="${s.sid}"] on ${location.pathname}`
        request('mount_failed', s.sid, { error })
      }
    }
  }

  // ---------- rendering ----------

  function themeClass() {
    root.className = opts.theme() === 'light' ? 'root light' : 'root'
  }

  function renderBar() {
    themeClass()
    const bar = layer('bar')
    const focused = shadow.activeElement as HTMLInputElement | null
    const keepFocus = focused?.dataset?.input === 'steer' ? { start: focused.selectionStart, end: focused.selectionEnd } : null
    if (!enabled || !open) {
      bar.innerHTML = ''
      layer('hint').innerHTML = ''
      return
    }
    const busy = [...sessions.values()].some((s) => BUSY.has(s.state)) || pending.size > 0
    const hasMic = !!(window as unknown as { SpeechRecognition?: unknown; webkitSpeechRecognition?: unknown }).SpeechRecognition ||
      !!(window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition
    bar.innerHTML =
      `<div class="bar" role="toolbar" aria-label="${esc(T.title)}">` +
      `<span class="mark${busy ? ' busy' : ''}" aria-hidden="true">L</span>` +
      `<button type="button" class="act" data-act="pick" aria-pressed="${picking === 'replace'}">${esc(T.pick)}</button>` +
      `<button type="button" class="act" data-act="insert" aria-pressed="${picking === 'insert'}" title="${esc(T.insert)}" aria-label="${esc(T.insert)}">+</button>` +
      `<button type="button" class="act" data-act="eye" aria-pressed="${hidden}" title="${esc(T.eye)}" aria-label="${esc(T.eye)}">👁</button>` +
      '<span class="sep" aria-hidden="true"></span>' +
      `<span class="steer"><input type="text" data-input="steer" placeholder="${esc(T.steer)}" aria-label="${esc(T.steer)}" value="${esc(steerText)}" />` +
      (hasMic ? `<button type="button" data-act="mic" title="${esc(T.mic)}" aria-label="${esc(T.mic)}">🎤</button>` : '') +
      `<button type="button" data-act="steer" aria-label="${esc(T.steerSend)}">↵</button></span>` +
      `<button type="button" data-act="close" title="${esc(T.close)}" aria-label="${esc(T.close)}">✕</button>` +
      '</div>'
    if (keepFocus) {
      const input = bar.querySelector<HTMLInputElement>('[data-input="steer"]')
      if (input) {
        input.focus()
        try {
          input.setSelectionRange(keepFocus.start, keepFocus.end)
        } catch {
          // Some input types have no selection.
        }
      }
    }
    const away = openSessions().filter((s) => !wrappers.some((w) => w.sid === s.sid) && pathOf(s.url) && pathOf(s.url) !== location.pathname)
    layer('hint').innerHTML = away.length
      ? `<div class="hint" role="status">${esc(fmt(T.pending, { n: away.length, path: pathOf(away[0].url) }))} ` +
        `<button type="button" data-act="goto-url" data-sid="${esc(away[0].sid)}">→</button></div>`
      : picking
        ? `<div class="hint" role="status">${esc(picking === 'insert' ? T.insertPicking : T.picking)}</div>`
        : ''
  }

  function renderPanel() {
    const el = layer('panel')
    if (!panel || !open) {
      el.innerHTML = ''
      return
    }
    const p = panel
    const chips = (p.kind === 'insert' ? ['freeform'] : ACTIONS)
      .map((a) => `<button type="button" class="chip" data-act="action" data-v="${a}" aria-pressed="${p.action === a}">${esc(T.actions[a] || a)}</button>`)
      .join('')
    const counts = [2, 3, 4]
      .map((n) => `<button type="button" class="chip" data-act="count" data-v="${n}" aria-pressed="${p.count === n}">${n}</button>`)
      .join('')
    const modes = (['inplace', 'compare'] as Mode[])
      .map((m) => `<button type="button" class="chip" data-act="pmode" data-v="${m}" aria-pressed="${p.mode === m}">${esc(m === 'compare' ? T.compare : T.inplace)}</button>`)
      .join('')
    const pos =
      p.kind === 'insert'
        ? `<div class="row">${(['before', 'after'] as const)
            .map((v) => `<button type="button" class="chip" data-act="pos" data-v="${v}" aria-pressed="${p.position === v}">${esc(v === 'before' ? T.before : T.after)}</button>`)
            .join('')}</div>`
        : ''
    const needPrompt = p.kind === 'insert' || p.action === 'freeform'
    el.innerHTML =
      '<div class="panel" role="dialog" aria-modal="false">' +
      `<div class="target" title="${esc(p.desc.selector)}">${esc(p.desc.tagName)}${p.desc.text ? ` · ${esc(p.desc.text.slice(0, 40))}` : ''}</div>` +
      `<div class="chips" role="group">${chips}</div>` +
      pos +
      `<textarea data-input="prompt" rows="2" placeholder="${esc(needPrompt ? T.promptRequired : T.prompt)}" aria-label="${esc(T.prompt)}">${esc(p.prompt)}</textarea>` +
      `<textarea data-input="notes" rows="1" placeholder="${esc(T.notes)}" aria-label="${esc(T.notes)}">${esc(p.notes)}</textarea>` +
      `<div class="row"><span class="label">${esc(T.count)}</span>${counts}<span class="label">${esc(T.display)}</span>${modes}</div>` +
      `<div class="row"><button type="button" data-act="cancel-panel">${esc(T.cancel)}</button>` +
      `<button type="button" class="go" data-act="go">${esc(T.go)}</button></div>` +
      '</div>'
    positionPanel()
  }

  function positionPanel() {
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
      frameHtml += `<div class="frame" data-frame="${esc(w.sid)}"></div>`
      if (busy) frameHtml += `<div class="shimmer" data-shimmer="${esc(w.sid)}"></div>`
      if (v.mode === 'compare') {
        const items = [...(w.original ? [{ n: 0, label: T.original }] : []), ...w.variants.map((x) => ({ n: x.n, label: x.label }))]
        for (const it of items) {
          html +=
            `<div class="badge" data-badge="${esc(w.sid)}" data-n="${it.n}"><b>${it.n || '0'}${it.label ? ` · ${esc(it.label)}` : ''}</b>` +
            (known && !busy
              ? it.n === 0
                ? `<button type="button" data-act="discard" data-sid="${esc(w.sid)}">${esc(T.keepOriginal)}</button>`
                : `<button type="button" class="accept" data-act="accept" data-sid="${esc(w.sid)}" data-n="${it.n}">${esc(T.choose)}</button>`
              : '') +
            (it.n ? `<button type="button" data-act="inplace" data-sid="${esc(w.sid)}" data-n="${it.n}">${esc(T.viewInPlace)}</button>` : '') +
            '</div>'
        }
        continue
      }
      const status = s?.state === 'failed'
        ? `<span class="err" title="${esc(s.error || '')}">${esc(T.failed)}${s.error ? `: ${esc(s.error)}` : ''}</span>`
        : busy
          ? `<span class="state" role="status">${esc(stateLabel(s?.state || 'generating') || T.generating)}</span>`
          : ''
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
      shadow.querySelectorAll<HTMLElement>(`[data-badge="${w.sid}"]`).forEach((b) => {
        const n = Number(b.dataset.n)
        const el = n === 0 ? w.original : w.variants.find((x) => x.n === n)?.el
        const rc = el?.getBoundingClientRect()
        if (!rc) {
          b.style.display = 'none'
          return
        }
        b.style.display = ''
        b.style.left = `${Math.max(8, rc.left)}px`
        b.style.top = `${Math.max(8, rc.top - 30)}px`
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
    renderBar()
    renderPanel()
    renderSwitchers()
    opts.changed()
  }

  // ---------- picking ----------

  function setPicking(kind: PickKind | null) {
    picking = kind
    if (kind) opts.stopPick()
    clearHover()
    if (kind) document.documentElement.setAttribute('data-grasp-live-picking', '')
    else document.documentElement.removeAttribute('data-grasp-live-picking')
    renderBar()
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

  function onMove(ev: MouseEvent) {
    if (!picking) return
    const el = pickable(ev.composedPath ? ev.composedPath()[0] ?? ev.target : ev.target)
    if (el === hoverEl) return
    clearHover()
    if (el) {
      hoverEl = el
      el.setAttribute('data-grasp-live-hover', '')
    }
  }

  function onClick(ev: MouseEvent) {
    if (!picking) return
    const path = ev.composedPath ? ev.composedPath() : []
    if (path.includes(host)) return
    const el = pickable(path[0] ?? ev.target)
    if (!el) return
    ev.preventDefault()
    ev.stopPropagation()
    const kind = picking
    clearHover()
    const wrapperEl = el.closest('[data-grasp-live]')
    setPicking(null)
    if (wrapperEl && kind === 'replace') {
      opts.notice(T.openOther)
      return
    }
    if (kind === 'replace' && openSessions().some((s) => s.mode !== 'steer')) {
      opts.notice(T.openOther)
      return
    }
    panel = {
      kind,
      el,
      desc: describeElement(el),
      action: kind === 'insert' ? 'freeform' : 'bolder',
      prompt: '',
      notes: '',
      count: 3,
      mode: 'inplace',
      position: 'after',
    }
    renderPanel()
  }

  function onKeydown(ev: KeyboardEvent) {
    if (!enabled || hidden) return
    if (ev.key === 'Escape') {
      if (picking) {
        setPicking(null)
        ev.stopPropagation()
      } else if (panel) {
        panel = null
        renderPanel()
      }
      return
    }
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
    const needPrompt = p.kind === 'insert' || p.action === 'freeform'
    if (needPrompt && !p.prompt.trim() && !p.notes.trim()) {
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
    })
    if (!ok) return
    const v: SessionView = { current: 1, mode: p.mode, params: {} }
    views.set(sid, v)
    putView(sid, v)
    sessions.set(sid, { sid, mode: p.kind, state: 'generating', selector: p.desc.selector, url: location.href })
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
      steerText = ''
      opts.notice(T.sent, true)
      renderBar()
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

  function accept(sid: string, n: number) {
    if (isBusy(sid) || !n) return
    const w = wrappers.find((x) => x.sid === sid)
    const variant = w?.variants.find((x) => x.n === n)
    const vals = viewOf(sid, w).params[String(n)] || {}
    const params: Record<string, string> = {}
    for (const p of variant?.params || []) {
      const val = vals[p.id] ?? paramDefault(p)
      params[p.id] = p.kind === 'range' ? `${val}${p.unit || ''}` : String(val)
    }
    if (request('accept', sid, { variant: n, params: Object.keys(params).length ? params : undefined })) {
      const s = sessions.get(sid)
      if (s) s.state = 'accepting'
      renderSwitchers()
      renderBar()
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
      renderBar()
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
  }

  function startMic() {
    const W = window as unknown as { SpeechRecognition?: new () => SpeechRec; webkitSpeechRecognition?: new () => SpeechRec }
    const Ctor = W.SpeechRecognition || W.webkitSpeechRecognition
    if (!Ctor) return
    const rec = new Ctor()
    rec.lang = navigator.language || 'zh-CN'
    rec.interimResults = false
    rec.onresult = (e) => {
      const said = e.results?.[0]?.[0]?.transcript || ''
      if (said) {
        steerText = (steerText ? `${steerText} ` : '') + said
        renderBar()
      }
    }
    try {
      rec.start()
    } catch {
      // Already listening or blocked.
    }
  }

  // ---------- events ----------

  let eyeDownAt = 0
  shadow.addEventListener('pointerdown', (ev) => {
    const t = (ev.target as Element).closest?.('[data-act="eye"]')
    if (!t) return
    eyeDownAt = Date.now()
    peek = true
    applyWrappers()
    renderSwitchers()
  })
  const endPeek = () => {
    if (!peek) return
    peek = false
    applyWrappers()
    renderSwitchers()
  }
  shadow.addEventListener('pointerup', endPeek)
  shadow.addEventListener('pointerleave', endPeek, true)

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
      case 'pick':
        setPicking(picking === 'replace' ? null : 'replace')
        break
      case 'insert':
        setPicking(picking === 'insert' ? null : 'insert')
        break
      case 'eye':
        if (Date.now() - eyeDownAt < 300) {
          hidden = !hidden
          applyWrappers()
          renderSwitchers()
          renderBar()
        }
        break
      case 'close':
        setOpen(false)
        break
      case 'mic':
        startMic()
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
      case 'accept':
        accept(sid, n)
        break
      case 'discard':
        discard(sid)
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
      if (m.replace === true) sessions.clear()
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
          if (s && (s.state === 'accepting' || s.state === 'discarding') && !m.session) s.state = 'ready'
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
      if (!open) setOpen(true)
      switch (m.cmd) {
        case 'goto':
          if (n) setView(sid, { current: n, mode: 'inplace' })
          break
        case 'compare':
          setView(sid, { mode: 'compare' })
          break
        case 'inplace':
          setView(sid, { mode: 'inplace' })
          break
        case 'accept':
          accept(sid, n || viewOf(sid).current)
          break
        case 'discard':
          discard(sid)
          break
      }
    }
  }

  function setOpen(on: boolean) {
    open = on && enabled
    if (!open) {
      setPicking(null)
      panel = null
    }
    if (open) rescan()
    renderAll()
  }

  function setEnabled(on: boolean) {
    enabled = on
    if (on) {
      mount()
      observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-grasp-live', 'data-grasp-variant'] })
      rescan()
    } else {
      observer.disconnect()
      setOpen(false)
      layer('switchers').innerHTML = ''
      layer('frames').innerHTML = ''
    }
    opts.changed()
  }

  return {
    onDrawer,
    toggle: () => setOpen(!open),
    isOpen: () => open,
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
      setPicking(null)
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
