<script setup lang="ts">
/** Generic shell skeleton: title + 2 cards + rows. `list` renders rows only (sidebars, trees). */
withDefaults(defineProps<{ rows?: number; list?: boolean }>(), { rows: 5, list: false })
</script>

<template>
  <div class="app-skeleton" aria-hidden="true" data-testid="app-skeleton">
    <template v-if="!list">
      <div class="app-skeleton__block app-skeleton__title" />
      <div class="mb-2.5 grid grid-cols-2 gap-2.5">
        <div class="app-skeleton__block app-skeleton__card" />
        <div class="app-skeleton__block app-skeleton__card" />
      </div>
    </template>
    <div v-for="i in rows" :key="i" class="app-skeleton__block app-skeleton__row" />
  </div>
</template>
