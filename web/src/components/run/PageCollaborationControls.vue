<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { placeFixedOverlayAbove, useFixedOverlayAboveListeners, type FixedOverlayAboveStyle } from '@/lib/composables/useFixedOverlayAbove'

withDefaults(defineProps<{ activeLabels?: string[]; compact?: boolean }>(), { activeLabels: () => [], compact: false })
const { t } = useI18n()
const id = useId()
const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const panelStyle = ref<FixedOverlayAboveStyle>()
let observer: ResizeObserver | undefined

async function reposition() {
  const style = await placeFixedOverlayAbove(trigger.value, panel.value, { align: 'left' })
  if (style && open.value) panelStyle.value = style
}
const { start, stop } = useFixedOverlayAboveListeners(open, reposition)

function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}

function outside(event: Event) {
  const target = event.target as Node | null
  if (open.value && target && !trigger.value?.contains(target) && !panel.value?.contains(target)) close()
}

function escape(event: KeyboardEvent) {
  if (open.value && event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close(true)
  }
}

// Tab out of the teleported panel returns to the trigger's natural tab order.
function tab(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const items = Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])
  const boundary = event.shiftKey ? items[0] : items.at(-1)
  if (document.activeElement !== boundary) return
  event.preventDefault()
  if (event.shiftKey) return close(true)
  const targets = Array.from(document.querySelectorAll<HTMLElement>('button, a[href], input, textarea, select, [tabindex]'))
    .filter((element) => element.tabIndex >= 0 && !element.matches(':disabled') && element.getClientRects().length && !panel.value?.contains(element))
  const next = targets[targets.indexOf(trigger.value!) + 1]
  close()
  ;(next || trigger.value)?.focus()
}

watch(open, async (value) => {
  observer?.disconnect()
  stop()
  if (!value) return
  await nextTick()
  if (!open.value) return
  await reposition()
  if (!open.value) return
  start()
  if (panel.value) observer?.observe(panel.value)
  const first = panel.value?.querySelector<HTMLElement>('[role="switch"]:not(:disabled)')
  ;(first || panel.value)?.focus()
})

onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(() => { if (open.value) void reposition() })
  document.addEventListener('pointerdown', outside, true)
  document.addEventListener('focusin', outside)
  document.addEventListener('keydown', escape, true)
})
onBeforeUnmount(() => {
  open.value = false
  observer?.disconnect()
  document.removeEventListener('pointerdown', outside, true)
  document.removeEventListener('focusin', outside)
  document.removeEventListener('keydown', escape, true)
})
</script>

<template>
  <div class="flex min-w-0 shrink-0 items-center gap-2" :class="compact ? '' : 'mx-3 mb-1 mt-2'" data-page-agent-not-interactive>
    <button
      ref="trigger"
      type="button"
      class="relative inline-flex shrink-0 items-center justify-center rounded-md border text-xs font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
      :class="[
        compact ? 'h-10 w-10' : 'h-8 gap-1.5 px-2',
        open || activeLabels.length ? 'border-accent/40 bg-accent-dim text-accent' : 'border-line bg-surface text-txt2 hover:border-line-strong hover:text-txt',
      ]"
      :title="[t('pages.embedChat.pageCollaboration'), ...activeLabels].join(' · ')"
      :aria-label="t('pages.embedChat.pageCollaboration')"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="`${id}-panel`"
      :aria-describedby="activeLabels.length ? `${id}-summary` : undefined"
      data-testid="page-collaboration-toggle"
      @click="open = !open"
    >
      <Icon name="settings" :size="compact ? 16 : 14" aria-hidden="true" />
      <span v-if="!compact">{{ t('pages.embedChat.pageCollaboration') }}</span>
      <span v-if="activeLabels.length" class="rounded px-1 text-[10px] tabular-nums" :class="compact ? 'absolute -right-1 -top-1 bg-accent text-white' : 'bg-accent/15'" aria-hidden="true">{{ activeLabels.length }}</span>
      <Icon v-if="!compact" name="chevron-down" :size="12" :class="{ 'rotate-180': open }" aria-hidden="true" />
    </button>
    <span :id="`${id}-summary`" :class="compact ? 'sr-only' : 'min-w-0 truncate text-[11px] text-txt3'" :title="activeLabels.join(' · ')" role="status" data-testid="page-collaboration-summary">{{ activeLabels.join(' · ') }}</span>
    <Teleport to="body">
      <section
        v-if="open"
        :id="`${id}-panel`"
        ref="panel"
        class="fixed z-[1000] max-h-[calc(100dvh-16px)] w-[min(360px,calc(100vw-16px))] overflow-y-auto rounded-lg border border-line-strong bg-surface text-txt shadow-xl"
        :style="panelStyle"
        role="dialog"
        :aria-label="t('pages.embedChat.pageCollaboration')"
        tabindex="-1"
        data-page-agent-not-interactive
        data-testid="page-collaboration-controls"
        @keydown="tab"
      >
        <div class="flex items-center justify-between gap-2 px-3 pb-1 pt-2">
          <h3 class="m-0 text-xs font-medium text-txt2">{{ t('pages.embedChat.pageCollaboration') }}</h3>
          <button type="button" class="flex h-6 w-6 items-center justify-center rounded text-txt3 hover:bg-elevated hover:text-txt" :aria-label="t('common.buttons.close')" data-testid="page-collaboration-close" @click="close(true)">
            <Icon name="close" :size="14" aria-hidden="true" />
          </button>
        </div>
        <div class="divide-y divide-line">
          <slot />
        </div>
      </section>
    </Teleport>
  </div>
</template>
