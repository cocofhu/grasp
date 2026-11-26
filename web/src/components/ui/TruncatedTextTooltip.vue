<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useAttrs, useId, useSlots } from 'vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
  defineProps<{ text: string; focusable?: boolean; focusParent?: boolean; measureChild?: boolean }>(),
  { focusable: true, focusParent: false, measureChild: false },
)
const attrs = useAttrs()
const slots = useSlots()
const trigger = ref<HTMLElement | null>(null)
const tooltip = ref<HTMLElement | null>(null)
const truncated = ref(false)
const open = ref(false)
const triggerHovered = ref(false)
const tooltipHovered = ref(false)
const focused = ref(false)
const touchPointer = ref(false)
const tooltipId = `truncated-tooltip-${useId().replace(/:/g, '')}`
const position = ref({ left: 8, top: 8, maxWidth: 320 })
let resizeObserver: ResizeObserver | undefined
let closeTimer: number | undefined
let parentFocusTarget: HTMLElement | null = null

const hasTooltip = computed(() => Boolean(slots.tooltip) || truncated.value)
const triggerAttrs = computed(() => ({
  ...attrs,
  tabindex: hasTooltip.value && props.focusable ? 0 : undefined,
  role: hasTooltip.value && props.focusable ? 'button' : undefined,
  'aria-expanded': hasTooltip.value && props.focusable ? open.value : undefined,
  'aria-describedby': open.value ? tooltipId : undefined,
}))

function syncParentDescription() {
  if (!parentFocusTarget) return
  if (open.value) parentFocusTarget.setAttribute('aria-describedby', tooltipId)
  else if (parentFocusTarget.getAttribute('aria-describedby') === tooltipId) {
    parentFocusTarget.removeAttribute('aria-describedby')
  }
}

function measureOverflow() {
  if (slots.tooltip) return
  const elements = props.measureChild
    ? Array.from(trigger.value?.querySelectorAll<HTMLElement>('*') ?? [])
    : trigger.value
      ? [trigger.value]
      : []
  truncated.value = elements.some((el) => el.scrollWidth > el.clientWidth + 1)
  if (!truncated.value) open.value = false
}

async function placeTooltip() {
  await nextTick()
  const anchor = trigger.value
  const tip = tooltip.value
  if (!anchor || !tip || !open.value) return

  const margin = 8
  const gap = 6
  const anchorBox = anchor.getBoundingClientRect()
  const maxWidth = Math.max(160, Math.min(360, window.innerWidth - margin * 2))
  position.value.maxWidth = maxWidth
  await nextTick()

  const tipBox = tip.getBoundingClientRect()
  const left = Math.min(
    window.innerWidth - tipBox.width - margin,
    Math.max(margin, anchorBox.left + anchorBox.width / 2 - tipBox.width / 2),
  )
  const below = anchorBox.bottom + gap
  const top =
    below + tipBox.height <= window.innerHeight - margin
      ? below
      : Math.max(margin, anchorBox.top - tipBox.height - gap)
  position.value = { left, top, maxWidth }
}

function show() {
  measureOverflow()
  if (!hasTooltip.value) return
  open.value = true
  syncParentDescription()
  void placeTooltip()
}

function hide() {
  open.value = false
  syncParentDescription()
}

function scheduleHide() {
  if (closeTimer) window.clearTimeout(closeTimer)
  closeTimer = window.setTimeout(() => {
    if (!triggerHovered.value && !tooltipHovered.value && !focused.value) hide()
  }, 40)
}

function onPointerDown(event: PointerEvent) {
  touchPointer.value = event.pointerType === 'touch'
}

function onClick(event: MouseEvent) {
  if (!touchPointer.value) return
  event.stopPropagation()
  measureOverflow()
  if (hasTooltip.value) {
    open.value = !open.value
    if (open.value) void placeTooltip()
  }
}

function onMouseEnter() {
  triggerHovered.value = true
  touchPointer.value = false
  show()
}

function onMouseLeave() {
  triggerHovered.value = false
  scheduleHide()
}

function onFocus() {
  focused.value = true
  if (!touchPointer.value) show()
}

function onBlur() {
  focused.value = false
  scheduleHide()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    hide()
    event.stopPropagation()
    return
  }
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    event.stopPropagation()
    measureOverflow()
    if (hasTooltip.value) {
      open.value = !open.value
      if (open.value) void placeTooltip()
    }
  }
}

function onParentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    hide()
    event.stopPropagation()
  }
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target as Node
  if (trigger.value?.contains(target) || tooltip.value?.contains(target)) return
  hide()
}

function onViewportChange() {
  measureOverflow()
  if (open.value) void placeTooltip()
}

onMounted(() => {
  measureOverflow()
  resizeObserver = new ResizeObserver(onViewportChange)
  if (trigger.value) resizeObserver.observe(trigger.value)
  if (props.focusParent && trigger.value?.parentElement) {
    parentFocusTarget = trigger.value.parentElement
    parentFocusTarget.addEventListener('focus', onFocus)
    parentFocusTarget.addEventListener('blur', onBlur)
    parentFocusTarget.addEventListener('keydown', onParentKeydown)
  }
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  parentFocusTarget?.removeEventListener('focus', onFocus)
  parentFocusTarget?.removeEventListener('blur', onBlur)
  parentFocusTarget?.removeEventListener('keydown', onParentKeydown)
  open.value = false
  syncParentDescription()
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
  if (closeTimer) window.clearTimeout(closeTimer)
})
</script>

<template>
  <span
    ref="trigger"
    v-bind="triggerAttrs"
    @pointerdown="onPointerDown"
    @click="onClick"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
    @focus="onFocus"
    @blur="onBlur"
    @keydown="onKeydown"
  >
    <slot>{{ text }}</slot>
  </span>
  <Teleport to="body">
    <Transition name="ui-fade">
      <div
        v-if="open"
        :id="tooltipId"
        ref="tooltip"
        data-testid="truncated-text-tooltip"
        role="tooltip"
        class="fixed z-[100] whitespace-normal rounded-md border border-line-strong bg-elevated px-2.5 py-1.5 text-[12px] leading-relaxed text-txt shadow-lg [overflow-wrap:anywhere]"
        :style="{
          left: position.left + 'px',
          top: position.top + 'px',
          maxWidth: position.maxWidth + 'px',
        }"
        @mouseenter="tooltipHovered = true"
        @mouseleave="tooltipHovered = false; scheduleHide()"
      >
        <slot name="tooltip">{{ text }}</slot>
      </div>
    </Transition>
  </Teleport>
</template>
