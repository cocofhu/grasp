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
  conversational: boolean
}

type NodeItem = Extract<LlmTranscriptItem, { type: 'node' }>

interface ExecGroup {
  node: NodeItem
  turns: LlmTurn[]
  error?: string
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

function groupItems(items: LlmTranscriptItem[]): ExecGroup[] {
  const groups: ExecGroup[] = []
  let current: ExecGroup | null = null
  for (const item of items) {
    if (item.type === 'trace') continue
    if (item.type === 'node') {
      current = { node: item, turns: [] }
      groups.push(current)
      continue
    }
    if (!current) continue
    if (item.type === 'turn') current.turns.push(item.turn)
    else if (item.type === 'error') current.error = item.text
  }
  return groups
}

function toSegment(group: ExecGroup): ClarifyPriorSegment {
  const node = group.node
  const kind = segmentKind(node.nodeType, group.turns)
  return {
    nodeId: node.nodeId,
    iteration: node.iteration,
    label: node.label,
    kind,
    status: node.status,
    at: node.startedAt || '',
    endedAt: endedFrom(node.startedAt, node.durationSec),
    turns: clarifyTurnsFromLlm(group.turns, group.error),
    open: false,
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
  if (!last) return { priors, currentTurns: [], current: null, conversational: false }
  const currentTurns = clarifyTurnsFromLlm(last.turns, last.error)
  const current: LlmThreadCurrent = {
    nodeId: last.node.nodeId,
    iteration: last.node.iteration,
    label: last.node.label,
    kind: segmentKind(last.node.nodeType, last.turns),
    status: last.node.status,
  }
  return {
    priors,
    currentTurns,
    current,
    conversational: llmNodeConversational(run, current.nodeId, current.iteration),
  }
}
