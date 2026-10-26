<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'

withDefaults(
  defineProps<{
    /** Override the default “Refreshing…” label. */
    message?: string
  }>(),
  {},
)

const { t } = useI18n()
</script>

<template>
  <div
    class="flex items-center gap-2 border-b border-accent/35 bg-accent-dim px-3 py-1.5 text-[12px] text-accent-2"
    role="status"
    aria-live="polite"
    data-testid="refresh-strip"
  >
    <Icon name="spinner" :size="12" class="text-accent" aria-hidden="true" />
    <span>{{ message || t('common.buttons.refreshing') }}</span>
  </div>
</template>
