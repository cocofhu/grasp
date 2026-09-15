<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import HtmlPreview from '../ui/HtmlPreview.vue'
import StructuredArtifactView, { isStructuredArtifactName } from './StructuredArtifactView.vue'
import Icon from '../ui/Icon.vue'
import { api } from '@/lib/api/api'
import { isReadonlyArtifactKind, type GatePrimaryProductRef } from '@/lib/inbox/gateUpstream'
import type { Artifact } from '@/lib/shared/types'

const props = withDefaults(
  defineProps<{
    runId: string
    gateNodeId: string
    products: GatePrimaryProductRef[]
    /** Saved content keyed by artifact name (preview source of truth). */
    savedContent: Record<string, string>
    /** ETag / updatedAt fingerprint per artifact for external-change detection. */
    savedMeta: Record<string, { etag?: string; updatedAt?: string; sizeBytes?: number }>
    artifacts?: Artifact[]
    /** Live run status for test_result screenshot error gating. */
    runStatus?: string
    /** When false, hide save (no permission / resolved). */
    canEdit: boolean
    /** produces-only names (not in body_template whitelist). */
    excludedNames?: string[]
    /** Content-fit HtmlPreview (GateApproval fillPreview shell). */
    fitContent?: boolean
    maxContentHeightVh?: number
    contentHeightOffsetPx?: number
    /**
     * Fill parent height (Run-detail mobile + desktop Inbox/Run-detail visual).
     * Disables content-height sizing; iframe/editor scrolls inside a flex-filled
     * shell (desktop shell also caps at ≈60vh via GateApproval).
     */
    fillParent?: boolean
    /** Parent product load in progress (first load or retry). */
    contentLoading?: boolean
    /** Parent product load failure message; null/empty when ok. */
    loadError?: string | null
    /**
     * Enable HtmlPreview DOM inspect (pick → selector + element screenshot).
     * Used by page.html human_gate Issue feedback; parent forwards `pick`.
     */
    inspectable?: boolean
    /** CommentPin badges for visual gate annotate MVP. */
    commentPins?: {
      id: string
      seq: number
      bounds?: { left: number; top: number; width: number; height: number }
      active?: boolean
    }[]
    annotateDraft?: {
      selector: string
      imageDataUrl?: string
      screenshotMissing?: boolean
      initialComment?: string
      bounds?: { left: number; top: number; width: number; height: number } | null
      style?: {
        color?: string
        fontSize?: string
        fontWeight?: string
        fontFamily?: string
        lineHeight?: string
      } | null
    } | null
    /**
     * Desktop: allow HTML main-product enlarge modal (inline/fillParent included).
     * Mobile approval keeps false so enlarge stays desktop-only.
     */
    enlargeable?: boolean
  }>(),
  {
    fitContent: false,
    maxContentHeightVh: 60,
    contentHeightOffsetPx: 0,
    fillParent: false,
    excludedNames: () => [],
    contentLoading: false,
    loadError: null,
    inspectable: false,
    commentPins: () => [],
    annotateDraft: null,
    enlargeable: true,
  },
)

const emit = defineEmits<{
  (e: 'saved', payload: { name: string; content: string; etag: string; updatedAt: string; sizeBytes: number }): void
  (e: 'dirty-change', dirty: boolean): void
  (e: 'refresh-request', name: string): void
  (e: 'retry-load'): void
  (
    e: 'pick',
    payload: {
      selector: string
      tagName: string
      imageDataUrl: string
      bounds?: { left: number; top: number; width: number; height: number }
      currentText?: string
      style?: {
        color?: string
        fontSize?: string
        fontWeight?: string
        fontFamily?: string
        lineHeight?: string
      }
    },
  ): void
  (e: 'pin-select', pinId: string): void
  (e: 'annotate-save', comment: string): void
  (e: 'annotate-send-chat', comment: string): void
  (e: 'annotate-close'): void
}>()

const { t } = useI18n()

/** File-name trigger for proposals.json form-mode stopgap (exact match only). */
function isProposalsArtifact(name?: string | null): boolean {
  return name === 'proposals.json'
}

