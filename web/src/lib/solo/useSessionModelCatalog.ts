import { computed, ref, watch, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { cliCatalogFailed, loadCliModels } from '@/lib/agent/cliCatalog'
import type { Agent, CliCatalogModel, ModelConfiguration, ModelConnection } from '@/lib/api/apiTypes'
import { configuredModelChoices } from './configuredModels'

/**
 * Model list for one Solo session. Same source as the start-of-session picker:
 * the Agent's current configuration, primary then auxiliaries.
 *
 * A failed directory request is not evidence that the saved model disappeared.
 * The page may fall back to the primary model only after the Agent configuration
 * has loaded and the model is absent from it, or after the directory itself
 * loaded and confirmed the model is missing.
 */
export function useSessionModelCatalog(agentName: Ref<string>) {
  const agents = ref<Agent[]>([])
  const configurations = ref<ModelConfiguration[]>([])
  const connections = ref<ModelConnection[]>([])
  const cliModels = ref<CliCatalogModel[]>([])
  const cliReady = ref(false)
  const cliFailed = ref(false)
  const listState = ref<'idle' | 'loading' | 'ok' | 'error'>('idle')
  const connectionLoaded = ref(false)

  const agent = computed(() => agents.value.find((item) => item.name === agentName.value) ?? null)
  const configuration = computed(() => {
    const binding = agent.value?.modelBinding
    if (binding?.kind !== 'configuration') return null
    return configurations.value.find((item) => item.id === binding.configurationId && item.state === 'active') ?? null
  })
  const connection = computed(() => {
    const binding = configuration.value?.binding
    return binding?.kind === 'connection' ? connections.value.find((item) => item.id === binding.connectionId) ?? null : null
  })
  const catalogConfirmed = computed(() => {
    if (listState.value !== 'ok' || !configuration.value) return false
    if (configuration.value.binding.kind === 'connection') return connectionLoaded.value && !!connection.value
    return cliReady.value && !cliFailed.value
  })
  const choices = computed(() => configuredModelChoices({
    binding: configuration.value?.binding,
    backend: configuration.value?.backend || agent.value?.acpBackend || '',
    cliModels: cliModels.value,
    connectionModels: connection.value?.models,
    providerId: connection.value?.providerId,
    catalogReady: catalogConfirmed.value,
  }))
  // True when the selector can offer a list that is safe to judge. A failed
  // read stays false so the page does not treat an empty list as "no models".
  const ready = computed(() => {
    if (listState.value !== 'ok') return false
    if (!configuration.value) return true
    return catalogConfirmed.value
  })
  const configLoaded = computed(() => listState.value === 'ok')
  // The directory request failed. Keep the saved model and tell the user.
  const blocked = computed(() => {
    if (!agentName.value) return false
    if (listState.value === 'error') return true
    if (listState.value !== 'ok' || !configuration.value) return false
    if (configuration.value.binding.kind === 'connection') return !connectionLoaded.value
    return cliFailed.value
  })

  watch(() => agent.value?.acpBackend || configuration.value?.backend || '', async (backend) => {
    cliReady.value = false
    cliFailed.value = false
    cliModels.value = backend ? await loadCliModels(backend) : []
    cliFailed.value = backend ? cliCatalogFailed(backend) : false
    cliReady.value = true
  })

  async function load() {
    listState.value = 'loading'
    connectionLoaded.value = false
    let agentsOk = false
    let configsOk = false
    let connectionsOk = false
    try {
      const [listed, configs, services] = await Promise.all([
        api.listAgents().then((value) => {
          agentsOk = true
          return value
        }).catch(() => [] as Agent[]),
        api.listModelConfigurations().then((value) => {
          configsOk = true
          return value
        }).catch(() => ({ items: [] as ModelConfiguration[] })),
        api.listModelConnections().then((value) => {
          connectionsOk = true
          return value
        }).catch(() => ({ items: [] as ModelConnection[] })),
      ])
      if (!agentsOk || !configsOk) {
        agents.value = []
        configurations.value = []
        connections.value = []
        listState.value = 'error'
        return
      }
      agents.value = listed
      configurations.value = configs.items
      connections.value = connectionsOk ? services.items : []
      connectionLoaded.value = connectionsOk
      listState.value = 'ok'
    } finally {
      if (listState.value === 'loading') listState.value = 'error'
    }
  }

  watch(agentName, () => void load(), { immediate: true })

  /** null while the configuration has not loaded; false when the id is outside it. */
  function modelInConfiguration(id: string): boolean | null {
    if (listState.value !== 'ok') return null
    const binding = configuration.value?.binding
    if (!binding) return false
    const primary = binding.modelId?.trim() || ''
    const auxiliary = (binding.auxiliaryModelIds || []).map((item) => item.trim()).filter(Boolean)
    return id === primary || auxiliary.includes(id)
  }

  return { choices, ready, blocked, configLoaded, catalogConfirmed, modelInConfiguration }
}
