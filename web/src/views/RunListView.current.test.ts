// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'

const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    isMobile: ref(false),
    listRuns: vi.fn(),
    listWorkflows: vi.fn(),
    cancelRun: vi.fn(),
    deleteRun: vi.fn(),
    success: vi.fn(),
    error: vi.fn(),
  }
})
vi.mock('@/lib/api/api', () => ({
  api: { listRuns: mocks.listRuns, listWorkflows: mocks.listWorkflows, cancelRun: mocks.cancelRun, deleteRun: mocks.deleteRun },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success, error: mocks.error, warn: vi.fn() }) }))
vi.mock('@/lib/composables/useBreakpoint', () => ({ useBreakpoint: () => ({ isMobile: mocks.isMobile }) }))

import RunListView from './RunListView.vue'

function run(over: Partial<Run>): Run {
  return {
    id: 'run-x',
    workflowId: 'wf-1',
    workflowName: '发布流程',
    workflowVersion: 4,
    title: '运行',
    status: 'running',
    trigger: 'manual',
    startedAt: '2026-11-11T00:00:00Z',
    durationSec: 12,
    progress: 0.2,
    priority: 'normal',
    tags: [],
    nodeRuns: {},
    ...over,
  } as Run
}

const running = (id: string, title = id) => run({ id, title, status: 'running' })
const save = (id: string, state: 'saving' | 'failed', title: string) => run({
  id,
  title,
  status: 'paused',
  workspaceSave: { checkpointId: 'cp', nodeId: 'code', state },
})

async function setup(path = '/runs') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common, ...pages } } })
  const wrapper = mount(RunListView, {
    attachTo: document.body,
    global: { plugins: [router, i18n], stubs: { teleport: true, WorkflowFilter: true, TagFilter: true, StatusFilter: true } },
  })
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  mocks.isMobile.value = false
  mocks.listWorkflows.mockResolvedValue([])
  mocks.listRuns.mockResolvedValue({ items: [], total: 0, hasMore: false })
})

describe('RunListView 进行中 current view', () => {
  function install(pools: Record<string, Run[]>) {
    mocks.listRuns.mockImplementation((params?: { status?: string; page?: number; pageSize?: number }) => {
      const status = params?.status ?? ''
      const page = params?.page ?? 1
      const pageSize = params?.pageSize ?? 20
      const pool = pools[status] ?? pools['*'] ?? []
      const start = (page - 1) * pageSize
      const items = pool.slice(start, start + pageSize)
      return Promise.resolve({ items, total: pool.length, hasMore: start + items.length < pool.length, page, pageSize })
    })
  }

  it('draws every running workflow plus unfinished saves, not one page', async () => {
    const live = Array.from({ length: 8 }, (_, i) => running(`run-${i}`, `运行 ${i}`))
    const paused = [
      save('saving', 'saving', '正在保存'),
      save('broken', 'failed', '保存坏了'),
      run({ id: 'hand', title: '我暂停的', status: 'paused' }),
    ]
    const queued = run({ id: 'queued-one', title: '排队的', status: 'queued' })
    const waiting = run({ id: 'wait', title: '等人', status: 'waiting_human' })
    install({
      '': [...live, queued, ...paused, waiting],
      running: live,
      paused,
      waiting_human: [waiting],
      completed: [run({ id: 'old', title: '已结束', status: 'completed' })],
    })
    const { wrapper } = await setup()
    await wrapper.get('[data-testid="run-seg-active"]').trigger('click')
    await flushPromises()

    const titles = wrapper.findAll('tr.run-row').map((row) => row.text())
    expect(titles).toHaveLength(10)
    expect(wrapper.get('[data-testid="run-seg-active"] [data-testid="seg-count"]').text()).toBe('10')
    expect(titles.filter((text) => text.includes('运行 '))).toHaveLength(8)
    const saving = wrapper.findAll('tr.run-row').find((row) => row.text().includes('正在保存'))!
    expect(saving.text()).toContain('保存工作区中')
    expect(saving.text()).toContain('#saving')
    expect(saving.text()).toContain('发布流程')
    expect(saving.text()).toContain('v4')
    expect(wrapper.findAll('tr.run-row').find((row) => row.text().includes('保存坏了'))!.text()).toContain('保存失败')
    expect(wrapper.text()).not.toContain('我暂停的')
    expect(wrapper.text()).not.toContain('排队的')
    expect(wrapper.text()).not.toContain('等人')
    expect(wrapper.find('.pagination').exists()).toBe(false)

    await wrapper.get('[data-testid="run-seg-waiting"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tr.run-row')).toHaveLength(1)
    expect(wrapper.text()).toContain('等人')
    expect(wrapper.text()).not.toContain('正在保存')
    wrapper.unmount()
  })

  it('keeps all 51 running rows on screen and still pages a finished segment', async () => {
    const live = Array.from({ length: 51 }, (_, i) => running(`run-${i}`, `运行 ${i}`))
    const done = Array.from({ length: 25 }, (_, i) => run({ id: `done-${i}`, title: `完成 ${i}`, status: 'completed' }))
    install({ '': [...live, ...done], running: live, paused: [], completed: done })
    const { wrapper } = await setup()
    await wrapper.get('[data-testid="run-seg-active"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tr.run-row')).toHaveLength(51)
    expect(wrapper.findAll('tr.run-row')[50].text()).toContain('运行 50')
    expect(wrapper.find('.pagination').exists()).toBe(false)
    const runningCalls = mocks.listRuns.mock.calls.filter((call) => call[0]?.status === 'running')
    expect(runningCalls[0][0]).toEqual(expect.objectContaining({ page: 1, pageSize: 100 }))

    await wrapper.get('[data-testid="run-seg-done"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tr.run-row')).toHaveLength(20)
    expect(wrapper.find('.pagination').exists()).toBe(true)
    wrapper.unmount()
  })

  it('drops a run from 进行中 once it has completed and keeps a failed save after refresh', async () => {
    const live = [running('stay', '还在'), running('finishing', '即将完成')]
    const paused = [save('broken', 'failed', '保存坏了')]
    const done: Run[] = []
    install({ '': [...live, ...paused], running: live, paused, completed: done })
    const { wrapper, router } = await setup()
    await wrapper.get('[data-testid="run-seg-active"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tr.run-row')).toHaveLength(3)

    live.splice(1, 1)
    done.push(run({ id: 'finishing', title: '即将完成', status: 'completed' }))
    await router.replace({ query: { ...router.currentRoute.value.query, refresh: '1' } })
    await flushPromises()
    const rows = wrapper.findAll('tr.run-row').map((row) => row.text())
    expect(rows.some((text) => text.includes('即将完成'))).toBe(false)
    expect(rows.some((text) => text.includes('还在'))).toBe(true)
    expect(rows.some((text) => text.includes('保存失败'))).toBe(true)

    await wrapper.get('[data-testid="run-seg-done"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tr.run-row')).toHaveLength(1)
    expect(wrapper.text()).toContain('即将完成')
    wrapper.unmount()
  })
})