const activeName = ref('')
const mode = ref<'edit' | 'preview'>('preview')
const structMode = ref<'form' | 'json'>('form')
const draft = ref('')
const saving = ref(false)
const saveError = ref<string | null>(null)
const saveOk = ref(false)
const externalChange = ref(false)
const imageSrc = ref<string | null>(null)
const imageLoading = ref(false)
const imageError = ref(false)
let imageBlobUrl: string | null = null
let imageLoadGen = 0

const activeProduct = computed(
  () => props.products.find((p) => p.name === activeName.value) || props.products[0] || null,
)

const isProposalsProduct = computed(() => isProposalsArtifact(activeProduct.value?.name))

const isReadonlyProduct = computed(() => {
  const p = activeProduct.value
  if (!p) return false
  return !!p.readonly || isReadonlyArtifactKind(p.kind)
})

/** Image-only lazy-load pane; other readonly kinds stay non-editable without image UI. */
const isReadonlyImage = computed(
  () => isReadonlyProduct.value && isReadonlyArtifactKind(activeProduct.value?.kind),
)

const canEditActive = computed(() => props.canEdit && !isReadonlyProduct.value)

const savedText = computed(() => {
  const name = activeProduct.value?.name
  return name ? props.savedContent[name] ?? '' : ''
})

/** HTML preview panel state: loading | empty | error | ready. */
const htmlPreviewState = computed<'loading' | 'empty' | 'error' | 'ready'>(() => {
  if (props.contentLoading) return 'loading'
  if (props.loadError) return 'error'
  if (!savedText.value.trim()) return 'empty'
  return 'ready'
})

const editTabDisabled = computed(
  () => !canEditActive.value || props.contentLoading,
)

function setMode(next: 'edit' | 'preview') {
  if (next === 'edit' && editTabDisabled.value) return
  mode.value = next
}

watch(
  () => props.contentLoading,
  (loading) => {
    // Stay on preview while loading / retrying so edit cannot be entered.
    if (loading && mode.value === 'edit') mode.value = 'preview'
  },
)

const isDirty = computed(() => {
  if (!canEditActive.value || !activeProduct.value) return false
  return draft.value !== savedText.value
})

const anyDirty = computed(() => {
  if (!canEditActive.value) return false
  // Only track the active draft vs saved; multi-tab dirty is per-active switch flush.
  return isDirty.value
})

function revokeImageBlob() {
  if (imageBlobUrl) {
    URL.revokeObjectURL(imageBlobUrl)
    imageBlobUrl = null
  }
}

async function loadReadonlyImage(name: string) {
  imageLoadGen++
  const gen = imageLoadGen
  revokeImageBlob()
  imageSrc.value = null
  imageError.value = false
  const art = (props.artifacts || []).find((a) => a.name === name)
  if (!art?.id) {
    imageError.value = true
    return
  }
  imageLoading.value = true
  try {
    const res = await fetch(api.artifactDownloadUrl(art.id), { credentials: 'include' })
    if (!res.ok) throw new Error(String(res.status))
    const blob = await res.blob()
    if (gen !== imageLoadGen) {
      // Stale response: drop blob; do not createObjectURL just to revoke.
      return
    }
    const url = URL.createObjectURL(blob)
    imageBlobUrl = url
    imageSrc.value = url
  } catch {
    if (gen === imageLoadGen) imageError.value = true
  } finally {
    if (gen === imageLoadGen) imageLoading.value = false
  }
}

onBeforeUnmount(() => {
  imageLoadGen++
  revokeImageBlob()
})

watch(
  () => props.products.map((p) => p.name).join('|'),
  () => {
    if (!activeName.value || !props.products.some((p) => p.name === activeName.value)) {
      activeName.value = props.products[0]?.name || ''
    }
  },
  { immediate: true },
)

/** Stable identity for softRefresh: ignore array ref / updatedAt noise. */
function artifactsIdentityFingerprint(arts: Artifact[] | undefined): string {
  return (arts || [])
    .map((a) => `${a.name}:${a.id}:${a.etag ?? ''}:${a.sizeBytes ?? 0}`)
    .join('|')
}

