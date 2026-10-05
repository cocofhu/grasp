import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export const WORKFLOW_FILTER_KEYS = {
  all: 'common.workflowFilter.all',
  noMatch: 'common.workflowFilter.noMatch',
  searchPlaceholder: 'common.search.workflowPlaceholder',
} as const

// useWorkflowFilter exposes the currently selected workflow (by id) as a
// writable value backed by the URL query param `?wf=`. Backing it by the URL
// means the choice is shared across the 运行 / 待审批 / 产物 views: switching
// between those pages keeps the same workflow in scope, and the filter is
// shareable / restorable via the link. Empty string = 全部工作流.
export function useWorkflowFilter() {
  const route = useRoute()
  const router = useRouter()

  const selected = computed<string>({
    get: () => (typeof route.query.wf === 'string' ? route.query.wf : ''),
    set: (val) => {
      const query = { ...route.query }
      if (val) query.wf = val
      else delete query.wf
      router.replace({ query })
    },
  })

  return { selected }
}
