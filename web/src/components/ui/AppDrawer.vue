<script setup lang="ts">
import Icon from './Icon.vue'

withDefaults(
  defineProps<{
    open: boolean
    title?: string
    width?: number
    /** Stick to the viewport top, right, and bottom. Default stays the floating card. */
    flush?: boolean
  }>(),
  { flush: false },
)
const emit = defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <Teleport to="body">
    <Transition name="drawer">
      <div v-if="open" class="fixed inset-0 z-40">
        <div class="absolute inset-0 bg-black/50" @click="emit('close')" />
        <div
          class="absolute flex flex-col bg-surface"
          :class="flush
            ? 'inset-y-0 right-0 overflow-hidden border-l border-line'
            : 'app-sidebar-card bottom-3.5 right-3.5 top-3.5'"
          :style="flush
            ? { width: (width || 420) + 'px', maxWidth: '100vw' }
            : { width: (width || 420) + 'px', maxWidth: 'calc(100vw - 28px)' }"
        >
          <div class="flex h-14 shrink-0 items-center gap-2 border-b border-line px-4">
            <div class="flex-1 text-sm font-semibold text-txt">{{ title }}</div>
            <button class="flex h-8 w-8 items-center justify-center rounded-md text-txt3 hover:bg-elevated hover:text-txt" @click="emit('close')">
              <Icon name="close" :size="18" />
            </button>
          </div>
          <div class="scroll-area min-h-0 flex-1 overflow-y-auto">
            <slot />
          </div>
          <div v-if="$slots.footer" class="shrink-0 border-t border-line p-4">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.drawer-enter-active,
.drawer-leave-active {
  transition: opacity var(--dur-overlay) var(--ease-out-expo);
}
.drawer-enter-active > div:last-child,
.drawer-leave-active > div:last-child {
  transition: transform var(--dur-overlay) var(--ease-out-expo);
}
.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}
.drawer-enter-from > div:last-child,
.drawer-leave-to > div:last-child {
  transform: translateX(100%);
}
</style>
