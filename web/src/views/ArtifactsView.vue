<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import ArtifactList from '@/components/run/ArtifactList.vue'
import ArtifactPreview from '@/components/run/ArtifactPreview.vue'
import RefreshStrip from '@/components/run/RefreshStrip.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Icon from '@/components/ui/Icon.vue'
import { api, isPaginated } from '@/lib/api/api'
import { groupByRun, UNNAMED_GROUP_KEY } from '@/lib/run/artifactGroups'
import { useArtifactGroupSelection } from '@/lib/run/useArtifactGroupSelection'
import { usePipelineFilter } from '@/lib/composables/usePipelineFilter'
import { useProjectContext } from '@/lib/composables/useProjectContext'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import ProjectFilter from '@/components/ui/ProjectFilter.vue'
import type { Artifact, Run, Workflow } from '@/lib/shared/types'

type MobileStep = 'groups' | 'list' | 'preview'

const PAGE_SIZE = 20
const WF_SEARCH_THRESHOLD = 8
const SEARCH_DEBOUNCE_MS = 300

const { t } = useI18n()
const { isMobile } = useBreakpoint()

const groupArtifacts = ref<Artifact[]>([])
const pageArtifacts = ref<Artifact[]>([])
const pageTotal = ref(0)
const page = ref(1)
const pageLoading = ref(false)
let pageLoadGen = 0
/** First paint + reloadGroups window — true before onMounted so empty groups never flash. */
const groupsLoading = ref(true)
let groupsLoadGen = 0
const workflows = ref<Workflow[]>([])
const workflowsMap = computed(() => {
  const map = new Map<string, { name: string }>()
  const list = Array.isArray(workflows.value) ? workflows.value : []
  for (const wf of list) map.set(wf.id, { name: wf.name })
  return map
})

const { selected } = usePipelineFilter()
const { selected: selectedProject, ensureHydrated: hydrateProject } = useProjectContext()
const {
  groups,
  activeGroup,
  shouldAutoSelectArtifact,
  selectGroup,
} = useArtifactGroupSelection(groupArtifacts, selected, workflowsMap)

/** Dual-track surface: groups and/or paginated list loading. */
const surfaceBusy = computed(() => groupsLoading.value || pageLoading.value)
/** Prefer RefreshStrip when any group/list rows are already on screen. */
const hasCachedSurface = computed(
  () => groups.value.length > 0 || pageArtifacts.value.length > 0,
)

const activeArtifact = ref<Artifact | null>(null)
const previewArtifacts = ref<Artifact[]>([])
/** Full Run for page.html version choices; null when load fails (degrade: no chip). */
const previewRun = ref<Run | null>(null)
const wfFilter = ref('')
const artSearch = ref('')
const searchQ = ref('')
const artifactListRef = ref<InstanceType<typeof ArtifactList> | null>(null)
const mobileStep = ref<MobileStep>('groups')

let searchTimer: ReturnType<typeof setTimeout> | null = null

const runSections = computed(() => groupByRun(pageArtifacts.value))

const showWfSearch = computed(() => groups.value.length >= WF_SEARCH_THRESHOLD)

const visibleWfGroups = computed(() => {
  const q = wfFilter.value.trim().toLowerCase()
  if (!q) return groups.value
  return groups.value.filter((g) => groupDisplayTitle(g).toLowerCase().includes(q))
})

const listEmptyText = computed(() => t('common.empty.noArtifactsInGroup'))

const mobileStepLabel = computed(() => {
  if (mobileStep.value === 'list') return t('pages.artifacts.stepList')
  if (mobileStep.value === 'preview') return t('pages.artifacts.stepPreview')
  return t('pages.artifacts.stepGroups')
})

const showMobileChrome = computed(() => isMobile.value && mobileStep.value !== 'groups')

function groupDisplayTitle(g: { title: string; isUnnamed: boolean }): string {
  if (g.isUnnamed || !g.title) return t('pages.workflowEditor.unnamedWorkflow')
  return g.title
}

function matchWfGroup(g: { title: string; isUnnamed: boolean }): boolean {
  const q = wfFilter.value.trim().toLowerCase()
  if (!q) return true
  return groupDisplayTitle(g).toLowerCase().includes(q)
}

const activeGroupTitle = computed(() => {
  if (!activeGroup.value) return t('pages.artifacts.platformArtifacts')
  return groupDisplayTitle(activeGroup.value)
})

function wfParamForGroup(g: typeof activeGroup.value): string | undefined {
  if (!g) return undefined
  if (g.isUnnamed) return UNNAMED_GROUP_KEY
  return g.workflowId ?? undefined
}

