import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api/api'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import type { Agent, CliCatalogModel, CredentialItem, ModelConfiguration, ModelConnection, Project, SoloSession } from '@/lib/api/apiTypes'
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
  // Deep-link dialogs turn this off so a preselect does not overwrite the home page's ids.
  let persistProjects = true
  let hasLoaded = false
  let loadGeneration = 0

  const agent = computed(() => agents.value.find((a) => a.name === agentName.value) ?? null)
  const modelChoices = computed(() => boundModelChoices({
    binding: agent.value?.modelBinding, configurations: configurations.value, connections: connections.value,
    cliModels: cliModels.value, cliReady: cliReady.value, credentials: credentials.value,
  }))
  const canStart = computed(() => !!agent.value && !creating.value)

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
  watch(() => boundConfigurationIds(agent.value?.modelBinding).map(id => configurations.value.find(row => row.id === id)?.backend || '').join('|'), async (backends) => {
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
      if (generation !== loadGeneration) return
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

  /** Creates the session and opens it; the first message runs once the sandbox is up. */
  async function start(text: string, images: ClarifyImage[] = [], title = ''): Promise<SoloSession | null> {
    if (!canStart.value) {
      if (!agent.value) error.value = t('pages.solo.home.noAgent')
      return null
    }
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
      const s = await api.createSoloSession({
        agentName: agentName.value,
        repos: soloCreateChoices(projectIds.value, branches.value),
        ...(initialModelId.value ? { initialModelId: parseModelRoute(initialModelId.value).modelId, ...(parseModelRoute(initialModelId.value).configurationId ? { initialConfigurationId: parseModelRoute(initialModelId.value).configurationId } : {}) } : {}),
        ...(reasoningEffort.value ? { reasoningEffort: reasoningEffort.value } : {}),
        title: title.trim() || undefined,
        text: text.trim() || undefined,
        images: images.length ? images : undefined,
      })
      soloPrefs.setLastProjects(projectIds.value)
      store.upsert(s)
      void router.push(`/solo/${s.id}`)
      return s
    } catch (e) {
      if (isSoloLimitError(e)) {
        limitOpen.value = true
        void store.refresh()
      } else {
        error.value = soloErrorText(e, t, te, locale.value)
      }
      return null
    } finally {
      creating.value = false
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
