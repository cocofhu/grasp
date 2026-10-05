<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import { api } from '@/lib/api/api'
import type { AgentTemplate } from '@/lib/api/apiTypes'
import { authGuideFor } from '@/lib/agent/backendAuthGuide'
import { ACP_BACKENDS, getRegionPolicy, type BackendId } from '@/lib/shared/regionPolicy'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'
import OnboardingTeamCard from './OnboardingTeamCard.vue'
import OnboardingWorkflowPreview from './OnboardingWorkflowPreview.vue'
import {
  OPENCODE_FALLBACK_PROVIDERS,
  openCodeCustomBaseRequired,
  openCodeModelRequired,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import { setLocale } from '@/lib/shared/locale'
import { setTheme, type ThemeName } from '@/lib/shared/theme'
import type { AppLocale } from '@/lib/shared/loadLocaleMessages'
import { useToast } from '@/lib/composables/useToast'
import { writeStoredProjectId } from '@/lib/composables/useProjectContext'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import type { GitCredentialType } from '@/lib/agent/gitCredentialAnalysis'
import {
  DEFAULT_PROJECT_ID,
  ONBOARDING_AGENT_NAMES,
  ONBOARDING_CLI_BACKENDS,
  ONBOARDING_GIT_TYPES,
  ONBOARDING_STEPS,
  applyDefaultTeamNames,
  applyOnboardingBackend,
  applyStartPath,
  assembleBootstrapBody,
  buildOnboardingWorkflowPreview,
  deriveOnboardingAgentNames,
  freshOnboardingDraft,
  gitConfigured,
  gitIdentityConfigured,
  repoConfigured,
  repoNameFromUrl,
  sanitizeOnboardingPrefix,
  setTeamMemberEnabled,
  teamNameIssues,
  teamValid,
  type OnboardingBootstrapResult,
  type OnboardingDraft,
  type OnboardingMode,
  type OnboardingStartPath,
  type OnboardingStepId,
  type OnboardingTeamMember,
  type OnboardingTemplateId,
} from '@/lib/pm/onboardingWizard'
import { START_PATH_OPTIONS } from '@/lib/shared/startPath'

export type OnboardingCompletedResult = OnboardingBootstrapResult & {
  projectId?: string
}

const props = withDefaults(
  defineProps<{
    open: boolean
    /** Used for firstInstall/retry; empty for createProject until created. */
    projectId?: string
    mode?: OnboardingMode
  }>(),
  {
    projectId: '',
    mode: 'firstInstall',
  },
)

const emit = defineEmits<{
  close: []
  completed: [result: OnboardingCompletedResult]
}>()

const { t } = useI18n()
const toast = useToast()
const router = useRouter()
const launch = useWorkflowRunLaunch()

const draft = ref<OnboardingDraft>(freshOnboardingDraft())
const creating = ref(false)
const createError = ref('')
const phase = ref<'wizard' | 'success'>('wizard')
const result = ref<OnboardingBootstrapResult | null>(null)
const createdProjectId = ref('')
const stepAnimKey = ref(0)
const keyError = ref(false)
const modelError = ref(false)
const projectNameError = ref('')
const templates = ref<AgentTemplate[]>([])
const templatesState = ref<'loading' | 'ready' | 'error'>('loading')
const retryProjectName = ref('')
const showTeamErrors = ref(false)
const leaving = ref(false)

const steps = ONBOARDING_STEPS
const wizardStepCount = steps.length - 1
const isCreate = computed(() => (props.mode || 'firstInstall') === 'createProject')
const activeIndex = computed(() => (phase.value === 'success' ? steps.length - 1 : draft.value.step))
const currentStep = computed(() => steps[activeIndex.value] || steps[0]!)
const languageOptions: { id: AppLocale; label: string; hint: string }[] = [
  { id: 'zh-CN', label: '简体中文', hint: 'Chinese (Simplified)' },
  { id: 'en', label: 'English', hint: '英语' },
]
const themeOptions: { id: ThemeName; labelKey: string }[] = [
  { id: 'light', labelKey: 'pages.onboarding.language.themeLight' },
  { id: 'dark', labelKey: 'pages.onboarding.language.themeDark' },
]
const startPathOptions = START_PATH_OPTIONS
const regionPolicy = computed(() => getRegionPolicy(draft.value.acpBackend))
const authGuide = computed(() => authGuideFor(draft.value.acpBackend, draft.value.region))
const primaryAuthKey = computed(() => authGuide.value.keys[0]?.key || '')
const primaryAuthAlt = computed(() => authGuide.value.keys[0]?.alt || '')
const backendLabel = computed(() => ACP_BACKENDS.find((b) => b.id === draft.value.acpBackend)?.label || '')
const targetProjectId = computed(() => createdProjectId.value || (props.projectId || '').trim())
const wizardTitle = computed(() => {
  if (isCreate.value) return t('pages.onboarding.titleCreate')
  if ((props.mode || 'firstInstall') === 'retry') return t('pages.onboarding.titleRetry')
  return t('pages.onboarding.title')
})
const gitOk = computed(() => gitConfigured(draft.value))
const identityOk = computed(() => gitIdentityConfigured(draft.value))
const repoOk = computed(() => repoConfigured(draft.value))
const repoDirName = computed(() => repoNameFromUrl(draft.value.repoUrl))

/** Names the server would derive; empty when the project name is not known yet. */
const defaultAgentNames = computed<string[]>(() => {
  if (isCreate.value) return deriveOnboardingAgentNames('new', draft.value.projectName)
  const pid = (props.projectId || DEFAULT_PROJECT_ID).trim() || DEFAULT_PROJECT_ID
  if (pid === DEFAULT_PROJECT_ID) return [...ONBOARDING_AGENT_NAMES]
  return deriveOnboardingAgentNames(pid, retryProjectName.value)
})
const allowBlankNames = computed(() => defaultAgentNames.value.length === 0)
const nameIssues = computed(() => teamNameIssues(draft.value.team, { allowBlank: allowBlankNames.value }))
const workflowPreview = computed(() => buildOnboardingWorkflowPreview(draft.value.team))
const reviewIncluded = computed(() => workflowPreview.value.nodes.some((n) => n.id === 'test_review'))
const enabledTeam = computed(() => draft.value.team.filter((m) => m.enabled))
const modelPlaceholder = computed(() =>
  draft.value.acpBackend === 'opencode' && draft.value.openCodeModel.trim()
    ? t('pages.onboarding.team.modelInherit', {
        model: draft.value.openCodeModel.trim(),
      })
    : t('pages.onboarding.team.modelPlaceholder'),
)
const successAgentNames = computed(() => {
  if (result.value?.agentIds?.length) return result.value.agentIds
  return enabledTeam.value.map((m) => m.name).filter(Boolean)
})

watch(defaultAgentNames, (names) => applyDefaultTeamNames(draft.value.team, names), { immediate: true })

watch(
  () => props.open,
  (open) => {
    if (!open) return
    const inheritAppLocale = isCreate.value
    draft.value = freshOnboardingDraft({ inheritAppLocale })
    // createProject must not overwrite the user's saved locale with browser/OS.
    if (!inheritAppLocale) {
      void setLocale(draft.value.language)
    }
    creating.value = false
    createError.value = ''
    phase.value = 'wizard'
    result.value = null
    createdProjectId.value = ''
    keyError.value = false
    modelError.value = false
    projectNameError.value = ''
    showTeamErrors.value = false
    leaving.value = false
    retryProjectName.value = ''
    applyDefaultTeamNames(draft.value.team, defaultAgentNames.value)
    stepAnimKey.value++
    void loadTemplates()
    void loadRetryProjectName()
  },
  { immediate: true },
)

async function loadTemplates() {
  templatesState.value = 'loading'
  try {
    const res = await api.listAgentTeamTemplates()
    templates.value = (res?.items || []) as AgentTemplate[]
    templatesState.value = 'ready'
  } catch {
    templates.value = []
    templatesState.value = 'error'
  }
}

async function loadRetryProjectName() {
  const pid = (props.projectId || '').trim()
  if (isCreate.value || !pid || pid === DEFAULT_PROJECT_ID) return
  try {
    const proj = await api.getProject(pid)
    retryProjectName.value = proj?.name || ''
  } catch {
    /* names stay blank; the server derives them */
  }
}

function templateFor(id: OnboardingTemplateId): AgentTemplate | undefined {
  return templates.value.find((x) => x.id === id)
}

/** Closes for this view only; a reload re-opens it until the default workflow exists. */
function closeWizard() {
  if (creating.value) return
  emit('close')
}

function selectBackend(id: BackendId) {
  applyOnboardingBackend(draft.value, id)
  keyError.value = false
  modelError.value = false
}

function selectOpenCodeModel(value: string) {
  draft.value.openCodeModel = value
  modelError.value = false
}

function selectStartPath(path: OnboardingStartPath) {
  applyStartPath(draft.value, path)
  keyError.value = false
  modelError.value = false
}

function selectLanguage(language: AppLocale) {
  if (draft.value.language === language) return
  draft.value.language = language
  void setLocale(language)
}

/** Theme applies immediately (like language) so the wizard previews the choice. */
function selectTheme(name: ThemeName) {
  if (draft.value.theme === name) return
  draft.value.theme = name
  setTheme(name)
}

function selectRegion(id: string) {
  draft.value.region = id
}

function selectGitType(id: GitCredentialType) {
  draft.value.gitCredentialType = draft.value.gitCredentialType === id ? '' : id
}

function toggleGitSkipped() {
  draft.value.gitSkipped = !draft.value.gitSkipped
}

function toggleVncPreview() {
  draft.value.vncPreview = !draft.value.vncPreview
  if (!draft.value.vncPreview) draft.value.browserMcp = false
}

function toggleBrowserMcp() {
  draft.value.browserMcp = !draft.value.browserMcp
  if (draft.value.browserMcp) draft.value.vncPreview = true
}

function toggleMember(id: OnboardingTemplateId, enabled: boolean) {
  setTeamMemberEnabled(draft.value.team, id, enabled)
}

function renameMember(m: OnboardingTeamMember, value: string) {
  m.name = value
  m.nameEdited = true
}

function validateProjectName(): boolean {
  const name = draft.value.projectName.trim()
  if (!name) {
    projectNameError.value = t('pages.onboarding.projectName.required')
    toast.error(projectNameError.value)
    return false
  }
  if (!sanitizeOnboardingPrefix(name)) {
    projectNameError.value = t('pages.onboarding.projectName.invalid')
    toast.error(projectNameError.value)
    return false
  }
  projectNameError.value = ''
  return true
}

function validatePrefs(): boolean {
  return !isCreate.value || !!createdProjectId.value || validateProjectName()
}

function validateKey(): boolean {
  if (!draft.value.apiKey.trim()) {
    keyError.value = true
    toast.error(t('pages.onboarding.toastNeedKey'))
    return false
  }
  if (draft.value.acpBackend === 'opencode' && openCodeModelRequired(draft.value.openCodeModel)) {
    modelError.value = true
    toast.error(t('pages.agentStudio.openCode.modelRequired'))
    return false
  }
  if (draft.value.acpBackend === 'opencode' && openCodeCustomBaseRequired(draft.value.openCodeProvider, draft.value.openCodeBaseURL)) {
    toast.error(t('pages.agentStudio.openCode.baseRequired'))
    return false
  }
  return true
}

function validateGit(): boolean {
  if (gitIdentityConfigured(draft.value)) return true
  toast.error(t('pages.onboarding.toastNeedGitUser'))
  return false
}

function validateTeam(): boolean {
  if (teamValid(draft.value.team, { allowBlank: allowBlankNames.value })) return true
  showTeamErrors.value = true
  toast.error(t('pages.onboarding.team.toastFixNames'))
  return false
}

function memberIssue(id: OnboardingTemplateId) {
  const issue = nameIssues.value[id]
  if (issue === 'required' && !showTeamErrors.value) return ''
  return issue
}

function goPrev() {
  if (draft.value.step === 0 || creating.value || phase.value === 'success') return
  draft.value.step--
  stepAnimKey.value++
}

function goNext() {
  if (creating.value) return
  const id = currentStep.value.id
  if (id === 'prefs' && !validatePrefs()) return
  if (id === 'key' && !validateKey()) return
  if (id === 'git' && !validateGit()) return
  if (id === 'team' && !validateTeam()) return
  if (id === 'workflow') {
    void submitBootstrap()
    return
  }
  keyError.value = false
  modelError.value = false
  projectNameError.value = ''
  draft.value.step = Math.min(draft.value.step + 1, wizardStepCount - 1)
  stepAnimKey.value++
}

async function runBootstrap(projectId: string, body: ReturnType<typeof assembleBootstrapBody>) {
  const res = await api.bootstrapProjectOnboarding(projectId, body)
  result.value = res
  phase.value = 'success'
  writeStoredProjectId(projectId)
  toast.success(t('pages.onboarding.toastOk'))
  emit('completed', { ...res, projectId })
}

async function submitBootstrap() {
  const checks: [OnboardingStepId, () => boolean][] = [
    ['prefs', validatePrefs],
    ['key', validateKey],
    ['git', validateGit],
    ['team', validateTeam],
  ]
  const failed = checks.find(([, ok]) => !ok())
  if (failed) {
    draft.value.step = steps.findIndex((s) => s.id === failed[0])
    stepAnimKey.value++
    return
  }
  creating.value = true
  createError.value = ''
  try {
    const body = assembleBootstrapBody(draft.value)

    // create→bootstrap failure: keep same project id and only retry bootstrap (s3/f6).
    if (createdProjectId.value) {
      await runBootstrap(createdProjectId.value, body)
      return
    }

    if (isCreate.value) {
      const created = await api.createProject({
        name: draft.value.projectName.trim(),
        description: '',
      })
      // Remember id so a bootstrap failure retries bootstrap only — never create again (s3/f6).
      createdProjectId.value = created.id
      await runBootstrap(created.id, body)
      return
    }

    const projectId = (props.projectId || '').trim()
    if (!projectId) {
      createError.value = t('pages.onboarding.toastErr')
      toast.error(createError.value)
      return
    }
    await runBootstrap(projectId, body)
  } catch (e: any) {
    createError.value = e?.message || String(e)
    toast.error(createError.value || t('pages.onboarding.toastErr'))
  } finally {
    creating.value = false
  }
}

function goToProject() {
  const id = targetProjectId.value
  if (!id) return
  writeStoredProjectId(id)
  void router.push({ path: `/projects/${id}` })
}

function finish() {
  goToProject()
  emit('close')
}

async function runOnce() {
  const id = result.value?.workflowId
  if (!id || leaving.value) return
  leaving.value = true
  try {
    const wf = await api.getWorkflow(id)
    goToProject()
    emit('close')
    await launch.openLaunch(wf)
  } catch (e: any) {
    toast.error(e?.message || String(e))
  } finally {
    leaving.value = false
  }
}

function editWorkflow() {
  const id = result.value?.workflowId
  if (!id) return
  if (targetProjectId.value) writeStoredProjectId(targetProjectId.value)
  void router.push({ path: `/workflows/${id}/edit` })
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4" data-testid="onboarding-wizard">
      <div class="onb-backdrop absolute inset-0" data-testid="onboarding-backdrop" @click="closeWizard" />
      <div class="onb-dialog relative z-10 flex w-full overflow-hidden" role="dialog" aria-modal="true" aria-labelledby="onb-title">
        <aside class="onb-rail flex w-[248px] shrink-0 flex-col">
          <div class="px-6 pt-6">
            <div class="onb-brand grid h-10 w-10 place-items-center rounded-xl">
              <Icon name="sparkles" :size="20" />
            </div>
            <h2 id="onb-title" class="mt-4 text-[17px] font-semibold leading-6 text-txt" data-testid="onboarding-title">
              {{ wizardTitle }}
            </h2>
            <p class="mt-1 text-[12px] leading-5 text-txt3">
              {{ t('pages.onboarding.railSub') }}
            </p>
          </div>
          <ol class="mt-7 flex-1 px-4" :aria-label="t('pages.onboarding.railCap')">
            <li
              v-for="(s, i) in steps"
              :key="s.id"
              class="onb-step"
              :class="{
                'is-done': i < activeIndex,
                'is-active': i === activeIndex,
              }"
              :data-testid="`onboarding-rail-${s.id}`"
              :data-active="i === activeIndex ? '1' : undefined"
              :aria-current="i === activeIndex ? 'step' : undefined"
            >
              <div class="onb-step-mark">
                <Icon v-if="i < activeIndex" name="check" :size="13" />
                <span v-else>{{ i + 1 }}</span>
              </div>
              <div class="min-w-0 pb-5">
                <div class="onb-step-title">{{ t(s.labelKey) }}</div>
                <div class="onb-step-desc">
                  {{ t(`pages.onboarding.railDesc.${s.id}`) }}
                </div>
              </div>
            </li>
          </ol>
          <p class="px-6 pb-5 text-[11px] leading-5 text-txt3">
            {{ t('pages.onboarding.railFoot') }}
          </p>
        </aside>

        <div class="relative flex min-w-0 flex-1 flex-col bg-surface">
          <button
            type="button"
            class="onb-close absolute right-4 top-4 z-10"
            :aria-label="t('pages.onboarding.close')"
            :disabled="creating"
            data-testid="onboarding-close"
            @click="closeWizard"
          >
            <Icon name="close" :size="17" />
          </button>

          <template v-if="phase === 'success'">
            <div class="min-h-0 flex-1 overflow-y-auto px-10 pb-8 pt-10" data-testid="onboarding-success">
              <div class="onb-success-mark grid h-12 w-12 place-items-center rounded-full">
                <Icon name="check" :size="24" />
              </div>
              <h3 class="mt-5 text-[22px] font-semibold text-txt">
                {{ t('pages.onboarding.success.title') }}
              </h3>
              <p class="mt-1.5 max-w-[60ch] text-[13px] leading-6 text-txt2">
                {{ t('pages.onboarding.success.desc') }}
              </p>

              <div class="mt-6 grid gap-4 lg:grid-cols-[1fr_1.25fr]">
                <section class="onb-card">
                  <div class="onb-card-title">
                    {{ t('pages.onboarding.success.created') }}
                  </div>
                  <ul class="mt-3 space-y-2" data-testid="onboarding-success-agents">
                    <li v-for="n in successAgentNames" :key="n" class="flex items-center gap-2.5 text-[13px] text-txt">
                      <span class="onb-avatar">{{ n.slice(0, 1) }}</span
                      >{{ n }}
                    </li>
                    <li class="flex items-center gap-2.5 text-[13px] text-txt">
                      <span class="onb-avatar is-flow"><Icon name="workflow" :size="13" /></span
                      >{{ t('pages.onboarding.success.publishedLine') }}
                    </li>
                  </ul>
                </section>
                <section class="onb-card">
                  <div class="onb-card-title">
                    {{ t('pages.onboarding.success.written') }}
                  </div>
                  <ul class="mt-3 space-y-2.5 text-[12.5px] leading-5">
                    <li class="onb-status" :class="repoOk ? 'is-ok' : 'is-warn'" data-testid="onboarding-success-repo">
                      <Icon :name="repoOk ? 'check' : 'alert'" :size="14" />
                      <span>{{
                        repoOk
                          ? t('pages.onboarding.success.repoOk', {
                              dir: repoDirName,
                            })
                          : t('pages.onboarding.success.limit')
                      }}</span>
                    </li>
                    <li class="onb-status" :class="gitOk ? 'is-ok' : 'is-warn'" data-testid="onboarding-success-git">
                      <Icon :name="gitOk ? 'check' : 'alert'" :size="14" />
                      <span>{{ gitOk ? t('pages.onboarding.success.gitOk') : t('pages.onboarding.success.gitSkip') }}</span>
                    </li>
                    <li class="onb-status is-ok" data-testid="onboarding-success-git-user">
                      <Icon name="check" :size="14" />
                      <span>{{
                        t('pages.onboarding.success.gitUserOk', {
                          name: draft.gitUserName,
                          email: draft.gitUserEmail,
                        })
                      }}</span>
                    </li>
                    <li class="onb-status is-ok" data-testid="onboarding-success-preview">
                      <Icon name="check" :size="14" />
                      <span>
                        {{
                          t('pages.onboarding.success.preview', {
                            vnc: draft.vncPreview ? t('pages.onboarding.preview.vncOn') : t('pages.onboarding.preview.vncOff'),
                            mcp: draft.browserMcp ? t('pages.onboarding.preview.browserOn') : t('pages.onboarding.preview.browserOff'),
                          })
                        }}
                      </span>
                    </li>
                  </ul>
                </section>
              </div>

              <div class="mt-6 grid gap-3 sm:grid-cols-2">
                <button type="button" class="onb-action is-primary" :disabled="leaving" data-testid="onboarding-run-once" @click="runOnce">
                  <span class="onb-action-icon"><Icon name="play" :size="16" /></span>
                  <span class="min-w-0 flex-1">
                    <strong class="block text-[14px]">{{ t('pages.onboarding.success.runOnce') }}</strong>
                    <span class="mt-0.5 block text-[12px] leading-5 opacity-80">{{ t('pages.onboarding.success.runOnceHint') }}</span>
                  </span>
                  <Icon name="chevron-right" :size="16" class="opacity-70" />
                </button>
                <button type="button" class="onb-action" data-testid="onboarding-edit-workflow" @click="editWorkflow">
                  <span class="onb-action-icon"><Icon name="edit" :size="16" /></span>
                  <span class="min-w-0 flex-1">
                    <strong class="block text-[14px] text-txt">{{ t('pages.onboarding.success.editWorkflow') }}</strong>
                    <span class="mt-0.5 block text-[12px] leading-5 text-txt3">{{ t('pages.onboarding.success.editWorkflowHint') }}</span>
                  </span>
                  <Icon name="chevron-right" :size="16" class="text-txt3" />
                </button>
              </div>
            </div>
            <footer class="onb-footer">
              <div class="flex-1" />
              <AppButton variant="ghost" data-testid="onboarding-success-close" @click="finish">
                {{ t('pages.onboarding.close') }}
              </AppButton>
            </footer>
          </template>

          <template v-else>
            <div class="min-h-0 flex-1 overflow-y-auto px-10 pb-8 pt-8" data-testid="onboarding-body">
              <div :key="stepAnimKey">
                <div class="text-[11.5px] font-medium text-accent-2">
                  {{
                    t('pages.onboarding.stepOf', {
                      n: activeIndex + 1,
                      total: wizardStepCount,
                    })
                  }}
                </div>
                <h3 class="mt-1.5 text-[22px] font-semibold leading-8 text-txt">
                  {{ t(currentStep.labelKey) }}
                </h3>

                <template v-if="currentStep.id === 'prefs'">
                  <p class="onb-lede">
                    {{ t(isCreate ? 'pages.onboarding.prefsMetaCreate' : 'pages.onboarding.prefsMeta') }}
                  </p>
                  <div class="mt-7 space-y-7">
                    <section v-if="isCreate" data-testid="onboarding-section-project">
                      <label class="block">
                        <span class="onb-label">{{ t('pages.onboarding.projectName.label') }} <span class="text-err">*</span></span>
                        <input
                          v-model="draft.projectName"
                          type="text"
                          autocomplete="off"
                          class="onb-input"
                          :class="{ 'is-invalid': projectNameError }"
                          :placeholder="t('pages.onboarding.projectName.placeholder')"
                          :disabled="!!createdProjectId"
                          data-testid="onboarding-project-name"
                          @input="projectNameError = ''"
                        />
                        <p class="onb-hint">
                          {{ t('pages.onboarding.projectName.meta') }}
                        </p>
                        <p v-if="projectNameError" class="onb-error">
                          {{ projectNameError }}
                        </p>
                      </label>
                    </section>
                    <section v-else data-testid="onboarding-section-language">
                      <div class="mt-4 grid gap-4 sm:grid-cols-2">
                        <div>
                          <div class="onb-label">
                            {{ t('pages.onboarding.language.languageLabel') }}
                          </div>
                          <div class="onb-seg" role="radiogroup" :aria-label="t('pages.onboarding.language.languageLabel')">
                            <button
                              v-for="option in languageOptions"
                              :key="option.id"
                              type="button"
                              role="radio"
                              class="onb-seg-item"
                              :aria-checked="draft.language === option.id"
                              :title="option.hint"
                              :data-testid="`onboarding-language-${option.id}`"
                              @click="selectLanguage(option.id)"
                            >
                              {{ option.label }}
                            </button>
                          </div>
                        </div>
                        <div>
                          <div class="onb-label">
                            {{ t('pages.onboarding.language.themeLabel') }}
                          </div>
                          <div class="onb-seg" role="radiogroup" :aria-label="t('pages.onboarding.language.themeLabel')">
                            <button
                              v-for="option in themeOptions"
                              :key="option.id"
                              type="button"
                              role="radio"
                              class="onb-seg-item"
                              :aria-checked="draft.theme === option.id"
                              :data-testid="`onboarding-theme-${option.id}`"
                              @click="selectTheme(option.id)"
                            >
                              <Icon :name="option.id === 'dark' ? 'moon' : 'sun'" :size="14" />
                              {{ t(option.labelKey) }}
                            </button>
                          </div>
                        </div>
                      </div>
                      <p class="onb-hint">
                        {{ t('pages.onboarding.language.detected') }}
                      </p>
                    </section>
                    <section>
                      <div class="onb-sub">
                        {{ t('pages.onboarding.preview.section') }}
                      </div>
                      <div class="onb-rows mt-2">
                        <button
                          type="button"
                          role="switch"
                          class="onb-row"
                          :aria-checked="draft.vncPreview"
                          data-testid="onboarding-vnc-preview"
                          @click="toggleVncPreview"
                        >
                          <span class="min-w-0 flex-1 text-left">
                            <strong class="block text-[13px] font-medium text-txt">{{ t('pages.onboarding.preview.vncLabel') }}</strong>
                            <span class="mt-0.5 block text-[12px] text-txt3">{{ t('pages.onboarding.preview.vncHint') }}</span>
                          </span>
                          <span class="onb-switch" aria-hidden="true" />
                        </button>
                        <button
                          type="button"
                          role="switch"
                          class="onb-row"
                          :aria-checked="draft.browserMcp"
                          data-testid="onboarding-browser-mcp"
                          @click="toggleBrowserMcp"
                        >
                          <span class="min-w-0 flex-1 text-left">
                            <strong class="block text-[13px] font-medium text-txt">{{ t('pages.onboarding.preview.browserLabel') }}</strong>
                            <span class="mt-0.5 block text-[12px] text-txt3">{{ t('pages.onboarding.preview.browserHint') }}</span>
                          </span>
                          <span class="onb-switch" aria-hidden="true" />
                        </button>
                      </div>
                    </section>
                  </div>
                </template>

                <template v-else-if="currentStep.id === 'model'">
                  <p class="onb-lede">{{ t('pages.onboarding.acp.meta') }}</p>
                  <section class="mt-7" data-testid="onboarding-section-backend">
                    <div class="grid gap-3 sm:grid-cols-2" role="radiogroup" :aria-label="t('pages.onboarding.steps.model')">
                      <button
                        v-for="option in startPathOptions"
                        :key="option.id"
                        type="button"
                        role="radio"
                        class="onb-tile"
                        :aria-checked="draft.startPath === option.id"
                        :data-testid="`onboarding-path-${option.id}`"
                        @click="selectStartPath(option.id)"
                      >
                        <span class="onb-tile-icon"><Icon :name="option.id === 'apiKey' ? 'lock' : 'terminal'" :size="16" /></span>
                        <span class="min-w-0 flex-1">
                          <strong class="block text-[13.5px] text-txt">{{ t(option.titleKey) }}</strong>
                          <span class="mt-1 block text-[12px] leading-5 text-txt3">{{ t(option.descKey) }}</span>
                        </span>
                        <span class="onb-check"><Icon name="check" :size="11" /></span>
                      </button>
                    </div>

                    <div v-if="draft.startPath === 'apiKey'" class="mt-4" data-testid="onboarding-path-apikey-detail">
                      <div class="onb-label">
                        {{ t('pages.onboarding.acp.apiKeyVendorsLabel') }}
                      </div>
                      <div class="flex flex-wrap gap-1.5">
                        <span v-for="p in OPENCODE_FALLBACK_PROVIDERS" :key="p.id" class="onb-chip">{{ t(p.labelKey) }}</span>
                      </div>
                      <p class="onb-hint">
                        {{ t('pages.onboarding.acp.apiKeyVendorsHint') }}
                        <code class="ml-1 font-mono text-txt2">/root/.config/opencode</code>
                      </p>
                    </div>
                    <div v-else class="mt-4">
                      <div class="onb-label">
                        {{ t('pages.onboarding.acp.cliLabel') }}
                      </div>
                      <div
                        class="grid grid-cols-2 gap-2.5 sm:grid-cols-4"
                        role="radiogroup"
                        :aria-label="t('pages.onboarding.acp.cliLabel')"
                      >
                        <button
                          v-for="b in ONBOARDING_CLI_BACKENDS"
                          :key="b.id"
                          type="button"
                          role="radio"
                          class="onb-tile is-compact"
                          :aria-checked="draft.acpBackend === b.id"
                          :data-testid="`onboarding-backend-${b.id}`"
                          @click="selectBackend(b.id)"
                        >
                          <span class="min-w-0 flex-1">
                            <strong class="block truncate text-[13px] text-txt">{{ b.label }}</strong>
                            <span class="mt-0.5 block truncate font-mono text-[10.5px] text-txt3">{{ b.configRoot }}</span>
                          </span>
                          <span class="onb-check"><Icon name="check" :size="11" /></span>
                        </button>
                      </div>
                    </div>
                    <div v-if="regionPolicy" class="mt-4">
                      <div class="onb-label">
                        {{ t('pages.onboarding.acp.region') }}
                      </div>
                      <div class="onb-seg" role="radiogroup" :aria-label="t('pages.onboarding.acp.region')">
                        <button
                          v-for="option in regionPolicy.options"
                          :key="option.id"
                          type="button"
                          role="radio"
                          class="onb-seg-item"
                          :aria-checked="draft.region === option.id"
                          @click="selectRegion(option.id)"
                        >
                          {{ t(option.labelKey) }}
                        </button>
                      </div>
                    </div>
                  </section>
                </template>

                <template v-else-if="currentStep.id === 'key'">
                  <p class="onb-lede">
                    {{ t('pages.onboarding.apiKey.meta') }}
                  </p>
                  <section class="onb-split mt-6" data-testid="onboarding-section-key">
                    <aside class="onb-guide">
                      <div class="onb-label">
                        {{ t('pages.onboarding.apiKey.envLabel') }}
                      </div>
                      <code class="onb-keyname">{{ primaryAuthKey }}</code>
                      <code v-if="primaryAuthAlt" class="onb-keyname is-alt">{{ primaryAuthAlt }}</code>
                      <ol v-if="authGuide.pathStepKeys.length" class="onb-steps-list mt-4">
                        <li v-for="(k, i) in authGuide.pathStepKeys" :key="i">
                          <span class="onb-steps-num">{{ i + 1 }}</span
                          ><span>{{ t(k) }}</span>
                        </li>
                      </ol>
                      <div v-if="authGuide.links.length" class="mt-3 flex flex-wrap gap-2">
                        <a
                          v-for="link in authGuide.links"
                          :key="link.url"
                          :href="link.url"
                          target="_blank"
                          rel="noopener noreferrer"
                          class="onb-link"
                          >{{ t(link.labelKey) }}<Icon name="arrow-up" :size="11" class="rotate-45"
                        /></a>
                      </div>
                    </aside>
                    <div class="min-w-0">
                      <OpenCodeProviderFields
                        v-if="draft.acpBackend === 'opencode'"
                        :provider="(draft.openCodeProvider || 'openai') as OpenCodeProviderId"
                        :base-url="draft.openCodeBaseURL"
                        :model="draft.openCodeModel"
                        :vision="draft.openCodeModelVision"
                        :require-base="openCodeCustomBaseRequired(draft.openCodeProvider, draft.openCodeBaseURL)"
                        :require-model="modelError"
                        @update:provider="draft.openCodeProvider = $event"
                        @update:base-url="draft.openCodeBaseURL = $event"
                        @update:model="selectOpenCodeModel"
                        @update:vision="draft.openCodeModelVision = $event"
                      />
                      <label class="mt-4 block">
                        <span class="onb-label">API Key <span class="text-err">*</span></span>
                        <input
                          id="onb-api-key"
                          v-model="draft.apiKey"
                          type="password"
                          autocomplete="off"
                          class="onb-input font-mono"
                          :class="{ 'is-invalid': keyError }"
                          data-testid="onboarding-api-key"
                          @input="keyError = false"
                        />
                        <p class="onb-hint">
                          {{ t('pages.onboarding.apiKey.hint') }}
                        </p>
                        <p v-if="keyError" class="onb-error">
                          {{ t('pages.onboarding.apiKey.required') }}
                        </p>
                      </label>
                    </div>
                  </section>
                </template>

                <template v-else-if="currentStep.id === 'git'">
                  <p class="onb-lede">{{ t('pages.onboarding.git.meta') }}</p>
                  <section class="mt-6" data-testid="onboarding-section-git">
                    <div class="onb-sub">
                      {{ t('pages.onboarding.gitUser.section') }}
                    </div>
                    <div class="mt-2 grid gap-3 sm:grid-cols-2">
                      <label class="block">
                        <span class="onb-label">{{ t('pages.onboarding.gitUser.nameLabel') }} <span class="text-err">*</span></span>
                        <input
                          v-model="draft.gitUserName"
                          type="text"
                          autocomplete="off"
                          :placeholder="t('pages.onboarding.gitUser.namePlaceholder')"
                          class="onb-input"
                          data-testid="onboarding-git-user-name"
                        />
                      </label>
                      <label class="block">
                        <span class="onb-label">{{ t('pages.onboarding.gitUser.emailLabel') }} <span class="text-err">*</span></span>
                        <input
                          v-model="draft.gitUserEmail"
                          type="email"
                          autocomplete="off"
                          :placeholder="t('pages.onboarding.gitUser.emailPlaceholder')"
                          class="onb-input"
                          data-testid="onboarding-git-user-email"
                        />
                      </label>
                    </div>
                    <p class="onb-hint">
                      {{ t('pages.onboarding.gitUser.hint') }}
                    </p>
                    <div class="mt-5 flex items-center gap-3">
                      <div class="onb-sub flex-1">
                        {{ t('pages.onboarding.git.repoCredSection') }}
                      </div>
                      <AppButton size="sm" variant="ghost" data-testid="onboarding-git-skip" @click="toggleGitSkipped">
                        {{ draft.gitSkipped ? t('pages.onboarding.git.unskip') : t('pages.onboarding.git.skip') }}
                      </AppButton>
                    </div>
                    <p v-if="draft.gitSkipped" class="onb-note mt-2" data-testid="onboarding-git-skipped">
                      {{ t('pages.onboarding.git.skippedHint') }}
                    </p>
                    <template v-else>
                      <div class="mt-2 grid gap-3 sm:grid-cols-[2fr_1fr]">
                        <label class="block">
                          <span class="onb-label">{{ t('pages.onboarding.repo.urlLabel') }}</span>
                          <input
                            v-model="draft.repoUrl"
                            type="text"
                            autocomplete="off"
                            placeholder="https://github.com/org/repo.git"
                            class="onb-input font-mono"
                            data-testid="onboarding-repo-url"
                          />
                        </label>
                        <label class="block">
                          <span class="onb-label">{{ t('pages.onboarding.repo.branchLabel') }}</span>
                          <input
                            v-model="draft.repoBranch"
                            type="text"
                            autocomplete="off"
                            :placeholder="t('pages.onboarding.repo.branchPlaceholder')"
                            class="onb-input font-mono"
                            data-testid="onboarding-repo-branch"
                          />
                        </label>
                      </div>
                      <p class="onb-hint" data-testid="onboarding-repo-hint">
                        {{
                          repoDirName
                            ? t('pages.onboarding.repo.cloneTo', {
                                dir: repoDirName,
                              })
                            : t('pages.onboarding.repo.hint')
                        }}
                      </p>
                      <div class="onb-label mt-4">
                        {{ t('pages.onboarding.repo.credSection') }}
                      </div>
                      <div class="onb-seg" role="radiogroup" :aria-label="t('pages.onboarding.repo.credSection')">
                        <button
                          v-for="g in ONBOARDING_GIT_TYPES"
                          :key="g.id"
                          type="button"
                          role="radio"
                          class="onb-seg-item"
                          :aria-checked="draft.gitCredentialType === g.id"
                          :data-testid="`onboarding-git-type-${g.id}`"
                          @click="selectGitType(g.id)"
                        >
                          {{ t(g.labelKey) }}
                        </button>
                      </div>
                      <label v-if="draft.gitCredentialType === 'github_https'" class="mt-3 block">
                        <span class="onb-label">GITHUB_TOKEN</span>
                        <input
                          v-model="draft.githubToken"
                          type="password"
                          autocomplete="off"
                          class="onb-input font-mono"
                          data-testid="onboarding-github-token"
                        />
                      </label>
                      <div v-else-if="draft.gitCredentialType === 'gitlab_https'" class="mt-3 grid gap-3 sm:grid-cols-2">
                        <label class="block">
                          <span class="onb-label">GITLAB_TOKEN</span>
                          <input
                            v-model="draft.gitlabToken"
                            type="password"
                            autocomplete="off"
                            class="onb-input font-mono"
                            data-testid="onboarding-gitlab-token"
                          />
                        </label>
                        <label class="block">
                          <span class="onb-label">GITLAB_URL</span>
                          <input
                            v-model="draft.gitlabUrl"
                            type="text"
                            placeholder="https://gitlab.example.com"
                            class="onb-input font-mono"
                            data-testid="onboarding-gitlab-url"
                          />
                        </label>
                      </div>
                      <div v-else-if="draft.gitCredentialType === 'ssh'" class="mt-3 grid gap-3">
                        <label class="block">
                          <span class="onb-label">SSH private key</span>
                          <textarea
                            v-model="draft.gitSshPrivateKey"
                            rows="4"
                            class="onb-input is-area font-mono"
                            data-testid="onboarding-ssh-key"
                          />
                        </label>
                        <label class="block">
                          <span class="onb-label">known_hosts</span>
                          <textarea v-model="draft.gitSshKnownHosts" rows="2" class="onb-input is-area font-mono" />
                        </label>
                      </div>
                      <p class="onb-hint">
                        {{ t('pages.onboarding.git.foot') }}
                      </p>
                    </template>
                  </section>
                </template>

                <template v-else-if="currentStep.id === 'team'">
                  <p class="onb-lede">{{ t('pages.onboarding.team.meta') }}</p>
                  <p v-if="templatesState === 'loading'" class="onb-note mt-4" data-testid="onboarding-team-loading">
                    {{ t('pages.onboarding.team.loading') }}
                  </p>
                  <div
                    v-else-if="templatesState === 'error'"
                    class="onb-note is-warn mt-4 flex items-center gap-3"
                    data-testid="onboarding-team-load-error"
                  >
                    <Icon name="alert" :size="14" />
                    <span class="flex-1">{{ t('pages.onboarding.team.loadError') }}</span>
                    <AppButton size="sm" variant="outline" data-testid="onboarding-team-retry" @click="loadTemplates">
                      {{ t('pages.onboarding.team.retry') }}
                    </AppButton>
                  </div>
                  <div class="mt-6 grid grid-cols-3 items-stretch gap-3">
                    <OnboardingTeamCard
                      v-for="m in draft.team"
                      :key="m.templateId"
                      :member="m"
                      :capabilities="templateFor(m.templateId)?.capabilities"
                      :summary="templateFor(m.templateId)?.summary"
                      :name-placeholder="t('pages.onboarding.team.namePlaceholder')"
                      :model-placeholder="modelPlaceholder"
                      :issue="memberIssue(m.templateId)"
                      @toggle="toggleMember(m.templateId, $event)"
                      @update:name="renameMember(m, $event)"
                      @update:model="m.model = $event"
                    />
                  </div>
                  <p class="onb-hint mt-3">
                    {{ t('pages.onboarding.team.foot') }}
                  </p>
                </template>

                <template v-else-if="currentStep.id === 'workflow'">
                  <p class="onb-lede">
                    {{ t('pages.onboarding.workflow.meta') }}
                  </p>
                  <section class="onb-card onb-canvas mt-6">
                    <OnboardingWorkflowPreview :preview="workflowPreview" />
                    <p class="mt-3 flex items-center gap-1.5 text-[12px] text-txt3" data-testid="onboarding-workflow-note">
                      {{ reviewIncluded ? t('pages.onboarding.workflow.failLoop') : t('pages.onboarding.workflow.noReview') }}
                    </p>
                  </section>

                  <section class="onb-card mt-4">
                    <div class="onb-card-title">
                      {{ t('pages.onboarding.workflow.summary') }}
                    </div>
                    <dl class="onb-summary mt-3">
                      <div>
                        <dt>{{ t('pages.onboarding.steps.model') }}</dt>
                        <dd>
                          <span class="onb-dot is-ok" />{{ backendLabel
                          }}<template v-if="regionPolicy && draft.region"> · {{ draft.region }}</template>
                        </dd>
                      </div>
                      <div>
                        <dt>API Key</dt>
                        <dd><span class="onb-dot is-ok" />{{ t('pages.onboarding.workflow.keyOn') }}</dd>
                      </div>
                      <div data-testid="onboarding-review-repo">
                        <dt>{{ t('pages.onboarding.repo.chip') }}</dt>
                        <dd>
                          <span class="onb-dot" :class="{ 'is-ok': repoOk }" />{{
                            repoOk ? repoDirName : t('pages.onboarding.workflow.repoSkip')
                          }}
                        </dd>
                      </div>
                      <div data-testid="onboarding-review-git-user">
                        <dt>{{ t('pages.onboarding.gitUser.chip') }}</dt>
                        <dd><span class="onb-dot is-ok" />{{ draft.gitUserName }}</dd>
                      </div>
                      <div>
                        <dt>{{ t('pages.onboarding.workflow.gitCred') }}</dt>
                        <dd>
                          <span class="onb-dot" :class="{ 'is-ok': gitOk }" />{{
                            gitOk ? draft.gitCredentialType : t('pages.onboarding.workflow.gitSkip')
                          }}
                        </dd>
                      </div>
                      <div data-testid="onboarding-review-agents">
                        <dt>Agent</dt>
                        <dd>
                          <span class="onb-dot is-ok" />{{
                            t('pages.onboarding.workflow.agentsValue', {
                              n: enabledTeam.length,
                            })
                          }}
                        </dd>
                      </div>
                      <div data-testid="onboarding-review-workflow">
                        <dt>{{ t('pages.onboarding.workflow.wfLabel') }}</dt>
                        <dd><span class="onb-dot is-ok" />{{ t('pages.onboarding.workflow.wfValue') }}</dd>
                      </div>
                    </dl>
                    <p class="onb-hint mt-3">
                      {{ t('pages.onboarding.workflow.reuseHint') }}
                    </p>
                  </section>
                  <p v-if="createError" class="onb-note is-err mt-3" data-testid="onboarding-create-error">
                    {{ createError }}
                  </p>
                </template>
              </div>
            </div>

            <footer class="onb-footer">
              <AppButton variant="ghost" data-testid="onboarding-later" :disabled="creating" @click="closeWizard">
                {{ t('pages.onboarding.later') }}
              </AppButton>
              <div class="flex-1" />
              <AppButton v-if="draft.step > 0" variant="outline" :disabled="creating" data-testid="onboarding-prev" @click="goPrev">
                <Icon name="chevron-left" :size="14" />{{ t('pages.onboarding.prev') }}
              </AppButton>
              <AppButton variant="primary" :disabled="creating" data-testid="onboarding-next" @click="goNext">
                <Icon v-if="creating" name="spinner" :size="14" class="animate-spin" />
                {{
                  creating
                    ? t('pages.onboarding.generating')
                    : currentStep.id === 'workflow'
                      ? t('pages.onboarding.generate')
                      : t('pages.onboarding.next')
                }}
                <Icon v-if="!creating" :name="currentStep.id === 'workflow' ? 'sparkles' : 'chevron-right'" :size="14" />
              </AppButton>
            </footer>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.onb-backdrop {
  background: rgb(10 10 14 / 0.56);
  backdrop-filter: blur(2px);
}
.onb-dialog {
  width: min(1040px, 100%);
  height: min(720px, 92vh);
  border-radius: 18px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  box-shadow:
    0 24px 64px -12px rgb(0 0 0 / 0.35),
    0 2px 6px rgb(0 0 0 / 0.08);
}
.onb-rail {
  border-right: 1px solid rgb(var(--c-line));
  background: radial-gradient(120% 60% at 0% 0%, rgb(var(--c-accent) / 0.1), transparent 60%), rgb(var(--c-elevated));
}
.onb-brand {
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
  box-shadow: inset 0 0 0 1px rgb(var(--c-accent) / 0.28);
}
.onb-step {
  position: relative;
  display: flex;
  gap: 12px;
  padding: 0 8px;
  color: rgb(var(--c-txt3));
}
.onb-step:not(:last-child)::before {
  content: '';
  position: absolute;
  left: 21px;
  top: 30px;
  bottom: 4px;
  width: 1px;
  background: rgb(var(--c-line-strong));
}
.onb-step.is-done:not(:last-child)::before {
  background: rgb(var(--c-ok) / 0.55);
}
.onb-step-mark {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid rgb(var(--c-line-strong));
  background: rgb(var(--c-surface));
}
.onb-step.is-done .onb-step-mark {
  color: rgb(var(--c-ok));
  border-color: rgb(var(--c-ok) / 0.5);
  background: rgb(var(--c-ok) / 0.12);
}
.onb-step.is-active .onb-step-mark {
  color: #fff;
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
  box-shadow: 0 0 0 4px rgb(var(--c-accent) / 0.16);
}
.onb-step-title {
  margin-top: 3px;
  font-size: 13px;
  font-weight: 600;
}
.onb-step.is-done .onb-step-title {
  color: rgb(var(--c-txt2));
}
.onb-step.is-active .onb-step-title {
  color: rgb(var(--c-txt));
}
.onb-step-desc {
  margin-top: 2px;
  font-size: 11.5px;
  line-height: 1.45;
  color: rgb(var(--c-txt3));
}
.onb-close {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  color: rgb(var(--c-txt3));
}
.onb-close:hover {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-elevated));
}
.onb-lede {
  margin-top: 6px;
  max-width: 64ch;
  font-size: 13px;
  line-height: 1.7;
  color: rgb(var(--c-txt2));
}
.onb-card {
  border: 1px solid rgb(var(--c-line));
  border-radius: 14px;
  background: rgb(var(--c-base));
  padding: 18px 20px;
}
.onb-card-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.onb-card-icon {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.1);
}
.onb-card-title {
  font-size: 14px;
  font-weight: 600;
  color: rgb(var(--c-txt));
}
.onb-card-desc {
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.6;
  color: rgb(var(--c-txt3));
}
.onb-sub {
  font-size: 12.5px;
  font-weight: 600;
  color: rgb(var(--c-txt));
}
.onb-label {
  display: block;
  margin-bottom: 6px;
  font-size: 12px;
  font-weight: 500;
  color: rgb(var(--c-txt2));
}
.onb-hint {
  margin-top: 6px;
  font-size: 11.5px;
  line-height: 1.6;
  color: rgb(var(--c-txt3));
}
.onb-error {
  margin-top: 4px;
  font-size: 12px;
  color: rgb(var(--c-err));
}
.onb-input {
  width: 100%;
  height: 36px;
  border-radius: 9px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 0 12px;
  font-size: 13px;
  color: rgb(var(--c-txt));
  outline: none;
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.onb-input.is-area {
  height: auto;
  padding: 8px 12px;
  font-size: 12px;
}
.onb-input::placeholder {
  color: rgb(var(--c-txt3));
}
.onb-input:hover {
  border-color: rgb(var(--c-line-strong));
}
.onb-input:focus {
  border-color: rgb(var(--c-accent));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.16);
}
.onb-input.is-invalid {
  border-color: rgb(var(--c-err));
}
.onb-seg {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px;
  padding: 3px;
  border-radius: 10px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated));
}
.onb-seg-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 14px;
  border-radius: 7px;
  font-size: 12.5px;
  font-weight: 500;
  color: rgb(var(--c-txt2));
  transition:
    background 0.15s,
    color 0.15s;
}
.onb-seg-item:hover {
  color: rgb(var(--c-txt));
}
.onb-seg-item[aria-checked='true'] {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-surface));
  box-shadow:
    0 1px 2px rgb(0 0 0 / 0.12),
    inset 0 0 0 1px rgb(var(--c-line));
}
.onb-tile {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  text-align: left;
  transition:
    border-color 0.15s,
    background 0.15s,
    box-shadow 0.15s;
}
.onb-tile.is-compact {
  align-items: center;
  padding: 10px 12px;
}
.onb-tile:hover {
  border-color: rgb(var(--c-line-strong));
}
.onb-tile[aria-checked='true'] {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent-dim));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.12);
}
.onb-tile-icon {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.onb-tile[aria-checked='true'] .onb-tile-icon {
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.14);
}
.onb-check {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 999px;
  border: 1px solid rgb(var(--c-line-strong));
  color: transparent;
}
.onb-tile[aria-checked='true'] .onb-check {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
  color: #fff;
}
.onb-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 9px;
  border-radius: 999px;
  font-size: 11.5px;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.onb-keyname {
  display: block;
  width: fit-content;
  max-width: 100%;
  overflow-wrap: anywhere;
  padding: 4px 9px;
  border-radius: 7px;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 11.5px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.1);
}
.onb-keyname.is-alt {
  margin-top: 6px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-line) / 0.5);
}
.onb-split {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 28px;
  align-items: start;
}
.onb-guide {
  border-radius: 12px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated));
  padding: 16px;
}
.onb-steps-list {
  display: grid;
  gap: 8px;
  font-size: 12.5px;
  line-height: 1.6;
  color: rgb(var(--c-txt2));
}
.onb-steps-list li {
  display: flex;
  gap: 10px;
}
.onb-steps-num {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  margin-top: 1px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.onb-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid rgb(var(--c-line));
  font-size: 12px;
  color: rgb(var(--c-txt2));
}
.onb-link:hover {
  color: rgb(var(--c-accent-2));
  border-color: rgb(var(--c-accent) / 0.5);
}
.onb-rows {
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
  overflow: hidden;
}
.onb-row {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
}
.onb-row + .onb-row {
  border-top: 1px solid rgb(var(--c-line));
}
.onb-row:hover {
  background: rgb(var(--c-elevated) / 0.6);
}
.onb-switch {
  position: relative;
  flex-shrink: 0;
  width: 34px;
  height: 20px;
  border-radius: 999px;
  background: rgb(var(--c-line-strong));
  transition: background 0.18s;
}
.onb-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
  transition: transform 0.18s;
}
.onb-row[aria-checked='true'] .onb-switch {
  background: rgb(var(--c-accent));
}
.onb-row[aria-checked='true'] .onb-switch::after {
  transform: translateX(14px);
}
.onb-note {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.6;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.onb-note.is-warn {
  color: rgb(var(--c-warn));
  background: rgb(var(--c-warn) / 0.1);
}
.onb-note.is-err {
  color: rgb(var(--c-err));
  background: rgb(var(--c-err) / 0.1);
}
.onb-canvas {
  padding: 16px 20px 14px;
  background:
    radial-gradient(circle, rgb(var(--c-line-strong) / 0.55) 1px, transparent 1.2px) 0 0 / 16px 16px,
    rgb(var(--c-base));
}
.onb-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px 20px;
}
.onb-summary > div {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 8px;
  border-bottom: 1px dashed rgb(var(--c-line));
  font-size: 12.5px;
}
.onb-summary dt {
  color: rgb(var(--c-txt3));
}
.onb-summary dd {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--c-txt));
  font-weight: 500;
}
.onb-dot {
  flex-shrink: 0;
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: rgb(var(--c-txt3));
}
.onb-dot.is-ok {
  background: rgb(var(--c-ok));
}
.onb-footer {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
  padding: 14px 24px;
  border-top: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
}
.onb-success-mark {
  color: rgb(var(--c-ok));
  background: rgb(var(--c-ok) / 0.12);
  box-shadow: 0 0 0 6px rgb(var(--c-ok) / 0.06);
}
.onb-avatar {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  font-size: 11.5px;
  font-weight: 600;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
}
.onb-avatar.is-flow {
  color: rgb(var(--c-ok));
  background: rgb(var(--c-ok) / 0.12);
}
.onb-status {
  display: flex;
  gap: 8px;
  color: rgb(var(--c-txt2));
}
.onb-status > :first-child {
  flex-shrink: 0;
  margin-top: 3px;
}
.onb-status.is-ok > :first-child {
  color: rgb(var(--c-ok));
}
.onb-status.is-warn > :first-child {
  color: rgb(var(--c-warn));
}
.onb-action {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  border-radius: 14px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-base));
  text-align: left;
  transition:
    border-color 0.15s,
    transform 0.15s;
}
.onb-action:hover {
  border-color: rgb(var(--c-line-strong));
  transform: translateY(-1px);
}
.onb-action.is-primary {
  color: #fff;
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
}
.onb-action.is-primary:hover {
  background: rgb(var(--c-accent-hover));
}
.onb-action:disabled {
  opacity: 0.6;
  transform: none;
}
.onb-action-icon {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-elevated));
}
.onb-action.is-primary .onb-action-icon {
  color: #fff;
  background: rgb(255 255 255 / 0.16);
}
@media (prefers-reduced-motion: reduce) {
  .onb-action,
  .onb-switch,
  .onb-switch::after {
    transition: none;
  }
}
</style>
