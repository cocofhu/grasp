<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import { api, type CredentialItem, type ModelBinding, type ModelConfiguration } from '@/lib/api/api'
import ModelBindingEditor from '@/components/agent/ModelBindingEditor.vue'
import AgentGitCredentialPicker from '@/components/agent/AgentGitCredentialPicker.vue'
import WorkspaceCredentialsPanel from '@/components/workspace/WorkspaceCredentialsPanel.vue'
import { usableGitCredential } from '@/lib/agent/gitBindings'
import { bindingProblem, credentialUsable, useRoutingCopy } from '@/lib/agent/modelRouting'
import type { AgentTemplate } from '@/lib/api/apiTypes'
import { getRegionPolicy } from '@/lib/shared/regionPolicy'
import OnboardingTeamCard from './OnboardingTeamCard.vue'
import OnboardingWorkflowPreview from './OnboardingWorkflowPreview.vue'
import { setLocale } from '@/lib/shared/locale'
import { setTheme, type ThemeName } from '@/lib/shared/theme'
import type { AppLocale } from '@/lib/shared/loadLocaleMessages'
import { useExecutionMode } from '@/lib/composables/useBrandSettings'
import { useToast } from '@/lib/composables/useToast'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import {
  ONBOARDING_STEPS,
  ONBOARDING_SOLO_AGENT_NAME,
  applyDefaultTeamNames,
  assembleBootstrapBody,
  buildOnboardingWorkflowPreview,
  deriveOnboardingAgentNames,
  freshOnboardingDraft,
  gitConfigured,
  gitIdentityConfigured,
  repoConfigured,
  repoNameFromUrl,
  teamNameIssues,
  teamValid,
  type OnboardingBootstrapResult,
  type OnboardingDraft,
  type OnboardingMode,
  type OnboardingStepId,
  type OnboardingTemplateId,
} from '@/lib/pm/onboardingWizard'

export type OnboardingCompletedResult = OnboardingBootstrapResult

