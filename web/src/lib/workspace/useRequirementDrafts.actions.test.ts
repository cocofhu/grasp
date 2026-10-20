// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { RequirementDraft } from '@/lib/shared/types'

const shared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return { isMobile: ref(false) }
})

const mocks = vi.hoisted(() => ({
  listRequirementDrafts: vi.fn(),
  createRequirementDraft: vi.fn(),
  updateRequirementDraft: vi.fn(),
  patchRequirementDraftStatus: vi.fn(),
  patchRequirementDraftSchedule: vi.fn(),
  deleteRequirementDraft: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastShow: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listRequirementDrafts: mocks.listRequirementDrafts,
      createRequirementDraft: mocks.createRequirementDraft,
      updateRequirementDraft: mocks.updateRequirementDraft,
      patchRequirementDraftStatus: mocks.patchRequirementDraftStatus,
      patchRequirementDraftSchedule: mocks.patchRequirementDraftSchedule,
      deleteRequirementDraft: mocks.deleteRequirementDraft,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    error: mocks.toastError,
    show: mocks.toastShow,
    warn: vi.fn(),
    info: vi.fn(),
  }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: shared.isMobile }),
}))

import { useRequirementDrafts } from './useRequirementDrafts'

const draft = (id: string, over: Partial<RequirementDraft> = {}): RequirementDraft =>
  ({
    id,
    projectId: 'proj-1',
    title: `Draft ${id}`,
    bodyMarkdown: '# body',
    status: 'open',
    kind: 'requirement',
    startAt: '2026-01-01',
    dueAt: '2026-01-10',
    progress: 20,
    parentId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-02T00:00:00Z',
    ...over,
  }) as RequirementDraft

const D1 = draft('d1')
const D2 = draft('d2', { title: 'Child', parentId: 'd1', startAt: '', dueAt: '' })
const M1 = draft('m1', { kind: 'milestone', dueAt: '2026-02-01', startAt: '' })

async function withDrafts(projectId = 'proj-1') {
  let detail!: ReturnType<typeof useRequirementDrafts>
  const props = { projectId }
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      detail = useRequirementDrafts(props)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  await flushPromises()
  await nextTick()
  return { detail, app, props }
}

