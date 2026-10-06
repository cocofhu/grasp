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
import ProjectTree from '@/components/ui/ProjectTree.vue'
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
import type { Artifact, ArtifactTreeProject, Run } from '@/lib/shared/types'

type MobileStep = 'list' | 'preview'

const PAGE_SIZE = 20
const SEARCH_DEBOUNCE_MS = 300
const SELECTION_QUERY_KEYS = ['project', 'workflow', 'session'] as const

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { isMobile } = useBreakpoint()

const tree = ref<ArtifactTreeProject[]>([])
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

const treeLabels = computed(() => ({
  session: t('pages.artifacts.sessionArtifacts'),
  unnamedWorkflow: t('pages.workflowEditor.unnamedWorkflow'),
}))
const treeNodes = computed(() => buildArtifactTreeNodes(tree.value, treeLabels.value))
const treeTotal = computed(() => tree.value.reduce((n, p) => n + p.count, 0))

const selection = computed(() => resolveArtifactSelection(tree.value, selectionFromQuery(route.query)))
const activeKey = computed(() => selectionTreeKey(selection.value))
const selectionInfo = computed(() => describeSelection(tree.value, selection.value, treeLabels.value))
const listTitle = computed(() => selectionInfo.value?.childLabel || selectionInfo.value?.projectLabel || '')

const runSections = computed(() => groupByRun(pageArtifacts.value))

const firstLoad = computed(() => treeLoading.value && !tree.value.length)
const noArtifacts = computed(() => !treeLoading.value && !treeFailed.value && !tree.value.length)
const treeFailedNoCache = computed(() => treeFailed.value && !tree.value.length)
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
    tree.value = Array.isArray(data) ? data : []
    treeFailed.value = false
  } catch {
    if (gen !== treeLoadGen) return
    // Keep the cached tree on refresh failure; only a cold failure blocks the page.
    if (!tree.value.length) treeFailed.value = true
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
  treeSheetOpen.value = false
  const sel = selectionFromTreeKey(key)
  if (!sel || key === activeKey.value) return
  void router.push({ query: queryWithSelection(sel) })
}

function selectProjectCrumb() {
  const sel = selection.value
  if (!sel || sel.kind === 'project') return
  onTreeSelect(selectionTreeKey({ kind: 'project', projectId: sel.projectId }))
}

/** User-initiated selection; advances the mobile step. */
function selectArtifact(a: Artifact) {
  activeArtifact.value = a
  if (isMobile.value) mobileStep.value = 'preview'
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
    page.value -= 1
    return
  }
  const list = pageArtifacts.value
  activeArtifact.value = list.length && prevIdx >= 0 ? list[Math.min(prevIdx, list.length - 1)] : (list[0] ?? null)
  if (isMobile.value && !activeArtifact.value) mobileStep.value = 'list'
}

watch(selection, (sel) => {
  if (!onOwnRoute()) return
  if (sel && !isCanonicalQuery(sel)) void router.replace({ query: queryWithSelection(sel) })
})

watch([activeKey, searchQ], ([key], [prevKey]) => {
  if (!onOwnRoute()) return
  if (key !== prevKey) {
    activeArtifact.value = null
    mobileStep.value = 'list'
  }
  if (page.value !== 1) {
    page.value = 1
    return
  }
  void loadPage()
  resetListScroll()
})

watch(page, () => {
  void loadPage()
  resetListScroll()
})