function resetListScroll() {
  nextTick(() => artifactListRef.value?.scrollToTop())
}

async function loadPageArtifacts({ showLoading = false }: { showLoading?: boolean } = {}) {
  const g = activeGroup.value
  if (!g) {
    pageArtifacts.value = []
    pageTotal.value = 0
    return
  }
  const gen = ++pageLoadGen
  if (showLoading) pageLoading.value = true
  try {
    const data = await api.listArtifacts({
      wf: wfParamForGroup(g),
      projectId: selectedProject.value || undefined,
      page: page.value,
      pageSize: PAGE_SIZE,
      q: searchQ.value || undefined,
      groupBy: 'run',
    })
    if (gen !== pageLoadGen) return
    if (isPaginated(data)) {
      pageArtifacts.value = Array.isArray(data.items) ? data.items : []
      pageTotal.value = data.total
    } else {
      pageArtifacts.value = Array.isArray(data) ? data : []
      pageTotal.value = pageArtifacts.value.length
    }
  } catch {
    if (gen !== pageLoadGen) return
    if (!pageArtifacts.value.length) {
      pageArtifacts.value = []
      pageTotal.value = 0
    }
  } finally {
    if (gen === pageLoadGen) pageLoading.value = false
  }
}

function onSelectGroup(key: string, isUnnamed: boolean) {
  activeArtifact.value = null
  page.value = 1
  selectGroup(key, isUnnamed)
  resetListScroll()
  if (isMobile.value) mobileStep.value = 'list'
}

/** User-initiated selection; advances mobile step. Auto-highlight must not call this. */
function selectArtifact(a: Artifact) {
  activeArtifact.value = a
  if (isMobile.value) mobileStep.value = 'preview'
}

function mobileBack() {
  if (mobileStep.value === 'preview') {
    mobileStep.value = 'list'
  } else if (mobileStep.value === 'list') {
    mobileStep.value = 'groups'
    activeArtifact.value = null
  }
}

function onSearchUpdate(q: string) {
  artSearch.value = q
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchQ.value = q.trim()
    page.value = 1
  }, SEARCH_DEBOUNCE_MS)
}

watch(searchQ, () => {
  loadPageArtifacts({ showLoading: true })
})

watch(page, () => {
  loadPageArtifacts({ showLoading: true })
  resetListScroll()
})

watch(activeGroup, (g) => {
  if (!g) activeArtifact.value = null
  page.value = 1
  loadPageArtifacts({ showLoading: true })
  resetListScroll()
})

watch(pageArtifacts, (list) => {
  if (activeArtifact.value && !list.some((a) => a.id === activeArtifact.value!.id)) {
    activeArtifact.value = shouldAutoSelectArtifact.value ? (list[0] ?? null) : null
  } else if (!activeArtifact.value && list.length && shouldAutoSelectArtifact.value) {
    activeArtifact.value = list[0]
  }
})

watch(
  () => activeArtifact.value?.runId,
  async (runId) => {
    previewArtifacts.value = []
    previewRun.value = null
    if (!runId) return
    try {
      const run = await api.getRun(runId)
      previewArtifacts.value = Array.isArray(run.artifacts) ? run.artifacts : []
      previewRun.value = run
    } catch {
      // Run detail unavailable: still preview list content, no version chip (f3).
      previewArtifacts.value = pageArtifacts.value.filter((a) => a.runId === runId)
      previewRun.value = null
    }
  },
  { immediate: true },
)

watch(isMobile, (mobile) => {
  if (mobile) mobileStep.value = 'groups'
})

