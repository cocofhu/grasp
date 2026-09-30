import type { IncomingMessage, ServerResponse } from 'node:http'

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
type Reply = { live?: Event; text?: string; liveCtx?: { sid: string; current: number; params?: Record<string, unknown> } }
type State = {
  source: string
  revision: number
  sessions: Session[]
  messages: Array<{ id: string; text: string; live: { sid: string; op: string; variant?: number } }>
  requests: Reply[]
}

export const LIVE_ORIGINAL = '<section id="newsletter" class="newsletter"><h2>Newsletter original</h2><p>Monthly design notes</p><button type="button" data-testid="newsletter-action">Subscribe</button></section>'
const states = new Map<string, State>()
const labels = ['层级', '紧凑', '强调色', '分栏']
const params = JSON.stringify([
  { id: 'gap', kind: 'range', min: 8, max: 48, step: 4, default: 24, unit: 'px', label: '间距' },
  { id: 'tone', kind: 'steps', default: 'soft', options: [{ value: 'soft', label: '柔和' }, { value: 'strong', label: '强烈' }], label: '强调' },
])

function stateFor(key: string): State {
  let state = states.get(key)
  if (!state) {
    state = { source: LIVE_ORIGINAL, revision: 0, sessions: [], messages: [], requests: [] }
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

export function handleLiveVariantsMock(req: IncomingMessage, res: ServerResponse): boolean {
  const url = new URL(req.url || '/', 'http://e2e.local')
  const drawer = url.pathname.match(/^\/embed\/runs\/live-e2e-([^/]+)\/nodes\/preview\/chat$/)
  if (drawer) {
    req.url = '/live-variants-drawer.html?key=' + encodeURIComponent(drawer[1])
    return false
  }
  if (url.pathname === '/__grasp/embed-origin' && url.searchParams.get('ticket')?.startsWith('live-e2e-')) {
    json(res, { origin: `http://${req.headers.host}`, runId: url.searchParams.get('ticket'), nodeId: 'preview' })
    return true
  }
  if (!url.pathname.startsWith('/__e2e/live/')) return false
  const key = url.searchParams.get('key') || 'default'
  if (url.pathname === '/__e2e/live/reset') {
    states.delete(key)
    json(res, publicState(stateFor(key)))
    return true
  }
  const state = stateFor(key)
  if (url.pathname === '/__e2e/live/state') {
    json(res, publicState(state))
    return true
  }
  if (url.pathname === '/__e2e/live/reply' && req.method === 'POST') {
    void readReply(req).then((reply) => {
      state.requests.push(reply)
      let ev = reply.live
      if (!ev && reply.liveCtx) {
        // Stand-in agent interprets the chat. The actual server's state and
        // permission validation is exercised separately by engine/handler tests.
        ev = { op: reply.text?.includes('就用这个') ? 'accept' : 'refine', sid: reply.liveCtx.sid, variant: reply.liveCtx.current, params: reply.liveCtx.params }
      }
      if (!ev) return json(res, { error: 'Live request required' }, 400)
      const event = ev
      let session = state.sessions.find((s) => s.sid === event.sid)
      if (!session) {
        session = {
          sid: event.sid, mode: event.op === 'steer' ? 'steer' : event.op === 'insert' ? 'insert' : 'replace',
          state: 'generating', prompt: event.prompt, selector: event.element?.selector,
          summary: event.element?.text, url: event.url, updatedAt: new Date().toISOString(), original: state.source, attempts: 0,
        }
        state.sessions.push(session)
      }
      const active = session
      const next = event.op === 'accept' ? 'accepting' : event.op === 'discard' ? 'discarding' : event.op === 'refine' || event.op === 'mount_failed' ? 'refining' : 'generating'
      update(state, active, next)
      state.messages.push({ id: String(state.messages.length + 1), text: reply.text || event.prompt || event.op, live: { sid: event.sid, op: event.op, variant: event.variant } })
      json(res, { status: 'accepted', live: publicState(state).sessions.find((s) => s.sid === active.sid) })
      setTimeout(() => runAgent(state, active, event), 80)
    }).catch((error: unknown) => json(res, { error: String(error) }, 400))
    return true
  }
  json(res, { error: 'unknown Live fixture route' }, 404)
  return true
}
