<script setup lang="ts">
import { computed, ref } from 'vue'
import StageWorkflow from '@/components/run/StageWorkflow.vue'
import { setTheme } from '@/lib/shared/theme'
import type { Run } from '@/lib/shared/types'

const scene = ref<'run' | 'hold' | 'fail' | 'done'>('run')
const themeName = ref<'light' | 'dark'>('light')
setTheme('light')

const nodes = [
  { id: 'start', type: 'input' as const, label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'clarify', type: 'agent' as const, label: '需求澄清', position: { x: 0, y: 0 }, config: {} },
  { id: 'impl', type: 'agent' as const, label: '实现', position: { x: 0, y: 0 }, config: {} },
  { id: 'review', type: 'agent' as const, label: '测试评审', position: { x: 0, y: 0 }, config: {} },
  { id: 'ship', type: 'agent' as const, label: '交付', position: { x: 0, y: 0 }, config: {} },
  { id: 'end', type: 'output' as const, label: '结束', position: { x: 0, y: 0 }, config: {} },
]

function statuses(current: string): Record<string, { nodeId: string; status: string }> {
  const order = ['start', 'clarify', 'impl', 'review', 'ship', 'end']
  const at = order.indexOf(current)
  return Object.fromEntries(order.map((id, index) => {
    let status = 'pending'
    if (index < at) status = 'completed'
    else if (index === at) status = scene.value === 'run' ? 'running' : scene.value === 'hold' ? 'waiting_human' : scene.value === 'fail' ? 'failed' : 'completed'
    else if (scene.value === 'done') status = 'completed'
    return [id, { nodeId: id, status }]
  }))
}

const currentId = computed(() => (scene.value === 'hold' ? 'clarify' : scene.value === 'done' ? 'impl' : 'impl'))

const run = computed(() => ({
  id: 'preview',
  nodes,
  nodeRuns: statuses(currentId.value),
} as unknown as Run))

function pick(next: 'run' | 'hold' | 'fail' | 'done') {
  scene.value = next
}

function toggleTheme() {
  themeName.value = themeName.value === 'light' ? 'dark' : 'light'
  setTheme(themeName.value)
}
</script>

<template>
  <div class="preview-page">
    <div class="preview-stage">
      <StageWorkflow :key="scene" :run="run" :current-node-id="currentId" />
    </div>
    <div class="preview-switch">
      <button type="button" :aria-pressed="scene === 'run'" @click="pick('run')">运行中</button>
      <button type="button" :aria-pressed="scene === 'hold'" @click="pick('hold')">等待人工</button>
      <button type="button" :aria-pressed="scene === 'fail'" @click="pick('fail')">失败</button>
      <button type="button" :aria-pressed="scene === 'done'" @click="pick('done')">已完成</button>
      <button type="button" @click="toggleTheme">{{ themeName === 'light' ? '深色' : '浅色' }}</button>
    </div>
  </div>
</template>

<style scoped>
.preview-page {
  min-height: 100vh;
  padding: 24px;
  background: var(--ra-bg, #fafafb);
}
.preview-stage {
  width: min(440px, 100%);
  padding: 16px;
  border: 1px solid var(--ra-line);
  border-radius: 12px;
  background: var(--ra-surface);
  --ra-bg: light-dark(#f8f9fc, #181a21);
  --ra-surface: light-dark(#fff, #242630);
  --ra-ink: light-dark(#262934, #eeeff6);
  --ra-muted: light-dark(#7c8193, #a5abbd);
  --ra-line: light-dark(#e5e8f0, #3b3f4e);
  --ra-accent: light-dark(#7b67da, #b3a3ef);
  --ra-green: light-dark(#4a9271, #88c6a6);
}
.preview-switch { display: flex; gap: 8px; margin-top: 16px; }
.preview-switch button { border: 1px solid #d4d4d8; border-radius: 999px; background: #fff; padding: 4px 10px; font: inherit; font-size: 12px; }
</style>
