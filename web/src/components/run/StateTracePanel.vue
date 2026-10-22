<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { llmTraceMark } from './llm/LlmSystemRow.vue'
import { fmtTime } from '@/lib/shared/format'
import type { StateTraceEntry } from '@/lib/shared/types'

const props = defineProps<{ trace: StateTraceEntry[] }>()

const { t } = useI18n()

const META: Record<string, { icon: string; cls: string; labelKey: string }> = {
  enter: { icon: 'arrow-right', cls: 'text-info border-info/40 bg-info/10', labelKey: 'pages.stateTrace.event.enter' },
  exit: { icon: 'check', cls: 'text-ok border-ok/40 bg-ok/10', labelKey: 'pages.stateTrace.event.exit' },
  transition: { icon: 'branch', cls: 'text-txt2 border-line bg-elevated', labelKey: 'pages.stateTrace.event.transition' },
  rollback: { icon: 'history', cls: 'text-warn border-warn/40 bg-warn/10', labelKey: 'pages.stateTrace.event.rollback' },
  pause: { icon: 'gate', cls: 'text-warn border-warn/40 bg-warn/10', labelKey: 'pages.stateTrace.event.pause' },
  resume: { icon: 'play', cls: 'text-accent-2 border-accent/40 bg-accent-dim', labelKey: 'pages.stateTrace.event.resume' },
  artifact_edit: { icon: 'edit', cls: 'text-accent-2 border-accent/40 bg-accent-dim', labelKey: 'pages.stateTrace.event.artifact_edit' },
}
const items = computed(() => props.trace || [])

function eventMeta(entry: StateTraceEntry) {
  const base = META[entry.event] || META.transition
  const mark = llmTraceMark(entry.event, entry.kind)
  if (!mark) return base
  const tone = mark.tone === 'err'
    ? 'text-err border-err/40 bg-err/10'
    : mark.tone === 'warn'
      ? 'text-warn border-warn/40 bg-warn/10'
      : mark.tone === 'accent'
        ? 'text-accent-2 border-accent/40 bg-accent-dim'
        : 'text-txt2 border-line bg-elevated'
  return { ...base, icon: mark.name, cls: tone }
}
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="border-b border-line px-4 py-2.5 text-[12px] text-txt3">
      {{ t('pages.stateTrace.header', { n: items.length }) }}
    </div>
    <div v-if="!items.length" class="flex flex-1 items-center justify-center text-[12px] text-txt3">{{ t('pages.stateTrace.empty') }}</div>
    <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto px-4 py-3">
      <ol class="relative space-y-3 border-l border-line pl-5">
        <li v-for="(e, i) in items" :key="i" class="relative">
          <span
            class="grasp-status-host absolute -left-[30px] flex h-6 w-6 items-center justify-center rounded-full border"
            :class="eventMeta(e).cls"
          >
            <Icon :name="eventMeta(e).icon" :size="16" />
          </span>
          <div class="flex items-center gap-2">
            <span class="text-[10px] font-medium uppercase tracking-wide" :class="eventMeta(e).cls.split(' ')[0]">
              {{ t(eventMeta(e).labelKey) }}
            </span>
            <code class="font-mono text-[12px] text-txt">{{ e.nodeId }}</code>
            <span
              v-if="e.event === 'enter' && (e.iteration || 0) > 1"
              class="rounded-full border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
              :title="t('pages.stateTrace.iterationTitle')"
              >{{ t('common.iterationN', { n: e.iteration }) }}</span
            >
            <span v-if="e.to" class="text-[11px] text-txt3">→ <code class="font-mono text-accent-2">{{ e.to }}</code></span>
            <span class="ml-auto text-[10px] text-txt3">{{ fmtTime(e.at) }}</span>
          </div>
          <div v-if="e.detail" class="mt-0.5 text-[11px] text-txt3">{{ e.detail }}</div>
        </li>
      </ol>
    </div>
  </div>
</template>
