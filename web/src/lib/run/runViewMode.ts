import type { LocationQuery } from 'vue-router'

/** `/runs/:id` shows execution details for these queries and the Run chat otherwise. */
export function isRunDetailsQuery(query: LocationQuery): boolean {
  return ['details', 'canvas', 'timeline', 'stats', 'llm'].includes(String(query.view || '')) || !!query.node || !!query.tab
}