watch(pageArtifacts, (list) => {
  if (activeArtifact.value && list.some((a) => a.id === activeArtifact.value!.id)) return
  activeArtifact.value = isMobile.value ? null : (list[0] ?? null)
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

watch(isMobile, (mobile) => {
  mobileStep.value = 'list'
  if (!mobile) treeSheetOpen.value = false
})

onMounted(() => {
  void loadTree()
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="mb-5 min-w-0 shrink-0">
      <h2 class="text-lg font-semibold text-txt">{{ t('pages.artifacts.title') }}</h2>
      <p class="text-sm text-txt3" v-html="t('pages.artifacts.subtitle')" />
    </div>

    <div
      class="card relative flex min-h-0 flex-1 overflow-hidden"
      data-testid="artifacts-surface"
      :aria-busy="surfaceBusy ? 'true' : 'false'"
    >
      <!-- cold load failure -->
      <div
        v-if="treeFailedNoCache"
        role="status"
        data-testid="artifacts-load-failed"
        class="flex flex-1 flex-col items-center justify-center px-6 text-center"
      >
        <Icon name="alert" :size="22" class="mb-3 text-err" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="artifacts-retry" @click="retryTree">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <!-- nothing produced anywhere yet -->
      <div v-else-if="noArtifacts" class="flex flex-1 items-center justify-center" data-testid="artifacts-empty">
        <EmptyState
          icon="artifact"
          :title="t('pages.artifacts.emptyTitle')"
          :desc="t('pages.artifacts.emptyDesc')"
        />
      </div>

      <template v-else>
        <aside
          v-if="!isMobile"
          class="flex w-[260px] min-w-[220px] shrink-0 flex-col border-r border-line bg-base/40"
          data-testid="artifacts-tree-pane"
        >
          <ProjectTree
            :nodes="treeNodes"
            :active-key="activeKey"
            :title="t('pages.artifacts.treeTitle')"
            :total="tree.length ? treeTotal : undefined"
            :search-placeholder="t('pages.artifacts.treeSearchPlaceholder')"
            :loading="treeLoading"
            :empty-text="t('pages.artifacts.emptyTitle')"
            storage-key="artifacts-tree"
            @select="onTreeSelect"
          />
        </aside>

        <section class="flex min-h-0 min-w-0 flex-1 flex-col">
          <div
            class="flex h-10 shrink-0 items-center gap-2 border-b border-line px-3"
            :class="isMobile ? 'min-h-11 h-auto py-1.5' : ''"
          >
            <button
              v-if="isMobile && mobileStep === 'preview'"
              type="button"
              data-testid="artifacts-mobile-back"
              class="inline-flex min-h-9 shrink-0 items-center gap-1 rounded-md border border-line bg-elevated px-2 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
              @click="mobileStep = 'list'"
            >
              <Icon name="arrow-left" :size="14" />
              {{ t('common.buttons.back') }}
            </button>
            <nav
              class="flex min-w-0 flex-1 items-center gap-1 text-[12.5px]"
              :aria-label="t('pages.artifacts.breadcrumbAria')"
              data-testid="artifacts-breadcrumb"
            >
              <div v-if="firstLoad" class="h-3 w-40 animate-pulse rounded bg-elevated" aria-hidden="true" />
              <template v-else-if="selectionInfo">
                <Icon name="folder" :size="13" class="shrink-0 text-warn" />
                <button
                  v-if="selectionInfo.childLabel"
                  type="button"
                  data-testid="artifacts-crumb-project"
                  class="min-w-0 truncate rounded px-0.5 text-txt3 transition hover:text-txt"
                  :title="selectionInfo.projectLabel"
                  @click="selectProjectCrumb"
                >{{ selectionInfo.projectLabel }}</button>
                <span
                  v-else
                  class="min-w-0 truncate font-medium text-txt"
                  aria-current="page"
                  :title="selectionInfo.projectLabel"
                >{{ selectionInfo.projectLabel }}</span>
                <template v-if="selectionInfo.childLabel">
                  <Icon name="chevron-right" :size="12" class="shrink-0 text-txt3" />
                  <Icon
                    :name="selection?.kind === 'session' ? 'robot' : 'workflow'"
                    :size="13"
                    class="shrink-0 text-accent-2"
                  />
                  <span
                    class="min-w-0 truncate font-medium text-txt"
                    aria-current="page"
                    :title="selectionInfo.childLabel"
                  >{{ selectionInfo.childLabel }}</span>
                </template>
              </template>
            </nav>
            <button
              v-if="isMobile"
              type="button"
              data-testid="artifacts-tree-toggle"
              class="inline-flex min-h-9 shrink-0 items-center gap-1 rounded-md border border-line bg-elevated px-2.5 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
              :aria-label="t('pages.artifacts.switchScope')"
              :aria-expanded="treeSheetOpen"
              @click="treeSheetOpen = true"
            >
              <Icon name="menu" :size="14" />
              <span>{{ t('pages.artifacts.switchScope') }}</span>
            </button>
          </div>

          <div class="flex min-h-0 flex-1" :class="isMobile ? 'flex-col' : ''">
            <div
              v-if="!isMobile || mobileStep === 'list'"
              class="relative flex min-h-0 flex-col"
              :class="isMobile ? 'w-full flex-1' : 'w-[38%] min-w-[260px] max-w-[460px] shrink-0 border-r border-line'"
              data-testid="artifacts-list-pane"
            >
              <RefreshStrip v-if="surfaceBusy && !firstLoad && !listSkeleton" class="shrink-0" />
              <div v-if="firstLoad || listSkeleton" class="p-3" data-testid="artifacts-list-skeleton">
                <div class="mb-3 h-8 w-full animate-pulse rounded-md bg-elevated" aria-hidden="true" />
                <AppSkeleton list :rows="6" />
              </div>
              <div
                v-else-if="listFailed"
                role="status"
                data-testid="artifacts-list-failed"
                class="flex flex-1 flex-col items-center justify-center px-6 text-center"
              >
                <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
                <p class="mt-1 max-w-xs text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
                <AppButton class="mt-4" size="sm" variant="outline" @click="retryList">
                  {{ t('common.buttons.retry') }}
                </AppButton>
              </div>
              <Transition v-else name="ui-fade" mode="out-in">
                <div :key="activeKey" class="flex min-h-0 flex-1 flex-col">
                  <ArtifactList
                    ref="artifactListRef"
                    :artifacts="pageArtifacts"
                    :run-sections="runSections"
                    :active-id="activeArtifact?.id"
                    scope="platform"
                    server-search
                    :search="searchText"
                    :group-total="selectionInfo?.count ?? 0"
                    :header-title="listTitle"
                    :header-subtitle="t('pages.artifacts.headerSubtitle')"
                    :empty-text="t('pages.artifacts.emptySelection')"
                    class="min-h-0 flex-1"
                    @select="selectArtifact"
                    @update:search="onSearchUpdate"
                  />
                  <Pagination
                    v-if="pageTotal > PAGE_SIZE"
                    v-model:page="page"
                    :page-size="PAGE_SIZE"
                    :total="pageTotal"
                    class="shrink-0"
                  />
                </div>
              </Transition>
            </div>

            <div
              v-if="!isMobile || mobileStep === 'preview'"
              class="flex min-h-0 min-w-0 flex-1 flex-col"
              :class="isMobile ? 'scroll-area w-full overflow-y-auto' : ''"
              data-testid="artifacts-preview-pane"
            >
              <div v-if="firstLoad" class="p-4" aria-hidden="true">
                <div class="mb-3 h-6 w-48 animate-pulse rounded bg-elevated" />
                <div class="h-64 w-full animate-pulse rounded-md bg-elevated" />
              </div>
              <ArtifactPreview
                v-else
                :artifact="activeArtifact"
                scope="platform"
                :artifacts="previewArtifacts"
                :run="previewRun"
                :run-id="activeArtifact?.runId"
                @deleted="onArtifactDeleted"
              />
            </div>
          </div>
        </section>
      </template>
    </div>

    <!-- narrow-screen project tree sheet -->
    <Teleport to="body">
      <Transition name="ui-fade">
        <div
          v-if="isMobile && treeSheetOpen"
          data-testid="artifacts-tree-sheet"
          class="fixed inset-0 z-40"
          role="dialog"
          aria-modal="true"
          :aria-label="t('pages.artifacts.treeTitle')"
        >
          <div class="absolute inset-0 bg-black/50" @click="treeSheetOpen = false" />
          <div
            class="absolute inset-x-0 bottom-0 flex h-[70vh] max-h-[70vh] flex-col overflow-hidden rounded-t-xl border-t border-line bg-elevated shadow-card"
          >
            <div class="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-line-strong/70" aria-hidden="true" />
            <div class="flex shrink-0 items-center gap-1.5 border-b border-line px-3 py-2.5">
              <h3 class="min-w-0 flex-1 truncate text-[14px] font-semibold text-txt">
                {{ t('pages.artifacts.switchScope') }}
              </h3>
              <button
                type="button"
                data-testid="artifacts-tree-sheet-close"
                class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
                :aria-label="t('common.buttons.close')"
                @click="treeSheetOpen = false"
              >
                <Icon name="close" :size="14" />
              </button>
            </div>
            <div class="min-h-0 flex-1 overflow-hidden">
              <ProjectTree
                :nodes="treeNodes"
                :active-key="activeKey"
                :title="t('pages.artifacts.treeTitle')"
                :total="tree.length ? treeTotal : undefined"
                :search-placeholder="t('pages.artifacts.treeSearchPlaceholder')"
                :loading="treeLoading"
                :empty-text="t('pages.artifacts.emptyTitle')"
                storage-key="artifacts-tree"
                @select="onTreeSelect"
              />
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>
