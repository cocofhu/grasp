import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import type { ClarifyNodeAsk } from './ClarifyHandoffBubble.vue'

/**
 * What a node starts from: the nearest note an earlier node handed over, else its own task.
 * A resume re-enters the same node (no new transition), so earlier visits of nodeId
 * are this node's own history, not its upstream — walk back past them, even when
 * that visit only has a platform-written failure note. A different node's visit
 * with an empty note is not an upstream either; keep walking. The first non-empty
 * note wins (so a later return, such as test review, is not replaced by an older
 * one). When the visits above are only this node and one of them has a user message,
 * the opening bubble uses the earliest of those messages instead of the node prompt.
 * No visits above still falls back to the task. Another node with no note does too.
 * A same-node rerun that has no user message does not.
 */
export function nodeAsk(list: readonly ClarifyPriorSegment[], index: number, task?: string, nodeId?: string): ClarifyNodeAsk | null {
  let sawOther = false
  for (let i = index - 1; i >= 0; i--) {
    const s = list[i]
    if (!s) continue
    if (nodeId && s.nodeId === nodeId) continue
    sawOther = true
    const text = s.handoff?.trim()
    if (!text) continue
    return { kind: 'handoff', text, from: s.label, source: s.handoffSource || 'agent' }
  }
  if (index > 0 && nodeId && !sawOther) {
    const words = earliestUserText(list, index, nodeId)
    return words ? { kind: 'task', text: words } : null
  }
  const own = task?.trim()
  return own ? { kind: 'task', text: own } : null
}

/** First non-empty user message across earlier visits, in time order. Later confirmations are not the opening. */
function earliestUserText(list: readonly ClarifyPriorSegment[], index: number, nodeId: string): string {
  for (let i = 0; i < index; i++) {
    const segment = list[i]
    if (!segment || segment.nodeId !== nodeId) continue
    for (const turn of segment.turns || []) {
      if (turn.role !== 'human') continue
      const text = turn.text?.trim()
      if (text) return text
    }
  }
  return ''
}

export function priorAsk(list: readonly ClarifyPriorSegment[], nodeId: string, iteration: number): ClarifyNodeAsk | null {
  const i = list.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  return i < 1 ? null : nodeAsk(list, i, list[i]?.task, list[i]?.nodeId)
}
