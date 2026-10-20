<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  hint?: string
  headingLevel?: 2 | 3
  /** Mode tabs rendered under the title. */
  modes?: { id: string; label: string; disabled?: boolean; testId?: string }[]
  mode?: string
}>(), { headingLevel: 2 })
const emit = defineEmits<{ (e: 'update:mode', v: string): void }>()
</script>

<template>
  <section class="token-chart-card flex min-w-0 flex-col border-t border-line/60 py-5">
    <div class="flex flex-wrap items-start justify-between gap-x-5 gap-y-3">
      <div class="min-w-0 flex-1">
        <component :is="`h${headingLevel}`" class="m-0 text-sm font-semibold leading-5 text-txt">{{ title }}</component>
        <p v-if="hint" class="mb-0 mt-1.5 max-w-3xl text-xs leading-5 text-txt3">{{ hint }}</p>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 flex-wrap items-center gap-2">
        <slot name="actions" />
      </div>
    </div>
    <div v-if="modes?.length" class="mt-4 flex flex-wrap gap-1" role="group" :aria-label="title">
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
        :aria-pressed="mode === m.id"
        :data-testid="m.testId"
        @click="emit('update:mode', m.id)"
      >
        {{ m.label }}
      </button>
    </div>
    <slot />
  </section>
</template>
