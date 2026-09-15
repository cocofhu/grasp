<script setup lang="ts">
import { ref, watch, nextTick, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type PreviewIssue } from '@/lib/api/api'
import ParagraphInput from '@/components/ui/ParagraphInput.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import { imgSrc } from '@/lib/shared/compositeText'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import type { ClarifyImage } from '@/lib/shared/types'

const props = withDefaults(
  defineProps<{
    runId: string
    nodeId: string
    port?: number
    // Optional DOM selector the user picked in the remote browser; attached to
    // the next submitted issue as context.
    selector?: string
    /**
     * Auto-captured element screenshot from HtmlPreview inspect (base64 parts).
     * When requireElement is true, submit is blocked until this (or attachments) exists.
     */
    elementImage?: ClarifyImage | null
    /**
     * HtmlPreview path: body + selector + element screenshot are all required
     * before submit. app_preview keeps the looser body-or-images rule.
     */
    requireElement?: boolean
    /** Compact bottom-bar layout: history collapsed by default, mt-4 shrink-0. */
    compact?: boolean
    /**
     * Sidebar full-height layout: history flex-scrolls; no compact bottom-bar density.
     * Used when the chat is mounted in ReviewShell #sidebar.
     */
    fillSidebar?: boolean
    /** Hide the built-in submit button (parent owns 记入/打回 actions). */
    hideSubmit?: boolean
    /** issue = app preview feedback; review = gate approval review copy. */
    copyVariant?: 'review' | 'issue'
  }>(),
  {
    port: 0,
    selector: '',
    elementImage: null,
    requireElement: false,
    compact: false,
    fillSidebar: false,
    hideSubmit: false,
    copyVariant: 'issue',
  },
)

const emit = defineEmits<{
  // Ask the parent to clear the picked selector after it was submitted.
  (e: 'clear-selector'): void
  (e: 'issues-changed'): void
}>()

/** Optional parent-owned draft (GateApproval unified input). */
const text = defineModel<string>('text', { default: '' })
const attachments = defineModel<ClarifyImage[]>('images', { default: () => [] })

const { t } = useI18n()
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

const copyNs = computed(() =>
  props.copyVariant === 'review'
    ? 'pages.gateApproval.reviewFeedback'
    : 'pages.appPreview.feedback',
)

function copyKey(key: string): string {
  return `${copyNs.value}.${key}`
}

const issues = ref<PreviewIssue[]>([])
const sending = ref(false)
const loadError = ref<string | null>(null)
const listRef = ref<HTMLDivElement | null>(null)
const historyExpanded = ref(false)

const hasElementShot = computed(() => {
  if (props.elementImage?.data) return true
  return attachments.value.length > 0
})

const canSubmit = computed(() => {
  if (sending.value) return false
  const body = text.value.trim()
  if (props.requireElement) {
    return !!body && !!props.selector?.trim() && hasElementShot.value
  }
  // Text ∪ attachments ∪ element screenshot (pick) — aligned with hot reject threshold.
  return !!body || attachments.value.length > 0 || !!props.elementImage?.data
})

const hasPendingDraft = computed(
  () =>
    text.value.trim().length > 0 ||
    attachments.value.length > 0 ||
    !!props.elementImage?.data ||
    !!props.selector?.trim(),
)

const submitBlockedHint = computed(() => {
  if (!props.requireElement || canSubmit.value) return ''
  if (!props.selector?.trim()) return t(copyKey('mustPick'))
  if (!hasElementShot.value) return t(copyKey('requireScreenshot'))
  if (!text.value.trim()) return t(copyKey('requireBody'))
  return ''
})

/** Soft guidance when pick is optional (default / Gate path). */
const optionalPickHint = computed(() => {
  if (props.requireElement) return ''
  return t(copyKey('requirePick'))
})

async function load() {
  loadError.value = null
  try {
    const r = await api.listPreviewIssues(props.runId, props.nodeId)
    issues.value = r.issues || []
    await nextTick()
    if (props.fillSidebar || !props.compact || historyExpanded.value) scrollToBottom()
  } catch (e: any) {
    loadError.value = e?.message || 'load failed'
  }
}

