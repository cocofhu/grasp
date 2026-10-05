import type { IncomingMessage, ServerResponse } from 'node:http'
import type { Duplex } from 'node:stream'
import type { WebSocketServer } from 'ws'
import type { AgentCapabilities } from '../src/lib/api/apiTypes'

/**
 * Deterministic agent/dev-server surrogate for browser protocol tests. Source
 * lives on this server, independently of the page DOM and Vue drawer state.
 * These tests verify the shipped UI/bridge; Go tests cover agent transitions.
 */
type Event = {
  op: string
  sid: string
  action?: string
  prompt?: string
  count?: number
  scope?: 'page'
  variant?: number
  position?: string
  params?: Record<string, unknown>
  element?: { selector?: string; text?: string }
  notes?: string[]
  marks?: Array<{
    kind: 'draw' | 'note'
    points: Array<{ x: number; y: number }>
    text?: string
    targets?: Array<{ selector: string; text?: string }>
  }>
  url?: string
  error?: string
}
type Session = {
  sid: string
  mode: string
  state: string
  prompt?: string
  selector?: string
  summary?: string
  url?: string
  variants?: Array<{ n: number; label: string }>
  selected?: number
  error?: string
  updatedAt: string
  original: string
  attempts: number
  refined?: number[]
}
type Reply = { images?: Array<{data?:string; mimeType?:string; name?:string}>; annotations?: Array<{selector?:string; label?:string; note?:string}>; token?: string; live?: Event; text?: string; liveCtx?: { sid: string; current: number; params?: Record<string, unknown> } }
type State = {
  permissionPreset?: 'full' | 'react_only'
  node: { type: 'agent'; caps: AgentCapabilities }
  source: string
  revision: number
  sessions: Session[]
  messages: Array<{ id: string; text: string; live?: { sid: string; op: string; variant?: number } }>
  requests: Reply[]
}

export const LIVE_ORIGINAL = '<section id="newsletter" class="newsletter"><h2>Newsletter original</h2><p>Monthly design notes</p><button type="button" data-testid="newsletter-action">Subscribe</button></section>'
const states = new Map<string, State>()
const listeners = new Map<string, Set<(frame: unknown) => void>>()
const labels = ['层级', '紧凑', '强调色', '分栏']
const params = JSON.stringify([
  { id: 'gap', kind: 'range', min: 8, max: 48, step: 4, default: 24, unit: 'px', label: '间距' },
  { id: 'tone', kind: 'steps', default: 'soft', options: [{ value: 'soft', label: '柔和' }, { value: 'strong', label: '强烈' }], label: '强调' },
])

// Capability presets selectable via /__e2e/live/reset?caps=…
export const LIVE_CAPS: Record<string, AgentCapabilities> = {
  clarify: { interaction: 'clarify', tools: ['ask_question', 'set_preview'] },
  review: { interaction: 'auto', review: true, tools: ['set_preview'] },
  'no-preview': { interaction: 'clarify', tools: ['ask_question'] },
}

function stateFor(key: string): State {
  let state = states.get(key)
  if (!state) {
    state = { node: { type: 'agent', caps: LIVE_CAPS.clarify }, source: LIVE_ORIGINAL, revision: 0, sessions: [], messages: [], requests: [] }
    states.set(key, state)
  }
  return state
}

function publicState(state: State) {
  return { ...state, sessions: state.sessions.map(({ original: _original, attempts: _attempts, refined: _refined, ...session }) => session) }
}

function candidate(n: number, inserted: boolean, finalParams?: Record<string, unknown>, refined = false): string {
  const title = `${refined ? 'Refined' : inserted ? 'Inserted' : 'Newsletter'} variant ${n}`
  const gap = typeof finalParams?.gap === 'string' ? finalParams.gap : '24px'
  const tone = finalParams?.tone === 'strong' ? '#1249b6' : '#30485b'
  const live = finalParams ? '' : ` data-grasp-variant="${n}" data-grasp-variant-label="${labels[n - 1]}" data-grasp-params='${params}'${n === 1 ? '' : ' hidden'}`
  // Variant 3 intentionally has author inline !important. Hiding must defeat
  // it, and comparison must restore it rather than permanently forcing none.
  const style = finalParams ? `display:grid;gap:${gap};color:${tone}` : `display:grid${n === 3 ? '!important' : ''};gap:var(--gp-gap,24px)`
  return `<section class="newsletter"${live} style="${style}"><h2>${title}</h2><p>Monthly design notes</p><button type="button" data-testid="newsletter-action">Subscribe</button></section>`
}

function preview(session: Session, count: number): string {
  const inserted = session.mode === 'insert'
  const original = inserted ? '' : LIVE_ORIGINAL.replace('class="newsletter"', 'class="newsletter" data-grasp-variant="0" hidden')
  const wrapper = `<div data-grasp-live="${session.sid}" style="display:contents">${original}${Array.from({ length: count }, (_, i) => candidate(i + 1, inserted)).join('')}</div>`
  return inserted ? session.original + wrapper : wrapper
}

