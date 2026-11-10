<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { ComposerCommand } from '@/lib/chat/useComposerCommands'

defineProps<{
  id: string
  commands: ComposerCommand[]
  selected: number
  testPrefix: string
  placement?: 'up' | 'down'
}>()
const emit = defineEmits<{ select: [command: ComposerCommand] }>()
const { t } = useI18n()
</script>

<template>
  <div
    :id="id"
    role="listbox"
    :aria-label="t('pages.solo.chat.commands')"
    :data-testid="`${testPrefix}-command-menu`"
    class="absolute left-0 right-0 z-40 rounded-xl border border-line-strong bg-elevated p-1.5 shadow-xl"
    :class="placement === 'down' ? 'top-[calc(100%+8px)]' : 'bottom-[calc(100%+8px)]'"
  >
    <button
      v-for="(command, index) in commands"
      :id="`${id}-${command.id}`"
      :key="command.id"
      type="button"
      role="option"
      :aria-selected="index === selected"
      tabindex="-1"
      :data-testid="`${testPrefix}-command-${command.id}`"
      class="flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left text-txt hover:bg-accent-dim"
      :class="{ 'bg-accent-dim': index === selected }"
      @pointerdown.prevent
      @click="emit('select', command)"
    >
      <Icon name="workflow" :size="18" class="mt-0.5 shrink-0 text-txt2" />
      <span class="min-w-0 flex-1">
        <span class="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[13px]">
          <span class="font-mono">/{{ command.id }}</span>
          <span>{{ command.label }}</span>
        </span>
        <span class="mt-1 block text-[12px] text-txt3">{{ command.description }}</span>
      </span>
    </button>
  </div>
</template>
