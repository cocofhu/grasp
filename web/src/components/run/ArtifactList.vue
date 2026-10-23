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
  <div class="artifact-directory flex h-full min-h-0 min-w-0 flex-col">
    <div class="artifact-list-header">
      <div class="artifact-list-heading">
        <h2 class="min-w-0 truncate text-xs font-medium text-txt2">{{ headerTitle || defaultHeaderTitle() }}</h2>
        <span class="artifact-list-count">{{ headerCount }}</span>
        <span v-if="matchCountLabel" class="text-[11px] text-txt3">{{ matchCountLabel }}</span>
        <div v-if="scope === 'platform' && runSections.length" class="ml-auto flex shrink-0 items-center gap-1">
          <button type="button" class="artifact-list-fold-action" @click="expandAllRuns">{{ t('pages.artifactList.expandAll') }}</button>
          <button type="button" class="artifact-list-fold-action" @click="collapseAllRuns">{{ t('pages.artifactList.collapseAll') }}</button>
        </div>
      </div>
      <p v-if="headerSubtitle" class="mt-1 text-[11px] leading-5 text-txt3">{{ headerSubtitle }}</p>
      <label v-if="scope === 'platform'" class="artifact-search">
        <Icon name="search" :size="14" />
        <input :value="serverSearch ? search : artFilter" type="search" :aria-label="t('pages.artifactList.artSearchPlaceholder')" :placeholder="t('pages.artifactList.artSearchPlaceholder')" @input="onSearchInput" />
      </label>
    </div>
    <div ref="scrollEl" class="scroll-area min-h-0 flex-1 overflow-y-auto px-2 pb-3">
      <div v-if="platformListEmpty || (!artifacts.length && scope === 'run')" class="px-2 py-8 text-center text-[12px] text-txt3">
        {{ platformListEmpty || emptyText || defaultEmptyText() }}
      </div>

      <template v-else-if="scope === 'platform' && runSections.length">
        <div v-for="sec in visibleRunSections" :key="sec.runId" class="artifact-run-section">
          <div class="artifact-run-heading">
            <button
              type="button"
              class="flex min-h-9 min-w-0 flex-1 items-center gap-1.5 px-1 text-left text-[11px] font-medium text-txt3 transition hover:text-txt2"
              :class="isRunCollapsed(sec.runId) ? 'collapsed' : ''"
              :aria-expanded="!isRunCollapsed(sec.runId)"
              @click="toggleRun(sec.runId)"
            >
              <Icon :interactive="false"
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
              class="artifact-run-pack"
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
              type="button"
              class="artifact-file-row"
              data-testid="artifact-list-row"
              :data-artifact-id="a.id"
              :aria-current="activeId === a.id ? 'true' : undefined"
              :class="{ 'is-active': activeId === a.id }"
              @click="emit('select', a)"
            >
              <div class="artifact-file-icon">
                <Icon :name="kindIcon[a.kind] || 'doc'" :size="16" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="artifact-file-name" :title="a.name">{{ artifactDisplayName(a) }}</div>
                <div class="artifact-file-meta">
                  <span v-if="artifactFriendlyName(a)">{{ artifactTechnicalDisplayName(a.name) }} · </span>{{ a.nodeId }} · {{ fmtTime(a.createdAt).slice(5) }}
                </div>
              </div>
            </button>
            </div>
          </div>
        </div>
      </template>

      <template v-else>
        <button
          v-for="a in plainItems"
          :key="a.id"
          type="button"
              class="artifact-file-row"
              data-testid="artifact-list-row"
              :data-artifact-id="a.id"
              :aria-current="activeId === a.id ? 'true' : undefined"
          :class="{ 'is-active': activeId === a.id }"
          @click="emit('select', a)"
        >
          <div class="artifact-file-icon">
            <Icon :name="kindIcon[a.kind] || 'doc'" :size="16" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="artifact-file-name" :title="a.name">{{ artifactDisplayName(a) }}</div>
            <div class="artifact-file-meta">
              <span v-if="artifactFriendlyName(a)">{{ artifactTechnicalDisplayName(a.name) }} · </span>
              <span v-if="scope === 'platform' && a.workflowName" class="text-txt2">{{ a.workflowName }}</span>
              <span v-if="scope === 'platform' && a.workflowName"> · </span>{{ a.nodeId }} · {{ fmtTime(a.createdAt).slice(5) }}
            </div>
          </div>
        </button>

        <div v-if="feedbackItems.length" class="mb-1.5">
          <button
            type="button"
            class="flex w-full items-center gap-1.5 px-2 py-1 text-left text-[12px] font-normal text-txt3 transition hover:text-txt2"
            data-testid="artifact-feedback-group"
            :aria-expanded="feedbackOpen"
            @click="feedbackOpen = !feedbackOpen"
          >
            <Icon :interactive="false"
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
              type="button"
              class="artifact-file-row"
              data-testid="artifact-list-row"
              :data-artifact-id="a.id"
              :aria-current="activeId === a.id ? 'true' : undefined"
              :class="{ 'is-active': activeId === a.id }"
              @click="emit('select', a)"
            >
              <div class="artifact-file-icon">
                <Icon name="doc" :size="16" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="artifact-file-name">{{ a.name }}</div>
                <div class="artifact-file-meta">{{ a.nodeId }} · {{ fmtTime(a.createdAt).slice(5) }}</div>
              </div>
            </button>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.artifact-list-header { flex: none; border-bottom: 1px solid rgb(var(--c-line) / .7); padding: 12px; }
