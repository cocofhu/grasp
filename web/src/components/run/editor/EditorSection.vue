<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from '@/components/ui/Icon.vue'

const props = withDefaults(
  defineProps<{
    title: string
    path?: string
    count?: number | null
    required?: boolean
    collapsed?: boolean
    invalid?: boolean
  }>(),
  { path: '', count: null, required: false, collapsed: false, invalid: false },
)

const open = ref(!props.collapsed)
watch(
  () => props.invalid,
  (v) => {
    if (v) open.value = true
  },
)
</script>

<template>
  <section
    class="ed-section"
    :class="{ 'ed-section--invalid': invalid }"
    :data-field-path="path || undefined"
    data-testid="artifact-editor-section"
  >
    <button
      type="button"
      class="ed-section__head"
      :aria-expanded="open"
      @click="open = !open"
    >
      <Icon :name="open ? 'chevron-down' : 'chevron-right'" class="ed-section__chev" />
      <span class="ed-section__title">{{ title }}</span>
      <span v-if="required" class="ed-req" aria-hidden="true">*</span>
      <span v-if="count != null" class="ed-section__count">{{ count }}</span>
    </button>
    <div v-show="open" class="ed-section__body">
      <slot />
    </div>
  </section>
</template>
