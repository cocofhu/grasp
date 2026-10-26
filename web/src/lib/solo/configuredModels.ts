import type { CliCatalogModel, ConcreteModelBinding } from '@/lib/api/apiTypes'
import { modelVendor } from '@/lib/agent/cliModelView'

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
    const cli = (input.cliModels || []).find((model) => model.id === id)
    const connection = (input.connectionModels || []).find((model) => model.id === id)
    const name = cli?.name || connection?.name || id
    const vendor = modelVendor(id, cli?.name || connection?.name, cli?.vendor || input.providerId || '')
    const known = !!cli || !!connection
    const supportsEffort = input.backend === 'codex' || input.backend === 'deepseek_harness'
    const declaredEfforts = connection?.reasoningEfforts?.length ? connection.reasoningEfforts : input.backend === 'codex' ? cli?.efforts : undefined
    const efforts = supportsEffort && connection?.capabilities?.reasoning !== false ? [...new Set((declaredEfforts || []).map(value => value.trim()).filter(Boolean))] : []
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
