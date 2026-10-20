<script lang="ts">
export type ComposerPrimaryKind = 'send' | 'stop' | 'confirm'

/**
 * Last unmounted primary (module scope, shared by every instance). The
 * connecting skeleton and ClarifyChat are separate components; an instance
 * mounted right after the previous one left starts from its width, so the
 * hand-off is a morph rather than a jump.
 */
const HANDOFF_MS = 600
/** How long a hand-off keeps showing the previous state so a state that settles right after mount does not flash. */
const HANDOFF_HOLD_MS = 280
type Handoff = { width: number; at: number; kind: ComposerPrimaryKind }
let handoff: Handoff | null = null
function freshHandoff(): Handoff | null {
  return handoff && Date.now() - handoff.at < HANDOFF_MS ? handoff : null
}
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/ui/Icon.vue'

/**
 * The composer's single primary action (发送 / 停止 / 确认并流转). One <button>
 * stays mounted and morphs between the existing chat-action-* styles, so focus
 * survives a state change and the toolbar never shows two primaries at once.
 */
const props = withDefaults(defineProps<{
  kind: ComposerPrimaryKind
  disabled?: boolean
  /** Send in flight: spinner instead of the arrow. */
  busy?: boolean
  /** Visible text for confirm; aria-label for send/stop. */
  label: string
  title?: string
  testid?: string
  /** Leave a hand-off for the next instance on unmount (the connecting skeleton). */
  handoffOut?: boolean
  /** Confirm-shaped exits other than 确认并流转 (cold gate: 退回 arrow, in-flight spinner). Default per kind. */
  icon?: string
}>(), { disabled: false, busy: false, title: undefined, testid: undefined, handoffOut: false, icon: undefined })

const emit = defineEmits<{ click: [kind: ComposerPrimaryKind] }>()
const el = ref<HTMLButtonElement | null>(null)

/** Previous instance's state, shown (disabled) briefly after a hand-off. */
const held = ref<ComposerPrimaryKind | null>(freshHandoff()?.kind ?? null)
const shownKind = computed<ComposerPrimaryKind>(() => held.value ?? props.kind)
const shownDisabled = computed(() => held.value != null || props.disabled)
const iconName = computed(() =>
  props.icon ?? (shownKind.value === 'stop' ? 'chat-stop' : shownKind.value === 'confirm' ? 'check' : props.busy ? 'spinner' : 'chat-send'),
)

function reduceMotion(): boolean {
  return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

function morphFrom(from: number) {
  const node = el.value
  if (!node || !from || typeof node.animate !== 'function' || reduceMotion()) return
  const to = node.getBoundingClientRect().width
  if (!to || Math.abs(to - from) < 1) return
  node.animate([{ width: `${from}px` }, { width: `${to}px` }], { duration: 240, easing: 'cubic-bezier(.22, 0, .18, 1)' })
}

let fromWidth = 0
watch(shownKind, () => { fromWidth = el.value?.getBoundingClientRect().width || 0 }, { flush: 'pre' })
watch(shownKind, () => morphFrom(fromWidth), { flush: 'post' })

let holdTimer: ReturnType<typeof setTimeout> | undefined
onMounted(() => {
  handoff = null
  if (held.value != null) holdTimer = setTimeout(() => { held.value = null }, HANDOFF_HOLD_MS)
})
onBeforeUnmount(() => {
  clearTimeout(holdTimer)
  if (!props.handoffOut) return
  const w = el.value?.getBoundingClientRect().width || 0
  handoff = w ? { width: w, at: Date.now(), kind: shownKind.value } : null
})
</script>

<template>
  <button
    ref="el"
    type="button"
    class="composer-primary"
    :class="shownKind === 'confirm' ? 'chat-action-confirm h-9 shrink-0 inline-flex' : shownKind === 'stop' ? 'chat-action-button chat-action-stop' : 'chat-action-button chat-action-primary'"
    :data-kind="shownKind"
    :data-testid="testid"
    :title="title ?? (shownKind === 'confirm' ? undefined : label)"
    :aria-label="shownKind === 'confirm' ? undefined : label"
    :aria-busy="busy ? 'true' : undefined"
    :disabled="shownDisabled"
    @click="emit('click', shownKind)"
  >
    <Transition name="composer-primary-icon" mode="out-in">
      <Icon
        :key="shownKind + (busy ? '-busy' : '') + (icon ?? '')"
        :name="iconName"
        :size="shownKind === 'confirm' ? 13 : shownKind === 'stop' ? 16 : 18"
        :class="iconName === 'spinner' ? 'animate-spin motion-reduce:animate-none' : ''"
      />
    </Transition>
    <span v-if="shownKind === 'confirm'">{{ label }}</span>
  </button>
</template>

<style scoped>
/* Motion only; colours and sizes come from chat-action-* in chat.css. */
.composer-primary {
  overflow: hidden;
  white-space: nowrap;
  transition: background-color .2s ease, border-color .2s ease, color .2s ease, border-radius .24s ease;
}
.composer-primary.chat-action-confirm { padding-inline: 10px; font-size: 12px; }
.composer-primary-icon-enter-active, .composer-primary-icon-leave-active { transition: opacity .12s ease, transform .12s ease; }
.composer-primary-icon-enter-from { opacity: 0; transform: rotate(-90deg) scale(.6); }
.composer-primary-icon-leave-to { opacity: 0; transform: rotate(90deg) scale(.6); }
@media (prefers-reduced-motion: reduce) {
  .composer-primary, .composer-primary-icon-enter-active, .composer-primary-icon-leave-active { transition: none; }
}
</style>
