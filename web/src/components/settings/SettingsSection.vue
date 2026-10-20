<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <section class="settings-section">
    <header class="mb-3">
      <h2 class="m-0 text-[15px] font-semibold leading-6 text-txt">{{ title }}</h2>
      <p v-if="description" class="mb-0 mt-1 text-[13px] leading-5 text-txt3">{{ description }}</p>
    </header>
    <div class="settings-section-list min-w-0">
      <slot />
    </div>
    <div v-if="$slots.footer" class="mt-2 text-xs leading-5 text-txt3"><slot name="footer" /></div>
  </section>
</template>

<style scoped>
.settings-section + .settings-section { margin-top: 32px; }
.settings-section-list { border-block: 1px solid rgb(var(--c-line) / 0.6); }
.settings-section-list :deep(> .settings-row + .settings-row) { border-top: 1px solid rgb(var(--c-line) / 0.6); }
</style>
