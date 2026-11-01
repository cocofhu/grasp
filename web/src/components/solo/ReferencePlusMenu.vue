<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { ReferenceTab } from '@/lib/solo/useReferenceSearch'

/** The composer "+" menu: upload a file, or open the @ picker on a tab. */
withDefaults(defineProps<{
  open: boolean
  /** Opens above the button by default; the home composer opens below. */
  placement?: 'up' | 'down'
  /** data-testid prefix: `<prefix>-menu`, `-upload`, `-session`, `-github`. */
  testPrefix: string
}>(), { placement: 'up' })
const emit = defineEmits<{ (e: 'upload'): void; (e: 'reference', tab: ReferenceTab): void }>()
const { t } = useI18n()
</script>

<template>
  <Transition name="ref-menu">
    <div
      v-if="open"
      class="ref-menu"
      :class="placement === 'down' ? 'ref-menu--down' : 'ref-menu--up'"
      role="menu"
      :data-testid="`${testPrefix}-menu`"
    >
      <button type="button" role="menuitem" class="ref-menu__item" :data-testid="`${testPrefix}-upload`" @click="emit('upload')">
        <Icon name="paperclip" :size="14" />{{ t('pages.solo.reference.menu.upload') }}
      </button>
      <button type="button" role="menuitem" class="ref-menu__item" :data-testid="`${testPrefix}-session`" @click="emit('reference', 'solo')">
        <Icon name="chat" :size="14" />{{ t('pages.solo.reference.menu.session') }}
      </button>
      <button type="button" role="menuitem" class="ref-menu__item" :data-testid="`${testPrefix}-github`" @click="emit('reference', 'issue')">
        <Icon name="branch" :size="14" />{{ t('pages.solo.reference.menu.github') }}
      </button>
    </div>
  </Transition>
</template>

<style scoped>
.ref-menu {
  position: absolute;
  left: 0;
  z-index: 45;
  min-width: 170px;
  border: 1px solid rgb(var(--c-line-strong));
  border-radius: 10px;
  background: rgb(var(--c-elevated));
  padding: 4px;
  box-shadow: 0 12px 32px rgb(0 0 0 / 0.18);
  text-align: left;
  --ref-menu-from: 4px;
}
.ref-menu--up {
  bottom: calc(100% + 6px);
  transform-origin: bottom left;
}
.ref-menu--down {
  top: calc(100% + 6px);
  transform-origin: top left;
  --ref-menu-from: -4px;
}
.ref-menu__item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 7px;
  padding: 7px 9px;
  color: rgb(var(--c-txt2));
  font-size: 12.5px;
  text-align: left;
  transition: background 120ms ease, color 120ms ease;
}
.ref-menu__item:hover {
  background: rgb(var(--c-overlay));
  color: rgb(var(--c-txt));
}
.ref-menu-enter-active {
  transition: opacity 160ms ease, transform 240ms cubic-bezier(0.16, 1, 0.3, 1);
}
.ref-menu-leave-active {
  pointer-events: none;
  transition: opacity 140ms ease, transform 180ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
.ref-menu-enter-from,
.ref-menu-leave-to {
  opacity: 0;
  transform: translateY(var(--ref-menu-from)) scale(0.985);
}
@media (prefers-reduced-motion: reduce) {
  .ref-menu-enter-active,
  .ref-menu-leave-active,
  .ref-menu__item {
    transition: none;
  }
}
</style>
