<script setup lang="ts">
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { computed, ref, watch } from 'vue'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { useRequirementDrafts } from '@/lib/workspace/useRequirementDrafts'

const {
  t,
  toast,
  isMobile,
  isNarrowEditor,
  viewMode,
  filter,
  query,
  items,
  catalog,
  loading,
  loadError,
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
} = useRequirementDrafts()

const inspectorOpen = ref(false)
const hasFilterContext = computed(() => Boolean(catalog.value.length || query.value.trim() || filter.value !== 'open'))

function clearFilters() {
  query.value = ''
  if (filter.value !== 'open') setFilter('open')
  else void loadList({ keepSelection: true })
}

watch(selectedId, (id) => {
  inspectorOpen.value = Boolean(id)
})

async function revealInspector(id: string) {
  await onSelectFromGantt(id)
  if (selectedId.value === id) inspectorOpen.value = true
}

async function onNewMilestone() {
  await onNewClick()
  if (showNewModal.value) newModalKind.value = 'milestone'
}

defineExpose({
  get isDirty() {
    return isDirty.value
  },
  requestLeave,
})
</script>

<template>
  <div
    class="requirement-drafts flex h-full min-h-0 min-w-0 flex-1 flex-col"
    data-testid="requirement-drafts-panel"
  >
    <nav class="rd-view-tabs shrink-0" data-testid="requirement-drafts-view-segment" :aria-label="t('pages.workspace.tabRequirementDrafts')">
      <button
        v-for="mode in (['gantt', 'edit', 'milestones'] as const)"
        :key="mode"
        type="button"
        :class="{ on: viewMode === mode }"
        :aria-current="viewMode === mode ? 'page' : undefined"
        :data-testid="`requirement-drafts-view-${mode}`"
        @click="setViewMode(mode)"
      >
        {{ t(`pages.workspace.requirementDrafts.${mode === 'edit' ? 'viewEdit' : mode === 'gantt' ? 'viewGantt' : 'viewMilestones'}`) }}
      </button>
    </nav>

    <div class="rd-filter-toolbar flex shrink-0 flex-wrap items-center gap-3 border-b border-line/60 py-4">
      <div class="relative min-w-0 flex-1 basis-48 sm:max-w-xs">
        <Icon name="search" :size="15" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-txt3" />
        <input
          v-model="query"
          type="search"
          class="input w-full !pl-9 text-[13px]"
          data-testid="requirement-drafts-search"
          :aria-label="t('pages.workspace.requirementDrafts.searchPh')"
          :placeholder="t('pages.workspace.requirementDrafts.searchPh')"
          @input="onSearchInput"
        />
      </div>
      <SlidingTabs :value="filter" indicator-class="rounded-md bg-elevated" class="rd-status-filter flex items-center gap-1" data-testid="requirement-drafts-filter" role="group" :aria-label="t('pages.workspace.requirementDrafts.filterAll')">
        <button
          v-for="status in (['open', 'done', 'all'] as const)"
          :key="status"
          type="button"
          :class="{ on: filter === status }"
          :aria-pressed="filter === status"
          :data-testid="`requirement-drafts-filter-${status}`"
          @click="setFilter(status)"
        >
          {{ t(`pages.workspace.requirementDrafts.${status === 'open' ? 'filterOpen' : status === 'done' ? 'filterDone' : 'filterAll'}`) }}
        </button>
      </SlidingTabs>
      <AppButton
        variant="primary"
        size="sm"
        icon="plus"
        class="ml-auto shrink-0"
        :disabled="creating"
        data-testid="requirement-drafts-new"
        @click="onNewClick"
      >
        {{ t('pages.workspace.requirementDrafts.new') }}
      </AppButton>
    </div>

    <div
      v-if="loadError && !loading"
      class="flex flex-col items-start gap-3 py-8 text-[13px]"
      data-testid="requirement-drafts-load-error"
      role="alert"
    >
      <p class="m-0 font-medium text-err">{{ t('pages.workspace.requirementDrafts.loadFailed') }}</p>
      <p class="m-0 text-txt3">{{ loadError }}</p>
      <AppButton variant="outline" size="sm" data-testid="requirement-drafts-load-retry" @click="loadList()">
        {{ t('common.loading.retry') }}
      </AppButton>
    </div>

    <div
      v-else-if="!loading && !items.length"
      class="mx-auto flex max-w-md flex-col items-center gap-4 px-2 py-12 text-center sm:py-16"
      data-testid="requirement-drafts-empty"
    >
      <Icon :name="hasFilterContext ? 'search' : 'doc'" :size="28" class="text-txt3" />
      <div>
        <h2 class="m-0 text-lg font-semibold text-txt">
          {{ t(`pages.workspace.requirementDrafts.${hasFilterContext ? 'noMatchesTitle' : 'emptyTitle'}`) }}
        </h2>
        <p class="mb-0 mt-2 text-[13px] leading-6 text-txt3" data-testid="requirement-drafts-list-empty">
          {{ t(`pages.workspace.requirementDrafts.${hasFilterContext ? 'noMatchesHint' : 'emptyHint'}`) }}
        </p>
      </div>
      <AppButton
        v-if="hasFilterContext"
        variant="outline"
        size="sm"
        data-testid="requirement-drafts-clear-filters"
        @click="clearFilters"
      >
        {{ t('pages.workspace.requirementDrafts.clearFilters') }}
      </AppButton>
      <AppButton
        v-else
        variant="primary"
        size="sm"
        icon="plus"
        :disabled="creating"
        data-testid="requirement-drafts-empty-new"
        @click="onNewClick"
      >
        {{ t('pages.workspace.requirementDrafts.newModalTitle') }}
      </AppButton>
    </div>

    <!-- Edit view -->
    <div
      v-else-if="viewMode === 'edit'"
      class="draft-layout grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[250px_1fr]"
    >
      <aside class="draft-list-pane flex min-h-0 flex-col border-b border-line/60 md:border-b-0 md:border-r">
        <div
          class="min-h-0 flex-1 overflow-auto py-2 md:pr-4"
          data-testid="requirement-drafts-list"
        >
          <div v-if="loading && !items.length" class="px-5 py-8 text-center text-[13px] text-txt3">
            {{ t('common.loading.label') }}
          </div>
          <div
            v-else-if="!items.length"
            class="px-5 py-8 text-center text-[13px] text-txt3"
            data-testid="requirement-drafts-list-empty"
          >
            {{ t('pages.workspace.requirementDrafts.listEmpty') }}
          </div>
          <button
            v-for="d in items"
            :key="d.id"
            type="button"
            class="relative block w-full border-b border-line/50 px-3 py-3 text-left transition last:border-b-0 hover:bg-elevated/50"
            :class="d.id === selectedId ? 'bg-accent-dim' : 'bg-transparent'"
            :data-testid="`requirement-drafts-item-${d.id}`"
            @click="onPickDraft(d.id)"
          >
            <span
              v-if="d.id === selectedId"
              class="absolute bottom-0 left-0 top-0 w-[3px] bg-accent"
              data-testid="requirement-drafts-item-active-bar"
            />
            <div class="mb-1 flex items-center gap-2 text-[13px] font-medium text-txt">
              <span class="min-w-0 truncate">{{ d.title }}</span>
            </div>
            <div class="flex flex-wrap items-center gap-2 text-[11px] text-txt3">
              <span class="rounded-lg border border-line bg-elevated px-1.5 py-px text-txt2">
                {{ kindLabel(d.kind) }}
              </span>
              <span
                class="rounded-lg border px-1.5 py-px"
                :class="
                  d.status === 'open'
                    ? 'border-info/40 text-info bg-elevated'
                    : 'border-ok/40 text-ok bg-elevated'
                "
              >
                {{
                  d.status === 'open'
                    ? t('pages.workspace.requirementDrafts.statusOpen')
                    : t('pages.workspace.requirementDrafts.statusDone')
                }}
              </span>
              <span>{{ t('pages.workspace.requirementDrafts.updated') }} {{ fmtTime(d.updatedAt) }}</span>
            </div>
          </button>
        </div>
      </aside>

      <div
        class="draft-detail flex min-h-0 min-w-0 flex-col"
        :class="{ 'draft-detail-selected': hasSelection }"
        data-testid="requirement-drafts-detail"
        @keydown="onDetailKeydown"
      >
        <div
          v-if="!hasSelection"
          class="px-5 py-8 text-center text-[13px] text-txt3"
          data-testid="requirement-drafts-empty-detail"
        >
          <p>{{ t('pages.workspace.requirementDrafts.emptyDetail') }}</p>
        </div>
        <template v-else>
          <div class="draft-detail-header flex flex-wrap items-start justify-between gap-3 border-b border-line/60 py-4 md:pl-5">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <strong class="text-[18px] font-[650] leading-tight text-txt">
                  {{ t('pages.workspace.requirementDrafts.editLabel') }}
                </strong>
                <span
                  class="rounded-md border px-1.5 py-px text-[11px]"
                  :class="
                    selectedStatus === 'open'
                      ? 'border-info/40 text-info bg-elevated'
                      : 'border-ok/40 text-ok bg-elevated'
                  "
                  data-testid="requirement-drafts-status-pill"
                >
                  {{
                    selectedStatus === 'open'
                      ? t('pages.workspace.requirementDrafts.statusOpen')
                      : t('pages.workspace.requirementDrafts.statusDone')
                  }}
                </span>
                <span
                  v-if="isDirty"
                  class="rounded-md border border-warn/40 bg-elevated px-1.5 py-px text-[11px] text-warn"
                  data-testid="requirement-drafts-dirty-chip"
                >
                  {{ t('pages.workspace.requirementDrafts.unsaved') }}
                </span>
              </div>
              <div class="mt-1 flex flex-wrap gap-3 text-[11px] text-txt3">
                <span>
                  {{ t('pages.workspace.requirementDrafts.created') }}
                  <span data-testid="requirement-drafts-created-at">{{ createdAtLabel }}</span>
                </span>
                <span>
                  {{ t('pages.workspace.requirementDrafts.updated') }}
                  <span data-testid="requirement-drafts-updated-at">{{ updatedAtLabel }}</span>
                </span>
              </div>
            </div>
            <div class="flex flex-wrap gap-2">
              <AppButton
                variant="outline"
                size="sm"
                :disabled="statusBusy"
                data-testid="requirement-drafts-toggle-status"
                @click="onToggleStatus"
              >
                {{
                  selectedStatus === 'open'
                    ? t('pages.workspace.requirementDrafts.markDone')
                    : t('pages.workspace.requirementDrafts.markOpen')
                }}
              </AppButton>
              <AppButton
                variant="outline"
                size="sm"
                class="text-err"
                data-testid="requirement-drafts-delete"
                @click="showDelete = true"
              >
                {{ t('common.buttons.delete') }}
              </AppButton>
              <AppButton
                variant="primary"
                size="sm"
                :disabled="saving"
                data-testid="requirement-drafts-save"
                @click="onSave"
              >
                {{ t('common.buttons.save') }}
              </AppButton>
            </div>
          </div>
          <div class="draft-editor-content flex min-h-0 flex-1 flex-col pb-4 pt-4 md:pl-5">
            <div>
              <label class="mb-1.5 block text-xs text-txt2" for="requirement-draft-title">
                {{ t('pages.workspace.requirementDrafts.titleLabel') }}
              </label>
              <input
                id="requirement-draft-title"
                v-model="editTitle"
                type="text"
                class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent focus:shadow-[0_0_0_2px_rgba(123,97,255,0.3)]"
                data-testid="requirement-drafts-title"
                :placeholder="t('pages.workspace.requirementDrafts.titlePh')"
              />
              <div class="mt-1 min-h-[18px] text-xs text-err" data-testid="requirement-drafts-title-error">
                {{ titleError }}
              </div>
            </div>

            <!-- Schedule block (edit view) -->
            <div
              class="rd-schedule mb-4 border-y border-line/60 py-3"
              data-testid="requirement-drafts-schedule-block"
            >
              <button
                type="button"
                class="rd-schedule-head mb-2 flex w-full items-center justify-between gap-2 text-left"
                data-testid="requirement-drafts-schedule-toggle"
                :aria-expanded="!scheduleCollapsed"
                :title="
                  scheduleCollapsed
                    ? t('pages.workspace.requirementDrafts.expandSchedule')
                    : t('pages.workspace.requirementDrafts.collapseSchedule')
                "
                @click="toggleScheduleCollapsed"
              >
                <span class="text-[13px] font-medium text-txt">
                  {{ t('pages.workspace.requirementDrafts.scheduleBlockTitle') }}
                </span>
                <span
                  class="rd-schedule-arrow text-txt3"
                  :class="{ 'is-open': !scheduleCollapsed }"
                  aria-hidden="true"
                >
                  <Icon name="chevron-down" :size="13" :interactive="false" />
                </span>
              </button>
              <div v-show="!scheduleCollapsed">
                <p class="mb-3 text-[11px] text-txt3">
                  {{ t('pages.workspace.requirementDrafts.scheduleHint') }}
                </p>
                <div class="rd-schedule-fields grid gap-3">
                <label class="block text-xs text-txt2">
                  {{ t('pages.workspace.requirementDrafts.kindLabel') }}
                  <select
                    v-model="editKind"
                    class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                    data-testid="requirement-drafts-schedule-kind"
                    :disabled="scheduleBusy"
                    @change="onScheduleKindChange"
                  >
                    <option value="requirement">
                      {{ t('pages.workspace.requirementDrafts.kindRequirement') }}
                    </option>
                    <option value="milestone">
                      {{ t('pages.workspace.requirementDrafts.kindMilestone') }}
                    </option>
                  </select>
                </label>
                <label v-if="editKind !== 'milestone'" class="block text-xs text-txt2">
                  {{ t('pages.workspace.requirementDrafts.startAtLabel') }}
                  <input
                    v-model="editStartAt"
                    type="date"
                    class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                    data-testid="requirement-drafts-schedule-start"
                    :disabled="scheduleBusy"
                    @change="onScheduleStartChange"
                  />
                </label>
                <label class="block text-xs text-txt2">
                  {{ t('pages.workspace.requirementDrafts.dueAtLabel') }}
                  <input
                    v-model="editDueAt"
                    type="date"
                    class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                    data-testid="requirement-drafts-schedule-due"
                    :disabled="scheduleBusy"
                    @change="onScheduleDueChange"
                  />
                </label>
                <label class="block text-xs text-txt2">
                  {{ t('pages.workspace.requirementDrafts.progressLabel') }}
                  <input
                    v-model.number="editProgress"
                    type="number"
                    min="0"
                    max="100"
                    class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                    data-testid="requirement-drafts-schedule-progress"
                    :disabled="scheduleBusy"
                    @change="onScheduleProgressChange"
                  />
                </label>
                <label v-if="canEditParent" class="rd-parent-field block text-xs text-txt2">
                  {{ t('pages.workspace.requirementDrafts.parentLabel') }}
                  <select
                    v-model="editParentId"
                    class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                    data-testid="requirement-drafts-schedule-parent"
                    :disabled="scheduleBusy"
                    @change="onScheduleParentChange"
                  >
                    <option :value="null">
                      {{ t('pages.workspace.requirementDrafts.parentNone') }}
                    </option>
                    <option v-for="p in parentOptions" :key="p.id" :value="p.id">
                      {{ p.title }}
                    </option>
                  </select>
                </label>
              </div>
              </div>
              <div
                v-if="scheduleError"
                class="mt-2 text-xs text-err"
                data-testid="requirement-drafts-schedule-error"
              >
                {{ scheduleError }}
              </div>
            </div>

            <div class="rd-body-editor flex min-h-0 flex-1 flex-col">
              <label class="mb-1.5 block text-xs text-txt2">
                {{ t('pages.workspace.requirementDrafts.bodyLabel') }}
              </label>
              <div
                class="rd-toolbar flex flex-wrap items-center gap-0.5 border-y border-line/60 py-1.5"
                data-testid="requirement-drafts-toolbar"
              >
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-h1"
                  :title="t('pages.workspace.requirementDrafts.tbH1')"
                  @click="applyInsert('h1')"
                >
                  H1
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-h2"
                  :title="t('pages.workspace.requirementDrafts.tbH2')"
                  @click="applyInsert('h2')"
                >
                  H2
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-h3"
                  :title="t('pages.workspace.requirementDrafts.tbH3')"
                  @click="applyInsert('h3')"
                >
                  H3
                </button>
                <span class="rd-tb-sep" />
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-bold"
                  :title="t('pages.workspace.requirementDrafts.tbBold')"
                  @click="applyInsert('bold')"
                >
                  B
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-italic"
                  :title="t('pages.workspace.requirementDrafts.tbItalic')"
                  @click="applyInsert('italic')"
                >
                  I
                </button>
                <span class="rd-tb-sep" />
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-ul"
                  :title="t('pages.workspace.requirementDrafts.tbUl')"
                  @click="applyInsert('ul')"
                >
                  · 列
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-ol"
                  :title="t('pages.workspace.requirementDrafts.tbOl')"
                  @click="applyInsert('ol')"
                >
                  1. 列
                </button>
                <span class="rd-tb-sep" />
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-link"
                  :title="t('pages.workspace.requirementDrafts.tbLink')"
                  @click="applyInsert('link')"
                >
                  链接
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-code"
                  :title="t('pages.workspace.requirementDrafts.tbCode')"
                  @click="applyInsert('code')"
                >
                  `
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-fence"
                  :title="t('pages.workspace.requirementDrafts.tbFence')"
                  @click="applyInsert('fence')"
                >
                  ```
                </button>
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-table"
                  :title="t('pages.workspace.requirementDrafts.tbTable')"
                  @click="applyInsert('table')"
                >
                  表格
                </button>
                <span class="rd-tb-grow" />
                <button
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-find"
                  :title="t('pages.workspace.requirementDrafts.tbFind')"
                  @click="openFind"
                >
                  {{ t('pages.workspace.requirementDrafts.find') }}
                </button>
                <button
                  v-if="!isNarrowEditor"
                  type="button"
                  class="rd-tb"
                  data-testid="requirement-drafts-tb-collapse"
                  :title="t('pages.workspace.requirementDrafts.collapsePreview')"
                  @click="togglePreviewCollapsed"
                >
                  {{
                    previewCollapsed
                      ? t('pages.workspace.requirementDrafts.expandPreview')
                      : t('pages.workspace.requirementDrafts.collapsePreview')
                  }}
                </button>
              </div>
              <div
                v-if="isNarrowEditor"
                class="flex gap-1.5 border-b border-line/60 py-1.5"
                data-testid="requirement-drafts-mobile-switch"
              >
                <button
                  type="button"
                  class="rounded-md flex-1 border px-2 py-1 text-xs"
                  :class="mobilePane === 'src' ? 'border-accent bg-accent-dim text-txt' : 'border-line text-txt2'"
                  data-testid="requirement-drafts-mobile-src"
                  @click="mobilePane = 'src'"
                >
                  {{ t('pages.workspace.requirementDrafts.source') }}
                </button>
                <button
                  type="button"
                  class="rounded-md flex-1 border px-2 py-1 text-xs"
                  :class="mobilePane === 'prev' ? 'border-accent bg-accent-dim text-txt' : 'border-line text-txt2'"
                  data-testid="requirement-drafts-mobile-prev"
                  @click="mobilePane = 'prev'"
                >
                  {{ t('pages.workspace.requirementDrafts.preview') }}
                </button>
              </div>
              <div
                ref="splitEl"
                class="rd-split flex min-h-[360px] flex-1 border-b border-line/60"
                :class="{
                  'rd-split-narrow': isNarrowEditor,
                  'rd-split-collapsed': previewCollapsed && !isNarrowEditor,
                }"
                data-testid="requirement-drafts-markdown-split"
              >
                <div
                  v-show="showSplitSource"
                  class="rd-pane flex min-w-0 flex-col"
                  :style="sourceWidthStyle"
                  data-testid="requirement-drafts-source-pane"
                >
                  <div class="flex h-7 items-center justify-between border-b border-line bg-elevated px-2.5 text-[11px] tracking-wide text-txt3">
                    <span>{{ t('pages.workspace.requirementDrafts.source') }}</span>
                  </div>
                  <div
                    v-show="findOpen"
                    class="flex items-center gap-1.5 border-b border-line bg-overlay px-2 py-1.5"
                    data-testid="requirement-drafts-findbar"
                  >
                    <input
                      ref="findInputEl"
                      v-model="findQuery"
                      type="search"
                      class="rounded-md min-w-0 flex-1 border border-line bg-base px-2 py-1 text-[12px] text-txt outline-none focus:border-accent"
                      data-testid="requirement-drafts-find-input"
                      :placeholder="t('pages.workspace.requirementDrafts.findPh')"
                      @keydown="onFindInputKeydown"
                    />
                    <button
                      type="button"
                      class="rd-tb"
                      data-testid="requirement-drafts-find-next"
                      @click="runFindNext"
                    >
                      {{ t('pages.workspace.requirementDrafts.findNext') }}
                    </button>
                    <button
                      type="button"
                      class="rd-tb"
                      data-testid="requirement-drafts-find-close"
                      @click="closeFind"
                    >
                      {{ t('pages.workspace.requirementDrafts.findClose') }}
                    </button>
                  </div>
                  <div class="rd-src-wrap relative flex min-h-0 flex-1">
                    <div
                      ref="gutterEl"
                      class="rd-gutter select-none overflow-hidden border-r border-line bg-elevated py-2.5 text-right font-mono text-[12px] leading-[1.6] text-txt3"
                      aria-hidden="true"
                      data-testid="requirement-drafts-gutter"
                    >
                      <div v-for="n in lineNumbers" :key="n" class="pr-2">{{ n }}</div>
                    </div>
                    <div class="relative min-h-0 min-w-0 flex-1">
                      <pre
                        ref="hlEl"
                        class="rd-hl pointer-events-none absolute inset-0 m-0 overflow-auto bg-base p-2.5 font-mono text-[12px] leading-[1.6] text-txt"
                        aria-hidden="true"
                        data-testid="requirement-drafts-highlight"
                        v-html="highlightHtml"
                      />
                      <textarea
                        ref="srcEl"
                        v-model="editBody"
                        class="rd-src absolute inset-0 z-[1] m-0 resize-none overflow-auto border-none bg-transparent p-2.5 font-mono text-[12px] leading-[1.6] text-transparent caret-txt outline-none"
                        data-testid="requirement-drafts-body"
                        spellcheck="false"
                        :placeholder="t('pages.workspace.requirementDrafts.bodyPh')"
                        @scroll="onSrcScroll"
                        @keydown="onSrcKeydown"
                      />
                    </div>
                  </div>
                </div>
                <div
                  v-show="showSplitSource && showSplitPreview"
                  class="rd-sash w-[5px] shrink-0 cursor-col-resize bg-line"
                  :class="sashDragging ? 'bg-accent' : 'hover:bg-accent'"
                  data-testid="requirement-drafts-sash"
                  :title="t('pages.workspace.requirementDrafts.sashTitle')"
                  @mousedown="onSashDown"
                />
                <div
                  v-show="showSplitPreview"
                  class="rd-pane flex min-w-0 flex-1 flex-col"
                  data-testid="requirement-drafts-preview-pane"
                >
                  <div class="flex h-7 items-center justify-between border-b border-line bg-elevated px-2.5 text-[11px] tracking-wide text-txt3">
                    <span>{{ t('pages.workspace.requirementDrafts.preview') }}</span>
                  </div>
                  <div
                    ref="previewEl"
                    class="md min-h-0 flex-1 overflow-auto bg-base px-3.5 py-3.5 text-[13px] leading-relaxed text-txt"
                    data-testid="requirement-drafts-preview"
                    @scroll="onPreviewScroll"
                    v-html="previewHtml"
                  />
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- Gantt view -->
    <div
      v-else-if="viewMode === 'gantt'"
      class="rd-gantt-layout flex min-h-0 flex-1 flex-col lg:flex-row"
    >
      <div
        class="rd-gantt-main flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
        data-testid="requirement-drafts-gantt"
      >
        <div class="flex items-center justify-between gap-3 border-b border-line/60 py-3">
          <SlidingTabs :value="ganttScale" indicator-class="rounded-md bg-elevated" class="rd-scale flex gap-1" data-testid="requirement-drafts-scale-segment">
            <button
              type="button"
              :class="{ on: ganttScale === 'day' }"
              :data-active="ganttScale === 'day'"
              data-testid="requirement-drafts-scale-day"
              @click="setGanttScale('day')"
            >
              {{ t('pages.workspace.requirementDrafts.scaleDay') }}
            </button>
            <button
              type="button"
              :class="{ on: ganttScale === 'week' }"
              :data-active="ganttScale === 'week'"
              data-testid="requirement-drafts-scale-week"
              @click="setGanttScale('week')"
            >
              {{ t('pages.workspace.requirementDrafts.scaleWeek') }}
            </button>
            <button
              type="button"
              :class="{ on: ganttScale === 'month' }"
              :data-active="ganttScale === 'month'"
              data-testid="requirement-drafts-scale-month"
              @click="setGanttScale('month')"
            >
              {{ t('pages.workspace.requirementDrafts.scaleMonth') }}
            </button>
          </SlidingTabs>
          <AppButton
            v-if="selectedDraft && !inspectorOpen"
            variant="ghost"
            size="sm"
            data-testid="requirement-drafts-inspector-reopen"
            @click="inspectorOpen = true"
          >
            {{ t('pages.workspace.requirementDrafts.inspectorTitle') }}
          </AppButton>
        </div>

        <div v-if="loading && !items.length" class="px-5 py-8 text-center text-[13px] text-txt3">
          {{ t('common.loading.label') }}
        </div>

        <div v-else class="min-h-0 flex-1 overflow-auto">
          <!-- Unscheduled zone -->
          <div
            v-if="ganttRows.unscheduled.length"
            class="border-b border-line bg-elevated"
            data-testid="requirement-drafts-unscheduled"
          >
            <div class="border-b border-line px-3 py-1.5 text-[11px] font-medium uppercase tracking-wide text-txt3">
              {{ t('pages.workspace.requirementDrafts.unscheduledTitle') }}
            </div>
            <div
              v-for="row in ganttRows.unscheduled"
              :key="'u-' + row.draft.id"
              class="rd-gantt-row flex cursor-pointer border-b border-line hover:bg-surface"
              :class="isRowSelected(row.draft.id) ? 'bg-accent-dim' : ''"
              @click="revealInspector(row.draft.id)"
            >
              <div class="rd-gantt-name sticky left-0 z-[2] shrink-0 border-r border-line bg-elevated px-3 py-2 text-[13px] text-txt">
                <span v-if="row.contextual" class="mr-1 text-[10px] text-txt3">
                  {{ t('pages.workspace.requirementDrafts.contextualParentHint') }}
                </span>
                {{ row.draft.title }}
              </div>
              <div class="min-w-[200px] flex-1 px-2 py-2 text-[11px] text-txt3">—</div>
            </div>
          </div>

          <p v-if="!ganttRows.scheduled.length" class="m-0 py-5 text-[13px] text-txt3">
            {{ t('pages.workspace.requirementDrafts.ganttEmptyScheduled') }}
          </p>

          <!-- Scheduled timeline -->
          <div v-if="ganttRows.scheduled.length" class="rd-gantt-scroll relative min-w-[640px]">
            <div class="rd-gantt-header flex border-b border-line bg-surface">
              <div class="rd-gantt-name sticky left-0 z-[3] shrink-0 border-r border-line bg-surface px-3 py-2 text-[11px] text-txt3">
                &nbsp;
              </div>
              <div class="relative min-w-0 flex-1">
                <div class="flex">
                  <div
                    v-for="tick in timelineWindow.ticks"
                    :key="tick"
                    class="rd-gantt-tick flex-1 border-r border-line px-1 py-2 text-center text-[10px] text-txt3"
                  >
                    {{ tickLabel(tick, ganttScale) }}
                  </div>
                </div>
                <div
                  class="rd-gantt-today pointer-events-none absolute bottom-0 top-0 z-[1] w-px bg-warn"
                  :style="todayLineStyle"
                  :title="t('pages.workspace.requirementDrafts.todayLabel')"
                />
              </div>
            </div>

            <div
              v-for="row in ganttRows.scheduled"
              :key="'s-' + row.draft.id"
              class="rd-gantt-row relative flex cursor-pointer border-b border-line hover:bg-elevated"
              :class="isRowSelected(row.draft.id) ? 'bg-accent-dim' : ''"
              @click="revealInspector(row.draft.id)"
            >
              <div
                class="rd-gantt-name sticky left-0 z-[2] shrink-0 border-r border-line bg-base px-3 py-2 text-[13px] text-txt"
                :class="row.indent ? 'pl-6' : ''"
              >
                <span v-if="row.contextual" class="mr-1 text-[10px] text-txt3">
                  {{ t('pages.workspace.requirementDrafts.contextualParentHint') }}
                </span>
                <span
                  v-if="row.rowKind === 'group'"
                  class="text-[11px] text-txt3"
                  :title="t('pages.workspace.requirementDrafts.groupRowHint')"
                >
                  {{ row.draft.title }}
                </span>
                <span v-else>{{ row.draft.title }}</span>
              </div>
              <div class="relative min-w-0 flex-1 py-2">
                <div
                  class="rd-gantt-today pointer-events-none absolute bottom-0 top-0 z-[1] w-px bg-warn"
                  :style="todayLineStyle"
                />
                <template v-if="row.rowKind === 'bar' && rowBarStyle(row)">
                  <div
                    class="rd-gantt-bar absolute top-1/2 h-[18px] -translate-y-1/2 cursor-pointer"
                    :style="rowBarStyle(row)!"
                    @click.stop="revealInspector(row.draft.id)"
                  >
                    <div
                      class="rd-gantt-bar-fill h-full"
                      :style="{ width: clampProgress(row.draft.progress) + '%' }"
                    />
                  </div>
                </template>
                <template v-else-if="row.rowKind === 'milestone' && row.draft.dueAt">
                  <div
                    class="rd-gantt-diamond absolute top-1/2 h-3 w-3 -translate-y-1/2 rotate-45 cursor-pointer"
                    :style="diamondStyle(row.draft.dueAt, timelineWindow)"
                    @click.stop="revealInspector(row.draft.id)"
                  />
                </template>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Inspector -->
      <aside
        v-if="selectedDraft && inspectorOpen"
        class="rd-gantt-inspector flex min-h-0 w-full shrink-0 flex-col border-t border-line/60 lg:w-[280px] lg:border-l lg:border-t-0 lg:pl-5"
        data-testid="requirement-drafts-inspector"
      >
        <div class="flex items-center justify-between gap-3 border-b border-line/60 py-3 text-[13px] font-medium text-txt">
          <span>{{ t('pages.workspace.requirementDrafts.inspectorTitle') }}</span>
          <button
            type="button"
            class="inline-flex h-7 w-7 items-center justify-center rounded-md text-txt3 hover:bg-elevated hover:text-txt"
            :aria-label="t('common.buttons.close')"
            data-testid="requirement-drafts-inspector-close"
            @click="inspectorOpen = false"
          >
            <Icon name="close" :size="14" />
          </button>
        </div>
        <div class="rd-inspector-fields flex min-h-0 flex-1 flex-col gap-3 overflow-auto py-4">
          <div class="text-[13px] font-medium text-txt">{{ selectedDraft?.title }}</div>
          <div class="grid gap-3">
            <label class="block text-xs text-txt2">
              {{ t('pages.workspace.requirementDrafts.kindLabel') }}
              <select
                v-model="editKind"
                class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                data-testid="requirement-drafts-inspector-kind"
                :disabled="scheduleBusy"
                @change="onScheduleKindChange"
              >
                <option value="requirement">
                  {{ t('pages.workspace.requirementDrafts.kindRequirement') }}
                </option>
                <option value="milestone">
                  {{ t('pages.workspace.requirementDrafts.kindMilestone') }}
                </option>
              </select>
            </label>
            <label v-if="editKind !== 'milestone'" class="block text-xs text-txt2">
              {{ t('pages.workspace.requirementDrafts.startAtLabel') }}
              <input
                v-model="editStartAt"
                type="date"
                class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                data-testid="requirement-drafts-inspector-start"
                :disabled="scheduleBusy"
                @change="onScheduleStartChange"
              />
            </label>
            <label class="block text-xs text-txt2">
              {{ t('pages.workspace.requirementDrafts.dueAtLabel') }}
              <input
                v-model="editDueAt"
                type="date"
                class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                data-testid="requirement-drafts-inspector-due"
                :disabled="scheduleBusy"
                @change="onScheduleDueChange"
              />
            </label>
            <label class="block text-xs text-txt2">
              {{ t('pages.workspace.requirementDrafts.progressLabel') }}
              <input
                v-model.number="editProgress"
                type="number"
                min="0"
                max="100"
                class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                data-testid="requirement-drafts-inspector-progress"
                :disabled="scheduleBusy"
                @change="onScheduleProgressChange"
              />
            </label>
            <label v-if="canEditParent" class="block text-xs text-txt2">
              {{ t('pages.workspace.requirementDrafts.parentLabel') }}
              <select
                v-model="editParentId"
                class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                data-testid="requirement-drafts-inspector-parent"
                :disabled="scheduleBusy"
                @change="onScheduleParentChange"
              >
                <option :value="null">
                  {{ t('pages.workspace.requirementDrafts.parentNone') }}
                </option>
                <option v-for="p in parentOptions" :key="p.id" :value="p.id">
                  {{ p.title }}
                </option>
              </select>
            </label>
          </div>
          <div v-if="scheduleError" class="text-xs text-err" data-testid="requirement-drafts-inspector-error">
            {{ scheduleError }}
          </div>
          <AppButton
            variant="outline"
            size="sm"
            data-testid="requirement-drafts-open-body"
            @click="openBody()"
          >
            {{ t('pages.workspace.requirementDrafts.openBody') }}
          </AppButton>
        </div>
      </aside>
    </div>

    <!-- Milestones view -->
    <div v-else class="min-h-0 flex-1 overflow-auto py-4">
      <div v-if="loading && !items.length" class="px-5 py-8 text-center text-[13px] text-txt3">
        {{ t('common.loading.label') }}
      </div>
      <div
        v-else-if="!milestoneItems.length"
        class="px-5 py-8 text-center text-[13px] text-txt3"
        data-testid="requirement-drafts-milestones-empty"
      >
        <p class="m-0">{{ t('pages.workspace.requirementDrafts.milestonesEmpty') }}</p>
        <AppButton
          v-if="!query.trim()"
          variant="outline"
          size="sm"
          class="mt-4"
          data-testid="requirement-drafts-new-milestone"
          :disabled="creating"
          @click="onNewMilestone"
        >
          {{ t('pages.workspace.requirementDrafts.new') }} {{ t('pages.workspace.requirementDrafts.kindMilestone') }}
        </AppButton>
      </div>
      <div v-else class="max-w-3xl">
        <div
          v-for="(m, idx) in milestoneItems"
          :key="m.id"
          class="rd-milestone-row flex cursor-pointer gap-4 border-b border-line/60 py-4"
          :class="isRowSelected(m.id) ? 'bg-accent-dim/40' : 'hover:bg-elevated/50'"
          :data-testid="`requirement-drafts-milestone-${m.id}`"
          @click="onPickDraft(m.id)"
        >
          <div class="flex w-6 shrink-0 flex-col items-center pt-1">
            <div class="h-3 w-3 rotate-45 bg-[#34D399]" />
            <div v-if="idx < milestoneItems.length - 1" class="mt-1 w-px flex-1 bg-line" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="text-[13px] font-medium text-txt">{{ m.title }}</div>
            <div class="mt-1 text-[11px] text-txt3">{{ m.dueAt || '—' }} · {{ m.progress }}%</div>
            <div v-if="isRowSelected(m.id)" class="rd-milestone-fields mt-4 grid gap-3 border-t border-line/60 pt-4">
              <label class="block text-xs text-txt2">
                {{ t('pages.workspace.requirementDrafts.dueAtLabel') }}
                <input
                  v-model="editDueAt"
                  type="date"
                  class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                  data-testid="requirement-drafts-milestone-due"
                  :disabled="scheduleBusy"
                  @change="onScheduleDueChange"
                  @click.stop
                />
              </label>
              <label class="block text-xs text-txt2">
                {{ t('pages.workspace.requirementDrafts.progressLabel') }}
                <input
                  v-model.number="editProgress"
                  type="number"
                  min="0"
                  max="100"
                  class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
                  data-testid="requirement-drafts-milestone-progress"
                  :disabled="scheduleBusy"
                  @change="onScheduleProgressChange"
                  @click.stop
                />
              </label>
              <div v-if="scheduleError" class="text-xs text-err">{{ scheduleError }}</div>
              <AppButton
                variant="outline"
                size="sm"
                data-testid="requirement-drafts-open-body"
                @click.stop="openBody(m.id)"
              >
                {{ t('pages.workspace.requirementDrafts.openBody') }}
              </AppButton>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- New draft modal -->
    <AppModal
      :open="showNewModal"
      :title="t('pages.workspace.requirementDrafts.newModalTitle')"
      :width="440"
      close-on-esc
      data-testid="requirement-drafts-new-modal"
      @close="closeNewModal"
    >
      <p class="mb-4 text-[13px] text-txt2">
        {{ t('pages.workspace.requirementDrafts.newModalHint') }}
      </p>
      <div class="mb-4 flex gap-2">
        <button
          type="button"
          class="rounded-lg flex-1 border px-3 py-2 text-[13px]"
          :class="
            newModalKind === 'requirement'
              ? 'border-accent bg-accent-dim text-txt'
              : 'border-line text-txt2'
          "
          data-testid="requirement-drafts-new-kind-requirement"
          @click="newModalKind = 'requirement'"
        >
          {{ t('pages.workspace.requirementDrafts.kindRequirement') }}
        </button>
        <button
          type="button"
          class="rounded-lg flex-1 border px-3 py-2 text-[13px]"
          :class="
            newModalKind === 'milestone'
              ? 'border-accent bg-accent-dim text-txt'
              : 'border-line text-txt2'
          "
          data-testid="requirement-drafts-new-kind-milestone"
          @click="newModalKind = 'milestone'"
        >
          {{ t('pages.workspace.requirementDrafts.kindMilestone') }}
        </button>
      </div>
      <label
        v-if="newModalKind === 'milestone'"
        class="mb-2 block text-xs text-txt2"
      >
        {{ t('pages.workspace.requirementDrafts.milestoneDueLabel') }}
        <input
          v-model="newModalDueAt"
          type="date"
          class="rounded-md mt-1 w-full border border-line bg-base px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
          data-testid="requirement-drafts-new-milestone-due"
        />
      </label>
      <div v-if="newModalError" class="text-xs text-err" data-testid="requirement-drafts-new-modal-error">
        {{ newModalError }}
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <AppButton
            variant="outline"
            size="sm"
            data-testid="requirement-drafts-new-cancel"
            @click="closeNewModal"
          >
            {{ t('common.buttons.cancel') }}
          </AppButton>
          <AppButton
            variant="primary"
            size="sm"
            :disabled="creating"
            data-testid="requirement-drafts-new-confirm"
            @click="onConfirmCreate"
          >
            {{ t('pages.workspace.requirementDrafts.newModalContinue') }}
          </AppButton>
        </div>
      </template>
    </AppModal>

    <AppModal
      :open="showDelete"
      :title="t('pages.workspace.requirementDrafts.deleteTitle')"
      :width="440"
      close-on-esc
      @close="showDelete = false"
    >
      <p class="text-[13px] leading-relaxed text-txt2">
        {{ t('pages.workspace.requirementDrafts.deleteBody') }}
      </p>
      <template #footer>
        <div class="flex justify-end gap-2">
          <AppButton variant="outline" size="sm" data-testid="requirement-drafts-delete-cancel" @click="showDelete = false">
            {{ t('common.buttons.cancel') }}
          </AppButton>
          <AppButton
            variant="outline"
            size="sm"
            class="text-err"
            :disabled="deleting"
            data-testid="requirement-drafts-delete-confirm"
            @click="onConfirmDelete"
          >
            {{ t('pages.workspace.requirementDrafts.deleteConfirm') }}
          </AppButton>
        </div>
      </template>
    </AppModal>

    <AppModal
      :open="showLeave"
      :title="t('pages.workspace.requirementDrafts.leaveTitle')"
      :width="440"
      close-on-esc
      @close="resolveLeave(false)"
    >
      <p class="text-[13px] leading-relaxed text-txt2">
        {{ t('pages.workspace.requirementDrafts.leaveBody') }}
      </p>
      <template #footer>
        <div class="flex justify-end gap-2">
          <AppButton
            variant="outline"
            size="sm"
            data-testid="requirement-drafts-leave-cancel"
            @click="resolveLeave(false)"
          >
            {{ t('common.buttons.cancel') }}
          </AppButton>
          <AppButton
            variant="primary"
            size="sm"
            data-testid="requirement-drafts-leave-confirm"
            @click="resolveLeave(true)"
          >
            {{ t('pages.workspace.requirementDrafts.leaveConfirm') }}
          </AppButton>
        </div>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
