<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ClarifyBootLoader from './ClarifyBootLoader.vue'

withDefaults(
  defineProps<{
    mode?: 'stage' | 'sidebar'
    showConfirm?: boolean
    /** Gateway lifecycle while inbox starting (e.g. pulling). */
    sandboxPhase?: string | null
  }>(),
  {
    mode: 'sidebar',
    showConfirm: true,
    sandboxPhase: null,
  },
)

const { t } = useI18n()
</script>

<template>
  <div
    v-if="mode === 'stage'"
    class="flex h-full min-h-0 flex-col"
    data-testid="react-connecting-stage"
    aria-busy="true"
  >
    <div
      class="flex shrink-0 gap-1 border-b border-line px-3 py-2"
      data-testid="react-connecting-stage-tabs"
      role="tablist"
    >
      <button
        type="button"
        role="tab"
        class="rounded-md bg-elevated px-2.5 py-1 text-[11px] text-txt2 transition"
        aria-selected="true"
        data-testid="react-connecting-tab-pipeline"
      >
        {{ t('pages.reactArtifactStage.pipelineTab') }}
      </button>
    </div>
    <div
      class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-5 text-center"
      data-testid="react-connecting-pipeline-skeleton"
    >
      <div class="w-full max-w-[420px] space-y-2" aria-hidden="true">
        <div class="h-28 animate-pulse rounded-md bg-elevated" />
        <div class="h-2.5 w-4/5 animate-pulse rounded bg-elevated" />
        <div class="h-2.5 w-1/2 animate-pulse rounded bg-elevated" />
      </div>
      <p class="text-[11px] text-txt3">{{ t('pages.clarify.connectingStageHint') }}</p>
    </div>
  </div>

  <div
    v-else
    class="flex h-full min-h-0 flex-col"
    data-testid="react-connecting-sidebar"
    aria-busy="true"
  >
    <div class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2.5">
      <Icon name="chat" :size="13" class="text-txt3" />
      <span class="text-[11px] text-txt3">{{ t('pages.clarify.header', { n: 0 }) }}</span>
      <span
        class="ml-auto inline-flex items-center gap-1.5 rounded-full border border-n-clarify/30 bg-n-clarify/10 px-2 py-0.5 text-[10px] text-n-clarify"
        data-testid="react-connecting-pill"
      >
        <i class="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
        {{
          sandboxPhase === 'pulling'
            ? t('pages.clarify.connectingPulling')
            : t('pages.clarify.connecting')
        }}
      </span>
    </div>
    <div class="min-h-0 flex-1 overflow-hidden">
      <ClarifyBootLoader phase="starting" :sandbox-phase="sandboxPhase" />
    </div>
    <div class="shrink-0 border-t border-line p-3">
      <textarea
        disabled
        rows="2"
        class="input min-h-[62px] w-full resize-none disabled:cursor-not-allowed disabled:bg-elevated disabled:text-txt3"
        :placeholder="t('pages.clarify.connectingInputPlaceholder')"
        data-testid="react-connecting-input"
      />
      <div class="mt-2 flex justify-end gap-2">
        <button
          v-if="showConfirm"
          type="button"
          disabled
          class="rounded-md bg-ok px-3 py-1.5 text-xs font-semibold text-white opacity-45"
          data-testid="react-connecting-confirm"
        >
          {{ t('pages.clarify.confirmFlow') }}
        </button>
        <button
          type="button"
          disabled
          class="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-white opacity-45"
          data-testid="react-connecting-send"
        >
          {{ t('pages.reviewComposer.send') }}
        </button>
      </div>
    </div>
  </div>
</template>
