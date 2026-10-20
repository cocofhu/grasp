import { api } from '@/lib/api/api'
import { inboxItemKey } from '@/lib/inbox/inboxActiveSelection'
import type { InboxItem } from '@/lib/shared/types'

/** Server rejects pageSize above this (handlers/pagination.go). */
const GATES_LIST_PAGE_SIZE = 100
/** Stop a bad total from paging forever (100 × 50). */
const GATES_LIST_MAX_PAGES = 50

interface ListAllPendingGatesParams {
  signal?: AbortSignal
  wf?: string
  projectId?: string
  tag?: string
}

/**
 * Every pending inbox row, not the first page.
 * The desktop sidebar is the only way in after the card column left, and the
 * server caps a single page at 100.
 */
export async function listAllPendingGates(
  params: ListAllPendingGatesParams = {},
): Promise<{ items: InboxItem[]; total: number }> {
  const items: InboxItem[] = []
  const seen = new Set<string>()
  let total = 0
  for (let page = 1; page <= GATES_LIST_MAX_PAGES; page++) {
    const data = await api.listGates({
      page,
      pageSize: GATES_LIST_PAGE_SIZE,
      signal: params.signal,
      wf: params.wf,
      projectId: params.projectId,
      tag: params.tag,
    })
    total = Number.isFinite(data.total) ? data.total : items.length
    let added = 0
    for (const it of data.items) {
      const key = inboxItemKey(it)
      if (seen.has(key)) continue
      seen.add(key)
      items.push(it)
      added++
    }
    if (added === 0 || items.length >= total || data.hasMore === false) break
  }
  return { items, total }
}