function scrollToBottom() {
  const el = listRef.value
  if (el) el.scrollTop = el.scrollHeight
}

function toggleHistory() {
  historyExpanded.value = !historyExpanded.value
  if (historyExpanded.value) nextTick(() => scrollToBottom())
}

function collectImages(): ClarifyImage[] {
  const images: ClarifyImage[] = []
  if (props.elementImage?.data) {
    images.push({
      data: props.elementImage.data,
      mimeType: props.elementImage.mimeType || 'image/png',
    })
  }
  for (const im of attachments.value) {
    if (im.data) images.push({ data: im.data, mimeType: im.mimeType, name: im.name })
  }
  return images
}

function clearDraft() {
  text.value = ''
  attachments.value = []
}

/**
 * Submit draft into history. Optional `body` overrides the textarea (e.g. annotation-only
 * placeholder from GateApproval) and bypasses the normal canSubmit gate when non-empty.
 */
async function send(opts?: { body?: string }): Promise<boolean> {
  const images = collectImages()
  const body = (opts?.body !== undefined ? opts.body : text.value).trim()
  if (opts?.body === undefined) {
    if (!canSubmit.value) return false
  } else if (!body && images.length === 0) {
    return false
  }
  sending.value = true
  loadError.value = null
  try {
    const issue = await api.createPreviewIssue(
      props.runId,
      props.nodeId,
      body,
      props.selector || '',
      props.port || 0,
      images,
    )
    issues.value.push(issue)
    clearDraft()
    if (props.selector || props.elementImage) emit('clear-selector')
    emit('issues-changed')
    await nextTick()
    if (props.fillSidebar || !props.compact || historyExpanded.value) scrollToBottom()
    return true
  } catch (e: any) {
    loadError.value = e?.message || 'send failed'
    return false
  } finally {
    sending.value = false
  }
}

/** Flush unsent draft into history when there is submittable content. */
async function flush(): Promise<boolean> {
  if (!canSubmit.value) return false
  return send()
}

/** Re-fetch history from API (parent dual-write / external mutations). */
async function reload(): Promise<void> {
  await load()
}

async function remove(id: string) {
  loadError.value = null
  try {
    await api.deletePreviewIssue(props.runId, props.nodeId, id)
    issues.value = issues.value.filter((i) => i.id !== id)
    emit('issues-changed')
  } catch (e: any) {
    loadError.value = e?.message || 'delete failed'
  }
}

function onKeydown(e: KeyboardEvent) {
  if (props.hideSubmit) return
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    void send()
  }
}

function fmtTime(s: string): string {
  const d = new Date(s)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}

watch(() => [props.runId, props.nodeId], load, { immediate: true })

defineExpose({
  send,
  flush,
  reload,
  clearDraft,
  hasPendingDraft,
  canSubmit,
})
</script>

