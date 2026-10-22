import { computed, type ComputedRef } from 'vue'
import { conversationToolbarRunName } from '@/lib/inbox/inboxDisplay'
import { usePendingGates } from '@/lib/inbox/usePendingGates'

/**
 * Display name for an open run conversation. Reads the pending rows
 * `patchRunTitle` rewrites, so a sidebar rename updates the toolbar without
 * a full reload. Returns '' when the run has no readable name yet.
 */
export function useConversationToolbarRunName(
  source: () => {
    run?: { id?: string; title?: string; workflowName?: string } | null
    fallback?: { runId?: string; runTitle?: string; workflowName?: string } | null
  },
): ComputedRef<string> {
  const { displayedItems, remoteItems, sidebarExtras } = usePendingGates()
  return computed(() => {
    const { run, fallback } = source()
    return conversationToolbarRunName({
      run,
      fallback,
      pendingRows: [...sidebarExtras.value, ...displayedItems.value, ...remoteItems.value],
    })
  })
}
