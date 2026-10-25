<script setup lang="ts">
/**
 * Capsule accent switch (role=switch) — shared boolean control.
 * On: bg-accent track + white thumb on the right.
 * Off: border-line-strong / bg-base + thumb on the left (Pm enable semantics).
 * Callers pass aria-label / class / data-testid via attribute fallthrough.
 */
const props = withDefaults(
  defineProps<{
    modelValue?: boolean
    disabled?: boolean
  }>(),
  { modelValue: false, disabled: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    toggle()
  }
}
</script>

<template>
  <button
    type="button"
    role="switch"
    class="app-switch relative h-[22px] w-10 shrink-0 rounded-full border disabled:cursor-not-allowed disabled:opacity-45"
    :class="modelValue ? 'border-accent bg-accent' : 'border-line-strong bg-base'"
    :aria-checked="modelValue"
    :aria-disabled="disabled ? 'true' : undefined"
    :disabled="disabled"
    :tabindex="disabled ? -1 : 0"
    @click="toggle"
    @keydown="onKeydown"
  >
    <span
      class="app-switch-thumb pointer-events-none absolute top-0.5 h-4 w-4 rounded-full"
      :class="modelValue ? 'left-[18px] bg-white' : 'left-0.5 bg-txt2'"
    />
  </button>
</template>

<style scoped>
.app-switch,
.app-switch-thumb {
  transition:
    background-color var(--dur-ui) var(--ease-out-expo),
    border-color var(--dur-ui) var(--ease-out-expo),
    left var(--dur-ui) var(--ease-out-expo),
    transform var(--dur-ui) var(--ease-out-expo);
}

/* On + enabled only. Resting track stays bg-accent (#7B61FF). */
.app-switch[aria-checked=true]:is(:hover, :focus-visible):not(:disabled) {
  background-color: rgb(var(--c-accent-hover));
  border-color: rgb(var(--c-accent-hover));
}

/* Color still snaps to the hover token; only the color transition is dropped. */
@media (prefers-reduced-motion: reduce) {
  .app-switch,
  .app-switch-thumb {
    transition:
      left var(--dur-ui) var(--ease-out-expo),
      transform var(--dur-ui) var(--ease-out-expo);
  }
}
</style>
