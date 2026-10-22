<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { HomeMode } from '@/lib/solo/soloPrefs'

defineProps<{ modelValue: HomeMode }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: HomeMode): void }>()
const { t } = useI18n()

const MODES: { id: HomeMode; icon: string; key: string }[] = [
  { id: 'workflow', icon: 'workflow', key: 'pages.solo.home.modeWorkflow' },
  { id: 'solo', icon: 'terminal', key: 'pages.solo.home.modeSolo' },
]
</script>

<template>
  <div class="home-mode" role="tablist" :aria-label="t('pages.solo.home.modeLabel')" data-testid="home-mode-switch">
    <button
      v-for="m in MODES"
      :key="m.id"
      type="button"
      role="tab"
      class="home-mode__opt"
      :class="{ 'home-mode__opt--on': modelValue === m.id }"
      :aria-selected="modelValue === m.id"
      :data-testid="`home-mode-${m.id}`"
      @click="emit('update:modelValue', m.id)"
    >
      <Icon :name="m.icon" :size="13" />
      <span>{{ t(m.key) }}</span>
    </button>
  </div>
</template>

<style scoped>
.home-mode {
  display: inline-flex;
  gap: 2px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
  background: rgb(var(--c-surface) / 0.7);
  padding: 3px;
  backdrop-filter: blur(6px);
}
.home-mode__opt {
  display: inline-flex;
  height: 26px;
  align-items: center;
  gap: 5px;
  border-radius: 999px;
  padding: 0 12px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  font-weight: 500;
  transition: background 0.16s ease, color 0.16s ease;
}
.home-mode__opt:hover {
  color: rgb(var(--c-txt));
}
.home-mode__opt--on {
  background: rgb(var(--c-accent) / 0.12);
  color: rgb(var(--c-accent-2));
}
.home-mode__opt:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / 0.6);
  outline-offset: 1px;
}
</style>
