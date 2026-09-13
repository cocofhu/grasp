<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { fmtTime } from '@/lib/shared/format'
import { runSectionTitle } from '@/lib/run/artifactGroups'
import {
  artifactFriendlyNameKey,
  artifactTechnicalDisplayName,
} from '@/lib/run/reactArtifactPreview'
import { downloadZip } from '@/lib/agent/agentIO'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { isFeedbackArtifactName } from './StructuredArtifactView.vue'
import type { Artifact } from '@/lib/shared/types'
import type { RunSection } from '@/lib/run/artifactGroups'

const props = withDefaults(
  defineProps<{
    artifacts: Artifact[]
    runSections?: RunSection[]
    activeId?: string | null
    scope?: 'run' | 'platform'
    serverSearch?: boolean
    search?: string
    groupTotal?: number
    matchTotal?: number
    headerTitle?: string
    headerSubtitle?: string
    emptyText?: string
  }>(),
  {
    activeId: null,
    runSections: () => [],
    scope: 'run',
    serverSearch: false,
    search: '',
    groupTotal: undefined,
    matchTotal: undefined,
    headerTitle: '',
    headerSubtitle: '',
    emptyText: '',
  },
)

const emit = defineEmits<{
  (e: 'select', artifact: Artifact): void
  (e: 'update:search', value: string): void
}>()

const { t } = useI18n()
const toast = useToast()

const artFilter = ref('')
const collapsedRuns = ref<Set<string>>(new Set())
const packingRuns = ref<Set<string>>(new Set())
const scrollEl = ref<HTMLElement | null>(null)
// A long review produces one product per round; left flat they would bury the
// actual deliverables, so the ledger gets its own collapsed group.
const feedbackOpen = ref(false)

const feedbackItems = computed(() => props.artifacts.filter((a) => isFeedbackArtifactName(a.name)))
const plainItems = computed(() => props.artifacts.filter((a) => !isFeedbackArtifactName(a.name)))

const kindIcon: Record<string, string> = { markdown: 'doc', json: 'doc', yaml: 'doc', html: 'dashboard' }

function defaultHeaderTitle() {
  return props.scope === 'platform' ? t('pages.artifactList.platform') : t('pages.artifactList.run')
}

function defaultEmptyText() {
  return props.scope === 'platform' ? t('pages.artifactList.emptyPlatform') : t('pages.artifactList.emptyRun')
}

function runLabel(sec: RunSection): string {
  return runSectionTitle(sec.runTitle, sec.runId)
}

function effectiveSearch(): string {
  return props.serverSearch ? props.search.trim() : artFilter.value.trim()
}

function artifactFriendlyName(a: Artifact): string {
  const key = artifactFriendlyNameKey(a.name)
  return key ? t(key) : ''
}

function artifactDisplayName(a: Artifact): string {
  return artifactFriendlyName(a) || artifactTechnicalDisplayName(a.name)
}

function matchArt(a: Artifact, label: string): boolean {
  if (props.serverSearch) return true
  const q = artFilter.value.trim().toLowerCase()
  if (!q) return true
  return (
    a.name.toLowerCase().includes(q) ||
    artifactFriendlyName(a).toLowerCase().includes(q) ||
    a.nodeId.toLowerCase().includes(q) ||
    label.toLowerCase().includes(q)
  )
}

const headerCount = computed(() => {
  if (props.serverSearch && props.groupTotal != null) return props.groupTotal
  return props.artifacts.length
})

const visibleMatchCount = computed(() => {
  if (props.serverSearch) return props.matchTotal ?? props.artifacts.length
  if (props.scope !== 'platform' || !props.runSections.length) return props.artifacts.length
  let count = 0
  for (const sec of props.runSections) {
    const label = runLabel(sec)
    count += sec.items.filter((a) => matchArt(a, label)).length
  }
  return count
})

const matchCountLabel = computed(() => {
  const q = effectiveSearch()
  if (!q) return ''
  if (props.serverSearch) {
    if (props.matchTotal == null || props.groupTotal == null) return ''
    if (props.matchTotal >= props.groupTotal) return ''
    return t('pages.artifactList.matchCount', { n: props.matchTotal })
  }
  if (visibleMatchCount.value === props.artifacts.length) return ''
  return t('pages.artifactList.matchCount', { n: visibleMatchCount.value })
})