async function onArtifactDeleted(id: string) {
  const prevPageIdx = pageArtifacts.value.findIndex((a) => a.id === id)
  const prevGroupKey = activeGroup.value?.key ?? null
  const prevGroupIdx = groups.value.findIndex((g) => g.key === prevGroupKey)

  // Precompute next group before filter so activeGroup watch does not briefly
  // fall through to resolveDefaultGroup and trigger a wrong loadPageArtifacts.
  const remaining = groupArtifacts.value.filter((a) => a.id !== id)
  const groupWillExist =
    prevGroupKey != null &&
    remaining.some((a) => (a.workflowId ? a.workflowId : UNNAMED_GROUP_KEY) === prevGroupKey)

  let nextGroup: { key: string; isUnnamed: boolean } | null = null
  if (!groupWillExist) {
    const listWithoutCurrent = groups.value.filter((g) => g.key !== prevGroupKey)
    if (listWithoutCurrent.length > 0) {
      const g =
        prevGroupIdx >= 0
          ? listWithoutCurrent[Math.min(prevGroupIdx, listWithoutCurrent.length - 1)]
          : listWithoutCurrent[0]
      nextGroup = { key: g.key, isUnnamed: g.isUnnamed }
    }
  }

  groupArtifacts.value = remaining
  previewArtifacts.value = previewArtifacts.value.filter((a) => a.id !== id)

  if (!groupWillExist) {
    if (nextGroup) {
      activeArtifact.value = null
      page.value = 1
      selectGroup(nextGroup.key, nextGroup.isUnnamed)
      // activeGroup watch reloads page; pageArtifacts watch auto-selects first
    } else {
      activeArtifact.value = null
      pageArtifacts.value = []
      pageTotal.value = 0
      if (isMobile.value) mobileStep.value = 'groups'
    }
    return
  }

  await loadPageArtifacts()

  if (pageArtifacts.value.length === 0 && page.value > 1) {
    page.value = page.value - 1
    await loadPageArtifacts()
  }

  if (pageArtifacts.value.length === 0) {
    activeArtifact.value = null
    if (isMobile.value) mobileStep.value = 'list'
    return
  }

  if (prevPageIdx >= 0) {
    const nextIdx = Math.min(prevPageIdx, pageArtifacts.value.length - 1)
    activeArtifact.value = pageArtifacts.value[nextIdx]
  } else if (!pageArtifacts.value.some((a) => a.id === activeArtifact.value?.id)) {
    activeArtifact.value = pageArtifacts.value[0]
  }

  if (isMobile.value && !activeArtifact.value) mobileStep.value = 'list'
}

function onSurfaceRetry() {
  if (groupsLoading.value) {
    void reloadGroups()
    return
  }
  void loadPageArtifacts({ showLoading: true })
}

async function reloadGroups() {
  const gen = ++groupsLoadGen
  groupsLoading.value = true
  let failedNoCache = false
  try {
    const pid = selectedProject.value || undefined
    const [arts, wfs] = await Promise.all([
      api.listArtifacts({ projectId: pid }),
      api.listWorkflows({ projectId: pid }),
    ])
    if (gen !== groupsLoadGen) return
    groupArtifacts.value = isPaginated(arts)
      ? (Array.isArray(arts.items) ? arts.items : [])
      : (Array.isArray(arts) ? arts : [])
    // Defend for…of in workflowsMap: never assign a non-array (e.g. unexpected payload shape)
    workflows.value = Array.isArray(wfs) ? wfs : []
  } catch {
    if (gen !== groupsLoadGen) return
    // Keep prior groups/list on failure; only treat as hard-fail when nothing to show.
    if (!groupArtifacts.value.length && !pageArtifacts.value.length) {
      failedNoCache = true
    }
  }
  if (gen !== groupsLoadGen) return
  if (failedNoCache) {
    // Leave groupsLoading true so HardLoadLayer stuck+retry stays (not success empty).
    return
  }
  // Start list load before clearing groupsLoading to avoid a no-overlay gap.
  const pagePromise = loadPageArtifacts({ showLoading: true })
  if (gen === groupsLoadGen) groupsLoading.value = false
  await pagePromise
}

watch(selectedProject, () => {
  page.value = 1
  void reloadGroups()
})