function update(state: State, session: Session, next: string) {
  session.state = next
  session.updatedAt = new Date().toISOString()
  state.revision++
  for (const [key, stored] of states) {
    if (stored === state) {
      const publicSession = publicState(state).sessions.find((item) => item.sid === session.sid)
      listeners.get(key)?.forEach((send) => send({ type: 'live', session: publicSession }))
    }
  }
}

function runAgent(state: State, session: Session, ev: Event) {
  session.error = undefined
  switch (ev.op) {
    case 'generate':
    case 'insert': {
      const count = ev.count || 3
      session.variants = Array.from({ length: count }, (_, i) => ({ n: i + 1, label: labels[i] }))
      // Mount failure exercises the real overlay's delayed missing-wrapper report.
      state.source = ev.prompt === 'simulate missing wrapper' ? session.original : preview(session, count)
      update(state, session, 'ready')
      break
    }
    case 'accept':
      session.selected = ev.variant
      state.source = (session.mode === 'insert' ? session.original : '') + candidate(ev.variant || 1, session.mode === 'insert', ev.params || {}, session.refined?.includes(ev.variant || 1))
      update(state, session, 'accepted')
      break
    case 'discard':
      // Failed Steer dismissal acknowledges partial edits; it is not a rollback.
      if (session.mode !== 'steer') state.source = session.original
      update(state, session, 'discarded')
      break
    case 'steer':
      session.attempts++
      if (session.attempts === 1) {
        state.source = session.original + '<p id="steer-partial">Partial page adjustment</p>'
        session.error = '模拟整页调整中断，部分修改已写入源码'
        update(state, session, 'failed')
      } else {
        state.source = session.original + '<p id="steer-complete">Page adjustment complete</p>'
        update(state, session, 'done')
      }
      break
    case 'mount_failed':
      session.error = ev.error || 'Missing preview wrapper'
      update(state, session, 'failed')
      break
    case 'refine':
      state.source = state.source.replace(`Newsletter variant ${ev.variant}`, `Refined variant ${ev.variant}`)
      session.refined = [...(session.refined || []), ev.variant || 1]
      update(state, session, 'ready')
      break
  }
}

async function readReply(req: IncomingMessage): Promise<Reply> {
  let body = ''
  for await (const chunk of req) body += chunk.toString()
  return JSON.parse(body) as Reply
}

function json(res: ServerResponse, body: unknown, status = 200) {
  res.statusCode = status
  res.setHeader('Content-Type', 'application/json; charset=utf-8')
  res.setHeader('Cache-Control', 'no-store')
  res.end(JSON.stringify(body))
}

function processReply(state: State, reply: Reply, res: ServerResponse) {
  state.requests.push(reply)
  let ev = reply.live
  if (!ev && reply.liveCtx) {
    // Stand-in agent interprets the chat. The actual server's state and
    // permission validation is exercised separately by engine/handler tests.
    ev = { op: reply.text?.includes('就用这个') ? 'accept' : 'refine', sid: reply.liveCtx.sid, variant: reply.liveCtx.current, params: reply.liveCtx.params }
  }
  if (!ev) {
    state.messages.push({ id: String(state.messages.length + 1), text: reply.text || '附件反馈' })
    json(res, { status: 'accepted' })
    return
  }
  const event = ev
  let session = state.sessions.find((s) => s.sid === event.sid)
  if (!session) {
    session = {
      sid: event.sid, mode: event.op === 'steer' ? 'steer' : event.op === 'insert' ? 'insert' : 'replace',
      state: 'generating', prompt: event.prompt, selector: event.element?.selector,
      summary: event.element?.text || (event.scope === 'page' ? '页面候选' : undefined), url: event.url, updatedAt: new Date().toISOString(), original: state.source, attempts: 0,
    }
    state.sessions.push(session)
  }
  const active = session
  const next = event.op === 'accept' ? 'accepting' : event.op === 'discard' ? 'discarding' : event.op === 'refine' || event.op === 'mount_failed' ? 'refining' : 'generating'
  update(state, active, next)
  state.messages.push({ id: String(state.messages.length + 1), text: reply.text || event.prompt || event.op, live: { sid: event.sid, op: event.op, variant: event.variant } })
  json(res, { status: 'accepted', live: publicState(state).sessions.find((s) => s.sid === active.sid) })
  setTimeout(() => runAgent(state, active, event), 80)
}

