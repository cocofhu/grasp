<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { api } from '@/lib/api/api'
import { nodeColor } from '@/data/nodeRegistry'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import StructuredArtifactView from './StructuredArtifactView.vue'
import HtmlPreview from '../ui/HtmlPreview.vue'
import SelectionAddToChat from './SelectionAddToChat.vue'
import UpstreamRequirementContext from './UpstreamRequirementContext.vue'
import { ARTIFACT_TO_OUTPUT_JSON } from '@/lib/run/structuredArtifacts'
import { productArtifactName, productArtifactsForNode, resolveStructuredProductArtifact } from '@/lib/run/productNodeArtifacts'
import { artifactFriendlyNameKey } from '@/lib/run/reactArtifactPreview'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import { addClarifyAnnotation } from '@/lib/inbox/useClarifyDraft'
import { useToast } from '@/lib/composables/useToast'
import type { NodeRun, WFNode, Run, ReactAnnotation } from '@/lib/shared/types'

// annotatable enables the per-field "⤴ 标注" affordance in the review phase:
// each staged reference is pushed onto the node's review composer draft.
const props = withDefaults(defineProps<{ node: WFNode; nodeRun: NodeRun; run: Run; annotatable?: boolean }>(), {
  annotatable: false,
})
const emit = defineEmits<{
  'update:historicalPreview': [historical: boolean]
}>()

const { t } = useI18n()
const toast = useToast()
const stageEl = ref<HTMLElement | null>(null)

function stageAnnotation(ann: ReactAnnotation) {
  if (!props.annotatable || isHistoricalPreview.value) return
  const result = addClarifyAnnotation(props.run.id, props.node.id, ann)
  if (result === 'duplicate') toast.warn(t('pages.reviewComposer.alreadyAdded'))
}

// Provide the annotate channel to descendant product cards. It is inert unless
// annotatable is set, so read-only product views never show pick affordances.
// Use a getter so `enabled` tracks the prop if the panel is reused.
provideReviewAnnotate({
  get enabled() {
    return !!props.annotatable && !isHistoricalPreview.value
  },
  annotate: (ann) => {
    stageAnnotation(ann)
  },
})

/** HTML toolbar pick → same annotations store as structured ⤴ chips (g3). */
function onHtmlPick(payload: { selector: string; tagName: string; imageDataUrl: string }) {
  if (!props.annotatable || isHistoricalPreview.value) return
  stageAnnotation({
    selector: payload.selector,
    label: payload.selector || payload.tagName,
  })
}

function onQuoteAdd(ann: ReactAnnotation) {
  stageAnnotation(ann)
}

const { NODE_DEFS } = useNodeDefs()

function jsonSnapshot(name: string): string {
  const jsonKey = ARTIFACT_TO_OUTPUT_JSON[name]
  if (!jsonKey) return ''
  const snap = props.nodeRun.outputs?.[jsonKey]
  return typeof snap === 'string' ? snap.trim() : ''
}

// More than one execution of this node: a missing snapshot must not be filled
// from the live store, which keeps only the latest same-named file.
const multiExecution = computed(
  () => (props.run.nodeExecutions?.[props.node.id]?.length ?? 0) > 1,
)

function legacyProductTabs() {
  const listed = productArtifactsForNode(props.node)
  if (listed.length <= 1) return listed
  // Only this node's writes count — same-named upstream leftovers must not
  // surface as optional tabs of a multi-product Agent.
  const ownedNames = new Set(
    (props.run.artifacts || []).filter((a) => a.nodeId === props.node.id).map((a) => a.name),
  )
  const outs = props.nodeRun.outputs || {}
  return listed.filter((a) => {
    if (a.required) return true
    if (ownedNames.has(a.name)) return true
    if (a.outputKey) {
      const snap = outs[a.outputKey] ?? outs[`${a.outputKey}_json`]
      if (typeof snap === 'string' && snap.trim()) return true
    }
    return false
  })
}

