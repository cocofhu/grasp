import { describe, expect, it, vi } from 'vitest'

const replace = vi.fn()
const route = { query: {} as Record<string, string> }

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace }),
}))

import { WORKFLOW_FILTER_KEYS, useWorkflowFilter } from './useWorkflowFilter'

describe('useWorkflowFilter', () => {
  it('reads and writes wf query param', () => {
    expect(WORKFLOW_FILTER_KEYS.all).toContain('workflowFilter')
    route.query = {}
    const { selected } = useWorkflowFilter()
    expect(selected.value).toBe('')
    selected.value = 'wf-1'
    expect(replace).toHaveBeenCalledWith({ query: { wf: 'wf-1' } })
    route.query = { wf: 'wf-1', other: '1' }
    selected.value = ''
    expect(replace).toHaveBeenCalledWith({ query: { other: '1' } })
  })
})
