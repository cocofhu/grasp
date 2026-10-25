<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import { agentHueVar, agentInitial } from '@/components/canvas/composables/agentAvatar'
import { backendLabel } from '@/lib/solo/soloPrefs'
import type { Agent } from '@/lib/api/apiTypes'

const props = withDefaults(
  defineProps<{ agents: Agent[]; modelValue: string; disabled?: boolean; loading?: boolean; invalid?: boolean }>(),
  { disabled: false, loading: false, invalid: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
const { t } = useI18n()

const current = computed(() => props.agents.find((a) => a.name === props.modelValue) ?? null)

function groups(q: string) {
  const out = new Map<string, Agent[]>()
  for (const a of props.agents) {
    const label = backendLabel(a.acpBackend)
    if (q && !a.name.toLowerCase().includes(q) && !label.toLowerCase().includes(q)) continue
    const list = out.get(label) ?? []
    list.push(a)
    out.set(label, list)
  }
  return [...out.entries()]
}

function avatar(name: string) {
  const v = agentHueVar(name)
  return { background: `rgb(var(${v}) / 0.16)`, color: `rgb(var(${v}))` }
}

function choose(name: string, close: () => void) {
  emit('update:modelValue', name)
  close()
}
</script>

<template>
  <SoloPickerShell
    :label="current?.name || t('pages.solo.agentSelect.placeholder')"
    :placeholder="!current"
    :invalid="invalid"
    :disabled="disabled"
    :search-placeholder="t('pages.solo.agentSelect.search')"
    test-id="solo-agent-select"
  >
    <template #trigger>
      <span v-if="current" class="solo-agent-avatar" :style="avatar(current.name)">{{ agentInitial(current.name) }}</span>
      <Icon v-else name="robot" :size="14" class="shrink-0 text-accent" />
    </template>
    <template #default="{ query, close }">
      <div v-if="loading && !agents.length" class="solo-picker-empty">
        <Icon name="spinner" :size="14" class="mr-1 inline" />
      </div>
      <div v-else-if="!agents.length" class="solo-picker-empty">{{ t('pages.solo.agentSelect.noAgents') }}</div>
      <template v-else>
        <template v-for="[group, list] in groups(query)" :key="group">
          <div class="solo-picker-group">{{ group }}</div>
          <button
            v-for="a in list"
            :key="a.name"
            type="button"
            role="option"
            data-picker-opt
            :aria-selected="a.name === modelValue"
            :data-testid="`solo-agent-option-${a.name}`"
            @click="choose(a.name, close)"
          >
            <span class="solo-agent-avatar" :style="avatar(a.name)">{{ agentInitial(a.name) }}</span>
            <span class="min-w-0 flex-1 truncate">{{ a.name }}</span>
            <Icon v-if="a.name === modelValue" name="check" :size="14" class="shrink-0 text-accent" />
          </button>
        </template>
        <div v-if="!groups(query).length" class="solo-picker-empty">{{ t('pages.solo.agentSelect.empty') }}</div>
      </template>
    </template>
    <template #footer="{ close }">
      <RouterLink to="/agents" data-picker-opt class="text-txt2" @click="close(false)">
        <Icon name="settings" :size="14" class="shrink-0 text-txt3" />
        <span>{{ t('pages.solo.agentSelect.manage') }}</span>
      </RouterLink>
    </template>
  </SoloPickerShell>
</template>

<style scoped>
.solo-agent-avatar {
  display: inline-flex;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  font-size: 10.5px;
  font-weight: 700;
}
</style>
