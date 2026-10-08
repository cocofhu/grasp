<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  beginAutoLoad,
  beginManualRetry,
  isKnownLoaded,
  isKnownMissing,
  markLoaded,
  markMissing,
  parseBlobId,
  subscribe,
} from '@/lib/shared/blobMissingCache'

const props = withDefaults(
  defineProps<{
    src: string
    label: string
    /** previewable: click opens preview; locked: ReAct agent/draft guard */
    mode?: 'previewable' | 'locked'
    size?: 'md' | 'sm' | 'xs'
    /**
     * cover: fixed square crop (composer, locked, other chats).
     * contain: full aspect ratio, width limited by the parent column.
     */
    fit?: 'cover' | 'contain'
    /** Extra classes (e.g. rounded-md) so each surface keeps its corner rules. */
    thumbClass?: string
    testId?: string
    alt?: string
  }>(),
  { mode: 'previewable', size: 'md', fit: 'cover', thumbClass: '', alt: '' },
)

const emit = defineEmits<{ preview: [] }>()
const { t } = useI18n()

const loadFailed = ref(false)
const retryNonce = ref(0)
/** When false, do not mount requesting <img> (known missing / peer inflight). */
const allowImg = ref(true)
const cacheTick = ref(0)

const blobId = computed(() => parseBlobId(props.src))

const sizeClass = computed(() => {
  if (props.fit === 'contain') return 'inline-block w-fit max-w-full'
  if (props.size === 'sm') return 'h-14 w-14'
  if (props.size === 'xs') return 'h-8 w-8'
  return 'h-20 w-20'
})

const imgClass = computed(() =>
  props.fit === 'contain'
    ? 'block h-auto max-h-48 w-auto max-w-full object-contain'
    : 'h-full w-full object-cover',
)

const failCardClass = computed(() => {
  if (props.size === 'xs') return 'w-28'
  if (props.size === 'sm') return 'w-36'
  return 'w-[10.5rem]'
})

const hintClass = computed(() =>
  props.size === 'xs'
    ? 'text-[8px] leading-tight'
    : 'text-[10px] leading-tight',
)

const displaySrc = computed(() => {
  if (!retryNonce.value || !props.src) return props.src
  const sep = props.src.includes('?') ? '&' : '?'
  return `${props.src}${sep}_r=${retryNonce.value}`
})

const retryTestId = computed(() =>
  props.testId ? `${props.testId}-retry` : 'chat-image-retry',
)

function syncFromCache() {
  void cacheTick.value
  const id = blobId.value
  if (!id) {
    allowImg.value = !!props.src
    return
  }
  if (isKnownMissing(id)) {
    loadFailed.value = true
    allowImg.value = false
    return
  }
  if (retryNonce.value > 0) {
    // Manual retry path owns the next GET.
    allowImg.value = true
    return
  }
  const decision = beginAutoLoad(id)
  if (decision === 'blocked_missing') {
    loadFailed.value = true
    allowImg.value = false
    return
  }
  if (decision === 'blocked_pending') {
    allowImg.value = false
    return
  }
  allowImg.value = true
}

watch(
  () => props.src,
  () => {
    loadFailed.value = false
    retryNonce.value = 0
    syncFromCache()
  },
  { immediate: true },
)

const unsub = subscribe(() => {
  cacheTick.value += 1
  const id = blobId.value
  if (id && isKnownMissing(id)) {
    loadFailed.value = true
    allowImg.value = false
    return
  }
  if (id && loadFailed.value && isKnownLoaded(id)) {
    // Peer chat retry succeeded — show via browser cache; no extra auto GET budget.
    loadFailed.value = false
    allowImg.value = true
    return
  }
  // Waiting on peer inflight / gate: re-run beginAutoLoad for THIS id only.
  // Never flip allowImg=true on an unrelated peer notify (g1.2 race).
  if (!allowImg.value && id && !loadFailed.value) {
    syncFromCache()
  }
})
onUnmounted(unsub)

function onImgError() {
  loadFailed.value = true
  allowImg.value = false
  const id = blobId.value
  if (id) markMissing(id)
}

function onImgLoad() {
  loadFailed.value = false
  allowImg.value = true
  const id = blobId.value
  if (id) markLoaded(id)
}

function retryLoad() {
  const id = blobId.value
  if (id) beginManualRetry(id)
  retryNonce.value += 1
  loadFailed.value = false
  allowImg.value = true
}

function onPreviewClick() {
  if (loadFailed.value) return
  if (props.mode === 'previewable') emit('preview')
}
</script>

<template>
  <div
    v-if="loadFailed"
    class="flex flex-col gap-1.5 rounded-md border border-err/40 bg-err/[0.08] p-2"
    :class="[failCardClass, thumbClass]"
    :data-testid="testId"
    data-image-failed="1"
  >
    <div
      class="flex h-7 w-7 items-center justify-center rounded border border-err/35 text-red-300"
      aria-hidden="true"
    >
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7">
        <rect x="3" y="5" width="18" height="14" />
        <path d="M3 15l5-5 4 4 3-3 6 6" />
        <path d="M15 5l6 6" />
      </svg>
    </div>
    <div class="text-[12px] font-semibold text-red-300">{{ t('common.chatImage.imageLoadFailed') }}</div>
    <div
      class="truncate font-mono text-[11px] text-txt2"
      :title="label"
    >{{ label }}</div>
    <button
      type="button"
      class="rounded-md bg-accent px-2.5 py-1 text-left text-[12px] text-white hover:brightness-110"
      :data-testid="retryTestId"
      @click.stop="retryLoad"
    >
      {{ t('common.chatImage.retry') }}
    </button>
  </div>
  <button
    v-else-if="mode === 'previewable'"
    type="button"
    class="group relative cursor-pointer overflow-hidden rounded-md border border-line transition hover:border-accent focus-visible:border-accent focus-visible:outline-none"
    :class="[sizeClass, thumbClass]"
    :data-testid="testId"
    :aria-label="t('common.chatImage.previewAria', { label })"
    @click="onPreviewClick"
  >
    <img
      v-if="allowImg"
      :src="displaySrc"
      :class="imgClass"
      :alt="alt"
      @error="onImgError"
      @load="onImgLoad"
    />
    <span
      class="pointer-events-none absolute inset-x-0 bottom-0 bg-black/55 px-1 py-0.5 text-center text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
      :class="hintClass"
    >{{ t('common.chatImage.clickToEnlarge') }}</span>
  </button>
  <div
    v-else
    class="relative overflow-hidden rounded-md border border-line"
    :class="[sizeClass, thumbClass]"
    :data-testid="testId"
  >
    <img
      v-if="allowImg"
      :src="displaySrc"
      :class="imgClass"
      :alt="alt"
      @error="onImgError"
      @load="onImgLoad"
    />
    <span
      class="pointer-events-none absolute inset-x-0 bottom-0 bg-black/45 px-0.5 py-0.5 text-center text-[9px] leading-tight text-txt2"
    >{{ t('common.chatImage.previewUnavailable') }}</span>
  </div>
</template>