const productTabs = computed(() => {
  const listed = productArtifactsForNode(props.node)
  const legacy = legacyProductTabs()
  if (!multiExecution.value) return legacy
  const legacyNames = new Set(legacy.map((a) => a.name))
  return listed.filter((a) => {
    // page.html keeps its own per-iteration HTML snapshot path.
    if (a.name === 'page.html') return legacyNames.has(a.name)
    return jsonSnapshot(a.name).length > 0
  })
})
const selectedArtifactName = ref('')
watch(
  () => productTabs.value.map((a) => a.name).join(','),
  () => {
    if (!productTabs.value.some((a) => a.name === selectedArtifactName.value)) {
      selectedArtifactName.value = productTabs.value[0]?.name || ''
    }
  },
  { immediate: true },
)

const spec = computed(() => {
  const tabs = productTabs.value
  const selected = tabs.some((a) => a.name === selectedArtifactName.value)
    ? selectedArtifactName.value
    : tabs[0]?.name || ''
  if (selected) return { name: selected }
  if (multiExecution.value) return undefined
  const name = productArtifactName(props.node)
  return name ? { name } : undefined
})
const hex = computed(() => nodeColor(props.node.type))
const hexSoft = computed(() => nodeColor(props.node.type, 0.13))
const def = computed(() => NODE_DEFS.value[props.node.type])

const artifact = computed(() => {
  if (!spec.value) return null
  const name = spec.value.name
  const jsonKey = ARTIFACT_TO_OUTPUT_JSON[name]
  const snap = jsonKey ? props.nodeRun.outputs?.[jsonKey] : undefined
  return resolveStructuredProductArtifact({
    name,
    nodeId: props.node.id,
    node: props.node,
    nodeStatus: props.nodeRun.status,
    hasSnapshot: typeof snap === 'string' && snap.trim().length > 0,
    artifacts: props.run.artifacts || [],
  })
})

const doc = ref<any>(null)
const rawHtml = ref('')
const isVisual = computed(() => spec.value?.name === 'page.html')

function artifactTabLabel(name: string): string {
  const key = artifactFriendlyNameKey(name)
  return key ? t(key) : name
}
const loading = ref(false)
const eligibleVersions = computed(() =>
  (props.run.nodeExecutions?.[props.node.id] || [])
    .filter(
      (nodeRun) =>
        (nodeRun.status === 'completed' || nodeRun.status === 'waiting_human') &&
        typeof nodeRun.outputs?.page === 'string' &&
        nodeRun.outputs.page.trim().length > 0,
    )
    .sort((a, b) => (a.iteration ?? 0) - (b.iteration ?? 0)),
)
const latestEligibleIteration = computed(
  () => eligibleVersions.value[eligibleVersions.value.length - 1]?.iteration ?? null,
)
const selectedIteration = ref<number | null>(null)
const followingLatest = ref(true)
const selectedVersion = computed(
  () => eligibleVersions.value.find((nodeRun) => nodeRun.iteration === selectedIteration.value) || null,
)
const isHistoricalPreview = computed(
  () =>
    isVisual.value &&
    selectedIteration.value !== null &&
    latestEligibleIteration.value !== null &&
    selectedIteration.value !== latestEligibleIteration.value,
)

watch(
  () => eligibleVersions.value.map((nodeRun) => nodeRun.iteration).join(','),
  () => {
    if (
      selectedIteration.value === null ||
      !eligibleVersions.value.some((nodeRun) => nodeRun.iteration === selectedIteration.value)
    ) {
      selectedIteration.value = latestEligibleIteration.value
      followingLatest.value = true
    } else if (followingLatest.value) {
      selectedIteration.value = latestEligibleIteration.value
    }
  },
  { immediate: true },
)
function onVersionChange() {
  followingLatest.value = selectedIteration.value === latestEligibleIteration.value
}
watch(
  isHistoricalPreview,
  (historical) => emit('update:historicalPreview', historical),
  { immediate: true },
)
function parseDoc(raw: string) {
  try {
    doc.value = JSON.parse(raw || '{}')
  } catch {
    doc.value = null
  }
}

/** In-flight / review pause: follow live page.html. Historical completed keep snap.
 *  annotatable only controls pick affordances — completed + annotatable must not
 *  bypass frozen snap (Clarify/Inbox shells always pass annotatable). */
