import type { ClarifyPriorSegment } from '@/components/run/ClarifyChat.vue'
import type { LlmAnswer, LlmTranscriptItem, LlmTurn } from '@/lib/run/llmTranscript'
import { isObservableNode, recentCurrentNode } from '@/lib/run/recentRuns'
import type { AgentPart, ClarifyTurn, Run } from '@/lib/shared/types'

export interface LlmThreadCurrent {
  nodeId: string
  iteration: number
  label: string
  kind: ClarifyPriorSegment['kind']
  status: string
}

export interface LlmThreadModel {
  priors: ClarifyPriorSegment[]
  currentTurns: ClarifyTurn[]
  current: LlmThreadCurrent | null
  currentNotes: LlmSystemNote[]
  conversational: boolean
}

type NodeItem = Extract<LlmTranscriptItem, { type: 'node' }>
type TraceItem = Extract<LlmTranscriptItem, { type: 'trace' }>

export interface LlmSystemNote {
  key: string
  event: string
  nodeLabel: string
  toLabel?: string
  edgeKind?: string
  /** Clarify-turn index this row follows. -1 sits after the node header, before the first turn. */
  afterTurn: number
}

const CLOSING_EVENTS = new Set(['exit', 'transition', 'rollback'])

function noteClosesVisit(note: { event: string; afterTurn?: number }, turnCount: number): boolean {
  if (!CLOSING_EVENTS.has(note.event)) return false
  return (note.afterTurn ?? -1) >= turnCount - 1
}

/** Trace rows that belong beside a turn: pause and resume, and anything that is not the visit's close. */
export function notesBesideTurn<T extends { event: string; afterTurn?: number }>(
  notes: T[] | undefined,
  turnIndex: number,
  turnCount: number,
): T[] {
  return (notes || []).filter((note) => (note.afterTurn ?? -1) === turnIndex && !noteClosesVisit(note, turnCount))
}

/** Exit, transition, and rollback that close the visit, drawn after its conversation. */
export function notesAfterVisit<T extends { event: string; afterTurn?: number }>(
  notes: T[] | undefined,
  turnCount: number,
): T[] {
  return (notes || []).filter((note) => noteClosesVisit(note, turnCount))
}

type NoteDraft = Omit<LlmSystemNote, 'afterTurn'>

type ExecEntry =
  | { type: 'turn'; turn: LlmTurn }
  | { type: 'error'; text: string }
  | { type: 'note'; note: NoteDraft }

interface ExecGroup {
  node: NodeItem
  entries: ExecEntry[]
}

function withMessage(parts: AgentPart[], text?: string): AgentPart[] {
  if (!text || parts.some((part) => part.kind === 'message' && part.text === text)) return parts
  return [...parts, { kind: 'message', text }]
}

function answerParts(answer: LlmAnswer): AgentPart[] {
  if (answer.parts?.length) return withMessage(answer.parts.map((part) => ({ ...part })), answer.text)
  const parts: AgentPart[] = []
  if (answer.thought) parts.push({ kind: 'thought', text: answer.thought })
  for (const tool of answer.tools) parts.push({ kind: 'tool', title: tool.title, status: tool.status })
  if (answer.plan) parts.push({ kind: 'message', text: answer.plan })
  if (answer.text) parts.push({ kind: 'message', text: answer.text })
  return parts
}

function endedFrom(startedAt: string | undefined, durationSec: number | undefined, explicit?: string): string | undefined {
  if (explicit) return explicit
  if (!startedAt || durationSec == null) return undefined
  const start = Date.parse(startedAt)
  if (!Number.isFinite(start)) return undefined
  return new Date(start + durationSec * 1000).toISOString()
}

/** Prompt → reply rows the conversation components already know how to draw. */
export function clarifyTurnsFromLlm(turns: LlmTurn[], error?: string): ClarifyTurn[] {
  const out: ClarifyTurn[] = []
  for (const turn of turns) {
    const prompt = turn.prompt
    if (prompt && (prompt.text || prompt.images?.length)) {
      out.push({
        role: 'human',
        text: prompt.text || '',
        at: prompt.at || '',
        ...(prompt.images?.length ? { images: prompt.images } : {}),
      })
    }
    const answers = turn.answers.length ? turn.answers : []
    if (!answers.length && (turn.live || turn.failed || turn.error)) {
      answers.push({ tools: [], text: turn.error || '', thought: undefined })
    }
    answers.forEach((answer, index) => {
      const last = index === answers.length - 1
      const streaming = !!turn.live && last
      const parts = answerParts(answer)
      const text = answer.text || (last && turn.failed ? turn.error || '' : '') || ''
      if (!streaming && !text && !answer.thought && !parts.length && !answer.tools.length) return
      const row: ClarifyTurn = {
        role: 'agent',
        text,
        at: (streaming ? prompt?.at : turn.endedAt || prompt?.at) || '',
        ...(answer.thought ? { thought: answer.thought } : {}),
        ...(parts.length ? { parts } : {}),
        ...(streaming ? { streaming: true } : {}),
      }
      if (!streaming && prompt?.at && turn.endedAt) {
        row.startedAt = prompt.at
        row.endedAt = turn.endedAt
      }
      out.push(row)
    })
  }
  if (error && !out.some((turn) => turn.role === 'agent' && turn.text === error)) {
    out.push({ role: 'agent', text: error, at: '', parts: [{ kind: 'message', text: error }] })
  }
  return out
}

