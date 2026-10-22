<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import { repoShort } from '@/lib/solo/soloPrefs'
import { repoAccessKey } from '@/lib/solo/projectForm'
import type { Project } from '@/lib/api/apiTypes'

const props = withDefaults(
  defineProps<{ projects: Project[]; modelValue: string; disabled?: boolean; loading?: boolean }>(),
  { disabled: false, loading: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void; (e: 'create'): void }>()
const { t } = useI18n()

const current = computed(() => props.projects.find((p) => p.id === props.modelValue) ?? null)

function matches(p: Project, q: string) {
  return !q || p.name.toLowerCase().includes(q) || repoShort(p.repoUrl).toLowerCase().includes(q)
}

function accessLabel(p: Project) {
  return t(`pages.solo.access.${repoAccessKey(p.gitCredentialType)}`)
}

function choose(id: string, close: () => void) {
  emit('update:modelValue', id)
  close()
}
</script>

<template>
  <SoloPickerShell
    :label="current?.name || t('pages.solo.projectSelect.none')"
    :placeholder="!current"
    icon="git"
    :disabled="disabled"
    :search-placeholder="projects.length > 5 ? t('pages.solo.projectSelect.search') : ''"
    :panel-width="360"
    test-id="solo-project-select"
  >
    <template #default="{ query, close }">
      <button
        type="button"
        role="option"
        data-picker-opt
        :aria-selected="!modelValue"
        data-testid="solo-project-option-none"
        @click="choose('', close)"
      >
        <Icon name="folder" :size="15" class="shrink-0 text-txt3" />
        <span class="min-w-0 flex-1">
          <span class="block truncate">{{ t('pages.solo.projectSelect.none') }}</span>
          <span class="block truncate text-[11px] text-txt3">{{ t('pages.solo.projectSelect.noneHint') }}</span>
        </span>
        <Icon v-if="!modelValue" name="check" :size="14" class="shrink-0 text-accent" />
      </button>
      <div v-if="projects.length" class="solo-picker-group">{{ t('pages.solo.projectSelect.group') }}</div>
      <button
        v-for="p in projects.filter((x) => matches(x, query))"
        :key="p.id"
        type="button"
        role="option"
        data-picker-opt
        :aria-selected="p.id === modelValue"
        :data-testid="`solo-project-option-${p.id}`"
        @click="choose(p.id, close)"
      >
        <Icon name="git" :size="15" class="shrink-0 text-accent" />
        <span class="min-w-0 flex-1">
          <span class="flex items-center gap-1.5">
            <span class="truncate">{{ p.name }}</span>
            <span v-if="p.gitCredentialType" class="solo-access-badge">{{ accessLabel(p) }}</span>
          </span>
          <span class="block truncate font-mono text-[11px] text-txt3">
            {{ repoShort(p.repoUrl) }}<template v-if="p.defaultBranch"> · {{ p.defaultBranch }}</template>
          </span>
        </span>
        <Icon v-if="p.id === modelValue" name="check" :size="14" class="shrink-0 text-accent" />
      </button>
      <div v-if="loading && !projects.length" class="solo-picker-empty">
        <Icon name="spinner" :size="14" class="inline animate-spin" />
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
