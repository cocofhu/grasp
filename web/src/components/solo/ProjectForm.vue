<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import { api } from '@/lib/api/api'
import type { Project } from '@/lib/api/apiTypes'
import {
  draftFromProject,
  draftProblems,
  emptyDraft,
  inputFromDraft,
  isRepoUrl,
  repoNameFromUrl,
  type ProjectDraft,
} from '@/lib/solo/projectForm'
import { soloErrorText } from '@/lib/solo/soloErrors'

const props = defineProps<{ project?: Project | null }>()
const emit = defineEmits<{ (e: 'saved', p: Project): void; (e: 'cancel'): void }>()
const { t, te } = useI18n()

const draft = ref<ProjectDraft>(emptyDraft())
const saving = ref(false)
const error = ref('')
const touched = ref(false)

watch(
  () => props.project,
  (p) => {
    draft.value = p ? draftFromProject(p) : emptyDraft()
    error.value = ''
    touched.value = false
  },
  { immediate: true },
)

const dirPlaceholder = computed(() => repoNameFromUrl(draft.value.repoUrl) || t('pages.projects.form.repoDirPlaceholder'))
const problems = computed(() => draftProblems(draft.value))
const show = (key: string) => touched.value && problems.value.includes(key)

async function save() {
  touched.value = true
  error.value = ''
  if (problems.value.length) return
  saving.value = true
  try {
    const body = inputFromDraft(draft.value)
    const saved = props.project ? await api.updateProject(props.project.id, body) : await api.createProject(body)
    emit('saved', saved)
  } catch (e) {
    error.value = soloErrorText(e, t, te)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="project-form" data-testid="project-form" @submit.prevent="save">
    <section class="project-form__section">
      <h3 class="project-form__title">{{ t('pages.projects.form.sectionBasic') }}</h3>
      <label class="block">
        <span class="label">{{ t('pages.projects.form.name') }}</span>
        <input
          v-model="draft.name"
          class="input w-full"
          :class="{ 'project-form__bad': show('name') }"
          maxlength="80"
          :placeholder="t('pages.projects.form.namePlaceholder')"
          data-testid="project-form-name"
        />
      </label>
      <label class="block">
        <span class="label">{{ t('pages.projects.form.description') }}</span>
        <input v-model="draft.description" class="input w-full" :placeholder="t('pages.projects.form.descriptionPlaceholder')" />
      </label>
    </section>

    <section class="project-form__section">
      <h3 class="project-form__title">{{ t('pages.projects.form.sectionRepo') }}</h3>
      <label class="block">
        <span class="label">{{ t('pages.projects.form.repoUrl') }}</span>
        <input
          v-model="draft.repoUrl"
          class="input w-full font-mono"
          :class="{ 'project-form__bad': show('repoUrl') }"
          spellcheck="false"
          :placeholder="t('pages.projects.form.repoUrlPlaceholder')"
          data-testid="project-form-repo"
        />
        <span v-if="show('repoUrl')" class="project-form__err">
          {{ draft.repoUrl.trim() && !isRepoUrl(draft.repoUrl) ? t('pages.projects.form.repoUrlInvalid') : t('pages.projects.form.repoUrlRequired') }}
        </span>
      </label>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="block">
          <span class="label">{{ t('pages.projects.form.defaultBranch') }}</span>
          <input
            v-model="draft.defaultBranch"
            class="input w-full font-mono"
            :class="{ 'project-form__bad': show('branch') }"
            spellcheck="false"
            :placeholder="t('pages.projects.form.defaultBranchPlaceholder')"
          />
        </label>
        <label class="block">
          <span class="label">{{ t('pages.projects.form.repoDir') }}</span>
          <input
            v-model="draft.repoDirName"
            class="input w-full font-mono"
            :class="{ 'project-form__bad': show('repoDir') }"
            spellcheck="false"
            :placeholder="dirPlaceholder"
          />
        </label>
      </div>
      <p class="project-form__hint">
        {{ t('pages.projects.form.repoDirHint', { dir: draft.repoDirName.trim() || dirPlaceholder }) }}
      </p>
    </section>

    <div v-if="error" class="project-form__error" role="alert">{{ error }}</div>
    <div class="project-form__actions">
      <AppButton variant="ghost" type="button" @click="emit('cancel')">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="primary" type="submit" :loading="saving" data-testid="project-form-save">{{ t('pages.projects.form.save') }}</AppButton>
    </div>
  </form>
</template>

<style scoped>
.project-form {
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.project-form__section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.project-form__title {
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.project-form__hint {
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
  line-height: 1.55;
}
.project-form__err {
  display: block;
  margin-top: 4px;
  color: rgb(var(--c-err));
  font-size: 11px;
}
.project-form__bad {
  border-color: rgb(var(--c-err) / 0.55) !important;
}
.project-form__error {
  border: 1px solid rgb(var(--c-err) / 0.3);
  border-radius: 8px;
  background: rgb(var(--c-err) / 0.08);
  padding: 8px 10px;
  color: rgb(var(--c-err));
  font-size: 12px;
}
.project-form__actions {
  position: sticky;
  bottom: 0;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  border-top: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 12px 0 4px;
}
</style>
