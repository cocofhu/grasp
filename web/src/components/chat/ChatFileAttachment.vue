<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import { attachmentByteLength, attachmentDetails, attachmentDisplayName, attachmentDownloadUrl, attachmentKind, attachmentMime, SITE_ATTACH_MAX_BYTES } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import type { ClarifyImage } from '@/lib/shared/types'

const props = defineProps<{ attachment: ClarifyImage; label?: string; src?: string; compact?: boolean }>()
defineOptions({ inheritAttrs: false })
const { t } = useI18n()
const open = ref(false)
const failed = ref(false)
const player = ref<HTMLMediaElement>()
const label = computed(() => props.label || attachmentDisplayName(props.attachment))
const kind = computed(() => attachmentKind(props.attachment))
const src = computed(() => attachmentDownloadUrl(props.src ?? imgSrc(props.attachment)))
const tooLarge = computed(() => attachmentByteLength(props.attachment) > SITE_ATTACH_MAX_BYTES)
const reading = computed(() => props.attachment.readState === 'reading')
const icon = computed(() => reading.value ? 'spinner' : kind.value === 'audio' ? 'file-audio' : kind.value === 'video' ? 'file-video' : 'artifact')
watch([open, src], () => {
  if (!open.value) player.value?.pause()
  failed.value = false
})
</script>

<template>
  <button
    v-bind="$attrs"
    type="button"
    class="chat-file-chip flex max-w-[220px] items-center gap-2 rounded-md border border-line bg-elevated px-2 py-1.5 text-left transition hover:border-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-60"
    :class="compact ? 'h-14' : ''"
    :title="label"
    :aria-label="t('common.attachments.preview', { name: label })"
    :disabled="reading"
    :aria-busy="reading"
    :data-attachment-kind="kind"
    @click="open = true"
  >
    <Icon :name="icon" :size="18" class="shrink-0 text-txt2" />
    <span class="min-w-0">
      <span class="block truncate text-[12px] text-txt">{{ label }}</span>
      <span class="block truncate text-[10px] text-txt3">{{ reading ? t('common.attachments.reading') : attachmentDetails(attachment) }}</span>
    </span>
  </button>
  <AppModal :open="open" :title="label" :width="880" close-on-esc trap-focus @close="open = false">
    <template v-if="open">
      <p class="mb-3 text-xs text-txt3">{{ attachmentDetails(attachment) }}</p>
      <p v-if="!src || failed || tooLarge || (kind !== 'audio' && kind !== 'video')" class="py-8 text-center text-sm text-txt2" role="status" data-testid="attachment-preview-fallback">
        {{ !src ? t('common.attachments.unavailable') : tooLarge ? t('common.attachments.tooLarge') : failed ? t('common.attachments.previewFailed') : t('common.attachments.unsupported') }}
      </p>
      <component
        :is="kind"
        ref="player"
        v-else
        :key="src"
        controls
        preload="metadata"
        playsinline
        tabindex="0"
        class="mx-auto max-h-[65vh] w-full"
        :aria-label="label"
        data-testid="attachment-media-player"
        @error="failed = true"
      >
        <source :src="src" :type="attachmentMime(attachment)" @error="failed = true" />
      </component>
      <a v-if="src" :href="src" :download="label" class="mt-4 inline-flex min-h-9 items-center gap-2 rounded-md border border-line px-3 py-2 text-sm text-txt hover:border-accent" data-testid="attachment-download">
        <Icon name="download" :size="16" />{{ t('common.attachments.download') }}
      </a>
    </template>
  </AppModal>
</template>
