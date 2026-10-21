<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import type { ConfirmFlowDiscard } from '@/lib/inbox/confirmFlowDiscard'

const props = withDefaults(
  defineProps<{
    open: boolean
    discard?: ConfirmFlowDiscard
  }>(),
  { discard: 'none' },
)

const emit = defineEmits<{
  (e: 'cancel'): void
  (e: 'confirm'): void
}>()

const { t } = useI18n()
const cancelBtn = ref<{ $el?: HTMLElement } | null>(null)

const discardText = computed(() => {
  switch (props.discard) {
    case 'edits':
      return t('pages.clarify.confirmFlowDialogDiscardEdits')
    case 'draft':
      return t('pages.clarify.confirmFlowDialogDiscardDraft')
    case 'both':
      return t('pages.clarify.confirmFlowDialogDiscardBoth')
    default:
      return ''
  }
})

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    await nextTick()
    cancelBtn.value?.$el?.focus()
  },
  { immediate: true },
)

function onCancelKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter') return
  event.preventDefault()
  emit('cancel')
}
</script>

<template>
  <AppModal
    :open="open"
    :title="t('pages.clarify.confirmFlowDialogTitle')"
    :close-on-esc="true"
    :width="440"
    @close="emit('cancel')"
  >
    <p data-testid="confirm-flow-dialog-body">{{ t('pages.clarify.confirmFlowDialogBody') }}</p>
    <p v-if="discardText" data-testid="confirm-flow-dialog-discard" class="mt-3 text-txt2">{{ discardText }}</p>
    <template #footer>
      <AppButton
        ref="cancelBtn"
        variant="ghost"
        data-testid="confirm-flow-dialog-cancel"
        @click="emit('cancel')"
        @keydown="onCancelKeydown"
      >
        {{ t('common.buttons.cancel') }}
      </AppButton>
      <AppButton variant="primary" data-testid="confirm-flow-dialog-accept" @click="emit('confirm')">
        {{ t('pages.clarify.confirmFlowDialogConfirm') }}
      </AppButton>
    </template>
  </AppModal>
</template>