/* View navigation and status filtering are distinct, flat controls. */
.requirement-drafts { container-type: inline-size; container-name: requirement-workbench; }
.draft-detail { container-type: inline-size; container-name: requirement-editor; }
.rd-schedule-fields, .rd-milestone-fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.rd-parent-field { grid-column: 1 / -1; }
.rd-view-tabs { display: flex; gap: 24px; border-bottom: 1px solid rgb(var(--c-line) / .6); }
.rd-view-tabs button { padding: 0 0 12px; border-bottom: 2px solid transparent; color: rgb(var(--c-txt3)); font-size: 14px; font-weight: 500; }
.rd-view-tabs button:hover:not(.on) { color: rgb(var(--c-txt2)); }
.rd-view-tabs button.on { border-color: rgb(var(--c-accent)); color: rgb(var(--c-txt)); }
.rd-status-filter button, .rd-scale button { padding: 6px 10px; border-radius: 6px; color: rgb(var(--c-txt3)); font-size: 12px; white-space: nowrap; }
.rd-status-filter button:hover:not(.on), .rd-scale button:hover:not(.on) { color: rgb(var(--c-txt2)); }
.rd-status-filter button.on, .rd-scale button.on { color: rgb(var(--c-txt)); font-weight: 600; }
.draft-detail { overflow-y: auto; }
@container requirement-workbench (max-width: 719px) {
  .rd-filter-toolbar { column-gap: 8px; }
  .rd-filter-toolbar > :first-child { flex-basis: 100%; max-width: none; }
  .draft-layout { display: flex; flex-direction: column; overflow-y: auto; }
  .draft-list-pane { max-height: 180px; flex-shrink: 0; border-right: 0; border-bottom: 1px solid rgb(var(--c-line) / .6); }
  .draft-list-pane > div { padding-right: 0; }
  .draft-detail-selected { min-height: 0; flex: none; overflow: visible; }
  .draft-detail-header, .draft-editor-content { padding-left: 0; }
  .draft-editor-content, .rd-body-editor { flex: none; }
  .rd-split { flex: none; height: 360px; }
  .rd-milestone-fields { grid-template-columns: minmax(0, 1fr); }
  .rd-gantt-name { width: 160px !important; min-width: 160px !important; }
}
@container requirement-editor (max-width: 479px) {
  .rd-schedule-fields { grid-template-columns: minmax(0, 1fr); }
}
@container requirement-workbench (max-width: 959px) {
  .rd-gantt-layout { flex-direction: column; overflow-y: auto; }
  .rd-gantt-main { min-height: 240px; flex-shrink: 0; }
  .rd-gantt-inspector { width: 100%; flex-shrink: 0; padding-left: 0; border-left: 0; border-top: 1px solid rgb(var(--c-line) / .6); }
  .rd-inspector-fields { flex: none; overflow: visible; }
}
.rd-tb {
  min-width: 28px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  padding: 4px 7px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
  color: rgb(var(--c-txt2));
}
.rd-tb:hover {
  border-color: rgb(var(--c-line));
  background: rgb(var(--c-overlay));
  color: rgb(var(--c-txt));
}
.rd-tb-sep {
  width: 1px;
  height: 16px;
  margin: 0 4px;
  background: rgb(var(--c-line-strong));
}
.rd-tb-grow {
  flex: 1;
}
.rd-src-wrap {
  min-height: 280px;
}
.rd-gutter {
  width: 36px;
  flex-shrink: 0;
}
.rd-hl,
.rd-src {
  white-space: pre;
  tab-size: 2;
}
.rd-src::placeholder {
  color: rgb(var(--c-txt3));
}
.rd-schedule-head {
  cursor: pointer;
  background: transparent;
  border: 0;
  padding: 0;
  border-radius: 8px;
}
.rd-schedule-head:hover .rd-schedule-arrow {
  color: rgb(var(--c-txt));
}
.rd-schedule-head:focus-visible {
  outline: 2px solid rgb(var(--c-accent));
  outline-offset: 2px;
}
.rd-schedule-arrow {
  display: inline-block;
  font-size: 12px;
  line-height: 1;
  transition: transform 0.15s ease;
}
.rd-schedule-arrow.is-open {
  transform: rotate(180deg);
}
.rd-gantt-name {
  width: 220px;
  min-width: 220px;
}
.rd-gantt-bar {
  border-radius: 8px;
  background: #7b61ff;
  min-width: 4px;
}
.rd-gantt-bar-fill {
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.35);
  pointer-events: none;
}
.rd-gantt-diamond {
  border-radius: 2px;
  background: #34d399;
}
.rd-gantt-today {
  background: #fbbf24;
}
:deep(.rd-tok-head) {
  color: rgb(var(--c-accent-2));
}
:deep(.rd-tok-bold) {
  color: rgb(var(--c-txt));
  font-weight: 700;
}
:deep(.rd-tok-em) {
  color: rgb(var(--c-accent-2));
}
:deep(.rd-tok-code) {
  color: rgb(var(--c-ok));
}
:deep(.rd-tok-link) {
  color: rgb(var(--c-info));
}
</style>
