<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { IconName } from '@/lib/icons/catalog'

defineProps<{
  icon: IconName
  label: string
  collapsed: boolean
  count?: number
  toggleTestId: string
}>()
const emit = defineEmits<{ toggle: [] }>()
const { t } = useI18n()
</script>

<template>
  <div class="sidebar-section-heading grasp-status-host">
    <button
      type="button"
      class="sidebar-section-label"
      :data-testid="toggleTestId"
      :aria-expanded="!collapsed"
      :aria-label="t(collapsed ? 'nav.sectionExpand' : 'nav.sectionCollapse', { name: label })"
      @click="emit('toggle')"
    >
      <Icon :name="icon" :size="14" class="sidebar-section-icon" aria-hidden="true" />
      <span>{{ label }}</span>
      <Icon :interactive="false" name="chevron-down" :size="12" class="sidebar-section-chevron" :class="{ 'is-collapsed': collapsed }" aria-hidden="true" />
    </button>
    <span v-if="count !== undefined" class="sidebar-section-count">{{ count }}</span>
  </div>
</template>

<style scoped>
.sidebar-section-heading {
  display: flex;
  height: 26px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
  padding: 0 10px;
  color: light-dark(#3f3f46, color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3))));
  font-size: 11.5px;
  font-weight: 600;
}
.sidebar-section-label {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  border-radius: 6px;
  color: inherit;
  font: inherit;
  transition: color 160ms ease;
}
.sidebar-section-label:hover {
  color: rgb(var(--c-txt));
}
.sidebar-section-label:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .6);
  outline-offset: -2px;
}
.sidebar-section-icon {
  flex: none;
}
.sidebar-section-chevron {
  flex: none;
  margin-left: -1px;
  opacity: .7;
  transition: transform 160ms ease, opacity 160ms ease;
}
.sidebar-section-label:hover .sidebar-section-chevron {
  opacity: 1;
}
.sidebar-section-chevron.is-collapsed {
  transform: rotate(-90deg);
}
@media (prefers-reduced-motion: reduce) {
  .sidebar-section-chevron {
    transition: none;
  }
}
.sidebar-section-count {
  display: inline-flex;
  min-width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-txt) / .04);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
</style>
