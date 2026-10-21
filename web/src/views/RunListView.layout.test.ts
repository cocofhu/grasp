// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { fmtTime, formatTrigger, relTime } from '@/lib/shared/format'
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

const zh = (pages as unknown as { pages: { runList: Record<string, string> } }).pages.runList
const ago = (sec: number) => new Date(Date.now() - sec * 1000).toISOString()

function run(over: Partial<Run>): Run {
  return {
    id: 'run-x',
    workflowId: 'wf-1',
    workflowName: 'Flow',
    workflowVersion: 3,
    title: 'Run title',
    status: 'completed',
    trigger: 'manual',
    startedAt: ago(7200),
    durationSec: 90,
    progress: 1,
    priority: 'normal',
    tags: [],
    nodeRuns: {},
    ...over,
  } as Run
}

const RUNNING = run({ id: 'run-aaa111', title: '修复回归测试', status: 'running', priority: 'high', trigger: 'api', tags: ['研发'], progress: 0.45, currentNodeLabel: '执行开发任务', startedAt: ago(3 * 3600) })
const DONE = run({ id: 'run-bbb222', title: '整理发布说明', status: 'completed', startedAt: ago(2 * 86400) })
const QUEUED = run({ id: 'run-ccc333', title: '排队中的运行', status: 'queued', startedAt: '', createdAt: ago(600), progress: 0 })

