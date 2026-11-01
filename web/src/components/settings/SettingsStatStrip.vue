<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/ui/Icon.vue'
import type { SettingsStatItem } from './settingsStats'

const props = withDefaults(defineProps<{ items: SettingsStatItem[]; loading?: boolean }>(), { loading: false })
const visible = computed(() => props.items.filter((item) => item.value !== undefined && item.value !== null && item.value !== ''))
const skeletonCount = computed(() => Math.min(4, Math.max(1, props.items.length || 4)))
</script>

<template>
  <div v-if="loading" class="settings-stat-strip m-0" data-testid="settings-stat-strip" aria-hidden="true" data-loading="true">
    <div v-for="n in skeletonCount" :key="n" class="settings-stat settings-stat--skeleton" data-testid="settings-stat-skeleton">
      <span class="settings-stat-icon animate-pulse" />
      <span class="min-w-0 flex-1">
        <span class="block h-3 w-16 animate-pulse rounded bg-elevated" />
        <span class="mt-2 block h-5 w-10 animate-pulse rounded bg-elevated" />
      </span>
    </div>
  </div>
  <dl v-else-if="visible.length" class="settings-stat-strip m-0" data-testid="settings-stat-strip">
    <div
      v-for="item in visible"
      :key="item.key"
      class="settings-stat"
      :class="`settings-stat--${item.tone || 'neutral'}`"
      :data-testid="`settings-stat-${item.key}`"
    >
      <span class="settings-stat-icon" aria-hidden="true"><Icon :name="item.icon" :size="16" :interactive="false" /></span>
      <div class="min-w-0">
        <dt class="settings-stat-label">{{ item.label }}</dt>
        <dd class="settings-stat-value" :class="{ 'settings-stat-value--text': typeof item.value === 'string' }">
          <span data-testid="settings-stat-value">{{ item.value }}</span>
          <span v-if="item.total !== undefined" class="settings-stat-extra">/ {{ item.total }}</span>
          <span v-if="item.unit" class="settings-stat-extra">{{ item.unit }}</span>
        </dd>
      </div>
    </div>
  </dl>
</template>

<style scoped>
.settings-stat-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.settings-stat {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 12px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  padding: 14px 16px;
  background: rgb(var(--c-surface));
}
.settings-stat-icon {
  display: flex;
  width: 34px;
  height: 34px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.settings-stat--ok .settings-stat-icon { color: rgb(var(--c-ok)); background: rgb(var(--c-ok) / .1); }
.settings-stat--warn .settings-stat-icon { color: rgb(var(--c-warn)); background: rgb(var(--c-warn) / .12); }
.settings-stat--accent .settings-stat-icon { color: rgb(var(--c-accent-2)); background: rgb(var(--c-accent) / .1); }
.settings-stat-label {
  overflow: hidden;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.settings-stat-value {
  margin: 0;
  overflow: hidden;
  color: rgb(var(--c-txt));
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: 26px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.settings-stat-value--text { font-size: 16px; }
.settings-stat-extra {
  margin-left: 3px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  font-weight: 500;
}
@media (max-width: 767px) {
  .settings-stat-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .settings-stat { gap: 10px; padding: 12px; }
  .settings-stat-icon { width: 30px; height: 30px; }
  .settings-stat-value { font-size: 18px; }
  .settings-stat-value--text { font-size: 15px; }
}
</style>
