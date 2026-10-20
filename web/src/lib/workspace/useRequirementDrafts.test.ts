// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { RequirementDraft } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
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
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), show: vi.fn() }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

import { useRequirementDrafts } from './useRequirementDrafts'

const DRAFT: RequirementDraft = {
  id: 'd1',
  projectId: 'proj-1',
  title: '草稿 A',
  bodyMarkdown: '# hello',
  status: 'open',
  kind: 'requirement',
  startAt: '2026-01-01',
  dueAt: '2026-01-10',
  progress: 20,
  parentId: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function withDrafts() {
  let api!: ReturnType<typeof useRequirementDrafts>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      api = useRequirementDrafts({ projectId: 'proj-1' })
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { api, app }
}

describe('useRequirementDrafts', () => {
  beforeEach(() => {
    mocks.listRequirementDrafts.mockResolvedValue({ items: [DRAFT] })
    mocks.createRequirementDraft.mockResolvedValue({ ...DRAFT, id: 'd2', title: '新草稿' })
    mocks.updateRequirementDraft.mockResolvedValue(DRAFT)
    mocks.patchRequirementDraftStatus.mockResolvedValue({ ...DRAFT, status: 'done' })
    mocks.patchRequirementDraftSchedule.mockResolvedValue({ ...DRAFT, progress: 50 })
    mocks.deleteRequirementDraft.mockResolvedValue({ status: 'deleted' })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads drafts, switches views, creates, saves, and deletes', async () => {
    const { api, app } = withDrafts()
    await flushPromises()
    await nextTick()
    expect(api.items.value.length).toBeGreaterThan(0)
    api.setViewMode('edit')
    api.setGanttScale('month')
    api.setFilter('all')
    await api.selectDraft('d1')
    await flushPromises()
    api.editTitle.value = '草稿 A 改'
    api.editBody.value = '# changed'
    await api.onSave()
    await flushPromises()
    api.closeNewModal()
    api.onNewClick()
    await api.onConfirmCreate()
    await flushPromises()
    await api.onSave()
    await api.onToggleStatus()
    api.showDelete.value = true
    await api.onConfirmDelete()
    await flushPromises()
    api.openFind()
    api.closeFind()
    api.togglePreviewCollapsed()
    app.unmount()
  })
})
