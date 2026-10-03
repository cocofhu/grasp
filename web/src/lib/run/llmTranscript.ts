import type {
  AcpEvent,
  AgentPart,
  ClarifyImage,
  McpCall,
  NodeRunStatus,
  Run,
  StateTraceEntry,
  TokenUsage,
} from '@/lib/shared/types'
import type { LlmInflightPrompt, LlmTranscriptExecution, LlmTranscriptResponse } from '@/lib/api/apiTypes'
import { tokenUsageTotal } from '@/lib/run/tokenUsage'

/** Where a Q came from: the node's first instruction, a human reply, or a platform follow-up. */
export type LlmPromptSource = 'instruction' | 'human' | 'followup'

export interface LlmPrompt {
  text: string
  truncated: boolean
  at?: string
  imageCount: number
  images?: ClarifyImage[]
  source: LlmPromptSource
}

export interface LlmToolStep {
  title: string
  status?: string
  artifact?: { name: string; kind: string }
}

/** One agent bubble. A turn has several when the agent sealed a segment. */
export interface LlmAnswer {
  thought?: string
  text?: string
  plan?: string
  tools: LlmToolStep[]
  /** Thought / tool / message steps in order (kind=timeline); rendered instead of the rails. */
  parts?: AgentPart[]
}

export interface LlmTurn {
  /** Missing on executions recorded before prompts were persisted. */
  prompt?: LlmPrompt
  answers: LlmAnswer[]
  mcpCalls: McpCall[]
  usage?: TokenUsage | null
  endedAt?: string
  failed: boolean
  error?: string
  /** Still streaming (prompt from inflight, answers from the live WS rail). */
  live: boolean
}

export interface LlmNodeInfo {
  label: string
  type: string
}

export type LlmTranscriptItem =
  | {
      type: 'node'
      key: string
      nodeId: string
      execIdx: number
      label: string
      nodeType: string
      iteration: number
      status: NodeRunStatus
      startedAt?: string
      durationSec?: number
      usage?: TokenUsage | null
      models: string[]
      turnCount: number
    }
  | { type: 'turn'; key: string; nodeId: string; execIdx: number; turn: LlmTurn; models: string[] }
  | { type: 'legacy'; key: string; nodeId: string; execIdx: number }
  | { type: 'error'; key: string; nodeId: string; execIdx: number; text: string }
  | {
      type: 'trace'
      key: string
      nodeId: string
      label: string
      event: StateTraceEntry['event'] | string
      at: string
      to?: string
      toLabel?: string
      detail?: string
      edgeKind?: string
    }

export interface BuildLlmTranscriptInput {
  run: Run
  transcript: LlmTranscriptResponse | null
  liveEvents?: Record<string, AcpEvent[] | undefined>
  nodeInfo: (nodeId: string) => LlmNodeInfo
}

