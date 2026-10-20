<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import {
  placeFixedOverlayAbove,
  useFixedOverlayAboveListeners,
  type FixedOverlayAboveStyle,
} from '@/lib/composables/useFixedOverlayAbove'

const props = withDefaults(defineProps<{ username: string; initials: string; visible?: boolean }>(), {
  visible: true,
})
const emit = defineEmits<{ (event: 'logout'): void }>()
const { t } = useI18n()
const route = useRoute()
const id = useId().replace(/:/g, '')
const triggerId = `sidebar-account-trigger-${id}`
const menuId = `sidebar-account-menu-${id}`
const displayName = computed(() => props.username || t('nav.settings'))
const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const menuStyle = ref<FixedOverlayAboveStyle | null>(null)

async function repositionMenu() {
  if (!open.value) return
  const style = await placeFixedOverlayAbove(trigger.value, menu.value, {
    align: 'left',
    gap: 8,
    width: Math.min(220, window.innerWidth - 16),
  })
  if (open.value) menuStyle.value = style
}

const { start, stop } = useFixedOverlayAboveListeners(open, repositionMenu)
watch(open, (value) => value ? start() : stop())

function actions() {
  return Array.from(menu.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
}

function focusAction(last = false) {
  const items = actions()
  items[last ? items.length - 1 : 0]?.focus()
}

async function openMenu(last = false) {
  if (!props.visible) return
  open.value = true
  await nextTick()
  await repositionMenu()
  if (open.value) focusAction(last)
}

async function closeMenu(restoreFocus = true) {
  if (!open.value) return
  open.value = false
  menuStyle.value = null
  await nextTick()
  if (restoreFocus && props.visible) trigger.value?.focus()
}

function toggleMenu() {
  if (open.value) void closeMenu()
  else void openMenu()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  event.stopPropagation()
  void openMenu(event.key === 'ArrowUp')
}

function onMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Tab') {
    event.preventDefault()
    void closeMenu()
    return
  }
  const items = actions()
  const index = items.indexOf(document.activeElement as HTMLElement)
  let next: number
  if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = items.length - 1
  else if (event.key === 'ArrowDown') next = (index + 1) % items.length
  else if (event.key === 'ArrowUp') next = (index - 1 + items.length) % items.length
  else return
  event.preventDefault()
  event.stopPropagation()
  items[next]?.focus()
}

function onDocumentClick(event: MouseEvent) {
  const target = event.target
  if (!open.value || !(target instanceof Node)) return
  if (trigger.value?.contains(target) || menu.value?.contains(target)) return
  void closeMenu(false)
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (!open.value || event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  void closeMenu()
}

async function logout() {
  await closeMenu()
  emit('logout')
}

watch(() => route.fullPath, () => void closeMenu(false))
watch(() => props.visible, (visible) => {
  if (!visible) void closeMenu(false)
})
onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onDocumentKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick)
  document.removeEventListener('keydown', onDocumentKeydown)
})
</script>

<template>
  <div class="sidebar-account">
    <button
      :id="triggerId"
      ref="trigger"
      type="button"
      class="sidebar-account-trigger"
      data-testid="sidebar-account-trigger"
      :aria-label="displayName"
      :title="displayName"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="menuId"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    >
      <span
        class="sidebar-account-avatar force-radius-full h-7 w-7 rounded-full"
        data-testid="sidebar-user-avatar"
        aria-hidden="true"
      >{{ initials }}</span>
    </button>

    <Teleport to="body">
      <Transition name="sidebar-account-menu">
        <div
          v-if="open"
          :id="menuId"
          ref="menu"
          class="sidebar-account-menu fixed"
          data-testid="sidebar-account-menu"
          role="menu"
          :aria-labelledby="triggerId"
          :style="menuStyle ?? undefined"
          @keydown="onMenuKeydown"
        >
          <div class="sidebar-account-identity">
            <span class="sidebar-account-avatar force-radius-full h-7 w-7 rounded-full" aria-hidden="true">{{ initials }}</span>
            <span class="sidebar-account-name" :title="displayName">{{ displayName }}</span>
          </div>
          <RouterLink
            to="/settings"
            class="sidebar-account-action"
            data-testid="sidebar-account-settings"
            role="menuitem"
            tabindex="-1"
            @click="closeMenu()"
          >
            <Icon name="settings" :size="16" aria-hidden="true" />
            <span>{{ t('nav.settings') }}</span>
          </RouterLink>
          <button
            type="button"
            class="sidebar-account-action sidebar-account-logout"
            data-testid="sidebar-logout"
            role="menuitem"
            tabindex="-1"
            @click="logout"
          >
            <Icon name="logout" :size="16" aria-hidden="true" />
            <span>{{ t('shell.logout') }}</span>
          </button>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.sidebar-account { display: flex; flex: none; }
.sidebar-account-trigger {
  display: flex;
  width: 44px;
  height: 44px;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 12px;
  background: transparent;
  color: rgb(var(--c-txt2));
  transition: background-color 140ms ease, color 140ms ease;
}
.sidebar-account-trigger:hover,
.sidebar-account-trigger[aria-expanded="true"] { background: rgb(var(--c-txt) / .05); }
.sidebar-account-avatar {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;
  background: rgb(var(--c-txt) / .075);
  color: rgb(var(--c-txt2));
  font-size: 11px;
  font-weight: 600;
}
.sidebar-account-menu {
  position: fixed;
  top: 8px;
  left: 8px;
  z-index: 70;
  width: 220px;
  max-width: calc(100vw - 16px);
  max-height: calc(100dvh - 16px);
  overflow-y: auto;
  padding: 6px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
  box-shadow: 0 10px 32px rgb(0 0 0 / .12);
}
.sidebar-account-identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
  padding: 8px 8px 12px;
  border-bottom: 1px solid rgb(var(--c-line));
}
.sidebar-account-name { min-width: 0; overflow: hidden; color: rgb(var(--c-txt)); font-size: 13px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.sidebar-account-action {
  display: flex;
  width: 100%;
  height: 36px;
  align-items: center;
  gap: 9px;
  padding: 0 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 13px;
  text-align: left;
  text-decoration: none;
  transition: background-color 140ms ease, color 140ms ease;
}
.sidebar-account-action:hover { background: rgb(var(--c-txt) / .05); color: rgb(var(--c-txt)); }
.sidebar-account-logout:hover { color: rgb(var(--c-err)); }
.sidebar-account-trigger:focus-visible,
.sidebar-account-action:focus-visible { outline: 2px solid rgb(var(--c-accent) / .55); outline-offset: -2px; }
.sidebar-account-menu-enter-active,
.sidebar-account-menu-leave-active { transition: opacity 120ms ease, transform 120ms ease; transform-origin: left bottom; }
.sidebar-account-menu-enter-from,
.sidebar-account-menu-leave-to { opacity: 0; transform: translateY(3px); }
@media (prefers-reduced-motion: reduce) {
  .sidebar-account-trigger,
  .sidebar-account-action,
  .sidebar-account-menu-enter-active,
  .sidebar-account-menu-leave-active { transition: none; }
}
</style>
