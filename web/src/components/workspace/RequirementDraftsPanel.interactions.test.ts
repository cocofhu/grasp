// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), status: vi.fn(), schedule: vi.fn(), del: vi.fn(),
  success: vi.fn(), error: vi.fn(), show: vi.fn(),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    listRequirementDrafts: mocks.list, createRequirementDraft: mocks.create,
    updateRequirementDraft: mocks.update, patchRequirementDraftStatus: mocks.status,
    patchRequirementDraftSchedule: mocks.schedule, deleteRequirementDraft: mocks.del,
  } }
})
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))
import RequirementDraftsPanel from './RequirementDraftsPanel.vue'

const req: any = { id: 'r1', title: 'Requirement', bodyMarkdown: '# Body\ntext', status: 'open', kind: 'requirement', startAt: '2026-09-01', dueAt: '2026-09-10', progress: 25, parentId: null, createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-02T00:00:00Z' }
const child: any = { ...req, id: 'r2', title: 'Child', parentId: 'r1', startAt: '', dueAt: '' }
const milestone: any = { ...req, id: 'm1', title: 'Milestone', kind: 'milestone', startAt: '', dueAt: '2026-09-20', progress: 50 }
function mountPanel() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(RequirementDraftsPanel, {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: {
      AppButton: { props: ['disabled'], template: '<button v-bind="$attrs" :disabled="disabled"><slot/></button>' },
      AppModal: { props: ['open'], emits: ['close'], template: '<div v-if="open" data-testid="visible-modal"><button data-testid="modal-close" @click="$emit(\'close\')"/><slot/><slot name="footer"/></div>' },
    } },
  })
}