/** Local wall-clock HH:MM:SS (24h), matching the run header's fmtTime style. */
export function fmtClock(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function ms(iso?: string): number {
  if (!iso) return Number.POSITIVE_INFINITY
  const v = Date.parse(iso)
  return Number.isNaN(v) ? Number.POSITIVE_INFINITY : v
}

function emptyAnswer(): LlmAnswer {
  return { tools: [] }
}

function answerHasContent(a: LlmAnswer): boolean {
  return !!(a.thought || a.text || a.plan || a.tools.length || a.parts?.length)
}

function joinText(prev: string | undefined, next: string): string {
  return prev ? `${prev}\n\n${next}` : next
}

function applyReplyEvent(answers: LlmAnswer[], ev: AcpEvent) {
  let cur = answers[answers.length - 1]
  if (!cur) {
    cur = emptyAnswer()
    answers.push(cur)
  }
  switch (ev.kind) {
    case 'segment':
      if (answerHasContent(cur)) answers.push(emptyAnswer())
      return
    case 'thought':
      if (ev.text) cur.thought = joinText(cur.thought, ev.text)
      return
    case 'message': {
      const text = ev.title && ev.text ? `**${ev.title}**\n\n${ev.text}` : ev.text || ev.title
      if (text) cur.text = joinText(cur.text, text)
      return
    }
    case 'plan':
      if (ev.text) cur.plan = ev.text
      return
    case 'timeline':
      if (ev.parts?.length) cur.parts = ev.parts
      return
    case 'tool_call':
      cur.tools.push({
        title: ev.title || ev.text || 'tool',
        status: ev.status,
        artifact: ev.artifact ? { name: ev.artifact.name, kind: ev.artifact.kind } : undefined,
      })
      return
    default:
  }
}

function newTurn(prompt?: LlmPrompt): LlmTurn {
  return { prompt, answers: [], mcpCalls: [], failed: false, live: false }
}

function finalizeAnswers(turn: LlmTurn) {
  turn.answers = turn.answers.filter(answerHasContent)
}

function promptFrom(ev: { text?: string; truncated?: boolean; at?: string; imageCount?: number }): LlmPrompt {
  return {
    text: ev.text || '',
    truncated: !!ev.truncated,
    at: ev.at,
    imageCount: ev.imageCount || 0,
    source: 'followup',
  }
}

function imageCountFromTitle(title?: string): number {
  const n = parseInt(title || '', 10)
  return Number.isFinite(n) ? n : 0
}

/** Split one execution's event log into prompt → reply turns. */
export function splitTurns(events: AcpEvent[] | null | undefined): LlmTurn[] {
  const turns: LlmTurn[] = []
  let cur: LlmTurn | null = null
  const close = () => {
    if (!cur) return
    finalizeAnswers(cur)
    turns.push(cur)
    cur = null
  }
  for (const ev of events || []) {
    if (ev.kind === 'prompt') {
      close()
      cur = newTurn(promptFrom({ ...ev, imageCount: imageCountFromTitle(ev.title) }))
      continue
    }
    if (ev.kind === 'turn_end') {
      if (!cur) cur = newTurn()
      cur.usage = ev.usage
      cur.endedAt = ev.at
      if (ev.status === 'failed') {
        cur.failed = true
        cur.error = ev.text
      }
      close()
      continue
    }
    if (ev.kind === 'commands') continue
    if (!cur) cur = newTurn()
    applyReplyEvent(cur.answers, ev)
  }
  close()
  return turns.filter((t) => t.prompt || t.answers.length || t.failed)
}

/** Attach each MCP call to the last turn that started at or before it. */
function assignMcpCalls(turns: LlmTurn[], calls: McpCall[] | null | undefined) {
  if (!calls?.length) return
  if (!turns.length) turns.push(newTurn())
  for (const c of calls) {
    const at = ms(c.at)
    let target = turns[0]!
    for (const t of turns) {
      if (ms(t.prompt?.at) <= at) target = t
    }
    target.mcpCalls.push(c)
  }
}

function normalize(s: string): string {
  return s.replace(/\s+/g, ' ').trim()
}

function labelPromptSources(turns: LlmTurn[], humanTurns: { text: string; images?: ClarifyImage[] }[]) {
  let first = true
  for (const t of turns) {
    if (!t.prompt) continue
    const text = normalize(t.prompt.text)
    const human = text
      ? humanTurns.find((h) => {
          const ht = normalize(h.text)
          return ht.length > 0 && (ht === text || (t.prompt!.truncated && ht.startsWith(text)))
        })
      : undefined
    if (human) {
      t.prompt.source = 'human'
      if (human.images?.length) t.prompt.images = human.images
    } else {
      t.prompt.source = first ? 'instruction' : 'followup'
    }
    first = false
  }
}

function modelsOf(ex: LlmTranscriptExecution): string[] {
  return Object.keys(ex.usageByModel || {}).filter((k) => k.trim() !== '')
}

/** Executions from run detail (prompts are previews) until the transcript loads. */
export function executionsFromRun(run: Run): LlmTranscriptExecution[] {
  const out: LlmTranscriptExecution[] = []
  const execs = run.nodeExecutions || {}
  const ids = new Set([...Object.keys(execs), ...Object.keys(run.nodeRuns || {})])
  for (const nodeId of ids) {
    const list = execs[nodeId]?.length ? execs[nodeId]! : run.nodeRuns?.[nodeId] ? [run.nodeRuns[nodeId]!] : []
    list.forEach((nr, i) => {
      out.push({
        id: 0,
        nodeId,
        iteration: nr.iteration || i + 1,
        status: nr.status,
        startedAt: nr.startedAt,
        durationSec: nr.durationSec,
        events: nr.events,
        mcpCalls: nr.mcpCalls,
        usage: nr.usage,
        usageByModel: nr.usageByModel,
        error: nr.error,
      })
    })
  }
  return out.sort((a, b) => ms(a.startedAt) - ms(b.startedAt))
}

/**
 * The server stops reporting a turn as inflight the moment it finishes, but
 * the fetched executions only contain that turn on a later poll. Keep the
 * previous inflight prompt of an active execution until its turn shows up,
 * so the just-streamed answer does not blink out in between.
 */
export function carryInflight(
  prev: LlmTranscriptResponse | null,
  next: LlmTranscriptResponse,
): LlmTranscriptResponse {
  const carried: Record<string, LlmInflightPrompt> = {}
  for (const [nodeId, p] of Object.entries(prev?.inflight || {})) {
    if (next.inflight?.[nodeId]) continue
    const latest = [...(next.executions || [])].reverse().find((ex) => ex.nodeId === nodeId)
    if (!latest || (latest.status !== 'running' && latest.status !== 'waiting_human')) continue
    if ((latest.events || []).some((ev) => ev.kind === 'prompt' && ev.at === p.at)) continue
    carried[nodeId] = p
  }
  if (!Object.keys(carried).length) return next
  return { ...next, inflight: { ...carried, ...(next.inflight || {}) } }
}

function liveTurn(inflight: LlmInflightPrompt, live: AcpEvent[] | undefined): LlmTurn {
  const turn = newTurn(promptFrom({ text: inflight.prompt, at: inflight.at, imageCount: inflight.imageCount }))
  turn.live = true
  for (const ev of live || []) {
    if (ev.kind === 'prompt' || ev.kind === 'turn_end' || ev.kind === 'commands') continue
    applyReplyEvent(turn.answers, ev)
  }
  finalizeAnswers(turn)
  return turn
}

interface Sortable {
  t: number
  seq: number
  item: LlmTranscriptItem
}

// Trace timestamps are second-precision, so a transition can tie with the
// start of the node it leads to; events that close a node sort first.
function tieRank(item: LlmTranscriptItem): number {
  if (item.type === 'error') return 0
  if (item.type !== 'trace') return 1
  return item.event === 'transition' || item.event === 'rollback' || item.event === 'exit' ? 0 : 2
}

/**
 * Flatten a run into one chronological chat: a divider per node execution, its
 * prompt → reply turns, and the FSM transitions / human pauses between them.
 */
export function buildLlmTranscript(input: BuildLlmTranscriptInput): LlmTranscriptItem[] {
  const { run, transcript, liveEvents, nodeInfo } = input
  const executions = transcript?.executions?.length ? transcript.executions : executionsFromRun(run)
  const inflight = transcript?.inflight || {}
  const sortable: Sortable[] = []
  let seq = 0
  const push = (t: number, item: LlmTranscriptItem) => sortable.push({ t, seq: seq++, item })

  const execCount: Record<string, number> = {}
  const latestIdx: Record<string, number> = {}
  for (const ex of executions) latestIdx[ex.nodeId] = (latestIdx[ex.nodeId] ?? -1) + 1

  for (const ex of executions) {
    const execIdx = execCount[ex.nodeId] ?? 0
    execCount[ex.nodeId] = execIdx + 1
    const info = nodeInfo(ex.nodeId)
    const models = modelsOf(ex)
    const turns = splitTurns(ex.events)
    assignMcpCalls(turns, ex.mcpCalls)

    const pending = inflight[ex.nodeId]
    if (pending && execIdx === latestIdx[ex.nodeId] && (ex.status === 'running' || ex.status === 'waiting_human')) {
      turns.push(liveTurn(pending, liveEvents?.[ex.nodeId]))
    }
    const human = (run.clarifyByNode?.[ex.nodeId]?.turns || []).filter((t) => t.role === 'human')
    labelPromptSources(turns, human)

    const start = ms(ex.startedAt)
    const keyBase = `${ex.nodeId}:${execIdx}`
    push(start, {
      type: 'node',
      key: `node:${keyBase}`,
      nodeId: ex.nodeId,
      execIdx,
      label: info.label,
      nodeType: ex.nodeType || info.type,
      iteration: ex.iteration || execIdx + 1,
      status: ex.status,
      startedAt: ex.startedAt,
      durationSec: ex.durationSec,
      usage: ex.usage,
      models,
      turnCount: turns.filter((t) => t.prompt).length,
    })
    const hasEvents = !!ex.events?.some((e) => e.kind !== 'commands')
    if (hasEvents && !turns.some((t) => t.prompt)) {
      push(start, { type: 'legacy', key: `legacy:${keyBase}`, nodeId: ex.nodeId, execIdx })
    }
    let last = start
    turns.forEach((turn, i) => {
      const pt = ms(turn.prompt?.at)
      const t = Number.isFinite(start) && Number.isFinite(pt) ? Math.max(pt, start) : start
      last = Math.max(t, last)
      push(last, { type: 'turn', key: `turn:${keyBase}:${i}`, nodeId: ex.nodeId, execIdx, turn, models })
    })
    if (ex.error) {
      const end = Number.isFinite(start) && ex.durationSec != null ? start + ex.durationSec * 1000 : last
      push(Math.max(end, last), { type: 'error', key: `error:${keyBase}`, nodeId: ex.nodeId, execIdx, text: ex.error })
    }
  }

  ;(run.trace || []).forEach((tr, i) => {
    const event = String(tr.event)
    if (event === 'enter' || (event === 'exit' && !tr.detail)) return
    push(ms(tr.at), {
      type: 'trace',
      key: `trace:${i}`,
      nodeId: tr.nodeId,
      label: nodeInfo(tr.nodeId).label,
      event,
      at: tr.at,
      to: tr.to,
      toLabel: tr.to ? nodeInfo(tr.to).label : undefined,
      detail: tr.detail,
      edgeKind: tr.kind,
    })
  })

  sortable.sort((a, b) => a.t - b.t || tieRank(a.item) - tieRank(b.item) || a.seq - b.seq)
  return sortable.map((s) => s.item)
}

export interface LlmTranscriptSummary {
  turns: number
  nodes: number
  totalTokens: number | null
}

export function summarizeLlmTranscript(items: LlmTranscriptItem[]): LlmTranscriptSummary {
  let turns = 0
  let nodes = 0
  let total: number | null = null
  for (const it of items) {
    if (it.type === 'turn' && it.turn.prompt) turns++
    if (it.type === 'node') {
      nodes++
      if (it.usage) total = (total ?? 0) + tokenUsageTotal(it.usage)
    }
  }
  return { turns, nodes, totalTokens: total }
}
