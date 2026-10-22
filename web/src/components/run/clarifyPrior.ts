import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import type { ClarifyNodeAsk } from './ClarifyHandoffBubble.vue'

/**
 * What a node starts from: the note of the visit that routed into it, else its own task.
 * A resume re-enters the same node (no new transition), so earlier visits of nodeId
 * are this node's own history, not its upstream — walk back past them.
 */
export function nodeAsk(list: readonly ClarifyPriorSegment[], index: number, task?: string, nodeId?: string): ClarifyNodeAsk | null {
  let up: ClarifyPriorSegment | undefined
  for (let i = index - 1; i >= 0; i--) {
    const s = list[i]
    if (nodeId && s?.nodeId === nodeId) continue
    up = s
    break
  }
  if (up?.handoff?.trim()) return { kind: 'handoff', text: up.handoff.trim(), from: up.label, source: up.handoffSource || 'agent' }
  return task?.trim() ? { kind: 'task', text: task.trim() } : null
}

export function priorAsk(list: readonly ClarifyPriorSegment[], nodeId: string, iteration: number): ClarifyNodeAsk | null {
  const i = list.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  return i < 1 ? null : nodeAsk(list, i, list[i]?.task, list[i]?.nodeId)
}
