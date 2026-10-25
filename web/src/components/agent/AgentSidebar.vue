<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ResourceList, { type ResourceListItem } from '@/components/ui/ResourceList.vue'
const props = defineProps<{ nodes: ResourceListItem[]; pinned?: ResourceListItem[]; activeKey: string; collapsed: boolean; hideCreateTeam?: boolean }>()
const emit = defineEmits<{
  (e: 'select', key: string): void
  (e: 'rename', agentName: string): void
  (e: 'delete', agentName: string): void
  (e: 'import'): void
  (e: 'create-agent'): void
  (e: 'create-team'): void
  (e: 'export-workspace'): void
  (e: 'toggle-collapsed'): void
}>()
const { t } = useI18n()
const context = ref<{ name: string; x: number; y: number } | null>(null)
const contextMenu = ref<HTMLElement | null>(null)
const contextStyle = ref<CSSProperties>({ visibility: 'hidden' })
const moreOpen = ref(false)
const moreTrigger = ref<HTMLButtonElement | null>(null)
const moreMenu = ref<HTMLElement | null>(null)
const moreStyle = ref<CSSProperties>({ visibility: 'hidden' })
const menuId = `agent-tree-actions-${useId().replace(/:/g, '')}`
const triggerId = `${menuId}-trigger`
const buttonClass = 'flex h-7 w-7 shrink-0 items-center justify-center rounded text-txt3 hover:bg-elevated hover:text-accent-2'
const CONTEXT_MARGIN = 8

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

function placeContext() {
  if (!context.value || !contextMenu.value) return
  const menu = contextMenu.value.getBoundingClientRect()
  const width = menu.width
  const height = menu.height
  const left = Math.max(CONTEXT_MARGIN, Math.min(context.value.x, window.innerWidth - width - CONTEXT_MARGIN))
  const top = Math.max(CONTEXT_MARGIN, Math.min(context.value.y, window.innerHeight - height - CONTEXT_MARGIN))
  contextStyle.value = { left: `${Math.round(left)}px`, top: `${Math.round(top)}px` }
}

function onWindowLayout() {
  positionMore()
  placeContext()
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
  const target = event.target
  if (!(target instanceof Node)) return
  if (moreOpen.value && !moreTrigger.value?.contains(target) && !moreMenu.value?.contains(target)) void closeMore()
  if (context.value && !contextMenu.value?.contains(target)) context.value = null
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (context.value) {
    event.preventDefault()
    context.value = null
    return
  }
  if (moreOpen.value) {
    event.preventDefault()
    void closeMore(true)
  }
}

function onListScroll() {
  if (context.value) context.value = null
}

function runMoreAction(action: 'import' | 'export' | 'team') {
  void closeMore()
  if (action === 'import') emit('import')
  else if (action === 'export') emit('export-workspace')
  else emit('create-team')
}

function onContext(event: MouseEvent, name: string) {
  event.preventDefault()
  void closeMore()
  if (props.nodes.some((node) => node.id === name && node.locked)) {
    context.value = null
    return
  }
  contextStyle.value = { visibility: 'hidden' }
  context.value = { name, x: event.clientX, y: event.clientY }
  void nextTick(() => placeContext())
}

function runContextAction(action: 'rename' | 'delete') {
  const name = context.value?.name
  context.value = null
  if (!name) return
  if (action === 'rename') emit('rename', name)
  else emit('delete', name)
}

