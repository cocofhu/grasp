<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import ArtifactList from '@/components/run/ArtifactList.vue'
import ArtifactPreview from '@/components/run/ArtifactPreview.vue'
import RefreshStrip from '@/components/run/RefreshStrip.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSkeleton from '@/components/ui/AppSkeleton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Icon from '@/components/ui/Icon.vue'
import Pagination from '@/components/ui/Pagination.vue'
import ResourceList from '@/components/ui/ResourceList.vue'
import { api } from '@/lib/api/api'
import {
  buildArtifactTreeNodes,
  describeSelection,
  resolveArtifactSelection,
  selectionFromQuery,
  selectionFromTreeKey,
  selectionScope,
  selectionToQuery,
  selectionTreeKey,
  type ArtifactSelection,
} from '@/lib/artifacts/artifactTree'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { groupByRun } from '@/lib/run/artifactGroups'
import type { Artifact, ArtifactTree, Run } from '@/lib/shared/types'

type MobileStep = 'list' | 'preview'

const PAGE_SIZE = 20
const SEARCH_DEBOUNCE_MS = 300
const SELECTION_QUERY_KEYS = ['workflow', 'session'] as const

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { isMobile } = useBreakpoint()

const tree = ref<ArtifactTree | null>(null)
/** True before onMounted so the empty state never flashes on first paint. */
const treeLoading = ref(true)
const treeFailed = ref(false)
let treeLoadGen = 0

const pageArtifacts = ref<Artifact[]>([])
/** Run count when paging with groupBy=run. */
const pageTotal = ref(0)
const page = ref(1)
const listLoading = ref(false)
const listFailed = ref(false)
let pageLoadGen = 0

const activeArtifact = ref<Artifact | null>(null)
const previewArtifacts = ref<Artifact[]>([])
/** Full Run for page.html version choices; null when load fails (degrade: no chip). */
const previewRun = ref<Run | null>(null)
const searchText = ref('')
const searchQ = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null
const artifactListRef = ref<InstanceType<typeof ArtifactList> | null>(null)
const mobileStep = ref<MobileStep>('list')
const treeSheetOpen = ref(false)
const browserEl = ref<HTMLElement | null>(null)
const scopeTrigger = ref<HTMLButtonElement | null>(null)
const scopePanel = ref<HTMLElement | null>(null)
const contentWidth = ref(0)
let resizeObserver: ResizeObserver | undefined
// The settings directory also takes space on tablets. Adapt to the canvas,
// rather than squeezing two panes into the remaining viewport width.
const singlePane = computed(() => isMobile.value || (contentWidth.value > 0 && contentWidth.value < 800))
const scopePosition = ref({ top: '0px', left: '0px' })

function positionScopePanel() {
  const rect = scopeTrigger.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(360, window.innerWidth - 32)
  scopePosition.value = {
    top: `${Math.min(rect.bottom + 8, Math.max(16, window.innerHeight - 430))}px`,
    left: `${Math.max(16, Math.min(rect.right - width, window.innerWidth - width - 16))}px`,
  }
}

function closeScopePanel() {
  treeSheetOpen.value = false
  nextTick(() => scopeTrigger.value?.focus())
}

function onScopeKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    closeScopePanel()
  } else if (event.key === 'Tab') {
    const buttons = scopePanel.value?.querySelectorAll<HTMLElement>('button, input')
    if (!buttons?.length) return
    const first = buttons[0]!
    const last = buttons[buttons.length - 1]!
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }
}

watch(treeSheetOpen, async (open) => {
  if (!open) return
  positionScopePanel()
  await nextTick()
  scopePanel.value?.querySelector<HTMLInputElement>('input')?.focus()
})

const treeLabels = computed(() => ({
  all: t('pages.artifacts.title'),
  session: t('pages.artifacts.sessionArtifacts'),
  unnamedWorkflow: t('pages.workflowEditor.unnamedWorkflow'),
}))
const treeNodes = computed(() => buildArtifactTreeNodes(tree.value, treeLabels.value))
const treeTotal = computed(() => tree.value?.count ?? 0)