<template>
  <div
    class="flex min-h-0 flex-col overflow-hidden rounded-md border border-line bg-elevated"
    :class="[
      fillSidebar ? 'flex-1' : compact ? 'mt-4 shrink-0' : 'mt-3',
    ]"
    data-testid="preview-feedback-chat"
  >
    <div class="flex shrink-0 items-center justify-between border-b border-line px-3 py-2">
      <span class="text-xs font-medium text-txt2">{{ t(copyKey('title')) }}</span>
      <button
        v-if="compact && !fillSidebar && issues.length"
        type="button"
        class="rounded px-2 py-0.5 text-[11px] text-accent hover:bg-accent/10"
        @click="toggleHistory"
      >
        {{
          historyExpanded
            ? t(copyKey('collapseHistory'))
            : t(`${copyNs}.expandHistory`, { n: issues.length })
        }}
      </button>
    </div>

    <div
      v-show="fillSidebar || !compact || historyExpanded"
      ref="listRef"
      class="scroll-area min-h-0 flex-1 space-y-2 overflow-y-auto p-3"
      :class="fillSidebar ? '' : compact ? 'max-h-[200px]' : 'max-h-[240px]'"
    >
      <div v-if="!issues.length" class="py-4 text-center text-xs text-txt3">
        {{ t(copyKey('empty')) }}
      </div>
      <div v-for="is in issues" :key="is.id" class="group rounded-md border border-line bg-base p-2">
        <div class="flex items-center gap-2">
          <span
            class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium"
            :class="is.status === 'resolved' ? 'bg-ok/15 text-ok' : 'bg-warn/15 text-warn'"
          >
            {{ t(`${copyNs}.status.${is.status === 'resolved' ? 'resolved' : 'open'}`) }}
          </span>
          <span class="ml-auto text-[10px] text-txt3">{{ fmtTime(is.createdAt) }}</span>
          <button
            type="button"
            class="flex h-5 w-5 shrink-0 items-center justify-center rounded text-txt3 opacity-0 transition hover:bg-err/10 hover:text-err group-hover:opacity-100"
            :title="t(copyKey('delete'))"
            @click="remove(is.id)"
          >
            <Icon name="trash" :size="12" />
          </button>
        </div>
        <p class="mt-1 whitespace-pre-wrap break-words text-xs text-txt">{{ is.body }}</p>
        <code v-if="is.selector" class="mt-1 block truncate text-[10px] text-accent">{{ is.selector }}</code>
        <div v-if="is.images?.length" class="mt-1.5 flex flex-wrap gap-1.5">
          <template v-for="(im, ii) in is.images" :key="ii">
            <ChatImageThumb
              v-if="isImageAttachment(im)"
              mode="previewable"
              size="sm"
              thumb-class="rounded-md"
              :src="imgSrc(im)"
              :label="attachmentDisplayName(im, ii)"
              test-id="preview-issue-image-thumb"
              @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, ii))"
            />
          </template>
        </div>
      </div>
    </div>

    <div
      v-if="compact && !fillSidebar && !historyExpanded && !issues.length"
      class="px-3 py-2 text-center text-xs text-txt3"
    >
      {{ t(copyKey('empty')) }}
    </div>

    <div v-if="!hideSubmit" class="shrink-0 border-t border-line p-3">
      <div
        v-if="selector || elementImage"
        class="mb-2 flex items-center gap-2 rounded-md border border-accent/30 bg-accent/10 px-2 py-1"
      >
        <span class="shrink-0 text-[10px] text-txt3">{{ t(copyKey('attached')) }}</span>
        <code v-if="selector" class="min-w-0 flex-1 truncate text-[10px] text-accent">{{ selector }}</code>
        <ChatImageThumb
          v-if="elementImage"
          mode="previewable"
          size="xs"
          thumb-class="rounded shrink-0"
          :src="imgSrc(elementImage)"
          :label="attachmentDisplayName(elementImage)"
          test-id="preview-element-image-thumb"
          @preview="openChatImagePreview(imgSrc(elementImage), attachmentDisplayName(elementImage))"
        />
        <button
          type="button"
          class="flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-txt3 hover:text-txt"
          :title="t(copyKey('detach'))"
          @click="emit('clear-selector')"
        >
          <Icon name="close" :size="9" />
        </button>
      </div>
      <div v-if="loadError" class="mb-2 text-[11px] text-err">{{ loadError }}</div>
      <div v-if="submitBlockedHint" class="mb-2 text-[11px] text-txt3">{{ submitBlockedHint }}</div>
      <div v-else-if="optionalPickHint" class="mb-2 text-[11px] text-txt3">{{ optionalPickHint }}</div>
      <div @keydown="onKeydown">
        <ParagraphInput
          v-model:text="text"
          v-model:images="attachments"
          :disabled="sending"
          :placeholder="t(copyKey('placeholder'))"
        />
      </div>
      <div v-if="!hideSubmit" class="mt-2 flex items-center justify-between">
        <span class="text-[10px] text-txt3">{{ t(copyKey('hint')) }}</span>
        <button
          type="button"
          class="rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white transition hover:bg-accent-hover disabled:opacity-50"
          :disabled="!canSubmit"
          @click="() => send()"
        >
          {{ sending ? t(copyKey('sending')) : t(copyKey('send')) }}
        </button>
      </div>
    </div>
  </div>
  <ChatImagePreviewModal
    :open="!!imagePreview"
    :src="imagePreview?.src || ''"
    :label="imagePreview?.label || ''"
    test-id-prefix="preview-feedback-image-preview"
    @close="closeChatImagePreview"
  />
</template>
