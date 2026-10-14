<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from './AppModal.vue'
import ChatImagePreviewBody from './ChatImagePreviewBody.vue'

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
  </AppModal>
</template>
