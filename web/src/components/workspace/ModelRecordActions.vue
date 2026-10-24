<script lang="ts">
let closeCurrent: (() => void) | undefined
</script>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import { useRoutingCopy } from '@/lib/agent/modelRouting'

const props = withDefaults(defineProps<{ name: string; active?: boolean; prefix: string; recordId: string; showEdit?: boolean; showToggle?: boolean }>(), { active: true, showEdit: true, showToggle: true })
const emit = defineEmits<{ edit: []; toggle: []; remove: [] }>()
const c = useRoutingCopy()
const { t } = useI18n()
const open = ref(false)
const trigger = ref<HTMLButtonElement>()
const menu = ref<HTMLElement>()
const position = ref<CSSProperties>({ visibility: 'hidden' })
const menuId = `model-actions-${useId().replace(/:/g, '')}`
function testId(action: string) { return `${props.prefix}-${action}-${props.recordId}` }
function items() { return Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || []) }
function place() {
  if (!trigger.value || !menu.value) return
  const box = trigger.value.getBoundingClientRect()
  if (box.bottom <= 0 || box.top >= innerHeight) { close(); return }
  const width = Math.min(168, innerWidth - 16)
  const height = menu.value.offsetHeight
  const below = box.bottom + 5
  position.value = { width: `${width}px`, left: `${Math.max(8, Math.min(box.right - width, innerWidth - width - 8))}px`, top: `${Math.max(8, below + height <= innerHeight - 8 ? below : box.top - height - 5)}px` }
}
function close(restore = false) {
  open.value = false
  if (closeCurrent === close) closeCurrent = undefined
  if (restore) trigger.value?.focus()
}
async function show(last = false) {
  closeCurrent?.()
  closeCurrent = close
  position.value = { visibility: 'hidden' }
  open.value = true
  await nextTick()
  if (!open.value) return
  place()
  const buttons = items()
  buttons[last ? buttons.length - 1 : 0]?.focus()
}
function activate(action: 'toggle' | 'remove') { close(true); if (action === 'toggle') emit('toggle'); else emit('remove') }
function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); return }
  if (event.key === 'Tab') { close(true); return }
  const buttons = items()
  if (!buttons.length) return
  const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
  const index = event.key === 'ArrowDown' ? (current + 1) % buttons.length : event.key === 'ArrowUp' ? (current - 1 + buttons.length) % buttons.length : event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : -1
  if (index < 0) return
  event.preventDefault(); buttons[index]?.focus()
}
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !trigger.value?.contains(event.target) && !menu.value?.contains(event.target)) close()
}
function escape(event: KeyboardEvent) {
  if (open.value && event.key === 'Escape') { event.preventDefault(); close(true) }
}
function dismiss() { if (open.value) close() }
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', escape)
  window.addEventListener('resize', dismiss)
  window.addEventListener('scroll', dismiss, true)
})
onBeforeUnmount(() => {
  close()
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', escape)
  window.removeEventListener('resize', dismiss)
  window.removeEventListener('scroll', dismiss, true)
})
</script>

<template>
  <div class="model-record-actions">
    <button v-if="showEdit" type="button" class="model-record-edit" :aria-label="c.edit" :title="c.edit" :data-testid="testId('edit')" @click="emit('edit')"><Icon name="edit" :size="15" /></button>
    <button ref="trigger" type="button" class="model-record-more" :aria-label="`${t('pages.agentStudio.tree.moreActions')} · ${name}`" aria-haspopup="menu" :aria-expanded="open" :aria-controls="open ? menuId : undefined" :data-testid="testId('more')" @click="open ? close() : show()" @keydown.down.prevent="show()" @keydown.up.prevent="show(true)">
      <Icon name="more" :size="16" />
    </button>
    <AppSwitch v-if="showToggle" :model-value="active" :aria-label="`${active ? c.disable : c.enable} · ${name}`" :data-testid="testId('toggle')" @update:model-value="emit('toggle')" />
    <Teleport to="body">
      <div v-if="open" :id="menuId" ref="menu" role="menu" :aria-label="name" class="model-record-menu" :data-testid="testId('menu')" :style="position" @keydown="onKey">
        <button type="button" role="menuitem" tabindex="-1" class="model-record-delete" :aria-label="`${c.remove} · ${name}`" :data-testid="testId('delete')" @click="activate('remove')"><Icon name="trash" :size="14" />{{ c.remove }}</button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.model-record-actions { display: flex; align-items: center; gap: 5px; }
.model-record-edit, .model-record-more { display: inline-flex; height: 28px; align-items: center; justify-content: center; border: 0; border-radius: 6px; background: transparent; color: rgb(var(--c-txt2)); cursor: pointer; }
.model-record-edit { width: 28px; color: rgb(var(--c-txt3)); }
.model-record-more { width: 28px; color: rgb(var(--c-txt3)); }
.model-record-edit:hover, .model-record-more:hover { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.model-record-actions button:focus-visible, .model-record-menu button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px; }
.model-record-menu { position: fixed; z-index: 10000; padding: 5px; border: 1px solid rgb(var(--c-line)); border-radius: 9px; background: rgb(var(--c-surface)); box-shadow: 0 8px 24px rgb(0 0 0 / .10); }
.model-record-menu button { display: flex; width: 100%; min-height: 32px; align-items: center; gap: 9px; border: 0; border-radius: 5px; padding: 6px 9px; background: transparent; color: rgb(var(--c-txt2)); text-align: left; font-size: 12px; cursor: pointer; }
.model-record-menu button:hover, .model-record-menu button:focus-visible { background: rgb(var(--c-elevated)); }
.model-record-menu .model-record-delete { color: rgb(var(--c-err)); }
</style>
