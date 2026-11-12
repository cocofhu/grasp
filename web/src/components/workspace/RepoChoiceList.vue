<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import Icon from '@/components/ui/Icon.vue'
import type { Project, RepoChoice } from '@/lib/api/apiTypes'
import { repoShort } from '@/lib/solo/soloPrefs'

const props = withDefaults(
  defineProps<{
    projects: Project[]
    modelValue: RepoChoice[]
    disabled?: boolean
    loading?: boolean
    /** Hides the link to repository settings (e.g. inside a launch dialog). */
    hideManageLink?: boolean
  }>(),
  { disabled: false, loading: false, hideManageLink: false },
)
const emit = defineEmits<{ 'update:modelValue': [value: RepoChoice[]] }>()
const { t } = useI18n()
const copy = (key: string, params?: Record<string, unknown>) => t(`common.repoChoices.${key}`, params ?? {})

const picked = computed(() => new Map(props.modelValue.map((choice) => [choice.projectId, choice])))
/** Chosen repositories first in clone order, then the rest; deleted ones stay visible so they can be removed. */
const rows = computed(() => {
  const byId = new Map(props.projects.map((project) => [project.id, project]))
  const chosen = props.modelValue.map((choice) => ({ id: choice.projectId, project: byId.get(choice.projectId) }))
  const rest = props.projects.filter((project) => !picked.value.has(project.id)).map((project) => ({ id: project.id, project }))
  return [...chosen, ...rest]
})

function toggle(id: string, on: boolean) {
  if (props.disabled) return
  emit('update:modelValue', on ? [...props.modelValue, { projectId: id }] : props.modelValue.filter((choice) => choice.projectId !== id))
}

function setBranch(id: string, branch: string) {
  emit(
    'update:modelValue',
    props.modelValue.map((choice) => {
      if (choice.projectId !== id) return choice
      const value = branch.trim()
      return value ? { projectId: id, branch: value } : { projectId: id }
    }),
  )
}
</script>

<template>
  <div class="space-y-2" data-testid="repo-choice-list">
    <p v-if="loading" class="text-[12px] text-txt3">{{ copy('loading') }}</p>
    <p v-else-if="!rows.length" class="rounded-md border border-dashed border-line px-3 py-4 text-center text-[12px] text-txt3">
      {{ copy('empty') }}
    </p>
    <div v-else class="divide-y divide-line rounded-md border border-line">
      <div
        v-for="row in rows"
        :key="row.id"
        class="flex min-w-0 flex-wrap items-center gap-2 px-3 py-2"
        :data-testid="`repo-choice-${row.id}`"
      >
        <div class="flex min-w-0 flex-1 items-center gap-2" :class="{ 'cursor-pointer': !disabled }" @click="toggle(row.id, !picked.has(row.id))">
          <Icon name="git" :size="13" class="shrink-0 text-txt3" />
          <span class="min-w-0 truncate text-[12.5px] text-txt" :class="{ 'text-err': !row.project }">
            {{ row.project?.name || copy('deleted') }}
          </span>
          <span v-if="row.project" class="min-w-0 truncate font-mono text-[11px] text-txt3" :title="row.project.repoUrl">
            {{ repoShort(row.project.repoUrl) }}
          </span>
        </div>
        <input
          v-if="picked.has(row.id) && row.project"
          :value="picked.get(row.id)?.branch || ''"
          :disabled="disabled"
          class="input w-36 shrink-0 font-mono text-[12px]"
          spellcheck="false"
          :placeholder="row.project.defaultBranch || copy('defaultBranch')"
          :aria-label="copy('branch', { name: row.project.name })"
          :data-testid="`repo-choice-branch-${row.id}`"
          @change="setBranch(row.id, ($event.target as HTMLInputElement).value)"
        />
        <AppSwitch
          :model-value="picked.has(row.id)"
          :disabled="disabled"
          :aria-label="row.project?.name || copy('deleted')"
          :data-testid="`repo-choice-toggle-${row.id}`"
          @update:model-value="toggle(row.id, $event)"
        />
      </div>
    </div>
    <RouterLink v-if="!hideManageLink" to="/settings/projects" class="inline-block text-[11px] text-accent-2">
      {{ copy('manage') }}
    </RouterLink>
  </div>
</template>
