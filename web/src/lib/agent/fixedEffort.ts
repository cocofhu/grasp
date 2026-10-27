import catalog from './opencodeVariants.json'

export type OpenCodeVariantModel = { spell: string; variants: string[] }

export const openCodeVariantCatalog = catalog as Record<string, OpenCodeVariantModel>

const LABELS: Record<string, { zh: string; en: string }> = {
  none: { zh: '无', en: 'None' },
  minimal: { zh: '最小', en: 'Minimal' },
  low: { zh: '低', en: 'Low' },
  medium: { zh: '中', en: 'Medium' },
  high: { zh: '高', en: 'High' },
  xhigh: { zh: '超高', en: 'Extra high' },
  max: { zh: '最高', en: 'Max' },
  thinking: { zh: 'thinking', en: 'thinking' },
}

// Verified TokenHub levels. They are not copied from the official catalog:
// deepseek-v4-pro is low/high/max here and high/max on OpenCode's own key.
const TOKEN_HUB_EFFORTS: Record<string, readonly string[]> = {
  'deepseek-v4.1-flash': ['low', 'high', 'max'],
  'deepseek-v4.1-flash-free': ['low', 'high', 'max'],
  'deepseek-v4-flash': ['low', 'high', 'max'],
  'deepseek-v4': ['low', 'high', 'max'],
  'deepseek-v4-pro': ['low', 'high', 'max'],
}

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

function tokenHubHost(baseUrl?: string) {
  const raw = (baseUrl || '').trim()
  if (!raw) return false
  try {
    const host = new URL(/^[a-z][a-z0-9+.-]*:/i.test(raw) ? raw : `https://${raw}`).hostname.toLowerCase()
    return host === 'tokenhub.tencentmaas.com' || host.endsWith('.tokenhub.tencentmaas.com')
  } catch {
    return false
  }
}

export function tokenHubService(providerId?: string, baseUrl?: string) {
  const id = (providerId || '').trim().toLowerCase()
  if (id === 'tokenhub' || id === 'hunyuan') return true
  return tokenHubHost(baseUrl)
}

export function tokenHubVariants(modelId: string): string[] {
  const id = modelId.trim().toLowerCase()
  const direct = TOKEN_HUB_EFFORTS[id]
  if (direct) return [...direct]
  const slash = id.lastIndexOf('/')
  if (slash >= 0) {
    const tail = TOKEN_HUB_EFFORTS[id.slice(slash + 1)]
    if (tail) return [...tail]
  }
  return []
}

export function effortChoices(backend: string, kind: string, providerId: string | undefined, baseUrl: string | undefined, modelId: string): string[] {
  if (backend !== 'opencode' || !modelId.trim()) return []
  if (kind === 'platform') return platformVariants(modelId)
  if (kind === 'connection' && tokenHubService(providerId, baseUrl)) return tokenHubVariants(modelId)
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