const platformListEmpty = computed(() => {
  if (!props.artifacts.length) {
    if (props.serverSearch && effectiveSearch()) {
      return t('pages.artifactList.noMatchingArtifacts')
    }
    return props.emptyText || defaultEmptyText()
  }
  if (!props.serverSearch && artFilter.value.trim() && visibleMatchCount.value === 0) {
    return t('pages.artifactList.noMatchingArtifacts')
  }
  return ''
})

const visibleRunSections = computed(() => {
  if (props.serverSearch || props.scope !== 'platform') return props.runSections
  const q = artFilter.value.trim()
  if (!q) return props.runSections
  return props.runSections.filter((sec) => {
    const label = runLabel(sec)
    return sec.items.some((a) => matchArt(a, label))
  })
})

function isRunCollapsed(runId: string): boolean {
  return collapsedRuns.value.has(runId)
}

function isPacking(runId: string): boolean {
  return packingRuns.value.has(runId)
}

function toggleRun(runId: string) {
  const next = new Set(collapsedRuns.value)
  if (next.has(runId)) next.delete(runId)
  else next.add(runId)
  collapsedRuns.value = next
}

function expandAllRuns() {
  collapsedRuns.value = new Set()
}

function collapseAllRuns() {
  collapsedRuns.value = new Set(props.runSections.map((s) => s.runId))
}

function runCountLabel(sec: RunSection): string {
  if (props.serverSearch) return String(sec.items.length)
  const label = runLabel(sec)
  const q = artFilter.value.trim()
  if (!q) return String(sec.items.length)
  const matched = sec.items.filter((a) => matchArt(a, label)).length
  return `${matched}/${sec.items.length}`
}

async function packRun(sec: RunSection, event: Event) {
  event.stopPropagation()
  event.preventDefault()
  if (!sec.items.length || isPacking(sec.runId)) return
  const next = new Set(packingRuns.value)
  next.add(sec.runId)
  packingRuns.value = next
  try {
    const { blob, filename } = await api.packRunArtifacts(sec.runId)
    downloadZip(blob, filename)
  } catch {
    toast.error(t('pages.artifactList.packFailed'))
  } finally {
    const done = new Set(packingRuns.value)
    done.delete(sec.runId)
    packingRuns.value = done
  }
}

function onSearchInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  if (props.serverSearch) {
    emit('update:search', value)
    return
  }
  artFilter.value = value
}

function scrollToTop() {
  if (scrollEl.value) scrollEl.value.scrollTop = 0
}

defineExpose({ scrollToTop })

watch(
  () => props.artifacts,
  () => {
    if (props.serverSearch) {
      collapsedRuns.value = new Set()
      return
    }
    artFilter.value = ''
    collapsedRuns.value = new Set()
  },
)

watch(artFilter, (q) => {
  if (props.serverSearch || props.scope !== 'platform') return
  if (!q.trim()) {
    expandAllRuns()
    return
  }
  const next = new Set(collapsedRuns.value)
  for (const sec of props.runSections) {
    const label = runLabel(sec)
    if (sec.items.some((a) => matchArt(a, label))) next.delete(sec.runId)
  }
  collapsedRuns.value = next
})

