import type { AcpEvent, AgentPart, AgentTool } from '../shared/types'

/** Max tool rows one turn keeps (matches the server's MaxReactTools). */
export const MAX_AGENT_TOOLS = 50

/**
 * Tool calls of one cumulative ACP snapshot, in order: name + status only.
 * Publish snapshots are absolute, so callers replace (not append) their list.
 */
export function toolsFromAcp(events: ReadonlyArray<Pick<AcpEvent, 'kind' | 'title' | 'status'>> | undefined): AgentTool[] {
  const out: AgentTool[] = []
  for (const ev of events || []) {
    if (ev.kind !== 'tool_call' || !ev.title) continue
    if (out.length === MAX_AGENT_TOOLS) break
    out.push(ev.status ? { title: ev.title, status: ev.status } : { title: ev.title })
  }
  return out
}

/** True when two tool lists render identically (avoids churning reactive state). */
export function sameTools(a: AgentTool[] | undefined, b: AgentTool[] | undefined): boolean {
  const x = a || []
  const y = b || []
  if (x.length !== y.length) return false
  return x.every((t, i) => t.title === y[i]!.title && (t.status || '') === (y[i]!.status || ''))
}

/**
 * Steps of the open agent row: the last kind=timeline event after the last
 * segment marker (sealed rows have none). Undefined when the snapshot has none.
 */
export function partsFromAcp(events: ReadonlyArray<Pick<AcpEvent, 'kind' | 'parts'>> | undefined): AgentPart[] | undefined {
  const list = events || []
  for (let i = list.length - 1; i >= 0; i--) {
    const ev = list[i]!
    if (ev.kind === 'segment') return undefined
    if (ev.kind === 'timeline') return ev.parts?.length ? ev.parts.map((p) => ({ ...p })) : undefined
  }
  return undefined
}

const partKey = (p: AgentPart) => [p.kind, p.text, p.title, p.status, p.summary, p.input, p.output].map((x) => x || '').join('\u0000')

/** True when two step lists render identically. */
export function sameParts(a: AgentPart[] | undefined, b: AgentPart[] | undefined): boolean {
  const x = a || []
  const y = b || []
  return x.length === y.length && x.every((p, i) => partKey(p) === partKey(y[i]!))
}

export type TimelineBlock =
  | { kind: 'tools'; tools: AgentTool[]; key: string; last: boolean }
  | { kind: 'thought' | 'message'; text: string; key: string; last: boolean; index: number }

/** Groups consecutive tool steps into one block, keeping everything else in order. */
export function timelineBlocks(parts: readonly AgentPart[] | undefined): TimelineBlock[] {
  const out: TimelineBlock[] = []
  ;(parts || []).forEach((p, index) => {
    const prev = out[out.length - 1]
    if (p.kind === 'tool') {
      const tool: AgentTool = { title: p.title || 'tool' }
      if (p.status) tool.status = p.status
      if (p.summary) tool.summary = p.summary
      if (p.input) tool.input = p.input
      if (p.output) tool.output = p.output
      if (prev?.kind === 'tools') prev.tools.push(tool)
      else out.push({ kind: 'tools', tools: [tool], key: `g${index}`, last: false })
    } else if ((p.kind === 'thought' || p.kind === 'message') && p.text?.trim()) {
      out.push({ kind: p.kind, text: p.text, key: `${p.kind[0]}${index}`, last: false, index })
    }
  })
  if (out.length) out[out.length - 1]!.last = true
  return out
}
