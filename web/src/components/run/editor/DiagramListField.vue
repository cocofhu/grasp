<script setup lang="ts">
import type { FieldDesc } from '@/lib/run/artifactForms'
import ObjectListField from './ObjectListField.vue'

defineProps<{ desc: FieldDesc; path: string }>()
const model = defineModel<Record<string, unknown>[]>({ default: () => [] })
</script>

<template>
  <ObjectListField v-model="model" :desc="desc" :path="path" variant="diagram" />
</template>
