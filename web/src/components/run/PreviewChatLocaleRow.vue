<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { locale, setLocale, type AppLocale } from '@/lib/shared/locale'

const { t } = useI18n()
const labelId = useId()

const options: { value: AppLocale; label: string; testId: string }[] = [
  { value: 'en', label: 'English', testId: 'preview-chat-locale-en' },
  { value: 'zh-CN', label: '中文', testId: 'preview-chat-locale-zh' },
]

function choose(next: AppLocale) {
  void setLocale(next)
}
</script>

<template>
  <div class="px-3 py-2" data-testid="preview-chat-locale">
    <div class="flex items-center justify-between gap-3 text-xs font-medium text-txt2">
      <span :id="labelId">{{ t('pages.embedChat.language') }}</span>
      <div
        class="inline-flex shrink-0 overflow-hidden rounded-md border border-line bg-surface"
        role="listbox"
        :aria-labelledby="labelId"
        data-testid="preview-chat-locale-options"
      >
        <button
          v-for="(option, index) in options"
          :key="option.value"
          type="button"
          role="option"
          class="bg-transparent px-2.5 py-1.5 text-xs font-medium"
          :class="[
            index > 0 ? 'border-l border-line' : '',
            locale === option.value ? 'bg-accent-dim text-txt' : 'text-txt2',
          ]"
          :aria-selected="locale === option.value"
          :data-testid="option.testId"
          @click="choose(option.value)"
        >
          {{ option.label }}
        </button>
      </div>
    </div>
  </div>
</template>
