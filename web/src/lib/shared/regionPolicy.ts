export type BackendId = 'cursor' | 'claude_code' | 'codebuddy' | 'trae' | 'opencode' | 'codex'
export type RegionSite = 'domestic' | 'international'
export type RegionMode = 'strict' | 'preserve-special'

export type RegionOption = {
  id: string
  site: RegionSite
  labelKey: string
  hintKey: string
}

export type RegionPolicy = {
  regionEnvKey: string
  defaultRegion: string
  options: readonly RegionOption[]
}

export const ACP_BACKENDS: { id: BackendId; label: string; configRoot: string }[] = [
  { id: 'cursor', label: 'Cursor', configRoot: '/root/.cursor' },
  { id: 'claude_code', label: 'Claude Code', configRoot: '/root/.claude' },
  { id: 'codebuddy', label: 'CodeBuddy', configRoot: '/root/.codebuddy' },
  { id: 'trae', label: 'Trae', configRoot: '/root/.trae' },
  { id: 'opencode', label: 'OpenCode', configRoot: '/root/.config/opencode' },
  { id: 'codex', label: 'Codex CLI (本机登录)', configRoot: '/root/.codex' },
]

const REGION_POLICIES: Partial<Record<BackendId, RegionPolicy>> = {
  codebuddy: {
    regionEnvKey: 'GRASP_CODEBUDDY_REGION',
    defaultRegion: 'public',
    options: [
      {
        id: 'internal',
        site: 'domestic',
        labelKey: 'pages.agentStudio.region.domestic',
        hintKey: 'pages.agentStudio.region.codebuddyDomesticHint',
      },
      {
        id: 'public',
        site: 'international',
        labelKey: 'pages.agentStudio.region.international',
        hintKey: 'pages.agentStudio.region.codebuddyInternationalHint',
      },
    ],
  },
  trae: {
    regionEnvKey: 'GRASP_TRAE_REGION',
    defaultRegion: 'intl',
    options: [
      {
        id: 'cn',
        site: 'domestic',
        labelKey: 'pages.agentStudio.region.domestic',
        hintKey: 'pages.agentStudio.region.traeDomesticHint',
      },
      {
        id: 'intl',
        site: 'international',
        labelKey: 'pages.agentStudio.region.international',
        hintKey: 'pages.agentStudio.region.traeInternationalHint',
      },
    ],
  },
}

const MANAGED_REGION_KEYS = new Set(
  Object.values(REGION_POLICIES).map((policy) => policy.regionEnvKey),
)

export function getRegionPolicy(backend: BackendId): RegionPolicy | undefined {
  return REGION_POLICIES[backend]
}

export function isManagedRegionKey(key: string): boolean {
  const k = key.trim()
  return MANAGED_REGION_KEYS.has(k)
}

export function setRegion(
  env: Record<string, string>,
  backend: BackendId,
  region: string,
): Record<string, string> {
  const policy = getRegionPolicy(backend)
  if (!policy || !policy.options.some((option) => option.id === region)) return { ...env }
  return { ...env, [policy.regionEnvKey]: region }
}

export function switchBackendRegions(
  env: Record<string, string>,
  backend: BackendId,
): Record<string, string> {
  const next = { ...env }
  for (const key of MANAGED_REGION_KEYS) delete next[key]
  const policy = getRegionPolicy(backend)
  if (policy) next[policy.regionEnvKey] = policy.defaultRegion
  return next
}

export type NormalizedRegions = {
  env: Record<string, string>
  region: string
  special: boolean
}

export function normalizeRegions(
  env: Record<string, string>,
  backend: BackendId,
  mode: RegionMode,
): NormalizedRegions {
  const next = { ...env }
  for (const key of MANAGED_REGION_KEYS) {
    if (key !== getRegionPolicy(backend)?.regionEnvKey) delete next[key]
  }

  const policy = getRegionPolicy(backend)
  if (!policy) {
    for (const key of MANAGED_REGION_KEYS) delete next[key]
    return { env: next, region: '', special: false }
  }

  const raw = (next[policy.regionEnvKey] || '').trim()
  if (policy.options.some((option) => option.id === raw)) {
    next[policy.regionEnvKey] = raw
    return { env: next, region: raw, special: false }
  }

  if (mode === 'preserve-special' && backend === 'codebuddy' && raw) {
    next[policy.regionEnvKey] = raw
    return { env: next, region: raw, special: true }
  }

  next[policy.regionEnvKey] = policy.defaultRegion
  return { env: next, region: policy.defaultRegion, special: false }
}

export type RegionSummary = {
  region: string
  site?: RegionSite
  labelKey?: string
  special: boolean
}

export function regionSummary(
  env: Record<string, string>,
  backend: BackendId,
  mode: RegionMode = 'strict',
): RegionSummary | undefined {
  const policy = getRegionPolicy(backend)
  if (!policy) return undefined
  const normalized = normalizeRegions(env, backend, mode)
  const option = policy.options.find((item) => item.id === normalized.region)
  return {
    region: normalized.region,
    site: option?.site,
    labelKey: option?.labelKey,
    special: normalized.special,
  }
}
