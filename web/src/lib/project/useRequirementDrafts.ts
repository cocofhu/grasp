import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useToast } from '@/lib/composables/useToast'
import {
  clampDraftSplitRatio,
  findNextDraftMatch,
  highlightDraftSource,
  insertDraftMarkdown,
  syncScrollTop,
  type DraftInsertCmd,
} from '@/lib/project/requirementDraftMarkdown'
import {
  barStyle,
  buildGanttRows,
  clampProgress,
  computeTimelineWindow,
  diamondStyle,
  draftBarRange,
  draftHasChildren,
  normalizeDraft,
  parentCandidates,
  sortMilestones,
  tickLabel,
  todayLocalISO,
  withContextualParents,
  type GanttRow,
  type GanttScale,
} from '@/lib/project/requirementDraftSchedule'
import { fmtTime } from '@/lib/shared/format'
import { renderMarkdown } from '@/lib/shared/markdown'
import type {
  RequirementDraft,
  RequirementDraftKind,
  RequirementDraftSchedulePatch,
  RequirementDraftStatusFilter,
  RequirementDraftViewMode,
} from '@/lib/shared/types'

export interface UseRequirementDraftsProps {
  projectId: string
}

export function useRequirementDrafts(props: UseRequirementDraftsProps) {
const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()

const viewMode = ref<RequirementDraftViewMode>('gantt')
const filter = ref<RequirementDraftStatusFilter>('open')
const query = ref('')
const items = ref<RequirementDraft[]>([])
const catalog = ref<RequirementDraft[]>([])
const loading = ref(false)
const selectedId = ref<string | null>(null)
const creating = ref(false)
const saving = ref(false)
const statusBusy = ref(false)
const deleting = ref(false)
const scheduleBusy = ref(false)
const showDelete = ref(false)
const showLeave = ref(false)
const showNewModal = ref(false)

const newModalKind = ref<RequirementDraftKind>('requirement')
const newModalDueAt = ref('')
const newModalError = ref('')

const ganttScale = ref<GanttScale>('week')

const editTitle = ref('')
const editBody = ref('')
const savedTitle = ref('')
const savedBody = ref('')
const titleError = ref('')
const scheduleError = ref('')
const editKind = ref<RequirementDraftKind>('requirement')
const editStartAt = ref('')
const editDueAt = ref('')
const editProgress = ref(0)
const editParentId = ref<string | null>(null)
const createdAtLabel = ref('—')
const updatedAtLabel = ref('—')
const selectedStatus = ref<'open' | 'done'>('open')

const previewHtml = ref('')
const highlightHtml = ref(' ')
const findOpen = ref(false)
const findQuery = ref('')
const previewCollapsed = ref(false)
const scheduleCollapsed = ref(false)
const mobilePane = ref<'src' | 'prev'>('src')
const splitRatio = ref(0.5)
const sashDragging = ref(false)

const srcEl = ref<HTMLTextAreaElement | null>(null)
const hlEl = ref<HTMLElement | null>(null)
const gutterEl = ref<HTMLElement | null>(null)
const previewEl = ref<HTMLElement | null>(null)
const splitEl = ref<HTMLElement | null>(null)
const findInputEl = ref<HTMLInputElement | null>(null)

const hasSelection = computed(() => Boolean(selectedId.value))
const isDirty = computed(
  () => editTitle.value !== savedTitle.value || editBody.value !== savedBody.value,
)
const lineNumbers = computed(() => {
  const n = Math.max(1, String(editBody.value || '').split('\n').length)
  return Array.from({ length: n }, (_, i) => i + 1)
})
const showSplitPreview = computed(() => {
  if (isMobile.value) return mobilePane.value === 'prev'
  return !previewCollapsed.value
})
const showSplitSource = computed(() => {
  if (isMobile.value) return mobilePane.value === 'src'
  return true
})
const sourceWidthStyle = computed(() => {
  if (isMobile.value || previewCollapsed.value || !showSplitPreview.value) {
    return { width: '100%' }
  }
  return { width: `${splitRatio.value * 100}%` }
})

const catalogById = computed(() => new Map(catalog.value.map((d) => [d.id, d])))

const contextualDisplay = computed(() => {
  const matched = items.value
  if (!query.value.trim()) {
    return matched.map((draft) => ({ draft, contextual: false }))
  }
  return withContextualParents(matched, catalogById.value)
})

const contextualIds = computed(
  () => new Set(contextualDisplay.value.filter((x) => x.contextual).map((x) => x.draft.id)),
)

const ganttRows = computed(() =>
  buildGanttRows(
    contextualDisplay.value.map((x) => x.draft),
    { contextualIds: contextualIds.value },
  ),
)

const timelineDrafts = computed(() => {
  const seen = new Set<string>()
  const out: RequirementDraft[] = []
  for (const row of [...ganttRows.value.unscheduled, ...ganttRows.value.scheduled]) {
    if (seen.has(row.draft.id)) continue
    if (draftBarRange(row.draft)) {
      seen.add(row.draft.id)
      out.push(row.draft)
    }
  }
  return out
})

const timelineWindow = computed(() => computeTimelineWindow(timelineDrafts.value, ganttScale.value))

const todayLineStyle = computed(() => {
  const today = todayLocalISO()
  return { left: barStyle(today, today, timelineWindow.value).left }
})

const milestoneItems = computed(() => sortMilestones(items.value))

const selectedDraft = computed(() => {
  if (!selectedId.value) return null
  return (
    items.value.find((d) => d.id === selectedId.value) ||
    catalog.value.find((d) => d.id === selectedId.value) ||
    null
  )
})

const parentOptions = computed(() => {
  const id = selectedId.value
  if (!id) return []
  return parentCandidates(catalog.value, id)
})

const canEditParent = computed(() => {
  const id = selectedId.value
  if (!id) return false
  return !draftHasChildren(catalog.value, id)
})

let leaveWaiter: ((ok: boolean) => void) | null = null
let loadSeq = 0
let previewRaf = 0
let highlightRaf = 0
let scrollLock = false

function emptyPreviewHtml() {
  return `<p class="text-txt3">${t('pages.projectDetail.requirementDrafts.emptyBodyPreview')}</p>`
}

function schedulePreview() {
  if (previewRaf) cancelAnimationFrame(previewRaf)
  previewRaf = requestAnimationFrame(() => {
    const src = editBody.value
    previewHtml.value = !String(src || '').trim() ? emptyPreviewHtml() : renderMarkdown(src)
  })
}

function scheduleHighlight() {
  if (highlightRaf) cancelAnimationFrame(highlightRaf)
  highlightRaf = requestAnimationFrame(() => {
    highlightHtml.value = highlightDraftSource(editBody.value)
  })
}

function applySavedBaseline(title: string, body: string) {
  savedTitle.value = title
  savedBody.value = body
}

function discardLocalBuffer() {
  editTitle.value = savedTitle.value
  editBody.value = savedBody.value
  titleError.value = ''
}

/** Restore schedule form fields from a draft without touching scheduleError. */
function applyScheduleFieldsFromDraft(d: RequirementDraft) {
  const n = normalizeDraft(d)
  editKind.value = n.kind
  editStartAt.value = n.startAt
  editDueAt.value = n.dueAt
  editProgress.value = n.progress
  editParentId.value = n.parentId
}

function applyScheduleFromDraft(d: RequirementDraft) {
  applyScheduleFieldsFromDraft(d)
  scheduleError.value = ''
}

function mergeScheduleIntoItems(updated: RequirementDraft) {
  const norm = normalizeDraft(updated)
  const patch = (list: RequirementDraft[]) => {
    const i = list.findIndex((d) => d.id === norm.id)
    if (i >= 0) list[i] = norm
  }
  patch(items.value)
  patch(catalog.value)
}

function mapScheduleApiError(msg: string): string {
  const m = String(msg || '').toLowerCase()
  if (m.includes('due') && m.includes('before') && m.includes('start')) {
    return t('pages.projectDetail.requirementDrafts.errDueBeforeStart')
  }
  if (m.includes('invalid date') || m.includes('yyyy-mm-dd')) {
    return t('pages.projectDetail.requirementDrafts.errInvalidDate')
  }
  if (m.includes('milestone') && (m.includes('due') || m.includes('date'))) {
    return t('pages.projectDetail.requirementDrafts.errMilestoneDue')
  }
  if (m.includes('parent')) return t('pages.projectDetail.requirementDrafts.errInvalidParent')
  if (m.includes('children') || m.includes('child')) {
    return t('pages.projectDetail.requirementDrafts.errHasChildren')
  }
  if (m.includes('kind') && m.includes('date')) {
    return t('pages.projectDetail.requirementDrafts.errKindNeedsDate')
  }
  if (m.includes('progress')) return t('pages.projectDetail.requirementDrafts.errInvalidProgress')
  if (m.includes('kind')) return t('pages.projectDetail.requirementDrafts.errInvalidKind')
  return msg || t('pages.projectDetail.requirementDrafts.scheduleFailed')
}

function revertScheduleFields() {
  const d = selectedDraft.value
  // Keep scheduleError so near-field inline hints survive failed PATCH (NFR n2 / g3.3).
  if (d) applyScheduleFieldsFromDraft(d)
}

function requestLeave(): Promise<boolean> {
  if (!isDirty.value) return Promise.resolve(true)
  if (leaveWaiter) {
    return new Promise((resolve) => {
      const prev = leaveWaiter
      leaveWaiter = (ok) => {
        prev?.(ok)
        resolve(ok)
      }
    })
  }
  showLeave.value = true
  return new Promise((resolve) => {
    leaveWaiter = resolve
  })
}

function resolveLeave(ok: boolean) {
  showLeave.value = false
  if (ok) discardLocalBuffer()
  const done = leaveWaiter
  leaveWaiter = null
  done?.(ok)
}

async function loadCatalog() {
  const res = await api.listRequirementDrafts(props.projectId, { status: filter.value })
  catalog.value = (Array.isArray(res.items) ? res.items : []).map(normalizeDraft)
}

async function loadList(opts?: { preferId?: string | null; keepSelection?: boolean }) {
  const seq = ++loadSeq
  loading.value = true
  try {
    const [res] = await Promise.all([
      api.listRequirementDrafts(props.projectId, {
        status: filter.value,
        q: query.value.trim() || undefined,
      }),
      loadCatalog(),
    ])
    if (seq !== loadSeq) return
    items.value = (Array.isArray(res.items) ? res.items : []).map(normalizeDraft)
    const prefer = opts?.preferId
    if (prefer && items.value.some((d) => d.id === prefer)) {
      selectDraft(prefer, { discardBuffer: opts?.keepSelection ? false : true })
      return
    }
    if (opts?.keepSelection && selectedId.value) {
      const cur =
        items.value.find((d) => d.id === selectedId.value) ||
        catalog.value.find((d) => d.id === selectedId.value)
      if (cur) {
        createdAtLabel.value = fmtTime(cur.createdAt)
        updatedAtLabel.value = fmtTime(cur.updatedAt)
        selectedStatus.value = cur.status
        applyScheduleFromDraft(cur)
      }
      return
    }
    if (selectedId.value) {
      const inItems = items.value.some((d) => d.id === selectedId.value)
      const inCatalog = catalog.value.some((d) => d.id === selectedId.value)
      if (inItems || inCatalog) {
        selectDraft(selectedId.value, { discardBuffer: false })
        return
      }
    }
    selectedId.value = null
  } catch (e: any) {
    if (seq !== loadSeq) return
    toast.error(e?.message || t('pages.projectDetail.requirementDrafts.loadFailed'))
    items.value = []
    catalog.value = []
    selectedId.value = null
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

function selectDraft(id: string, opts?: { discardBuffer?: boolean }) {
  const d =
    items.value.find((x) => x.id === id) || catalog.value.find((x) => x.id === id)
  if (!d) {
    selectedId.value = null
    return
  }
  if (selectedId.value !== id) scheduleCollapsed.value = false
  selectedId.value = id
  if (opts?.discardBuffer !== false) {
    editTitle.value = d.title
    editBody.value = d.bodyMarkdown || ''
    titleError.value = ''
    applySavedBaseline(d.title, d.bodyMarkdown || '')
  }
  applyScheduleFromDraft(d)
  createdAtLabel.value = fmtTime(d.createdAt)
  updatedAtLabel.value = fmtTime(d.updatedAt)
  selectedStatus.value = d.status
}

async function onPickDraft(id: string) {
  if (id === selectedId.value) return
  if (isDirty.value) {
    const ok = await requestLeave()
    if (!ok) return
  }
  selectDraft(id, { discardBuffer: true })
}

async function onSelectFromGantt(id: string) {
  await onPickDraft(id)
}

function setViewMode(mode: RequirementDraftViewMode) {
  viewMode.value = mode
}

function setGanttScale(scale: GanttScale) {
  ganttScale.value = scale
}

function openNewModal() {
  newModalKind.value = 'requirement'
  newModalDueAt.value = ''
  newModalError.value = ''
  showNewModal.value = true
}

function closeNewModal() {
  showNewModal.value = false
  newModalError.value = ''
}

async function onNewClick() {
  if (creating.value) return
  if (isDirty.value) {
    const ok = await requestLeave()
    if (!ok) return
  }
  openNewModal()
}

async function onConfirmCreate() {
  newModalError.value = ''
  if (newModalKind.value === 'milestone' && !newModalDueAt.value.trim()) {
    newModalError.value = t('pages.projectDetail.requirementDrafts.milestoneDueRequired')
    return
  }
  creating.value = true
  try {
    const body =
      newModalKind.value === 'milestone'
        ? { kind: 'milestone' as const, dueAt: newModalDueAt.value.trim() }
        : { kind: 'requirement' as const }
    const created = await api.createRequirementDraft(props.projectId, body)
    closeNewModal()
    filter.value = 'open'
    query.value = ''
    const norm = normalizeDraft(created)
    if (newModalKind.value === 'requirement' && viewMode.value === 'milestones') {
      viewMode.value = 'edit'
    } else if (newModalKind.value === 'requirement') {
      viewMode.value = 'edit'
    }
    await loadList({ preferId: norm.id })
    toast.success(t('pages.projectDetail.requirementDrafts.createdToast'))
  } catch (e: any) {
    toast.error(e?.message || t('pages.projectDetail.requirementDrafts.createFailed'))
  } finally {
    creating.value = false
  }
}

async function patchSchedule(body: RequirementDraftSchedulePatch) {
  const id = selectedId.value
  if (!id || scheduleBusy.value) return
  scheduleBusy.value = true
  scheduleError.value = ''
  try {
    const updated = await api.patchRequirementDraftSchedule(props.projectId, id, body)
    mergeScheduleIntoItems(updated)
    applyScheduleFromDraft(updated)
    await loadList({ keepSelection: true })
    toast.success(t('pages.projectDetail.requirementDrafts.scheduleSaved'))
  } catch (e: any) {
    // Revert inputs first, then set error (revert must not clear scheduleError).
    revertScheduleFields()
    scheduleError.value = mapScheduleApiError(e?.message || '')
    toast.error(scheduleError.value)
  } finally {
    scheduleBusy.value = false
  }
}

function onScheduleKindChange() {
  const kind = editKind.value
  void patchSchedule({ kind })
}

function onScheduleStartChange() {
  void patchSchedule({ startAt: editStartAt.value })
}

function onScheduleDueChange() {
  void patchSchedule({ dueAt: editDueAt.value })
}

function onScheduleProgressChange() {
  editProgress.value = clampProgress(editProgress.value)
  void patchSchedule({ progress: editProgress.value })
}

function onScheduleParentChange() {
  void patchSchedule({ parentId: editParentId.value })
}

async function openBody(id?: string) {
  const targetId = id || selectedId.value
  if (!targetId) return
  if (targetId !== selectedId.value && isDirty.value) {
    const ok = await requestLeave()
    if (!ok) return
  }
  if (targetId !== selectedId.value) {
    selectDraft(targetId, { discardBuffer: true })
  }
  viewMode.value = 'edit'
}

async function onSave() {
  const id = selectedId.value
  if (!id || saving.value) return
  if (!isDirty.value) {
    toast.show(t('pages.projectDetail.requirementDrafts.noUnsavedToast'))
    return
  }
  const title = editTitle.value.replace(/^\s+|\s+$/g, '')
  if (!title) {
    titleError.value = t('pages.projectDetail.requirementDrafts.titleRequired')
    return
  }
  titleError.value = ''
  saving.value = true
  try {
    const saved = await api.updateRequirementDraft(props.projectId, id, {
      title,
      bodyMarkdown: editBody.value,
    })
    editTitle.value = saved.title
    editBody.value = saved.bodyMarkdown || ''
    applySavedBaseline(saved.title, saved.bodyMarkdown || '')
    await loadList({ preferId: saved.id, keepSelection: true })
    const cur = items.value.find((d) => d.id === saved.id)
    if (cur) {
      createdAtLabel.value = fmtTime(cur.createdAt)
      updatedAtLabel.value = fmtTime(cur.updatedAt)
      selectedStatus.value = cur.status
    }
    toast.success(t('pages.projectDetail.requirementDrafts.savedToast'))
  } catch (e: any) {
    toast.error(e?.message || t('pages.projectDetail.requirementDrafts.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function onToggleStatus() {
  const id = selectedId.value
  if (!id || statusBusy.value) return
  const next = selectedStatus.value === 'open' ? 'done' : 'open'
  statusBusy.value = true
  try {
    const patched = await api.patchRequirementDraftStatus(props.projectId, id, next)
    selectedStatus.value = patched.status
    createdAtLabel.value = fmtTime(patched.createdAt)
    updatedAtLabel.value = fmtTime(patched.updatedAt)
    toast.success(
      next === 'done'
        ? t('pages.projectDetail.requirementDrafts.archivedToast')
        : t('pages.projectDetail.requirementDrafts.reopenedToast'),
    )
    await loadList({ keepSelection: true })
  } catch (e: any) {
    toast.error(e?.message || t('pages.projectDetail.requirementDrafts.statusFailed'))
  } finally {
    statusBusy.value = false
  }
}

async function onConfirmDelete() {
  const id = selectedId.value
  if (!id || deleting.value) return
  deleting.value = true
  try {
    await api.deleteRequirementDraft(props.projectId, id)
    showDelete.value = false
    selectedId.value = null
    editTitle.value = ''
    editBody.value = ''
    applySavedBaseline('', '')
    titleError.value = ''
    toast.success(t('pages.projectDetail.requirementDrafts.deletedToast'))
    await loadList()
  } catch (e: any) {
    toast.error(e?.message || t('pages.projectDetail.requirementDrafts.deleteFailed'))
  } finally {
    deleting.value = false
  }
}

function setFilter(f: RequirementDraftStatusFilter) {
  if (filter.value === f) return
  filter.value = f
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    void loadList({ keepSelection: true })
  }, 200)
}

function applyInsert(cmd: DraftInsertCmd) {
  const ta = srcEl.value
  const start = ta?.selectionStart ?? editBody.value.length
  const end = ta?.selectionEnd ?? start
  const next = insertDraftMarkdown({
    value: editBody.value,
    selectionStart: start,
    selectionEnd: end,
    cmd,
  })
  editBody.value = next.value
  void nextTick(() => {
    ta?.focus()
    ta?.setSelectionRange(next.selectionStart, next.selectionEnd)
    syncOverlayScroll()
  })
}

function openFind() {
  findOpen.value = true
  void nextTick(() => findInputEl.value?.focus())
}

function closeFind() {
  findOpen.value = false
  srcEl.value?.focus()
}

function runFindNext() {
  const ta = srcEl.value
  const hit = findNextDraftMatch({
    value: editBody.value,
    query: findQuery.value,
    from: ta?.selectionEnd || 0,
  })
  if (!hit) {
    toast.show(t('pages.projectDetail.requirementDrafts.findNoMatch'))
    return
  }
  ta?.focus()
  ta?.setSelectionRange(hit.index, hit.end)
  if (ta) ta.scrollTop = hit.scrollTop
  syncOverlayScroll()
}

function togglePreviewCollapsed() {
  if (isMobile.value) return
  previewCollapsed.value = !previewCollapsed.value
}

function toggleScheduleCollapsed() {
  scheduleCollapsed.value = !scheduleCollapsed.value
}

function onSashDown(e: MouseEvent) {
  if (isMobile.value || previewCollapsed.value) return
  e.preventDefault()
  sashDragging.value = true
  const split = splitEl.value
  function move(ev: MouseEvent) {
    if (!split) return
    const r = split.getBoundingClientRect()
    if (r.width <= 0) return
    splitRatio.value = clampDraftSplitRatio((ev.clientX - r.left) / r.width)
  }
  function up() {
    sashDragging.value = false
    document.removeEventListener('mousemove', move)
    document.removeEventListener('mouseup', up)
  }
  document.addEventListener('mousemove', move)
  document.addEventListener('mouseup', up)
}

function syncOverlayScroll() {
  const ta = srcEl.value
  if (!ta) return
  if (hlEl.value) {
    hlEl.value.scrollTop = ta.scrollTop
    hlEl.value.scrollLeft = ta.scrollLeft
  }
  if (gutterEl.value) gutterEl.value.scrollTop = ta.scrollTop
}

function onSrcScroll() {
  syncOverlayScroll()
  if (previewCollapsed.value || !showSplitPreview.value) return
  if (!srcEl.value || !previewEl.value) return
  if (scrollLock) return
  scrollLock = true
  syncScrollTop(srcEl.value, previewEl.value)
  scrollLock = false
}

function onPreviewScroll() {
  if (previewCollapsed.value || !showSplitSource.value) return
  if (!srcEl.value || !previewEl.value) return
  if (scrollLock) return
  scrollLock = true
  syncScrollTop(previewEl.value, srcEl.value)
  syncOverlayScroll()
  scrollLock = false
}

function onDetailKeydown(e: KeyboardEvent) {
  const meta = e.metaKey || e.ctrlKey
  if (!meta) return
  if (e.key.toLowerCase() === 's') {
    e.preventDefault()
    void onSave()
  }
}

function onSrcKeydown(e: KeyboardEvent) {
  const meta = e.metaKey || e.ctrlKey
  if (!meta) return
  const k = e.key.toLowerCase()
  if (k === 'b') {
    e.preventDefault()
    applyInsert('bold')
    return
  }
  if (k === 'i') {
    e.preventDefault()
    applyInsert('italic')
    return
  }
  if (k === 'f') {
    e.preventDefault()
    openFind()
  }
}

function onFindInputKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    runFindNext()
  }
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (!isDirty.value) return
  e.preventDefault()
  e.returnValue = ''
}

function kindLabel(kind: RequirementDraftKind) {
  return kind === 'milestone'
    ? t('pages.projectDetail.requirementDrafts.kindMilestone')
    : t('pages.projectDetail.requirementDrafts.kindRequirement')
}

function rowBarStyle(row: GanttRow) {
  const range = draftBarRange(row.draft)
  if (!range) return null
  return barStyle(range.start, range.end, timelineWindow.value)
}

function isRowSelected(id: string) {
  return selectedId.value === id
}

watch(
  () => props.projectId,
  () => {
    filter.value = 'open'
    query.value = ''
    viewMode.value = 'gantt'
    ganttScale.value = 'week'
    selectedId.value = null
    editTitle.value = ''
    editBody.value = ''
    scheduleCollapsed.value = false
    applySavedBaseline('', '')
    void loadList()
  },
)

watch(filter, () => {
  void loadList({ keepSelection: true })
})

watch(editBody, () => {
  schedulePreview()
  scheduleHighlight()
})

watch(isMobile, (narrow) => {
  if (narrow) mobilePane.value = 'src'
})

onMounted(() => {
  schedulePreview()
  scheduleHighlight()
  void loadList()
  window.addEventListener('beforeunload', onBeforeUnload)
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
  if (previewRaf) cancelAnimationFrame(previewRaf)
  if (highlightRaf) cancelAnimationFrame(highlightRaf)
  if (searchTimer) clearTimeout(searchTimer)
  if (leaveWaiter) {
    leaveWaiter(false)
    leaveWaiter = null
  }
})

  return {
  t,
  toast,
  isMobile,
  viewMode,
  filter,
  query,
  items,
  catalog,
  loading,
  selectedId,
  creating,
  saving,
  statusBusy,
  deleting,
  scheduleBusy,
  showDelete,
  showLeave,
  showNewModal,
  newModalKind,
  newModalDueAt,
  newModalError,
  ganttScale,
  editTitle,
  editBody,
  savedTitle,
  savedBody,
  titleError,
  scheduleError,
  editKind,
  editStartAt,
  editDueAt,
  editProgress,
  editParentId,
  createdAtLabel,
  updatedAtLabel,
  selectedStatus,
  previewHtml,
  highlightHtml,
  findOpen,
  findQuery,
  previewCollapsed,
  scheduleCollapsed,
  mobilePane,
  splitRatio,
  sashDragging,
  srcEl,
  hlEl,
  gutterEl,
  previewEl,
  splitEl,
  findInputEl,
  hasSelection,
  isDirty,
  lineNumbers,
  showSplitPreview,
  showSplitSource,
  sourceWidthStyle,
  catalogById,
  contextualDisplay,
  contextualIds,
  ganttRows,
  timelineDrafts,
  timelineWindow,
  todayLineStyle,
  milestoneItems,
  selectedDraft,
  parentOptions,
  canEditParent,
  loadSeq,
  previewRaf,
  highlightRaf,
  scrollLock,
  emptyPreviewHtml,
  schedulePreview,
  scheduleHighlight,
  applySavedBaseline,
  discardLocalBuffer,
  applyScheduleFieldsFromDraft,
  applyScheduleFromDraft,
  mergeScheduleIntoItems,
  mapScheduleApiError,
  revertScheduleFields,
  requestLeave,
  resolveLeave,
  loadCatalog,
  loadList,
  selectDraft,
  onPickDraft,
  onSelectFromGantt,
  setViewMode,
  setGanttScale,
  openNewModal,
  closeNewModal,
  onNewClick,
  onConfirmCreate,
  patchSchedule,
  onScheduleKindChange,
  onScheduleStartChange,
  onScheduleDueChange,
  onScheduleProgressChange,
  onScheduleParentChange,
  openBody,
  onSave,
  onToggleStatus,
  onConfirmDelete,
  setFilter,
  onSearchInput,
  applyInsert,
  openFind,
  closeFind,
  runFindNext,
  togglePreviewCollapsed,
  toggleScheduleCollapsed,
  onSashDown,
  syncOverlayScroll,
  onSrcScroll,
  onPreviewScroll,
  onDetailKeydown,
  onSrcKeydown,
  onFindInputKeydown,
  onBeforeUnload,
  kindLabel,
  rowBarStyle,
  isRowSelected,
  diamondStyle,
  tickLabel,
  clampProgress,
  fmtTime
  }
}