describe('useRequirementDrafts actions coverage', () => {
  beforeEach(() => {
    shared.isMobile.value = false
    for (const fn of Object.values(mocks)) fn.mockReset()
    mocks.listRequirementDrafts.mockResolvedValue({ items: [D1, D2, M1] })
    mocks.createRequirementDraft.mockResolvedValue(draft('new'))
    mocks.updateRequirementDraft.mockImplementation(async (_p, id, body) =>
      draft(id, { title: body.title, bodyMarkdown: body.bodyMarkdown }),
    )
    mocks.patchRequirementDraftStatus.mockResolvedValue(draft('d1', { status: 'done' }))
    mocks.patchRequirementDraftSchedule.mockImplementation(async (_p, id, body) =>
      draft(id, body),
    )
    mocks.deleteRequirementDraft.mockResolvedValue({ status: 'deleted' })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads, derives schedule views, and handles selection guards', async () => {
    const { detail, app } = await withDrafts()
    expect(detail.items.value).toHaveLength(3)
    expect(detail.timelineDrafts.value.map((d) => d.id)).toEqual(['d1', 'm1'])
    expect(detail.milestoneItems.value[0]?.id).toBe('m1')
    expect(detail.todayLineStyle.value.left).toBeTruthy()
    expect(detail.lineNumbers.value).toEqual([1])
    expect(detail.hasSelection.value).toBe(false)

    detail.selectDraft('missing')
    expect(detail.selectedId.value).toBeNull()
    detail.selectDraft('d1')
    expect(detail.selectedDraft.value?.id).toBe('d1')
    expect(detail.parentOptions.value.map((d) => d.id)).not.toContain('d1')
    expect(detail.canEditParent.value).toBe(false)
    expect(detail.rowBarStyle(detail.ganttRows.value.scheduled[0]!)).toBeTruthy()
    expect(detail.rowBarStyle({ draft: D2 } as never)).toBeNull()
    expect(detail.isRowSelected('d1')).toBe(true)
    expect(detail.kindLabel('milestone')).not.toBe(detail.kindLabel('requirement'))

    detail.query.value = 'child'
    await detail.loadList({ keepSelection: true })
    expect(detail.contextualDisplay.value.length).toBeGreaterThan(0)
    detail.setViewMode('milestones')
    detail.setGanttScale('month')
    expect(detail.viewMode.value).toBe('milestones')
    expect(detail.ganttScale.value).toBe('month')

    app.unmount()
  })

  it('confirms dirty navigation and creation, including validation and failures', async () => {
    const { detail, app } = await withDrafts()
    detail.selectDraft('d1')
    expect(await detail.requestLeave()).toBe(true)
    detail.editBody.value = 'changed'

    const first = detail.requestLeave()
    const second = detail.requestLeave()
    expect(detail.showLeave.value).toBe(true)
    detail.resolveLeave(false)
    expect(await first).toBe(false)
    expect(await second).toBe(false)

    const accepted = detail.requestLeave()
    detail.resolveLeave(true)
    expect(await accepted).toBe(true)
    expect(detail.editBody.value).toBe(detail.savedBody.value)

    detail.openNewModal()
    detail.newModalKind.value = 'milestone'
    await detail.onConfirmCreate()
    expect(detail.newModalError.value).toBeTruthy()
    expect(mocks.createRequirementDraft).not.toHaveBeenCalled()

    detail.newModalDueAt.value = '2026-03-01'
    mocks.createRequirementDraft.mockResolvedValueOnce(M1)
    await detail.onConfirmCreate()
    expect(mocks.createRequirementDraft).toHaveBeenCalledWith('proj-1', {
      kind: 'milestone',
      dueAt: '2026-03-01',
    })
    expect(detail.showNewModal.value).toBe(false)

    detail.openNewModal()
    detail.newModalKind.value = 'requirement'
    detail.viewMode.value = 'milestones'
    await detail.onConfirmCreate()
    expect(detail.viewMode.value).toBe('edit')

    detail.openNewModal()
    mocks.createRequirementDraft.mockRejectedValueOnce(new Error('create down'))
    await detail.onConfirmCreate()
    expect(mocks.toastError).toHaveBeenCalledWith('create down')
    detail.closeNewModal()

    detail.selectDraft('d1')
    detail.editBody.value = 'dirty'
    const click = detail.onNewClick()
    detail.resolveLeave(false)
    await click
    expect(detail.showNewModal.value).toBe(false)
    detail.creating.value = true
    await detail.onNewClick()

    app.unmount()
  })

  it('saves content and covers validation, no-op, busy, and rejection paths', async () => {
    const { detail, app } = await withDrafts()
    await detail.onSave()
    detail.selectDraft('d1')
    await detail.onSave()
    expect(mocks.toastShow).toHaveBeenCalled()

    detail.editTitle.value = '   '
    await detail.onSave()
    expect(detail.titleError.value).toBeTruthy()

    detail.editTitle.value = '  Renamed  '
    detail.editBody.value = 'new body'
    await detail.onSave()
    expect(mocks.updateRequirementDraft).toHaveBeenCalledWith('proj-1', 'd1', {
      title: 'Renamed',
      bodyMarkdown: 'new body',
    })
    expect(detail.isDirty.value).toBe(false)

    detail.editBody.value = 'will fail'
    mocks.updateRequirementDraft.mockRejectedValueOnce(new Error('save down'))
    await detail.onSave()
    expect(mocks.toastError).toHaveBeenCalledWith('save down')
    detail.saving.value = true
    mocks.updateRequirementDraft.mockClear()
    await detail.onSave()
    expect(mocks.updateRequirementDraft).not.toHaveBeenCalled()

    app.unmount()
  })

  it('patches every schedule field and maps backend errors while reverting inputs', async () => {
    const { detail, app } = await withDrafts()
    detail.selectDraft('d1')

    detail.editKind.value = 'milestone'
    detail.onScheduleKindChange()
    await flushPromises()
    detail.editStartAt.value = '2026-01-02'
    detail.onScheduleStartChange()
    await flushPromises()
    detail.editDueAt.value = '2026-01-12'
    detail.onScheduleDueChange()
    await flushPromises()
    detail.editProgress.value = 200
    detail.onScheduleProgressChange()
    await flushPromises()
    detail.editParentId.value = 'm1'
    detail.onScheduleParentChange()
    await flushPromises()
    expect(mocks.patchRequirementDraftSchedule).toHaveBeenCalledTimes(5)
    expect(detail.editProgress.value).toBeLessThanOrEqual(100)

    for (const message of [
      'due before start',
      'invalid date yyyy-mm-dd',
      'milestone due required',
      'invalid parent',
      'has children',
      'kind needs date',
      'invalid progress',
      'invalid kind',
      '',
    ]) {
      mocks.patchRequirementDraftSchedule.mockRejectedValueOnce(new Error(message))
      detail.editDueAt.value = 'bad'
      await detail.patchSchedule({ dueAt: 'bad' })
      expect(detail.scheduleError.value).toBeTruthy()
      expect(detail.editDueAt.value).toBe('2026-01-10')
    }

    detail.selectedId.value = null
    mocks.patchRequirementDraftSchedule.mockClear()
    await detail.patchSchedule({})
    detail.scheduleBusy.value = true
    detail.selectedId.value = 'd1'
    await detail.patchSchedule({})
    expect(mocks.patchRequirementDraftSchedule).not.toHaveBeenCalled()

    app.unmount()
  })

  it('toggles status, deletes, reloads, and reports all API errors', async () => {
    const { detail, app } = await withDrafts()
    detail.selectDraft('d1')
    await detail.onToggleStatus()
    expect(mocks.patchRequirementDraftStatus).toHaveBeenCalledWith('proj-1', 'd1', 'done')
    expect(mocks.toastSuccess).toHaveBeenCalled()

    mocks.patchRequirementDraftStatus.mockResolvedValueOnce(draft('d1', { status: 'open' }))
    await detail.onToggleStatus()
    expect(detail.selectedStatus.value).toBe('open')
    mocks.patchRequirementDraftStatus.mockRejectedValueOnce(new Error('status down'))
    await detail.onToggleStatus()
    expect(mocks.toastError).toHaveBeenCalledWith('status down')

    detail.statusBusy.value = true
    mocks.patchRequirementDraftStatus.mockClear()
    await detail.onToggleStatus()
    expect(mocks.patchRequirementDraftStatus).not.toHaveBeenCalled()
    detail.statusBusy.value = false

    mocks.deleteRequirementDraft.mockRejectedValueOnce(new Error('delete down'))
    await detail.onConfirmDelete()
    expect(mocks.toastError).toHaveBeenCalledWith('delete down')
    await detail.onConfirmDelete()
    expect(detail.selectedId.value).toBeNull()
    await detail.onConfirmDelete()

    mocks.listRequirementDrafts.mockRejectedValueOnce(new Error('list down'))
    await detail.loadList()
    expect(detail.items.value).toEqual([])
    expect(mocks.toastError).toHaveBeenCalledWith('list down')

    app.unmount()
  })

  it('edits markdown, find state, scrolling, keyboard shortcuts, and desktop sash', async () => {
    const { detail, app } = await withDrafts()
    detail.selectDraft('d1')
    const ta = document.createElement('textarea')
    ta.value = detail.editBody.value
    ta.selectionStart = 0
    ta.selectionEnd = 0
    ta.focus = vi.fn()
    ta.setSelectionRange = vi.fn()
    const hl = document.createElement('div')
    const gutter = document.createElement('div')
    const preview = document.createElement('div')
    Object.defineProperties(ta, {
      scrollHeight: { value: 200 },
      clientHeight: { value: 100 },
    })
    Object.defineProperties(preview, {
      scrollHeight: { value: 300 },
      clientHeight: { value: 100 },
    })
    detail.srcEl.value = ta
    detail.hlEl.value = hl
    detail.gutterEl.value = gutter
    detail.previewEl.value = preview

    detail.applyInsert('bold')
    await nextTick()
    expect(detail.editBody.value).toContain('**')
    detail.onSrcKeydown(new KeyboardEvent('keydown', { key: 'i', ctrlKey: true }))
    detail.onSrcKeydown(new KeyboardEvent('keydown', { key: 'b', metaKey: true }))
    detail.onSrcKeydown(new KeyboardEvent('keydown', { key: 'f', ctrlKey: true }))
    await nextTick()
    expect(detail.findOpen.value).toBe(true)
    detail.onSrcKeydown(new KeyboardEvent('keydown', { key: 'x', ctrlKey: true }))
    detail.onSrcKeydown(new KeyboardEvent('keydown', { key: 'b' }))

    detail.findQuery.value = 'body'
    detail.runFindNext()
    detail.findQuery.value = 'absent'
    detail.runFindNext()
    expect(mocks.toastShow).toHaveBeenCalled()
    detail.onFindInputKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    detail.onFindInputKeydown(new KeyboardEvent('keydown', { key: 'x' }))
    detail.closeFind()

    ta.scrollTop = 20
    ta.scrollLeft = 4
    detail.syncOverlayScroll()
    expect(hl.scrollTop).toBe(20)
    expect(gutter.scrollTop).toBe(20)
    detail.onSrcScroll()
    detail.onPreviewScroll()
    detail.previewCollapsed.value = true
    detail.onSrcScroll()
    detail.onPreviewScroll()
    detail.previewCollapsed.value = false

    const split = document.createElement('div')
    split.getBoundingClientRect = () => ({ left: 10, width: 200 }) as DOMRect
    detail.splitEl.value = split
    detail.onSashDown(new MouseEvent('mousedown', { clientX: 50 }))
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 190 }))
    expect(detail.splitRatio.value).toBeGreaterThan(0.5)
    document.dispatchEvent(new MouseEvent('mouseup'))
    expect(detail.sashDragging.value).toBe(false)
    split.getBoundingClientRect = () => ({ left: 0, width: 0 }) as DOMRect
    detail.onSashDown(new MouseEvent('mousedown'))
    document.dispatchEvent(new MouseEvent('mousemove'))
    document.dispatchEvent(new MouseEvent('mouseup'))

    detail.togglePreviewCollapsed()
    expect(detail.previewCollapsed.value).toBe(true)
    shared.isMobile.value = true
    await nextTick()
    expect(detail.mobilePane.value).toBe('src')
    detail.togglePreviewCollapsed()
    detail.onSashDown(new MouseEvent('mousedown'))
    expect(detail.sourceWidthStyle.value.width).toBe('100%')
    detail.mobilePane.value = 'prev'
    expect(detail.showSplitPreview.value).toBe(true)
    expect(detail.showSplitSource.value).toBe(false)

    detail.editTitle.value = 'dirty'
    const unload = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent
    detail.onBeforeUnload(unload)
    expect(unload.defaultPrevented).toBe(true)
    detail.onDetailKeydown(new KeyboardEvent('keydown', { key: 's', ctrlKey: true }))
    detail.onDetailKeydown(new KeyboardEvent('keydown', { key: 'x' }))
    await flushPromises()

    app.unmount()
  })

  it('debounces search and respects dirty selection/open-body decisions', async () => {
    vi.useFakeTimers()
    const { detail, app } = await withDrafts()
    detail.setFilter('open')
    detail.setFilter('all')
    detail.onSearchInput()
    detail.onSearchInput()
    await vi.advanceTimersByTimeAsync(200)
    expect(mocks.listRequirementDrafts).toHaveBeenCalled()

    detail.selectDraft('d1')
    await detail.onPickDraft('d1')
    detail.editBody.value = 'dirty'
    const pick = detail.onPickDraft('d2')
    detail.resolveLeave(false)
    await pick
    expect(detail.selectedId.value).toBe('d1')
    const gantt = detail.onSelectFromGantt('d2')
    detail.resolveLeave(true)
    await gantt
    expect(detail.selectedId.value).toBe('d2')

    detail.editBody.value = 'dirty again'
    const body = detail.openBody('d1')
    detail.resolveLeave(false)
    await body
    expect(detail.selectedId.value).toBe('d2')
    await detail.openBody()
    expect(detail.viewMode.value).toBe('edit')
    detail.selectedId.value = null
    await detail.openBody()

    app.unmount()
    vi.useRealTimers()
  })
})
