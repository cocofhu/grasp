<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'

withDefaults(defineProps<{ icon: string; title: string; description?: string; level?: 1 | 2 }>(), { level: 1 })
</script>

<template>
  <header class="settings-page-header" data-testid="settings-page-header">
    <div class="settings-page-header-main">
      <span class="settings-page-header-emblem" aria-hidden="true" data-testid="settings-page-header-emblem">
        <Icon :name="icon" :size="20" :interactive="false" />
      </span>
      <div class="min-w-0">
        <component :is="level === 1 ? 'h1' : 'h2'" class="settings-page-header-title">{{ title }}</component>
        <p v-if="description" class="settings-page-header-desc">{{ description }}</p>
      </div>
    </div>
    <div v-if="$slots.actions" class="settings-page-header-actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.settings-page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.settings-page-header-main {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 14px;
}
.settings-page-header-emblem {
  display: flex;
  width: 40px;
  height: 40px;
  flex: none;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(var(--c-accent) / .16);
  border-radius: 11px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / .08);
}
.settings-page-header-title {
  margin: 0;
  color: rgb(var(--c-txt));
  font-size: 22px;
  font-weight: 600;
  line-height: 30px;
  letter-spacing: -0.01em;
  overflow-wrap: anywhere;
}
.settings-page-header-desc {
  margin: 2px 0 0;
  max-width: 640px;
  color: rgb(var(--c-txt3));
  font-size: 13px;
  line-height: 20px;
}
.settings-page-header-actions {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.settings-page-header-actions:empty { display: none; }
@media (max-width: 767px) {
  .settings-page-header { flex-wrap: wrap; }
  .settings-page-header-emblem { width: 36px; height: 36px; border-radius: 10px; }
  .settings-page-header-title { font-size: 20px; line-height: 28px; }
  .settings-page-header-actions { width: 100%; justify-content: flex-start; padding-left: 50px; }
}
</style>