watch(
  // Join to a string so array-ref churn from softRefresh does not re-fire when
  // the identity fingerprint is unchanged (Object.is on a fresh tuple always differs).
  () =>
    [
      activeName.value,
      activeProduct.value?.kind ?? '',
      String(activeProduct.value?.readonly ?? ''),
      artifactsIdentityFingerprint(props.artifacts),
    ].join('\0'),
  () => {
    draft.value = savedText.value
    saveError.value = null
    saveOk.value = false
    externalChange.value = false
    // Default to preview on product switch so approval landing matches read-only
    // structured/HTML views; user switches to Edit explicitly.
    mode.value = 'preview'
    // Reset struct mode by artifact name so proposals.json defaults to raw JSON
    // and other structured products keep the existing form default (no cross-product residue).
    structMode.value = isProposalsArtifact(activeProduct.value?.name) ? 'json' : 'form'
    if (isReadonlyImage.value && activeProduct.value) {
      void loadReadonlyImage(activeProduct.value.name)
    } else {
      imageLoadGen++
      revokeImageBlob()
      imageSrc.value = null
      imageError.value = false
      imageLoading.value = false
    }
  },
  { immediate: true },
)

watch(
  () => savedText.value,
  (next, prev) => {
    if (prev === undefined || next === prev) return
    // External refresh while editing a dirty draft: keep edit mode and banner.
    if (mode.value === 'edit' && isDirty.value && draft.value !== next) {
      externalChange.value = true
      return
    }
    if (!isDirty.value || draft.value === '') {
      draft.value = next
    }
    saveError.value = null
    saveOk.value = false
  },
)

watch(anyDirty, (d) => emit('dirty-change', d), { immediate: true })

const previewDoc = computed(() => {
  const name = activeProduct.value?.name
  if (!name || !isStructuredArtifactName(name)) return null
  try {
    return JSON.parse(savedText.value || '{}')
  } catch {
    return null
  }
})

const formTitle = ref('')
const formSummary = ref('')

watch(
  () => [mode.value, structMode.value, draft.value, activeProduct.value?.name] as const,
  () => {
    const name = activeProduct.value?.name
    if (!name || !isStructuredArtifactName(name) || structMode.value !== 'form') return
    // proposals.json has no top-level title/summary — keep controls empty & unused.
    if (isProposalsArtifact(name)) {
      formTitle.value = ''
      formSummary.value = ''
      return
    }
    try {
      const doc = JSON.parse(draft.value || '{}') as { title?: string; summary?: string }
      formTitle.value = typeof doc.title === 'string' ? doc.title : ''
      formSummary.value = typeof doc.summary === 'string' ? doc.summary : ''
    } catch {
      /* keep */
    }
  },
  { immediate: true },
)

function applyFormToDraft() {
  const name = activeProduct.value?.name
  if (!name || !isStructuredArtifactName(name)) return
  // Never merge form title/summary into proposals.json draft (schema is context + proposals[]).
  if (isProposalsArtifact(name)) return
  try {
    const doc = JSON.parse(draft.value || '{}') as Record<string, unknown>
    if ('title' in doc || formTitle.value) doc.title = formTitle.value
    if ('summary' in doc || formSummary.value) doc.summary = formSummary.value
    draft.value = JSON.stringify(doc, null, 2)
  } catch {
    /* ignore */
  }
}

function switchToStructJson() {
  // Skip form→draft merge for proposals.json; other products keep existing merge.
  applyFormToDraft()
  structMode.value = 'json'
}

function selectProduct(name: string) {
  if (name === activeName.value) return
  if (isDirty.value && !window.confirm(t('pages.gateApproval.discardSwitchConfirm'))) {
    return
  }
  draft.value = props.savedContent[name] ?? ''
  activeName.value = name
}

