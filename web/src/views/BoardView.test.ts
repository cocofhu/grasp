// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
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

async function setup(props: Record<string, unknown> = {}, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }] })
  await router.push('/board?window=all&from=2020-01-01')
  const messages = locale === 'en'
    ? { en: { ...commonEn, ...pagesEn } }
    : { 'zh-CN': { ...common, ...pages } }
  const i18n = createI18n({ legacy: false, locale, messages })
  const wrapper = mount(BoardView, { props, global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs: { RunBoardPreviewDrawer: { name: 'RunBoardPreviewDrawer', props: ['open', 'run'], template: '<div v-if="open" data-testid="preview"><button @click="$emit(\'close\')">关闭</button></div>' }, RunBoardStatusListModal: { name: 'RunBoardStatusListModal', props: ['open', 'status'], template: '<div v-if="open" data-testid="status-list" />' } } } })
  mounted.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => { vi.clearAllMocks(); api.listRuns.mockImplementation(async (params) => listResponse(params)) })
afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()) })

describe('execution board', () => {
  it('summarizes current runs and recently loaded completions without loading token analytics', async () => {
    const { wrapper } = await setup()
    expect(wrapper.get('[data-testid="board-overview-running"] [data-testid="board-overview-value"]').text()).toBe('1')
    expect(wrapper.get('[data-testid="board-overview-waiting_human"] [data-testid="board-overview-value"]').text()).toBe('1')
    expect(wrapper.get('[data-testid="board-overview-completed"] [data-testid="board-overview-value"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="board-overview-completed"]').text()).toContain('共 48 条执行记录')
    expect(wrapper.get('[data-testid="board-overview-running"]').text()).toContain('正在执行')
    expect(wrapper.get('[data-testid="board-overview-waiting_human"]').text()).toContain('需要处理')
    expect(wrapper.findAll('[data-testid="run-board-column"]')).toHaveLength(3)
    expect(api.listRuns).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-testid="board-usage-link"]').attributes('href')).toBe('/stats')
    expect(wrapper.get('[data-testid="board-all-runs"]').attributes('href')).toBe('/runs')
    expect(api.listRuns).toHaveBeenCalledWith(expect.objectContaining({ status: 'completed', pageSize: 20 }))
  })

  it('refreshes only the three main status columns', async () => {
    const { wrapper } = await setup()
    api.listRuns.mockClear()
    await wrapper.get('[data-testid="board-refresh"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).toHaveBeenCalledTimes(3)
    const statuses = api.listRuns.mock.calls.map((call) => call[0].status).sort()
    expect(statuses).toEqual(['completed', 'running', 'waiting_human'])
    expect(wrapper.find('[data-testid="board-filter-queued"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-filter-failed"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-filter-cancelled"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-extra-columns"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="run-board-column"]')).toHaveLength(3)
    expect(wrapper.text()).not.toContain('同时显示')
    expect(wrapper.text()).not.toContain('工作 failed')
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
    expect(wrapper.get('[data-testid="board-overview-running"] [data-testid="board-overview-value"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="board-overview-completed"]').text()).toContain('共 — 条执行记录')
    api.listRuns.mockImplementation(async (params) => listResponse(params))
    await wrapper.get('[data-testid="board-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="board-load-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="board-overview-running"] [data-testid="board-overview-value"]').text()).toBe('1')
  })

  it('shows the subtitle, keeps the title and actions, and moves the column hint into the column foot', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'BoardView.vue'), 'utf8')
    expect(src).toContain('pages.board.subtitle')
    expect(src).toContain('pages.board.toolbarHint')
    expect(src).toContain('setInterval(load, 3000)')
    expect(src).not.toMatch(/52vh|420px|任务执行中心/)
    expect(src).toContain('SlidingTabs')
    expect(src).toContain('rounded-md bg-surface shadow-sm')
    expect(src).toContain('indicator-testid="board-segment-indicator"')
    expect(src).toMatch(/class="board-segment border-0 bg-transparent/)
    expect(src).toContain("segment === seg.id ? 'font-semibold text-txt' : 'text-txt3 hover:text-txt'")
    expect(src).not.toMatch(/board-search/)
    expect(src).not.toMatch(/board-filter-/)
    expect(src).not.toMatch(/pages\.board\.filters/)
    expect(src).not.toMatch(/#fafafb|#f4f4f5/)
    const { wrapper } = await setup()
    expect(wrapper.get('h2').text()).toBe('运行看板')
    expect(wrapper.get('[data-testid="board-subtitle"]').text()).toBe('跟进正在执行的工作，处理等待人工的任务。')
    expect(wrapper.text()).toContain('用量统计')
    expect(wrapper.text()).toContain('全部运行')
    expect(wrapper.text()).toContain('点击列标题查看全部运行')
    expect(wrapper.text()).not.toContain('按等待时间排序')
    expect(wrapper.text()).not.toContain('任务执行中心')
    expect(wrapper.get('[data-testid="board-refresh"]').exists()).toBe(true)
    const selected = wrapper.get('[data-testid="board-segment-all"]')
    expect(selected.attributes('aria-pressed')).toBe('true')
    expect(selected.classes()).toContain('font-semibold')
    expect(selected.classes()).toContain('border-0')
    expect(selected.classes()).toContain('bg-transparent')
    expect(wrapper.get('[data-testid="board-segment-running"]').classes()).toContain('hover:text-txt')
    expect(wrapper.get('[data-testid="board-segment-running"]').classes()).not.toContain('hover:bg-elevated')
  })

  it('hides the large title when embedded and still shows the subtitle', async () => {
    const { wrapper } = await setup({ embedded: true })
    expect(wrapper.find('h2').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('运行看板')
    expect(wrapper.get('[data-testid="board-subtitle"]').text()).toContain('跟进正在执行的工作')
    expect(wrapper.get('[data-testid="board-usage-link"]').exists()).toBe(true)
  })

  it('filters main columns locally with the sliding segment', async () => {
    const { wrapper } = await setup()
    api.listRuns.mockClear()
    await wrapper.get('[data-testid="board-segment-waiting_human"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).not.toHaveBeenCalled()
    const visible = wrapper.findAll('[data-testid="run-board-column"]').filter((col) => (col.element as HTMLElement).style.display !== 'none')
    expect(visible).toHaveLength(1)
    expect(visible[0].text()).toContain('等待人工')
    expect(wrapper.find('[data-testid="board-extra-columns"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-filter-failed"]').exists()).toBe(false)
    await wrapper.get('[data-testid="board-segment-all"]').trigger('click')
    const allVisible = wrapper.findAll('[data-testid="run-board-column"]').filter((col) => (col.element as HTMLElement).style.display !== 'none')
    expect(allVisible).toHaveLength(3)
  })

  it('does not offer task search and shows every loaded card', async () => {
    const { wrapper } = await setup()
    expect(wrapper.find('[data-testid="board-search"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-search-input"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('搜索当前任务')
    expect(wrapper.text()).toContain('工作 active')
    expect(wrapper.text()).toContain('工作 waiting')
    expect(wrapper.text()).toContain('工作 done-1')
    expect(wrapper.text()).not.toContain('没有匹配的任务')
    const running = wrapper.findAll('[data-testid="run-board-column"]')[0]
    expect(running.get('[data-testid="run-board-column-count"]').text()).toBe('1')
    await wrapper.get('[data-testid="run-board-column-header"]').trigger('click')
    expect(wrapper.findComponent({ name: 'RunBoardStatusListModal' }).props()).toMatchObject({ open: true, status: 'running' })
  })

  it('hides extra filters and search in English as well', async () => {
    const { wrapper } = await setup({}, 'en')
    expect(wrapper.find('[data-testid="board-filter-queued"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-filter-failed"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-filter-cancelled"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-search-input"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Also show')
    expect(wrapper.text()).not.toContain('Search loaded tasks')
    expect(wrapper.text()).not.toContain('No matching tasks')
    expect(wrapper.get('[data-testid="board-segment-all"]').text()).toBe('All')
    expect(wrapper.get('[data-testid="board-segment-running"]').text()).toBe('Running')
    expect(wrapper.findAll('[data-testid="run-board-column"]')).toHaveLength(3)
  })

  it('keeps the segment and column scroll across refresh', async () => {
    const { wrapper } = await setup()
    await wrapper.get('[data-testid="board-segment-running"]').trigger('click')
    await flushPromises()
    const body = wrapper.get('[data-testid="run-board-column-body"]')
    ;(body.element as HTMLElement).scrollTop = 80
    await body.trigger('scroll')
    ;(body.element as HTMLElement).scrollTop = 0
    api.listRuns.mockClear()
    await wrapper.get('[data-testid="board-refresh"]').trigger('click')
    await flushPromises()
    expect(api.listRuns).toHaveBeenCalledTimes(3)
    const statuses = api.listRuns.mock.calls.map((call) => call[0].status).sort()
    expect(statuses).toEqual(['completed', 'running', 'waiting_human'])
    expect(wrapper.get('[data-testid="board-segment-running"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="board-search-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-extra-columns"]').exists()).toBe(false)
    expect((wrapper.get('[data-testid="run-board-column-body"]').element as HTMLElement).scrollTop).toBe(80)
  })
})
