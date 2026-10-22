import type { BackendId } from '@/lib/shared/regionPolicy'
import { openCodeCatalogKnowsProvider } from '@/lib/agent/openCodeCatalog'

export const OPENCODE_PROVIDER_ENV = 'GRASP_OPENCODE_PROVIDER'
export const OPENCODE_BASE_URL_ENV = 'GRASP_OPENCODE_BASE_URL'
export const OPENCODE_MODEL_VISION_ENV = 'GRASP_OPENCODE_MODEL_VISION'
export const OPENCODE_MODEL_ENV = 'ACP_BRIDGE_MODEL'
export const DEFAULT_OPENCODE_PROVIDER = 'openai'

/**
 * A vendor id from the OpenCode catalog (models.dev), or `custom` for a gateway
 * absent from it. The catalog holds hundreds and grows without us, so ids are not
 * enumerated here — the picker reads them from the server.
 */
export type OpenCodeProviderId = string

/** The id `custom` is ours, not the catalog's: it carries a base URL and model. */
export const OPENCODE_CUSTOM_PROVIDER = 'custom'

/**
 * Vendors offered when the catalog cannot be reached, so an offline install still
 * has a usable list. Labels are translated; models are never listed here, because
 * a hand-written model list rots into ids OpenCode rejects.
 */
export const OPENCODE_FALLBACK_PROVIDERS: { id: OpenCodeProviderId; labelKey: string }[] = [
  { id: 'aimlapi', labelKey: 'pages.agentStudio.openCode.providers.aimlapi' },
  { id: 'openai', labelKey: 'pages.agentStudio.openCode.providers.openai' },
  { id: 'anthropic', labelKey: 'pages.agentStudio.openCode.providers.anthropic' },
  { id: 'google', labelKey: 'pages.agentStudio.openCode.providers.google' },
  { id: 'openrouter', labelKey: 'pages.agentStudio.openCode.providers.openrouter' },
  { id: 'deepseek', labelKey: 'pages.agentStudio.openCode.providers.deepseek' },
  { id: 'moonshotai', labelKey: 'pages.agentStudio.openCode.providers.moonshotai' },
  { id: 'alibaba', labelKey: 'pages.agentStudio.openCode.providers.alibaba' },
  { id: 'xai', labelKey: 'pages.agentStudio.openCode.providers.xai' },
  { id: OPENCODE_CUSTOM_PROVIDER, labelKey: 'pages.agentStudio.openCode.providers.custom' },
]

/** Mirrors the server: a catalog-shaped id survives, anything else is custom. */
const PROVIDER_ID_SHAPE = /^[a-z0-9][a-z0-9._-]*$/

export function normalizeOpenCodeProvider(raw: string): OpenCodeProviderId {
  const v = raw.trim().toLowerCase()
  if (!v) return DEFAULT_OPENCODE_PROVIDER
  if (!PROVIDER_ID_SHAPE.test(v)) return OPENCODE_CUSTOM_PROVIDER
  return v
}

/**
 * The stored `provider/model` value, with the vendor prefixed when the typed id
 * does not already carry it.
 *
 * A vendor's own id may contain slashes (`anthropic/claude-sonnet-4-5` on
 * OpenRouter, `deepseek/deepseek-flash` on a TokenHub-style endpoint), so the
 * prefix is what tells OpenCode where the id ends up — it is never the id's own
 * first segment. Prefixing is idempotent, which is what makes a picked value and
 * a hand-typed one land on the same string.
 */
export function openCodeModelWithProvider(model: string, provider: string): string {
  const m = model.trim()
  if (!m) return ''
  const id = normalizeOpenCodeProvider(provider)
  return m.startsWith(`${id}/`) ? m : `${id}/${m}`
}

/** The vendor's own id, with the `provider/` prefix dropped. */
export function openCodeModelID(model: string, provider: string): string {
  const m = model.trim()
  const prefix = `${normalizeOpenCodeProvider(provider)}/`
  if (m.startsWith(prefix) && m.length > prefix.length) return m.slice(prefix.length)
  return m
}

