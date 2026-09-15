import type { Agent, AgentFile, AgentPrompts, MCPServer } from '@/lib/api/api'
import type { GitCredentialType } from '@/lib/agent/gitCredentialAnalysis'
import { validateAgentName, normalizeAgentName } from '@/lib/agent/agentIO'
import {
  AGENT_SETTINGS_REL_PATH,
  authGuideFor,
  BACKEND_AUTH_HINTS,
  hasAuthKeyConfigured,
} from '@/lib/agent/backendAuthGuide'
import { stripTokenKeysFromKV, stripTokenKeysFromRecord } from '@/lib/agent/tokenEnvKeys'
import { switchOpenCodeEnv } from '@/lib/agent/openCodeProvider'
import { agentConfigRelPath } from '@/lib/agent/backendAuthGuide'
import {
  ACP_BACKENDS,
  isManagedRegionKey,
  normalizeRegions,
  regionSummary,
  switchBackendRegions,
  type BackendId,
} from '@/lib/shared/regionPolicy'
import {
  APIKEY_BACKEND,
  CLI_BACKEND_DEFAULT,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  backendForStartPath,
  syncStartPathFields,
  type StartPath,
} from '@/lib/shared/startPath'

export type WizardBackendId = BackendId
export { ACP_BACKENDS, CLI_BACKENDS, START_PATH_OPTIONS, type StartPath }
/** Wizard step ids. Internal `acp` is shown as Agent via i18n; capability/env steps are not in WIZARD_STEPS. */
export type WizardStepId =
  | 'basics'
  | 'acp'
  | 'apiKey'
  | 'git'
  | 'env'
  | 'mcp'
  | 'rules'
  | 'skills'
  | 'commands'
  | 'prompts'
  | 'review'

export type WizardKV = { k: string; v: string }

export type WizardMCP = {
  name: string
  transport: 'url' | 'command'
  url: string
  headers: WizardKV[]
  command: string
  args: string
  env: WizardKV[]
}

export type WizardSkill = { name: string; content: string }
export type WizardCommand = { name: string; content: string }

export type WizardPromptKey = keyof AgentPrompts
export type WizardPrompts = Record<WizardPromptKey, string>

export type WizardSkipped = Partial<Record<WizardStepId, boolean>>

export type WizardAuthMode = 'apiKey' | 'customConfig'

export const AGENT_SETTINGS_PATH = AGENT_SETTINGS_REL_PATH

export type WizardDraft = {
  step: number
  name: string
  description: string
  /**
   * Role pack id for POST /agents templateId.
   * Empty / "blank" = generic agent (default rule). Selection never rewrites name (plan g1.4).
   */
  templateId: string
  /** apiKey (OpenCode BYOK) or cli (coding-CLI vendor). */
  startPath: StartPath
  acpBackend: WizardBackendId
  /** Last CLI-path backend, restored when switching back from apiKey. */
  cliBackend: WizardBackendId
  authMode: WizardAuthMode
  customConfigContent: string
  gitCredentialType?: GitCredentialType
  configRoot: string
  env: WizardKV[]
  mcp: WizardMCP[]
  rulesEdited: boolean
  rulesContent: string
  skills: WizardSkill[]
  commands: WizardCommand[]
  prompts: WizardPrompts
  skipped: WizardSkipped
}

export type WizardStepDef = {
  id: WizardStepId
  labelKey: string
  skip: boolean
}

/** Demo v4 IA: basics → Agent(acp) → API Key → Git → review. */
export const WIZARD_STEPS: WizardStepDef[] = [
  { id: 'basics', labelKey: 'pages.agentStudio.wizard.steps.basics', skip: false },
  { id: 'acp', labelKey: 'pages.agentStudio.wizard.steps.acp', skip: true },
  { id: 'apiKey', labelKey: 'pages.agentStudio.wizard.steps.apiKey', skip: true },
  { id: 'git', labelKey: 'pages.agentStudio.wizard.steps.git', skip: true },
  { id: 'review', labelKey: 'pages.agentStudio.wizard.steps.review', skip: false },
]

export const DEFAULT_CONFIG_ROOT = '/root/.cursor'
export const DEFAULT_WORKSPACE_DIR = '/root/workspace'

