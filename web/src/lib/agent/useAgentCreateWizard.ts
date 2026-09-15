/**
 * Agent create wizard UI orchestration (step machine in agentCreateWizard.ts).
 */
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type Agent } from '@/lib/api/api'
import {
  ACP_BACKENDS,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  WIZARD_STEPS,
  applyAcpBackend,
  applyWizardStartPath,
  assembleCreatePayload,
  buildReviewSummary,
  freshDraft,
  hasPathDeps,
  hasRoleTemplate,
  kvToRec,
  parseCustomConfigJson,
  stripAuthKeysFromEnv,
  validateBasics,
  type StartPath,
  type WizardAuthMode,
  type WizardBackendId,
  type WizardDraft,
  type WizardStepId,
} from '@/lib/agent/agentCreateWizard'
import { buildTemplateOptions } from '@/lib/agent/agentTemplateOptions'
import type { AgentTemplateOption } from '@/components/agent/AgentTemplateSelect.vue'
import { backendForStartPath } from '@/lib/shared/startPath'
import { authGuideFor, defaultSettingsPlaceholder, hasAuthKeyConfigured } from '@/lib/agent/backendAuthGuide'
import type { GitCredentialType } from '@/lib/agent/gitCredentialAnalysis'
import { getRegionPolicy, setRegion } from '@/lib/shared/regionPolicy'
import { useInheritedGitEnv } from '@/lib/agent/useInheritedGitEnv'
import {
  applyOpenCodeFields,
  openCodeCustomBaseRequired,
  openCodeFieldsFromEnv,
  openCodeModelRequired,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'

export interface AgentCreateWizardProps {
  open: boolean
  existingNames: string[]
  /** When set, Git step can inherit project shared Agent env for hide/infer. */
  projectId?: string
}

export type AgentCreateWizardEmit = {
  (e: 'close'): void
  (e: 'created', agent: Agent): void
}


export function useAgentCreateWizard(props: AgentCreateWizardProps, emit: AgentCreateWizardEmit) {
const { t } = useI18n()
const { inheritedEnv } = useInheritedGitEnv(() => props.projectId)

const draft = ref<WizardDraft>(freshDraft())
const nameError = ref('')
const creating = ref(false)
const createError = ref('')
const pendingAcp = ref<WizardBackendId | null>(null)
const showAcpConfirm = ref(false)
const envHelpOpen = ref(false)
const stepAnimKey = ref(0)
const apiKeyInput = ref('')
const customConfigError = ref(false)
const openCodeBaseError = ref(false)
const openCodeModelError = ref(false)
const customConfigDraft = ref('')
const templateOptions = ref<AgentTemplateOption[]>([])

const showDescField = computed(() => !hasRoleTemplate(draft.value))
const templateHint = computed(() => {
  const id = (draft.value.templateId || 'blank').trim()
  if (id === 'blank') return t('pages.agentStudio.wizard.basics.templateHintBlank')
  const opt = templateOptions.value.find((o) => o.id === id)
  if (!opt) return t('pages.agentStudio.wizard.basics.templateHintPack', { name: id })
  return t('pages.agentStudio.wizard.basics.templateHintPack', {
    name: opt.subtitle || opt.name,
  })
})

async function loadTemplates() {
  try {
    const res = await api.listAgentTeamTemplates()
    templateOptions.value = buildTemplateOptions(
      res.items || [],
      t('pages.agentStudio.wizard.basics.templateBlank'),
      t('pages.agentStudio.wizard.basics.templateBlankSub'),
    )
  } catch {
    // Offline / API miss: still offer blank + pinned packs so UI is usable.
    templateOptions.value = buildTemplateOptions(
      [
        { id: 'test', embedName: 'TestAgent', roleLabelZh: '测试工程师', summary: '测试验证' },
        {
          id: 'preflight',
          embedName: 'PreflightAgent',
          roleLabelZh: '环境确认工程师',
          summary: '环境确认',
        },
      ],
      t('pages.agentStudio.wizard.basics.templateBlank'),
      t('pages.agentStudio.wizard.basics.templateBlankSub'),
    )
  }
}

onMounted(() => {
  void loadTemplates()
})

function onTemplateSelect(id: string) {
  // plan g1.4 — template change must not rewrite draft.name
  draft.value.templateId = id
  if (hasRoleTemplate(draft.value)) {
    draft.value.description = ''
  }
}

const currentStep = computed(() => WIZARD_STEPS[draft.value.step])
const progressPct = computed(() => ((draft.value.step + 1) / WIZARD_STEPS.length) * 100)
const reviewItems = computed(() => buildReviewSummary(draft.value))
const currentRegionPolicy = computed(() => getRegionPolicy(draft.value.acpBackend))
const currentRegion = computed(() => {
  const policy = currentRegionPolicy.value
  if (!policy) return ''
  return draft.value.env.find((item) => item.k.trim() === policy.regionEnvKey)?.v || ''
})
const authGuide = computed(() => authGuideFor(draft.value.acpBackend, currentRegion.value))
const authConfigured = computed(() => {
  if (draft.value.authMode === 'customConfig') {
    const parsed = parseCustomConfigJson(draft.value.customConfigContent)
    return parsed.ok && parsed.normalized !== ''
  }
  return hasAuthKeyConfigured(draft.value.env, draft.value.acpBackend)
})
const showAuthReminder = computed(() => !authConfigured.value)

const primaryAuthKey = computed(() => authGuide.value.keys[0]?.key || '')
const primaryAuthAlt = computed(() => authGuide.value.keys[0]?.alt || '')

const headSub = computed(() => {
  const id = currentStep.value.id
  if (id === 'review') return t('pages.agentStudio.wizard.head.review')
  if (id === 'basics') return t('pages.agentStudio.wizard.head.basics')
  return t('pages.agentStudio.wizard.head.optional')
})

watch(
  () => props.open,
  (open) => {
    if (open) {
      draft.value = freshDraft()
      nameError.value = ''
      createError.value = ''
      creating.value = false
      pendingAcp.value = null
      showAcpConfirm.value = false
      envHelpOpen.value = false
      apiKeyInput.value = ''
      customConfigError.value = false
      customConfigDraft.value = ''
      openCodeBaseError.value = false
      openCodeModelError.value = false
      stepAnimKey.value++
      void loadTemplates()
      nextTick(() => {
        document.getElementById('wiz-name-input')?.focus()
      })
    }
  },
)

watch(
  () => [draft.value.acpBackend, currentRegion.value] as const,
  () => {
    const key = primaryAuthKey.value
    const row = draft.value.env.find((e) => e.k === key)
    apiKeyInput.value = row?.v ?? ''
  },
)

function close() {
  if (creating.value) return
  emit('close')
}

function upsertEnv(key: string, value: string) {
  const row = draft.value.env.find((e) => e.k === key)
  if (row) row.v = value
  else draft.value.env.push({ k: key, v: value })
}

function selectRegion(region: string) {
  const env = Object.fromEntries(
    draft.value.env.filter((item) => item.k.trim()).map((item) => [item.k.trim(), item.v]),
  )
  draft.value.env = Object.entries(setRegion(env, draft.value.acpBackend, region)).map(
    ([k, v]) => ({ k, v }),
  )
  markConfigured('acp')
}

function markConfigured(step: WizardStepId) {
  delete draft.value.skipped[step]
}

function selectAcp(id: WizardBackendId) {
  if (id === draft.value.acpBackend) return
  if (hasPathDeps(draft.value)) {
    pendingAcp.value = id
    showAcpConfirm.value = true
    return
  }
  applyAcpBackend(draft.value, id)
  markConfigured('acp')
  syncApiKeyInput()
}

function selectStartPath(path: StartPath) {
  if (path === draft.value.startPath) return
  const nextId = backendForStartPath(path, draft.value.cliBackend)
  if (nextId !== draft.value.acpBackend && hasPathDeps(draft.value)) {
    pendingAcp.value = nextId
    showAcpConfirm.value = true
    return
  }
  applyWizardStartPath(draft.value, path)
  markConfigured('acp')
  syncApiKeyInput()
}

function confirmAcpSwitch() {
  if (pendingAcp.value) {
    applyAcpBackend(draft.value, pendingAcp.value)
    markConfigured('acp')
    syncApiKeyInput()
  }
  pendingAcp.value = null
  showAcpConfirm.value = false
}

function cancelAcpSwitch() {
  pendingAcp.value = null
  showAcpConfirm.value = false
}

function syncApiKeyInput() {
  const key = primaryAuthKey.value
  const row = draft.value.env.find((e) => e.k === key)
  apiKeyInput.value = row?.v ?? ''
}

function clearAuthKeys() {
  draft.value.env = stripAuthKeysFromEnv(draft.value.env, draft.value.acpBackend)
  apiKeyInput.value = ''
}

function setAuthMode(mode: WizardAuthMode) {
  if (mode === draft.value.authMode) return
  customConfigError.value = false
  if (mode === 'apiKey') {
    customConfigDraft.value = draft.value.customConfigContent
    draft.value.customConfigContent = ''
    draft.value.authMode = mode
    syncApiKeyInput()
    return
  }
  clearAuthKeys()
  draft.value.authMode = mode
  draft.value.customConfigContent =
    customConfigDraft.value || defaultSettingsPlaceholder(draft.value.acpBackend)
  markConfigured('apiKey')
}

function onCustomConfigInput(value: string) {
  draft.value.customConfigContent = value
  customConfigError.value = false
  if (value.trim()) markConfigured('apiKey')
}

function patchOpenCode(fields: Parameters<typeof applyOpenCodeFields>[1]) {
  draft.value.env = Object.entries(applyOpenCodeFields(kvToRec(draft.value.env), fields)).map(
    ([k, v]) => ({ k, v }),
  )
  openCodeBaseError.value = false
  openCodeModelError.value = false
}

function onOpenCodeProvider(value: OpenCodeProviderId) {
  patchOpenCode({ provider: value })
  markConfigured('apiKey')
}

function onOpenCodeBaseURL(value: string) {
  patchOpenCode({ baseURL: value })
  markConfigured('apiKey')
}

function onOpenCodeModel(value: string) {
  patchOpenCode({ model: value })
  markConfigured('apiKey')
}

function onOpenCodeVision(value: boolean) {
  patchOpenCode({ vision: value })
  markConfigured('apiKey')
}

function onApiKeyInput(value: string) {
  apiKeyInput.value = value
  const key = primaryAuthKey.value
  if (!key) return
  if (value.trim()) {
    upsertEnv(key, value)
    markConfigured('apiKey')
  } else {
    const idx = draft.value.env.findIndex((e) => e.k === key)
    if (idx >= 0) draft.value.env.splice(idx, 1)
  }
}

function onGitCredentialType(value: GitCredentialType) {
  draft.value.gitCredentialType = value
  markConfigured('git')
}

function goPrev() {
  if (draft.value.step === 0 || creating.value) return
  draft.value.step--
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

function goSkip() {
  const step = currentStep.value
  if (!step.skip || creating.value) return
  draft.value.skipped[step.id] = true
  if (step.id === 'apiKey') {
    clearAuthKeys()
    draft.value.customConfigContent = ''
    customConfigDraft.value = ''
    customConfigError.value = false
  }
  draft.value.step++
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

function validateApiKeyStep(): boolean {
  if (draft.value.acpBackend === 'opencode' && draft.value.authMode === 'apiKey') {
    const fields = openCodeFieldsFromEnv(kvToRec(draft.value.env))
    if (openCodeModelRequired(fields.model)) {
      openCodeModelError.value = true
      return false
    }
    if (openCodeCustomBaseRequired(fields.provider, fields.baseURL)) {
      openCodeBaseError.value = true
      return false
    }
  }
  if (draft.value.authMode !== 'customConfig') return true
  const parsed = parseCustomConfigJson(draft.value.customConfigContent)
  if (!parsed.ok) {
    customConfigError.value = true
    return false
  }
  customConfigError.value = false
  return true
}

function goNext() {
  if (creating.value) return
  const step = currentStep.value
  if (step.id === 'basics') {
    const err = validateBasics(draft.value, props.existingNames)
    if (err) {
      nameError.value =
        err === 'required'
          ? t('pages.agentStudio.dialogs.nameRequired')
          : err === 'invalid'
            ? t('pages.agentStudio.dialogs.nameInvalid')
            : t('pages.agentStudio.dialogs.nameExists')
      return
    }
    nameError.value = ''
  }
  if (step.id === 'apiKey' && !validateApiKeyStep()) return
  if (step.id === 'review') {
    void submitCreate()
    return
  }
  markConfigured(step.id)
  draft.value.step++
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

async function submitCreate() {
  const err = validateBasics(draft.value, props.existingNames)
  if (err) {
    draft.value.step = 0
    nameError.value =
      err === 'required'
        ? t('pages.agentStudio.dialogs.nameRequired')
        : err === 'invalid'
          ? t('pages.agentStudio.dialogs.nameInvalid')
          : t('pages.agentStudio.dialogs.nameExists')
    stepAnimKey.value++
    return
  }
  creating.value = true
  createError.value = ''
  try {
    const payload = assembleCreatePayload(draft.value)
    if (props.projectId) payload.projectId = props.projectId
    const created = await api.createAgent(payload)
    emit('created', created)
    emit('close')
  } catch (e: any) {
    createError.value = e?.message || String(e)
  } finally {
    creating.value = false
  }
}

function chipClass(kind: string) {
  if (kind === 'ok') return 'border-ok/35 bg-ok/10 text-ok'
  if (kind === 'def') return 'border-accent/35 bg-accent-dim text-accent-2'
  return 'border-line bg-elevated text-txt3'
}

  return {
  t,
  draft,
  nameError,
  creating,
  createError,
  pendingAcp,
  showAcpConfirm,
  envHelpOpen,
  stepAnimKey,
  apiKeyInput,
  customConfigError,
  openCodeBaseError,
  openCodeModelError,
  currentStep,
  progressPct,
  reviewItems,
  currentRegionPolicy,
  currentRegion,
  authGuide,
  authConfigured,
  showAuthReminder,
  primaryAuthKey,
  primaryAuthAlt,
  headSub,
  templateOptions,
  showDescField,
  templateHint,
  onTemplateSelect,
  close,
  upsertEnv,
  selectRegion,
  markConfigured,
  selectAcp,
  selectStartPath,
  confirmAcpSwitch,
  cancelAcpSwitch,
  syncApiKeyInput,
  setAuthMode,
  onCustomConfigInput,
  onApiKeyInput,
  onOpenCodeProvider,
  onOpenCodeBaseURL,
  onOpenCodeModel,
  onOpenCodeVision,
  onGitCredentialType,
  inheritedEnv,
  goPrev,
  goSkip,
  goNext,
  submitCreate,
  chipClass,
  WIZARD_STEPS,
  ACP_BACKENDS,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  }
}
