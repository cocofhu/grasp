<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'

defineProps<{ title: string; description?: string; icon?: string; count?: number | string }>()
</script>

<template>
  <section class="settings-section">
    <header class="settings-section-head">
      <div class="flex min-w-0 items-start gap-2.5">
        <Icon v-if="icon" :name="icon" :size="16" :interactive="false" class="settings-section-icon" data-testid="settings-section-icon" />
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="m-0 min-w-0 text-[14px] font-semibold leading-6 text-txt">{{ title }}</h2>
            <span v-if="count !== undefined && count !== ''" class="settings-section-count" data-testid="settings-section-count">{{ count }}</span>
          </div>
          <p v-if="description" class="mb-0 mt-0.5 text-[12px] leading-5 text-txt3">{{ description }}</p>
        </div>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2"><slot name="actions" /></div>
    </header>
    <div class="settings-section-list min-w-0">
      <slot />
    </div>
    <div v-if="$slots.footer" class="settings-section-footer text-xs leading-5 text-txt3"><slot name="footer" /></div>
  </section>
</template>

<style scoped>
.settings-section {
  min-width: 0;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
}
.settings-section + .settings-section { margin-top: 24px; }
.settings-section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 20px;
  border-bottom: 1px solid rgb(var(--c-line) / 0.8);
}
.settings-section-icon { margin-top: 4px; flex-shrink: 0; color: rgb(var(--c-txt3)); }
.settings-section-count {
  border-radius: 999px;
  padding: 0 7px;
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 18px;
}
.settings-section-list { padding-inline: 20px; }
.settings-section-list :deep(> .settings-row + .settings-row) { border-top: 1px solid rgb(var(--c-line) / 0.6); }
.settings-section-footer {
  padding: 10px 20px 12px;
  border-top: 1px solid rgb(var(--c-line) / 0.6);
}
@media (max-width: 767px) {
  .settings-section-head { padding: 12px 16px; }
  .settings-section-list { padding-inline: 16px; }
  .settings-section-footer { padding-inline: 16px; }
}
</style>
