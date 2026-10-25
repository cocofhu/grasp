import type { CliCatalogModel, ConcreteModelBinding } from '@/lib/api/apiTypes'

export interface ConfiguredModelChoice {
  id: string
  name: string
  hint: string
  vendor: string
  /** Use the coding-tool mark, including the automatic sparkle. */
  backend: boolean
  unavailable: boolean
}

/**
 * Models a Solo start may offer. Empty id is the platform automatic primary.
 * The list follows configuration order and does not label primary or auxiliary.
 */
export function configuredModelChoices(input: {
  binding?: ConcreteModelBinding | null
  backend: string
  cliModels?: CliCatalogModel[]
  connectionModels?: { id: string; name?: string }[]
  providerId?: string
  catalogReady: boolean
}): ConfiguredModelChoice[] {
  const binding = input.binding
  if (!binding) return []
  const primary = binding.modelId?.trim() || ''
  const auxiliary = (binding.auxiliaryModelIds || []).map((id) => id.trim()).filter(Boolean)
  const ids = binding.kind === 'platform' ? [primary, ...auxiliary] : [primary, ...auxiliary].filter((id, index) => id || index > 0)
  const seen = new Set<string>()
  const choices: ConfiguredModelChoice[] = []
  for (const id of ids) {
    if (seen.has(id)) continue
    seen.add(id)
    if (!id) {
      choices.push({ id: '', name: '自动', hint: '交给编码工具选择', vendor: input.backend, backend: true, unavailable: false })
      continue
    }
    const cli = (input.cliModels || []).find((model) => model.id === id)
    const connection = (input.connectionModels || []).find((model) => model.id === id)
    const name = cli?.name || connection?.name || id
    const vendor = cli?.vendor || input.providerId || ''
    const known = !!cli || !!connection
    choices.push({
      id,
      name,
      hint: id,
      vendor,
      backend: false,
      unavailable: input.catalogReady && !known,
    })
  }
  return choices
}