.artifact-list-heading { display: flex; min-width: 0; align-items: center; gap: 7px; }
.artifact-list-count { flex: none; color: rgb(var(--c-txt3)); font-size: 11px; font-variant-numeric: tabular-nums; }
.artifact-list-fold-action { min-height: 28px; border-radius: 5px; padding: 3px 5px; color: rgb(var(--c-txt3)); font-size: 10px; white-space: nowrap; }
.artifact-list-fold-action:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt2)); }
.artifact-search { display: flex; min-width: 0; height: 34px; align-items: center; gap: 7px; margin-top: 10px; border: 1px solid rgb(var(--c-line)); border-radius: 7px; padding: 0 9px; color: rgb(var(--c-txt3)); }
.artifact-search:focus-within { border-color: rgb(var(--c-accent) / .5); }
.artifact-search input { width: 100%; min-width: 0; background: transparent; color: rgb(var(--c-txt)); font-size: 12px; outline: none; }
.artifact-run-section + .artifact-run-section { margin-top: 12px; }
.artifact-run-heading { display: flex; width: 100%; min-height: 38px; align-items: center; gap: 6px; padding: 3px 1px 0; }
.artifact-run-pack { display: inline-flex; min-height: 28px; flex: none; align-items: center; gap: 4px; border-radius: 5px; padding: 3px 6px; color: rgb(var(--c-txt3)); font-size: 10px; line-height: 18px; }
.artifact-run-pack:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt)); }
.artifact-run-pack:disabled { cursor: not-allowed; opacity: .45; }
.artifact-file-row { position: relative; display: flex; width: 100%; min-width: 0; min-height: 54px; align-items: center; gap: 10px; border-bottom: 1px solid rgb(var(--c-line) / .5); padding: 9px 10px; text-align: left; transition: background-color var(--dur-ui); }
.artifact-file-row:hover { background: rgb(var(--c-txt) / .025); }
.artifact-file-row.is-active { background: rgb(var(--c-txt) / .05); }
.artifact-file-row.is-active::before { position: absolute; top: 12px; bottom: 12px; left: 0; width: 2px; background: rgb(var(--c-txt2) / .65); content: ''; }
.artifact-file-row:focus-visible { outline: 2px solid rgb(var(--c-accent) / .55); outline-offset: -2px; }
.artifact-file-icon { display: flex; width: 18px; height: 22px; flex: none; align-items: center; justify-content: center; color: rgb(var(--c-txt3)); }
.artifact-file-name { overflow: hidden; color: rgb(var(--c-txt)); font-size: 12px; font-weight: 500; line-height: 19px; text-overflow: ellipsis; white-space: nowrap; }
.artifact-file-meta { overflow: hidden; margin-top: 2px; color: rgb(var(--c-txt3)); font-size: 10px; line-height: 16px; text-overflow: ellipsis; white-space: nowrap; }
@media (prefers-reduced-motion: reduce) {
  .artifact-file-row { transition: none; }
}
</style>