const WIZARD_PROMPT_KEYS: WizardPromptKey[] = [
  'upstreamArtifactsHeader',
  'producesContract',
  'reactOpenSuffix',
  'producesRetry',
]

export const GIT_ENV_KEYS = new Set([
  'GIT_REPOS',
  'GITHUB_TOKEN',
  'GITHUB_URL',
  'GITLAB_TOKEN',
  'GITLAB_URL',
  'GIT_SSH_PRIVATE_KEY',
  'GIT_SSH_KNOWN_HOSTS',
])

export function emptyPrompts(): WizardPrompts {
  return {
    upstreamArtifactsHeader: '',
    producesContract: '',
    reactOpenSuffix: '',
    producesRetry: '',
  }
}

export function freshDraft(): WizardDraft {
  return {
    step: 0,
    name: '',
    description: '',
    templateId: 'blank',
    startPath: 'apiKey',
    acpBackend: APIKEY_BACKEND,
    cliBackend: CLI_BACKEND_DEFAULT,
    authMode: 'apiKey',
    customConfigContent: '',
    gitCredentialType: undefined,
    configRoot: configRootFor(APIKEY_BACKEND),
    env: [],
    mcp: [],
    rulesEdited: false,
    rulesContent: '',
    skills: [],
    commands: [],
    prompts: emptyPrompts(),
    skipped: {},
  }
}

/** True when a role pack template is selected (not blank). */
export function hasRoleTemplate(draft: WizardDraft): boolean {
  const id = (draft.templateId || 'blank').trim()
  return id !== '' && id !== 'blank'
}

export function configRootFor(backend: WizardBackendId): string {
  return ACP_BACKENDS.find((b) => b.id === backend)?.configRoot || DEFAULT_CONFIG_ROOT
}

export function applyAcpBackend(draft: WizardDraft, id: WizardBackendId): void {
  syncStartPathFields(draft, id)
  draft.acpBackend = id
  draft.configRoot = configRootFor(id)
  draft.env = recToKV(switchOpenCodeEnv(switchBackendRegions(kvToRec(draft.env), id), id))
}

/** Switch start path; CLI restores the last CLI backend that was picked. */
export function applyWizardStartPath(draft: WizardDraft, path: StartPath): void {
  applyAcpBackend(draft, backendForStartPath(path, draft.cliBackend))
}

/** True when switching Backend may remapping path-dependent configs. */
export function hasPathDeps(draft: WizardDraft): boolean {
  return (
    draft.mcp.length > 0 ||
    draft.skills.length > 0 ||
    draft.commands.length > 0 ||
    draft.rulesEdited
  )
}

/** Default alwaysApply identity rule; description (if any) becomes the preface. */
export function buildDefaultRule(name: string, description = ''): string {
  const n = name.trim() || 'agent'
  const intro = description.trim() ? `${description.trim()}\n\n` : ''
  return `---\ndescription: ${n} 身份\nalwaysApply: true\n---\n\n# ${n}\n\n${intro}描述该 Agent 的职责与行为。`
}

function defaultSkillTemplate(name: string): string {
  const n = name.trim() || 'skill'
  return `---\nname: ${n}\ndescription: \n---\n\n# ${n}\n\n描述该 Skill 的用途与用法。\n`
}

function defaultCommandTemplate(name: string): string {
  const n = name.trim() || 'command'
  return `# ${n}\n\n描述该 Command 的用途与触发方式。\n`
}

export function kvToRec(kvs: WizardKV[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const { k, v } of kvs) {
    if (k.trim()) out[k.trim()] = v
  }
  return out
}

export function recToKV(rec: Record<string, string>): WizardKV[] {
  return Object.entries(rec).map(([k, v]) => ({ k, v }))
}

export function normalizeWizardRegions(draft: WizardDraft): Record<string, string> {
  const normalized = normalizeRegions(kvToRec(draft.env), draft.acpBackend, 'strict').env
  draft.env = recToKV(normalized)
  return normalized
}

function draftMcpToApi(m: WizardMCP): MCPServer {
  if (m.transport === 'command') {
    return {
      name: m.name.trim(),
      command: m.command.trim(),
      args: m.args
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean),
      env: kvToRec(m.env),
    }
  }
  return {
    name: m.name.trim(),
    url: m.url.trim(),
    headers: kvToRec(m.headers),
  }
}

