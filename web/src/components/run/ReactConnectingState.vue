<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ClarifyBootLoader from './ClarifyBootLoader.vue'
import ComposerShell from './ComposerShell.vue'
import ComposerPrimaryAction from './ComposerPrimaryAction.vue'

const props = withDefaults(
  defineProps<{
    mode?: 'stage' | 'sidebar'
    /** Gateway lifecycle while inbox starting (e.g. pulling). */
    sandboxPhase?: string | null
    /** Boot record key (runId:nodeId:iteration) driving the checklist. */
    bootKey?: string | null
    /** Sandbox drawer already titles the panel; don't repeat the old home tab. */
    hideHomeTab?: boolean
  }>(),
  {
    mode: 'sidebar',
    sandboxPhase: null,
    bootKey: null,
    hideHomeTab: false,
  },
)

const { t } = useI18n()

const isPulling = computed(() => (props.sandboxPhase || '').trim().toLowerCase() === 'pulling')

const stageTitle = computed(() =>
  isPulling.value
    ? t('pages.clarify.connectingStagePullingTitle')
    : t('pages.clarify.connectingStageTitle'),
)

/** Prefer JS gate so unit tests can assert spinner stops under matchMedia reduce. */
const reduceMotion = ref(false)
onMounted(() => {
  reduceMotion.value = typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
})
</script>

<template>
  <div
    v-if="mode === 'stage'"
    class="flex h-full min-h-0 flex-col"
    data-testid="react-connecting-stage"
    aria-busy="true"
  >
    <div
      v-if="!hideHomeTab"
      class="flex shrink-0 gap-1 border-b border-line px-3 py-2"
      data-testid="react-connecting-stage-tabs"
      role="tablist"
    >
      <button
        type="button"
        role="tab"
        class="rounded-md bg-elevated px-2.5 py-1 text-[11px] text-txt2 transition"
        aria-selected="true"
        data-testid="react-connecting-tab-workflow"
      >
        {{ t('pages.reactArtifactStage.workflowTab') }}
      </button>
    </div>
    <div
      class="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 py-7 text-center"
      data-testid="react-connecting-stage-body"
    >
      <Icon
        name="spinner"
        :size="28"
        class="text-accent"
        :class="reduceMotion ? '' : 'animate-spin'"
        aria-hidden="true"
        data-testid="react-connecting-stage-spinner"
      />
      <p
        class="mt-1.5 text-[13px] text-txt2"
        data-testid="react-connecting-stage-title"
      >
        {{ stageTitle }}
      </p>
      <p
        class="m-0 max-w-[260px] text-[12px] leading-[1.55] text-txt3"
        role="status"
        data-testid="react-connecting-stage-hint"
      >
        {{ t('pages.clarify.connectingStageHint') }}
      </p>
    </div>
  </div>

  <!--
    Same chat skeleton as ClarifyChat (centered transcript column + ComposerShell),
    so starting → connecting → clarify/ReAct never swaps layout. No meta row above
    the transcript: the boot loader is the only connecting status (it pins the
    pull-image step when sandboxPhase is pulling); the composer is only disabled.
  -->
  <div
    v-else
    class="chat-thread flex h-full min-h-0 min-w-0 flex-1 flex-col"
    data-testid="react-connecting-sidebar"
    aria-busy="true"
  >
    <div class="react-connecting-scroller scroll-area min-h-0 flex-1 overflow-y-auto" data-testid="react-connecting-scroller">
      <div class="react-connecting-transcript">
        <div class="react-connecting-turn">
          <ClarifyBootLoader phase="starting" variant="inline" :sandbox-phase="sandboxPhase" :boot-key="bootKey" />
        </div>
      </div>
    </div>
    <div class="react-connecting-actions shrink-0 p-3" data-testid="react-connecting-actions">
      <ComposerShell wrap-toolbar :show-footer="false" box-test-id="react-connecting-input-row">
        <template #input>
          <textarea
            disabled
            rows="1"
            class="chat-input min-h-[40px] w-full min-w-0 resize-none overflow-y-hidden border-0 bg-transparent p-0 shadow-none outline-none focus:ring-0 disabled:cursor-not-allowed"
            :placeholder="t('pages.clarify.connectingInputPlaceholder')"
            data-testid="react-connecting-input"
          />
        </template>
        <template #toolbar-start>
          <button
            type="button"
            disabled
            class="chat-action-button react-connecting-attach"
            :aria-label="t('pages.clarify.addImage')"
            data-testid="react-connecting-attach"
          >
            <Icon name="plus" :size="18" />
          </button>
        </template>
        <template #toolbar-end>
          <!-- State #1 (button-states.md): one disabled 发送; ClarifyChat morphs it on hand-off. -->
          <ComposerPrimaryAction
            kind="send"
            disabled
            :label="t('pages.reviewComposer.send')"
            testid="react-connecting-send"
            handoff-out
          />
        </template>
      </ComposerShell>
    </div>
  </div>
</template>

<style scoped>
/* Mirrors ClarifyChat's scroller / transcript / hot-actions geometry. */
.react-connecting-scroller { padding: 24px 24px 56px 44px; scrollbar-gutter: stable; }
.react-connecting-transcript { width: 100%; max-width: 760px; margin-inline: auto; }
.react-connecting-turn { margin-top: 28px; }
.react-connecting-actions { width: 100%; max-width: 800px; margin-inline: auto; }
.react-connecting-attach { border-color: transparent; background: transparent; }
@media (max-width: 767px) {
  .react-connecting-scroller { padding: 18px 16px 56px 36px; }
}
</style>
