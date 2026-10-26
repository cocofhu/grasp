<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { bindConfirmFlowWindow } from '@/lib/inbox/confirmFlowWindow'
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
  /**
   * Confirm window has left the document.
   * Fired from the modal leave transition's after-leave, not when open turns off.
   * Unmount releases immediately because the page is tearing the window down.
   */
  (e: 'visual-close'): void
}>()

/** Disk, stroke, and label finish around 490ms; keep the settled label readable. */
const DONE_HOLD_MS = 880

const { t } = useI18n()
const cancelBtn = ref<{ $el?: HTMLElement } | null>(null)
const done = ref(false)
const shown = ref(false)
/** Set on confirm so a later hide — timer, early dismiss, or a following cancel — can notify. */
let closeAfterDone = false
/** Confirm committed and the overlay has not finished leaving yet. */
let notifyWhenLeft = false
let hideTimer: ReturnType<typeof setTimeout> | undefined

// Parents must wait until this window is gone, not until `open` flips false.
bindConfirmFlowWindow(shown)

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

function clearHideTimer() {
  if (hideTimer != null) {
    clearTimeout(hideTimer)
    hideTimer = undefined
  }
}

/** Hide the window. The sidebar hears about it only after the overlay has left. */
function finishShownWindow() {
  clearHideTimer()
  if (shown.value && closeAfterDone) notifyWhenLeft = true
  shown.value = false
  done.value = false
  closeAfterDone = false
}

function onModalAfterLeave() {
  if (!notifyWhenLeft) return
  notifyWhenLeft = false
  emit('visual-close')
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      clearHideTimer()
      done.value = false
      shown.value = true
      return
    }
    // Cancel, close, Escape, and backdrop never enter the done frame.
    // A confirm window that was reopened and then cancelled still has to leave.
    if (!done.value) {
      if (shown.value && closeAfterDone) finishShownWindow()
      else shown.value = false
      return
    }
    // The parent closes as soon as confirm commits. Keep this window at the
    // same size until the in-dialog stroke finishes. Do not play ConfirmFlowOverlay.
    clearHideTimer()
    hideTimer = setTimeout(() => {
      hideTimer = undefined
      if (!props.open) finishShownWindow()
    }, DONE_HOLD_MS)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  clearHideTimer()
  // Navigation tears the window down with the page, so the fade may not finish.
  const notify = notifyWhenLeft || (shown.value && closeAfterDone)
  notifyWhenLeft = false
  closeAfterDone = false
  if (notify) emit('visual-close')
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
  onDismiss()
}

function onDismiss() {
  if (done.value) finishShownWindow()
  emit('cancel')
}

function onConfirm() {
  if (done.value) return
  closeAfterDone = true
  done.value = true
  emit('confirm')
}
</script>

<template>
  <AppModal
    :open="shown"
    :title="t('pages.clarify.confirmFlowDialogTitle')"
    :close-on-esc="true"
    :width="440"
    :dim-content="done"
    @close="onDismiss"
    @after-leave="onModalAfterLeave"
  >
    <p data-testid="confirm-flow-dialog-body">{{ t('pages.clarify.confirmFlowDialogBody') }}</p>
    <p v-if="discardText" data-testid="confirm-flow-dialog-discard" class="mt-3 text-txt2">{{ discardText }}</p>
    <template #footer>
      <AppButton
        ref="cancelBtn"
        variant="ghost"
        icon="close"
        data-testid="confirm-flow-dialog-cancel"
        @click="onDismiss"
        @keydown="onCancelKeydown"
      >
        {{ t('common.buttons.cancel') }}
      </AppButton>
      <AppButton variant="primary" icon="check" data-testid="confirm-flow-dialog-accept" @click="onConfirm">
        {{ t('pages.clarify.confirmFlowDialogConfirm') }}
      </AppButton>
    </template>
    <template #cover>
      <div v-if="done" class="confirm-flow-done" data-testid="confirm-flow-dialog-done" role="status">
        <div class="confirm-flow-ring" aria-hidden="true">
          <div class="confirm-flow-disk" />
          <svg class="confirm-flow-check" viewBox="0 0 24 24">
            <path pathLength="28" d="M4.2 12.2 L9.3 17.3 L19.8 6.4" />
          </svg>
        </div>
        <div class="confirm-flow-label">{{ t('pages.clarify.confirmFlowDialogDone') }}</div>
      </div>
    </template>
  </AppModal>
</template>

<style scoped>
.confirm-flow-done {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  text-align: center;
}
.confirm-flow-ring {
  position: relative;
  display: flex;
  width: 52px;
  height: 52px;
  align-items: center;
  justify-content: center;
  color: rgb(var(--c-ok));
}
.confirm-flow-disk {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: rgb(var(--c-ok) / 0.15);
  transform: scale(0.9);
  opacity: 0;
  animation: confirm-flow-disk-in 240ms cubic-bezier(0.22, 1, 0.36, 1) both;
}
.confirm-flow-check {
  position: relative;
  z-index: 1;
  width: 28px;
  height: 28px;
  fill: none;
  stroke: currentColor;
  stroke-width: 2.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.confirm-flow-check path {
  stroke-dasharray: 28;
  stroke-dashoffset: 28;
  animation: confirm-flow-draw 420ms cubic-bezier(0.33, 0, 0.2, 1) 70ms forwards;
}
.confirm-flow-label {
  font-size: 15px;
  font-weight: 600;
  color: rgb(var(--c-txt));
  opacity: 0;
  animation: confirm-flow-rise 220ms var(--ease-out-expo) 240ms both;
}
@keyframes confirm-flow-disk-in {
  from {
    opacity: 0;
    transform: scale(0.9);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}
@keyframes confirm-flow-draw {
  to {
    stroke-dashoffset: 0;
  }
}
@keyframes confirm-flow-rise {
  from {
    opacity: 0;
    transform: translateY(6px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}
@media (prefers-reduced-motion: reduce) {
  .confirm-flow-disk,
  .confirm-flow-check path,
  .confirm-flow-label {
    animation: none;
    opacity: 1;
    transform: none;
    stroke-dashoffset: 0;
  }
}
</style>