async function save() {
  if (!canEditActive.value || !activeProduct.value || saving.value) return
  saveError.value = null
  saveOk.value = false
  // Hard-block form-mode save for proposals.json before any API / draft merge.
  if (isProposalsArtifact(activeProduct.value.name) && structMode.value === 'form') {
    saveError.value = t('pages.gateApproval.proposalsFormSaveBlocked')
    return
  }
  if (structMode.value === 'form' && isStructuredArtifactName(activeProduct.value.name)) {
    applyFormToDraft()
  }
  saving.value = true
  try {
    const meta = props.savedMeta[activeProduct.value.name]
    const res = await api.saveGateArtifact(
      props.runId,
      props.gateNodeId,
      activeProduct.value.name,
      draft.value,
      meta?.etag,
    )
    const saved = typeof res.content === 'string' ? res.content : draft.value
    draft.value = saved
    emit('saved', {
      name: activeProduct.value.name,
      content: saved,
      etag: res.etag,
      updatedAt: res.updatedAt,
      sizeBytes: res.sizeBytes,
    })
    saveOk.value = true
    externalChange.value = false
    mode.value = 'preview'
  } catch (e: any) {
    const msg = e?.message || String(e)
    if (msg.includes('externally') || msg.includes('refresh') || msg.includes('409')) {
      externalChange.value = true
    }
    saveError.value = msg
  } finally {
    saving.value = false
  }
}

function onRefreshExternal() {
  externalChange.value = false
  if (activeProduct.value) emit('refresh-request', activeProduct.value.name)
}

defineExpose({
  isDirty: anyDirty,
  /** Discard active draft back to last saved. */
  discard() {
    draft.value = savedText.value
    saveError.value = null
  },
})
</script>

