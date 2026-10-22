<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { api } from '@/lib/api/api'
import type { CredentialItem, GitCredentialType, Project } from '@/lib/api/apiTypes'
import {
  GIT_CREDENTIAL_ENV,
  SSH_HOSTS_ENV,
  accessMismatch,
  bindingKeyProblem,
  credentialsFor,
  draftFromProject,
  draftProblems,
  emptyDraft,
  envKeyProblem,
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
const credentials = ref<CredentialItem[]>([])
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

onMounted(async () => {
  try {
    credentials.value = (await api.listCredentials()).items ?? []
  } catch {
    credentials.value = []
  }
})

const ACCESS: GitCredentialType[] = ['', 'github_https', 'gitlab_https', 'ssh']

function credOptions(envKey: string) {
  return credentialsFor(credentials.value, envKey).map((c) => ({
    value: c.id,
    label: c.name,
    hint: [c.envKey, c.masked].filter(Boolean).join(' · '),
  }))
}

const gitCredOptions = computed(() => {
  const type = draft.value.gitCredentialType
  return type ? credOptions(GIT_CREDENTIAL_ENV[type]) : []
})
const hostsOptions = computed(() => [{ value: '', label: t('pages.projects.form.hostsNone') }, ...credOptions(SSH_HOSTS_ENV)])
const bindingOptions = computed(() =>
  credentials.value.filter((c) => c.enabled !== false && !c.revokedAt).map((c) => ({ value: c.id, label: c.name, hint: c.envKey || '' })),
)

const dirPlaceholder = computed(() => repoNameFromUrl(draft.value.repoUrl) || t('pages.projects.form.repoDirPlaceholder'))
const mismatch = computed(() => accessMismatch(draft.value.repoUrl, draft.value.gitCredentialType))
const problems = computed(() => draftProblems(draft.value))
const show = (key: string) => touched.value && problems.value.includes(key)

function keyHint(problem: string) {
  return problem ? t(`pages.projects.form.keyProblem.${problem}`) : ''
}

function setAccess(type: GitCredentialType) {
  if (draft.value.gitCredentialType === type) return
  draft.value.gitCredentialType = type
  const opts = type ? credOptions(GIT_CREDENTIAL_ENV[type]) : []
  draft.value.gitCredentialId = opts.length === 1 ? opts[0]!.value : ''
  if (type !== 'ssh') draft.value.sshHostsCredentialId = ''
}

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

    <section class="project-form__section">
      <h3 class="project-form__title">{{ t('pages.projects.form.sectionAccess') }}</h3>
      <div class="project-form__access" role="radiogroup" :aria-label="t('pages.projects.form.sectionAccess')">
        <button
          v-for="type in ACCESS"
          :key="type || 'none'"
          type="button"
          role="radio"
          class="project-form__access-opt"
          :class="{ 'project-form__access-opt--on': draft.gitCredentialType === type }"
          :aria-checked="draft.gitCredentialType === type"
          :data-testid="`project-form-access-${type || 'none'}`"
          @click="setAccess(type)"
        >
          <span class="font-medium text-txt">{{ t(`pages.solo.access.${type || 'none'}`) }}</span>
          <span class="text-[11px] leading-snug text-txt3">{{ t(`pages.projects.form.accessHint.${type || 'none'}`) }}</span>
        </button>
      </div>
      <div v-if="mismatch" class="project-form__warn" data-testid="project-form-mismatch">
        <Icon name="alert" :size="13" class="mt-0.5 shrink-0" />
        <span>{{ t(`pages.projects.form.mismatch.${mismatch}`) }}</span>
      </div>
      <template v-if="draft.gitCredentialType">
        <label class="block">
          <span class="label">{{ t('pages.projects.form.credential') }}</span>
          <AppSelect
            v-model="draft.gitCredentialId"
            :options="gitCredOptions"
            :invalid="show('credential')"
            searchable
            :placeholder="t('pages.projects.form.credentialPlaceholder')"
            :aria-label="t('pages.projects.form.credential')"
          />
        </label>
        <label v-if="draft.gitCredentialType === 'ssh'" class="block">
          <span class="label">{{ t('pages.projects.form.hosts') }}</span>
          <AppSelect v-model="draft.sshHostsCredentialId" :options="hostsOptions" :aria-label="t('pages.projects.form.hosts')" />
        </label>
        <p v-if="!gitCredOptions.length" class="project-form__hint">
          {{ t('pages.projects.form.noCredential') }}
          <RouterLink to="/settings/credentials" class="text-accent-2 hover:underline">{{ t('pages.projects.form.manageCredentials') }}</RouterLink>
        </p>
      </template>
    </section>

    <section class="project-form__section">
      <h3 class="project-form__title">{{ t('pages.projects.form.sectionEnv') }}</h3>
      <p class="project-form__hint">{{ t('pages.projects.form.envHint') }}</p>
      <div v-for="(row, i) in draft.env" :key="`env-${i}`" class="project-form__kv">
        <div class="min-w-0 flex-1">
          <input
            v-model="row.k"
            class="input w-full font-mono"
            :class="{ 'project-form__bad': !!envKeyProblem(row.k) }"
            spellcheck="false"
            :placeholder="t('pages.projects.form.envKey')"
            :data-testid="`project-form-env-key-${i}`"
          />
          <span v-if="envKeyProblem(row.k)" class="project-form__err">{{ keyHint(envKeyProblem(row.k)) }}</span>
        </div>
        <input v-model="row.v" class="input min-w-0 flex-1 font-mono" spellcheck="false" :placeholder="t('pages.projects.form.envValue')" />
        <button type="button" class="project-form__del" :aria-label="t('common.buttons.delete')" @click="draft.env.splice(i, 1)">
          <Icon name="trash" :size="14" />
        </button>
      </div>
      <button type="button" class="project-form__add" data-testid="project-form-add-env" @click="draft.env.push({ k: '', v: '' })">
        <Icon name="plus" :size="13" />{{ t('pages.projects.form.addEnv') }}
      </button>
    </section>

    <section class="project-form__section">
      <h3 class="project-form__title">{{ t('pages.projects.form.sectionBindings') }}</h3>
      <p class="project-form__hint">{{ t('pages.projects.form.bindingsHint') }}</p>
      <div v-for="(row, i) in draft.bindings" :key="`bind-${i}`" class="project-form__kv">
        <div class="min-w-0 flex-1">
          <input
            v-model="row.k"
            class="input w-full font-mono"
            :class="{ 'project-form__bad': !!bindingKeyProblem(row.k) }"
            spellcheck="false"
            :placeholder="t('pages.projects.form.bindingKey')"
          />
          <span v-if="bindingKeyProblem(row.k)" class="project-form__err">{{ keyHint(bindingKeyProblem(row.k)) }}</span>
        </div>
        <div class="min-w-0 flex-1">
          <AppSelect
            v-model="row.v"
            :options="bindingOptions"
            searchable
            :invalid="touched && !!row.k.trim() && !row.v"
            :placeholder="t('pages.projects.form.bindingCred')"
            :aria-label="t('pages.projects.form.bindingCred')"
          />
        </div>
        <button type="button" class="project-form__del" :aria-label="t('common.buttons.delete')" @click="draft.bindings.splice(i, 1)">
          <Icon name="trash" :size="14" />
        </button>
      </div>
      <button type="button" class="project-form__add" @click="draft.bindings.push({ k: '', v: '' })">
        <Icon name="plus" :size="13" />{{ t('pages.projects.form.addBinding') }}
      </button>
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
.project-form__access {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.project-form__access-opt {
  display: flex;
  flex-direction: column;
  gap: 3px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  padding: 9px 11px;
  font-size: 12.5px;
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.project-form__access-opt:hover {
  border-color: rgb(var(--c-line-strong, var(--c-line)));
}
.project-form__access-opt--on {
  border-color: rgb(var(--c-accent) / 0.55);
  background: rgb(var(--c-accent) / 0.06);
}
.project-form__warn {
  display: flex;
  gap: 6px;
  border: 1px solid rgb(var(--c-warn) / 0.35);
  border-radius: 8px;
  background: rgb(var(--c-warn) / 0.08);
  padding: 7px 10px;
  color: rgb(var(--c-warn));
  font-size: 12px;
}
.project-form__kv {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.project-form__del {
  display: flex;
  width: 32px;
  height: 36px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: rgb(var(--c-txt3));
}
.project-form__del:hover {
  color: rgb(var(--c-err));
}
.project-form__add {
  display: inline-flex;
  align-self: flex-start;
  align-items: center;
  gap: 4px;
  color: rgb(var(--c-accent-2));
  font-size: 12px;
}
.project-form__add:hover {
  text-decoration: underline;
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
@media (max-width: 520px) {
  .project-form__access {
    grid-template-columns: minmax(0, 1fr);
  }
  .project-form__kv {
    flex-wrap: wrap;
  }
}
</style>
