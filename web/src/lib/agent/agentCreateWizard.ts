import type { Agent, AgentCapabilities, AgentFile, MCPServer, ModelBinding, AgentGitBindings } from '@/lib/api/api'
import { cloneGitBindings } from './gitBindings'
import { validateAgentName, normalizeAgentName } from '@/lib/agent/agentIO'
import { stripSecretKeysFromKV, stripSecretKeysFromRecord } from '@/lib/agent/secretEnvKeys'
import {
  ACP_BACKENDS,
  isManagedRegionKey,
  normalizeRegions,
  regionSummary,
  switchBackendRegions,
  type BackendId,
} from '@/lib/shared/regionPolicy'

export type WizardBackendId = BackendId
export { ACP_BACKENDS }
/** Wizard step ids. Internal `acp` is shown as Agent via i18n; capability/env steps are not in WIZARD_STEPS. */
export type WizardStepId =
  | 'basics'
  | 'acp'
  | 'git'
  | 'env'
  | 'mcp'
  | 'rules'
  | 'skills'
  | 'commands'
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

export type WizardSkipped = Partial<Record<WizardStepId, boolean>>

export type WizardDraft = {
  inheritModel?: boolean
  inheritGit?: boolean
  gitBindings?: AgentGitBindings
  modelBinding?: ModelBinding
  step: number
  name: string
  description: string
  /**
   * Role pack id for POST /agents templateId.
   * Empty / "blank" = generic agent (default rule). Selection never rewrites name (plan g1.4).
   */
  templateId: string
  acpBackend: WizardBackendId
  configRoot: string
  env: WizardKV[]
  mcp: WizardMCP[]
  rulesEdited: boolean
  rulesContent: string
  skills: WizardSkill[]
  commands: WizardCommand[]
  skipped: WizardSkipped
}

export type WizardStepDef = {
  id: WizardStepId
  labelKey: string
  skip: boolean
}

/** Basics → coding tool and model (acp) → Git → review. */
export const WIZARD_STEPS: WizardStepDef[] = [
  { id: 'basics', labelKey: 'pages.agentStudio.wizard.steps.basics', skip: false },
  { id: 'acp', labelKey: 'pages.agentStudio.wizard.steps.acp', skip: false },
  { id: 'git', labelKey: 'pages.agentStudio.wizard.steps.git', skip: true },
  { id: 'review', labelKey: 'pages.agentStudio.wizard.steps.review', skip: false },
]

export const DEFAULT_CONFIG_ROOT = '/root/.cursor'
export const DEFAULT_WORKSPACE_DIR = '/root/workspace'

export const GIT_ENV_KEYS = new Set([
  'GIT_REPOS',
  'GITHUB_TOKEN',
  'GITHUB_URL',
  'GITLAB_TOKEN',
  'GITLAB_URL',
  'GIT_SSH_PRIVATE_KEY',
  'GIT_SSH_KNOWN_HOSTS',
])

/** Starting capabilities of a blank Agent: one autonomous run reading every product. */
export function blankAgentCapabilities(): AgentCapabilities {
  return { interaction: 'auto', reads: ['*'] }
}

