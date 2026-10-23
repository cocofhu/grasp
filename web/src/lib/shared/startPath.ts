import { ACP_BACKENDS, type BackendId } from '@/lib/shared/regionPolicy'

/**
 * Two ways to start an Agent: bring a model-vendor API key (BYOK, served by
 * OpenCode), or use a coding tool (Cursor / Claude Code / CodeBuddy / Trae /
 * Codex / DeepSeek Harness). Shared by onboarding, create-Agent, and team wizards.
 */
export type StartPath = 'apiKey' | 'cli'

/** Default API-key harness; existing setups continue to use OpenCode. */
export const APIKEY_BACKEND: BackendId = 'opencode'

export const CLI_BACKEND_DEFAULT: BackendId = 'cursor'

export const APIKEY_BACKENDS = ACP_BACKENDS.filter((b) => b.id === APIKEY_BACKEND)

/** Native coding tools, including tools authenticated with their own API key. */
export const CLI_BACKENDS = ACP_BACKENDS.filter((b) => !APIKEY_BACKENDS.some((item) => item.id === b.id))

export function startPathForBackend(backend: BackendId): StartPath {
  return APIKEY_BACKENDS.some((b) => b.id === backend) ? 'apiKey' : 'cli'
}

/** Resolve the Backend that a path switch should land on. */
export function backendForStartPath(path: StartPath, cliBackend: BackendId, apiKeyBackend: BackendId = APIKEY_BACKEND): BackendId {
  const backends = path === 'apiKey' ? APIKEY_BACKENDS : CLI_BACKENDS
  const remembered = path === 'apiKey' ? apiKeyBackend : cliBackend
  return backends.some((b) => b.id === remembered)
    ? remembered
    : path === 'apiKey' ? APIKEY_BACKEND : CLI_BACKEND_DEFAULT
}

/** Path-card i18n; shared so create/team wizards match onboarding copy. */
export const START_PATH_OPTIONS: {
  id: StartPath
  titleKey: string
  descKey: string
}[] = [
  {
    id: 'apiKey',
    titleKey: 'pages.onboarding.acp.paths.apiKey.title',
    descKey: 'pages.onboarding.acp.paths.apiKey.desc',
  },
  {
    id: 'cli',
    titleKey: 'pages.onboarding.acp.paths.cli.title',
    descKey: 'pages.onboarding.acp.paths.cli.desc',
  },
]

/** Minimal draft shape for path ↔ Backend sync (onboarding / wizard drafts). */
export type StartPathDraft = {
  startPath: StartPath
  acpBackend: BackendId
  cliBackend: BackendId
  apiKeyBackend?: BackendId
}

/**
 * Keep startPath / cliBackend aligned with the selected Backend.
 * Does not touch configRoot / env — callers apply those separately.
 */
export function syncStartPathFields(draft: StartPathDraft, id: BackendId): void {
  const path = startPathForBackend(id)
  draft.startPath = path
  draft.apiKeyBackend = backendForStartPath('apiKey', draft.cliBackend, draft.apiKeyBackend)
  draft.cliBackend = backendForStartPath('cli', draft.cliBackend)
  if (path === 'cli') draft.cliBackend = id
  else draft.apiKeyBackend = id
}