const selection = computed(() => resolveArtifactSelection(tree.value, selectionFromQuery(route.query)))
const activeKey = computed(() => selectionTreeKey(selection.value))
const selectionInfo = computed(() => describeSelection(tree.value, selection.value, treeLabels.value))
const listTitle = computed(() => selectionInfo.value?.childLabel || selectionInfo.value?.rootLabel || '')

const runSections = computed(() => groupByRun(pageArtifacts.value))

const firstLoad = computed(() => treeLoading.value && !tree.value)
const noArtifacts = computed(() => !treeLoading.value && !treeFailed.value && !treeTotal.value)
const treeFailedNoCache = computed(() => treeFailed.value && !tree.value)
const surfaceBusy = computed(() => treeLoading.value || listLoading.value)
const listSkeleton = computed(() => listLoading.value && !pageArtifacts.value.length)

function resetListScroll() {
  nextTick(() => artifactListRef.value?.scrollToTop())
}

function queryWithSelection(sel: ArtifactSelection) {
  const rest = { ...route.query }
  for (const k of SELECTION_QUERY_KEYS) delete rest[k]
  return { ...rest, ...selectionToQuery(sel) }
}

const ownRouteName = route.name

/** The global route keeps changing while this view leaves; never rewrite another page's query. */
function onOwnRoute(): boolean {
  return route.name === ownRouteName
}

function isCanonicalQuery(sel: ArtifactSelection): boolean {
  const want: Record<string, string> = selectionToQuery(sel)
  return SELECTION_QUERY_KEYS.every((k) => (route.query[k] ?? undefined) === want[k])
}

async function loadTree() {
  const gen = ++treeLoadGen
  treeLoading.value = true
  try {
    const data = await api.getArtifactTree()
    if (gen !== treeLoadGen) return
    tree.value = data
    treeFailed.value = false
  } catch {
    if (gen !== treeLoadGen) return
    // Keep the cached tree on refresh failure; only a cold failure blocks the page.
    if (!tree.value) treeFailed.value = true
  } finally {
    if (gen === treeLoadGen) treeLoading.value = false
  }
}

async function loadPage({ showLoading = true }: { showLoading?: boolean } = {}) {
  const sel = selection.value
  const gen = ++pageLoadGen
  if (!sel) {
    pageArtifacts.value = []
    pageTotal.value = 0
    listLoading.value = false
    return
  }
  if (showLoading) listLoading.value = true
  try {
    const data = await api.listArtifacts({
      ...selectionScope(sel),
      q: searchQ.value || undefined,
      page: page.value,
      pageSize: PAGE_SIZE,
      groupBy: 'run',
    })
    if (gen !== pageLoadGen) return
    pageArtifacts.value = Array.isArray(data.items) ? data.items : []
    pageTotal.value = data.total
    listFailed.value = false
  } catch {
    if (gen !== pageLoadGen) return
    if (!pageArtifacts.value.length) listFailed.value = true
  } finally {
    if (gen === pageLoadGen) listLoading.value = false
  }
}

function onTreeSelect(key: string) {
  closeScopePanel()
  const sel = selectionFromTreeKey(key)
  if (!sel || key === activeKey.value) return
  void router.push({ query: queryWithSelection(sel) })
}

function selectRootCrumb() {
  const sel = selection.value
  if (!sel || sel.kind === 'all') return
  onTreeSelect(selectionTreeKey({ kind: 'all' }))
}

/** User-initiated selection opens the preview on narrower canvases. */
function selectArtifact(a: Artifact) {
  activeArtifact.value = a
  if (singlePane.value) mobileStep.value = 'preview'
}

function onSearchUpdate(q: string) {
  searchText.value = q
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchQ.value = q.trim()
  }, SEARCH_DEBOUNCE_MS)
}

function retryTree() {
  void loadTree()
}

function retryList() {
  void loadPage()
}