/**
 * Whether the model was picked from this vendor's catalog listing, which is what
 * makes it expire when the vendor changes. A hand-typed id is kept across the
 * switch: only the endpoint knows whether it serves that id, and retyping a
 * gateway's id on every switch is the more common annoyance.
 */
export function openCodeModelFromCatalog(
  model: string,
  provider: string,
  models: { id: string }[],
): boolean {
  const id = openCodeModelID(model, provider)
  if (!id) return false
  return models.some((m) => m.id === id)
}

export type OpenCodeFields = {
  provider: OpenCodeProviderId
  baseURL: string
  model: string
  vision: boolean
}

export function openCodeEnvEnabled(value: string | undefined): boolean {
  switch ((value || '').trim().toLowerCase()) {
    case '1':
    case 'true':
    case 'yes':
    case 'on':
      return true
    default:
      return false
  }
}

export function openCodeFieldsFromEnv(env: Record<string, string>): OpenCodeFields {
  return {
    provider: normalizeOpenCodeProvider(env[OPENCODE_PROVIDER_ENV] || ''),
    baseURL: (env[OPENCODE_BASE_URL_ENV] || '').trim(),
    model: (env[OPENCODE_MODEL_ENV] || '').trim(),
    vision: openCodeEnvEnabled(env[OPENCODE_MODEL_VISION_ENV]),
  }
}

export function applyOpenCodeFields(
  env: Record<string, string>,
  fields: Partial<OpenCodeFields>,
): Record<string, string> {
  const current = openCodeFieldsFromEnv(env)
  const next: OpenCodeFields = {
    provider: fields.provider ?? current.provider,
    baseURL: fields.baseURL !== undefined ? fields.baseURL.trim() : current.baseURL,
    model: fields.model !== undefined ? fields.model.trim() : current.model,
    vision: fields.vision ?? current.vision,
  }
  // Stored as `provider/model`, so a hand-typed bare id gains its vendor here.
  next.model = openCodeModelWithProvider(next.model, next.provider)
  const out = { ...env }
  out[OPENCODE_PROVIDER_ENV] = next.provider
  if (next.baseURL) out[OPENCODE_BASE_URL_ENV] = next.baseURL
  else delete out[OPENCODE_BASE_URL_ENV]
  if (next.model) out[OPENCODE_MODEL_ENV] = next.model
  else delete out[OPENCODE_MODEL_ENV]
  if (next.vision) out[OPENCODE_MODEL_VISION_ENV] = '1'
  else delete out[OPENCODE_MODEL_VISION_ENV]
  return out
}

export function switchOpenCodeEnv(
  env: Record<string, string>,
  backend: BackendId,
): Record<string, string> {
  const out = { ...env }
  delete out[OPENCODE_PROVIDER_ENV]
  delete out[OPENCODE_BASE_URL_ENV]
  delete out[OPENCODE_MODEL_VISION_ENV]
  if (backend === 'opencode') {
    out[OPENCODE_PROVIDER_ENV] = DEFAULT_OPENCODE_PROVIDER
  }
  return out
}

/**
 * Vendors the server knows the endpoint of, so an empty API Base is filled in
 * rather than required — even when the catalog has never heard of them.
 */
export const OPENCODE_KNOWN_ENDPOINTS: Record<string, string> = {
  aimlapi: 'https://api.aimlapi.com/v1',
}

export function openCodeCustomBaseRequired(provider: string, baseURL: string): boolean {
  if (baseURL.trim()) return false
  const id = normalizeOpenCodeProvider(provider)
  if (id === 'custom') return true
  if (id in OPENCODE_KNOWN_ENDPOINTS) return false
  // A readable catalog that does not list the typed id means the server must
  // declare it as an OpenAI-compatible endpoint, which has no default URL.
  return openCodeCatalogKnowsProvider(id) === false
}

export function openCodeModelRequired(model: string): boolean {
  return !model.trim()
}
