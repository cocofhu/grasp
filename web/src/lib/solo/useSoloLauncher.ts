import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api/api'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import type { Agent, CliCatalogModel, CredentialItem, MessageReference, ModelConfiguration, ModelConnection, Project, SoloCreateBody, SoloSession } from '@/lib/api/apiTypes'
import type { ClarifyImage } from '@/lib/shared/types'
import { boundModelChoices } from './configuredModels'
import { parseModelRoute, boundConfigurationIds, effectiveModelAgent } from '@/lib/agent/modelBindings'
import { ensureModelCatalogEvents, modelCatalogRevision } from '@/lib/api/modelCatalogEvents'
import { soloPrefs } from './soloPrefs'
import { isSoloLimitError, soloErrorText } from './soloErrors'
import { soloCreateChoices } from './sessionRepos'
import { useSoloSessions } from './useSoloSessions'
import { ONBOARDING_SOLO_AGENT_NAME } from '@/lib/pm/onboardingWizard'
import { firstInstallCompletedAt } from '@/lib/pm/firstInstall'
import { useAuth } from '@/lib/composables/useAuth'
import { newSoloRequestID, readSoloOutbox, removeSoloOutbox, saveSoloOutbox, soloOutboxScope, type SoloCreationOutbox } from './soloOutbox'

function listedEfforts(efforts?: string[]) {
  return [...new Set((efforts || []).map(value => value.trim()).filter(Boolean))]
}

/**
 * Agent / repository selection and session creation shared by the home
 * composer and the /solo "new session" dialog. Repository ids persist across
 * visits; branch overrides apply only to the current launch.
 */