function draftPromptsToApi(p: WizardPrompts, skipped: boolean): AgentPrompts | undefined {
  if (skipped) return undefined
  const out: AgentPrompts = {}
  let any = false
  for (const k of WIZARD_PROMPT_KEYS) {
    if (p[k].trim()) {
      out[k] = p[k]
      any = true
    }
  }
  return any ? out : undefined
}

export function parseCustomConfigJson(
  raw: string,
): { ok: true; normalized: string } | { ok: false } {
  const trimmed = raw.trim()
  if (!trimmed) return { ok: true, normalized: '' }
  try {
    JSON.parse(trimmed)
    return { ok: true, normalized: trimmed }
  } catch {
    return { ok: false }
  }
}

export function hasCustomConfigWritten(draft: WizardDraft): boolean {
  if (draft.authMode !== 'customConfig') return false
  const parsed = parseCustomConfigJson(draft.customConfigContent)
  return parsed.ok && parsed.normalized !== ''
}

export function stripAuthKeysFromEnv(env: WizardKV[], backend: WizardBackendId): WizardKV[] {
  const guide = authGuideFor(backend)
  const keys = new Set<string>()
  for (const spec of guide.keys) {
    keys.add(spec.key)
    if (spec.alt) keys.add(spec.alt)
  }
  const hint = BACKEND_AUTH_HINTS[backend]
  keys.add(hint.key)
  if (hint.alt) keys.add(hint.alt)
  if (backend === 'trae') keys.add('TRAE_API_KEY')
  return env.filter((e) => !keys.has(e.k.trim()))
}

function collectFiles(draft: WizardDraft): AgentFile[] {
  const files: AgentFile[] = []
  // Role template: backend copies embed workspace; only attach custom auth config if any.
  if (hasRoleTemplate(draft)) {
    if (draft.authMode === 'customConfig') {
      const parsed = parseCustomConfigJson(draft.customConfigContent)
      if (parsed.ok && parsed.normalized) {
        files.push({ path: agentConfigRelPath(draft.acpBackend), content: parsed.normalized })
      }
    }
    return files
  }
  const name = draft.name.trim() || 'agent'
  const ruleContent =
    draft.rulesEdited && draft.rulesContent.trim()
      ? draft.rulesContent
      : buildDefaultRule(name, draft.description)
  files.push({ path: `rules/${name}.md`, content: ruleContent })

  for (const s of draft.skills) {
    const slug = s.name.trim()
    if (!slug) continue
    files.push({
      path: `skills/${slug}/SKILL.md`,
      content: s.content || defaultSkillTemplate(slug),
    })
  }
  for (const c of draft.commands) {
    const slug = c.name.trim()
    if (!slug) continue
    files.push({
      path: `commands/${slug}.md`,
      content: c.content || defaultCommandTemplate(slug),
    })
  }
  if (draft.authMode === 'customConfig') {
    const parsed = parseCustomConfigJson(draft.customConfigContent)
    if (parsed.ok && parsed.normalized) {
      files.push({ path: agentConfigRelPath(draft.acpBackend), content: parsed.normalized })
    }
  }
  return files
}

/** Assemble POST /agents payload. Skip Rules still writes default rule; Skip Prompts omits prompts.
 * Token-class keys are always stripped from env (write them in Project shared Agent config).
 * When templateId is set (not blank), payload includes templateId and omits default identity files
 * so the server can copy the embed pack (plan g2.1). */
export function assembleCreatePayload(draft: WizardDraft): Agent & { templateId?: string } {
  const name = normalizeAgentName(draft.name)
  const prompts = draftPromptsToApi(draft.prompts, !!draft.skipped.prompts)
  const envDraft: WizardDraft = {
    ...draft,
    env: stripTokenKeysFromKV(
      draft.authMode === 'customConfig'
        ? stripAuthKeysFromEnv(draft.env, draft.acpBackend)
        : draft.env,
    ),
  }
  const env = stripTokenKeysFromRecord(normalizeWizardRegions(envDraft))
  const useTemplate = hasRoleTemplate(draft)
  const files = collectFiles(draft)
  return {
    name,
    acpBackend: draft.acpBackend || APIKEY_BACKEND,
    ...(draft.gitCredentialType ? { gitCredentialType: draft.gitCredentialType } : {}),
    ...(useTemplate ? { templateId: draft.templateId.trim() } : {}),
    files,
    mcp: draft.mcp.filter((m) => m.name.trim()).map(draftMcpToApi),
    env,
    layout: {
      configRoot: draft.configRoot.trim() || configRootFor(draft.acpBackend),
      workspaceDir: DEFAULT_WORKSPACE_DIR,
    },
    ...(prompts ? { prompts } : {}),
  }
}

