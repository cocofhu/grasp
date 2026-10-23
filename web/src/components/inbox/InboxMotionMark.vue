<script setup lang="ts">
import Icon from '../ui/Icon.vue'

defineProps<{
  /** spin: sandbox still booting. pulse: agent is writing a reply. */
  kind: 'spin' | 'pulse'
}>()
</script>

<template>
  <Icon
    :name="kind === 'spin' ? 'inbox-boot' : 'inbox-reply'"
    :size="14"
    busy
    class="shrink-0 text-accent-2"
    :class="kind === 'spin' ? 'inbox-motion-spin' : 'inbox-motion-pulse'"
    :data-testid="kind === 'spin' ? 'inbox-motion-spin' : 'inbox-motion-pulse'"
    aria-hidden="true"
  />
</template>
