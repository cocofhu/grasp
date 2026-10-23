<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SandboxEnvEntry } from '@/lib/shared/types'

const props = defineProps<{ entries: SandboxEnvEntry[] }>()
const { t } = useI18n()
const items = computed(() => props.entries || [])
</script>

<template>
  <div class="flex h-full flex-col" data-testid="run-sandbox-env-panel">
    <div class="border-b border-line px-4 py-2.5 text-[12px] text-txt3">
      {{ t('pages.runDetail.sandboxEnv.hint') }}
    </div>
    <div
      v-if="!items.length"
      class="flex flex-1 items-center justify-center text-[12px] text-txt3"
      data-testid="run-sandbox-env-empty"
    >
      {{ t('pages.runDetail.sandboxEnv.empty') }}
    </div>
    <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto p-3">
      <div class="rounded-lg overflow-hidden border border-line">
        <div
          class="hidden gap-2 border-b border-line bg-elevated/55 px-3 py-2 text-[11px] font-semibold uppercase tracking-wider text-txt3 sm:grid sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1.6fr)_72px]"
        >
          <span>{{ t('pages.runDetail.sandboxEnv.key') }}</span>
          <span>{{ t('pages.runDetail.sandboxEnv.value') }}</span>
          <span>{{ t('pages.runDetail.sandboxEnv.secret') }}</span>
        </div>
        <div
          v-for="e in items"
          :key="e.key"
          class="grid grid-cols-1 gap-1 border-b border-line px-3 py-2 last:border-b-0 sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1.6fr)_72px] sm:items-center"
          data-testid="run-sandbox-env-row"
        >
          <code class="font-mono text-[12px] text-txt">{{ e.key }}</code>
          <code class="break-all font-mono text-[12px] text-txt2">{{ e.value }}</code>
          <span
            class="chip w-fit justify-center text-[11px]"
            :class="e.secret ? 'border-warn/40 text-warn' : 'text-txt3'"
          >
            {{ e.secret ? t('pages.runDetail.sandboxEnv.secretBadge') : t('pages.runDetail.sandboxEnv.plain') }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
