<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AgentSidebar from '@/components/agent/AgentSidebar.vue'
import type { ResourceListItem } from '@/components/ui/ResourceList.vue'

defineProps<{ label: string; nodes: ResourceListItem[]; pinned?: ResourceListItem[]; activeKey: string; hideCreateTeam?: boolean }>()
const emit = defineEmits<{
  (e: 'select', key: string): void
  (e: 'rename', agentName: string): void
  (e: 'delete', agentName: string): void
  (e: 'import'): void
  (e: 'create-agent'): void
  (e: 'create-team'): void
  (e: 'export-workspace'): void
}>()
const { t } = useI18n()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const list = ref<InstanceType<typeof AgentSidebar> | null>(null)
const panelId = `agent-switcher-${useId().replace(/:/g, '')}`
/** The list's own menus are teleported to <body>; presses inside them still belong to the switcher. */
const LIST_POPUPS = '[data-testid="agent-tree-more-menu"],[data-testid="agent-row-menu"]'

async function show() {
  open.value = true
  await nextTick()
  panel.value?.querySelector<HTMLInputElement>('input[type="search"]')?.focus()
}

function hide(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  list.value?.closeMenus()
  if (restoreFocus) trigger.value?.focus()
}

function toggle() {
  if (open.value) hide()
  else void show()
}

function onSelect(key: string) {
  hide()
  emit('select', key)
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!open.value) return
  const target = event.target
  if (!(target instanceof Node) || root.value?.contains(target)) return
  if (target instanceof Element && target.closest(LIST_POPUPS)) return
  hide()
}

function onDocumentKeydown(event: KeyboardEvent) {
  // The list closes its own menus on Escape first and marks the event handled.
  if (event.key !== 'Escape' || !open.value || event.defaultPrevented) return
  event.preventDefault()
  hide(true)
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('keydown', onDocumentKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('keydown', onDocumentKeydown)
})
</script>
<template>
  <div ref="root" class="relative flex min-w-0" data-testid="agent-switcher">
    <button
      ref="trigger"
      type="button"
      data-testid="agent-switcher-trigger"
      class="inline-flex min-w-0 items-center gap-1 rounded px-1.5 py-1 text-[13px] font-medium text-txt transition hover:bg-overlay"
      :title="label"
      :aria-label="`${label} · ${t('pages.agentStudio.switcher.trigger')}`"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="panelId"
      @click="toggle"
    >
      <span class="min-w-0 truncate">{{ label }}</span>
      <Icon :interactive="false" name="chevron-down" :size="14" class="shrink-0 text-txt3 transition-transform" :class="open ? 'rotate-180' : ''" />
    </button>
    <div
      v-show="open"
      :id="panelId"
      ref="panel"
      role="dialog"
      :aria-label="t('pages.agentStudio.switcher.trigger')"
      data-testid="agent-switcher-panel"
      class="absolute left-0 top-full z-50 mt-1.5 flex h-[min(440px,calc(100dvh-160px))] w-72 flex-col overflow-hidden rounded-lg border border-line bg-surface shadow-card"
    >
      <AgentSidebar
        ref="list"
        class="min-h-0 flex-1"
        :nodes="nodes"
        :pinned="pinned"
        :active-key="activeKey"
        :hide-create-team="hideCreateTeam"
        @select="onSelect"
        @rename="hide(); emit('rename', $event)"
        @delete="hide(); emit('delete', $event)"
        @import="hide(); emit('import')"
        @create-agent="hide(); emit('create-agent')"
        @create-team="hide(); emit('create-team')"
        @export-workspace="hide(); emit('export-workspace')"
      />
    </div>
  </div>
</template>
