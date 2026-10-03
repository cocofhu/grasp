<script setup lang="ts">
/**
 * Shared four-phase busy stream for GateApproval / ReviewComposer(gate):
 * placeholder → collapsible thought → outputting (shimmer+caret) → restrained done.
 * Smooth catch-up reveal + ThoughtSummaryStatus aligned with ClarifyChat / Demo.
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { relTime } from '@/lib/shared/format'
import { createStreamTextReveal } from '@/lib/run/streamTextReveal'
import type { AgentTool } from '@/lib/shared/types'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import AgentToolGroup from './AgentToolGroup.vue'
import StreamMarkdown from './StreamMarkdown.vue'
import { renderMarkdownBlocks, type MarkdownBlockCache } from '@/lib/shared/markdown'

const props = withDefaults(
  defineProps<{
    thinking?: boolean
    streamText?: string
    streamThought?: string
    interrupted?: boolean
    /** ISO timestamp when turn completed normally (not interrupted/error). */
    completedAt?: string | null
    /** Tool calls of the current turn (name + status). */
    streamTools?: AgentTool[]
  }>(),
  {
    thinking: false,
    streamText: '',
    streamThought: '',
    interrupted: false,
    completedAt: null,
    streamTools: () => [],
  },
)

const { t, locale } = useI18n()

const thoughtOpenOverride = ref<boolean | undefined>(undefined)
/** Revealed display texts (catch-up); authority stays on props. */
const revealedThought = ref('')
const revealedMessage = ref('')

const syncReveal = Boolean(import.meta.env.VITEST)
const thoughtReveal = createStreamTextReveal({
  sync: syncReveal,
  onReveal: (text) => {
    revealedThought.value = text
  },
})
const messageReveal = createStreamTextReveal({
  sync: syncReveal,
  onReveal: (text) => {
    revealedMessage.value = text
  },
})

watch(
  () => [props.streamText, props.streamThought, props.thinking, props.completedAt] as const,
  () => {
    // Reset manual override when rails / phase change so Demo defaults apply.
    thoughtOpenOverride.value = undefined
  },
)

watch(
  () => props.streamThought ?? '',
  (text) => {
    thoughtReveal.setTarget(text)
  },
  { immediate: true },
)

watch(
  () => props.streamText ?? '',
  (text) => {
    messageReveal.setTarget(text)
  },
  { immediate: true },
)

