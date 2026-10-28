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
import type { AgentTool, ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import type { GateReactTurn } from './gateApproval/gateApprovalContext'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { imgSrc } from '@/lib/shared/compositeText'
import { attachmentDisplayName } from '@/lib/shared/attachments'
import AnnotationChip from './AnnotationChip.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import ThinkingBusyCue from './ThinkingBusyCue.vue'
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
    /** Exact run owning any artifact links shown by tools. */
    runId?: string
    /** Accepted user text, visible before the agent frame arrives. */
    userText?: string
    userImages?: ClarifyImage[]
    userAnnotations?: ReactAnnotation[]
    /** Each request owns its response; settled turns stay above queued requests. */
    turns?: GateReactTurn[]
  }>(),
  {
    thinking: false,
    streamText: '',
    streamThought: '',
    interrupted: false,
    completedAt: null,
    streamTools: () => [],
    userText: '',
    userImages: () => [],
    userAnnotations: () => [],
  },
)

const { t, locale } = useI18n()
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

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
    !!props.userText || props.userImages.length > 0 || props.userAnnotations.length > 0 ||
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
  <div v-if="turns?.length" class="space-y-3" data-testid="gate-react-transcript">
    <GateReactStreamPanel
      v-for="turn in turns"
      :key="turn.key"
      :run-id="runId"
      :user-text="turn.historySynced ? '' : turn.text"
      :user-images="turn.historySynced ? [] : turn.images"
      :user-annotations="turn.historySynced ? [] : turn.annotations"
      :thinking="turn.thinking"
      :stream-text="turn.streamText"
      :stream-thought="turn.streamThought"
      :stream-tools="turn.streamTools"
      :interrupted="turn.interrupted"
      :completed-at="turn.completedAt"
    />
  </div>
  <div
    v-else-if="showPanel"
    class="min-w-0 space-y-3 py-2"
    data-testid="gate-react-stream"
  >
    <div v-if="userText || userImages.length || userAnnotations.length" class="flex justify-end" data-testid="gate-live-human">
      <div class="max-w-[88%] space-y-2 rounded-lg bg-elevated px-3 py-2 text-[13px] text-txt">
        <div v-if="userText" class="whitespace-pre-wrap break-words">{{ userText }}</div>
        <div v-if="userImages.length" class="flex flex-wrap gap-1.5">
          <ChatImageThumb
            v-for="(im, index) in userImages"
            :key="index"
            :src="imgSrc(im)"
            :label="attachmentDisplayName(im, index)"
            size="sm"
            test-id="gate-live-human-image"
            @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, index))"
          />
        </div>
        <div v-if="userAnnotations.length" class="flex flex-wrap gap-1.5">
          <AnnotationChip v-for="(ann, index) in userAnnotations" :key="index" :ann="ann" />
        </div>
      </div>
    </div>
    <div
      v-if="streaming && !hasMessage && !hasThought"
      class="chat-meta inline-flex items-center gap-2"
      data-testid="gate-busy-placeholder"
    >
      <ThinkingBusyCue :label="t('pages.clarify.thinkingBusy')" />
    </div>
    <div
      v-else-if="streaming && hasThought && !hasMessage"
      class="chat-meta inline-flex items-center gap-2"
      data-testid="gate-busy-status"
    >
      <ThinkingBusyCue :label="t('pages.clarify.thinkingBusy')" />
    </div>
    <div
      v-else-if="streaming && hasMessage"
      class="chat-meta"
      data-testid="gate-busy-status"
    >
      <span class="text-accent-2">{{ t('pages.clarify.outputting') }}</span>
    </div>

    <details
      v-if="hasThought"
      class="chat-meta min-w-0"
      data-testid="gate-react-thought"
      :open="thoughtOpen"
      @toggle="onThoughtToggle"
    >
      <summary
        class="flex cursor-pointer select-none items-center gap-2 py-1.5 hover:text-accent-2"
        data-testid="gate-thought-summary"
      >
        <ThoughtSummaryStatus
          :busy="streaming"
          :completed="showCompleted"
          :interrupted="!!interrupted && !streaming"
        />
      </summary>
      <div class="ml-1.5 whitespace-pre-wrap break-words border-l border-line/70 pl-3 pb-2 pt-1.5 text-[12px] leading-6 [overflow-wrap:anywhere]">
        {{ revealedThought }}
      </div>
    </details>

    <AgentToolGroup v-if="streamTools.length" :tools="streamTools" :busy="streaming" :run-id="runId" />

    <div v-if="hasMessage" class="chat-prose md" data-testid="gate-react-message">
      <StreamMarkdown :blocks="messageBlocks" /><span
        v-if="streaming"
        class="gate-stream-caret"
        data-testid="gate-stream-caret"
        aria-hidden="true"
      />
      <span v-if="interrupted" class="ml-1.5 text-[12px] text-warn">interrupted</span>
    </div>

    <div
      v-if="showCompleted"
      class="chat-meta flex items-center justify-between gap-2 pt-1.5"
      data-testid="gate-turn-completed"
    >
      <span class="text-txt2">{{ t('pages.clarify.turnCompleted') }}</span>
      <span>{{ locale && completedAt ? relTime(completedAt) : '' }}</span>
    </div>
  </div>
  <ChatImagePreviewModal
    v-if="imagePreview"
    :open="!!imagePreview"
    :src="imagePreview.src"
    :label="imagePreview.label"
    @close="closeChatImagePreview"
  />
</template>

<style scoped>
.gate-stream-caret {
  display: inline-block;
  width: 2px;
  height: 1.1em;
  margin-left: 4px;
  border-radius: 1px;
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
  .gate-stream-caret {
    animation: none !important;
  }
}
</style>
