/** Agent creation binds one saved model configuration and an explicit Git list. */
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type Agent, type AgentGitBindings, type AgentTemplate, type CredentialItem, type ModelConfiguration, type SharedAgentConfig } from '@/lib/api/api'
import { ACP_BACKENDS, WIZARD_STEPS, applyAcpBackend, assembleCreatePayload, buildReviewSummary, freshDraft, hasPathDeps, hasRoleTemplate, kvToRec, validateBasics, type WizardBackendId, type WizardDraft, type WizardStepId } from './agentCreateWizard'
import { buildTemplateOptions } from './agentTemplateOptions'
import { getRegionPolicy, setRegion } from '@/lib/shared/regionPolicy'
import { setDraftGitBindings } from './gitBindings'
import { useRoutingCopy } from './modelRouting'
export interface AgentCreateWizardProps { open: boolean; existingNames: string[] }
export type AgentCreateWizardEmit = { (e: 'close'): void; (e: 'created', agent: Agent): void }
export function useAgentCreateWizard(props: AgentCreateWizardProps, emit: AgentCreateWizardEmit) {
  const { t, te } = useI18n()
  const c = useRoutingCopy()
  const draft = ref<WizardDraft>(freshDraft())
  const nameError = ref(''); const creating = ref(false); const createError = ref('')
  const pendingAcp = ref<WizardBackendId | null>(null); const showAcpConfirm = ref(false); const envHelpOpen = ref(false); const stepAnimKey = ref(0)
  const templateRows = ref<AgentTemplate[]>([]); const credentialItems = ref<CredentialItem[]>([]); const modelConfigurations = ref<ModelConfiguration[]>([])
  const sharedConfig = ref<SharedAgentConfig>()
  const effectiveModel = computed(() => draft.value.inheritModel ? sharedConfig.value?.modelBinding : draft.value.modelBinding)
  watch(() => draft.value.modelBinding?.configurationId, (id, previous) => {
    if (id?.trim() && id !== previous) draft.value.inheritModel = false
  }, { flush: 'sync' })
  const templateOptions = computed(() => buildTemplateOptions(templateRows.value, t, te))
  const selectedTemplate = computed(() => templateOptions.value.find((row) => row.id === draft.value.templateId) || null)
  const showDescField = computed(() => !hasRoleTemplate(draft.value))
  const templateHint = computed(() => hasRoleTemplate(draft.value) ? t('pages.agentStudio.wizard.basics.templateHintPack', { name: selectedTemplate.value?.name || draft.value.templateId }) : t('pages.agentStudio.wizard.basics.templateHintBlank'))
  const currentStep = computed(() => WIZARD_STEPS[draft.value.step]!)
  const progressPct = computed(() => ((draft.value.step + 1) / WIZARD_STEPS.length) * 100)
  const currentRegionPolicy = computed(() => getRegionPolicy(draft.value.acpBackend))
  const currentRegion = computed(() => draft.value.env.find((row) => row.k.trim() === currentRegionPolicy.value?.regionEnvKey)?.v || '')
  const authConfigured = computed(() => !!effectiveModel.value?.configurationId)
  const showAuthReminder = computed(() => !authConfigured.value)
  const headSub = computed(() => t(`pages.agentStudio.wizard.head.${currentStep.value.id === 'review' ? 'review' : currentStep.value.id === 'basics' ? 'basics' : 'optional'}`))
  function alias(id: string) { return credentialItems.value.find((row) => row.id === id)?.name || id }
  const reviewItems = computed(() => {
    const configuration = modelConfigurations.value.find((row) => row.id === effectiveModel.value?.configurationId)
    const effectiveDraft = { ...draft.value, ...(draft.value.inheritModel ? { modelBinding: effectiveModel.value, acpBackend: ACP_BACKENDS.find((row) => row.id === configuration?.backend)?.id || draft.value.acpBackend } : {}), ...(draft.value.inheritGit ? { gitBindings: sharedConfig.value?.gitBindings || { items: [] } } : {}) }
    return buildReviewSummary(effectiveDraft, { model: configuration?.name || effectiveModel.value?.configurationId, gitItems: Object.fromEntries((effectiveDraft.gitBindings?.items || []).map((row) => [row.credentialId, alias(row.credentialId)])), preferredLabel: t('pages.agentStudio.gitCredentials.preferred') })
  })
  async function loadOptions() {
    sharedConfig.value = undefined
    const [templates, keys, models, defaults] = await Promise.allSettled([api.listAgentTeamTemplates(), api.listCredentials(), api.listModelConfigurations(), Promise.resolve().then(() => api.getSharedAgentConfig())])
    templateRows.value = templates.status === 'fulfilled' ? templates.value.items || [] : []
    credentialItems.value = keys.status === 'fulfilled' ? keys.value.items || [] : []
    modelConfigurations.value = models.status === 'fulfilled' ? models.value.items || [] : []
    if (defaults.status === 'fulfilled') sharedConfig.value = defaults.value
  }
  onMounted(loadOptions)
  watch(() => props.open, (open) => { if (open) { draft.value = freshDraft(); nameError.value = ''; createError.value = ''; creating.value = false; pendingAcp.value = null; showAcpConfirm.value = false; envHelpOpen.value = false; stepAnimKey.value++; void loadOptions(); void nextTick(() => document.getElementById('wiz-name-input')?.focus()) } })
  function onTemplateSelect(id: string) { draft.value.templateId = id; if (hasRoleTemplate(draft.value)) draft.value.description = '' }
  function close() { if (!creating.value) emit('close') }
  function markConfigured(step: WizardStepId) { delete draft.value.skipped[step] }
  function selectRegion(value: string) { draft.value.env = Object.entries(setRegion(kvToRec(draft.value.env), draft.value.acpBackend, value)).map(([k, v]) => ({ k, v })); markConfigured('acp') }
  function selectAcp(id: WizardBackendId) {
    if (id === draft.value.acpBackend) return
    if (hasPathDeps(draft.value)) { pendingAcp.value = id; showAcpConfirm.value = true; return }
    applyAcpBackend(draft.value, id); markConfigured('acp')
  }
  function confirmAcpSwitch() { if (pendingAcp.value) applyAcpBackend(draft.value, pendingAcp.value); pendingAcp.value = null; showAcpConfirm.value = false; markConfigured('acp') }
  function cancelAcpSwitch() { pendingAcp.value = null; showAcpConfirm.value = false }
  function onGitBindings(value: AgentGitBindings) { draft.value.inheritGit = false; setDraftGitBindings(draft.value, value); markConfigured('git') }
  function goPrev() { if (draft.value.step && !creating.value) { draft.value.step--; stepAnimKey.value++ } }
  function goSkip() { if (currentStep.value.id !== 'git' || creating.value) return; draft.value.skipped.git = true; draft.value.inheritGit = false; setDraftGitBindings(draft.value, { items: [] }); draft.value.step++; stepAnimKey.value++ }
  function validateName() {
    const error = validateBasics(draft.value, props.existingNames)
    nameError.value = error ? t(`pages.agentStudio.dialogs.${error === 'required' ? 'nameRequired' : error === 'invalid' ? 'nameInvalid' : 'nameExists'}`) : ''
    return !error
  }
  function validModel() {
    const selected = modelConfigurations.value.find((row) => row.id === effectiveModel.value?.configurationId && row.state === 'active')
    if (!selected) { createError.value = c.value.missingConfiguration; return false }
    if (!draft.value.inheritModel && selected.backend !== draft.value.acpBackend) { createError.value = c.value.configurationBackendMismatch; return false }
    createError.value = ''; return true
  }
  async function submitCreate() {
    if (!validateName()) { draft.value.step = 0; stepAnimKey.value++; return }
    if (!validModel()) { draft.value.step = 1; stepAnimKey.value++; return }
    creating.value = true; createError.value = ''
    try { const created = await api.createAgent(assembleCreatePayload(draft.value)); emit('created', created); emit('close') }
    catch (cause) { createError.value = String((cause as Error).message || cause) }
    finally { creating.value = false }
  }
  function goNext() { if (creating.value || (currentStep.value.id === 'basics' && !validateName()) || (currentStep.value.id === 'acp' && !validModel())) return; if (currentStep.value.id === 'review') { void submitCreate(); return }; markConfigured(currentStep.value.id); draft.value.step++; stepAnimKey.value++ }
  function chipClass(kind: string) { return kind === 'ok' ? 'border-ok/35 bg-ok/10 text-ok' : 'border-line bg-elevated text-txt3' }
  return { t, draft, sharedConfig, effectiveModel, nameError, creating, createError, pendingAcp, showAcpConfirm, envHelpOpen, stepAnimKey, credentialItems, currentStep, progressPct, reviewItems, currentRegionPolicy, currentRegion, authConfigured, showAuthReminder, headSub, templateOptions, showDescField, templateHint, selectedTemplate, onTemplateSelect, close, selectRegion, markConfigured, selectAcp, confirmAcpSwitch, cancelAcpSwitch, onGitBindings, goPrev, goSkip, goNext, submitCreate, chipClass, WIZARD_STEPS, ACP_BACKENDS }
}
