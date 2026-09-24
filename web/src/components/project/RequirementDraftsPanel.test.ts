// @vitest-environment happy-dom
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  listRequirementDrafts: vi.fn(),
  createRequirementDraft: vi.fn(),
  updateRequirementDraft: vi.fn(),
  patchRequirementDraftStatus: vi.fn(),
  patchRequirementDraftSchedule: vi.fn(),
  deleteRequirementDraft: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listRequirementDrafts: apiMocks.listRequirementDrafts,
      createRequirementDraft: apiMocks.createRequirementDraft,
      updateRequirementDraft: apiMocks.updateRequirementDraft,
      patchRequirementDraftStatus: apiMocks.patchRequirementDraftStatus,
      patchRequirementDraftSchedule: apiMocks.patchRequirementDraftSchedule,
      deleteRequirementDraft: apiMocks.deleteRequirementDraft,
    },
  }
})

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  show: vi.fn(),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => toastMocks,
}))

import RequirementDraftsPanel from './RequirementDraftsPanel.vue'

const draftOpen = {
  id: 'rd-1',
  projectId: 'proj-a',
  title: '支付失败重试',
  bodyMarkdown: '## 要点',
  status: 'open' as const,
  kind: 'requirement' as const,
  startAt: '2026-08-01',
  dueAt: '2026-08-15',
  progress: 30,
  parentId: null,
  createdAt: '2026-08-08T10:12:00Z',
  updatedAt: '2026-08-10T14:22:00Z',
}

const draftOpen2 = {
  ...draftOpen,
  id: 'rd-2',
  title: '另一条草稿',
  bodyMarkdown: '旧正文',
  startAt: '',
  dueAt: '',
  progress: 0,
}

function mockMatchMedia(isNarrow: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches: isNarrow && String(query).includes('max-width: 767px'),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
    }),
  })
}

function mountPanel() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(RequirementDraftsPanel, {
    props: { projectId: 'proj-a' },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        AppButton: defineComponent({
          props: ['disabled'],
          emits: ['click'],
          setup(_, { slots, emit, attrs }) {
            return () =>
              h(
                'button',
                {
                  type: 'button',
                  disabled: _.disabled as boolean | undefined,
                  'data-testid': attrs['data-testid'],
                  onClick: () => emit('click'),
                },
                slots.default?.(),
              )
          },
        }),
        AppModal: defineComponent({
          props: ['open', 'title'],
          emits: ['close'],
          setup(p, { slots, attrs }) {
            return () =>
              p.open
                ? h(
                    'div',
                    {
                      'data-testid':
                        attrs['data-testid'] ||
                        (String(p.title || '').includes('新建') ? 'requirement-drafts-new-modal' : 'app-modal'),
                    },
                    [h('div', p.title), slots.default?.(), slots.footer?.()],
                  )
                : null
          },
        }),
      },
    },
  })
}

async function switchToEdit(w: ReturnType<typeof mountPanel>) {
  await w.get('[data-testid="requirement-drafts-view-edit"]').trigger('click')
  await nextTick()
}