watch(() => props.collapsed, () => {
  void closeMore()
  context.value = null
})
onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onWindowLayout)
  window.addEventListener('scroll', onWindowLayout, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onWindowLayout)
  window.removeEventListener('scroll', onWindowLayout, true)
})
</script>
<template>
  <div class="flex min-h-0 min-w-0 flex-col border-r border-line" data-testid="agent-resource-sidebar" @scroll.capture="onListScroll">
    <button v-if="collapsed" type="button" :class="buttonClass" :aria-label="t('pages.agentStudio.agentList.expand')" @click="emit('toggle-collapsed')"><Icon name="chevron-right" :size="14" /></button>
    <ResourceList v-else :nodes="nodes" :pinned="pinned" :active-key="activeKey" :title="t('pages.agentStudio.tree.title')" :total="nodes.length" :search-placeholder="t('pages.agentStudio.tree.searchPlaceholder')" :empty-text="t('pages.agentStudio.tree.empty')" :no-match-text="t('common.empty.noMatchingAgents')" @select="emit('select', $event)" @contextmenu="onContext">
      <template #actions>
        <button type="button" data-testid="agent-tree-create-agent" :class="buttonClass" :title="t('common.buttons.newAgent')" :aria-label="t('common.buttons.newAgent')" @click="emit('create-agent')"><Icon name="plus" :size="12" /></button>
        <button :id="triggerId" ref="moreTrigger" type="button" data-testid="agent-tree-more" :class="buttonClass" :title="t('pages.agentStudio.tree.moreActions')" :aria-label="t('pages.agentStudio.tree.moreActions')" aria-haspopup="menu" :aria-expanded="moreOpen" :aria-controls="menuId" @click="moreOpen ? closeMore() : openMore()" @keydown="onTriggerKeydown"><Icon name="more" :size="15" /></button>
        <button type="button" data-testid="agent-tree-collapse" :class="buttonClass" :title="t('pages.agentStudio.agentList.collapse')" :aria-label="t('pages.agentStudio.agentList.collapse')" @click="emit('toggle-collapsed')"><Icon :interactive="false" name="chevron-right" :size="14" class="rotate-180" /></button>
      </template>
    </ResourceList>
    <Teleport to="body">
      <div v-if="moreOpen" :id="menuId" ref="moreMenu" role="menu" :aria-labelledby="triggerId" data-testid="agent-tree-more-menu" class="agent-actions-menu fixed z-[9999] max-h-[calc(100dvh-16px)] w-52 max-w-[calc(100vw-16px)] overflow-y-auto rounded-xl border border-line bg-surface p-1 shadow-card" :style="moreStyle" @keydown="onMenuKeydown">
        <button type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-import" @click="runMoreAction('import')"><Icon name="input" :size="15" />{{ t('pages.agentStudio.exportImport.import') }}</button>
        <button type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-export" @click="runMoreAction('export')"><Icon name="download" :size="15" />{{ t('pages.agentStudio.exportImport.export') }}</button>
        <button v-if="!hideCreateTeam" type="button" role="menuitem" tabindex="-1" data-testid="agent-tree-create-team" @click="runMoreAction('team')"><Icon name="users" :size="15" />{{ t('pages.agentStudio.tree.newTeam') }}</button>
      </div>
      <div
        v-if="context"
        ref="contextMenu"
        role="menu"
        data-testid="agent-row-menu"
        class="agent-row-menu fixed z-[9999] min-w-[148px] rounded-lg border border-line bg-elevated py-1 shadow-card"
        :style="contextStyle"
        @contextmenu.prevent
      >
        <button type="button" role="menuitem" class="agent-row-menu__item" data-testid="agent-row-rename" @click="runContextAction('rename')">
          <Icon name="edit" :size="14" />
          <span>{{ t('pages.agentStudio.tree.contextRename') }}</span>
        </button>
        <button type="button" role="menuitem" class="agent-row-menu__item agent-row-menu__item--danger" data-testid="agent-row-delete" @click="runContextAction('delete')">
          <Icon name="trash" :size="14" />
          <span>{{ t('pages.agentStudio.tree.contextDelete') }}</span>
        </button>
      </div>
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
.agent-row-menu__item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border: 0;
  background: transparent;
  padding: 6px 12px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  text-align: start;
  white-space: nowrap;
  cursor: pointer;
}
.agent-row-menu__item:hover,
.agent-row-menu__item:focus-visible {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-overlay));
  outline: none;
}
.agent-row-menu__item--danger,
.agent-row-menu__item--danger:hover,
.agent-row-menu__item--danger:focus-visible {
  color: rgb(var(--c-err));
}
.agent-row-menu__item--danger:hover,
.agent-row-menu__item--danger:focus-visible {
  background: rgb(var(--c-err) / .1);
}
</style>
