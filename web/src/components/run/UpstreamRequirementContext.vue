<script lang="ts">
export const UPSTREAM_REQUIREMENT_ARTIFACT = 'clarified_requirement.json'

/** True when body_template already surfaces clarified_requirement.json (structured key or artifact()). */
export function bodyTemplateShowsRequirement(bodyTemplate?: string | null): boolean {
  const t = (bodyTemplate || '').toString()
  if (!t) return false
  if (/\{\{\s*artifact\(\s*['"]clarified_requirement\.json['"]\s*\)\s*\}\}/.test(t)) return true
  if (/\{\{\s*nodes\.[^.}\s]+\.outputs\.clarified_requirement\s*\}\}/.test(t)) return true
  return false
}
</script>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AppModal from '../ui/AppModal.vue'
import StructuredArtifactView from './StructuredArtifactView.vue'
import { api } from '@/lib/api/api'
import { parseJsonState } from '@/lib/shared/highlightJson'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import type { Artifact } from '@/lib/shared/types'

const props = withDefaults(
  defineProps<{
    artifacts?: Artifact[]
    runId?: string
    /** Live run status; forwarded for structured views that need terminal error gating. */
    runStatus?: string
    /** Current main product name; skip when already reviewing this JSON. */
    productName?: string | null
    /** Gate body_template; used to skip when main preview already is the requirement. */
    bodyTemplate?: string | null
    /**
     * Review-desk opt-in: always-visible bar (title + hint + enlarge), no chevron /
     * inline 40vh. GateApproval omits this and keeps the fold + inline default.
     */
    variant?: 'default' | 'persistent-bar'
    /**
     * Review-desk opt-in: re-provide annotate channel as disabled so AppModal
     * Teleport cannot inherit ↗ from a parent review panel. GateApproval omits this.
     */
    disableAnnotate?: boolean
  }>(),
  {
    variant: 'default',
    disableAnnotate: false,
  },
)

if (props.disableAnnotate) {
  provideReviewAnnotate({
    enabled: false,
    annotate() {},
  })
}

const { t } = useI18n()

const contextArtifact = computed(
  () => props.artifacts?.find((a) => a.name === UPSTREAM_REQUIREMENT_ARTIFACT) ?? null,
)

const visible = computed(
  () =>
    !!contextArtifact.value &&
    props.productName !== UPSTREAM_REQUIREMENT_ARTIFACT &&
    !bodyTemplateShowsRequirement(props.bodyTemplate),
)

const expanded = ref(false)
const modalOpen = ref(false)
const inlineMode = ref<'structured' | 'raw'>('structured')
const modalMode = ref<'structured' | 'raw'>('structured')
const loading = ref(false)
const loaded = ref(false)
const loadErr = ref('')
const rawContent = ref('')
const doc = ref<Record<string, unknown> | null>(null)

const jsonState = computed(() => (rawContent.value ? parseJsonState(rawContent.value) : null))
const enlargeLabel = computed(() => t('pages.gateApproval.upstreamEnlarge'))
const persistentBar = computed(() => props.variant === 'persistent-bar')

async function loadIfNeeded() {
  const wantInline = !persistentBar.value && expanded.value
  if ((!wantInline && !modalOpen.value) || !contextArtifact.value || loaded.value || loading.value) {
    return
  }
  loading.value = true
  loadErr.value = ''
  try {
    const full = await api.artifactContent(contextArtifact.value.id)
    rawContent.value = full.content ?? ''
    try {
      doc.value = JSON.parse(rawContent.value || '{}') as Record<string, unknown>
    } catch {
      doc.value = null
    }
    loaded.value = true
  } catch (e: unknown) {
    loadErr.value = e instanceof Error ? e.message : String(e || '')
    rawContent.value = ''
    doc.value = null
  } finally {
    loading.value = false
  }
}

function retryLoad() {
  loaded.value = false
  loadErr.value = ''
  void loadIfNeeded()
}

function openModal(e?: Event) {
  e?.stopPropagation()
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
}

watch([expanded, modalOpen], () => {
  void loadIfNeeded()
})

watch(
  () => contextArtifact.value?.id,
  () => {
    rawContent.value = ''
    doc.value = null
    loaded.value = false
    expanded.value = false
    modalOpen.value = false
    inlineMode.value = 'structured'
    modalMode.value = 'structured'
    loadErr.value = ''
  },
)

watch(visible, (isVisible) => {
  if (!isVisible) {
    modalOpen.value = false
    expanded.value = false
  }
})
</script>

<template>
  <div
    v-if="visible"
    class="shrink-0 border-t border-line"
    data-testid="upstream-context"
    :data-variant="persistentBar ? 'persistent-bar' : 'default'"
  >
    <div v-if="persistentBar" class="flex w-full items-center gap-2 px-4 py-2.5">
      <div class="min-w-0 flex-1 text-xs text-txt2" data-testid="upstream-bar-hint">
        <span class="font-medium text-txt">{{ t('pages.gateApproval.upstreamContext') }}</span>
        <span class="text-txt3"> · {{ t('pages.gateApproval.upstreamBarHint') }}</span>
      </div>
      <button
        type="button"
        class="enlarge-btn inline-flex shrink-0 items-center gap-1.5 rounded-md bg-accent px-2.5 py-1 text-[11px] font-medium text-white transition hover:bg-accent-hover"
        data-testid="upstream-enlarge"
        :title="enlargeLabel"
        :aria-label="enlargeLabel"
        @click="openModal"
      >
        <Icon name="expand" :size="14" />
        <span class="enlarge-label">{{ enlargeLabel }}</span>
      </button>
    </div>
    <div v-else class="flex w-full items-stretch">
      <button
        type="button"
        class="flex min-w-0 flex-1 items-center gap-2 px-4 py-2.5 text-left text-xs text-txt2 transition hover:bg-elevated/60"
        data-testid="upstream-context-toggle"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <Icon
          name="chevron-right"
          :size="14"
          class="shrink-0 text-txt3 transition-transform duration-150"
          :class="expanded ? 'rotate-90' : ''"
        />
        <span class="shrink-0 font-medium text-txt">{{ t('pages.gateApproval.upstreamContext') }}</span>
        <span class="truncate text-txt3">{{ UPSTREAM_REQUIREMENT_ARTIFACT }}</span>
      </button>
      <button
        type="button"
        class="enlarge-btn mr-2.5 mt-1.5 mb-1.5 inline-flex shrink-0 items-center gap-1.5 px-2.5 text-[11px] font-medium text-txt3 transition hover:border-line hover:bg-elevated hover:text-txt border border-transparent rounded-md"
        data-testid="upstream-enlarge"
        :title="enlargeLabel"
        :aria-label="enlargeLabel"
        @click="openModal"
      >
        <Icon name="expand" :size="14" />
        <span class="enlarge-label">{{ enlargeLabel }}</span>
      </button>
    </div>
    <div
      v-if="!persistentBar && expanded"
      class="upstream-context-body scroll-area border-t border-line px-4 py-3"
      data-testid="upstream-context-body"
    >
      <div class="mb-3 flex flex-wrap gap-1">
        <button
          type="button"
          class="rounded border px-2 py-1 text-[11px] font-medium transition"
          :class="
            inlineMode === 'structured'
              ? 'border-accent/40 bg-accent-dim/40 text-accent-2'
              : 'border-line text-txt3 hover:border-line-strong hover:text-txt2'
          "
          data-testid="upstream-mode-structured"
          @click="inlineMode = 'structured'"
        >
          {{ t('pages.gateApproval.upstreamViewStructured') }}
        </button>
        <button
          type="button"
          class="rounded border px-2 py-1 text-[11px] font-medium transition"
          :class="
            inlineMode === 'raw'
              ? 'border-accent/40 bg-accent-dim/40 text-accent-2'
              : 'border-line text-txt3 hover:border-line-strong hover:text-txt2'
          "
          data-testid="upstream-mode-raw"
          @click="inlineMode = 'raw'"
        >
          {{ t('pages.gateApproval.upstreamViewRaw') }}
        </button>
      </div>
      <div v-if="loading" class="py-4 text-center text-[12px] text-txt3">
        {{ t('pages.gateApproval.loadingArtifact') }}
      </div>
      <div v-else-if="loadErr" class="py-4 text-center text-[12px] text-err">
        {{ t('pages.gateApproval.upstreamLoadFailed', { error: loadErr }) }}
        <button
          type="button"
          class="ml-2 text-accent-2 underline underline-offset-2 hover:text-txt"
          data-testid="upstream-retry"
          @click="retryLoad"
        >
          {{ t('pages.gateApproval.upstreamRetry') }}
        </button>
      </div>
      <StructuredArtifactView
        v-else-if="inlineMode === 'structured' && doc"
        :name="UPSTREAM_REQUIREMENT_ARTIFACT"
        :doc="doc"
        :artifacts="artifacts"
        :run-id="runId"
        :run-status="runStatus"
      />
      <div v-else-if="inlineMode === 'raw' && jsonState" class="json-code-view scroll-area">
        <div v-if="!jsonState.ok" class="fallback-tag">{{ t('pages.artifactPreview.fallbackPlainText') }}</div>
        <pre v-html="jsonState.html" />
      </div>
      <div v-else class="py-4 text-center text-[12px] text-txt3">
        {{ t('pages.artifactPreview.contentEmpty') }}
      </div>
    </div>

    <AppModal
      :open="modalOpen"
      :title="t('pages.gateApproval.upstreamContext')"
      :width="960"
      :close-on-backdrop="false"
      data-testid="upstream-enlarge-modal"
      @close="closeModal"
    >
      <div class="space-y-3" data-testid="upstream-modal-body">
        <div class="flex flex-wrap gap-1">
          <button
            type="button"
            class="rounded border px-2 py-1 text-[11px] font-medium transition"
            :class="
              modalMode === 'structured'
                ? 'border-accent/40 bg-accent-dim/40 text-accent-2'
                : 'border-line text-txt3 hover:border-line-strong hover:text-txt2'
            "
            data-testid="upstream-modal-mode-structured"
            @click="modalMode = 'structured'"
          >
            {{ t('pages.gateApproval.upstreamViewStructured') }}
          </button>
          <button
            type="button"
            class="rounded border px-2 py-1 text-[11px] font-medium transition"
            :class="
              modalMode === 'raw'
                ? 'border-accent/40 bg-accent-dim/40 text-accent-2'
                : 'border-line text-txt3 hover:border-line-strong hover:text-txt2'
            "
            data-testid="upstream-modal-mode-raw"
            @click="modalMode = 'raw'"
          >
            {{ t('pages.gateApproval.upstreamViewRaw') }}
          </button>
        </div>
        <div v-if="loading" class="py-8 text-center text-[12px] text-txt3">
          {{ t('pages.gateApproval.loadingArtifact') }}
        </div>
        <div v-else-if="loadErr" class="py-8 text-center text-[12px] text-err">
          {{ t('pages.gateApproval.upstreamLoadFailed', { error: loadErr }) }}
          <button
            type="button"
            class="ml-2 text-accent-2 underline underline-offset-2 hover:text-txt"
            data-testid="upstream-modal-retry"
            @click="retryLoad"
          >
            {{ t('pages.gateApproval.upstreamRetry') }}
          </button>
        </div>
        <StructuredArtifactView
          v-else-if="modalMode === 'structured' && doc"
          :name="UPSTREAM_REQUIREMENT_ARTIFACT"
          :doc="doc"
          :artifacts="artifacts"
          :run-id="runId"
          :run-status="runStatus"
        />
        <div v-else-if="modalMode === 'raw' && jsonState" class="json-code-view json-code-view--modal scroll-area">
          <div v-if="!jsonState.ok" class="fallback-tag">{{ t('pages.artifactPreview.fallbackPlainText') }}</div>
          <pre v-html="jsonState.html" />
        </div>
        <div v-else class="py-8 text-center text-[12px] text-txt3">
          {{ t('pages.artifactPreview.contentEmpty') }}
        </div>
      </div>
      <template #footer>
        <span class="mr-auto text-[11px] text-txt3" data-testid="upstream-modal-readonly-footer">
          {{ t('pages.gateApproval.upstreamReadonlyFooter') }}
        </span>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.upstream-context-body {
  max-height: min(40vh, 28rem);
  overflow-y: auto;
}
.json-code-view {
  padding: 12px 14px;
  overflow: auto;
  max-height: 100%;
}
.json-code-view--modal {
  max-height: none;
}
.fallback-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  padding: 3px 8px;
  font-size: 11px;
  color: rgb(var(--c-warn, 251 191 36));
  border: 1px solid rgba(251, 191, 36, 0.35);
  background: rgba(251, 191, 36, 0.08);
}

@media (max-width: 640px) {
  .enlarge-label {
    display: none;
  }
}
</style>
