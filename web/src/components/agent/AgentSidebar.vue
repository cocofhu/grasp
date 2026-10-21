<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ResourceList, { type ResourceListItem } from '@/components/ui/ResourceList.vue'
const props = defineProps<{ nodes: ResourceListItem[]; pinned?: ResourceListItem[]; activeKey: string; collapsed: boolean; hideCreateTeam?: boolean }>()
const emit = defineEmits<{
  (e: 'select', key: string): void
  (e: 'open-manage', agentName?: string): void
  (e: 'import'): void
  (e: 'create-agent'): void
  (e: 'create-team'): void
  (e: 'export-workspace'): void
  (e: 'toggle-collapsed'): void
}>()
const { t } = useI18n()
const context = ref<{ name: string; x: number; y: number } | null>(null)
const moreOpen = ref(false)
const moreTrigger = ref<HTMLButtonElement | null>(null)
const moreMenu = ref<HTMLElement | null>(null)
const moreStyle = ref<CSSProperties>({ visibility: 'hidden' })
const menuId = `agent-tree-actions-${useId().replace(/:/g, '')}`
const triggerId = `${menuId}-trigger`
const buttonClass = 'flex h-7 w-7 shrink-0 items-center justify-center rounded text-txt3 hover:bg-elevated hover:text-accent-2'

function positionMore() {
  if (!moreOpen.value || !moreTrigger.value || !moreMenu.value) return
  const anchor = moreTrigger.value.getBoundingClientRect()
  const menu = moreMenu.value.getBoundingClientRect()
  const margin = 8
  const width = Math.min(menu.width || 208, window.innerWidth - margin * 2)
  const height = Math.min(menu.height, window.innerHeight - margin * 2)
  const left = Math.max(margin, Math.min(anchor.right - width, window.innerWidth - width - margin))
  const below = anchor.bottom + 6
  const preferredTop = below + height <= window.innerHeight - margin ? below : anchor.top - height - 6
  const top = Math.max(margin, Math.min(preferredTop, window.innerHeight - height - margin))
  moreStyle.value = { top: `${Math.round(top)}px`, left: `${Math.round(left)}px`, width: `${width}px` }
}

function menuItems() {
  return Array.from(moreMenu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
}

async function openMore(last = false) {
  context.value = null
  moreStyle.value = { visibility: 'hidden' }
  moreOpen.value = true
  await nextTick()
  if (!moreOpen.value || props.collapsed) return
  positionMore()
  const items = menuItems()
  items[last ? items.length - 1 : 0]?.focus()
}

async function closeMore(restoreFocus = false) {
  if (!moreOpen.value) return
  moreOpen.value = false
  if (restoreFocus) {
    await nextTick()
    if (!props.collapsed) moreTrigger.value?.focus()
  }
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  void openMore(event.key === 'ArrowUp')
}

function onMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Tab') {
    moreTrigger.value?.focus()
    void closeMore()
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    void closeMore(true)
    return
  }
  const items = menuItems()
  const index = items.indexOf(document.activeElement as HTMLButtonElement)
  let next: number
  if (event.key === 'ArrowDown') next = (index + 1) % items.length
  else if (event.key === 'ArrowUp') next = (index - 1 + items.length) % items.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = items.length - 1
  else return
  event.preventDefault()
  items[next]?.focus()
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!moreOpen.value || !(event.target instanceof Node)) return
  if (moreTrigger.value?.contains(event.target) || moreMenu.value?.contains(event.target)) return
  void closeMore()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (moreOpen.value && event.key === 'Escape') {
    event.preventDefault()
    void closeMore(true)
  }
}

function runMoreAction(action: 'manage' | 'import' | 'export' | 'team') {
  void closeMore()
  if (action === 'manage') emit('open-manage')
  else if (action === 'import') emit('import')
  else if (action === 'export') emit('export-workspace')
  else emit('create-team')
}

function onContext(event: MouseEvent, name: string) {
  event.preventDefault()
  void closeMore()
  context.value = { name, x: event.clientX, y: event.clientY }
}

