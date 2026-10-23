// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'

const listSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'SidebarWorkspaceLists.vue'), 'utf8')
function styleBlock(selector: string): string {
  const style = listSrc.slice(listSrc.indexOf('<style scoped>'))
  const found = style.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`, 's'))
  expect(found, selector).not.toBeNull()
  return found![0]
}
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  const signedInWorkspace = { name: 'Workspace', description: '', variables: [] }
  return { signedInWorkspace, workspace: ref<typeof signedInWorkspace | null>(signedInWorkspace), entries: ref<{ workflowId: string; favoritedAt: number }[]>([]), listWorkflowCatalog: vi.fn(), listRuns: vi.fn(), getWorkflow: vi.fn(), openLaunch: vi.fn(), error: vi.fn() }
})
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, loading: ref(false), ensureLoaded: async () => mocks.workspace.value }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ error: mocks.error }) }))
vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({ useWorkflowRunLaunch: () => ({ openLaunch: mocks.openLaunch }) }))
vi.mock('@/lib/run/useWorkflowFavorites', () => ({ useWorkflowFavorites: () => ({ entries: mocks.entries, isFavorite: (id: string) => mocks.entries.value.some((item) => item.workflowId === id), toggleFavorite: (id: string) => { mocks.entries.value = mocks.entries.value.some((item) => item.workflowId === id) ? mocks.entries.value.filter((item) => item.workflowId !== id) : [{ workflowId: id, favoritedAt: Date.now() }, ...mocks.entries.value] } }) }))
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import { RECENT_POLL_MS, RECENT_RELAY_DEBOUNCE_MS, notifyRecentActivity } from '@/lib/run/recentRuns'
import { __resetSidebarSectionsForTests } from '@/lib/shared/sidebarSections'
const catalog = Array.from({ length: 14 }, (_, index) => ({ id: `wf-${index}`, name: `流程 ${index}`, description: `说明 ${index}`, status: index % 2 ? 'draft' : 'published', version: 2, publishedVersion: 1, updatedAt: `2026-10-${String(20 - index).padStart(2, '0')}` }))
async function setup(path = '/workflows/wf-3/edit') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common } } })
  const wrapper = mount(SidebarWorkspaceLists, { attachTo: document.body, global: { plugins: [router, i18n], directives: { 'hover-ink': vHoverInk } } })
  await flushPromises()
  return { wrapper, router }
}

const cur = (id: string, kind: string, mode: string, state: string) => ({ id, name: id, iteration: 1, kind, mode, state })
const r = (id: string, currentNode?: ReturnType<typeof cur>, status = 'running') => ({ id, title: `运行 ${id}`, workflowName: 'wf', status, currentNode })
const order = (w: { findAll: (s: string) => { attributes: (n: string) => string | undefined }[] }) => w.findAll('[data-testid="sidebar-recent-run"]').map((x) => x.attributes('data-run-id'))
beforeEach(() => {
  vi.clearAllMocks()
  __resetSidebarSectionsForTests()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  mocks.workspace.value = mocks.signedInWorkspace
  mocks.entries.value = []
  mocks.listWorkflowCatalog.mockResolvedValue([])
  mocks.listRuns.mockResolvedValue({ items: [r('a', cur('impl', 'agent', 'auto', 'running')), r('b', cur('gate', 'gate', 'gate', 'waiting_human'), 'waiting_human'), r('c', undefined, 'completed')], total: 3 })
})
afterEach(() => { vi.useRealTimers(); document.body.replaceChildren() })

describe('sidebar 历史执行 (finished runs, activity order, targets)', () => {
  it('requests finished runs only, in last_activity order, under the 历史执行 heading', async () => {
    mocks.listRuns.mockResolvedValue({ items: [r('c', undefined, 'completed'), r('d', undefined, 'failed'), r('e', undefined, 'cancelled')], total: 3 })
    const { wrapper } = await setup('/')
    expect(mocks.listRuns).toHaveBeenCalledWith(
      expect.objectContaining({ status: 'completed,failed,cancelled', sort: 'last_activity', order: 'desc' }),
    )
    expect(wrapper.get('[data-testid="sidebar-recent"] h3').text()).toBe('历史执行')
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(order(wrapper)).toEqual(['c', 'd', 'e'])
    expect(rows[0].attributes('href')).toBe('/runs/c')
    expect(rows[0].find('[data-testid="sidebar-recent-ended"]').exists()).toBe(false)
    expect(rows[1].find('[data-testid="sidebar-recent-ended"]').exists()).toBe(true)
    expect(rows[1].find('[data-testid="sidebar-recent-ended"]').classes()).toContain('is-failed')
    expect(rows[1].get('[data-testid="sidebar-recent-ended"]').attributes('title')).toBe('失败')
    expect(rows[2].find('[data-testid="sidebar-recent-ended"]').exists()).toBe(true)
    expect(rows[2].find('[data-testid="sidebar-recent-ended"]').classes()).toContain('is-cancelled')
    expect(rows[2].find('[data-testid="sidebar-recent-ended"]').attributes('title')).toBe('已取消')
    expect(wrapper.find('[data-testid="sidebar-recent-type-icon"]').exists()).toBe(false)
    for (const row of rows) {
      expect(row.find('[data-icon="history"]').exists()).toBe(false)
      const content = [...row.element.children].find((el) => !el.classList.contains('hover-ink'))
      expect(content).toBe(row.get('.workspace-list-title').element)
      const ended = row.find('[data-testid="sidebar-recent-ended"]')
      if (ended.exists()) expect(row.element.lastElementChild).toBe(ended.element)
      expect(row.find('[data-icon="chat"]').exists()).toBe(false)
      expect(row.find('[data-testid="nav-pending-type-icon"]').exists()).toBe(false)
      expect(row.find('[data-testid="nav-pending-pin"]').exists()).toBe(false)
    }
    wrapper.unmount()
  })

  it('marks the current history row and aligns the row box and landing ink with 正在执行', async () => {
    mocks.listRuns.mockResolvedValue({ items: [r('c', undefined, 'completed'), r('d', undefined, 'failed'), r('e', undefined, 'cancelled')], total: 3 })
    const { wrapper } = await setup('/runs/c')
    const rows = wrapper.findAll('[data-testid="sidebar-recent-run"]')
    expect(rows[0].classes()).toContain('is-active')
    expect(rows[0].attributes('aria-current')).toBe('page')
    expect(rows[1].classes()).not.toContain('is-active')
    expect(rows[1].attributes('aria-current')).toBeUndefined()
    expect(listSrc).toMatch(/<RouterLink\s+v-hover-ink\b/)
    expect(listSrc).not.toMatch(/contextmenu|nav-pending-pin|beginRename/)
    expect(rows[0].classes()).toContain('hover-ink-host')
    expect(rows[0].find('.hover-ink').exists()).toBe(true)
    expect(rows[0].element.firstElementChild?.classList.contains('hover-ink')).toBe(true)
    expect(styleBlock('\\.workspace-list-heading')).toContain('color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3)))')
    const row = styleBlock('\\.workspace-recent-row')
    expect(row).toContain('color: rgb(var(--c-txt2))')
    expect(row).toContain('min-height: 36px')
    expect(row).toContain('border-radius: 10px')
    expect(row).toContain('padding: 8px 10px')
    expect(row).toContain('gap: 10px')
    expect(row).toContain('margin-bottom: 2px')
    expect(row).toContain('font-size: 13px')
    expect(row).toContain('--hover-ink-color: rgb(var(--c-txt) / .04)')
    expect(row).not.toContain('background')
    expect(styleBlock('\\.sidebar-tree-item > \\.workspace-recent-row')).toContain('padding-left: 6px')
    const hover = styleBlock('\\.workspace-recent-row:hover')
    expect(hover).toContain('color: rgb(var(--c-txt))')
    expect(hover).not.toContain('background')
    const active = styleBlock('\\.workspace-recent-row\\.is-active, \\.workspace-recent-row\\.is-active:hover')
    expect(active).toContain('color: rgb(var(--c-txt))')
    expect(active).toContain('light-dark(#f1f1f1, rgb(var(--c-txt) / .065))')
    expect(styleBlock('\\.workspace-list-title')).toContain('font-weight: 400')
    expect(styleBlock('\\.workspace-list-title')).toContain('font-size: 13px')
    expect(styleBlock('\\.workspace-recent-ended\\.is-failed')).toContain('light-dark(#d17a7a, #e08a8a)')
    expect(styleBlock('\\.workspace-recent-ended\\.is-cancelled')).toContain('light-dark(#b4b4bb, #6f6f78)')
    expect(wrapper.find('[data-testid="sidebar-recent-type-icon"]').exists()).toBe(false)
    expect(rows.every((item) => item.element.parentElement?.classList.contains('sidebar-tree-item'))).toBe(true)
    wrapper.unmount()
  })

  it('a poll reorders the list without touching focus, text or selection in the composer', async () => {
    const composer = document.createElement('textarea')
    document.body.append(composer)
    const { wrapper } = await setup('/gates?run=b&node=gate')
    composer.focus()
    composer.value = '正在输入的草稿'
    composer.setSelectionRange(2, 4)
    mocks.listRuns.mockResolvedValue({ items: [r('c', cur('impl', 'agent', 'auto', 'running')), r('a', undefined, 'completed'), r('b', cur('gate', 'gate', 'gate', 'waiting_human'), 'waiting_human')], total: 3 })
    vi.advanceTimersByTime(RECENT_POLL_MS)
    await flushPromises()
    expect(mocks.listRuns).toHaveBeenCalledTimes(2)
    expect(order(wrapper)).toEqual(['c', 'a', 'b'])
    expect(document.activeElement).toBe(composer)
    expect(composer.value).toBe('正在输入的草稿')
    expect([composer.selectionStart, composer.selectionEnd]).toEqual([2, 4])
    wrapper.unmount()
  })

  it('holds a reorder while a recent row has focus or the pointer, then applies it', async () => {
    const { wrapper } = await setup('/')
    const first = wrapper.findAll('[data-testid="sidebar-recent-run"]')[0]
    ;(first.element as HTMLElement).focus()
    mocks.listRuns.mockResolvedValue({ items: [r('b', cur('gate', 'gate', 'gate', 'waiting_human'), 'waiting_human'), r('a', cur('impl', 'agent', 'auto', 'running'))], total: 2 })
    vi.advanceTimersByTime(RECENT_POLL_MS)
    await flushPromises()
    expect(order(wrapper)).toEqual(['a', 'b', 'c'])
    expect(document.activeElement).toBe(first.element)
    const list = first.element.closest('[data-testid="sidebar-recent-list"]') as HTMLElement
    list.dispatchEvent(new Event('pointerenter'))
    ;(first.element as HTMLElement).blur()
    list.dispatchEvent(new FocusEvent('focusout'))
    await flushPromises()
    expect(order(wrapper)).toEqual(['a', 'b', 'c'])
    list.dispatchEvent(new Event('pointerleave'))
    await flushPromises()
    expect(order(wrapper)).toEqual(['b', 'a'])
    wrapper.unmount()
  })

  it('refreshes on a local activity relay and skips polls while hidden', async () => {
    const { wrapper } = await setup('/')
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    vi.advanceTimersByTime(RECENT_POLL_MS)
    await flushPromises()
    expect(mocks.listRuns).toHaveBeenCalledTimes(1)
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    notifyRecentActivity()
    await new Promise((res) => setTimeout(res, RECENT_RELAY_DEBOUNCE_MS + 20))
    await flushPromises()
    expect(mocks.listRuns).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('shows failed and cancelled end icons after a relay without remounting', async () => {
    mocks.listRuns.mockResolvedValue({ items: [r('c', undefined, 'completed')], total: 1 })
    const { wrapper } = await setup('/')
    const list = wrapper.get('[data-testid="sidebar-recent"]')
    expect(list.find('[data-testid="sidebar-recent-ended"]').exists()).toBe(false)
    mocks.listRuns.mockResolvedValue({
      items: [r('d', undefined, 'failed'), r('e', undefined, 'cancelled'), r('c', undefined, 'completed')],
      total: 3,
    })
    notifyRecentActivity()
    await new Promise((res) => setTimeout(res, RECENT_RELAY_DEBOUNCE_MS + 30))
    await flushPromises()
    const ended = (id: string) => wrapper.get(`[data-run-id="${id}"]`).find('[data-testid="sidebar-recent-ended"]')
    expect(ended('d').classes()).toContain('is-failed')
    expect(ended('e').classes()).toContain('is-cancelled')
    expect(ended('c').exists()).toBe(false)
    expect(list.element.isConnected).toBe(true)
    wrapper.unmount()
  })
})
