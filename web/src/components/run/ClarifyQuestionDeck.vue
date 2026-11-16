<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import ClarifyDemoFrame from './ClarifyDemoFrame.vue'
import type { QuestionDeck } from '@/lib/inbox/clarifyAnswers'
import type { ReactQuestion } from '@/lib/shared/types'

const props = defineProps<{ deck: QuestionDeck; busy?: boolean }>()
const emit = defineEmits<{ submit: []; enlarge: [question: ReactQuestion, optionId: string] }>()
const { t } = useI18n()
const {
  questions, other, step, curQuestion, isFirstCard, isLastCard, someAnswered, hasRecommended,
  isSelected, toggle, isOtherSelected, toggleOther, answered, applyRecommended, prevCard, nextCard, selectedDemo,
} = props.deck

function submitRecommended() {
  applyRecommended()
  emit('submit')
}
</script>

<template>
  <div v-if="curQuestion" class="clarify-request relative min-w-0" data-testid="clarify-question-deck">
    <div v-if="questions.length > 1" class="mb-2.5 flex items-center justify-between">
      <div class="flex items-center gap-1">
        <span
          v-for="(q, qi) in questions"
          :key="qi"
          class="h-1.5 rounded-full transition-all"
          :class="qi === step ? 'w-4 bg-accent' : answered(q) ? 'w-1.5 bg-accent/40' : 'w-1.5 bg-line-strong'"
        />
      </div>
      <span class="text-[11px] text-txt2">{{ step + 1 }} / {{ questions.length }}</span>
    </div>

    <Transition name="deck" mode="out-in">
      <div :key="step">
        <div class="clarify-question-heading mb-3 text-[13px] font-medium text-txt">
          <Icon name="chat" :size="16" class="shrink-0 text-txt2" />
          <span
            class="min-w-0 break-words leading-relaxed [overflow-wrap:anywhere]"
            data-testid="clarify-question-prompt"
          >{{ curQuestion.prompt }}</span>
          <span class="clarify-question-kind text-[11px] font-normal leading-5 text-txt2">{{ curQuestion.allowMultiple ? t('pages.clarify.multiple') : t('pages.clarify.single') }}</span>
        </div>
        <div class="space-y-1.5">
          <button
            v-for="o in curQuestion.options"
            :key="o.id"
            type="button"
            class="chat-choice-option flex w-full min-w-0 items-center gap-2 rounded-lg border border-line bg-surface px-2.5 py-2 text-left text-[12px] text-txt2 transition-colors"
            :aria-pressed="isSelected(curQuestion.id, o.id)"
            data-testid="clarify-option-btn"
            @click="toggle(curQuestion, o.id)"
          >
            <span
              class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
              :class="[
                curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
                isSelected(curQuestion.id, o.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
              ]"
            >
              <Icon v-if="isSelected(curQuestion.id, o.id)" name="check" :size="10" />
            </span>
            <span
              class="min-w-0 flex-1 break-words [overflow-wrap:anywhere]"
              data-testid="clarify-option-label"
            >{{ o.label }}</span>
            <span
              v-if="o.recommended"
              class="chat-choice-recommendation ml-auto shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium"
            >{{ t('pages.clarify.recommended') }}</span>
          </button>
        </div>
        <div
          class="chat-choice-other mt-1.5 flex w-full items-center gap-2 rounded-lg border border-line bg-surface px-2.5 py-2 text-left text-[12px] text-txt2 transition-colors"
          :class="{ 'is-selected': isOtherSelected(curQuestion.id) }"
          data-testid="clarify-other-row"
        >
          <button
            type="button"
            class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
            :class="[
              curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
              isOtherSelected(curQuestion.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
            ]"
            data-testid="clarify-other-checkbox"
            :aria-pressed="isOtherSelected(curQuestion.id)"
            :aria-label="t('pages.clarify.otherPlaceholder')"
            @click="toggleOther(curQuestion)"
          >
            <Icon v-if="isOtherSelected(curQuestion.id)" name="check" :size="10" />
          </button>
          <input
            v-model="other[curQuestion.id]"
            type="text"
            class="input min-h-0 min-w-0 flex-1 border-0 bg-transparent p-0 text-[12px] shadow-none focus:border-transparent focus:ring-0 [overflow-wrap:anywhere]"
            :placeholder="t('pages.clarify.otherPlaceholder')"
            data-testid="clarify-other-input"
          />
        </div>
        <div v-if="selectedDemo(curQuestion)" class="mt-2.5" data-testid="clarify-inline-demos">
          <ClarifyDemoFrame
            :label="selectedDemo(curQuestion)!.label"
            :html="selectedDemo(curQuestion)!.demoHtml!"
            highlighted
            @enlarge="emit('enlarge', curQuestion, selectedDemo(curQuestion)!.id)"
          />
        </div>
      </div>
    </Transition>

    <div class="mt-3 flex items-center justify-between">
      <button
        v-if="!isFirstCard"
        class="chat-request-back inline-flex items-center gap-0.5 rounded-md px-2 py-1 text-[12px] text-txt2 hover:text-txt"
        @click="prevCard"
      >
        <Icon name="arrow-left" :size="13" /> {{ t('pages.clarify.prev') }}
      </button>
      <span v-else />

      <button v-if="!isLastCard" class="chat-action-secondary" @click="nextCard">
        {{ t('pages.clarify.next') }} <Icon name="chevron-right" :size="13" />
      </button>
      <div v-else class="flex flex-wrap items-center gap-2">
        <button
          v-if="hasRecommended"
          class="chat-action-secondary"
          :disabled="busy"
          :title="t('pages.clarify.applyRecommendedTitle')"
          data-testid="clarify-apply-recommended"
          @click="submitRecommended"
        >
          <Icon name="check" :size="14" /> {{ t('pages.clarify.applyRecommended') }}
        </button>
        <button
          class="chat-action-confirm"
          :disabled="!someAnswered || busy"
          data-testid="clarify-submit-choices"
          @click="emit('submit')"
        >
          <Icon name="chat-send" :size="16" /> {{ t('pages.clarify.submitChoices') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.clarify-question-heading {
  display: grid;
  grid-template-columns: 16px minmax(0, 1fr);
  align-items: start;
  column-gap: 8px;
  row-gap: 6px;
}
.clarify-question-heading > .app-icon { margin-top: 3px; }
.clarify-question-kind { grid-column: 2; }
@media (max-width: 767px) {
  [data-testid="clarify-option-btn"],
  [data-testid="clarify-other-row"] { min-height: 44px; }
}

/* Card-deck step transition: subtle slide + fade as one card advances. */
.deck-enter-active,
.deck-leave-active {
  transition: opacity 0.16s ease, transform 0.16s ease;
}
.deck-enter-from {
  opacity: 0;
  transform: translateX(8px);
}
.deck-leave-to {
  opacity: 0;
  transform: translateX(-8px);
}
@media (prefers-reduced-motion: reduce) {
  .deck-enter-active, .deck-leave-active { transition: none; }
  .deck-enter-from, .deck-leave-to { transform: none; }
}
</style>