/** End of turn: flush reveal so UI matches authority (done / interrupt). */
watch(
  () => [props.thinking, props.completedAt, props.interrupted] as const,
  ([thinking, completedAt, interrupted]) => {
    if (!thinking || completedAt || interrupted) {
      thoughtReveal.flush()
      messageReveal.flush()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  thoughtReveal.reset()
  messageReveal.reset()
})

const blockCache: MarkdownBlockCache = new Map()
const messageBlocks = computed(() => renderMarkdownBlocks(revealedMessage.value, blockCache))

const hasThought = computed(() => !!props.streamThought)
const hasMessage = computed(() => !!props.streamText)
const streaming = computed(() => !!props.thinking && !props.completedAt)
const showCompleted = computed(
  () =>
    !!props.completedAt &&
    !props.interrupted &&
    !props.thinking &&
    (hasThought.value || hasMessage.value),
)
const showPanel = computed(
  () =>
    props.thinking ||
    showCompleted.value ||
    (props.interrupted && (hasThought.value || hasMessage.value)),
)

/** Default open while thought-only streaming; collapse once message / done. */
const thoughtOpen = computed(() => {
  if (thoughtOpenOverride.value !== undefined) return thoughtOpenOverride.value
  return !!(streaming.value && hasThought.value && !hasMessage.value)
})

function onThoughtToggle(e: Event) {
  const el = e.target as HTMLDetailsElement
  thoughtOpenOverride.value = el.open
}
</script>

<template>
  <div
    v-if="showPanel"
    class="mt-2 max-h-40 space-y-1.5 overflow-y-auto rounded border border-line bg-surface px-2 py-1.5 text-[12px] text-txt2"
    data-testid="gate-react-stream"
  >
    <div
      v-if="streaming && !hasMessage && !hasThought"
      class="inline-flex items-center gap-2 text-txt3"
      data-testid="gate-busy-placeholder"
    >
      <span class="typing-dots" aria-hidden="true"><i /><i /><i /></span>
      <span>{{ t('pages.clarify.thinkingBusy') }}</span>
    </div>
    <div
      v-else-if="streaming && hasThought && !hasMessage"
      class="text-txt3"
      data-testid="gate-busy-status"
    >
      {{ t('pages.clarify.thinkingBusy') }}
    </div>
    <div
      v-else-if="streaming && hasMessage"
      class="gate-stream-outputting"
      data-testid="gate-busy-status"
    >
      {{ t('pages.clarify.outputting') }}
    </div>

    <details
      v-if="hasThought"
      class="rounded border border-line bg-base/60 text-[11.5px] text-txt3"
      data-testid="gate-react-thought"
      :open="thoughtOpen"
      @toggle="onThoughtToggle"
    >
      <summary
        class="flex cursor-pointer select-none items-center gap-1 px-2 py-1 hover:text-txt2"
        data-testid="gate-thought-summary"
      >
        <ThoughtSummaryStatus
          :busy="streaming"
          :completed="showCompleted"
          :interrupted="!!interrupted && !streaming"
        />
      </summary>
      <div class="whitespace-pre-wrap break-words border-t border-dashed border-line px-2 pb-1.5 pt-1 font-mono leading-5 [overflow-wrap:anywhere]">
        {{ revealedThought }}
      </div>
    </details>

    <AgentToolGroup v-if="streamTools.length" :tools="streamTools" :busy="streaming" />

    <div v-if="hasMessage" class="md" data-testid="gate-react-message">
      <StreamMarkdown :blocks="messageBlocks" /><span
        v-if="streaming"
        class="gate-stream-caret"
        data-testid="gate-stream-caret"
        aria-hidden="true"
      />
      <span v-if="interrupted" class="ml-1 text-[10px] text-warn">interrupted</span>
    </div>

    <div
      v-if="showCompleted"
      class="flex items-center justify-between gap-2 border-t border-line pt-1.5 text-[11px] text-txt3"
      data-testid="gate-turn-completed"
    >
      <span class="text-txt2">{{ t('pages.clarify.turnCompleted') }}</span>
      <span>{{ locale && completedAt ? relTime(completedAt) : '' }}</span>
    </div>
  </div>
</template>

<style scoped>
.typing-dots {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.typing-dots i {
  width: 5px;
  height: 5px;
  border-radius: 9999px;
  background: #22d3ee;
  animation: typing-bounce 1.2s infinite ease-in-out both;
}
.typing-dots i:nth-child(2) {
  animation-delay: 0.16s;
}
.typing-dots i:nth-child(3) {
  animation-delay: 0.32s;
}
@keyframes typing-bounce {
  0%,
  70%,
  100% {
    transform: translateY(0);
    opacity: 0.35;
  }
  35% {
    transform: translateY(-4px);
    opacity: 1;
  }
}
.gate-stream-outputting {
  color: rgb(var(--c-accent-2));
  background: var(--grad-logo);
  background-size: var(--grad-logo-size);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  animation: shimmer 3.5s ease-in-out infinite;
}
.gate-stream-caret {
  display: inline-block;
  width: 7px;
  height: 1em;
  margin-left: 2px;
  vertical-align: text-bottom;
  background: rgb(var(--c-accent));
  animation: gate-caret-blink 0.9s step-end infinite;
}
@keyframes gate-caret-blink {
  50% {
    opacity: 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .typing-dots i,
  .gate-stream-outputting,
  .gate-stream-caret {
    animation: none !important;
  }
  .gate-stream-outputting {
    color: rgb(var(--c-accent-2));
    background: none;
    -webkit-background-clip: unset;
    background-clip: unset;
    -webkit-text-fill-color: unset;
  }
}
</style>
