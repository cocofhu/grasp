import { contentText, flattenUpdate, normalizeKind, steerText } from '@/lib/run/acpUnpack'
import { acpFailureText, appendAcpError } from '@/lib/run/acpFailure'
import { contextSlashCommand, type ContextCommand } from '@/lib/chat/chatCommands'
import type { AgentPart, AgentTool } from '@/lib/shared/types'

/** A reply's tool row: the shared tool shape plus the id its updates key on. */
type ReplyTool = AgentTool & { id: string; status: string }
/** Arrival order of a reply: text runs, steers, or an index into AcpReply.tools. */
export type ReplyStep = { kind: 'thought' | 'message' | 'steer'; text: string; imageURLs?: string[] } | { kind: 'tool'; tool: number }
type PlanEntry = { content: string; status: string }

/** The reply state one agent turn folds its acp-bridge frames into. */
export type AcpReply = {
  text: string
  thought?: string
  tools: ReplyTool[]
  steps?: ReplyStep[]
  plan: PlanEntry[]
  contextCommand?: ContextCommand
  status?: string
  error?: string
  /** A tool, thought or steer arrived since the last reply text, which therefore starts a new paragraph. A tool update changes its row in place, so steps alone cannot tell. */
  paragraph?: boolean
}

/** Localizes tool names; returns '' when the key is unknown. */
export type ToolLabeler = (key: string) => string

const SUMMARY_KEYS = ['command', 'cmd', 'file_path', 'filePath', 'path', 'target_file', 'url', 'uri', 'pattern', 'glob_pattern', 'query', 'search_term', 'name', 'description']
const TOOL_DETAIL_MAX = 2000
const SECRET_KEY_RE = /token|secret|password|passwd|api[_-]?key|authorization|cookie|credential/i

function field(o: any, ...keys: string[]): string {
  for (const k of keys) if (o && typeof o[k] === 'string' && o[k]) return o[k]
  return ''
}

function pick(o: any, ...keys: string[]): unknown {
  for (const k of keys) if (o && o[k] !== undefined) return o[k]
  return undefined
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
  // Agents like Codex title a call with the raw command or path; keep those verbatim.
  if (!/^[A-Za-z][\w -]*$/.test(s)) return s
  return s.replace(/_/g, ' ').replace(/\s+/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

function addStep(turn: AcpReply, kind: 'thought' | 'message', s: string) {
  if (!s) return
  const steps = (turn.steps ??= [])
  const last = steps[steps.length - 1]
  if (last && last.kind === kind && !(kind === 'message' && turn.paragraph)) last.text += s
  else steps.push({ kind, text: s })
}

/** Reply text that resumes after a tool call, thought or steer starts a new paragraph, matching the server's narration. */
function addText(turn: AcpReply, s: string) {
  if (!s) return
  if (turn.paragraph && turn.text) turn.text = `${turn.text.replace(/\n+$/, '')}\n\n`
  turn.text += s
  addStep(turn, 'message', s)
  turn.paragraph = false
}

function applyTool(u: any, turn: AcpReply, label: ToolLabeler, fallback: string) {
  const tc = (u.toolCall || u.tool || u.call || {}) as any
  const id = field(u, 'toolCallId', 'tool_call_id', 'id', 'callId') || field(tc, 'toolCallId', 'id', 'callId')
  const title = humanizeTool(field(u, 'title', 'name', 'toolName', 'tool_name', 'kind') || field(tc, 'title', 'name', 'toolName', 'kind'), label)
  const stat = field(u, 'status', 'state') || field(tc, 'status', 'state')
  const rawIn = pick(u, 'rawInput', 'raw_input', 'input', 'arguments', 'args') ?? pick(tc, 'rawInput', 'input')
  const rawOut = pick(u, 'rawOutput', 'raw_output', 'output', 'result', 'content') ?? pick(tc, 'rawOutput', 'output')
  const existing = id ? turn.tools.find((t) => t.id === id) : undefined
  const tool = existing ?? { id, title: title || fallback, status: stat || 'pending' }
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
  turn.paragraph = true
}

/** Folds one acp-bridge frame ({op:"event",data:{...}}, its data, or an error frame) into a reply. */
export function applyAcp(envelope: any, turn: AcpReply, label: ToolLabeler, toolFallback: string) {
  const failure = acpFailureText(envelope)
  if (failure !== null) {
    turn.status = 'failed'
    turn.error = appendAcpError(turn.error, failure)
    return
  }
  const ev = envelope?.data ?? envelope
  if (ev?.type === 'context_command') {
    turn.contextCommand = contextSlashCommand(String(ev.command ?? ''))
    return
  }
  if (ev?.type === 'user_steer') {
    // Each steer is its own user message; never merge it into a neighbour.
    const text = steerText(ev)
    const imageURLs = Array.isArray(ev.imageURLs)
      ? ev.imageURLs.filter((url: unknown): url is string => typeof url === 'string' && !!url.trim())
      : []
    if (text || imageURLs.length) {
      ;(turn.steps ??= []).push({ kind: 'steer', text, ...(imageURLs.length ? { imageURLs } : {}) })
      turn.paragraph = true
    }
    return
  }
  if (!ev || ev.type !== 'session_update' || !ev.update) return
  const u = flattenUpdate(ev.update)
  const kind = normalizeKind(u.sessionUpdate || u.session_update || u.type || u.kind || '')
  if (kind === 'agent_message_chunk') {
    addText(turn, contentText(u.content))
  } else if (kind === 'agent_thought_chunk') {
    const s = contentText(u.content)
    if (s) {
      turn.thought = (turn.thought ?? '') + s
      turn.paragraph = true
    }
    addStep(turn, 'thought', s)
  } else if (kind === 'plan') {
    const raw = u.entries || u.steps || []
    turn.plan = Array.isArray(raw)
      ? raw
          .map((e: any) => ({ content: field(e, 'content', 'title', 'text'), status: field(e, 'status', 'state') || 'pending' }))
          .filter((e: PlanEntry) => e.content)
      : []
  } else if (kind.includes('tool_call') || kind.includes('toolcall')) {
    applyTool(u, turn, label, toolFallback)
  }
}

/** Timeline parts of a reply; tool steps read the live tool row. */
export function partsOf(turn: AcpReply): AgentPart[] {
  return (turn.steps ?? []).map((st) => {
    if (st.kind !== 'tool') return { kind: st.kind, text: st.text, imageURLs: st.imageURLs }
    const tool = turn.tools[st.tool]
    return tool
      ? { kind: 'tool', title: tool.title, status: tool.status, summary: tool.summary, input: tool.input, output: tool.output }
      : { kind: 'tool', title: '' }
  })
}
