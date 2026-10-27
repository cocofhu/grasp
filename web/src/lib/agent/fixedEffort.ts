import catalog from './opencodeVariants.json'
import providerCatalog from '../../../../server/internal/modelrouting/provider_efforts.json'

export type OpenCodeVariantModel = { spell: string; variants: string[] }

export const openCodeVariantCatalog = catalog as Record<string, OpenCodeVariantModel>

type ProviderEffortEntry = { ids?: string[]; hosts?: string[]; suffixes?: string[]; models: Record<string, string[]> }
const providerEfforts = (providerCatalog as { providers: ProviderEffortEntry[] }).providers

const LABELS: Record<string, { zh: string; en: string }> = {
  none: { zh: '无', en: 'None' },
  minimal: { zh: '最小', en: 'Minimal' },
  low: { zh: '低', en: 'Low' },
  medium: { zh: '中', en: 'Medium' },
  high: { zh: '高', en: 'High' },
  xhigh: { zh: '超高', en: 'Extra high' },
  max: { zh: '最高', en: 'Max' },
  ultra: { zh: '极致', en: 'Ultra' },
  thinking: { zh: 'thinking', en: 'thinking' },
}

const EFFORT_DATE = /-(?:\d{8}|\d{6}|\d{4})$/

export function openCodeModelID(modelId: string) {
  const id = modelId.trim()
  const bare = id.startsWith('opencode/') ? id.slice('opencode/'.length) : id
  const hash = bare.lastIndexOf('#')
  return hash >= 0 ? bare.slice(0, hash) : bare
}

export function platformVariants(modelId: string): string[] {
  const entry = openCodeVariantCatalog[openCodeModelID(modelId)]
  return entry?.variants?.length ? [...entry.variants] : []
}

function connectionHost(baseUrl?: string) {
  const raw = (baseUrl || '').trim()
  if (!raw) return ''
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:/i.test(raw) ? raw : `https://${raw}`).hostname.toLowerCase()
  } catch {
    return ''
  }
}

function providerEntry(providerId?: string, baseUrl?: string) {
  const id = (providerId || '').trim().toLowerCase()
  if (id) {
    const byId = providerEfforts.find((entry) => entry.ids?.some((candidate) => candidate === id))
    if (byId) return byId
  }
  const host = connectionHost(baseUrl)
  if (!host) return undefined
  return providerEfforts.find((entry) => entry.hosts?.includes(host) || entry.suffixes?.some((suffix) => suffix && host.endsWith(suffix)))
}

function lookupProviderEfforts(models: Record<string, string[]>, modelId: string) {
  const id = modelId.trim().toLowerCase()
  if (!id) return []
  const keys = [id]
  const slash = id.lastIndexOf('/')
  if (slash >= 0) keys.push(id.slice(slash + 1))
  for (const key of keys) {
    const direct = models[key]
    if (direct) return [...direct]
  }
  for (const key of keys) {
    const stripped = key.replace(EFFORT_DATE, '')
    if (stripped !== key && models[stripped]) return [...models[stripped]]
  }
  return []
}

export function tokenHubVariants(modelId: string): string[] {
  return lookupProviderEfforts(providerEntry('tokenhub')?.models || {}, modelId)
}

export function effortChoices(backend: string, kind: string, providerId: string | undefined, baseUrl: string | undefined, modelId: string): string[] {
  if (backend !== 'opencode' || !modelId.trim()) return []
  if (kind === 'platform') return platformVariants(modelId)
  if (kind === 'connection') return lookupProviderEfforts(providerEntry(providerId, baseUrl)?.models || {}, modelId)
  return []
}

export function effortLabel(value: string, locale: string) {
  const label = LABELS[value]
  if (!label) return value
  return locale.toLowerCase().startsWith('zh') ? label.zh : label.en
}

export function validEffortToken(value: string) {
  return /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/.test(value)
}
