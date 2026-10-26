<script setup lang="ts">
import { ref } from 'vue'
import SoloProjectSelect from '@/components/solo/SoloProjectSelect.vue'
import type { Project } from '@/lib/api/apiTypes'

const projects = [
  { id: 'grasp', name: 'GRASP', repoUrl: 'https://github.com/cocofhu/grasp.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
  { id: 'web', name: 'web', repoUrl: 'https://github.com/cocofhu/web.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
  { id: 'docs', name: 'docs', repoUrl: 'https://github.com/cocofhu/docs.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
] as Project[]

const ids = ref<string[]>(['grasp', 'web'])

function setRepos(next: string[]) {
  ids.value = [...next]
}

;(window as unknown as { __setRepos?: (next: string[]) => void }).__setRepos = setRepos
</script>

<template>
  <div class="roll-page">
    <SoloProjectSelect v-model="ids" :projects="projects" />
  </div>
</template>

<style>
html.light body {
  background: #fff;
}
.roll-page {
  padding: 48px;
}
</style>
