<script setup lang="ts">
import { nextTick, ref, useId, watch } from 'vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'

const props = withDefaults(defineProps<{
  inline?: boolean
  open: boolean
  title: string
  width?: number
  closeOnEsc?: boolean
  trapFocus?: boolean
  closeLabel: string
  returnFocus?: boolean
  hideHeading?: boolean
}>(), { inline: false, closeOnEsc: false, trapFocus: false, returnFocus: true, hideHeading: false })
const emit = defineEmits<{ close: [] }>()
const headingId = `credential-editor-${useId().replace(/:/g, '')}`
const surface = ref<HTMLElement | null>(null)
const content = ref<HTMLElement | null>(null)
let opener: HTMLElement | null = null
let panel: HTMLElement | null = null
let focusGeneration = 0
function visible(element: HTMLElement | null): element is HTMLElement {
  if (!element?.isConnected || element.matches(':disabled')) return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    const style = window.getComputedStyle(node)
    if (style.display === 'none' || style.visibility === 'hidden') return false
  }
  return true
}
watch(() => [props.open, props.returnFocus] as const, async ([open, returnFocus], [wasOpen]) => {
  if (!props.inline) return
  if (open && wasOpen) return
  const generation = ++focusGeneration
  if (open && document.activeElement instanceof HTMLElement && document.activeElement !== document.body && !surface.value?.contains(document.activeElement)) opener = document.activeElement
  await nextTick()
  if (generation !== focusGeneration) return
  if (open) {
    panel = surface.value?.closest<HTMLElement>('[data-testid="workspace-credentials-panel"]') || surface.value?.parentElement || null
    const target = content.value?.querySelector<HTMLElement>('[autofocus], input:not([disabled]), [role="radio"][tabindex="0"]') || content.value?.querySelector<HTMLElement>('button:not([disabled])')
    target?.focus({ preventScroll: true })
  } else if (returnFocus) {
    const target = visible(opener) ? opener : Array.from(panel?.querySelectorAll<HTMLElement>('[data-editor-trigger]') || []).find(visible)
    if (target) { target.focus({ preventScroll: true }); opener = null }
  }
})
</script>

<template>
  <section v-if="inline && open" ref="surface" class="credential-inline-editor" :aria-labelledby="hideHeading ? undefined : headingId" :aria-label="hideHeading ? title : undefined" data-testid="credential-inline-editor">
    <header v-if="!hideHeading" class="credential-inline-heading">
      <h3 :id="headingId">{{ title }}</h3>
      <AppButton size="sm" variant="ghost" icon="close" :disabled="!closeOnEsc" :aria-label="closeLabel" :title="closeLabel" @click="emit('close')" />
    </header>
    <div ref="content" class="min-w-0"><slot /></div>
    <footer class="credential-inline-footer"><slot name="footer" /></footer>
  </section>
  <AppModal v-else-if="!inline" :open="open" :title="title" :width="width" :close-on-esc="closeOnEsc" :trap-focus="trapFocus" :close-label="closeLabel" @close="emit('close')">
    <slot />
    <template #footer><slot name="footer" /></template>
  </AppModal>
</template>

<style scoped>
.credential-inline-editor { min-width: 0; width: 100%; }
.credential-inline-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 24px; }
.credential-inline-heading h3 { margin: 0; color: rgb(var(--c-txt)); font-size: 16px; font-weight: 600; }
.credential-inline-footer { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-top: 24px; padding-top: 20px; border-top: 1px solid rgb(var(--c-line)); }
</style>
