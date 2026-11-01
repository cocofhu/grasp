import type { ReferenceInput } from '@/lib/api/apiTypes'

/**
 * Trailing `clientMessageId, references` arguments of `api.reactReply`, each
 * present only when set so plain replies keep their old call shape.
 */
export function replyTail(
  clientMessageId?: string,
  references?: ReferenceInput[],
): [clientMessageId?: string, references?: ReferenceInput[]] {
  if (references?.length) return [clientMessageId, references]
  return clientMessageId ? [clientMessageId] : []
}