onMounted(async () => {
  hydrateProject()
  await reloadGroups()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="mb-5 flex shrink-0 flex-col gap-3 md:flex-row md:items-start md:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.artifacts.title') }}</h2>
        <p class="text-sm text-txt3" v-html="t('pages.artifacts.subtitle')" />
      </div>
      <ProjectFilter v-model="selectedProject" />
    </div>

    <div
      v-if="showMobileChrome"
      class="mb-2 flex shrink-0 items-center gap-1.5"
    >
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded-md border border-line bg-elevated px-2.5 py-1.5 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
        @click="mobileBack"
      >
        <Icon name="arrow-left" :size="14" />
        {{ t('common.buttons.back') }}
      </button>
      <span class="ml-auto text-[11px] text-txt3">{{ mobileStepLabel }}</span>
    </div>

    <div
      class="card relative flex min-h-0 flex-1 flex-col overflow-hidden"
      :aria-busy="surfaceBusy ? 'true' : 'false'"
    >
      <RefreshStrip v-if="surfaceBusy && hasCachedSurface" />
      <HardLoadLayer
        v-else-if="surfaceBusy && !hasCachedSurface"
        :overlay="true"
        :stuck-after-ms="10_000"
        :stage="t('common.loading.label')"
        @retry="onSurfaceRetry"
      />
      <div class="flex min-h-0 flex-1" :class="isMobile ? 'flex-col' : ''">
        <aside
          v-if="!isMobile || mobileStep === 'groups'"
          class="flex min-h-0 flex-col"
          :class="
            isMobile
              ? 'w-full flex-1 min-w-0 scroll-area overflow-y-auto'
              : 'w-[200px] min-w-[160px] shrink-0 border-r border-line'
          "
        >
          <div class="shrink-0 border-b border-line px-3.5 py-2.5 text-xs font-medium text-txt2">
            {{ t('pages.artifacts.groupsTitle') }}
            <span class="font-normal text-txt3">{{ t('pages.artifacts.groupsCount', { n: groups.length }) }}</span>
          </div>
          <div v-if="showWfSearch" class="shrink-0 border-b border-line px-2.5 py-2">
            <input
              v-model="wfFilter"
              type="search"
              class="w-full rounded-md border border-line bg-base py-1.5 pl-7 pr-2 text-[12px] text-txt outline-none focus:border-accent/55 focus:ring-2 focus:ring-accent/12"
              :placeholder="t('pages.artifacts.wfSearchPlaceholder')"
            />
          </div>
          <div class="scroll-area min-h-0 flex-1 overflow-y-auto p-1.5">
            <div
              v-if="!groups.length && !surfaceBusy"
              class="px-2 py-8 text-center text-[12px] text-txt3"
            >
              {{ t('common.empty.noMatchingGroups') }}
            </div>
            <div
              v-else-if="groups.length && !visibleWfGroups.length"
              class="px-2 py-8 text-center text-[12px] text-txt3"
            >
              {{ t('pages.artifacts.noMatchingWorkflows') }}
            </div>
            <template v-else>
              <button
                v-for="g in groups"
                v-show="matchWfGroup(g)"
                :key="g.key"
                type="button"
                class="mb-1 flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left text-[13px] transition-[background-color,border-color,color] duration-[var(--dur-ui)] ease-out"
                :class="
                  g.key === activeGroup?.key
                    ? 'border-accent/45 bg-accent-dim/55 text-txt'
                    : 'border-transparent text-txt2 hover:border-line hover:bg-elevated'
                "
                @click="onSelectGroup(g.key, g.isUnnamed)"
              >
                <span class="min-w-0 flex-1 truncate" :title="groupDisplayTitle(g)">{{ groupDisplayTitle(g) }}</span>
                <span
                  class="chip shrink-0 text-[11px]"
                  :class="g.key === activeGroup?.key ? 'border-accent/30 text-accent-2' : ''"
                >
                  {{ g.count }}
                </span>
              </button>
            </template>
          </div>
        </aside>

        <section
          v-if="!isMobile || mobileStep === 'list'"
          class="flex min-h-0 flex-col"
          :class="
            isMobile
              ? 'w-full flex-1 min-w-0'
              : 'w-[36%] min-w-[200px] shrink-0 border-r border-line'
          "
        >
          <Transition name="ui-fade" mode="out-in">
            <div
              :key="activeGroup?.key ?? '__none__'"
              class="flex min-h-0 flex-1 flex-col"
            >
              <ArtifactList
                ref="artifactListRef"
                :artifacts="pageArtifacts"
                :run-sections="runSections"
                :active-id="activeArtifact?.id"
                scope="platform"
                server-search
                :search="artSearch"
                :group-total="activeGroup?.count ?? 0"
                :match-total="pageTotal"
                :header-title="activeGroupTitle"
                :header-subtitle="t('pages.artifacts.headerSubtitle')"
                :empty-text="surfaceBusy ? '' : listEmptyText"
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
        </section>

        <section
          v-if="!isMobile || mobileStep === 'preview'"
          class="flex min-h-0 min-w-0 flex-col"
          :class="isMobile ? 'w-full flex-1 scroll-area overflow-y-auto' : 'flex-1'"
        >
          <ArtifactPreview
            :artifact="activeArtifact"
            scope="platform"
            :artifacts="previewArtifacts"
            :run="previewRun"
            :run-id="activeArtifact?.runId"
            @deleted="onArtifactDeleted"
          />
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
input[type='search'] {
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='13' height='13' viewBox='0 0 24 24' fill='none' stroke='%236e6e78' stroke-width='2'%3E%3Ccircle cx='11' cy='11' r='8'/%3E%3Cpath d='m21 21-4.35-4.35'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: 9px center;
}
</style>