watch(() => props.collapsed, () => {
  void closeMore()
  context.value = null
})
onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', positionMore)
  window.addEventListener('scroll', positionMore, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', positionMore)
  window.removeEventListener('scroll', positionMore, true)
})
</script>
<template>
  <div class="flex min-h-0 min-w-0 flex-col border-r border-line" data-testid="agent-resource-sidebar">
    <button v-if="collapsed" type="button" :class="buttonClass" :aria-label="t('pages.agentStudio.agentList.expand')" @click="emit('toggle-collapsed')"><Icon name="chevron-right" :size="14" /></button>
    <ResourceList v-else :nodes="nodes" :pinned="pinned" :active-key="activeKey" :title="t('pages.agentStudio.tree.title')" :total="nodes.length" :search-placeholder="t('pages.agentStudio.tree.searchPlaceholder')" :empty-text="t('pages.agentStudio.tree.empty')" @select="emit('select', $event)" @contextmenu="onContext">
      <template #actions>
        <button type="button" data-testid="agent-tree-create-agent" :class="buttonClass" :title="t('common.buttons.newAgent')" :aria-label="t('common.buttons.newAgent')" @click="emit('create-agent')"><Icon name="plus" :size="12" /></button>
        <button :id="triggerId" ref="moreTrigger" type="button" data-testid="agent-tree-more" :class="buttonClass" :title="t('pages.agentStudio.tree.moreActions')" :aria-label="t('pages.agentStudio.tree.moreActions')" aria-haspopup="menu" :aria-expanded="moreOpen" :aria-controls="menuId" @click="moreOpen ? closeMore() : openMore()" @keydown="onTriggerKeydown"><Icon name="more" :size="15" /></button>
        <button type="button" data-testid="agent-tree-collapse" :class="buttonClass" :title="t('pages.agentStudio.agentList.collapse')" :aria-label="t('pages.agentStudio.agentList.collapse')" @click="emit('toggle-collapsed')"><Icon name="chevron-right" :size="14" class="rotate-180" /></button>
      </template>
    </ResourceList>
    <Teleport to="body">
      <div v-if="moreOpen" :id="menuId" ref="moreMenu" role="menu" :aria-labelledby="triggerId" data-testid="agent-tree-more-menu" class="agent-actions-menu fixed z-[9999] max-h-[calc(100dvh-16px)] w-52 max-w-[calc(100vw-16px)] overflow-y-auto rounded-xl border border-line bg-surface p-1 shadow-card" :style="moreStyle" @keydown="onMenuKeydown">
        <button type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-manage" @click="runMoreAction('manage')"><Icon name="user" :size="15" />{{ t('pages.agentStudio.tree.manageTitle') }}</button>
        <button type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-import" @click="runMoreAction('import')"><Icon name="input" :size="15" />{{ t('pages.agentStudio.exportImport.import') }}</button>
        <button type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-export" @click="runMoreAction('export')"><Icon name="download" :size="15" />{{ t('pages.agentStudio.exportImport.export') }}</button>
        <button v-if="!hideCreateTeam" type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-create-team" @click="runMoreAction('team')"><Icon name="skills" :size="15" />{{ t('pages.agentStudio.tree.newTeam') }}</button>
      </div>
      <div v-if="context" class="fixed inset-0 z-[9998]" @click="context = null" @contextmenu.prevent="context = null" />
      <button v-if="context" type="button" class="fixed z-[9999] rounded border border-line bg-elevated px-3 py-2 text-xs text-txt2 shadow-card" :style="{ left: context.x + 'px', top: context.y + 'px' }" @click="emit('open-manage', context.name); context = null">{{ t('pages.agentStudio.tree.renameViaManage') }}</button>
    </Teleport>
  </div>
</template>

<style scoped>
.agent-actions-menu > button {
  display: flex;
  width: 100%;
  min-height: 36px;
  align-items: center;
  gap: 9px;
  border-radius: 8px;
  padding: 8px 10px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  text-align: left;
}
.agent-actions-menu > button:hover,
.agent-actions-menu > button:focus-visible {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-txt) / .055);
  outline: none;
}
</style>