export function useSoloLauncher() {
  const { t, te, locale } = useI18n()
  const router = useRouter()
  const store = useSoloSessions()
  const auth = useAuth()
  const principal = computed(() => auth.user.value?.username || 'anonymous')

  const agents = ref<Agent[]>([])
  const projects = ref<Project[]>([])
  const configurations = ref<ModelConfiguration[]>([])
  const connections = ref<ModelConnection[]>([])
  const credentials = ref<CredentialItem[]>()
  const cliModels = ref<Record<string, CliCatalogModel[]>>({})
  const cliReady = ref(false)
  const agentName = ref(soloPrefs.lastAgent())
  const projectIds = ref<string[]>(soloPrefs.lastProjects())
  const branches = ref<Record<string, string>>({})
  const initialModelId = ref('')
  const reasoningEffort = ref('')
  const loading = ref(false)
  const creating = ref(false)
  const error = ref('')
  const limitOpen = ref(false)
  const pendingCreation = ref<SoloCreationOutbox | null>(null)
  const recoveredSession = ref<SoloSession | null>(null)
  const recoveryChecked = ref(false)
  const recovering = ref(false)
  const authorizedScope = ref('')
  // Deep-link dialogs turn this off so a preselect does not overwrite the home page's ids.
  let persistProjects = true
  let hasLoaded = false
  let loadGeneration = 0
  let recoveryGeneration = 0
  let launchGeneration = 0
  let disposed = false

  const agent = computed(() => agents.value.find((a) => a.name === agentName.value) ?? null)
  const modelChoices = computed(() => boundModelChoices({
    binding: agent.value?.modelBinding, configurations: configurations.value, connections: connections.value,
    cliModels: cliModels.value, cliReady: cliReady.value, credentials: credentials.value,
  }))
  const canStart = computed(() => !!agent.value && !creating.value && !recovering.value && !pendingCreation.value && !!authorizedScope.value)

  onScopeDispose(() => { disposed = true; loadGeneration++; recoveryGeneration++; launchGeneration++ })
  watch(principal, () => {
    loadGeneration++
    recoveryGeneration++
    launchGeneration++
    authorizedScope.value = ''
    pendingCreation.value = null
    recoveredSession.value = null
    recoveryChecked.value = false
    recovering.value = false
    creating.value = false
    error.value = ''
    if (hasLoaded) void load()
  })

  watch(agentName, (v) => soloPrefs.setLastAgent(v))
  watch(agentName, () => { initialModelId.value = ''; reasoningEffort.value = '' })
  watch([initialModelId, modelChoices], ([modelId, choices]) => {
    const selected = modelId ? choices.find(choice => choice.id === modelId) : choices[0]
    const listed = listedEfforts(selected?.efforts)
    if (reasoningEffort.value && !listed.includes(reasoningEffort.value)) {
      const next = listed.at(-1) || ''
      reasoningEffort.value = next
      if (next) error.value = t('pages.solo.modelSelect.effortResetNotice')
    }
  })
  watch(modelChoices, (choices) => {
    const selected = initialModelId.value
    if (!selected) return
    const hit = choices.find((choice) => choice.id === selected)
    if (!hit || hit.unavailable) {
      error.value = t('pages.solo.modelSelect.unavailableSelection')
    }
  })
  let cliGeneration = 0
  watch(() => boundConfigurationIds(agent.value?.modelBinding, configurations.value).map(id => configurations.value.find(row => row.id === id)?.backend || '').join('|'), async (backends) => {
    const generation = ++cliGeneration
    cliReady.value = false
    const rows = await Promise.all([...new Set(backends.split('|').filter(Boolean))].map(async backend => [backend, await loadCliModels(backend)] as const))
    if (generation !== cliGeneration) return
    cliModels.value = Object.fromEntries(rows)
    cliReady.value = true
  })
  ensureModelCatalogEvents()
  watch(modelCatalogRevision, () => { if (hasLoaded) void load() })
  watch(projectIds, (ids) => {
    if (persistProjects) soloPrefs.setLastProjects(ids)
    const keep = new Set(ids)
    const next: Record<string, string> = {}
    for (const [id, branch] of Object.entries(branches.value)) {
      if (keep.has(id) && branch) next[id] = branch
    }
    branches.value = next
  })
  watch(firstInstallCompletedAt, (completedAt) => {
    if (completedAt && hasLoaded) void load()
  })

  async function load() {
    hasLoaded = true
    const generation = ++loadGeneration
    const owner = principal.value
    loading.value = true
    try {
      const [a, p, configs, services, keys] = await Promise.all([
        api.listAgents(),
        api.listProjects().catch(() => [] as Project[]),
        api.listModelConfigurations(),
        api.listModelConnections(),
        Promise.resolve().then(() => api.listCredentials?.()).then(value => value?.items).catch(() => undefined),
        store.refresh(),
      ])
      const defaults = a.some(agent => agent.inheritModel) ? await api.getSharedAgentConfig() : undefined
      if (disposed || generation !== loadGeneration || principal.value !== owner) return
      agents.value = a.map(agent => effectiveModelAgent(agent, defaults))
      projects.value = p
      configurations.value = configs.items
      connections.value = services.items
      if (keys !== undefined) credentials.value = keys
      if (agentName.value && !a.some((x) => x.name === agentName.value)) agentName.value = ''
      if (!agentName.value) {
        const defaultAgent = a.find((item) => item.name === ONBOARDING_SOLO_AGENT_NAME)
        agentName.value = defaultAgent?.name || (a.length === 1 ? a[0]!.name : '')
      }
      projectIds.value = projectIds.value.filter((id) => p.some((x) => x.id === id))
      // Authorized reads must succeed before a local draft belonging to this
      // account may be displayed. No browser record is an authentication token.
      const scope = soloOutboxScope(owner, 'creation')
      const saved = await readSoloOutbox(scope).catch(() => { throw new Error(t('pages.solo.outboxUnavailable')) })
      if (disposed || generation !== loadGeneration || principal.value !== owner) return
      authorizedScope.value = scope
      pendingCreation.value = saved.find((record): record is SoloCreationOutbox => record.kind === 'creation') || null
      recoveredSession.value = null
      recoveryChecked.value = false
      if (pendingCreation.value) await checkPendingCreation()
    } catch (cause) {
      if (generation === loadGeneration) error.value = String((cause as Error).message || cause)
    } finally {
      if (generation === loadGeneration) loading.value = false
    }
  }

  function toggleProject(id: string) {
    const current = projectIds.value
    projectIds.value = current.includes(id) ? current.filter((x) => x !== id) : [...current, id]
  }

  function clearProjects() {
    projectIds.value = []
    branches.value = {}
  }

  function selectOnly(id: string) {
    projectIds.value = id ? [id] : []
    branches.value = {}
  }

  function setPersistProjects(next: boolean) {
    persistProjects = next
  }

  function setProjectBranch(id: string, branch: string) {
    branches.value = { ...branches.value, [id]: branch }
  }

  function onProjectSaved(p: Project) {
    const i = projects.value.findIndex((x) => x.id === p.id)
    if (i >= 0) projects.value.splice(i, 1, p)
    else projects.value.unshift(p)
    if (!projectIds.value.includes(p.id)) projectIds.value = [...projectIds.value, p.id]
  }

  function ownsRequest(record: SoloCreationOutbox) {
    return !disposed && authorizedScope.value === record.scope && record.scope === soloOutboxScope(principal.value, 'creation')
  }

  function verifyReceipt(record: SoloCreationOutbox, session: SoloSession) {
    if (!session.id || session.clientRequestId !== record.id || session.userId !== principal.value ||
      (!record.continuationRunId && hasFirstMessage(record.request) && !session.initialTurnId)) {
      throw new Error(t('pages.solo.invalidReceipt'))
    }
  }

  function hasFirstMessage(request: SoloCreateBody) {
    return !!(request.text?.trim() || request.images?.length || request.references?.length)
  }

  /** Reconcile after refresh using GET only. Sending always needs a click. */
  async function checkPendingCreation(): Promise<SoloSession | null> {
    const record = pendingCreation.value
    if (!record || !ownsRequest(record) || recovering.value || creating.value) return null
    const generation = ++recoveryGeneration
    recovering.value = true
    recoveryChecked.value = false
    try {
      const session = await api.resolveSoloCreation(record.id)
      if (!ownsRequest(record) || generation !== recoveryGeneration) return null
      verifyReceipt(record, session)
      recoveredSession.value = session
      recoveryChecked.value = true
      return session
    } catch (cause) {
      if (!ownsRequest(record) || generation !== recoveryGeneration) return null
      recoveredSession.value = null
      if ((cause as { status?: number; code?: string })?.status === 404 && (cause as { code?: string })?.code === 'creation_not_found') {
        recoveryChecked.value = true
      } else {
        error.value = soloErrorText(cause, t, te, locale.value)
      }
      return null
    } finally {
      if (generation === recoveryGeneration) recovering.value = false
    }
  }

  async function acceptCreation(record: SoloCreationOutbox, session: SoloSession): Promise<SoloSession | null> {
    if (!ownsRequest(record)) return null
    verifyReceipt(record, session)
    // Continuing an already linked workflow is navigation, not acceptance of
    // this opening message. Preserve it as a separate, explicitly retried turn.
    try {
      if (record.continuationRunId && hasFirstMessage(record.request) && !session.initialTurnId) {
        const id = `creation:${record.id}`
        await saveSoloOutbox({
          scope: soloOutboxScope(session.userId, 'message', session.id), id, kind: 'message', createdAt: record.createdAt,
          request: { clientMessageId: id, text: record.request.text || '', images: record.request.images, references: record.request.references },
        })
      }
      await removeSoloOutbox(record.scope, record.id)
    } catch { throw new Error(t('pages.solo.outboxUnavailable')) }
    if (!ownsRequest(record)) return null
    pendingCreation.value = null
    recoveredSession.value = null
    recoveryChecked.value = false
    error.value = ''
    soloPrefs.setLastProjects(record.request.repos?.map(repo => repo.projectId) || [])
    store.upsert(session)
    void router.push(`/solo/${session.id}`)
    return session
  }

  async function postCreation(record: SoloCreationOutbox): Promise<SoloSession | null> {
    if (!ownsRequest(record)) return null
    const session = record.continuationRunId
      ? await api.continueWorkflow(record.continuationRunId, record.request)
      : await api.createSoloSession(record.request)
    if (!ownsRequest(record)) return null
    verifyReceipt(record, session)
    recoveredSession.value = session
    recoveryChecked.value = true
    return acceptCreation(record, session)
  }

  function reportCreationError(cause: unknown) {
    if (isSoloLimitError(cause)) {
      limitOpen.value = true
      void store.refresh()
    } else {
      error.value = soloErrorText(cause, t, te, locale.value)
    }
  }

  /** Explicitly retry the original nonce and frozen payload, never the form. */
  async function retryPendingCreation(): Promise<SoloSession | null> {
    const record = pendingCreation.value
    if (!record || !ownsRequest(record) || creating.value || recovering.value) return null
    error.value = ''
    const session = await checkPendingCreation()
    if (!ownsRequest(record) || !recoveryChecked.value) return null
    const generation = ++launchGeneration
    creating.value = true
    try {
      return session ? await acceptCreation(record, session) : await postCreation(record)
    } catch (cause) {
      if (ownsRequest(record)) reportCreationError(cause)
      return null
    } finally {
      if (generation === launchGeneration) creating.value = false
    }
  }

  async function stopCreationRetry() {
    const record = pendingCreation.value
    if (!record || !ownsRequest(record) || creating.value || recovering.value) return
    try {
      await removeSoloOutbox(record.scope, record.id)
      if (!ownsRequest(record)) return
      pendingCreation.value = null
      recoveredSession.value = null
      recoveryChecked.value = false
      error.value = ''
    } catch { if (ownsRequest(record)) error.value = t('pages.solo.outboxUnavailable') }
  }

  /** Creates the session and opens it; the first message runs once the sandbox is up. */
  async function start(text: string, images: ClarifyImage[] = [], title = '', references: MessageReference[] = [], continuationRunId = ''): Promise<SoloSession | null> {
    if (!canStart.value) {
      if (!agent.value) error.value = t('pages.solo.home.noAgent')
      return null
    }
    const generation = ++launchGeneration
    const originalScope = authorizedScope.value
    creating.value = true
    error.value = ''
    try {
      const selected = modelChoices.value.find((choice) => choice.id === initialModelId.value)
      if (initialModelId.value && (!selected || selected.unavailable)) {
        error.value = t('pages.solo.modelSelect.unavailableSelection')
        return null
      }
      const effective = initialModelId.value ? selected : modelChoices.value[0]
      const listed = listedEfforts(effective?.efforts)
      if (reasoningEffort.value && !listed.includes(reasoningEffort.value)) {
        reasoningEffort.value = listed.at(-1) || ''
        error.value = t('pages.solo.modelSelect.effortResetNotice')
        return null
      }
      // The selector shows an empty effort as the highest level. Send that token
      // so creation does not inherit a lower model-group variant or the tool default.
      if (!reasoningEffort.value && listed.length) reasoningEffort.value = listed[listed.length - 1]!
      const id = newSoloRequestID()
      const request: SoloCreateBody = {
        clientRequestId: id,
        agentName: agentName.value,
        repos: soloCreateChoices(projectIds.value, branches.value),
        ...(initialModelId.value ? { initialModelId: parseModelRoute(initialModelId.value).modelId, ...(parseModelRoute(initialModelId.value).configurationId ? { initialConfigurationId: parseModelRoute(initialModelId.value).configurationId } : {}) } : {}),
        ...(reasoningEffort.value ? { reasoningEffort: reasoningEffort.value } : {}),
        title: title.trim() || undefined,
        text: text.trim() || undefined,
        images: images.length ? images : undefined,
        references: references.length ? references.map(({ kind, target }) => ({ kind, target })) : undefined,
      }
      const record: SoloCreationOutbox = JSON.parse(JSON.stringify({
        scope: authorizedScope.value, id, kind: 'creation', request, createdAt: new Date().toISOString(),
        ...(continuationRunId ? { continuationRunId } : {}),
      }))
      try { await saveSoloOutbox(record) } catch {
        error.value = t('pages.solo.outboxUnavailable')
        // Another tab or the other creation entry may have saved first.
        // Adopt its pending request instead of offering another new nonce.
        try {
          const saved = await readSoloOutbox(record.scope)
          if (ownsRequest(record)) {
            const existing = saved.find((value): value is SoloCreationOutbox => value.kind === 'creation')
            if (existing) {
              pendingCreation.value = existing
              recoveredSession.value = null
              recoveryChecked.value = false
              error.value = t('pages.solo.outboxRestored')
            }
          }
        } catch { /* The storage failure remains visible; no POST was sent. */ }
        return null
      }
      if (!ownsRequest(record)) return null
      pendingCreation.value = record
      return await postCreation(record)
    } catch (e) {
      if (generation === launchGeneration && originalScope === authorizedScope.value) reportCreationError(e)
      return null
    } finally {
      if (generation === launchGeneration) creating.value = false
    }
  }

  return {
    agents,
    projects,
    agentName,
    projectIds,
    branches,
    initialModelId,
    reasoningEffort,
    modelChoices,
    agent,
    loading,
    creating,
    error,
    limitOpen,
    pendingCreation,
    recoveredSession,
    recoveryChecked,
    recovering,
    checkPendingCreation,
    retryPendingCreation,
    stopCreationRetry,
    canStart,
    load,
    start,
    toggleProject,
    clearProjects,
    selectOnly,
    setPersistProjects,
    setProjectBranch,
    onProjectSaved,
  }
}
