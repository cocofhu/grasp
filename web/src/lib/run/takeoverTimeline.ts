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

/** Last message text the auto-phase rows show (rail text or timeline message part). */
function lastAutoMessage(rows: readonly ClarifyTurn[]): string {
  for (let i = rows.length - 1; i >= 0; i--) {
    const row = rows[i]!
    const parts = row.parts || []
    for (let j = parts.length - 1; j >= 0; j--) {
      if (parts[j]!.kind === 'message' && parts[j]!.text?.trim()) return parts[j]!.text!.trim()
    }
    if (row.text.trim()) return row.text.trim()
  }
  return ''
}

/**
 * The server prefixes the opening text with the auto phase's last message
 * ("<last message>\n\n---\n<opening>"). With the timeline drawn above, that
 * prefix is a duplicate: keep only the opening.
 */
function dropRepeatedNarration(text: string, rows: readonly ClarifyTurn[]): string {
  const cut = text.indexOf('\n\n---\n')
  if (cut < 0) return text
  const last = lastAutoMessage(rows)
  return last && text.slice(0, cut).trim() === last ? text.slice(cut + 6) : text
}

/** turns with an opening auto-phase timeline expanded in front of the narration. */
export function expandAutoPhase(turns: readonly ClarifyTurn[]): ClarifyTurn[] {
  const first = turns[0]
  if (!first || first.role !== 'agent' || !first.events?.length) return turns as ClarifyTurn[]
  const { events: _events, ...opening } = first
  const rows = autoPhaseRows(first)
  if (rows.length) opening.text = dropRepeatedNarration(opening.text, rows)
  return [...rows, opening, ...turns.slice(1)]
}
