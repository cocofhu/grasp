<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import { SHORTCUTS, formatKey } from '../composables/useCanvasShortcuts'

const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const MOUSE = ['pan', 'zoom', 'select', 'dblclick'] as const

onMounted(async () => {
  await nextTick()
  root.value?.focus()
})
</script>

<template>
  <div class="absolute inset-0 z-40 flex items-center justify-center bg-base/60" data-testid="shortcut-help" @mousedown.self="emit('close')">
    <div
      ref="root"
      class="cchrome w-[560px] max-w-[calc(100%-32px)] p-5 outline-none"
      role="dialog"
      aria-modal="true"
      :aria-label="t('canvas.shortcuts.title')"
      tabindex="-1"
      @keydown.esc.stop.prevent="emit('close')"
    >
      <div class="mb-4 flex items-center justify-between">
        <h2 class="text-[14px] font-semibold text-txt">{{ t('canvas.shortcuts.title') }}</h2>
        <button type="button" class="cchrome-btn" :aria-label="t('common.buttons.close')" @click="emit('close')"><Icon name="close" :size="14" /></button>
      </div>
      <div class="grid grid-cols-2 gap-x-6 gap-y-2">
        <div v-for="s in SHORTCUTS" :key="s.action" class="flex items-center justify-between gap-3 text-[12px]">
          <span class="text-txt2">{{ t(`canvas.shortcuts.actions.${s.labelKey}`) }}</span>
          <span class="flex shrink-0 items-center gap-1">
            <template v-for="(combo, ci) in s.keys" :key="ci">
              <span v-if="ci" class="text-[10px] text-txt3">/</span>
              <kbd v-for="k in combo" :key="k" class="ckbd">{{ formatKey(k) }}</kbd>
            </template>
          </span>
        </div>
      </div>
      <div class="mt-5 border-t border-line pt-3">
        <div class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-txt3">{{ t('canvas.shortcuts.mouse.title') }}</div>
        <ul class="space-y-1 text-[12px] text-txt2">
          <li v-for="m in MOUSE" :key="m">{{ t(`canvas.shortcuts.mouse.${m}`) }}</li>
        </ul>
      </div>
    </div>
  </div>
</template>
