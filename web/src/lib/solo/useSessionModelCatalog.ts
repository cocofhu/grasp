import { computed, ref, watch, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import type { Agent, CliCatalogModel, ModelConfiguration, ModelConnection } from '@/lib/api/apiTypes'
import { configuredModelChoices } from './configuredModels'

/**
 * Model list for one Solo session. Same source as the start-of-session picker:
 * the Agent's current configuration, primary then auxiliaries.
 */
export function useSessionModelCatalog(agentName: Ref<string>) {
  const agents = ref<Agent[]>([])
  const configurations = ref<ModelConfiguration[]>([])
  const connections = ref<ModelConnection[]>([])
  const cliModels = ref<CliCatalogModel[]>([])
  const cliReady = ref(false)
  const loaded = ref(false)

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
  const choices = computed(() => configuredModelChoices({
    binding: configuration.value?.binding,
    backend: configuration.value?.backend || agent.value?.acpBackend || '',
    cliModels: cliModels.value,
    connectionModels: connection.value?.models,
    providerId: connection.value?.providerId,
    catalogReady: configuration.value?.binding.kind === 'connection' ? !!connection.value : cliReady.value,
  }))
  // True once the Agent list and, when a configuration exists, its catalog are in.
  const ready = computed(() => {
    if (!loaded.value) return false
    if (!configuration.value) return true
    if (configuration.value.binding.kind === 'connection') return true
    return cliReady.value
  })

  watch(() => agent.value?.acpBackend || configuration.value?.backend || '', async (backend) => {
    cliReady.value = false
    cliModels.value = backend ? await loadCliModels(backend) : []
    cliReady.value = true
  })

  async function load() {
    loaded.value = false
    try {
      const [listed, configs, services] = await Promise.all([
        api.listAgents().catch(() => [] as Agent[]),
        api.listModelConfigurations().catch(() => ({ items: [] as ModelConfiguration[] })),
        api.listModelConnections().catch(() => ({ items: [] as ModelConnection[] })),
      ])
      agents.value = listed
      configurations.value = configs.items
      connections.value = services.items
    } finally {
      loaded.value = true
    }
  }

  watch(agentName, () => void load(), { immediate: true })

  return { choices, ready }
}