describe('RequirementDraftsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMatchMedia(false)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})
    apiMocks.listRequirementDrafts.mockResolvedValue({ items: [draftOpen, draftOpen2] })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('defaults to gantt view on mount', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="requirement-drafts-gantt"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-view-gantt"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-edit"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-milestones"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-open"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-done"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-all"]').classes()).not.toContain('on')
    expect(w.find('[data-testid="requirement-drafts-empty-detail"]').exists()).toBe(false)
    w.unmount()
  })

  it('loads open drafts by default and shows master-detail in edit view', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(apiMocks.listRequirementDrafts).toHaveBeenCalledWith('proj-a', {
      status: 'open',
      q: undefined,
    })
    expect(w.find('[data-testid="requirement-drafts-panel"]').exists()).toBe(true)
    await switchToEdit(w)
    expect(w.get('[data-testid="requirement-drafts-empty-detail"]').text()).toContain('未选中草稿')
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty(
      'value',
      '支付失败重试',
    )
    expect(w.find('[data-testid="requirement-drafts-markdown-split"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-status-pill"]').text()).toContain('未完成')
    expect(w.find('[data-testid="requirement-drafts-item-active-bar"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-toggle-status"]').text()).toContain('标记为已完成')
    w.unmount()
  })

  it('switches gantt scale day/week/month', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-scale-week"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-scale-day"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-scale-month"]').classes()).not.toContain('on')
    await w.get('[data-testid="requirement-drafts-scale-day"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-scale-day"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-scale-week"]').classes()).not.toContain('on')
    await w.get('[data-testid="requirement-drafts-scale-month"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-scale-month"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-scale-day"]').classes()).not.toContain('on')
    w.unmount()
  })

  it('switches filter segment with exclusive on class', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-filter-open"]').classes()).toContain('on')
    await w.get('[data-testid="requirement-drafts-filter-done"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-filter-done"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-open"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-all"]').classes()).not.toContain('on')
    await w.get('[data-testid="requirement-drafts-filter-all"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-filter-all"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-filter-done"]').classes()).not.toContain('on')
    w.unmount()
  })

  it('switches view segment with exclusive on class', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-view-gantt"]').classes()).toContain('on')
    await w.get('[data-testid="requirement-drafts-view-edit"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-view-edit"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-gantt"]').classes()).not.toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-milestones"]').classes()).not.toContain('on')
    await w.get('[data-testid="requirement-drafts-view-milestones"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-view-milestones"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-edit"]').classes()).not.toContain('on')
    await w.get('[data-testid="requirement-drafts-view-gantt"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-view-gantt"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-scale-week"]').classes()).toContain('on')
    w.unmount()
  })

  it('schedule patch does not clear dirty title/body', async () => {
    apiMocks.patchRequirementDraftSchedule.mockResolvedValue({
      ...draftOpen,
      progress: 50,
    })
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('未保存标题')
    expect(w.get('[data-testid="requirement-drafts-dirty-chip"]').text()).toContain('未保存')
    await w.get('[data-testid="requirement-drafts-schedule-progress"]').setValue('50')
    await w.get('[data-testid="requirement-drafts-schedule-progress"]').trigger('change')
    await flushPromises()
    expect(apiMocks.patchRequirementDraftSchedule).toHaveBeenCalled()
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty('value', '未保存标题')
    expect(w.find('[data-testid="requirement-drafts-dirty-chip"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps near-field schedule error after invalid due date (edit view, g3.3)', async () => {
    apiMocks.patchRequirementDraftSchedule.mockRejectedValue(
      new Error('due date must not be before start date'),
    )
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('未保存标题')
    await w.get('[data-testid="requirement-drafts-schedule-due"]').setValue('2026-07-01')
    await w.get('[data-testid="requirement-drafts-schedule-due"]').trigger('change')
    await flushPromises()
    expect(apiMocks.patchRequirementDraftSchedule).toHaveBeenCalledWith('proj-a', 'rd-1', {
      dueAt: '2026-07-01',
    })
    // Fields revert to server values; inline error must remain visible (not cleared by revert).
    expect(w.get('[data-testid="requirement-drafts-schedule-due"]').element).toHaveProperty(
      'value',
      '2026-08-15',
    )
    expect(w.get('[data-testid="requirement-drafts-schedule-error"]').text()).toContain(
      '截止日不能早于开始日',
    )
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty('value', '未保存标题')
    expect(toastMocks.error).toHaveBeenCalled()
    w.unmount()
  })

  it('keeps near-field inspector error after invalid due date (gantt, g3.3/g6.1)', async () => {
    apiMocks.patchRequirementDraftSchedule.mockRejectedValue(
      new Error('due date must not be before start date'),
    )
    const w = mountPanel()
    await flushPromises()
    const scheduledRows = w.findAll('.rd-gantt-scroll .rd-gantt-row')
    expect(scheduledRows.length).toBeGreaterThan(0)
    await scheduledRows[0].trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-inspector"]').exists()).toBe(true)
    await w.get('[data-testid="requirement-drafts-inspector-due"]').setValue('2026-07-01')
    await w.get('[data-testid="requirement-drafts-inspector-due"]').trigger('change')
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-inspector-due"]').element).toHaveProperty(
      'value',
      '2026-08-15',
    )
    expect(w.get('[data-testid="requirement-drafts-inspector-error"]').text()).toContain(
      '截止日不能早于开始日',
    )
    expect(toastMocks.error).toHaveBeenCalled()
    w.unmount()
  })

  it('openBody switches to edit view', async () => {
    const w = mountPanel()
    await flushPromises()
    const scheduledRows = w.findAll('.rd-gantt-scroll .rd-gantt-row')
    expect(scheduledRows.length).toBeGreaterThan(0)
    await scheduledRows[0].trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-open-body"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-markdown-split"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-view-edit"]').classes()).toContain('on')
    expect(w.get('[data-testid="requirement-drafts-view-gantt"]').classes()).not.toContain('on')
    w.unmount()
  })

  it('create modal cancel does not call create', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="requirement-drafts-new"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-new-modal"]').exists()).toBe(true)
    await w.get('[data-testid="requirement-drafts-new-cancel"]').trigger('click')
    await nextTick()
    expect(apiMocks.createRequirementDraft).not.toHaveBeenCalled()
    w.unmount()
  })

  it('milestone create without date shows error and no create', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="requirement-drafts-new"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-new-kind-milestone"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-new-confirm"]').trigger('click')
    await nextTick()
    expect(apiMocks.createRequirementDraft).not.toHaveBeenCalled()
    expect(w.get('[data-testid="requirement-drafts-new-modal-error"]').text()).toContain('必须填写日期')
    w.unmount()
  })

  it('creates requirement draft via modal then selects it in edit view', async () => {
    const created = {
      ...draftOpen,
      id: 'rd-new',
      title: '未命名需求',
      bodyMarkdown: '',
      startAt: '',
      dueAt: '',
    }
    apiMocks.createRequirementDraft.mockResolvedValue(created)
    apiMocks.listRequirementDrafts
      .mockResolvedValueOnce({ items: [draftOpen] })
      .mockResolvedValue({ items: [created, draftOpen] })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="requirement-drafts-new"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-new-confirm"]').trigger('click')
    await flushPromises()
    expect(apiMocks.createRequirementDraft).toHaveBeenCalledWith('proj-a', { kind: 'requirement' })
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty(
      'value',
      '未命名需求',
    )
    expect(w.get('[data-testid="requirement-drafts-view-edit"]').classes()).toContain('on')
    w.unmount()
  })

  it('rejects empty title on save', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    const title = w.get('[data-testid="requirement-drafts-title"]')
    await title.setValue('   ')
    await w.get('[data-testid="requirement-drafts-save"]').trigger('click')
    await nextTick()
    expect(apiMocks.updateRequirementDraft).not.toHaveBeenCalled()
    expect(w.get('[data-testid="requirement-drafts-title-error"]').text()).toContain('不能为空')
    w.unmount()
  })

  it('inserts Demo wrap syntax from the cross-pane toolbar', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-toolbar"]').text()).toContain('H1')
    expect(w.get('[data-testid="requirement-drafts-toolbar"]').text()).toContain('· 列')
    expect(w.get('[data-testid="requirement-drafts-toolbar"]').text()).toContain('查找')
    expect(w.get('[data-testid="requirement-drafts-toolbar"]').text()).toContain('折叠预览')
    await w.get('[data-testid="requirement-drafts-body"]').setValue('')
    await w.get('[data-testid="requirement-drafts-tb-bold"]').trigger('click')
    await nextTick()
    expect((w.get('[data-testid="requirement-drafts-body"]').element as HTMLTextAreaElement).value).toBe(
      '**粗体**',
    )
    expect(w.find('[data-testid="requirement-drafts-gutter"]').text()).toContain('1')
    expect(w.find('[data-testid="requirement-drafts-highlight"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps the buffer when canceling a dirty draft switch', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('未保存标题')
    expect(w.get('[data-testid="requirement-drafts-dirty-chip"]').text()).toContain('未保存')
    await w.get('[data-testid="requirement-drafts-item-rd-2"]').trigger('click')
    await nextTick()
    expect(w.text()).toContain('丢弃未保存的修改？')
    await w.get('[data-testid="requirement-drafts-leave-cancel"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty('value', '未保存标题')
    expect(w.find('[data-testid="requirement-drafts-dirty-chip"]').exists()).toBe(true)
    w.unmount()
  })

  it('asks before create when dirty and keeps buffer on cancel', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('本地标题')
    await w.get('[data-testid="requirement-drafts-new"]').trigger('click')
    await nextTick()
    expect(apiMocks.createRequirementDraft).not.toHaveBeenCalled()
    expect(w.text()).toContain('丢弃未保存的修改？')
    await w.get('[data-testid="requirement-drafts-leave-cancel"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty('value', '本地标题')
    w.unmount()
  })

  it('keeps dirty title after mark-done refresh (keepSelection)', async () => {
    const patched = {
      ...draftOpen,
      status: 'done' as const,
      updatedAt: '2026-08-11T16:00:00Z',
    }
    apiMocks.patchRequirementDraftStatus.mockResolvedValue(patched)
    apiMocks.listRequirementDrafts
      .mockResolvedValueOnce({ items: [draftOpen, draftOpen2] })
      .mockResolvedValue({ items: [draftOpen2] })
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('仍未保存')
    expect(w.get('[data-testid="requirement-drafts-dirty-chip"]').text()).toContain('未保存')
    await w.get('[data-testid="requirement-drafts-toggle-status"]').trigger('click')
    await flushPromises()
    expect(apiMocks.patchRequirementDraftStatus).toHaveBeenCalledWith('proj-a', 'rd-1', 'done')
    expect(w.find('[data-testid="requirement-drafts-empty-detail"]').exists()).toBe(false)
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty('value', '仍未保存')
    expect(w.find('[data-testid="requirement-drafts-dirty-chip"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-status-pill"]').text()).toContain('已完成')
    expect(w.find('[data-testid="requirement-drafts-item-rd-1"]').exists()).toBe(false)
    expect(w.find('[data-testid="requirement-drafts-item-rd-2"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-toggle-status"]').text()).toContain('标记为未完成')
    w.unmount()
  })

  it('does not stack source and preview in a narrow viewport', async () => {
    mockMatchMedia(true)
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-mobile-switch"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-markdown-split"]').classes()).toContain(
      'rd-split-narrow',
    )
    expect(w.get('[data-testid="requirement-drafts-source-pane"]').isVisible()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-preview-pane"]').isVisible()).toBe(false)
    expect(w.find('[data-testid="requirement-drafts-sash"]').isVisible()).toBe(false)
    await w.get('[data-testid="requirement-drafts-mobile-prev"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-source-pane"]').isVisible()).toBe(false)
    expect(w.get('[data-testid="requirement-drafts-preview-pane"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('saves via Ctrl/Cmd+S on the editor pane', async () => {
    apiMocks.updateRequirementDraft.mockResolvedValue({
      ...draftOpen,
      title: '快捷键保存',
      bodyMarkdown: '## 要点',
    })
    apiMocks.listRequirementDrafts
      .mockResolvedValueOnce({ items: [draftOpen] })
      .mockResolvedValue({
        items: [{ ...draftOpen, title: '快捷键保存' }],
      })
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('快捷键保存')
    await w.get('[data-testid="requirement-drafts-detail"]').trigger('keydown', {
      key: 's',
      ctrlKey: true,
    })
    await flushPromises()
    expect(apiMocks.updateRequirementDraft).toHaveBeenCalledWith('proj-a', 'rd-1', {
      title: '快捷键保存',
      bodyMarkdown: '## 要点',
    })
    expect(toastMocks.success).toHaveBeenCalledWith('已保存')
    expect(w.find('[data-testid="requirement-drafts-dirty-chip"]').exists()).toBe(false)
    w.unmount()
  })

  it('opens the in-pane find bar from the toolbar', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-findbar"]').isVisible()).toBe(false)
    await w.get('[data-testid="requirement-drafts-tb-find"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-findbar"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('shows the schedule block expanded by default (g1.2)', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    const toggle = w.get('[data-testid="requirement-drafts-schedule-toggle"]')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(toggle.text()).toContain('排期（即时保存）')
    expect(w.get('[data-testid="requirement-drafts-schedule-toggle"] .rd-schedule-arrow').classes()).toContain(
      'is-open',
    )
    expect(w.get('[data-testid="requirement-drafts-schedule-kind"]').isVisible()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-schedule-due"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('collapses and re-expands schedule fields from the title row (g1.2/g2.1)', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(
      w.get('[data-testid="requirement-drafts-schedule-toggle"]').attributes('aria-expanded'),
    ).toBe('false')
    expect(
      w.get('[data-testid="requirement-drafts-schedule-toggle"] .rd-schedule-arrow').classes(),
    ).not.toContain('is-open')
    expect(w.get('[data-testid="requirement-drafts-schedule-kind"]').isVisible()).toBe(false)
    expect(w.get('[data-testid="requirement-drafts-schedule-start"]').isVisible()).toBe(false)
    expect(w.get('[data-testid="requirement-drafts-schedule-due"]').isVisible()).toBe(false)
    expect(w.get('[data-testid="requirement-drafts-schedule-progress"]').isVisible()).toBe(false)
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(
      w.get('[data-testid="requirement-drafts-schedule-toggle"]').attributes('aria-expanded'),
    ).toBe('true')
    expect(w.get('[data-testid="requirement-drafts-schedule-kind"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('collapsing the schedule block does not patch or dirty the draft (f4)', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-title"]').setValue('未保存标题')
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await flushPromises()
    expect(apiMocks.patchRequirementDraftSchedule).not.toHaveBeenCalled()
    expect(w.find('[data-testid="requirement-drafts-dirty-chip"]').exists()).toBe(true)
    expect(w.get('[data-testid="requirement-drafts-title"]').element).toHaveProperty(
      'value',
      '未保存标题',
    )
    w.unmount()
  })

  it('resets schedule collapse to expanded when switching drafts (f2)', async () => {
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(
      w.get('[data-testid="requirement-drafts-schedule-toggle"]').attributes('aria-expanded'),
    ).toBe('false')
    await w.get('[data-testid="requirement-drafts-item-rd-2"]').trigger('click')
    await nextTick()
    expect(
      w.get('[data-testid="requirement-drafts-schedule-toggle"]').attributes('aria-expanded'),
    ).toBe('true')
    expect(w.get('[data-testid="requirement-drafts-schedule-kind"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('keeps the schedule error visible across collapse and expand (edge)', async () => {
    apiMocks.patchRequirementDraftSchedule.mockRejectedValue(
      new Error('due date must not be before start date'),
    )
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-schedule-due"]').setValue('2026-07-01')
    await w.get('[data-testid="requirement-drafts-schedule-due"]').trigger('change')
    await flushPromises()
    expect(w.get('[data-testid="requirement-drafts-schedule-error"]').text()).toContain(
      '截止日不能早于开始日',
    )
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(w.find('[data-testid="requirement-drafts-schedule-error"]').exists()).toBe(true)
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-schedule-error"]').isVisible()).toBe(true)
    w.unmount()
  })

  it('collapses the schedule block on a narrow viewport (n2)', async () => {
    mockMatchMedia(true)
    const w = mountPanel()
    await flushPromises()
    await switchToEdit(w)
    await w.get('[data-testid="requirement-drafts-item-rd-1"]').trigger('click')
    await nextTick()
    await w.get('[data-testid="requirement-drafts-schedule-toggle"]').trigger('click')
    await nextTick()
    expect(w.get('[data-testid="requirement-drafts-schedule-kind"]').isVisible()).toBe(false)
    w.unmount()
  })
})