describe('RequirementDraftsPanel interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return Math.random() })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    mocks.list.mockResolvedValue({ items: [req, child, milestone] })
    mocks.create.mockResolvedValue(req); mocks.update.mockResolvedValue(req)
    mocks.status.mockResolvedValue({ ...req, status: 'done' }); mocks.schedule.mockResolvedValue(req); mocks.del.mockResolvedValue(undefined)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders scheduled, unscheduled, milestone and edit branches', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="requirement-drafts-gantt"]').exists()).toBe(true)
    const vm = w.vm as any
    vm.selectDraft('r1')
    vm.setViewMode('gantt')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="requirement-drafts-inspector"]').text()).toContain('Requirement')
    vm.setViewMode('milestones')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="requirement-drafts-milestone-m1"]').exists()).toBe(true)
    await w.get('[data-testid="requirement-drafts-milestone-m1"]').trigger('click')
    await w.get('[data-testid="requirement-drafts-open-body"]').trigger('click')
    expect(vm.viewMode).toBe('edit')
    w.unmount()
  })

  it('covers save/status/delete success and failure guards', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.selectDraft('r1')
    await vm.onSave()
    expect(mocks.show).toHaveBeenCalled()
    vm.editTitle = 'Changed'; vm.editBody = 'new'
    mocks.update.mockResolvedValueOnce({ ...req, title: 'Changed', bodyMarkdown: 'new' })
    await vm.onSave()
    expect(mocks.update).toHaveBeenCalled()
    vm.editTitle = 'Again'; mocks.update.mockRejectedValueOnce(new Error('save boom'))
    await vm.onSave()
    expect(mocks.error).toHaveBeenCalledWith('save boom')
    mocks.status.mockRejectedValueOnce(new Error('status boom'))
    await vm.onToggleStatus()
    vm.showDelete = true
    mocks.del.mockRejectedValueOnce(new Error('delete boom'))
    await vm.onConfirmDelete()
    await vm.onConfirmDelete()
    expect(vm.selectedId).toBeNull()
    expect(mocks.success).toHaveBeenCalled()
    w.unmount()
  })

  it('creates both kinds, validates milestone date, and reports create errors', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.openNewModal(); vm.newModalKind = 'milestone'
    await vm.onConfirmCreate()
    expect(vm.newModalError).toBeTruthy()
    vm.newModalDueAt = '2026-10-01'
    mocks.create.mockResolvedValueOnce({ ...milestone, id: 'm2', dueAt: '2026-10-01' })
    await vm.onConfirmCreate()
    expect(mocks.create).toHaveBeenCalledWith({ kind: 'milestone', dueAt: '2026-10-01' })
    vm.openNewModal(); mocks.create.mockRejectedValueOnce(new Error('create boom'))
    await vm.onConfirmCreate()
    expect(mocks.error).toHaveBeenCalledWith('create boom')
    vm.closeNewModal()
    w.unmount()
  })

  it('maps every schedule error and exercises schedule guards and clamping', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    for (const [message, expected] of [
      ['due before start', '截止'], ['invalid date', '日期'], ['milestone due missing', '里程碑'],
      ['invalid parent', '父'], ['has children', '子'], ['kind date', '里程碑'],
      ['invalid progress', '进度'], ['invalid kind', '类型'],
    ]) expect(vm.mapScheduleApiError(message)).toContain(expected)
    await vm.patchSchedule({ progress: 1 })
    vm.selectDraft('r1')
    mocks.schedule.mockRejectedValueOnce(new Error('invalid progress'))
    vm.editProgress = 500
    vm.onScheduleProgressChange()
    await flushPromises()
    expect(mocks.schedule).toHaveBeenCalledWith('r1', { progress: 100 })
    vm.editKind = 'milestone'; vm.onScheduleKindChange()
    vm.editStartAt = '2026-01-01'; vm.onScheduleStartChange()
    vm.editDueAt = '2026-02-01'; vm.onScheduleDueChange()
    vm.editParentId = 'm1'; vm.onScheduleParentChange()
    await flushPromises()
    w.unmount()
  })

  it('handles find, markdown shortcuts, scroll syncing, sash dragging and unload', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.selectDraft('r1'); vm.setViewMode('edit'); await w.vm.$nextTick()
    const ta = w.get('[data-testid="requirement-drafts-body"]').element as HTMLTextAreaElement
    ta.setSelectionRange(0, 0)
    vm.onSrcKeydown(new KeyboardEvent('keydown', { key: 'b', ctrlKey: true }))
    vm.onSrcKeydown(new KeyboardEvent('keydown', { key: 'i', ctrlKey: true }))
    vm.onSrcKeydown(new KeyboardEvent('keydown', { key: 'f', ctrlKey: true }))
    await w.vm.$nextTick()
    vm.findQuery = 'Body'; vm.runFindNext()
    vm.findQuery = 'absent'; vm.runFindNext()
    expect(mocks.show).toHaveBeenCalled()
    vm.onFindInputKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    vm.closeFind(); vm.togglePreviewCollapsed(); vm.togglePreviewCollapsed()
    const split = w.get('[data-testid="requirement-drafts-markdown-split"]').element as HTMLElement
    split.getBoundingClientRect = () => ({ left: 0, width: 200 } as DOMRect)
    vm.onSashDown(new MouseEvent('mousedown'))
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 150 }))
    document.dispatchEvent(new MouseEvent('mouseup'))
    expect(vm.splitRatio).toBeGreaterThan(0.5)
    const ev = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent
    window.dispatchEvent(ev)
    w.unmount()
  })

  it('resolves concurrent leave requests and reload/search failures', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.selectDraft('r1'); vm.editTitle = 'dirty'
    const one = vm.requestLeave(); const two = vm.requestLeave()
    vm.resolveLeave(false)
    expect(await one).toBe(false); expect(await two).toBe(false)
    mocks.list.mockRejectedValueOnce(new Error('load boom'))
    await vm.loadList()
    expect(vm.items).toEqual([])
    vi.useFakeTimers()
    vm.query = 'abc'; vm.onSearchInput(); vm.onSearchInput()
    vi.runAllTimers(); await flushPromises()
    vi.useRealTimers()
    expect(mocks.list).toHaveBeenCalledWith({ status: 'open', q: 'abc' })
    w.unmount()
  })

  it('renders loading, empty, editor controls and all confirmation modals', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.items = []; vm.loading = true; vm.setViewMode('edit')
    await w.vm.$nextTick()
    expect(w.text()).toContain('加载')
    vm.loading = false
    await w.vm.$nextTick()
    expect(w.find('[data-testid="requirement-drafts-list-empty"]').exists()).toBe(true)
    vm.items = [req, child, milestone]; vm.catalog = [req, child, milestone]
    vm.selectDraft('r1')
    await w.vm.$nextTick()
    for (const id of ['h1', 'h2', 'h3', 'bold', 'italic', 'ul', 'ol', 'link', 'code', 'fence', 'table']) {
      await w.get(`[data-testid="requirement-drafts-tb-${id}"]`).trigger('click')
    }
    await w.get('[data-testid="requirement-drafts-schedule-kind"]').setValue('milestone')
    await w.get('[data-testid="requirement-drafts-schedule-kind"]').trigger('change')
    await flushPromises()
    vm.showDelete = true; await w.vm.$nextTick()
    expect(w.findAll('[data-testid="visible-modal"]').length).toBeGreaterThan(0)
    await w.get('[data-testid="requirement-drafts-delete-cancel"]').trigger('click')
    vm.editTitle = 'dirty'
    const leave = vm.requestLeave(); await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-leave-confirm"]').trigger('click')
    expect(await leave).toBe(true)
    vm.openNewModal(); await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-new-kind-milestone"]').trigger('click')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="requirement-drafts-new-milestone-due"]').exists()).toBe(true)
    await w.get('[data-testid="modal-close"]').trigger('click')
    w.unmount()
  })

  it('renders gantt empty states and selected milestone inspector controls', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.items = []; vm.catalog = []; vm.setViewMode('gantt')
    await w.vm.$nextTick()
    expect(w.text()).toContain('把想法整理成下一步')
    vm.items = [milestone]; vm.catalog = [milestone]; vm.selectDraft('m1')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="requirement-drafts-inspector-kind"]').exists()).toBe(true)
    await w.get('[data-testid="requirement-drafts-inspector-kind"]').setValue('requirement')
    await w.get('[data-testid="requirement-drafts-inspector-kind"]').trigger('change')
    await flushPromises()
    vm.setViewMode('milestones'); await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-milestone-due"]').setValue('2026-10-10')
    await w.get('[data-testid="requirement-drafts-milestone-due"]').trigger('change')
    await flushPromises()
    w.unmount()
  })

  it('drives rendered toolbar, filters, schedule fields and keyboard handlers', async () => {
    const w = mountPanel(); await flushPromises()
    for (const id of ['day', 'week', 'month']) await w.get(`[data-testid="requirement-drafts-scale-${id}"]`).trigger('click')
    const row = w.find('.rd-gantt-scroll .rd-gantt-row')
    await row.trigger('click')
    await w.get('[data-testid="requirement-drafts-open-body"]').trigger('click')
    await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('Edited')
    await w.get('[data-testid="requirement-drafts-body"]').setValue('find this')
    for (const id of ['start', 'due', 'progress']) {
      const el = w.get(`[data-testid="requirement-drafts-schedule-${id}"]`)
      await el.setValue(id === 'progress' ? '70' : '2026-09-15')
      await el.trigger('change')
      await flushPromises()
    }
    if (w.find('[data-testid="requirement-drafts-schedule-parent"]').exists()) {
      await w.get('[data-testid="requirement-drafts-schedule-parent"]').setValue('m1')
      await w.get('[data-testid="requirement-drafts-schedule-parent"]').trigger('change')
      await flushPromises()
    }
    await w.get('[data-testid="requirement-drafts-tb-find"]').trigger('click')
    await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-find-input"]').setValue('find')
    await w.get('[data-testid="requirement-drafts-find-input"]').trigger('keydown', { key: 'Enter' })
    await w.get('[data-testid="requirement-drafts-find-next"]').trigger('click')
    await w.get('[data-testid="requirement-drafts-find-close"]').trigger('click')
    await w.get('[data-testid="requirement-drafts-tb-collapse"]').trigger('click')
    await w.get('[data-testid="requirement-drafts-body"]').trigger('scroll')
    await w.get('[data-testid="requirement-drafts-body"]').trigger('keydown', { key: 'i', ctrlKey: true })
    await w.get('[data-testid="requirement-drafts-sash"]').trigger('mousedown')
    await w.get('[data-testid="requirement-drafts-preview"]').trigger('scroll')
    await w.get('[data-testid="requirement-drafts-delete"]').trigger('click')
    await w.vm.$nextTick()
    await w.get('[data-testid="requirement-drafts-delete-cancel"]').trigger('click')
    await w.get('[data-testid="requirement-drafts-detail"]').trigger('keydown', { key: 's', ctrlKey: true })
    await flushPromises()
    for (const id of ['open', 'done', 'all']) {
      await w.get(`[data-testid="requirement-drafts-filter-${id}"]`).trigger('click')
      await flushPromises()
    }
    await w.get('[data-testid="requirement-drafts-search"]').setValue('needle')
    await w.get('[data-testid="requirement-drafts-search"]').trigger('input')
    w.unmount()
  })
})
