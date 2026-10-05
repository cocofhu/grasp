<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import NodeShell from './NodeShell.vue'
import { agentHueVar, agentInitial } from '../composables/agentAvatar'
import { AGENT_NODE_WIDTH } from '../composables/useAutoLayout'
import type { CanvasNodeData } from '../composables/canvasContext'

const props = defineProps<{ id: string; data: CanvasNodeData; selected?: boolean }>()
const { t } = useI18n()

const hue = computed(() => (props.data.agentName ? agentHueVar(props.data.agentName) : ''))
const avatarStyle = computed(() =>
  hue.value
    ? { background: `rgb(var(${hue.value}) / 0.16)`, color: `rgb(var(${hue.value}))` }
    : { background: 'rgb(var(--c-elevated))', color: 'rgb(var(--c-txt3))' },
)
const subtitle = computed(() => {
  if (!props.data.agentName) return t('canvas.node.noAgent')
  if (props.data.agentMissing) return t('canvas.node.agentMissing', { name: props.data.agentName })
  return props.data.agentName
})
const caps = computed(() => {
  const f = props.data.flags
  return [
    { key: 'ask', on: !!f?.ask, icon: 'chat', label: t('canvas.caps.ask') },
    { key: 'preview', on: !!f?.preview, icon: 'monitor', label: t('canvas.caps.previewFull') },
    { key: 'review', on: !!f?.review, icon: 'eye-off', label: t('canvas.caps.reviewFull') },
    { key: 'gate', on: !!f?.gate, icon: 'gate', label: t('canvas.caps.gateFull') },
  ].filter((c) => c.on)
})
</script>

<template>
  <NodeShell v-memo="[data, selected]" :id="id" :data="data" :selected="selected" :width="AGENT_NODE_WIDTH">
    <template #icon>
      <span class="cnode-avatar" :style="avatarStyle" aria-hidden="true">
        <template v-if="data.agentName">{{ agentInitial(data.agentName) }}</template>
        <Icon v-else name="robot" :size="15" />
      </span>
    </template>
    <template #sub>
      <span :class="{ 'text-warn': !data.agentName || data.agentMissing }">{{ subtitle }}</span>
    </template>
    <div class="cnode-body">
      <p class="cnode-goal" :class="{ 'is-empty': !data.goal }" data-testid="canvas-node-goal">
        {{ data.goal || t('canvas.node.goalEmpty') }}
      </p>
    </div>
    <div v-if="caps.length" class="cnode-foot">
      <span
        v-for="c in caps"
        :key="c.key"
        class="cnode-cap is-on"
        :title="c.label"
        :aria-label="c.label"
        :data-testid="`canvas-cap-${c.key}`"
      >
        <Icon :name="c.icon" :size="12" />
      </span>
    </div>
  </NodeShell>
</template>
