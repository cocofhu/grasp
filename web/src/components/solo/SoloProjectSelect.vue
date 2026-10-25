<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import SoloBranchChip from './SoloBranchChip.vue'
import { repoShort } from '@/lib/solo/soloPrefs'
import { repoTriggerLabel } from '@/lib/solo/sessionRepos'
import { repoAccessKey } from '@/lib/solo/projectForm'
import type { Project } from '@/lib/api/apiTypes'

const props = withDefaults(
  defineProps<{
    projects?: Project[]
    modelValue?: string[]
    branches?: Record<string, string>
    disabled?: boolean
    loading?: boolean
  }>(),
  { projects: () => [], modelValue: () => [], branches: () => ({}), disabled: false, loading: false },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string[]): void
  (e: 'update:branch', id: string, branch: string): void
  (e: 'create'): void
}>()
const { t } = useI18n()

const selected = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []))

const label = computed(() =>
  repoTriggerLabel(selected.value, (id) => props.projects.find((p) => p.id === id)?.name || '', t('pages.solo.projectSelect.none')),
)

function matches(p: Project, q: string) {
  return !q || p.name.toLowerCase().includes(q) || repoShort(p.repoUrl).toLowerCase().includes(q)
}

function accessLabel(p: Project) {
  return t(`pages.solo.access.${repoAccessKey(p.gitCredentialType)}`)
}

function isOn(id: string) {
  return selected.value.includes(id)
}

function toggle(id: string) {
  const current = selected.value
  emit('update:modelValue', current.includes(id) ? current.filter((x) => x !== id) : [...current, id])
}

function clear() {
  emit('update:modelValue', [])
}

function onRowKeydown(e: KeyboardEvent, id: string) {
  if (e.target instanceof HTMLInputElement) return
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    toggle(id)
  }
}
</script>

<template>
  <SoloPickerShell
    :label="label"
    :placeholder="!selected.length"
    icon="git"
    multiple
    :disabled="disabled"
    :search-placeholder="projects.length > 5 ? t('pages.solo.projectSelect.search') : ''"
    :panel-width="360"
    test-id="solo-project-select"
  >
    <template #default="{ query }">
      <button
        type="button"
        role="option"
        data-picker-opt
        :aria-selected="!selected.length"
        data-testid="solo-project-option-none"
        @click="clear"
      >
        <span class="solo-project-box" />
        <Icon name="folder" :size="15" class="shrink-0 text-txt3" />
        <span class="min-w-0 flex-1">
          <span class="block truncate">{{ t('pages.solo.projectSelect.none') }}</span>
          <span class="block truncate text-[11px] text-txt3">{{ t('pages.solo.projectSelect.noneHint') }}</span>
        </span>
        <Icon v-if="!selected.length" name="check" :size="14" class="shrink-0 text-accent" />
      </button>
      <div v-if="projects.length" class="solo-picker-group">{{ t('pages.solo.projectSelect.group') }}</div>
      <div
        v-for="p in projects.filter((x) => matches(x, query))"
        :key="p.id"
        role="option"
        tabindex="0"
        data-picker-opt
        class="solo-project-opt"
        :aria-selected="isOn(p.id)"
        :data-testid="`solo-project-option-${p.id}`"
        @click="toggle(p.id)"
        @keydown="onRowKeydown($event, p.id)"
      >
        <span class="solo-project-box" />
        <Icon name="git" :size="15" class="mt-0.5 shrink-0 text-accent" />
        <span class="min-w-0 flex-1">
          <span class="flex items-center gap-1.5">
            <span class="truncate">{{ p.name }}</span>
            <span v-if="p.gitCredentialType" class="solo-access-badge">{{ accessLabel(p) }}</span>
          </span>
          <span class="block truncate font-mono text-[11px] text-txt3">
            {{ repoShort(p.repoUrl) }}<template v-if="!isOn(p.id) && p.defaultBranch"> · {{ p.defaultBranch }}</template>
          </span>
          <span v-if="isOn(p.id)" class="mt-1.5 inline-flex" @click.stop @keydown.stop>
            <SoloBranchChip
              :model-value="branches[p.id] || ''"
              :default-branch="p.defaultBranch || ''"
              @update:model-value="emit('update:branch', p.id, $event)"
            />
          </span>
        </span>
        <Icon v-if="isOn(p.id)" name="check" :size="14" class="mt-0.5 shrink-0 text-accent" />
      </div>
      <div v-if="loading && !projects.length" class="solo-picker-empty">
        <Icon name="spinner" :size="14" class="inline" />
      </div>
      <div v-else-if="projects.length && !projects.some((x) => matches(x, query))" class="solo-picker-empty">
        {{ t('pages.solo.projectSelect.empty') }}
      </div>
    </template>
    <template #footer="{ close }">
      <button type="button" data-picker-opt data-testid="solo-project-create" @click="close(false); emit('create')">
        <Icon name="plus" :size="14" class="shrink-0 text-txt2" />
        <span>{{ t('pages.solo.projectSelect.create') }}</span>
      </button>
      <RouterLink to="/settings/projects" data-picker-opt class="text-txt2" @click="close(false)">
        <Icon name="settings" :size="14" class="shrink-0 text-txt3" />
        <span>{{ t('pages.solo.projectSelect.manage') }}</span>
      </RouterLink>
    </template>
  </SoloPickerShell>
</template>

<style scoped>
.solo-project-opt.solo-project-opt {
  align-items: flex-start;
}
.solo-project-box {
  width: 14px;
  height: 14px;
  margin-top: 3px;
  flex: none;
  border: 1px solid rgb(var(--c-txt3));
  border-radius: 3px;
}
.solo-project-opt[aria-selected='true'] .solo-project-box,
[data-testid='solo-project-option-none'][aria-selected='true'] .solo-project-box {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
  box-shadow: inset 0 0 0 2px rgb(var(--c-elevated));
}
.solo-access-badge {
  flex-shrink: 0;
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
  padding: 0 6px;
  color: rgb(var(--c-txt3));
  font-size: 10px;
  line-height: 16px;
}
</style>