export function freshDraft(): WizardDraft {
  return {
    inheritModel: true,
    inheritGit: true,
    gitBindings: { items: [] },
    step: 0,
    name: '',
    description: '',
    templateId: 'blank',
    acpBackend: 'cursor',
    modelBinding: { kind: 'configuration', configurationId: '' },
    configRoot: configRootFor('cursor'),
    env: [],
    mcp: [],
    rulesEdited: false,
    rulesContent: '',
    skills: [],
    commands: [],
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
  draft.inheritModel = false
  if (draft.acpBackend !== id) draft.modelBinding = { kind: 'configuration', configurationId: '' }
  draft.acpBackend = id
  draft.configRoot = configRootFor(id)
  draft.env = recToKV(switchBackendRegions(kvToRec(draft.env), id))
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

function collectFiles(draft: WizardDraft): AgentFile[] {
  const files: AgentFile[] = []
  if (hasRoleTemplate(draft)) return files
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
  return files
}

/** Assemble POST /agents payload. Skip Rules still writes default rule.
 * Token-class keys are always stripped from env (write them in Workspace shared Agent config).
 * When templateId is set (not blank), payload includes templateId and omits default identity files
 * so the server can copy the embed pack and its capabilities; blank Agents start with
 * blankAgentCapabilities(). */
export function assembleCreatePayload(draft: WizardDraft): Agent & { templateId?: string } {
  const name = normalizeAgentName(draft.name)
  const envDraft: WizardDraft = {
    ...draft,
    env: stripSecretKeysFromKV(draft.env),
  }
  const env = stripSecretKeysFromRecord(normalizeWizardRegions(envDraft))
  for (const key of ['ACP_BRIDGE_MODEL', 'ACP_BRIDGE_REASONING_EFFORT', 'GRASP_OPENCODE_PROVIDER', 'GRASP_OPENCODE_BASE_URL', 'GRASP_OPENCODE_MODEL_VISION']) delete env[key]
  const useTemplate = hasRoleTemplate(draft)
  const files = collectFiles(draft)
  return {
    name,
    ...(draft.inheritModel !== undefined ? { inheritModel: draft.inheritModel } : {}),
    ...(draft.inheritGit !== undefined ? { inheritGit: draft.inheritGit } : {}),
    gitBindings: cloneGitBindings(draft.gitBindings || { items: [] }),
    acpBackend: draft.acpBackend,
    ...(!draft.inheritModel || draft.modelBinding?.configurationId?.trim()
      ? { modelBinding: { kind: 'configuration' as const, configurationId: draft.modelBinding?.configurationId || '' } }
      : {}),
    ...(useTemplate ? { templateId: draft.templateId.trim() } : {}),
    files,
    mcp: draft.mcp.filter((m) => m.name.trim()).map(draftMcpToApi),
    env,
    layout: {
      configRoot: draft.configRoot.trim() || configRootFor(draft.acpBackend),
      workspaceDir: DEFAULT_WORKSPACE_DIR,
    },
    ...(useTemplate ? {} : { capabilities: blankAgentCapabilities() }),
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

export type ReviewChipKind = 'ok' | 'empty' | 'def'

export type ReviewSummaryItem = {
  key: string
  kind: ReviewChipKind
  labelKey: string
  detail?: string
}

/** Aliases resolved by the wizard. Ids are the fallback when a name is unknown. */
export type WizardReviewNames = {
  model?: string
  gitItems?: Record<string, string>
  preferredLabel?: string
}

/** Build confirmation-page summary chips aligned to the 4-step wizard. */
export function buildReviewSummary(draft: WizardDraft, names: WizardReviewNames = {}): ReviewSummaryItem[] {
  const normalizedEnv = normalizeRegions(kvToRec(draft.env), draft.acpBackend, 'strict').env
  const region = regionSummary(normalizedEnv, draft.acpBackend, 'strict')
  const name = draft.name.trim() || '—'
  const authConfigured = !!draft.modelBinding?.configurationId
  const apiKeySkipped = !authConfigured
  const gitParts = (draft.gitBindings?.items || []).map((item) => `${names.gitItems?.[item.credentialId] || item.credentialId}${item.credentialId === draft.gitBindings?.preferredCredentialId ? ` (${names.preferredLabel || 'Preferred'})` : ''}`)

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
      labelKey: 'pages.agentStudio.wizard.review.modelSource_configuration',
      detail: names.model || draft.modelBinding?.configurationId,
    },
    {
      key: 'git',
      kind: gitParts.length ? 'ok' : 'empty',
      labelKey: 'pages.agentStudio.wizard.review.git',
      detail: gitParts.length ? gitParts.join(' · ') : undefined,
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
