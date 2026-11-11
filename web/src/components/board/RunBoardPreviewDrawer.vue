<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import AppButton from '@/components/ui/AppButton.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import PriorityBadge from '@/components/ui/PriorityBadge.vue'
import { fmtDuration, fmtTime } from '@/lib/shared/format'
import { runBoardTitle, runIdShort, runProgressPercent } from '@/lib/run/runBoard'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{ open: boolean; run: Run | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const router = useRouter()
const { t } = useI18n()

const title = computed(() => (props.run ? runBoardTitle(props.run) : ''))
const shortId = computed(() => (props.run ? runIdShort(props.run.id) : ''))
const progressPercent = computed(() => runProgressPercent(props.run?.progress))
const progressLabel = computed(() => (progressPercent.value == null ? '—' : `${progressPercent.value}%`))
const progressWidth = computed(() => `${progressPercent.value ?? 0}%`)

function goDetail() {
  if (!props.run) return
  const id = props.run.id
  emit('close')
  router.push('/runs/' + id)
}
</script>

<template>
  <AppDrawer :open="open" flush :title="t('pages.board.preview.title')" :width="420" @close="emit('close')">
    <div v-if="run" class="run-summary-body px-5 pb-2 pt-5">
      <h3 class="text-lg font-semibold leading-snug text-txt [overflow-wrap:anywhere]">{{ title }}</h3>
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <StatusPill :status="run.status" size="sm" />
        <PriorityBadge :priority="run.priority" />
      </div>
      <div class="mt-[22px]">
        <div class="flex items-baseline justify-between gap-3">
          <span class="text-xs text-txt2">{{ t('pages.board.preview.progress') }}</span>
          <span class="text-xl font-semibold tabular-nums text-txt">{{ progressLabel }}</span>
        </div>
        <div class="mt-2.5 h-1.5 overflow-hidden rounded-full bg-line">
          <div class="run-summary-fill h-full rounded-full bg-accent-2" :style="{ width: progressWidth }" />
        </div>
      </div>
      <dl class="run-summary-facts mb-3 mt-[22px] border-t border-line pt-[18px]">
        <div class="min-w-0">
          <dt class="mb-1 text-xs text-txt2">{{ t('common.table.workflow') }}</dt>
          <dd class="text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]">{{ run.workflowName || '—' }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="mb-1 text-xs text-txt2">{{ t('pages.board.preview.currentNode') }}</dt>
          <dd class="text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]">{{ run.currentNodeLabel || '—' }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="mb-1 text-xs text-txt2">{{ t('common.table.start') }}</dt>
          <dd class="text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]">{{ fmtTime(run.startedAt) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="mb-1 text-xs text-txt2">{{ t('pages.board.preview.duration') }}</dt>
          <dd class="text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]">{{ fmtDuration(run.durationSec) }}</dd>
        </div>
        <div class="col-span-full min-w-0">
          <dt class="mb-1 text-xs text-txt2">{{ t('pages.board.preview.runId') }}</dt>
          <dd class="font-mono text-[13px] font-medium leading-snug text-txt [overflow-wrap:anywhere]">#{{ shortId }}</dd>
        </div>
      </dl>
    </div>
    <template #footer>
      <div class="run-summary-actions flex justify-end gap-2">
        <AppButton variant="outline" size="md" @click="emit('close')">
          {{ t('pages.board.preview.dismiss') }}
        </AppButton>
        <AppButton variant="primary" size="md" :glow="false" :disabled="!run" @click="goDetail">
          {{ t('pages.board.preview.openDetail') }}
        </AppButton>
      </div>
    </template>
  </AppDrawer>
</template>

<style scoped>
.run-summary-body {
  container-type: inline-size;
  container-name: run-summary;
}
.run-summary-facts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px 20px;
}
@container run-summary (max-width: 359px) {
  .run-summary-facts {
    grid-template-columns: 1fr;
  }
}
.run-summary-actions :deep(button) {
  flex: none;
}
</style>