watch(
  () => props.search,
  (q) => {
    if (!props.serverSearch || !q.trim()) return
    const next = new Set(collapsedRuns.value)
    for (const sec of props.runSections) {
      next.delete(sec.runId)
    }
    collapsedRuns.value = next
  },
)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="shrink-0 border-b border-line px-4 py-2.5">
      <div class="flex flex-wrap items-center justify-between gap-x-2 gap-y-1.5 text-xs font-medium text-txt2">
        <div class="min-w-0 flex-1 basis-[10rem]">
          {{ headerTitle || defaultHeaderTitle() }} · {{ headerCount }}
          <span v-if="matchCountLabel" class="font-normal text-txt3">{{ matchCountLabel }}</span>
          <span class="font-normal text-txt3">{{ headerSubtitle || t('pages.artifactList.subtitle') }}</span>
        </div>
        <div v-if="scope === 'platform' && artifacts.length" class="flex shrink-0 items-center gap-1">
          <button
            type="button"
            class="rounded border border-line bg-base px-1.5 py-0.5 text-[10px] leading-4 text-txt3 transition hover:border-line-strong hover:text-txt2"
            @click="expandAllRuns"
          >
            {{ t('pages.artifactList.expandAll') }}
          </button>
          <button
            type="button"
            class="rounded border border-line bg-base px-1.5 py-0.5 text-[10px] leading-4 text-txt3 transition hover:border-line-strong hover:text-txt2"
            @click="collapseAllRuns"
          >
            {{ t('pages.artifactList.collapseAll') }}
          </button>
        </div>
      </div>
      <div v-if="scope === 'platform'" class="mt-2">
        <input
          :value="serverSearch ? search : artFilter"
          type="search"
          class="art-search w-full rounded-md border border-line bg-base py-1.5 pl-7 pr-2 text-[12px] text-txt outline-none focus:border-accent/55 focus:ring-2 focus:ring-accent/12"
          :placeholder="t('pages.artifactList.artSearchPlaceholder')"
          @input="onSearchInput"
        />
      </div>
    </div>
    <div ref="scrollEl" class="scroll-area min-h-0 flex-1 overflow-y-auto p-2">
      <div v-if="platformListEmpty || (!artifacts.length && scope === 'run')" class="px-2 py-8 text-center text-[12px] text-txt3">
        {{ platformListEmpty || emptyText || defaultEmptyText() }}
      </div>

      <template v-else-if="scope === 'platform' && runSections.length">
        <div v-for="sec in visibleRunSections" :key="sec.runId" class="mb-3 last:mb-0">
          <div class="mb-1 flex w-full items-center gap-1.5 px-1 py-0.5">
            <button
              type="button"
              class="flex min-w-0 flex-1 items-center gap-1.5 px-1 py-1 text-left text-[12px] font-normal text-txt3 transition hover:text-txt2"
              :class="isRunCollapsed(sec.runId) ? 'collapsed' : ''"
              :aria-expanded="!isRunCollapsed(sec.runId)"
              @click="toggleRun(sec.runId)"
            >
              <Icon
                name="chevron-down"
                :size="12"
                class="ui-fold-chevron shrink-0 text-txt3"
                :class="isRunCollapsed(sec.runId) ? '-rotate-90' : ''"
              />
              <span class="min-w-0 flex-1 truncate" :title="runLabel(sec)">{{ runLabel(sec) }}</span>
              <span class="shrink-0 text-[10px] tabular-nums text-txt3">{{ runCountLabel(sec) }}</span>
            </button>
            <button
              type="button"
              class="inline-flex shrink-0 items-center gap-1 rounded-md border border-accent/45 bg-accent-dim px-2 py-0.5 text-[11px] leading-4 text-accent-2 transition hover:border-accent-2 hover:text-txt disabled:cursor-not-allowed disabled:opacity-45"
              :class="isPacking(sec.runId) ? 'opacity-80' : ''"
              data-testid="artifact-run-pack"
              :data-run-id="sec.runId"
              :disabled="!sec.items.length || isPacking(sec.runId)"
              :title="!sec.items.length ? t('pages.artifactList.packEmptyDisabled') : undefined"
              :aria-label="isPacking(sec.runId) ? t('pages.artifactList.packing') : t('pages.artifactList.pack')"
              @click="packRun(sec, $event)"
            >
              <Icon name="download" :size="12" class="shrink-0" />
              <span>{{ isPacking(sec.runId) ? t('pages.artifactList.packing') : t('pages.artifactList.pack') }}</span>
            </button>
          </div>
          <div class="ui-fold" :class="{ 'is-open': !isRunCollapsed(sec.runId) }">
            <div class="ui-fold-inner">
            <button
              v-for="a in sec.items"
              v-show="matchArt(a, runLabel(sec))"
              :key="a.id"
              class="mb-1.5 flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-[background-color,border-color] duration-[var(--dur-ui)] ease-out"
              :class="activeId === a.id ? 'border-accent/50 bg-accent-dim/40' : 'border-line hover:border-line-strong hover:bg-elevated'"
              @click="emit('select', a)"
            >
              <div class="flex h-8 w-8 items-center justify-center rounded-md bg-n-artifact/15 text-n-artifact">
                <Icon :name="kindIcon[a.kind] || 'doc'" :size="16" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="truncate text-[13px] font-medium text-txt" :title="a.name">{{ artifactDisplayName(a) }}</div>
                <div class="truncate text-[10px] text-txt3">
                  <span v-if="artifactFriendlyName(a)">{{ artifactTechnicalDisplayName(a.name) }} · </span>{{ a.nodeId }} · {{ fmtTime(a.createdAt) }}
                </div>
              </div>
              <Icon name="chevron-right" :size="14" class="text-txt3" />
            </button>
            </div>
          </div>
        </div>
      </template>

      <template v-else>
        <button
          v-for="a in plainItems"
          :key="a.id"
          class="mb-1.5 flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-[background-color,border-color] duration-[var(--dur-ui)] ease-out"
          :class="activeId === a.id ? 'border-accent/50 bg-accent-dim/40' : 'border-line hover:border-line-strong hover:bg-elevated'"
          @click="emit('select', a)"
        >
          <div class="flex h-8 w-8 items-center justify-center rounded-md bg-n-artifact/15 text-n-artifact">
            <Icon :name="kindIcon[a.kind] || 'doc'" :size="16" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="truncate text-[13px] font-medium text-txt" :title="a.name">{{ artifactDisplayName(a) }}</div>
            <div class="truncate text-[10px] text-txt3">
              <span v-if="artifactFriendlyName(a)">{{ artifactTechnicalDisplayName(a.name) }} · </span>
              <span v-if="scope === 'platform' && a.workflowName" class="text-accent-2">{{ a.workflowName }}</span>
              <span v-if="scope === 'platform' && a.workflowName"> · </span>{{ a.nodeId }} · {{ fmtTime(a.createdAt) }}
            </div>
          </div>
          <Icon name="chevron-right" :size="14" class="text-txt3" />
        </button>

        <div v-if="feedbackItems.length" class="mb-1.5">
          <button
            type="button"
            class="flex w-full items-center gap-1.5 px-2 py-1 text-left text-[12px] font-normal text-txt3 transition hover:text-txt2"
            data-testid="artifact-feedback-group"
            @click="feedbackOpen = !feedbackOpen"
          >
            <Icon
              name="chevron-down"
              :size="12"
              class="ui-fold-chevron shrink-0 text-txt3"
              :class="feedbackOpen ? '' : '-rotate-90'"
            />
            <span class="min-w-0 flex-1 truncate">{{ t('pages.artifactList.feedbackGroup') }}</span>
            <span class="shrink-0 text-[10px] tabular-nums text-txt3">{{ feedbackItems.length }}</span>
          </button>
          <div class="ui-fold mt-1" :class="{ 'is-open': feedbackOpen }">
            <div class="ui-fold-inner">
            <button
              v-for="a in feedbackItems"
              :key="a.id"
              class="mb-1.5 flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left transition-[background-color,border-color] duration-[var(--dur-ui)] ease-out"
              :class="activeId === a.id ? 'border-accent/50 bg-accent-dim/40' : 'border-line hover:border-line-strong hover:bg-elevated'"
              @click="emit('select', a)"
            >
              <div class="flex h-8 w-8 items-center justify-center rounded-md bg-n-artifact/15 text-n-artifact">
                <Icon name="doc" :size="16" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="truncate text-[13px] font-medium text-txt">{{ a.name }}</div>
                <div class="truncate text-[10px] text-txt3">{{ a.nodeId }} · {{ fmtTime(a.createdAt) }}</div>
              </div>
              <Icon name="chevron-right" :size="14" class="text-txt3" />
            </button>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.art-search {
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='13' height='13' viewBox='0 0 24 24' fill='none' stroke='%236e6e78' stroke-width='2'%3E%3Ccircle cx='11' cy='11' r='8'/%3E%3Cpath d='m21 21-4.35-4.35'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: 9px center;
}
</style>
