<script setup lang="ts">
defineProps<{
  title: string
  hint?: string
  /** Mode tabs rendered under the title. */
  modes?: { id: string; label: string; disabled?: boolean; testId?: string }[]
  mode?: string
}>()
const emit = defineEmits<{ (e: 'update:mode', v: string): void }>()
</script>

<template>
  <section class="token-chart-card flex min-w-0 flex-col rounded-xl border border-line bg-surface p-3.5 shadow-sm">
    <div class="flex flex-wrap items-start justify-between gap-2">
      <h2 class="m-0 min-w-0 text-sm font-semibold">
        {{ title }}
        <em v-if="hint" class="ml-2 text-[11px] font-normal not-italic text-txt3">{{ hint }}</em>
      </h2>
      <div v-if="$slots.actions" class="flex shrink-0 items-center gap-1.5">
        <slot name="actions" />
      </div>
    </div>
    <div v-if="modes?.length" class="mt-2 flex flex-wrap gap-1">
      <button
        v-for="m in modes"
        :key="m.id"
        type="button"
        class="rounded-md px-2.5 py-1 text-xs transition-colors"
        :class="[
          mode === m.id ? 'bg-elevated font-semibold text-txt' : 'text-txt3 hover:text-txt2',
          m.disabled ? 'cursor-not-allowed opacity-40' : '',
        ]"
        :disabled="m.disabled"
        :data-testid="m.testId"
        @click="emit('update:mode', m.id)"
      >
        {{ m.label }}
      </button>
    </div>
    <slot />
  </section>
</template>
