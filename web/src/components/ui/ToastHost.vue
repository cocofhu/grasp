<script setup lang="ts">
import { useToast } from '@/lib/composables/useToast'

const { toasts, runAction } = useToast()
</script>

<template>
  <Teleport to="body">
    <div
      class="pointer-events-none fixed bottom-6 right-6 z-[100] flex flex-col gap-2"
      role="status"
      aria-live="polite"
      data-testid="toast-host"
    >
      <TransitionGroup name="toast">
        <div
          v-for="t in toasts"
          :key="t.id"
          class="flex items-center gap-3 rounded-lg border border-line bg-elevated px-4 py-2.5 text-[13px] font-medium text-txt shadow-card"
          :class="{
            'border-ok/40 text-ok': t.type === 'success',
            'border-err/40 text-err': t.type === 'error',
            'border-warn/40 text-warn': t.type === 'warn',
          }"
        >
          <span>{{ t.message }}</span>
          <button
            v-if="t.action"
            type="button"
            class="pointer-events-auto -my-1 rounded-md px-2 py-1 text-[12px] font-semibold text-accent-2 hover:bg-accent-dim"
            data-testid="toast-action"
            @click="runAction(t.id)"
          >
            {{ t.action.label }}
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition:
    opacity var(--dur-overlay) var(--ease-out-expo),
    transform var(--dur-overlay) var(--ease-out-expo);
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>
