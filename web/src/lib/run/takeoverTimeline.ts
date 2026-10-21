import type { AgentTool, ClarifyTurn } from '@/lib/shared/types'
import { splitTurns } from './llmTranscript'

/**
 * Takeover conversation opening turn: the server puts the interrupted auto
 * phase's aggregated ACP events (prompt stripped) on clarifyByNode[node]
 * .turns[0].events. The chat continues that timeline instead of showing the
 * bare narration: one agent row per sealed segment, drawn by the existing
 * rows (思考过程 / 使用了 N 个工具 / message, or the step timeline when the
 * events carry kind=timeline parts), then the opening narration itself.
 */
export function autoPhaseRows(turn: ClarifyTurn): ClarifyTurn[] {
  const rows: ClarifyTurn[] = []
  for (const lt of splitTurns(turn.events)) {
    for (const a of lt.answers) {
      const row: ClarifyTurn = { role: 'agent', text: a.text || '', at: turn.at }
      if (a.thought) row.thought = a.thought
      if (a.tools.length) row.tools = a.tools.map((x): AgentTool => (x.status ? { title: x.title, status: x.status } : { title: x.title }))
      if (a.parts?.length) row.parts = a.parts.map((p) => ({ ...p }))
      if (!row.text && !row.thought && !row.tools && !row.parts) continue
      rows.push(row)
    }
  }
  // Same shape as live segments: earlier rows are handoff (no「已完成」); the
  // last keeps its completed state unless it has no text/thought (a tools-only
  // row must not read as an empty failed reply).
  rows.forEach((row, i) => {
    if (i < rows.length - 1 || (!row.text.trim() && !(row.thought || '').trim())) row.handoff = true
  })
  return rows
}

/**
 * The auto phase's span from the opening turn (startedAt = this visit's start,
 * endedAt = when the takeover took effect). Both or nothing: a half span
 * would draw no duration anyway.
 */
function autoPhaseSpan(turn: ClarifyTurn): { startedAt: string; endedAt: string } | null {
  return turn.startedAt && turn.endedAt ? { startedAt: turn.startedAt, endedAt: turn.endedAt } : null
}

/**
 * turns with an opening auto-phase timeline expanded in front of the takeover
 * notice. With events the server sends the notice alone (no narration
 * prefix). The span goes on the last auto row, so its fold row shows the
 * duration in the existing format (AgentTimeline elapsed line).
 */
export function expandAutoPhase(turns: readonly ClarifyTurn[]): ClarifyTurn[] {
  const first = turns[0]
  if (!first || first.role !== 'agent' || !first.events?.length) return turns as ClarifyTurn[]
  const { events: _events, startedAt: _startedAt, endedAt: _endedAt, ...opening } = first
  const rows = autoPhaseRows(first)
  const span = autoPhaseSpan(first)
  if (span && rows.length) Object.assign(rows[rows.length - 1]!, span)
  return [...rows, opening, ...turns.slice(1)]
}

/**
 * Opening User Message of a taken-over conversation: the node's task as the
 * backend sends it (clarifyByNode[nodeId].task — interpolated prompt, secrets
 * masked, truncated at 280 chars). Empty when absent; never derived here.
 */
export function takeoverTask(conv: { takeover?: boolean; task?: string } | null | undefined): string {
  return (conv?.task ?? '').trim()
}