<template>
  <div
    v-if="products.length"
    class="flex min-h-0 flex-col"
    :class="fillParent ? 'h-full' : ''"
    data-testid="gate-product-editor"
  >
    <div
      v-if="products.length > 1"
      class="flex flex-wrap gap-0 border-b border-line px-1"
      role="tablist"
    >
      <button
        v-for="p in products"
        :key="p.name"
        type="button"
        role="tab"
        class="relative -mb-px border-b-2 px-3 py-2 text-xs transition"
        :class="
          p.name === activeName
            ? 'border-accent-2 text-txt'
            : 'border-transparent text-txt2 hover:text-txt'
        "
        @click="selectProduct(p.name)"
      >
        {{ p.name }}
        <span
          v-if="p.readonly || isReadonlyArtifactKind(p.kind)"
          class="rounded-md ml-1.5 border border-line-strong bg-overlay px-1 py-px text-[10px] text-txt3"
          data-testid="gate-readonly-badge"
          >{{ t('pages.gateApproval.readonlyBadge') }}</span
        >
        <span
          v-else
          class="rounded-md ml-1.5 border border-accent/35 bg-accent-dim px-1 py-px text-[10px] text-accent-2"
          >{{ t('pages.gateApproval.primaryBadge') }}</span
        >
      </button>
    </div>

    <div
      v-if="externalChange"
      class="rounded-md mx-3 mt-3 flex items-start gap-2 border border-warn/35 bg-warn/10 px-3 py-2 text-xs text-warn"
      data-testid="gate-external-change"
      role="status"
    >
      <div class="min-w-0 flex-1">
        <div class="font-medium">{{ t('pages.gateApproval.externalChangeTitle') }}</div>
        <div class="mt-0.5 opacity-90">{{ t('pages.gateApproval.externalChangeBody') }}</div>
      </div>
      <button
        type="button"
        class="rounded-md shrink-0 border border-line-strong bg-elevated px-2.5 py-1 text-[11px] text-txt hover:bg-overlay"
        @click="onRefreshExternal"
      >
        {{ t('pages.gateApproval.externalChangeRefresh') }}
      </button>
    </div>

    <div class="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
      <div class="rounded-lg inline-flex border border-line bg-elevated" role="group">
        <button
          type="button"
          class="px-3 py-1 text-xs"
          :class="mode === 'edit' ? 'bg-overlay text-txt' : 'text-txt2 hover:text-txt'"
          :disabled="editTabDisabled"
          data-testid="gate-mode-edit"
          @click="setMode('edit')"
        >
          {{ t('pages.gateApproval.modeEdit') }}
        </button>
        <button
          type="button"
          class="px-3 py-1 text-xs"
          :class="mode === 'preview' ? 'bg-overlay text-txt' : 'text-txt2 hover:text-txt'"
          data-testid="gate-mode-preview"
          @click="setMode('preview')"
        >
          {{ t('pages.gateApproval.modePreview') }}
        </button>
      </div>
      <div class="flex items-center gap-2">
        <span v-if="isReadonlyProduct" class="text-[11px] text-txt3">{{
          t('pages.gateApproval.readonlyHint')
        }}</span>
        <span v-else-if="isDirty" class="text-[11px] text-warn">{{ t('pages.gateApproval.dirtyHint') }}</span>
        <span
          v-else-if="mode === 'preview' && canEditActive"
          class="text-[11px] text-txt3"
          >{{ t('pages.gateApproval.previewSavedOnly') }}</span
        >
        <button
          v-if="canEditActive"
          type="button"
          class="inline-flex items-center gap-1.5 bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-45"
          :disabled="!isDirty || saving"
          data-testid="gate-artifact-save"
          @click="save"
        >
          <Icon v-if="saving" name="spinner" :size="12" class="animate-spin" />
          {{ t('pages.gateApproval.save') }}
        </button>
      </div>
    </div>

    <div
      v-if="saveError"
      class="rounded-md mx-3 mb-2 border border-err/30 bg-err/10 px-3 py-2 text-xs text-err"
      data-testid="gate-save-error"
    >
      {{ saveError }}
    </div>
    <div
      v-if="saveOk"
      class="rounded-md mx-3 mb-2 border border-ok/30 bg-ok/10 px-3 py-2 text-xs text-ok"
    >
      {{ t('pages.gateApproval.saveOk', { name: activeProduct?.name }) }}
    </div>

    <div
      class="rounded-lg mx-3 mb-3 flex flex-col border border-line bg-surface"
      :class="fillParent ? 'min-h-0 flex-1' : 'min-h-[240px]'"
    >
      <div
        class="flex items-center justify-between gap-2 border-b border-line bg-elevated/60 px-3 py-1.5 text-[11px] text-txt3"
      >
        <span>
          <template v-if="isReadonlyImage">
            {{ t('pages.gateApproval.paneImageReadonly') }}
          </template>
          <template v-else-if="isReadonlyProduct">
            {{ t('pages.gateApproval.readonlyHint') }}
          </template>
          <template v-else-if="activeProduct?.kind === 'json'">
            {{
              mode === 'edit'
                ? t('pages.gateApproval.paneStructuredEdit')
                : t('pages.gateApproval.paneStructuredPreview')
            }}
          </template>
          <template v-else-if="activeProduct?.kind === 'html'">
            {{
              mode === 'edit'
                ? t('pages.gateApproval.paneHtmlEdit')
                : htmlPreviewState === 'loading'
                  ? t('pages.gateApproval.paneHtmlLoading')
                  : t('pages.gateApproval.paneHtmlPreview')
            }}
          </template>
          <template v-else>
            {{
              mode === 'edit'
                ? t('pages.gateApproval.paneTextEdit')
                : t('pages.gateApproval.paneTextPreview')
            }}
          </template>
        </span>
        <div
          v-if="mode === 'edit' && canEditActive && activeProduct && isStructuredArtifactName(activeProduct.name)"
          class="rounded-lg inline-flex border border-line"
        >
          <button
            type="button"
            class="px-2 py-0.5 text-[11px]"
            :class="structMode === 'form' ? 'bg-overlay text-txt' : 'text-txt2'"
            data-testid="gate-struct-form"
            @click="structMode = 'form'"
          >
            {{ t('pages.gateApproval.structForm') }}
          </button>
          <button
            type="button"
            class="px-2 py-0.5 text-[11px]"
            :class="structMode === 'json' ? 'bg-overlay text-txt' : 'text-txt2'"
            data-testid="gate-struct-json"
            @click="switchToStructJson"
          >
            {{ t('pages.gateApproval.structJson') }}
          </button>
        </div>
      </div>

      <!-- Readonly image: artifact download / lazy load; no text edit -->
      <div
        v-if="isReadonlyImage"
        class="flex min-h-[220px] flex-1 flex-col items-center justify-center gap-3 p-4 text-[13px] text-txt2"
        data-testid="gate-readonly-image"
      >
        <div v-if="imageLoading" class="text-txt3">{{ t('pages.gateApproval.loadingArtifact') }}</div>
        <img
          v-else-if="imageSrc"
          :src="imageSrc"
          :alt="activeProduct?.name || ''"
          class="rounded-lg max-h-[320px] max-w-full border border-line object-contain"
        />
        <div v-else class="text-center text-txt3">
          <div class="font-medium text-txt">{{ activeProduct?.name }}</div>
          <div class="mt-1 text-[11px]">
            {{
              imageError
                ? t('pages.gateApproval.imageLoadFailed')
                : t('pages.gateApproval.readonlyImageHint')
            }}
          </div>
        </div>
        <p class="max-w-[36ch] text-center text-[11px] text-txt3">
          {{ t('pages.gateApproval.readonlyImageHint') }}
        </p>
      </div>

      <!-- Non-image readonly: no edit, no image loader -->
      <div
        v-else-if="isReadonlyProduct"
        class="flex min-h-[220px] flex-1 flex-col items-center justify-center gap-2 p-4 text-[13px] text-txt2"
        data-testid="gate-readonly-generic"
      >
        <div class="font-medium text-txt">{{ activeProduct?.name }}</div>
        <p class="max-w-[36ch] text-center text-[11px] text-txt3">
          {{ t('pages.gateApproval.readonlyHint') }}
        </p>
      </div>

      <!-- Edit -->
      <div
        v-else-if="mode === 'edit' && canEditActive"
        :class="fillParent ? 'min-h-0 flex-1 overflow-auto' : 'min-h-[220px] flex-1'"
      >
        <div
          v-if="
            activeProduct &&
            isStructuredArtifactName(activeProduct.name) &&
            structMode === 'form'
          "
          class="space-y-3 p-4"
          data-testid="gate-struct-form-pane"
        >
          <div
            v-if="isProposalsProduct"
            class="rounded-lg border border-warn/35 bg-warn/10 px-3.5 py-3"
            data-testid="gate-proposals-form-unsupported"
            role="status"
          >
            <div class="text-xs font-medium text-warn">
              {{ t('pages.gateApproval.proposalsFormUnsupportedTitle') }}
            </div>
            <p class="mt-1 text-xs text-txt2">
              {{ t('pages.gateApproval.proposalsFormUnsupportedBody') }}
              <button
                type="button"
                class="text-accent-2 underline"
                data-testid="gate-proposals-switch-json"
                @click="switchToStructJson"
              >
                {{ t('pages.gateApproval.structJson') }}
              </button>
            </p>
          </div>
          <div>
            <label class="label">title</label>
            <input
              v-model="formTitle"
              type="text"
              class="rounded-md w-full border border-line bg-base px-2.5 py-2 text-sm outline-none focus:border-accent-2 disabled:cursor-not-allowed disabled:opacity-55"
              data-testid="gate-form-title"
              :disabled="isProposalsProduct"
              @input="applyFormToDraft"
            />
          </div>
          <div>
            <label class="label">summary</label>
            <textarea
              v-model="formSummary"
              rows="4"
              class="rounded-md w-full border border-line bg-base px-2.5 py-2 text-sm outline-none focus:border-accent-2 disabled:cursor-not-allowed disabled:opacity-55"
              data-testid="gate-form-summary"
              :disabled="isProposalsProduct"
              @input="applyFormToDraft"
            />
          </div>
          <p v-if="!isProposalsProduct" class="text-[11px] text-txt3">
            {{ t('pages.gateApproval.formSubsetHint') }}
          </p>
        </div>
        <textarea
          v-else
          v-model="draft"
          class="min-h-[280px] w-full flex-1 resize-y border-0 bg-transparent px-4 py-3 font-mono text-[12.5px] leading-relaxed text-txt outline-none"
          data-testid="gate-artifact-textarea"
          spellcheck="false"
        />
      </div>

      <!-- Preview: always last saved -->
      <div
        v-else
        class="flex-1"
        :class="
          fillParent
            ? 'flex min-h-0 flex-col overflow-hidden'
            : 'scroll-area min-h-[220px] overflow-auto p-4'
        "
      >
        <template v-if="activeProduct?.kind === 'html'">
          <div
            v-if="htmlPreviewState === 'loading'"
            class="flex min-h-[200px] flex-col items-center justify-center gap-2.5 px-4 py-8 text-center"
            data-testid="gate-preview-loading"
          >
            <Icon name="spinner" :size="28" class="animate-spin text-accent" />
            <p class="text-[12px] text-txt3">{{ t('pages.gateApproval.loadingArtifact') }}</p>
          </div>
          <div
            v-else-if="htmlPreviewState === 'error'"
            class="flex min-h-[200px] flex-col items-center justify-center gap-2.5 px-4 py-8 text-center"
            data-testid="gate-preview-error"
            role="alert"
          >
            <p class="text-[13px] font-medium text-txt">{{ t('pages.gateApproval.previewLoadFailedTitle') }}</p>
            <p class="max-w-[36ch] text-[12px] text-txt3">
              {{ t('pages.gateApproval.previewLoadFailedBody') }}
            </p>
            <p v-if="loadError" class="max-w-[42ch] text-[11px] text-err">{{ loadError }}</p>
            <button
              type="button"
              class="mt-1 bg-accent px-3.5 py-1.5 text-xs font-medium text-white hover:bg-accent-hover"
              data-testid="gate-preview-retry"
              @click="emit('retry-load')"
            >
              {{ t('pages.gateApproval.previewRetry') }}
            </button>
          </div>
          <div
            v-else-if="htmlPreviewState === 'empty'"
            class="flex min-h-[200px] flex-col items-center justify-center gap-2.5 px-4 py-8 text-center"
            data-testid="gate-preview-empty"
          >
            <p class="text-[13px] font-medium text-txt">{{ t('pages.gateApproval.previewEmptyTitle') }}</p>
            <p class="max-w-[36ch] text-[12px] text-txt3">
              {{ t('pages.gateApproval.previewEmptyBody') }}
            </p>
          </div>
          <HtmlPreview
            v-else
            class="min-h-0 flex-1"
            :html="savedText"
            :mode="fitContent && !fillParent ? 'default' : 'inline'"
            :fit-content="fitContent && !fillParent"
            :fill-parent="fillParent"
            :max-content-height-vh="fillParent ? undefined : maxContentHeightVh"
            :content-height-offset-px="contentHeightOffsetPx"
            :inspectable="inspectable"
            :enlargeable="enlargeable"
            :modal-title="activeProduct?.name || ''"
            :comment-pins="commentPins"
            :annotate-draft="annotateDraft"
            @pick="emit('pick', $event)"
            @pin-select="emit('pin-select', $event)"
            @annotate-save="emit('annotate-save', $event)"
            @annotate-send-chat="emit('annotate-send-chat', $event)"
            @annotate-close="emit('annotate-close')"
          />
        </template>
        <StructuredArtifactView
          v-else-if="activeProduct && isStructuredArtifactName(activeProduct.name) && previewDoc"
          :name="activeProduct.name"
          :doc="previewDoc"
          :artifacts="artifacts"
          :run-id="runId"
          :run-status="runStatus"
        />
        <div
          v-else
          class="whitespace-pre-wrap text-[13px] leading-relaxed text-txt2"
        >{{ savedText }}</div>
      </div>
    </div>

    <div
      v-if="excludedNames?.length"
      class="rounded-lg mx-3 mb-3 border border-dashed border-line-strong bg-elevated px-3 py-2.5 text-[11px] text-txt3"
      data-testid="gate-excluded-produces"
    >
      <b class="font-medium text-txt2">{{ t('pages.gateApproval.excludedTitle') }}</b>
      <template v-for="(name, i) in excludedNames" :key="name">
        <code class="font-mono text-txt2">{{ name }}</code
        ><span v-if="i < excludedNames.length - 1">、</span>
      </template>
      <span class="ml-1">{{ t('pages.gateApproval.excludedHint') }}</span>
    </div>
  </div>
</template>