export function validateBasics(draft: WizardDraft, existingNames: string[]): string {
  const code = validateAgentName(draft.name)
  if (code === 'required') return 'required'
  if (code === 'invalid') return 'invalid'
  const normalized = normalizeAgentName(draft.name)
  if (existingNames.includes(normalized)) return 'exists'
  return ''
}

export function envConfiguredCount(draft: WizardDraft, gitOnly = false): number {
  return draft.env.filter((e) => {
    const k = e.k.trim()
    if (!k) return false
    return gitOnly ? GIT_ENV_KEYS.has(k) : !GIT_ENV_KEYS.has(k) && !isManagedRegionKey(k)
  }).length
}

function promptConfiguredCount(draft: WizardDraft): number {
  return WIZARD_PROMPT_KEYS.filter((k) => draft.prompts[k].trim()).length
}

export type ReviewChipKind = 'ok' | 'empty' | 'def'

export type ReviewSummaryItem = {
  key: string
  kind: ReviewChipKind
  labelKey: string
  detail?: string
}

/** Build confirmation-page summary chips aligned to the 5-step wizard IA. */
export function buildReviewSummary(draft: WizardDraft): ReviewSummaryItem[] {
  const normalizedEnv = normalizeRegions(kvToRec(draft.env), draft.acpBackend, 'strict').env
  const region = regionSummary(normalizedEnv, draft.acpBackend, 'strict')
  const name = draft.name.trim() || '—'
  const gitN = envConfiguredCount(draft, true)
  const authViaKey =
    draft.authMode === 'apiKey' && hasAuthKeyConfigured(draft.env, draft.acpBackend)
  const authViaConfig = hasCustomConfigWritten(draft)
  const authConfigured = authViaKey || authViaConfig
  const apiKeySkipped = !!draft.skipped.apiKey || !authConfigured

  const templateDetail = hasRoleTemplate(draft) ? draft.templateId.trim() : 'blank'
  const items: ReviewSummaryItem[] = [
    { key: 'name', kind: 'ok', labelKey: 'pages.agentStudio.wizard.review.name', detail: name },
    {
      key: 'template',
      kind: 'ok',
      labelKey: 'pages.agentStudio.wizard.review.template',
      detail: templateDetail,
    },
    {
      key: 'acp',
      kind: 'ok',
      labelKey: 'pages.agentStudio.wizard.review.acp',
      detail: `${draft.acpBackend} · ${draft.configRoot}`,
    },
    ...(region
      ? [
          {
            key: 'region',
            kind: 'ok' as const,
            labelKey: region.labelKey!,
            detail: region.region,
          },
        ]
      : []),
    {
      key: 'apiKey',
      kind: authConfigured ? 'ok' : 'empty',
      labelKey: authViaConfig
        ? 'pages.agentStudio.wizard.review.customConfigWritten'
        : authViaKey
          ? 'pages.agentStudio.wizard.review.apiKeyConfigured'
          : 'pages.agentStudio.wizard.review.apiKeySkipped',
    },
    {
      key: 'git',
      kind: gitN > 0 || draft.gitCredentialType ? 'ok' : 'empty',
      labelKey: 'pages.agentStudio.wizard.review.git',
      detail:
        gitN > 0
          ? String(gitN)
          : draft.gitCredentialType
            ? draft.gitCredentialType
            : undefined,
    },
  ]
  if (apiKeySkipped && !authConfigured) {
    items.push({
      key: 'authReminder',
      kind: 'def',
      labelKey: 'pages.agentStudio.wizard.review.authReminder',
    })
  }
  return items
}