async function setup(path = '/runs') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common, ...pages } } })
  const wrapper = mount(RunListView, {
    attachTo: document.body,
    global: {
      plugins: [router, i18n],
      stubs: { teleport: true, WorkflowFilter: true, TagFilter: true, StatusFilter: true },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

const rowOf = (wrapper: Awaited<ReturnType<typeof setup>>['wrapper'], title: string) =>
  wrapper.findAll('tr.run-row').find((tr) => tr.text().includes(title))!

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  mocks.isMobile.value = false
  mocks.listRuns.mockResolvedValue({ items: [RUNNING, DONE, QUEUED], total: 3 })
  mocks.listWorkflows.mockResolvedValue([])
  mocks.cancelRun.mockResolvedValue({})
  mocks.deleteRun.mockResolvedValue({})
})

describe('RunListView status segments', () => {
  it('renders the five segments with 全部 active by default', async () => {
    const { wrapper } = await setup()
    const segs = wrapper.findAll('[data-testid^="run-seg-"]')
    expect(segs.map((s) => s.text())).toEqual([zh.segAll, zh.segActive, zh.segWaiting, zh.segDone, zh.segFailed])
    expect(wrapper.get('[data-testid="run-seg-all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="run-seg-active"]').attributes('aria-pressed')).toBe('false')
    wrapper.unmount()
  })

  it('maps each segment onto the existing ?status= filter and reloads the list', async () => {
    const { wrapper, router } = await setup()
    await wrapper.get('[data-testid="run-seg-active"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.status).toBe('running,queued')
    expect(mocks.listRuns).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'running,queued' }))
    expect(wrapper.get('[data-testid="run-seg-active"]').attributes('aria-pressed')).toBe('true')

    await wrapper.get('[data-testid="run-seg-failed"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.status).toBe('failed,cancelled')

    await wrapper.get('[data-testid="run-seg-all"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.status).toBeUndefined()
    wrapper.unmount()
  })

  it('highlights the matching segment for a status URL and none for a custom combination', async () => {
    const waiting = await setup('/runs?status=waiting_human')
    expect(waiting.wrapper.get('[data-testid="run-seg-waiting"]').attributes('aria-pressed')).toBe('true')
    waiting.wrapper.unmount()
    const custom = await setup('/runs?status=running,completed')
    expect(custom.wrapper.findAll('[data-testid^="run-seg-"][aria-pressed="true"]')).toHaveLength(0)
    custom.wrapper.unmount()
  })

  it('keeps the original three filter dropdowns on mobile instead of segments', async () => {
    mocks.isMobile.value = true
    const { wrapper } = await setup()
    expect(wrapper.find('[data-testid="run-list-toolbar"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'StatusFilter' }).exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('RunListView merged cells', () => {
  it('uses four data columns: 运行 / 状态 / 开始 / 耗时 plus a hidden-label actions column', async () => {
    const { wrapper } = await setup()
    const heads = wrapper.findAll('thead th').map((th) => th.text().trim())
    expect(heads.slice(0, 4)).toEqual([zh.colRun, zh.colStatus, zh.colStart, zh.colDuration])
    expect(heads).toHaveLength(5)
    expect(wrapper.get('thead th:last-child .sr-only').text()).toBe('操作')
    expect(wrapper.get('table').classes()).toContain('run-table')
    wrapper.unmount()
  })

  it('puts title, #id, workflow + version, non-default trigger and tags in the run cell', async () => {
    const { wrapper } = await setup()
    const cell = rowOf(wrapper, '修复回归测试').get('[data-testid="run-cell-main"]')
    const title = cell.get('.truncate')
    expect(title.text()).toBe('修复回归测试')
    expect(title.attributes('title')).toBe('修复回归测试')
    expect(cell.findComponent({ name: 'PriorityBadge' }).exists()).toBe(true)
    const meta = cell.findAll('div')[1]!
    expect(meta.text()).toContain('#aaa111')
    expect(meta.text()).toContain('Flow')
    expect(meta.text()).toContain('v3')
    expect(meta.text()).toContain(formatTrigger('api'))
    expect(meta.find('.chip').text()).toBe('研发')
    expect(meta.classes()).toContain('whitespace-nowrap')
    wrapper.unmount()
  })

  it('hides default normal priority and manual trigger', async () => {
    const { wrapper } = await setup()
    const cell = rowOf(wrapper, '整理发布说明').get('[data-testid="run-cell-main"]')
    expect(cell.findComponent({ name: 'PriorityBadge' }).exists()).toBe(false)
    expect(cell.text()).not.toContain(formatTrigger('manual'))
    expect(cell.find('.chip').exists()).toBe(false)
    wrapper.unmount()
  })

  it('merges status and progress: progress + node label only for in-flight / failed runs', async () => {
    const { wrapper } = await setup()
    const running = rowOf(wrapper, '修复回归测试').findAll('td')[1]!
    expect(running.findComponent({ name: 'StatusPill' }).props('status')).toBe('running')
    expect(running.text()).toContain('45%')
    expect(running.text()).toContain('执行开发任务')
    const done = rowOf(wrapper, '整理发布说明').findAll('td')[1]!
    expect(done.findComponent({ name: 'StatusPill' }).props('status')).toBe('completed')
    expect(done.text()).not.toContain('%')
    wrapper.unmount()
  })
})

describe('RunListView relative start time', () => {
  it('shows relative start time with the absolute time as a tooltip', async () => {
    const { wrapper } = await setup()
    const start = rowOf(wrapper, '修复回归测试').get('[data-testid="run-cell-start"]')
    expect(start.text()).toBe(relTime(RUNNING.startedAt))
    expect(start.text()).not.toBe(fmtTime(RUNNING.startedAt))
    expect(start.attributes('title')).toBe(fmtTime(RUNNING.startedAt))
    expect(start.classes()).toContain('whitespace-nowrap')
    expect(rowOf(wrapper, '整理发布说明').get('[data-testid="run-cell-start"]').text()).toBe(relTime(DONE.startedAt))
    // hours vs days buckets come out differently
    expect(relTime(RUNNING.startedAt)).not.toBe(relTime(DONE.startedAt))
    wrapper.unmount()
  })

  it('uses createdAt for queued runs and shows — for their duration', async () => {
    const { wrapper } = await setup()
    const row = rowOf(wrapper, '排队中的运行')
    expect(row.get('[data-testid="run-cell-start"]').text()).toBe(relTime(QUEUED.createdAt!))
    expect(row.findAll('td')[3]!.text()).toBe('—')
    wrapper.unmount()
  })
})

describe('RunListView row ⋯ menu (cancel / delete)', () => {
  it('reveals the more button on row hover only', async () => {
    const { wrapper } = await setup()
    const row = rowOf(wrapper, '修复回归测试')
    expect(row.classes()).toContain('group')
    const more = row.get('[data-testid="run-more-btn"]')
    expect(more.classes()).toEqual(expect.arrayContaining(['opacity-0', 'group-hover:opacity-100', 'focus-visible:opacity-100']))
    expect(more.attributes('aria-haspopup')).toBe('menu')
    expect(more.attributes('aria-label')).toBe(zh.moreActions)
    // No red inline buttons in the row any more.
    expect(row.find('[data-testid="cancel-run-btn"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('offers 取消运行 for an in-flight run, confirms, then cancels without opening the run', async () => {
    const { wrapper, router } = await setup()
    const row = rowOf(wrapper, '修复回归测试')
    const more = row.get('[data-testid="run-more-btn"]')
    await more.trigger('click')
    expect(more.attributes('aria-expanded')).toBe('true')
    expect(more.classes()).toContain('opacity-100')
    const menu = wrapper.get('[data-testid="run-row-menu"]')
    expect(menu.attributes('role')).toBe('menu')
    expect(menu.find('[data-testid="delete-run-btn"]').exists()).toBe(false)
    const cancel = menu.get('[data-testid="cancel-run-btn"]')
    expect(cancel.text()).toBe(zh.cancelRun)
    expect(cancel.classes()).toContain('text-err')
    await cancel.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(false)
    expect(mocks.cancelRun).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-cancel-run-btn"]').trigger('click')
    await flushPromises()
    expect(mocks.cancelRun).toHaveBeenCalledWith('run-aaa111')
    expect(router.currentRoute.value.path).toBe('/runs')
    wrapper.unmount()
  })

  it('offers 删除运行 for a finished run and deletes after confirmation', async () => {
    const { wrapper, router } = await setup()
    await rowOf(wrapper, '整理发布说明').get('[data-testid="run-more-btn"]').trigger('click')
    const menu = wrapper.get('[data-testid="run-row-menu"]')
    expect(menu.find('[data-testid="cancel-run-btn"]').exists()).toBe(false)
    const del = menu.get('[data-testid="delete-run-btn"]')
    expect(del.text()).toBe(zh.deleteRun)
    await del.trigger('click')
    await flushPromises()
    expect(mocks.deleteRun).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-delete-run-btn"]').trigger('click')
    await flushPromises()
    expect(mocks.deleteRun).toHaveBeenCalledWith('run-bbb222')
    expect(router.currentRoute.value.path).toBe('/runs')
    wrapper.unmount()
  })

  it('toggles from the button and closes on an outside click or Escape', async () => {
    const { wrapper } = await setup()
    const more = rowOf(wrapper, '整理发布说明').get('[data-testid="run-more-btn"]')
    await more.trigger('click')
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(true)
    await more.trigger('click')
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(false)
    await more.trigger('click')
    document.body.click()
    await flushPromises()
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(false)
    await more.trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('closes on any scroll so the fixed menu never floats away from its row', async () => {
    const { wrapper } = await setup()
    const more = rowOf(wrapper, '整理发布说明').get('[data-testid="run-more-btn"]')
    await more.trigger('click')
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(true)
    // Capture phase: a scroll inside the list container also closes it.
    document.body.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.find('[data-testid="run-row-menu"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
