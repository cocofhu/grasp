import type { CliCatalogModel, ConcreteModelBinding, CredentialItem, ModelBinding, ModelConfiguration, ModelConnection } from '@/lib/api/apiTypes'
import { modelVendor } from '@/lib/agent/cliModelView'
import { effortChoices, validEffortToken } from '@/lib/agent/fixedEffort'
import { credentialUsable } from '@/lib/agent/modelRouting'
import { boundConfigurationIds, modelRouteValue } from '@/lib/agent/modelBindings'

export interface ConfiguredModelChoice {
  id: string
  name: string
  hint: string
  vendor: string
  /** Use the coding-tool mark, including the automatic sparkle. */
  backend: boolean
  unavailable: boolean
  /** Exact levels accepted by this model and its coding-tool adapter. */
  efforts?: string[]
  configurationId?: string
  modelId?: string
  configurationName?: string
  tool?: string
  source?: string
}

/** Expand every bound group without merging same-named models from different accounts. */
export function boundModelChoices(input: {
  binding?: ModelBinding
  configurations: ModelConfiguration[]
  connections: ModelConnection[]
  cliModels: Record<string, CliCatalogModel[]>
  cliReady: boolean
  connectionReady?: boolean
  credentials?: CredentialItem[]
}): ConfiguredModelChoice[] {
  const ids = boundConfigurationIds(input.binding, input.configurations)
  const grouped = !!input.binding?.allConfigurations || ids.length > 1
  return ids.flatMap(id => {
    const configuration = input.configurations.find(row => row.id === id && row.state === 'active')
    if (!configuration) return []
    const binding = configuration.binding
    const connection = binding.kind === 'connection' ? input.connections.find(row => row.id === binding.connectionId && row.state === 'active') : undefined
    const credentialId = binding.kind === 'platform' ? binding.credentialId : connection?.credentialId
    const credentialUnavailable = input.credentials !== undefined && !input.credentials.some(credential => credential.id === credentialId && credentialUsable(credential))
    return configuredModelChoices({
      binding, configurationName: configuration.name, backend: configuration.backend,
      cliModels: input.cliModels[configuration.backend] || [], connectionModels: connection?.models,
      providerId: connection?.providerId, baseUrl: connection?.baseUrl,
      catalogReady: binding.kind === 'connection' ? !!connection && input.connectionReady !== false : input.cliReady,
    }).map(choice => ({
      ...choice,
      id: grouped ? modelRouteValue(id, choice.id) : choice.id,
      configurationId: id, modelId: choice.id, configurationName: configuration.name,
      tool: configuration.backend, source: connection?.name,
      unavailable: choice.unavailable || credentialUnavailable || (binding.kind === 'connection' && input.connectionReady !== false && !connection),
    }))
  })
}

/**
 * Models a Solo start may offer. Empty id is the platform automatic primary.
 * The list follows configuration order and does not label primary or auxiliary.
 */
export function configuredModelChoices(input: {
  binding?: ConcreteModelBinding | null
  /** 自动平台主模型可使用配置名称；具体模型始终显示真实模型名称。 */
  configurationName?: string
  backend: string
  cliModels?: CliCatalogModel[]
  connectionModels?: { id: string; name?: string; reasoningEfforts?: string[]; capabilities?: { reasoning?: boolean } }[]
  providerId?: string
  baseUrl?: string
  catalogReady: boolean
}): ConfiguredModelChoice[] {
  const binding = input.binding
  if (!binding) return []
  const primary = binding.modelId?.trim() || ''
  const configurationName = input.configurationName?.trim() || ''
  const auxiliary = (binding.auxiliaryModelIds || []).map((id) => id.trim()).filter(Boolean)
  const ids = binding.kind === 'platform' ? [primary, ...auxiliary] : [primary, ...auxiliary].filter((id, index) => id || index > 0)
  const seen = new Set<string>()
  const choices: ConfiguredModelChoice[] = []
  for (const id of ids) {
    if (seen.has(id)) continue
    seen.add(id)
    if (!id) {
      choices.push({ id: '', name: configurationName || '自动', hint: '交给编码工具选择', vendor: input.backend, backend: true, unavailable: false })
      continue
    }
    const cli = (input.cliModels || []).find((model) => model.id === id || model.aliases?.includes(id))
    const connection = (input.connectionModels || []).find((model) => model.id === id)
    const name = cli?.name || connection?.name || id
    const vendor = modelVendor(id, cli?.name || connection?.name, cli?.vendor || input.providerId || '')
    const known = !!cli || !!connection
    const supportsEffort = input.backend === 'codex' || input.backend === 'deepseek_harness' || input.backend === 'kimi_code'
    const declaredEfforts = input.backend === 'kimi_code' ? cli?.efforts : connection?.reasoningEfforts?.length ? connection.reasoningEfforts : input.backend === 'codex' ? cli?.efforts : undefined
    const adapterEfforts = supportsEffort && (input.backend === 'kimi_code' || connection?.capabilities?.reasoning !== false) ? [...new Set((declaredEfforts || []).map(value => value.trim()).filter(Boolean))] : []
    const catalogEfforts = input.backend === 'opencode' ? effortChoices(input.backend, binding.kind, input.providerId, input.baseUrl, id) : []
    const savedEffort = binding.toolSettings?.modelEfforts?.[id]?.trim() || ''
    const efforts = input.backend === 'opencode' && validEffortToken(savedEffort) && !catalogEfforts.includes(savedEffort) ? [] : input.backend === 'opencode' ? catalogEfforts : adapterEfforts
    choices.push({
      id,
      name,
      hint: id,
      vendor: vendor || input.backend,
      backend: !vendor,
      unavailable: input.catalogReady && !known,
      efforts,
    })
  }
  return choices
}
