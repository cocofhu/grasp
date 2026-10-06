// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  listRuns: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuns: apiMocks.listRuns,
    },
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

import WorkflowRunHistoryTab from './WorkflowRunHistoryTab.vue'

function sampleRun(): Run {
  return {
    id: 'run-abc',
    title: 'run',
    workflowId: 'wf-1',
    workflowName: 'demo',
    status: 'completed',
    createdAt: '2026-07-18T00:00:00Z',
    nodes: [],
    edges: [],
    nodeStates: {},
    artifacts: [],
  } as unknown as Run
}

function mountTab() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(WorkflowRunHistoryTab, {
    props: { workflowId: 'wf-1' },
    global: {
      plugins: [i18n],
      stubs: { StatusPill: { template: '<span />' } },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  apiMocks.listRuns.mockResolvedValue({ items: [sampleRun()], total: 1, page: 1, pageSize: 20, hasMore: false })
})

describe('WorkflowRunHistoryTab', () => {
  it('loads runs on mount and renders table', async () => {
    const wrapper = mountTab()
    await flushPromises()
    expect(apiMocks.listRuns).toHaveBeenCalledWith({ wf: 'wf-1' })
    expect(wrapper.find('table').exists()).toBe(true)
    wrapper.unmount()
    vi.useRealTimers()
  })
})
