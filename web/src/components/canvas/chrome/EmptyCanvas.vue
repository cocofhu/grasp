<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'

const emit = defineEmits<{ (e: 'template'): void; (e: 'blank'): void }>()
const { t } = useI18n()
const STEPS = ['clarify', 'implement', 'test_review'] as const
</script>

<template>
  <div class="pointer-events-none absolute inset-0 z-[5] flex flex-col items-center justify-center px-6" data-testid="empty-canvas">
    <h2 class="mb-5 text-[15px] font-semibold text-txt">{{ t('canvas.empty.title') }}</h2>
    <div class="pointer-events-auto grid w-full max-w-[600px] grid-cols-1 gap-3 sm:grid-cols-2">
      <button
        type="button"
        class="cchrome group flex flex-col items-start gap-3 p-4 text-left transition hover:border-accent"
        data-testid="empty-canvas-template"
        @click="emit('template')"
      >
        <span class="flex items-center gap-1.5" aria-hidden="true">
          <template v-for="(s, i) in STEPS" :key="s">
            <span v-if="i" class="h-px w-3 bg-line-strong" />
            <span class="rounded-md border border-line bg-elevated px-1.5 py-0.5 text-[10.5px] text-txt2">{{ t(`canvas.template.labels.${s}`) }}</span>
          </template>
        </span>
        <span>
          <span class="block text-[13px] font-semibold text-txt group-hover:text-accent-2">{{ t('canvas.empty.templateTitle') }}</span>
          <span class="mt-1 block text-[12px] leading-5 text-txt3">{{ t('canvas.empty.templateDesc') }}</span>
        </span>
      </button>
      <button
        type="button"
        class="cchrome group flex flex-col items-start gap-3 p-4 text-left transition hover:border-accent"
        data-testid="empty-canvas-blank"
        @click="emit('blank')"
      >
        <span class="flex h-[22px] items-center gap-1.5 text-txt3" aria-hidden="true">
          <Icon name="input" :size="15" /><span class="h-px w-10 border-t border-dashed border-line-strong" /><Icon name="output" :size="15" />
        </span>
        <span>
          <span class="block text-[13px] font-semibold text-txt group-hover:text-accent-2">{{ t('canvas.empty.blankTitle') }}</span>
          <span class="mt-1 block text-[12px] leading-5 text-txt3">{{ t('canvas.empty.blankDesc') }}</span>
        </span>
      </button>
    </div>
    <p class="mt-4 flex items-center gap-1.5 text-[12px] text-txt3">
      <kbd class="ckbd">/</kbd>{{ t('canvas.empty.hint') }}
    </p>
  </div>
</template>
