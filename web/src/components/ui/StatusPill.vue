<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'

const props = defineProps<{ status: string; size?: 'sm' | 'md' }>()
const { t } = useI18n()

const map: Record<string, { labelKey: string; cls: string; icon: string; spin?: boolean }> = {
  running: { labelKey: 'common.status.running', cls: 'text-info bg-info/10 border-info/30', icon: 'breath-dot', spin: true },
  queued: { labelKey: 'common.status.queued', cls: 'text-txt2 bg-elevated border-line', icon: 'clock' },
  pending: { labelKey: 'common.status.pending', cls: 'text-txt3 bg-elevated border-line', icon: 'dot' },
  waiting_human: { labelKey: 'common.status.waiting_human', cls: 'text-warn bg-warn/10 border-warn/30', icon: 'user-hold' },
  completed: { labelKey: 'common.status.completed', cls: 'text-ok bg-ok/10 border-ok/30', icon: 'check' },
  success: { labelKey: 'common.status.success', cls: 'text-ok bg-ok/10 border-ok/30', icon: 'check' },
  failed: { labelKey: 'common.status.failed', cls: 'text-err bg-err/10 border-err/30', icon: 'alert' },
  cancelled: { labelKey: 'common.status.cancelled', cls: 'text-txt3 bg-elevated border-line', icon: 'close' },
  cancelling: { labelKey: 'common.status.cancelling', cls: 'text-txt3 bg-elevated border-line', icon: 'close' },
  skipped: { labelKey: 'common.status.skipped', cls: 'text-txt3 bg-elevated border-line', icon: 'dot' },
  published: { labelKey: 'common.status.published', cls: 'text-ok bg-ok/10 border-ok/30', icon: 'check' },
  draft: { labelKey: 'common.status.draft', cls: 'text-txt2 bg-elevated border-line', icon: 'edit' },
}

const cfg = computed(() => {
  const base = map[props.status] ?? map.pending
  return { ...base, label: t(base.labelKey) }
})
const pad = computed(() => (props.size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs'))
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border font-medium"
    :class="[cfg.cls, pad]"
  >
    <Icon :name="cfg.icon" :size="12" :busy="!!cfg.spin" />
    {{ cfg.label }}
  </span>
</template>
