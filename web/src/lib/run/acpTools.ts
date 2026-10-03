import type { AcpEvent, AgentTool } from '../shared/types'

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
