// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'

const api = vi.hoisted(() => ({ listRuns: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api }))
import BoardView from './BoardView.vue'

const mounted: ReturnType<typeof mount>[] = []
const run = (id: string, status: string) => ({ id, status, title: `工作 ${id}`, workflowId: 'workflow', workflowName: '研发流程', progress: 35, currentNodeLabel: '实现', nodeRuns: {}, artifacts: [] }) as unknown as Run
const items: Record<string, Run[]> = { running: [run('active', 'running')], waiting_human: [run('waiting', 'waiting_human')], completed: [run('done-1', 'completed'), run('done-2', 'completed')], failed: [run('failed', 'failed')], queued: [], cancelled: [] }
function listResponse({ status, pageSize }: { status: string; pageSize: number }) {
  return { items: items[status] || [], total: status === 'completed' ? 48 : items[status]?.length || 0, page: 1, pageSize, hasMore: status === 'completed' }
}

async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }] })
  await router.push('/board?window=all&from=2020-01-01')
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(BoardView, { global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs: { RunBoardPreviewDrawer: { name: 'RunBoardPreviewDrawer', props: ['open', 'run'], template: '<div v-if="open" data-testid="preview"><button @click="$emit(\'close\')">关闭</button></div>' }, RunBoardStatusListModal: { name: 'RunBoardStatusListModal', props: ['open', 'projectId', 'status'], template: '<div v-if="open" data-testid="status-list" />' } } } })
  mounted.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => { vi.clearAllMocks(); api.listRuns.mockImplementation(async (params) => listResponse(params)) })
afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()) })

describe('execution board', () => {
  it('summarizes current runs and recently loaded completions without loading token analytics', async () => {
    const { wrapper } = await setup()
    expect(wrapper.get('[data-testid="board-overview-running"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="board-overview-waiting_human"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="board-overview-completed"]').text()).toContain('最近完成2')
    expect(wrapper.findAll('[data-testid="run-board-column"]')).toHaveLength(3)
    expect(api.listRuns).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-testid="board-usage-link"]').attributes('href')).toBe('/stats')
    expect(wrapper.get('[data-testid="board-all-runs"]').attributes('href')).toBe('/runs')
    expect(api.listRuns).toHaveBeenCalledWith(expect.objectContaining({ status: 'completed', pageSize: 20 }))
  })

  it('refreshes run columns and preserves optional status filters', async () => {
    const { wrapper } = await setup()
    api.listRuns.mockClear()
    await wrapper.get('[data-testid="board-refresh"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).toHaveBeenCalledTimes(3)
    await wrapper.get('[data-testid="board-filter-failed"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="run-board-column"]')).toHaveLength(4)
    expect(wrapper.text()).toContain('工作 failed')
    expect(api.listRuns).toHaveBeenCalledWith(expect.objectContaining({ status: 'failed', pageSize: 100 }))
  })

  it('preserves run preview, status list identity and completed-list navigation', async () => {
    const { wrapper, router } = await setup()
    await wrapper.get('.run-board-card').trigger('click')
    expect(wrapper.find('[data-testid="preview"]').exists()).toBe(true)
    await wrapper.get('[data-testid="run-board-column-header"]').trigger('click')
    expect(wrapper.find('[data-testid="preview"]').exists()).toBe(false)
    const modal = wrapper.findComponent({ name: 'RunBoardStatusListModal' })
    expect(modal.props()).toMatchObject({ open: true, status: 'running' })
    await wrapper.get('[data-testid="board-view-more-completed"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/runs')
    expect(router.currentRoute.value.query).toEqual({ status: 'completed' })
  })

  it('keeps unavailable counts distinct from zero and recovers through retry', async () => {
    api.listRuns.mockRejectedValue(new Error('Offline'))
    const { wrapper } = await setup()
    expect(wrapper.find('[data-testid="board-load-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="board-overview-running"]').text()).toContain('—')
    api.listRuns.mockImplementation(async (params) => listResponse(params))
    await wrapper.get('[data-testid="board-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="board-load-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="board-overview-running"]').text()).toContain('1')
  })
})
