<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'

const props = defineProps<{
  messageKey?: string
  message?: string
}>()

const { t } = useI18n()

const displayMessage = computed(() => {
  if (props.message) return props.message
  if (props.messageKey) return t(props.messageKey)
  return t('pages.gateApproval.loadingArtifact')
})
</script>

<template>
  <div class="flex h-full min-h-[200px] flex-col items-center justify-center gap-2.5 px-6 py-8 text-center">
    <Icon name="spinner" :size="28" class="text-accent" />
    <p class="text-[12px] text-txt3">{{ displayMessage }}</p>
  </div>
</template>
