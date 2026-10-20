import type { NodeRun, StateTraceEntry, WFEdge } from '@/lib/shared/types'
import type { EdgeHandoff } from './canvasContext'

function edgeKind(edge: WFEdge): string {
  return edge.kind || 'success'
}

function traceKind(entry: StateTraceEntry): string {
  return entry.kind || 'success'
}

/** The execution's handoff, or undefined when that visit left no text. */
function handoffOf(execs: NodeRun[] | undefined, iteration: number): EdgeHandoff | undefined {
  if (!execs?.length || iteration < 1) return undefined
  let row = execs.find((item) => item.iteration === iteration)
  if (!row) {
    const byIndex = execs[iteration - 1]
    if (byIndex && (byIndex.iteration == null || byIndex.iteration === iteration)) row = byIndex
  }
  if (!row) return undefined
  const text = typeof row.outputs?.handoff === 'string' ? row.outputs.handoff.trim() : ''
  if (!text) return undefined
  return { text, source: row.outputs?.handoff_source === 'platform' ? 'platform' : 'agent' }
}

/**
 * Workspace each traced leave (transition or rollback) onto the edge it took,
 * in trace order. A visit counts only when that execution's handoff has text.
 * An in-progress execution with no handoff is not a visit and does not replace
 * earlier ones. Edges the trace never left through stay absent.
 */
export function collectEdgeHandoffs(
  edges: WFEdge[] | undefined,
  trace: StateTraceEntry[] | undefined,
  executions: Record<string, NodeRun[]> | undefined,
): Record<string, EdgeHandoff[]> {
  const out: Record<string, EdgeHandoff[]> = {}
  if (!edges?.length || !trace?.length) return out
  const iteration = new Map<string, number>()
  const inferred = new Map<string, number>()
  for (const entry of trace) {
    if (!entry?.nodeId) continue
    if (entry.event === 'enter') {
      const prev = iteration.get(entry.nodeId) ?? 0
      iteration.set(entry.nodeId, entry.iteration && entry.iteration > 0 ? entry.iteration : prev + 1)
      continue
    }
    if (entry.event !== 'transition' && entry.event !== 'rollback') continue
    if (!entry.to) continue
    let visit = iteration.get(entry.nodeId) ?? 0
    if (visit < 1) {
      visit = (inferred.get(entry.nodeId) ?? 0) + 1
      inferred.set(entry.nodeId, visit)
    }
    const handoff = handoffOf(executions?.[entry.nodeId], visit)
    if (!handoff) continue
    const kind = traceKind(entry)
    const edge = edges.find((item) => item.source === entry.nodeId && item.target === entry.to && edgeKind(item) === kind)
    if (!edge) continue
    const list = out[edge.id] ?? []
    list.push(handoff)
    out[edge.id] = list
  }
  return out
}
