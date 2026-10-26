import { api } from '@/lib/api/api'
import type { CliCatalogModel } from '@/lib/api/apiTypes'

/**
 * Reads a coding CLI's model shortlist through the server.
 *
 * Like the OpenCode catalog this is a convenience, never a gate: failures
 * resolve to an empty list and the picker falls back to "auto" plus a typed id.
 * Answers are memoized for the page's lifetime; the list ships with the server.
 */

const once = new Map<string, Promise<CliCatalogModel[]>>()
const failed = new Set<string>()

export function loadCliModels(backend: string): Promise<CliCatalogModel[]> {
  const id = backend.trim().toLowerCase()
  if (!id) return Promise.resolve([])
  const cached = once.get(id)
  if (cached) return cached
  const pending = Promise.resolve()
    .then(() => api.cliModels(id))
    .then((r) => {
      if (r?.error) throw new Error(r.error)
      failed.delete(id)
      return r?.models || []
    })
    .catch(() => {
      failed.add(id)
      once.delete(id)
      return []
    })
  once.set(id, pending)
  return pending
}

/** True when the last attempt to read this backend's list failed outright. */
export function cliCatalogFailed(backend: string): boolean {
  return failed.has(backend.trim().toLowerCase())
}

export function resetCliCatalog(): void {
  once.clear()
  failed.clear()
}
