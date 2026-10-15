<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
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
    /** Prefix for data-testid (e.g. clarify-image-preview). */
    testIdPrefix?: string
    /** modal: cap height inside the full-page dialog; frame: fill the host window. */
    fit?: 'modal' | 'frame'
    /** Caller already knows this blob is missing; do not request it again. */
    forceMissing?: boolean
  }>(),
  { testIdPrefix: 'chat-image-preview', fit: 'modal', forceMissing: false },
)

const emit = defineEmits<{ missing: [blobId: string] }>()

const { t } = useI18n()
const loadFailed = ref(false)
const cacheTick = ref(0)

const blobId = computed(() => parseBlobId(props.src))

const blockedMissing = computed(() => {
  void cacheTick.value
  const id = blobId.value
  return props.forceMissing || (!!id && isKnownMissing(id))
})

const showFailed = computed(() => loadFailed.value || blockedMissing.value)

watch(
  () => [props.src, props.forceMissing] as const,
  () => {
    loadFailed.value = false
  },
)

const unsub = subscribe(() => {
  cacheTick.value += 1
  const id = blobId.value
  if (id && isKnownMissing(id)) {
    loadFailed.value = true
  } else if (id && isKnownLoaded(id)) {
    loadFailed.value = false
  }
})
onUnmounted(unsub)

const ids = computed(() => ({
  body: `${props.testIdPrefix}-body`,
  img: `${props.testIdPrefix}-img`,
  failed: `${props.testIdPrefix}-failed`,
}))

function onError() {
  loadFailed.value = true
  const id = blobId.value
  if (id) {
    markMissing(id)
    emit('missing', id)
  }
}

function onLoad() {
  loadFailed.value = false
  const id = blobId.value
  if (id) markLoaded(id)
}
</script>

<template>
  <div
    class="flex items-center justify-center"
    :class="fit === 'frame' ? 'h-full min-h-0 w-full' : 'min-h-[280px]'"
    :data-testid="ids.body"
  >
    <div
      v-if="showFailed"
      class="px-4 py-8 text-center text-[13px] text-txt2"
      :data-testid="ids.failed"
    >
      <em class="mb-2 block font-semibold not-italic text-err">{{ t('common.chatImage.imageLoadFailed') }}</em>
      <span v-if="blockedMissing" class="block text-[12px] text-txt3">
        {{ t('common.compositeImage.attachmentUnavailable') }}
      </span>
    </div>
    <img
      v-else
      :src="src"
      :alt="label"
      :class="fit === 'frame' ? 'max-h-full max-w-full object-contain' : 'max-h-[74vh] max-w-full object-contain'"
      :data-testid="ids.img"
      @error="onError"
      @load="onLoad"
    />
  </div>
</template>
