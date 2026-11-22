import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { i18n } from '@/lib/shared/i18n'

export type ReactQueueTarget = { runId: string; nodeId: string }

/**
 * Remove, steer and reorder the pending queue of a node's chat. The edits
 * race the running turn: a refusal (the item already ran) shows in the next
 * queue snapshot, but a failed request produces none, so it is reported.
 * target returns null when the chat cannot take queue edits.
 */
export function reactQueueActions(target: () => ReactQueueTarget | null) {
  const toast = useToast()
  async function run(name: string, call: (at: ReactQueueTarget) => Promise<unknown>) {
    const at = target()
    if (!at) return
    try {
      await call(at)
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      console.warn(`${name} failed`, msg)
      toast.error(String(i18n.global.t('pages.clarify.queueActionFailed', { msg })))
    }
  }
  return {
    queueRemove: async (itemId: string | undefined) => {
      if (itemId) await run('reactQueueRemove', (at) => api.reactQueueRemove(at.runId, at.nodeId, itemId))
    },
    queueSteer: (itemId: string) => run('reactQueueSteer', (at) => api.reactQueueSteer(at.runId, at.nodeId, itemId)),
    queueReorder: async (itemIds: string[]) => {
      if (itemIds.length) await run('reactQueueReorder', (at) => api.reactQueueReorder(at.runId, at.nodeId, itemIds))
    },
  }
}
