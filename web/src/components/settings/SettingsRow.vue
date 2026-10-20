<script setup lang="ts">
defineProps<{ label: string; description?: string; for?: string; stacked?: boolean; inline?: boolean }>()
</script>

<template>
  <div class="settings-row py-4" :class="{ 'settings-row-stacked': stacked, 'settings-row-inline': inline && !stacked }">
    <div class="settings-row-main">
      <div class="settings-row-copy min-w-0 flex-1">
        <label v-if="$props.for" :for="$props.for" class="text-[14px] font-medium leading-5 text-txt">{{ label }}</label>
        <div v-else class="text-[14px] font-medium leading-5 text-txt">{{ label }}</div>
        <div v-if="$slots.description || description" class="mt-1 text-[13px] leading-5 text-txt3"><slot name="description">{{ description }}</slot></div>
        <div v-if="$slots.meta" class="mt-2 flex flex-wrap items-center gap-2 text-[11px] leading-4 text-txt3"><slot name="meta" /></div>
      </div>
      <div class="settings-row-control min-w-0"><slot /></div>
    </div>
    <div v-if="$slots.details" class="mt-3 min-w-0 text-[13px] leading-5 text-txt3"><slot name="details" /></div>
  </div>
</template>

<style scoped>
.settings-row-main { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.settings-row-control { flex-shrink: 0; max-width: 50%; }
.settings-row-stacked .settings-row-main { flex-direction: column; align-items: stretch; gap: 12px; }
.settings-row-stacked .settings-row-control { max-width: none; }
@media (max-width: 639px) {
  .settings-row-main { flex-direction: column; align-items: stretch; gap: 12px; }
  .settings-row-control { max-width: none; }
  .settings-row-inline .settings-row-main { flex-direction: row; align-items: center; }
  .settings-row-inline .settings-row-control { flex: none; max-width: none; }
}
</style>
