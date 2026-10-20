<script setup lang="ts">
import { computed, ref } from 'vue'
import { useGateApprovalCtx } from './gateApprovalContext'
import Icon from '../../ui/Icon.vue'
import ParagraphInput from '../../ui/ParagraphInput.vue'
import ComposerShell from '../ComposerShell.vue'
import ComposerPrimaryAction from '../ComposerPrimaryAction.vue'
import { actionIcon } from './gateApprovalActions'

/**
 * Cold gate exit (no live ReAct session): the same ComposerShell + single
 * primary as ReviewComposer / ClarifyChat. With `formKey`, that gate.form field
 * (e.g.「评审意见」) is the composer input; without it the shell is footer-only,
 * like ReviewComposer's cold session. Semantics are unchanged: the button calls
 * onSidebarAction(id) and the field still lands in buildFormPayload().
 */
const props = defineProps<{
  /** mobile fill uses full-width buttons; others vary by isMobile */
  layout: 'mobile' | 'content-fit' | 'desktop'
  /** gate.form key rendered as the composer input (desktop body). */
  formKey?: string | null
  /** Footer hint (cold help line). */
  hint?: string
}>()

const { s } = useGateApprovalCtx()
const t = s.t
const paragraphRef = ref<{ pickFiles?: () => void } | null>(null)

const field = computed(() =>
  props.formKey ? (s.gate.form ?? []).find((f) => f.key === props.formKey) ?? null : null,
)
const fullWidth = computed(() => props.layout === 'mobile' || s.isMobile)
const inputLocked = computed(() => s.actionSubmitting || s.resolved != null)

function pending(id: string): boolean {
  return s.actionSubmitting && s.resolved === id
}
</script>

<template>
  <ComposerShell
    :show-chrome="!!field"
    :show-footer="!field || !!hint"
    box-test-id="gate-cold-composer"
    footer-test-id="gate-cold-footer"
    data-gate-cold-actions
  >
    <template v-if="field" #input>
      <ParagraphInput
        ref="paragraphRef"
        class="chat-input"
        v-model:text="s.formText[field.key]"
        v-model:images="s.formImages[field.key]"
        embedded
        :text-only="s.isMobile"
        :disabled="inputLocked"
        :placeholder="t('pages.gateApproval.formPlaceholder', { label: field.label })"
        :aria-label="field.label"
        data-testid="gate-cold-input"
      />
    </template>
    <template v-if="field" #toolbar-start>
      <button
        v-if="!s.isMobile"
        type="button"
        class="chat-action-button"
        data-testid="paragraph-input-attach"
        :disabled="inputLocked"
        :title="t('common.paragraphInput.addImage')"
        :aria-label="t('common.paragraphInput.addImage')"
        @click="paragraphRef?.pickFiles?.()"
      >
        <Icon name="plus" :size="18" />
      </button>
    </template>
    <template v-if="field" #toolbar-end>
      <ComposerPrimaryAction
        v-for="a in s.footerActions"
        :key="a.id"
        kind="confirm"
        :icon="pending(a.id) ? 'spinner' : actionIcon(a.id)"
        :label="pending(a.id) ? s.actionPendingLabel(a.id) : a.label"
        :title="s.actionButtonTitle(a.id) || undefined"
        :busy="pending(a.id)"
        :disabled="s.isActionDisabled(a.id) || s.reactSending"
        testid="review-composer-pass"
        @click="s.onSidebarAction(a.id)"
      />
    </template>
    <template v-if="hint" #hint>
      <p class="m-0 min-w-0 leading-snug [overflow-wrap:anywhere]" data-testid="gate-cold-help">{{ hint }}</p>
    </template>
    <template v-if="!field" #footer>
      <ComposerPrimaryAction
        v-for="a in s.footerActions"
        :key="a.id"
        kind="confirm"
        :class="fullWidth ? 'min-h-9 flex-1 justify-center' : ''"
        :icon="pending(a.id) ? 'spinner' : actionIcon(a.id)"
        :label="pending(a.id) ? s.actionPendingLabel(a.id) : a.label"
        :title="s.actionButtonTitle(a.id) || undefined"
        :busy="pending(a.id)"
        :disabled="s.isActionDisabled(a.id) || s.reactSending"
        testid="review-composer-pass"
        @click="s.onSidebarAction(a.id)"
      />
    </template>
  </ComposerShell>
</template>