const props = withDefaults(
  defineProps<{
    open: boolean
    mode?: OnboardingMode
  }>(),
  {
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
const { localMode, hostGitUser } = useExecutionMode()

const draft = ref<OnboardingDraft>(freshOnboardingDraft())
const creating = ref(false)
const createError = ref('')
const phase = ref<'wizard' | 'success'>('wizard')
const result = ref<OnboardingBootstrapResult | null>(null)
const stepAnimKey = ref(0)
const modelValid = ref(false)
const gitValid = ref(true)
const gitSetupOpen = ref(false)
const resourceEditing = ref(false)
const resourcesLoading = ref(false)
const resourcesError = ref('')
const body = ref<HTMLElement | null>(null)
const stepHeading = ref<HTMLElement | null>(null)
let resourceRequest = 0
const resourcesVersion = ref(0)
const models = ref<ModelConfiguration[]>([])
const credentials = ref<CredentialItem[]>([])
const credentialsReady = computed(() => credentials.value.some(item => ['model_api', 'tool_auth'].includes(item.purpose || '') && credentialUsable(item)))
const c = useRoutingCopy()
const templates = ref<AgentTemplate[]>([])
const templatesState = ref<'loading' | 'ready' | 'error'>('loading')
const showTeamErrors = ref(false)
const leaving = ref(false)

const steps = computed(() => localMode.value ? ONBOARDING_STEPS.filter((s) => s.id !== 'credentials') : ONBOARDING_STEPS)
const wizardStepCount = computed(() => steps.value.length - 1)
const activeIndex = computed(() => (phase.value === 'success' ? steps.value.length - 1 : draft.value.step))
const currentStep = computed(() => steps.value[activeIndex.value] || steps.value[0]!)
const languageOptions: { id: AppLocale; label: string; hint: string }[] = [
  { id: 'zh-CN', label: '简体中文', hint: 'Chinese (Simplified)' },
  { id: 'en', label: 'English', hint: '英语' },
]
const themeOptions: { id: ThemeName; labelKey: string }[] = [
  { id: 'light', labelKey: 'pages.onboarding.language.themeLight' },
  { id: 'dark', labelKey: 'pages.onboarding.language.themeDark' },
]
const selectedModel = computed(() => models.value.find((row) => row.id === draft.value.modelConfigurationId))
const regionPolicy = computed(() => selectedModel.value ? getRegionPolicy(selectedModel.value.backend as import('@/lib/shared/regionPolicy').BackendId) : undefined)
const backendSummary = computed(() => selectedModel.value?.name || draft.value.modelConfigurationId)
const gitCredLabel = computed(() => `${draft.value.gitBindings.items.length} ${t('pages.agentStudio.gitCredentials.title')}`)
const modelBinding = computed<ModelBinding>({ get: () => ({ kind: 'configuration' as const, configurationId: draft.value.modelConfigurationId }), set: (value) => { draft.value.modelConfigurationId = value.configurationId; draft.value.region = regionPolicy.value?.defaultRegion || '' } })
async function refreshResources() {
  const request = ++resourceRequest
  resourcesLoading.value = true
  resourcesError.value = ''
  modelValid.value = false
  try {
    const [configs, keys, services, caps] = await Promise.all([
      api.listModelConfigurations(), api.listCredentials(), api.listModelConnections(), api.listToolCapabilities(),
    ])
    if (request !== resourceRequest || !props.open) return
    models.value = configs.items || []
    credentials.value = keys.items || []
    if (localMode.value && !draft.value.modelConfigurationId && models.value.length) {
      draft.value.modelConfigurationId = models.value[0]!.id
    }
    const configurationId = draft.value.modelConfigurationId
    if (configurationId) {
      const configuration = models.value.find(model => model.id === configurationId)
      modelValid.value = !bindingProblem({ kind: 'configuration', configurationId }, configuration?.backend || '', services.items || [], keys.items || [], caps.items || [], models.value)
      if (!modelValid.value && draft.value.step > steps.value.findIndex(step => step.id === 'model')) goToStep('model')
    }
    resourcesVersion.value++
  } catch (error) {
    if (request !== resourceRequest || !props.open) return
    resourcesError.value = String((error as Error).message || error)
    modelValid.value = false
    if (draft.value.modelConfigurationId && draft.value.step > steps.value.findIndex(step => step.id === 'model')) goToStep('model')
  } finally {
    if (request === resourceRequest) resourcesLoading.value = false
  }
}
function selectSavedModel(model: ModelConfiguration) {
  models.value = [...models.value.filter(row => row.id !== model.id), model]
  modelBinding.value = { kind: 'configuration', configurationId: model.id }
  resourceEditing.value = false
  if (props.open && currentStep.value.id === 'model') goToStep('git')
  void refreshResources()
}
function completeCredentialStep(credential: CredentialItem) {
  if (credentialUsable(credential) && ['model_api', 'tool_auth'].includes(credential.purpose || '')) {
    credentials.value = [...credentials.value.filter(item => item.id !== credential.id), credential]
    resourceEditing.value = false
    goToStep('model')
  }
  void refreshResources()
}
function selectSavedGit(credential: CredentialItem) {
  if (usableGitCredential(credential) && !draft.value.gitBindings.items.some(item => item.credentialId === credential.id)) {
    draft.value.gitBindings.items.push({ credentialId: credential.id })
  }
  void refreshResources()
}
watch([activeIndex, () => props.open], async () => {
  await nextTick()
  if (!props.open) return
  if (body.value) body.value.scrollTop = 0
  stepHeading.value?.focus({ preventScroll: true })
}, { immediate: true })
const summaryItems = computed(() => [
  { id: 'backend', label: t('pages.onboarding.steps.model'), value: backendSummary.value, ok: true },
  { id: 'repo', label: t('pages.onboarding.repo.chip'), value: repoOk.value ? repoDirName.value : t('pages.onboarding.workflow.repoSkip'), ok: repoOk.value },
  { id: 'git-user', label: t('pages.onboarding.gitUser.chip'), value: draft.value.gitUserName, ok: true },
  { id: 'git-cred', label: t('pages.onboarding.workflow.gitCred'), value: gitOk.value ? gitCredLabel.value : t('pages.onboarding.workflow.gitSkip'), ok: gitOk.value },
  { id: 'agents', label: 'Agent', value: t('pages.onboarding.workflow.agentsWithSolo', { n: enabledTeam.value.length }), ok: true },
  { id: 'workflow', label: t('pages.onboarding.workflow.wfLabel'), value: t('pages.onboarding.workflow.wfValue'), ok: true, wide: true },
])
const wizardTitle = computed(() => {
  if ((props.mode || 'firstInstall') === 'retry') return t('pages.onboarding.titleRetry')
  return t('pages.onboarding.title')
})
const gitOk = computed(() => gitConfigured(draft.value))
const identityOk = computed(() => gitIdentityConfigured(draft.value))
const repoOk = computed(() => repoConfigured(draft.value))
const repoDirName = computed(() => repoNameFromUrl(draft.value.repoUrl))

/** Names the server would derive for the chosen language. */
const defaultAgentNames = computed<string[]>(() => deriveOnboardingAgentNames(draft.value.language))
const nameIssues = computed(() => teamNameIssues(draft.value.team))
const workflowPreview = computed(() => buildOnboardingWorkflowPreview(draft.value.team))
const reviewIncluded = computed(() => workflowPreview.value.nodes.some((n) => n.id === 'test_review'))
const enabledTeam = computed(() => draft.value.team.filter((m) => m.enabled))
const modelPlaceholder = computed(() => selectedModel.value?.name || c.value.selectConfiguration)
const successAgentNames = computed(() => {
  const team = result.value?.agentIds?.length ? result.value.agentIds : enabledTeam.value.map(member => member.name).filter(Boolean)
  return [...new Set([...team, ...(result.value?.soloAgentId ? [result.value.soloAgentId] : [])])]
})

watch(defaultAgentNames, (names) => applyDefaultTeamNames(draft.value.team, names), { immediate: true })

watch(
  () => props.open,
  (open) => {
    if (!open) { resourceRequest++; return }
    const inheritAppLocale = false
    draft.value = freshOnboardingDraft({ inheritAppLocale })
    if (localMode.value) {
      if (!draft.value.gitUserName) draft.value.gitUserName = hostGitUser.value.name || 'local'
      if (!draft.value.gitUserEmail) draft.value.gitUserEmail = hostGitUser.value.email || 'local@localhost'
    }
    // Opening the wizard must not overwrite the user's saved locale with browser/OS.
    if (!inheritAppLocale) {
      void setLocale(draft.value.language)
    }
    creating.value = false
    createError.value = ''
    phase.value = 'wizard'
    result.value = null
    modelValid.value = false
    gitValid.value = true
    showTeamErrors.value = false
    gitSetupOpen.value = false
    resourceEditing.value = false
    leaving.value = false
    applyDefaultTeamNames(draft.value.team, defaultAgentNames.value)
    stepAnimKey.value++
    void loadTemplates()
    void refreshResources()
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

function templateFor(id: OnboardingTemplateId): AgentTemplate | undefined {
  return templates.value.find((x) => x.id === id)
}

/** Closes for this view only; a reload re-opens it until the default workflow exists. */
function closeWizard() {
  if (creating.value || resourceEditing.value) return
  emit('close')
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

function validatePrefs(): boolean { return true }

function validateCredentials(): boolean {
  if (localMode.value) return true
  if (!resourcesLoading.value && !resourcesError.value && credentialsReady.value) return true
  toast.error(t('pages.onboarding.credentialsSetup.required'))
  return false
}

function validateModel(): boolean {
  if (!resourcesLoading.value && !resourcesError.value && modelValid.value && draft.value.modelConfigurationId) return true
  toast.error(c.value.selectConfiguration)
  return false
}

function validateGit(): boolean {
  if (gitIdentityConfigured(draft.value) && (draft.value.gitSkipped || gitValid.value)) return true
  toast.error(t('pages.onboarding.toastNeedGitUser'))
  return false
}

function validateTeam(): boolean {
  if (teamValid(draft.value.team)) return true
  showTeamErrors.value = true
  toast.error(t('pages.onboarding.team.toastFixNames'))
  return false
}

function memberIssue(id: OnboardingTemplateId) {
  const issue = nameIssues.value[id]
  if (issue === 'required' && !showTeamErrors.value) return ''
  return issue
}

function goToStep(id: OnboardingStepId) {
  draft.value.step = steps.value.findIndex((s) => s.id === id)
  stepAnimKey.value++
}

function goPrev() {
  if (draft.value.step === 0 || creating.value || resourceEditing.value || phase.value === 'success') return
  draft.value.step--
  stepAnimKey.value++
}

function goNext() {
  if (creating.value || resourceEditing.value) return
  const id = currentStep.value.id
  if (id === 'prefs' && !validatePrefs()) return
  if (id === 'credentials' && !validateCredentials()) return
  if (id === 'model' && !validateModel()) return
  if (id === 'git' && !validateGit()) return
  if (id === 'team' && !validateTeam()) return
  if (id === 'workflow') {
    void submitBootstrap()
    return
  }
  draft.value.step = Math.min(draft.value.step + 1, wizardStepCount.value - 1)
  stepAnimKey.value++
}

async function runBootstrap(body: ReturnType<typeof assembleBootstrapBody>) {
  const res = await api.bootstrapOnboarding(body)
  result.value = res
  phase.value = 'success'
  toast.success(t('pages.onboarding.toastOk'))
  emit('completed', res)
}

async function submitBootstrap() {
  const checks: [OnboardingStepId, () => boolean][] = [
    ['prefs', validatePrefs],
    ['credentials', validateCredentials],
    ['model', validateModel],
    ['git', validateGit],
    ['team', validateTeam],
  ]
  const failed = checks.find(([, ok]) => !ok())
  if (failed) {
    goToStep(failed[0])
    return
  }
  creating.value = true
  createError.value = ''
  try {
    await runBootstrap(assembleBootstrapBody(draft.value))
  } catch (e: any) {
    createError.value = e?.message || String(e)
    toast.error(createError.value || t('pages.onboarding.toastErr'))
  } finally {
    creating.value = false
  }
}

function goToWorkflows() {
  void router.push({ path: '/workflows' })
}

function finish() {
  goToWorkflows()
  emit('close')
}

async function runOnce() {
  const id = result.value?.workflowId
  if (!id || leaving.value) return
  leaving.value = true
  try {
    const wf = await api.getWorkflow(id)
    goToWorkflows()
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
  void router.push({ path: `/workflows/${id}/edit` })
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4" data-testid="onboarding-wizard">
      <div class="onb-backdrop absolute inset-0" data-testid="onboarding-backdrop" @click="closeWizard" />
      <div class="onb-dialog relative z-10 flex w-full overflow-hidden" role="dialog" aria-modal="true" aria-labelledby="onb-title">
        <aside class="onb-rail hidden w-[248px] shrink-0 flex-col md:flex" data-testid="onboarding-rail">
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
            :disabled="creating || resourceEditing"
            data-testid="onboarding-close"
            @click="closeWizard"
          >
            <Icon name="close" :size="17" />
          </button>

          <template v-if="phase === 'success'">
            <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-8 pt-10 sm:px-10" data-testid="onboarding-success">
              <div class="onb-success-mark grid h-12 w-12 place-items-center rounded-full">
                <Icon name="check" :size="24" />
              </div>
              <h3 ref="stepHeading" tabindex="-1" class="mt-5 text-[22px] font-semibold text-txt outline-none">
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
                      ><span>{{ n }}<span v-if="n === result?.soloAgentId" class="ml-2 text-[11px] text-txt3">{{ t('pages.onboarding.solo.emptyShort') }}</span></span>
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
            <footer class="onb-footer max-sm:flex-wrap max-sm:!px-3">
              <div class="flex-1" />
              <AppButton variant="ghost" data-testid="onboarding-success-close" @click="finish">
                {{ t('pages.onboarding.close') }}
              </AppButton>
            </footer>
          </template>

          <template v-else>
            <div ref="body" class="min-h-0 flex-1 overflow-y-auto px-5 pb-8 pt-8 sm:px-10" data-testid="onboarding-body">
              <div :key="stepAnimKey">
                <div class="text-[11.5px] font-medium text-accent-2">
                  {{
                    t('pages.onboarding.stepOf', {
                      n: activeIndex + 1,
                      total: wizardStepCount,
                    })
                  }}
                </div>
                <h3 ref="stepHeading" tabindex="-1" class="mt-1.5 text-[22px] font-semibold leading-8 text-txt outline-none">
                  {{ t(currentStep.labelKey) }}
                </h3>
                <p v-if="resourcesError" role="alert" class="onb-note is-err mt-3">{{ resourcesError }} <AppButton size="sm" @click="refreshResources">{{ c.retry }}</AppButton></p>

                <template v-if="currentStep.id === 'prefs'">
                  <p class="onb-lede">
                    {{ t('pages.onboarding.prefsMeta') }}
                  </p>
                  <div class="mt-7 space-y-7">
                    <section data-testid="onboarding-section-language">
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

                <template v-else-if="currentStep.id === 'credentials'">
                  <p class="onb-lede">{{ t('pages.onboarding.credentialsSetup.description') }}</p>
                  <WorkspaceCredentialsPanel class="mt-6" inline scope="model" view="credentials" @credential-saved="completeCredentialStep" @editing="resourceEditing = $event" />
                </template>

                <template v-else-if="currentStep.id === 'model'">
                  <p class="onb-lede">{{ t('pages.onboarding.toolSetup.description') }}</p>
                  <WorkspaceCredentialsPanel class="mt-6" inline scope="model" view="models" setup :configuration-id="draft.modelConfigurationId" @model-saved="selectSavedModel" @editing="resourceEditing = $event" />
                  <section v-if="!resourceEditing && models.length" class="onb-card mt-4" data-testid="onboarding-section-model">
                    <div class="onb-label">{{ t('pages.onboarding.toolSetup.existing') }}</div>
                    <ModelBindingEditor :key="resourcesVersion" v-model="modelBinding" :allow-all-configurations="false" @validity="modelValid = $event" />
                  </section>
                  <div v-if="regionPolicy && !resourceEditing" class="onb-seg mt-4" role="radiogroup" :aria-label="t('pages.onboarding.acp.region')"><button v-for="option in regionPolicy.options" :key="option.id" type="button" role="radio" class="onb-seg-item" :aria-checked="draft.region === option.id" @click="selectRegion(option.id)">{{ t(option.labelKey) }}</button></div>
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
                    <div class="mt-5 flex items-center gap-3">
                      <div class="onb-sub flex-1">
                        {{ t('pages.onboarding.git.repoCredSection') }}
                      </div>
                      <AppButton size="sm" variant="ghost" :disabled="resourceEditing" data-testid="onboarding-git-skip" @click="toggleGitSkipped">
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
                      <p v-if="repoDirName" class="onb-hint" data-testid="onboarding-repo-hint">
                        {{ t('pages.onboarding.repo.cloneTo', { dir: repoDirName }) }}
                      </p>
                      <template v-if="!localMode">
                      <div class="mt-4">
                        <AgentGitCredentialPicker :key="resourcesVersion" v-model="draft.gitBindings" hide-manage-link :disabled="resourceEditing" @validity="gitValid = $event" />
                      </div>
                      <div class="mt-5 border-t border-line pt-5">
                        <div class="flex items-center justify-between gap-3">
                          <div>
                            <h2 class="onb-card-title">{{ t('pages.onboarding.gitSetup.title') }}</h2>
                            <p class="onb-card-desc">{{ t('pages.onboarding.gitSetup.hint') }}</p>
                          </div>
                          <AppButton size="sm" variant="outline" :disabled="resourceEditing" :aria-expanded="gitSetupOpen" aria-controls="onboarding-git-setup" data-testid="onboarding-manage-git" @click="gitSetupOpen = !gitSetupOpen">{{ t(gitSetupOpen ? 'pages.onboarding.collapseSetup' : 'pages.onboarding.expandSetup') }}</AppButton>
                        </div>
                        <WorkspaceCredentialsPanel v-if="gitSetupOpen" id="onboarding-git-setup" class="mt-4" inline scope="git" @changed="refreshResources" @credential-saved="selectSavedGit" @editing="resourceEditing = $event" />
                      </div>
                      </template>
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
                  <div class="onb-team mt-6">
                  <template v-for="(m, i) in draft.team" :key="m.templateId">
                    <div v-if="i" class="onb-team-arrow" :class="{ 'is-off': !m.enabled }" aria-hidden="true">
                      <Icon name="chevron-right" :size="13" />
                    </div>
                    <OnboardingTeamCard
                      :index="i"
                      :member="m"
                      :capabilities="templateFor(m.templateId)?.capabilities"
                      :summary="templateFor(m.templateId)?.summary"
                      :name-placeholder="t('pages.onboarding.team.namePlaceholder')"
                      :model-placeholder="modelPlaceholder"
                      :model-configurations="models"
                      :issue="memberIssue(m.templateId)"
                      @update:model="m.modelConfigurationId = $event"
                    />
                  </template>
                </div>
                  <p class="onb-hint mt-3">
                    {{ t('pages.onboarding.team.foot') }}
                  </p>
                  <section class="onb-card mt-4" data-testid="onboarding-solo-agent">
                    <div class="flex items-start gap-3">
                      <span class="onb-avatar shrink-0">S</span>
                      <div>
                        <div class="onb-card-title">{{ ONBOARDING_SOLO_AGENT_NAME }}</div>
                        <p class="onb-card-desc">{{ t('pages.onboarding.solo.description') }}</p>
                      </div>
                    </div>
                  </section>
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
                      <div
                        v-for="item in summaryItems"
                        :key="item.id"
                        :class="{ 'col-span-2': 'wide' in item }"
                        :data-testid="`onboarding-review-${item.id}`"
                      >
                        <dt>{{ item.label }}</dt>
                        <dd :title="item.value">
                          <span class="onb-dot" :class="{ 'is-ok': item.ok }" /><span class="truncate">{{ item.value }}</span>
                        </dd>
                      </div>
                    </dl>
                  </section>
                  <p v-if="createError" class="onb-note is-err mt-3" data-testid="onboarding-create-error">
                    {{ createError }}
                  </p>
                </template>
              </div>
            </div>

            <footer v-show="!resourceEditing" class="onb-footer max-sm:flex-wrap max-sm:!px-3">
              <AppButton v-show="!resourceEditing" variant="ghost" data-testid="onboarding-later" :disabled="creating || resourceEditing" @click="closeWizard">
                {{ t('pages.onboarding.later') }}
              </AppButton>
              <div class="flex-1" />
              <AppButton v-if="draft.step > 0" v-show="!resourceEditing" variant="outline" :disabled="creating || resourceEditing" data-testid="onboarding-prev" @click="goPrev">
                <Icon name="chevron-left" :size="14" />{{ t('pages.onboarding.prev') }}
              </AppButton>
              <AppButton v-show="!resourceEditing" variant="primary" :disabled="creating || resourceEditing || resourcesLoading" data-testid="onboarding-next" @click="goNext">
                <Icon v-if="creating" name="spinner" :size="14" />
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
.onb-input.is-lg {
  height: 42px;
  font-size: 14px;
}
.onb-input.is-area {
  height: auto;
  padding: 8px 12px;
  font-size: 12px;
}
.onb-input::placeholder {
  font-family: theme('fontFamily.sans');
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
.onb-cred {
  border-radius: 14px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-base));
  overflow: hidden;
}
.onb-cred-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated) / 0.6);
}
.onb-cred-body {
  padding: 16px 18px 18px;
}
.onb-text-btn {
  flex-shrink: 0;
  font-size: 12px;
  color: rgb(var(--c-accent-2));
}
.onb-text-btn:hover {
  text-decoration: underline;
}
.onb-howto {
  margin-top: 18px;
}
.onb-howto-steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
  margin-top: 10px;
}
.onb-howto-steps li {
  display: flex;
  gap: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: rgb(var(--c-txt3));
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
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
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
.onb-team {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 20px minmax(0, 1fr) 20px minmax(0, 1fr) 20px minmax(0, 1fr);
  align-items: stretch;
}
.onb-team-arrow {
  display: grid;
  place-items: center;
  align-self: center;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
  margin: 0 -4px;
  z-index: 1;
}
.onb-team-arrow.is-off {
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-elevated));
}
.onb-canvas {
  padding: 16px 20px 14px;
  background:
    radial-gradient(circle, rgb(var(--c-line-strong) / 0.55) 1px, transparent 1.2px) 0 0 / 16px 16px,
    rgb(var(--c-base));
}
.onb-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px 20px;
}
.onb-summary > div {
  min-width: 0;
}
.onb-summary dt {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  line-height: 16px;
  color: rgb(var(--c-txt3));
}
.onb-summary dd {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  margin-top: 2px;
  font-size: 12.5px;
  line-height: 18px;
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
