import type { ClarifyTurn } from '@/lib/shared/types'
import { lastMessageText } from '@/lib/chat/messagePreview'
import { attachmentDisplayName } from '@/lib/shared/attachments'
import type { ClarifyPriorSegment } from './ClarifyChat.vue'

type QuestionAnchor = { id: string; signature: string; identity: string }

/** Client identities survive snapshot replacement; transcript indices never become bookmarks. */
export function createClarifyChatAnchors(prefix: string) {
  let currentScope = ''
  let scopeEpoch = 0
  let sequence = 0
  let previous: QuestionAnchor[] = []

  return (scope: string, turns: readonly ClarifyTurn[]): { keys: string[]; anchors: Array<string | undefined> } => {
    if (scope !== currentScope) {
      currentScope = scope
      scopeEpoch++
      previous = []
      sequence = 0
    }
    const humans = turns.filter(turn => turn.role === 'human').map(turn => {
      const signature = JSON.stringify([
        (turn.text || '').trim(),
        turn.live ? [turn.live.sid, turn.live.op, turn.live.variant, turn.live.prompt, turn.live.generated] : null,
        // Blob externalization changes URL/ref after persistence; it is still the same attachment.
        (turn.images || []).map(image => [image.name || '', image.mimeType || '']),
        turn.annotations || [],
      ])
      return { signature, identity: `${turn.opId || turn.at || ''}\u0000${signature}` }
    })
    const used = new Set<QuestionAnchor>()
    const byIdentity = new Map<string, QuestionAnchor[]>()
    for (const anchor of previous) {
      const rows = byIdentity.get(anchor.identity) || []
      rows.push(anchor)
      byIdentity.set(anchor.identity, rows)
    }
    // Reserve exact snapshot matches before matching a live/seed row with its persisted replacement.
    const matches = humans.map(human => {
      const match = byIdentity.get(human.identity)?.shift()
      if (match) used.add(match)
      return match
    })
    const bySignature = new Map<string, QuestionAnchor[]>()
    for (const anchor of previous) {
      if (used.has(anchor)) continue
      const rows = bySignature.get(anchor.signature) || []
      rows.push(anchor)
      bySignature.set(anchor.signature, rows)
    }
    const next = humans.map((human, i) => {
      const match = matches[i] || bySignature.get(human.signature)?.shift()
      return { ...human, id: match?.id || `${prefix}-scope-${scopeEpoch}-question-${++sequence}` }
    })
    previous = next
    let question = `${prefix}-scope-${scopeEpoch}-intro`
    let humanIndex = 0
    let agentIndex = 0
    const anchors: Array<string | undefined> = Array(turns.length).fill(undefined)
    const keys = turns.map((turn, i) => {
      if (turn.role === 'human') {
        question = next[humanIndex++]!.id
        agentIndex = 0
        anchors[i] = question
        return question
      }
      return `${question}-reply-${agentIndex++}`
    })
    return { keys, anchors }
  }
}

export type ClarifyRailItem = { id: string; title: string; preview: string }
export type ClarifyRailOptions = {
  /** Title for a reply with no text or files (choice / form answers). */
  fallbackTitle: string
  /** Bubble text as the host draws it; defaults to the user's prompt for generated Live text. */
  bubbleText?: (turn: ClarifyTurn) => string
}

/** Earlier visits are stable history, so their position is the anchor. */
export function priorAnchorId(seg: Pick<ClarifyPriorSegment, 'nodeId' | 'iteration'>, pi: number): string {
  return `prior:${seg.nodeId}:${seg.iteration}:${pi}`
}

/** Read-only open visit (LLM transcript); its rows only grow. */
export function currentAnchorId(nodeId: string, index: number): string {
  return `current:${nodeId}:${index}`
}

function defaultBubbleText(turn: ClarifyTurn): string {
  const generated = !!turn.live && (turn.live.generated === true || /^Live · /.test(turn.text || ''))
  return generated ? turn.live?.prompt || '' : turn.text || ''
}

/** Rail rows for the human turns of one visit: title from the bubble, preview from the last agent text before the next question. */
export function turnRailItems(turns: readonly ClarifyTurn[], idFor: (index: number) => string, options: ClarifyRailOptions): ClarifyRailItem[] {
  const bubbleText = options.bubbleText || defaultBubbleText
  return turns.flatMap((turn, index) => {
    if (turn.role !== 'human') return []
    if (!bubbleText(turn) && !turn.text && !turn.images?.length && !turn.annotations?.length) return []
    let preview = ''
    for (let i = index + 1; i < turns.length && turns[i]!.role !== 'human'; i++) {
      const reply = turns[i]!
      const message = lastMessageText(reply.parts, reply.text)
      const text = message.trim() ? message : reply.questions?.map(question => question.prompt).join('\n') || ''
      if (text.trim()) preview = text
    }
    const title = bubbleText(turn) || turn.images?.map((image, i) => attachmentDisplayName(image, i)).join(', ') || options.fallbackTitle
    return [{ id: idFor(index), title, preview }]
  })
}

/** Rail rows for earlier visits; folded auto runs without turns have no questions. */
export function priorRailItems(priorSegments: readonly ClarifyPriorSegment[] | undefined, options: ClarifyRailOptions): ClarifyRailItem[] {
  return (priorSegments || []).flatMap(seg => {
    if (seg.kind === 'auto' && !seg.turns.length) return []
    return turnRailItems(seg.turns, pi => priorAnchorId(seg, pi), options)
  })
}

/** Folded history hides its anchors; open the fold before the rail measures the target. */
export function revealRailTarget(scroller: HTMLElement | null | undefined, id: string): void {
  if (!scroller) return
  for (const element of scroller.querySelectorAll<HTMLElement>('[data-chat-message-id]')) {
    if (element.dataset.chatMessageId !== id) continue
    const fold = element.closest('details')
    if (fold && !fold.open) fold.open = true
    return
  }
}
