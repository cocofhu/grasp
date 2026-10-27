import { contentText, flattenUpdate, normalizeKind } from '@/lib/run/acpUnpack'
import { acpFailureText, appendAcpError } from '@/lib/run/acpFailure'
import type { AgentPart, ClarifyImage } from '@/lib/shared/types'
import type { SoloMessage } from '@/lib/api/apiTypes'

export type SoloTool = { id: string; title: string; status: string; summary?: string; input?: string; output?: string }
/** Arrival order of a reply: text runs, or an index into SoloTurn.tools. */
export type SoloStep = { kind: 'thought' | 'message'; text: string } | { kind: 'tool'; tool: number }

export type SoloTurn = {
  id: string
  role: 'user' | 'agent' | 'system'
  text: string
  tools: SoloTool[]
  steps: SoloStep[]
  plan: { content: string; status: string }[]
  streaming: boolean
  images?: ClarifyImage[]
  /** User turn: failed | stopped when its reply did not complete. */
  status?: string
  error?: string
  startedAt?: string
  endedAt?: string
  createdAt?: string
}

/** Localizes tool names; returns '' when the key is unknown. */
export type ToolLabeler = (key: string) => string

const SUMMARY_KEYS = ['command', 'cmd', 'file_path', 'filePath', 'path', 'target_file', 'url', 'uri', 'pattern', 'glob_pattern', 'query', 'search_term', 'name', 'description']
const TOOL_DETAIL_MAX = 2000
const SECRET_KEY_RE = /token|secret|password|passwd|api[_-]?key|authorization|cookie|credential/i

export function emptyAgentTurn(id: string, streaming: boolean, startedAt?: string): SoloTurn {
  return { id, role: 'agent', text: '', tools: [], steps: [], plan: [], streaming, startedAt }
}

function field(o: any, ...keys: string[]): string {
  for (const k of keys) if (o && typeof o[k] === 'string' && o[k]) return o[k]
  return ''
}

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

function humanizeTool(raw: string, label: ToolLabeler): string {
  const s = String(raw || '').trim()
  if (!s || /^tool_[0-9a-f-]{8,}$/i.test(s)) return ''
  if (/[\u4e00-\u9fff]/.test(s)) return s
  const localized = label(s.toLowerCase().replace(/\s+/g, '_').replace(/-/g, '_'))
  if (localized) return localized
  return s.replace(/_/g, ' ').replace(/\s+/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

function addStep(turn: SoloTurn, kind: 'thought' | 'message', s: string) {
  if (!s) return
  const last = turn.steps[turn.steps.length - 1]
  if (last && last.kind === kind) last.text += s
  else turn.steps.push({ kind, text: s })
}

function applyTool(u: any, turn: SoloTurn, label: ToolLabeler, fallback: string) {
  const tc = (u.toolCall || u.tool || u.call || {}) as any
  const id = field(u, 'toolCallId', 'tool_call_id', 'id', 'callId') || field(tc, 'toolCallId', 'id', 'callId')
  const title = humanizeTool(field(u, 'title', 'name', 'toolName', 'tool_name', 'kind') || field(tc, 'title', 'name', 'toolName', 'kind'), label)
  const stat = field(u, 'status', 'state') || field(tc, 'status', 'state')
  const rawIn = u.rawInput ?? u.raw_input ?? u.input ?? tc.rawInput ?? tc.input
  const rawOut = u.rawOutput ?? u.raw_output ?? u.output ?? tc.rawOutput ?? tc.output
  const existing = id ? turn.tools.find((t) => t.id === id) : undefined
  const tool = existing ?? { id, title: title || fallback, status: stat || 'pending' }
  if (existing) {
    if (title) existing.title = title
    if (stat) existing.status = stat
  } else {
    turn.tools.push(tool)
    turn.steps.push({ kind: 'tool', tool: turn.tools.length - 1 })
  }
  if (rawIn !== undefined) {
    tool.summary = toolSummary(rawIn) || undefined
    tool.input = toolDetail(rawIn, false) || undefined
  }
  if (rawOut !== undefined) tool.output = toolDetail(rawOut, true) || undefined
}

/** Folds one acp-bridge frame ({op:"event",data:{type,update}}) into a reply turn. */
export function applyAcp(envelope: any, turn: SoloTurn, label: ToolLabeler, toolFallback: string) {
  const failure = acpFailureText(envelope)
  if (failure !== null) {
    turn.status = 'failed'
    turn.error = appendAcpError(turn.error, failure)
    return
  }
  const ev = envelope?.data ?? envelope
  if (!ev || ev.type !== 'session_update' || !ev.update) return
  const u = flattenUpdate(ev.update)
  const kind = normalizeKind(u.sessionUpdate || u.session_update || u.type || u.kind || '')
  if (kind === 'agent_message_chunk') {
    const s = contentText(u.content)
    turn.text += s
    addStep(turn, 'message', s)
  } else if (kind === 'agent_thought_chunk') {
    addStep(turn, 'thought', contentText(u.content))
  } else if (kind === 'plan') {
    const raw = u.entries || u.steps || []
    turn.plan = Array.isArray(raw)
      ? raw
          .map((e: any) => ({ content: field(e, 'content', 'title', 'text'), status: field(e, 'status', 'state') || 'pending' }))
          .filter((e: { content: string }) => e.content)
      : []
  } else if (kind.includes('tool_call') || kind.includes('toolcall')) {
    applyTool(u, turn, label, toolFallback)
  }
}

/** Timeline parts of a reply; tool steps read the live tool row. */
export function partsOf(turn: SoloTurn): AgentPart[] {
  return turn.steps.map((st) => {
    if (st.kind !== 'tool') return { kind: st.kind, text: st.text }
    const tool = turn.tools[st.tool]
    return tool
      ? { kind: 'tool', title: tool.title, status: tool.status, summary: tool.summary, input: tool.input, output: tool.output }
      : { kind: 'tool', title: '' }
  })
}

function addMs(iso: string, ms?: number): string | undefined {
  const t = Date.parse(iso)
  if (!Number.isFinite(t) || !ms) return undefined
  return new Date(t - ms).toISOString()
}

/** Rebuilds the transcript from persisted messages. */
export function turnsFromMessages(messages: SoloMessage[], label: ToolLabeler, toolFallback: string): SoloTurn[] {
  const out: SoloTurn[] = []
  for (const m of messages) {
    if (m.role === 'user') {
      out.push({
        id: `u:${m.id}`,
        role: 'user',
        text: m.content,
        tools: [],
        steps: [],
        plan: [],
        streaming: false,
        images: m.images?.length ? m.images : undefined,
        status: m.status || undefined,
        createdAt: m.createdAt,
      })
    } else if (m.role === 'system') {
      out.push({ id: `s:${m.id}`, role: 'system', text: m.content, tools: [], steps: [], plan: [], streaming: false, createdAt: m.createdAt })
    } else {
      const turn = emptyAgentTurn(`a:${m.id}`, false, addMs(m.createdAt, m.durationMs))
      turn.endedAt = m.createdAt
      turn.createdAt = m.createdAt
      for (const ev of m.events ?? []) applyAcp(ev, turn, label, toolFallback)
      if (!turn.steps.length && m.content) {
        turn.text = m.content
        turn.steps.push({ kind: 'message', text: m.content })
      }
      if (m.status && m.status !== 'ok') turn.status = m.status
      if (m.error) turn.error = m.error
      out.push(turn)
    }
  }
  return out
}