async function onArtifactDeleted(id: string) {
  const prevIdx = pageArtifacts.value.findIndex((a) => a.id === id)
  const keyBefore = activeKey.value
  pageArtifacts.value = pageArtifacts.value.filter((a) => a.id !== id)
  previewArtifacts.value = previewArtifacts.value.filter((a) => a.id !== id)

  await loadTree()
  // The selected bucket vanished: selection fell back and the activeKey watcher reloads.
  if (activeKey.value !== keyBefore) return

  await loadPage({ showLoading: false })
  if (!pageArtifacts.value.length && page.value > 1) {
    if (singlePane.value) mobileStep.value = 'list'
    page.value -= 1
    return
  }
  const list = pageArtifacts.value
  activeArtifact.value = list.length && prevIdx >= 0 ? list[Math.min(prevIdx, list.length - 1)] : (list[0] ?? null)
  if (singlePane.value && !activeArtifact.value) mobileStep.value = 'list'
}

watch(selection, (sel) => {
  if (!onOwnRoute()) return
  if (sel && !isCanonicalQuery(sel)) void router.replace({ query: queryWithSelection(sel) })
})

watch([activeKey, searchQ], () => {
  if (!onOwnRoute()) return
  // Cached rows belong to the previous source/search. Keep warm content only
  // when refreshing that same result, never under another source's heading.
  activeArtifact.value = null
  pageArtifacts.value = []
  pageTotal.value = 0
  listFailed.value = false
  mobileStep.value = 'list'
  if (page.value !== 1) {
    page.value = 1
    return
  }
  void loadPage()
  resetListScroll()
})

watch(page, () => {
  activeArtifact.value = null
  pageArtifacts.value = []
  listFailed.value = false
  void loadPage()
  resetListScroll()
})

watch(pageArtifacts, (list) => {
  if (activeArtifact.value && list.some((a) => a.id === activeArtifact.value!.id)) return
  activeArtifact.value = singlePane.value ? null : (list[0] ?? null)
})

watch(
  () => activeArtifact.value?.runId,
  async (runId) => {
    previewArtifacts.value = []
    previewRun.value = null
    if (!runId) return
    try {
      const run = await api.getRun(runId)
      if (activeArtifact.value?.runId !== runId) return
      previewArtifacts.value = Array.isArray(run.artifacts) ? run.artifacts : []
      previewRun.value = run
    } catch {
      if (activeArtifact.value?.runId !== runId) return
      // Run detail unavailable: still preview list content, no version chip.
      previewArtifacts.value = pageArtifacts.value.filter((a) => a.runId === runId)
      previewRun.value = null
    }
  },
)

watch(singlePane, () => {
  mobileStep.value = 'list'
})

onMounted(() => {
  if (browserEl.value) {
    contentWidth.value = browserEl.value.clientWidth
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(([entry]) => {
        if (entry) contentWidth.value = entry.contentRect.width
        if (treeSheetOpen.value) positionScopePanel()
      })
      resizeObserver.observe(browserEl.value)
    }
  }
  void loadTree()
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  resizeObserver?.disconnect()
})
</script>

