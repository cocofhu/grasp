import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import type { ClarifyNodeAsk } from './ClarifyHandoffBubble.vue'

/**
 * What a node starts from: the nearest note an earlier node handed over, else its own task.
 * A resume re-enters the same node (no new transition), so earlier visits of nodeId
 * are this node's own history, not its upstream — walk back past them, even when
 * that visit only has a platform-written failure note. A different node's visit
 * with an empty note is not an upstream either; keep walking. The first non-empty
 * note wins (so a later return, such as test review, is not replaced by an older
 * one). Only when the whole chain has no note do we fall back to the task.
 */
export function nodeAsk(list: readonly ClarifyPriorSegment[], index: number, task?: string, nodeId?: string): ClarifyNodeAsk | null {
  for (let i = index - 1; i >= 0; i--) {
    const s = list[i]
    if (!s || (nodeId && s.nodeId === nodeId)) continue
    const text = s.handoff?.trim()
    if (!text) continue
    return { kind: 'handoff', text, from: s.label, source: s.handoffSource || 'agent' }
  }
  const own = task?.trim()
  return own ? { kind: 'task', text: own } : null
}

export function priorAsk(list: readonly ClarifyPriorSegment[], nodeId: string, iteration: number): ClarifyNodeAsk | null {
  const i = list.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  return i < 1 ? null : nodeAsk(list, i, list[i]?.task, list[i]?.nodeId)
}
