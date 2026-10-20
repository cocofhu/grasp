import type { ClarifyTurn } from '@/lib/shared/types'

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