export function handleLiveVariantsMock(req: IncomingMessage, res: ServerResponse): boolean {
  const url = new URL(req.url || '/', 'http://e2e.local')
  const drawer = url.pathname.match(/^\/embed\/runs\/live-e2e-([^/]+)\/nodes\/preview\/chat$/)
  if (drawer) {
    req.url = (drawer[1].startsWith('entry-') ? '/live-entry-chat.html' : '/live-variants-drawer.html') + '?key=' + encodeURIComponent(drawer[1])
    return false
  }
  if (url.pathname === '/__grasp/embed-origin' && url.searchParams.get('ticket')?.startsWith('live-e2e-')) {
    json(res, { origin: `http://${req.headers.host}`, runId: url.searchParams.get('ticket'), nodeId: 'preview' })
    return true
  }
  if (handleLiveEntryApi(req, res, url)) return true
  if (!url.pathname.startsWith('/__e2e/live/')) return false
  const key = url.searchParams.get('key') || 'default'
  if (url.pathname === '/__e2e/live/reset') {
    states.delete(key)
    const state = stateFor(key)
    state.node = { type: 'agent', caps: LIVE_CAPS[url.searchParams.get('caps') || 'clarify'] || LIVE_CAPS.clarify }
    if (url.searchParams.get('permission') === 'react_only') state.permissionPreset = 'react_only'
    json(res, publicState(state))
    return true
  }
  const state = stateFor(key)
  if (url.pathname === '/__e2e/live/state') {
    json(res, publicState(state))
    return true
  }
  if (url.pathname === '/__e2e/live/reply' && req.method === 'POST') {
    void readReply(req).then((reply) => processReply(state, reply, res)).catch((error: unknown) => json(res, { error: String(error) }, 400))
    return true
  }
  json(res, { error: 'unknown Live fixture route' }, 404)
  return true
}

// HTTP surrogate mirrors models.LiveVariantsEnabled: an interactive Agent
// (clarify, or auto + review) that may register previews (set_preview).
function entryEnabled(state: State): boolean {
  const caps = state.node.caps
  const interactive = caps.interaction === 'clarify' || !!caps.review
  return interactive && !!caps.tools?.includes('set_preview')
}
function entryKey(token: unknown): string | null {
  return typeof token === 'string' && token.startsWith('live-e2e-entry-') ? token.slice('live-e2e-'.length) : null
}
function handleLiveEntryApi(req: IncomingMessage, res: ServerResponse, url: URL): boolean {
  if (url.pathname === '/embed-api/session' && req.method === 'POST') {
    void readReply(req).then((body) => {
      const ticket = (body as unknown as { ticket: string }).ticket
      const key = entryKey(ticket)
      if (!key) return json(res, { error: 'unknown ticket' }, 403)
      json(res, { token: ticket, runId: ticket, nodeId: 'preview', expiresAt: new Date(Date.now() + 3600_000).toISOString() })
    })
    return true
  }
  if (!url.pathname.startsWith('/public/gate-approvals/')) return false
  if (url.pathname === '/public/gate-approvals/reply' && req.method === 'POST') {
    void readReply(req).then((reply) => {
      const key = entryKey(reply.token)
      if (!key) return json(res, { error: 'unknown token' }, 403)
      const state = stateFor(key)
      if (reply.live && (!entryEnabled(state) || state.permissionPreset === 'react_only')) return json(res, { error: 'Live disabled' }, 403)
      processReply(state, reply, res)
    })
    return true
  }
  const key = entryKey(req.headers['x-gate-share-token'])
  if (!key) return false
  const state = stateFor(key)
  if (url.pathname.endsWith('/live-sessions')) {
    json(res, { status: 'active', enabled: entryEnabled(state), sessions: publicState(state).sessions })
    return true
  }
  if (url.pathname.endsWith('/preview')) {
    json(res, {
      status: 'active', kind: 'review', nodeType: state.node.type, interaction: state.node.caps.interaction, title: 'Live direct preview',
      remainingSec: 3600, nonce: 'live-entry', permissionPreset: state.permissionPreset || 'full', reactSessionAlive: true,
      sessionBusy: false, waiting: 0, queueItems: [], actions: { reply: 'reply', confirm: 'confirm' },
      turns: [
        { role: 'agent', text: '应用已启动，可在预览中使用 Live。', at: '2026-09-30T00:00:00Z' },
        ...state.messages.map((message) => ({ ...message, role: 'human', at: '2026-09-30T00:00:00Z' })),
      ],
    })
    return true
  }
  if (url.pathname.endsWith('/artifacts')) {
    json(res, { status: 'active', artifacts: [], nodes: [] })
    return true
  }
  return false
}

export function handleLiveEntryUpgrade(req: IncomingMessage, socket: Duplex, head: Buffer, wss: WebSocketServer): boolean {
  if (req.url !== '/public/gate-approvals/events') return false
  wss.handleUpgrade(req, socket, head, (ws) => {
    let key: string | null = null
    const send = (frame: unknown) => { if (ws.readyState === 1) ws.send(JSON.stringify(frame)) }
    ws.on('message', (raw) => {
      const message = JSON.parse(raw.toString()) as { token?: string }
      if (!message.token) return
      key = entryKey(message.token)
      if (!key) return
      if (!listeners.has(key)) listeners.set(key, new Set())
      listeners.get(key)!.add(send)
      send({ type: 'ready' })
    })
    ws.on('close', () => { if (key) listeners.get(key)?.delete(send) })
  })
  return true
}
