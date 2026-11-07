<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from './AppModal.vue'
import ChatImagePreviewBody from './ChatImagePreviewBody.vue'
import Icon from './Icon.vue'
import { attachmentDownloadUrl } from '@/lib/shared/attachments'

const props = withDefaults(
  defineProps<{
    open: boolean
    src: string
    label: string
    /** Prefix for data-testid (e.g. clarify-image-preview). */
    testIdPrefix?: string
  }>(),
  { testIdPrefix: 'chat-image-preview' },
)

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const title = computed(() =>
  props.label ? t('common.chatImage.previewTitle', { label: props.label }) : '',
)
</script>

<template>
  <AppModal :open="open" :title="title" :width="960" @close="emit('close')">
    <ChatImagePreviewBody
      v-if="open"
      :src="src"
      :label="label"
      :test-id-prefix="testIdPrefix"
    />
    <a v-if="open && attachmentDownloadUrl(src)" :href="attachmentDownloadUrl(src)" :download="label || 'image'" class="mt-3 inline-flex min-h-9 items-center gap-2 rounded-md border border-line px-3 py-2 text-sm text-txt hover:border-accent" data-testid="chat-image-download">
      <Icon name="download" :size="16" />{{ t('common.attachments.download') }}
    </a>
  </AppModal>
</template>
