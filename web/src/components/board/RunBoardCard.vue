<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusPill from '@/components/ui/StatusPill.vue'
import PriorityBadge from '@/components/ui/PriorityBadge.vue'
import { fmtDuration, truncateText } from '@/lib/shared/format'
import { runBoardTitle, runIdShort } from '@/lib/run/runBoard'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{ run: Run }>()
const emit = defineEmits<{ (e: 'select', run: Run): void }>()
const { t } = useI18n()

const title = computed(() => runBoardTitle(props.run))
const shortId = computed(() => runIdShort(props.run.id))
const showProgress = computed(
  () => props.run.status === 'running' || props.run.status === 'waiting_human',
)
const nodeLine = computed(() => {
  if (showProgress.value && props.run.currentNodeLabel) {
    return t('pages.board.card.currentNode', { label: props.run.currentNodeLabel })
  }
  if (props.run.status === 'completed' && props.run.durationSec != null) {
    return t('pages.board.card.duration', { duration: fmtDuration(props.run.durationSec) })
  }
  return props.run.currentNodeLabel || ''
})
</script>

<template>
  <button
    type="button"
    class="run-board-card w-full border-b border-line/70 px-3.5 py-4 text-left transition-colors last:border-b-0 hover:bg-elevated/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-accent-2"
    @click="emit('select', run)"
  >
    <div class="mb-2 flex items-start justify-between gap-2">
      <div class="min-w-0 text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]" :title="title">
        {{ truncateText(title, 64) }}
      </div>
      <div class="flex shrink-0 flex-col items-end gap-1">
        <StatusPill :status="run.status" size="sm" />
        <PriorityBadge v-if="run.priority === 'high' || run.priority === 'low'" :priority="run.priority" />
      </div>
    </div>
    <div class="mb-1.5 truncate text-xs text-txt2" :title="`${run.workflowName} · #${shortId}`">
      {{ run.workflowName }} · #{{ shortId }}
    </div>
    <div v-if="nodeLine" class="truncate text-[11px] text-txt3" :title="nodeLine">
      {{ nodeLine }}
    </div>
    <div v-if="showProgress" class="mt-2 h-0.5 overflow-hidden bg-elevated">
      <div
        class="h-full bg-accent-2 transition-[width] duration-300"
        :style="{ width: `${Math.min(100, Math.max(0, run.progress || 0))}%` }"
      />
    </div>
  </button>
</template>
