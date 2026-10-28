import { computed, ref, watch, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { cliCatalogFailed, loadCliModels } from '@/lib/agent/cliCatalog'
import type { Agent, CliCatalogModel, CredentialItem, ModelConfiguration, ModelConnection } from '@/lib/api/apiTypes'
import { boundConfigurationIds, effectiveModelAgent, parseModelRoute } from '@/lib/agent/modelBindings'
import { ensureModelCatalogEvents, modelCatalogRevision } from '@/lib/api/modelCatalogEvents'
import { boundModelChoices } from './configuredModels'

/** One reactive directory for every group bound to the session's Agent. */
export function useSessionModelCatalog(agentName: Ref<string>) {
  const agents = ref<Agent[]>([])
  const configurations = ref<ModelConfiguration[]>([])
  const connections = ref<ModelConnection[]>([])
  const credentials = ref<CredentialItem[]>()
  const cliModels = ref<Record<string, CliCatalogModel[]>>({})
  const cliReady = ref(false)
  const cliFailed = ref(false)
  const listState = ref<'idle' | 'loading' | 'ok' | 'error'>('idle')
  const connectionLoaded = ref(false)
  let loadGeneration = 0
  let cliGeneration = 0
  const agent = computed(() => agents.value.find(item => item.name === agentName.value) ?? null)
  const bound = computed(() => boundConfigurationIds(agent.value?.modelBinding))
  const active = computed(() => bound.value.map(id => configurations.value.find(row => row.id === id && row.state === 'active')).filter((row): row is ModelConfiguration => !!row))
  const hasConnections = computed(() => active.value.some(row => row.binding.kind === 'connection'))
  const hasPlatform = computed(() => active.value.some(row => row.binding.kind === 'platform'))
  const catalogConfirmed = computed(() => listState.value === 'ok' && (!hasConnections.value || connectionLoaded.value) && (!hasPlatform.value || cliReady.value && !cliFailed.value))
  const choices = computed(() => boundModelChoices({
    binding: agent.value?.modelBinding, configurations: configurations.value, connections: connections.value,
    cliModels: cliModels.value, cliReady: cliReady.value && !cliFailed.value, connectionReady: connectionLoaded.value, credentials: credentials.value,
  }))
  const ready = computed(() => catalogConfirmed.value)
  const configLoaded = computed(() => listState.value === 'ok')
  const blocked = computed(() => !!agentName.value && (listState.value === 'error' || listState.value === 'ok' && (hasConnections.value && !connectionLoaded.value || hasPlatform.value && cliFailed.value)))

  watch(() => active.value.map(row => row.backend).join('|'), async (backends) => {
    const generation = ++cliGeneration
    cliReady.value = false
    cliFailed.value = false
    const rows = await Promise.all([...new Set(backends.split('|').filter(Boolean))].map(async backend => [backend, await loadCliModels(backend)] as const))
    if (generation !== cliGeneration) return
    cliModels.value = Object.fromEntries(rows)
    cliFailed.value = rows.some(([backend]) => cliCatalogFailed(backend))
    cliReady.value = true
  })

  async function load() {
    const generation = ++loadGeneration
    listState.value = 'loading'
    try {
      const [listed, configs, services, keys] = await Promise.all([
        api.listAgents(), api.listModelConfigurations(),
        api.listModelConnections().then(value => ({ ok: true, items: value.items })).catch(() => ({ ok: false, items: [] as ModelConnection[] })),
        Promise.resolve().then(() => api.listCredentials?.()).then(value => value?.items).catch(() => undefined),
      ])
      const defaults = listed.some(agent => agent.inheritModel) ? await api.getSharedAgentConfig() : undefined
      if (generation !== loadGeneration) return
      agents.value = listed.map(agent => effectiveModelAgent(agent, defaults))
      configurations.value = configs.items
      if (services.ok) connections.value = services.items
      if (keys !== undefined) credentials.value = keys
      connectionLoaded.value = services.ok
      listState.value = 'ok'
    } catch {
      if (generation === loadGeneration) listState.value = 'error'
      // A failed read preserves the last known directory and the selected route.
    }
  }
  ensureModelCatalogEvents()
  watch(modelCatalogRevision, () => void load())
  watch(agentName, () => void load(), { immediate: true })

  /** Null means the server directory has not safely confirmed membership. */
  function modelInConfiguration(id: string, configurationId?: string): boolean | null {
    if (listState.value !== 'ok') return null
    const route = parseModelRoute(id)
    const groupId = configurationId || route.configurationId || agent.value?.modelBinding?.configurationId
    if (!groupId || !bound.value.includes(groupId)) return false
    const configuration = active.value.find(row => row.id === groupId)
    if (!configuration) return false
    const primary = configuration.binding.modelId?.trim() || ''
    return !route.modelId || route.modelId === primary || (configuration.binding.auxiliaryModelIds || []).includes(route.modelId)
  }
  return { choices, ready, blocked, configLoaded, catalogConfirmed, modelInConfiguration, defaultConfigurationId: computed(() => agent.value?.modelBinding?.configurationId || '') }
}
