<script setup lang="ts">
/**
 * Read-only digest of an Agent's capabilities: interaction, review, outlets,
 * tools, reads and writes. Shared by the template picker and the canvas.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentCapabilities } from '@/lib/api/apiTypes'
import { summarizeCapabilities } from '@/lib/workflow/agentCapabilities'

const props = withDefaults(defineProps<{ caps?: AgentCapabilities | null; compact?: boolean }>(), {
  caps: null,
  compact: false,
})

const { t } = useI18n()
const summary = computed(() => summarizeCapabilities(props.caps))
</script>

<template>
  <div class="text-[12px] leading-6 text-txt2" data-testid="capability-summary">
    <p v-if="!summary" class="text-warn" data-testid="capability-summary-missing">
      {{ t('nodes.capabilities.errors.missing') }}
    </p>
    <template v-else>
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="chip" data-testid="capability-summary-interaction">
          {{ t(`nodes.capabilities.interaction.${summary.interaction}`) }}
        </span>
        <span v-if="summary.review" class="chip" data-testid="capability-summary-review">
          {{ t('nodes.capabilities.review') }}
        </span>
        <span v-if="summary.gated" class="chip border-ok/40 text-ok" data-testid="capability-summary-gated">
          {{ t('nodes.capabilities.gated') }}
        </span>
        <span v-for="tool in summary.tools" :key="tool" class="chip text-txt3" :title="t(`nodes.capabilities.tools.${tool}.desc`)">
          {{ t(`nodes.capabilities.tools.${tool}.label`) }}
        </span>
      </div>
      <ul v-if="!compact && summary.writes.length" class="mt-2 space-y-0.5" data-testid="capability-summary-writes">
        <li v-for="w in summary.writes" :key="w.name" class="flex items-center gap-2">
          <span class="text-txt">{{ t(`nodes.schemas.${w.name}.label`) }}</span>
          <span class="font-mono text-[11px] text-txt3">{{ w.artifactName }}</span>
          <span class="text-[11px]" :class="w.required ? 'text-accent-2' : 'text-txt3'">
            {{ w.required ? t('nodes.capabilities.required') : t('nodes.capabilities.optional') }}
          </span>
        </li>
      </ul>
    </template>
  </div>
</template>