function segmentKind(nodeType: string, turns: LlmTurn[]): ClarifyPriorSegment['kind'] {
  if (nodeType === 'human_gate') return 'gate'
  if (turns.some((turn) => turn.prompt?.source === 'human')) return 'chat'
  return 'auto'
}

function noteOf(item: TraceItem): NoteDraft {
  return {
    key: item.key,
    event: item.event,
    nodeLabel: item.label,
    toLabel: item.toLabel,
    edgeKind: item.edgeKind,
  }
}

function groupItems(items: LlmTranscriptItem[]): ExecGroup[] {
  const groups: ExecGroup[] = []
  let current: ExecGroup | null = null
  const hold: ExecEntry[] = []
  for (const item of items) {
    if (item.type === 'trace') {
      const entry: ExecEntry = { type: 'note', note: noteOf(item) }
      if (current) current.entries.push(entry)
      else hold.push(entry)
      continue
    }
    if (item.type === 'node') {
      current = { node: item, entries: hold.splice(0) }
      groups.push(current)
      continue
    }
    if (!current) continue
    if (item.type === 'turn') current.entries.push({ type: 'turn', turn: item.turn })
    else if (item.type === 'error') current.entries.push({ type: 'error', text: item.text })
  }
  return groups
}

/** Keep trace rows in the order they happened, slotted against the bubbles already drawn. */
function materialize(group: ExecGroup): { turns: ReturnType<typeof clarifyTurnsFromLlm>; notes: LlmSystemNote[]; llmTurns: LlmTurn[] } {
  const turns: ReturnType<typeof clarifyTurnsFromLlm> = []
  const notes: LlmSystemNote[] = []
  const llmTurns: LlmTurn[] = []
  for (const entry of group.entries) {
    if (entry.type === 'note') {
      notes.push({ ...entry.note, afterTurn: turns.length - 1 })
      continue
    }
    if (entry.type === 'error') {
      turns.push(...clarifyTurnsFromLlm([], entry.text))
      continue
    }
    llmTurns.push(entry.turn)
    turns.push(...clarifyTurnsFromLlm([entry.turn]))
  }
  return { turns, notes, llmTurns }
}

function toSegment(group: ExecGroup): ClarifyPriorSegment {
  const node = group.node
  const { turns, notes, llmTurns } = materialize(group)
  const kind = segmentKind(node.nodeType, llmTurns)
  return {
    nodeId: node.nodeId,
    iteration: node.iteration,
    label: node.label,
    kind,
    status: node.status,
    at: node.startedAt || '',
    endedAt: endedFrom(node.startedAt, node.durationSec),
    turns,
    open: false,
    notes,
  }
}

/**
 * The current node is one the conversation page would put a composer on:
 * clarify / review / takeover, waiting for the person or busy in a manual turn.
 * An auto node being watched is not that.
 */
export function llmNodeConversational(run: Run, nodeId: string, iteration: number): boolean {
  const current = recentCurrentNode(run)
  if (!current || current.id !== nodeId) return false
  if ((current.iteration || 1) !== iteration) return false
  if (isObservableNode(current)) return false
  return current.kind === 'clarify' || current.kind === 'review' || current.kind === 'takeover'
}

/** Earlier nodes fold; the latest execution stays the open conversation. */
export function llmTranscriptThread(run: Run, items: LlmTranscriptItem[]): LlmThreadModel {
  const groups = groupItems(items)
  const last = groups[groups.length - 1]
  const priors = groups.slice(0, -1).map(toSegment)
  if (!last) return { priors, currentTurns: [], current: null, currentNotes: [], conversational: false }
  const { turns: currentTurns, notes, llmTurns } = materialize(last)
  const current: LlmThreadCurrent = {
    nodeId: last.node.nodeId,
    iteration: last.node.iteration,
    label: last.node.label,
    kind: segmentKind(last.node.nodeType, llmTurns),
    status: last.node.status,
  }
  return {
    priors,
    currentTurns,
    current,
    currentNotes: notes,
    conversational: llmNodeConversational(run, current.nodeId, current.iteration),
  }
}