<template>
  <div ref="browserEl" class="artifact-library flex h-full min-h-0 flex-col" :class="{ 'artifact-library--single': singlePane }">
    <header class="artifact-library-heading">
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-3">
          <h2 class="text-lg font-semibold text-txt">{{ t('pages.artifacts.title') }}</h2>
          <span v-if="!firstLoad && !treeFailedNoCache" class="text-xs tabular-nums text-txt3" data-testid="artifacts-total">{{ t('pages.artifacts.total', { n: treeTotal }) }}</span>
        </div>
        <p class="mt-1 text-sm text-txt3">{{ t('pages.artifacts.subtitle') }}</p>
      </div>
      <button
        v-if="!noArtifacts && !treeFailedNoCache"
        ref="scopeTrigger"
        type="button"
        data-testid="artifacts-tree-toggle"
        class="artifact-scope-trigger"
        :disabled="firstLoad"
        :aria-label="t('pages.artifacts.switchScope')"
        :aria-expanded="treeSheetOpen"
        aria-haspopup="dialog"
        @click="treeSheetOpen = !treeSheetOpen"
      >
        <Icon name="filter" :size="15" class="shrink-0 text-txt3" />
        <span class="min-w-0 truncate">{{ selectionInfo?.childLabel || t('pages.artifacts.allSources') }}</span>
        <Icon name="chevron-down" :size="13" class="shrink-0 text-txt3" />
      </button>
    </header>

    <div class="relative flex min-h-0 flex-1 flex-col" data-testid="artifacts-surface" :aria-busy="surfaceBusy ? 'true' : 'false'">
      <div v-if="treeFailedNoCache" role="status" data-testid="artifacts-load-failed" class="flex flex-1 flex-col items-center justify-center px-6 text-center">
        <Icon name="alert" :size="22" class="mb-3 text-err" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="artifacts-retry" @click="retryTree">{{ t('common.buttons.retry') }}</AppButton>
      </div>
      <div v-else-if="noArtifacts" class="flex flex-1 items-center justify-center" data-testid="artifacts-empty">
        <EmptyState icon="artifact" :title="t('pages.artifacts.emptyTitle')" :desc="t('pages.artifacts.emptyDesc')" />
      </div>

      <template v-else>
        <div class="artifact-location-bar">
          <button v-if="singlePane && mobileStep === 'preview'" type="button" data-testid="artifacts-mobile-back" class="artifact-back" @click="mobileStep = 'list'">
            <Icon name="arrow-left" :size="15" />{{ t('pages.artifacts.backToFiles') }}
          </button>
          <nav class="flex min-w-0 flex-1 items-center gap-1.5 text-xs" :aria-label="t('pages.artifacts.breadcrumbAria')" data-testid="artifacts-breadcrumb">
            <div v-if="firstLoad" class="h-3 w-40 animate-pulse rounded bg-elevated" aria-hidden="true" />
            <template v-else-if="selectionInfo">
              <button v-if="selectionInfo.childLabel" type="button" data-testid="artifacts-crumb-root" class="shrink-0 text-txt3 hover:text-txt" @click="selectRootCrumb">{{ selectionInfo.rootLabel }}</button>
              <span v-else class="text-txt2" aria-current="page">{{ selectionInfo.rootLabel }}</span>
              <template v-if="selectionInfo.childLabel">
                <Icon name="chevron-right" :size="12" class="shrink-0 text-txt3" />
                <span class="min-w-0 truncate text-txt2" aria-current="page" :title="selectionInfo.childLabel">{{ selectionInfo.childLabel }}</span>
              </template>
            </template>
          </nav>
          <span class="shrink-0 text-[11px] text-txt3">{{ t('pages.artifacts.newestFirst') }}</span>
        </div>
        <RefreshStrip v-if="surfaceBusy && !firstLoad && !listSkeleton" class="shrink-0" />
        <div class="artifact-browser min-h-0 flex-1">
          <div v-if="!singlePane || mobileStep === 'list'" class="artifact-files relative flex min-h-0 min-w-0 flex-col" data-testid="artifacts-list-pane">
            <div v-if="firstLoad || listSkeleton" class="p-3" data-testid="artifacts-list-skeleton">
              <div class="mb-3 h-8 w-full animate-pulse rounded-md bg-elevated" aria-hidden="true" />
              <AppSkeleton list :rows="6" />
            </div>
            <div v-else-if="listFailed" role="status" data-testid="artifacts-list-failed" class="flex flex-1 flex-col items-center justify-center px-6 text-center">
              <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
              <p class="mt-1 max-w-xs text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
              <AppButton class="mt-4" size="sm" variant="outline" @click="retryList">{{ t('common.buttons.retry') }}</AppButton>
            </div>
            <Transition v-else name="ui-fade" mode="out-in">
              <div :key="activeKey" class="flex min-h-0 flex-1 flex-col">
                <ArtifactList ref="artifactListRef" :artifacts="pageArtifacts" :run-sections="runSections" :active-id="activeArtifact?.id" scope="platform" server-search :search="searchText" :group-total="selectionInfo?.count ?? 0" :header-title="listTitle" :empty-text="t('pages.artifacts.emptySelection')" class="min-h-0 flex-1" @select="selectArtifact" @update:search="onSearchUpdate" />
                <Pagination v-if="pageTotal > PAGE_SIZE" v-model:page="page" :page-size="PAGE_SIZE" :total="pageTotal" class="shrink-0" />
              </div>
            </Transition>
          </div>
          <div v-if="!singlePane || mobileStep === 'preview'" class="artifact-preview flex min-h-0 min-w-0 flex-col" data-testid="artifacts-preview-pane">
            <div v-if="firstLoad" class="p-4" aria-hidden="true">
              <div class="mb-3 h-6 w-48 animate-pulse rounded bg-elevated" />
              <div class="h-64 w-full animate-pulse rounded-md bg-elevated" />
            </div>
            <ArtifactPreview v-else :artifact="activeArtifact" scope="platform" :artifacts="previewArtifacts" :run="previewRun" :run-id="activeArtifact?.runId" @deleted="onArtifactDeleted" />
          </div>
        </div>
      </template>
    </div>

    <Teleport to="body">
      <Transition name="ui-fade">
        <div v-if="treeSheetOpen" class="fixed inset-0 z-40" data-testid="artifacts-tree-sheet">
          <div class="absolute inset-0" :class="isMobile ? 'bg-black/35' : ''" @click="closeScopePanel" />
          <div ref="scopePanel" role="dialog" aria-modal="true" :aria-label="t('pages.artifacts.switchScope')" class="artifact-source-panel" :class="{ 'artifact-source-panel--mobile': isMobile }" :style="isMobile ? undefined : scopePosition" @keydown="onScopeKeydown">
            <div class="flex shrink-0 items-center gap-2 border-b border-line px-4 py-2">
              <h3 class="min-w-0 flex-1 text-sm font-medium text-txt">{{ t('pages.artifacts.switchScope') }}</h3>
              <button type="button" data-testid="artifacts-tree-sheet-close" class="flex h-8 w-8 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt" :aria-label="t('common.buttons.close')" @click="closeScopePanel"><Icon name="close" :size="15" /></button>
            </div>
            <ResourceList :nodes="treeNodes" :active-key="activeKey" :title="t('pages.artifacts.treeTitle')" :total="tree ? treeTotal : undefined" :search-placeholder="t('pages.artifacts.treeSearchPlaceholder')" :loading="treeLoading" :empty-text="t('pages.artifacts.noMatchingSources')" @select="onTreeSelect" />
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.artifact-library-heading { display: flex; align-items: center; gap: 20px; padding-bottom: 24px; }
.artifact-scope-trigger { display: inline-flex; align-items: center; gap: 8px; max-width: 280px; min-height: 36px; padding: 0 10px; border: 1px solid rgb(var(--c-line)); border-radius: 6px; color: rgb(var(--c-txt2)); font-size: 12px; }
.artifact-scope-trigger:hover { background: rgb(var(--c-overlay)); }
.artifact-location-bar { display: flex; align-items: center; gap: 14px; min-height: 42px; flex-shrink: 0; border-top: 1px solid rgb(var(--c-line)); border-bottom: 1px solid rgb(var(--c-line)); }
.artifact-back { display: inline-flex; align-items: center; gap: 6px; flex-shrink: 0; min-height: 36px; color: rgb(var(--c-txt2)); font-size: 12px; }
.artifact-browser { display: grid; grid-template-columns: clamp(280px, 32%, 370px) minmax(0, 1fr); }
.artifact-files { border-right: 1px solid rgb(var(--c-line)); }
.artifact-preview { overflow: hidden; }
.artifact-library--single .artifact-browser { grid-template-columns: minmax(0, 1fr); }
.artifact-library--single .artifact-files { border-right: 0; }
.artifact-library--single .artifact-library-heading { align-items: flex-start; flex-direction: column; gap: 14px; padding-bottom: 18px; }
.artifact-library--single .artifact-scope-trigger { max-width: 100%; }
.artifact-source-panel { position: fixed; display: flex; flex-direction: column; width: min(360px, calc(100vw - 32px)); height: min(414px, calc(100dvh - 32px)); overflow: hidden; border: 1px solid rgb(var(--c-line)); border-radius: 8px; background: rgb(var(--c-surface)); box-shadow: 0 8px 32px rgb(0 0 0 / .12); }
.artifact-source-panel--mobile { inset: auto 0 0; width: 100%; height: min(480px, 75dvh); padding-bottom: env(safe-area-inset-bottom, 0px); border-radius: 12px 12px 0 0; }
.artifact-source-panel :deep(.scroll-area) { overscroll-behavior: contain; }
@media (max-width: 767px) {
  .artifact-library { padding-bottom: 64px; }
  .artifact-library-heading p { font-size: 12px; }
}
</style>