const followLive = computed(
  () =>
    props.nodeRun.status === 'waiting_human' ||
    props.nodeRun.status === 'running' ||
    props.nodeRun.status === 'pending',
)
const previewNodeRun = computed(() => {
  if (!isVisual.value || !selectedVersion.value) return props.nodeRun
  // The run-detail execution timeline may supply an older nodeRun. The picker
  // remains independent of that selection and therefore still defaults to the
  // newest eligible page. An active execution is the sole case that keeps the
  // live nodeRun as the preview source.
  if (
    isHistoricalPreview.value ||
    (!followLive.value && props.nodeRun.iteration !== selectedVersion.value.iteration)
  ) {
    return selectedVersion.value
  }
  return props.nodeRun
})

async function load() {
  const name = spec.value?.name
  if (!name) {
    doc.value = null
    rawHtml.value = ''
    return
  }
  // The visual node's product is raw HTML, not JSON. Prefer THIS execution's own
  // HTML snapshot (nodeRun.outputs.page): the run artifact store replaces the
  // same-named page.html on every iteration, so reading it would show the latest
  // page for every historical "第 N 次" tab. Fall back to the store only when the
  // snapshot is absent (older runs predating the snapshot).
  // Exception: review / in-flight (followLive) reads the live store first so
  // interactive write_artifact updates show without a full-node re-run.
  if (isVisual.value) {
    const priorHtml = rawHtml.value
    doc.value = null
    const snap = previewNodeRun.value.outputs?.page
    const a = artifact.value
    if (!isHistoricalPreview.value && followLive.value && a) {
      loading.value = true
      try {
        const full = await api.artifactContent(a.id)
        rawHtml.value = full.content || (typeof snap === 'string' ? snap : '') || priorHtml || ''
      } catch {
        // Keep prior success / snap on fetch failure — never blank a good preview.
        rawHtml.value =
          (typeof snap === 'string' && snap.trim() ? snap : priorHtml) || ''
      } finally {
        loading.value = false
      }
      return
    }
    rawHtml.value = ''
    if (typeof snap === 'string' && snap.trim()) {
      rawHtml.value = snap
      return
    }
    if (!a) return
    loading.value = true
    try {
      const full = await api.artifactContent(a.id)
      rawHtml.value = full.content || ''
    } catch {
      rawHtml.value = ''
    } finally {
      loading.value = false
    }
    return
  }
  // Each execution persists its own JSON in nodeRun.outputs; the run artifact
  // store replaces same-named files, so a visit with no snapshot must not
  // read it once this node has executed more than once. A single execution
  // that predates snapshots may still fall back to the live file.
  const jsonKey = ARTIFACT_TO_OUTPUT_JSON[name]
  const snap = jsonKey ? props.nodeRun.outputs?.[jsonKey] : undefined
  if (typeof snap === 'string' && snap.trim()) {
    parseDoc(snap)
    return
  }
  if (multiExecution.value) {
    doc.value = null
    return
  }
  const a = artifact.value
  if (!a) {
    doc.value = null
    return
  }
  loading.value = true
  try {
    // List DTO omits artifact content; fetch the full record by id. Re-fetched
    // when id/size changes (e.g. a plan node's plan.json growing mid-run).
    const full = await api.artifactContent(a.id)
    parseDoc(full.content || '{}')
  } catch {
    doc.value = null
  } finally {
    loading.value = false
  }
}
watch(
  () => {
    const name = spec.value?.name || ''
    const jsonKey = name ? ARTIFACT_TO_OUTPUT_JSON[name] : ''
    const snap = jsonKey ? props.nodeRun.outputs?.[jsonKey] : ''
    // The visual node keeps its per-iteration HTML under outputs.page; include a
    // fingerprint so switching execution tabs reloads the right page.
    const page = isVisual.value ? previewNodeRun.value.outputs?.page : ''
    const pageKey = typeof page === 'string' ? `${page.length}` : ''
    const a = artifact.value
    // followLive also keys on updatedAt so interactive writes reload even when
    // sizeBytes is unchanged (rare) or snap has not yet been synced.
    const liveKey = !isHistoricalPreview.value && followLive.value
      ? `${a?.sizeBytes ?? ''}:${a?.updatedAt ?? ''}:${props.annotatable ? 1 : 0}:${props.nodeRun.status || ''}`
      : ''
    return `${previewNodeRun.value.iteration ?? 0}:${selectedIteration.value ?? ''}:${isHistoricalPreview.value ? 1 : 0}:${jsonKey}:${snap}:${pageKey}:${a ? `${a.id}:${a.sizeBytes}` : ''}:${liveKey}`
  },
  load,
  { immediate: true },
)

