import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import type { ClarifyNodeAsk } from './ClarifyHandoffBubble.vue'

/** What a node starts from: the note of the visit right before it, else its own task. */
export function nodeAsk(list: readonly ClarifyPriorSegment[], index: number, task?: string): ClarifyNodeAsk | null {
  const up = index > 0 ? list[index - 1] : undefined
  if (up?.handoff?.trim()) return { kind: 'handoff', text: up.handoff.trim(), from: up.label, source: up.handoffSource || 'agent' }
  return task?.trim() ? { kind: 'task', text: task.trim() } : null
}

export function priorAsk(list: readonly ClarifyPriorSegment[], nodeId: string, iteration: number): ClarifyNodeAsk | null {
  const i = list.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  return i < 1 ? null : nodeAsk(list, i, list[i]?.task)
}
