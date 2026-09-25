<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PageControlState } from '@/lib/inbox/embedPageControl'

/** One-line page-control status: can the agent act on this person's preview page right now. */
const props = withDefaults(defineProps<{ state: PageControlState; active?: boolean }>(), { active: true })

const { t } = useI18n()

const view = computed(() => {
  if (props.state === 'online' && !props.active) return { key: 'transferred', dot: 'bg-warn' }
  if (props.state === 'online') return { key: 'online', dot: 'bg-ok' }
  if (props.state === 'paused') return { key: 'paused', dot: 'bg-warn' }
  return { key: 'offline', dot: 'bg-txt3' }
})
</script>

<template>
  <span
    class="inline-flex min-w-0 items-center gap-1.5 text-[11px] leading-snug text-txt3"
    role="status"
    data-testid="page-control-status"
    :data-state="view.key"
  >
    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="view.dot" aria-hidden="true" />
    <span class="min-w-0 truncate">{{ t(`pages.embedChat.pageControl.status.${view.key}`) }}</span>
  </span>
</template>