const pending = computed(() => props.nodeRun.status === 'pending')
</script>

<template>
  <div ref="stageEl" class="flex h-full min-h-0 flex-col" data-review-annotate-stage>
    <div
      class="flex shrink-0 items-center gap-2.5 px-4 pt-4 pb-3"
      data-testid="structured-product-header"
    >
      <div class="flex h-8 w-8 items-center justify-center rounded-md" :style="{ background: hexSoft, color: hex }">
        <Icon :name="def.icon" :size="16" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <div class="text-sm font-semibold text-txt">{{ def.label }}{{ t('pages.structuredProduct.titleSuffix') }}</div>
          <select
            v-if="isVisual && eligibleVersions.length >= 2"
            v-model="selectedIteration"
            @change="onVersionChange"
            class="rounded-md border border-line bg-elevated px-2 py-1 text-xs text-txt outline-none focus:border-accent"
            data-testid="structured-product-version-select"
            aria-label="选择视觉产物版本"
          >
            <option v-for="version in eligibleVersions" :key="version.iteration" :value="version.iteration">
              第 {{ version.iteration }} 次{{ version.iteration === latestEligibleIteration ? ' · 最新' : '' }}
            </option>
          </select>
        </div>
        <div
          v-if="productTabs.length > 1"
          class="mt-1.5 flex flex-wrap gap-1"
          data-testid="structured-product-tabs"
        >
          <button
            v-for="tab in productTabs"
            :key="tab.name"
            type="button"
            class="rounded-md border px-2 py-0.5 text-[11px]"
            :class="
              selectedArtifactName === tab.name
                ? 'border-accent bg-accent/10 text-accent-2'
                : 'border-line bg-elevated text-txt3'
            "
            :data-testid="`structured-product-tab-${tab.name}`"
            @click="selectedArtifactName = tab.name"
          >
            {{ artifactTabLabel(tab.name) }}
          </button>
        </div>
        <div class="text-[11px] text-txt3" data-testid="structured-product-name">{{ spec?.name }}</div>
      </div>
    </div>

    <div
      class="min-h-0 flex-1"
      :class="isVisual && rawHtml ? 'flex flex-col overflow-hidden px-4' : 'overflow-y-auto px-4 pb-4'"
      data-testid="structured-product-preview"
    >
      <div v-if="isVisual && rawHtml" class="rounded-lg relative min-h-0 flex-1 border border-line">
        <div
          v-if="isHistoricalPreview"
          class="rounded-md absolute left-2 top-2 z-10 border border-warn/50 bg-base/95 px-2 py-1 text-xs text-warn"
          data-testid="structured-product-historical-banner"
        >
          历史版本 · 只读
        </div>
        <HtmlPreview
          :html="rawHtml"
          :inspectable="annotatable && !isHistoricalPreview"
          fill-parent
          @pick="onHtmlPick"
        />
      </div>

      <StructuredArtifactView
        v-else-if="doc && spec"
        :name="spec.name"
        :doc="doc"
        :accent="hex"
        :run-id="run.id"
        :artifacts="run.artifacts"
        :run-status="run.status"
      />

      <div v-else class="flex h-[60%] items-center justify-center text-center text-[12px] text-txt3">
        <div>
          <Icon name="artifact" :size="26" class="mx-auto mb-2 opacity-40" />
          <p v-if="loading">{{ t('pages.structuredProduct.loading') }}</p>
          <p v-else-if="pending">{{ t('pages.structuredProduct.pending') }}</p>
          <p v-else-if="isVisual">{{ t('pages.structuredProduct.noPage') }}</p>
          <p v-else>{{ t('pages.structuredProduct.noStructured') }}</p>
        </div>
      </div>

      <SelectionAddToChat
        v-if="annotatable && !isVisual"
        :enabled="annotatable && !isHistoricalPreview"
        :root="stageEl"
        @add="onQuoteAdd"
      />
    </div>

    <UpstreamRequirementContext
      variant="persistent-bar"
      disable-annotate
      :artifacts="run.artifacts"
      :run-id="run.id"
      :run-status="run.status"
      :product-name="spec?.name"
    />
  </div>
</template>
