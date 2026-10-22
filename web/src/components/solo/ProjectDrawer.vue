<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import ProjectForm from './ProjectForm.vue'
import type { Project } from '@/lib/api/apiTypes'

defineProps<{ open: boolean; project?: Project | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved', p: Project): void }>()
const { t } = useI18n()
</script>

<template>
  <AppDrawer :open="open" :title="project ? t('pages.projects.edit') : t('pages.projects.new')" :width="520" @close="emit('close')">
    <div class="px-5 pt-4">
      <ProjectForm v-if="open" :project="project" @cancel="emit('close')" @saved="emit('saved', $event)" />
    </div>
  </AppDrawer>
</template>
