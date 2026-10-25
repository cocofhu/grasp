/** Browser address for one run's conversation. Node id is a path segment. */
const NODE_CONVERSATION = /^\/runs\/([^/]+)\/nodes\/([^/]+)\/?$/

/** Placeholder when a launch does not yet know the parked node id. */
export const UNKNOWN_CONVERSATION_NODE = 'incoming'

export function nodeConversationPath(runId: string, nodeId?: string): string {
  const node = (nodeId || '').trim() || UNKNOWN_CONVERSATION_NODE
  return `/runs/${encodeURIComponent(runId)}/nodes/${encodeURIComponent(node)}`
}

export function isNodeConversationPath(path: string): boolean {
  return NODE_CONVERSATION.test(path)
}

/** Run id from `/runs/{runId}/nodes/{nodeId}`, or empty when the path is something else. */
export function nodeConversationRunId(path: string): string {
  const match = path.match(NODE_CONVERSATION)
  if (!match) return ''
  try {
    return decodeURIComponent(match[1])
  } catch {
    return match[1]
  }
}
