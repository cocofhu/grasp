import type { BackendId } from '@/lib/shared/regionPolicy'
import { getRegionPolicy } from '@/lib/shared/regionPolicy'
import type { GitCredentialType } from '@/lib/agent/gitCredentialAnalysis'
import type { AppLocale } from '@/lib/shared/loadLocaleMessages'
import type { ThemeName } from '@/lib/shared/theme'
import { locale } from '@/lib/shared/locale'
import { theme } from '@/lib/shared/theme'
import { DEFAULT_OPENCODE_PROVIDER } from '@/lib/agent/openCodeProvider'
import { normalizeAgentName, validateAgentName } from '@/lib/agent/agentIO'
import {
  APIKEY_BACKEND,
  CLI_BACKEND_DEFAULT,
  CLI_BACKENDS,
  type StartPath,
  startPathForBackend,
  syncStartPathFields,
} from '@/lib/shared/startPath'
import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  migrateLocalStorageKey,
} from '@/lib/shared/migrateBrandStorage'

export {
  APIKEY_BACKEND as ONBOARDING_APIKEY_BACKEND,
  CLI_BACKEND_DEFAULT as ONBOARDING_CLI_BACKEND_DEFAULT,
  CLI_BACKENDS as ONBOARDING_CLI_BACKENDS,
  startPathForBackend,
  type StartPath as OnboardingStartPath,
}

/** Matches models.DefaultProjectID — first-install wizard only opens here. */
export const DEFAULT_PROJECT_ID = 'proj-default'

export const ONBOARDING_WORKFLOW_NAME = '默认工作流'
/** Mirrors services.FirstInstallGroupName. */
export const FIRST_INSTALL_GROUP_NAME = '默认项目组'

/** Built-in templates the wizard creates, in workflow order (server TeamEngineerTemplates). */
export type OnboardingTemplateId = 'clarify' | 'implement' | 'test_review'

export const ONBOARDING_TEMPLATES: readonly { id: OnboardingTemplateId; label: string }[] = [
  { id: 'clarify', label: '需求澄清' },
  { id: 'implement', label: '实现' },
  { id: 'test_review', label: '测试评审' },
]

/**
 * The default workflow cannot run without clarify → implement; only test_review
 * may be unchecked (implement then ends the workflow). Mirrors the server.
 */
export const ONBOARDING_REQUIRED_TEMPLATE_IDS: readonly OnboardingTemplateId[] = ['clarify', 'implement']

const ONBOARDING_ROLE_NAMES_EN: Record<OnboardingTemplateId, string> = {
  clarify: 'Clarify',
  implement: 'Implement',
  test_review: 'TestReview',
}

/** Longest role suffix in any locale (TestReview). */
const LONGEST_ONBOARDING_ROLE_SUFFIX = 10
const MAX_AGENT_NAME_RUNES = 64

/** Canonical default-project Agent names (the server's template labels); used for conflict checks. */
export const ONBOARDING_AGENT_NAMES: readonly string[] = ONBOARDING_TEMPLATES.map((t) => t.label)

/** Default role names shown and saved for the given UI language. */
export function onboardingRoleNames(lang: AppLocale): string[] {
  return ONBOARDING_TEMPLATES.map((t) => (lang === 'en' ? ONBOARDING_ROLE_NAMES_EN[t.id] : t.label))
}

export type OnboardingMode = 'firstInstall' | 'createProject' | 'retry'

export type OnboardingStepId = 'prefs' | 'model' | 'key' | 'git' | 'team' | 'workflow' | 'done'

export type OnboardingStep = {
  id: OnboardingStepId
  labelKey: string
}

/** Same steps in every mode, one topic per page; `done` is the success page after generating. */
export const ONBOARDING_STEPS: OnboardingStep[] = [
  { id: 'prefs', labelKey: 'pages.onboarding.steps.prefs' },
  { id: 'model', labelKey: 'pages.onboarding.steps.model' },
  { id: 'key', labelKey: 'pages.onboarding.steps.key' },
  { id: 'git', labelKey: 'pages.onboarding.steps.git' },
  { id: 'team', labelKey: 'pages.onboarding.steps.team' },
  { id: 'workflow', labelKey: 'pages.onboarding.steps.workflow' },
  { id: 'done', labelKey: 'pages.onboarding.steps.done' },
]

export type OnboardingTeamMember = {
  templateId: OnboardingTemplateId
  enabled: boolean
  /** Full Agent name; '' lets the server derive it from the project name. */
  name: string
  /** True once the user typed a name, so default refreshes leave it alone. */
  nameEdited: boolean
  /** ACP_BRIDGE_MODEL for this Agent; '' inherits the project default. */
  model: string
}

export function freshOnboardingTeam(): OnboardingTeamMember[] {
  return ONBOARDING_TEMPLATES.map((t) => ({
    templateId: t.id,
    enabled: true,
    name: '',
    nameEdited: false,
    model: '',
  }))
}

export function isRequiredTemplate(id: OnboardingTemplateId): boolean {
  return ONBOARDING_REQUIRED_TEMPLATE_IDS.includes(id)
}

/** Required templates stay checked. */
export function setTeamMemberEnabled(team: OnboardingTeamMember[], id: OnboardingTemplateId, enabled: boolean): void {
  const m = team.find((x) => x.templateId === id)
  if (!m) return
  m.enabled = isRequiredTemplate(id) ? true : enabled
}

/** Fill names the user has not edited with the derived defaults (same order as ONBOARDING_TEMPLATES). */
export function applyDefaultTeamNames(team: OnboardingTeamMember[], defaults: readonly string[]): void {
  ONBOARDING_TEMPLATES.forEach((t, i) => {
    const m = team.find((x) => x.templateId === t.id)
    if (m && !m.nameEdited) m.name = defaults[i] || ''
  })
}

export type TeamNameIssue = '' | 'required' | 'invalid' | 'duplicate'

/** Per-member name problems for enabled members. Blank is fine only when the server derives it. */
export function teamNameIssues(
  team: OnboardingTeamMember[],
  opts: { allowBlank?: boolean } = {},
): Record<OnboardingTemplateId, TeamNameIssue> {
  const out = { clarify: '', implement: '', test_review: '' } as Record<OnboardingTemplateId, TeamNameIssue>
  const counts = new Map<string, number>()
  for (const m of team) {
    if (!m.enabled) continue
    const n = normalizeAgentName(m.name)
    if (n) counts.set(n, (counts.get(n) || 0) + 1)
  }
  for (const m of team) {
    if (!m.enabled) continue
    const n = normalizeAgentName(m.name)
    if (!n) {
      if (!opts.allowBlank) out[m.templateId] = 'required'
      continue
    }
    if (validateAgentName(n)) out[m.templateId] = 'invalid'
    else if ((counts.get(n) || 0) > 1) out[m.templateId] = 'duplicate'
  }
  return out
}

export function teamValid(team: OnboardingTeamMember[], opts: { allowBlank?: boolean } = {}): boolean {
  return Object.values(teamNameIssues(team, opts)).every((v) => !v)
}

export type OnboardingPreviewNode = {
  id: string
  kind: 'input' | 'agent' | 'output'
  templateId?: OnboardingTemplateId
  name?: string
}

export type OnboardingPreviewEdge = {
  from: string
  to: string
  handle?: 'pass' | 'fail'
}

export type OnboardingWorkflowPreview = {
  nodes: OnboardingPreviewNode[]
  edges: OnboardingPreviewEdge[]
}

/**
 * The default workflow bootstrap will publish for this team:
 * input → 需求澄清 → 实现 → 测试评审 -pass→ output, -fail→ 实现.
 * Without test_review, 实现 goes straight to output (same pruning as the server).
 */
export function buildOnboardingWorkflowPreview(team: OnboardingTeamMember[]): OnboardingWorkflowPreview {
  const agents = ONBOARDING_TEMPLATES.filter(
    (t) => isRequiredTemplate(t.id) || team.find((m) => m.templateId === t.id)?.enabled,
  )
  const nodes: OnboardingPreviewNode[] = [
    { id: 'input', kind: 'input' },
    ...agents.map((t) => ({
      id: t.id,
      kind: 'agent' as const,
      templateId: t.id,
      name: normalizeAgentName(team.find((m) => m.templateId === t.id)?.name || '') || t.label,
    })),
    { id: 'output', kind: 'output' },
  ]
  const edges: OnboardingPreviewEdge[] = []
  for (let i = 0; i < nodes.length - 1; i++) {
    const from = nodes[i]!
    const to = nodes[i + 1]!
    edges.push(from.id === 'test_review' ? { from: from.id, to: to.id, handle: 'pass' } : { from: from.id, to: to.id })
  }
  if (agents.some((t) => t.id === 'test_review')) {
    edges.push({ from: 'test_review', to: 'implement', handle: 'fail' })
  }
  return { nodes, edges }
}

export const ONBOARDING_GIT_TYPES: { id: GitCredentialType; labelKey: string }[] = [
  { id: 'github_https', labelKey: 'pages.agentStudio.git.types.github_https' },
  { id: 'gitlab_https', labelKey: 'pages.agentStudio.git.types.gitlab_https' },
  { id: 'ssh', labelKey: 'pages.agentStudio.git.types.ssh' },
]

export type OnboardingDraft = {
  step: number
  projectName: string
  language: AppLocale
  theme: ThemeName
  startPath: StartPath
  acpBackend: BackendId
  /** Last CLI-path backend, so switching paths back restores the pick. */
  cliBackend: BackendId
  region: string
  apiKey: string
  gitCredentialType: GitCredentialType | ''
  githubToken: string
  gitlabToken: string
  gitlabUrl: string
  gitSshPrivateKey: string
  gitSshKnownHosts: string
  repoUrl: string
  repoBranch: string
  /** Repo + credentials skipped on the connect page (identity is still required). */
  gitSkipped: boolean
  gitUserName: string
  gitUserEmail: string
  vncPreview: boolean
  browserMcp: boolean
  openCodeProvider: string
  openCodeBaseURL: string
  openCodeModel: string
  openCodeModelVision: boolean
  team: OnboardingTeamMember[]
}

export type OnboardingAgentChoice = {
  templateId: OnboardingTemplateId
  name?: string
  model?: string
}

export type OnboardingBootstrapBody = {
  acpBackend: BackendId
  apiKey: string
  region?: string
  gitCredentialType?: GitCredentialType
  githubToken?: string
  gitlabToken?: string
  gitlabUrl?: string
  gitSshPrivateKey?: string
  gitSshKnownHosts?: string
  repoUrl?: string
  repoBranch?: string
  gitUserName?: string
  gitUserEmail?: string
  vncPreview?: boolean
  browserMcp?: boolean
  openCodeProvider?: string
  openCodeBaseURL?: string
  openCodeModel?: string
  openCodeModelVision?: boolean
  agents?: OnboardingAgentChoice[]
}

export type OnboardingBootstrapResult = {
  agentIds: string[]
  workflowId: string
  published: boolean
  groupName?: string
}

/**
 * Hard suppression for tests and local debugging only. The wizard's "later"
 * button deliberately does NOT write this: closing it is per-view, and a reload
 * re-opens the wizard until the default workflow exists (see needsOnboarding).
 * The key differs from the old `approving-onboarding-dismiss:` one so browsers
 * that dismissed the wizard before this rule change are not stuck forever.
 * Brand clear: migrate from approving-onboarding-suppress: → grasp-… once.
 */
const SUPPRESS_PREFIX = GRASP_STORAGE_KEYS.onboardingSuppressPrefix

export function onboardingSuppressKey(projectId: string): string {
  const key = `${SUPPRESS_PREFIX}${projectId}`
  migrateLocalStorageKey(`${LEGACY_STORAGE_KEYS.onboardingSuppressPrefix}${projectId}`, key)
  return key
}

export function isOnboardingSuppressed(projectId: string): boolean {
  if (!projectId) return true
  try {
    return localStorage.getItem(onboardingSuppressKey(projectId)) === '1'
  } catch {
    return true
  }
}

export function suppressOnboarding(projectId: string): void {
  if (!projectId) return
  try {
    localStorage.setItem(onboardingSuppressKey(projectId), '1')
  } catch {
    /* ignore */
  }
}

/** Wash project name into a valid Agent-name prefix (mirrors server SanitizeOnboardingPrefix). */
export function sanitizeOnboardingPrefix(projectName: string): string {
  const raw = normalizeAgentName(projectName)
  if (!raw) return ''
  let out = ''
  for (const ch of raw) {
    if (/\s/u.test(ch)) continue
    if (/[./\\]/u.test(ch)) continue
    if (/[－＿．／＼、，。！？：；（）【】]/u.test(ch)) continue
    if (/^[\p{L}\p{N}_-]$/u.test(ch)) out += ch
  }
  const maxPrefix = MAX_AGENT_NAME_RUNES - LONGEST_ONBOARDING_ROLE_SUFFIX
  if (Array.from(out).length > maxPrefix) {
    out = Array.from(out).slice(0, maxPrefix).join('')
  }
  return validateAgentName(out) === '' ? out : ''
}

/** Agent names that bootstrap will create for this project. */
export function deriveOnboardingAgentNames(projectId: string, projectName: string, lang: AppLocale = 'zh-CN'): string[] {
  const roles = onboardingRoleNames(lang)
  if (projectId === DEFAULT_PROJECT_ID) return roles
  const prefix = sanitizeOnboardingPrefix(projectName)
  if (!prefix) return []
  return roles.map((n) => prefix + n)
}

function hasOnboardingNameConflict(
  agents: { name?: string; projectId?: string }[],
  projectId: string,
  names: readonly string[],
): boolean {
  return agents.some((a) => {
    const name = (a.name || '').trim()
    if (!name || !(names as readonly string[]).includes(name)) return false
    const owner = (a.projectId || '').trim()
    return owner !== '' && owner !== projectId
  })
}

/**
 * Empty project eligible for install CTA: 0 workflows, 0 bound agents, no name conflicts.
 * Any project may retry; App-level auto-open stays default-only via needsOnboarding.
 */
export function isEmptyProjectForOnboarding(
  workflowCount: number,
  agents: { name?: string; projectId?: string }[],
  projectId: string,
  projectName = '',
): boolean {
  if (!projectId) return false
  if (workflowCount > 0) return false
  const bound = agents.filter((a) => (a.projectId || '') === projectId)
  if (bound.length > 0) return false
  const names = deriveOnboardingAgentNames(projectId, projectName)
  if (!names.length) return projectId === DEFAULT_PROJECT_ID
  return !hasOnboardingNameConflict(agents, projectId, [...names, ...deriveOnboardingAgentNames(projectId, projectName, 'en')])
}

/** The default workflow is the completion marker for first install. */
export function hasDefaultWorkflow(workflows: { name?: string }[]): boolean {
  return workflows.some((w) => (w.name || '').trim() === ONBOARDING_WORKFLOW_NAME)
}

/**
 * App-level auto-open only for the default project until 默认工作流 exists.
 */
export function needsOnboarding(
  workflows: { name?: string }[],
  agents: { name?: string; projectId?: string }[],
  projectId: string,
): boolean {
  if (projectId !== DEFAULT_PROJECT_ID) return false
  if (hasDefaultWorkflow(workflows)) return false
  return !hasOnboardingNameConflict(agents, projectId, ONBOARDING_AGENT_NAMES)
}

export function shouldAutoOpenOnboarding(
  projectId: string,
  workflows: { name?: string }[],
  agents: { name?: string; projectId?: string }[],
): boolean {
  if (!projectId) return false
  if (isOnboardingSuppressed(projectId)) return false
  return needsOnboarding(workflows, agents, projectId)
}

/**
 * @param opts.inheritAppLocale — createProject: seed from current app locale
 *   (not browser/OS). firstInstall/retry keep detectSystemLocale().
 */
export function freshOnboardingDraft(opts?: { inheritAppLocale?: boolean }): OnboardingDraft {
  return {
    step: 0,
    projectName: '',
    language: opts?.inheritAppLocale ? locale.value : detectSystemLocale(),
    theme: theme.value,
    startPath: 'apiKey',
    acpBackend: APIKEY_BACKEND,
    cliBackend: CLI_BACKEND_DEFAULT,
    region: getRegionPolicy(APIKEY_BACKEND)?.defaultRegion || '',
    apiKey: '',
    gitCredentialType: '',
    githubToken: '',
    gitlabToken: '',
    gitlabUrl: '',
    gitSshPrivateKey: '',
    gitSshKnownHosts: '',
    repoUrl: '',
    repoBranch: '',
    gitSkipped: false,
    gitUserName: '',
    gitUserEmail: '',
    vncPreview: true,
    browserMcp: true,
    openCodeProvider: DEFAULT_OPENCODE_PROVIDER,
    openCodeBaseURL: '',
    openCodeModel: '',
    openCodeModelVision: false,
    team: freshOnboardingTeam(),
  }
}

/**
 * Select a backend. Keeps startPath in sync and resets values that belong to the
 * previous backend (region default, and the key, which is vendor-specific).
 */
export function applyOnboardingBackend(draft: OnboardingDraft, id: BackendId): void {
  syncStartPathFields(draft, id)
  if (draft.acpBackend === id) return
  draft.acpBackend = id
  draft.region = getRegionPolicy(id)?.defaultRegion || ''
  draft.apiKey = ''
  if (id === APIKEY_BACKEND && !draft.openCodeProvider) {
    draft.openCodeProvider = DEFAULT_OPENCODE_PROVIDER
  }
}

/** Switch start path; the CLI path restores the last CLI backend that was picked. */
export function applyStartPath(draft: OnboardingDraft, path: StartPath): void {
  const id =
    path === 'apiKey' ? APIKEY_BACKEND : draft.cliBackend || CLI_BACKEND_DEFAULT
  applyOnboardingBackend(draft, id)
}

/** Mirrors the server's RepoNameFromURL so the wizard can preview the clone dir. */
export function repoNameFromUrl(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, '')
  if (!trimmed) return ''
  const seg = trimmed.split(/[/:]/).pop() || ''
  return seg.replace(/\.git$/, '').trim()
}

export function repoConfigured(draft: OnboardingDraft): boolean {
  return !draft.gitSkipped && Boolean(draft.repoUrl.trim())
}

export function gitIdentityConfigured(draft: OnboardingDraft): boolean {
  return Boolean(draft.gitUserName.trim() && draft.gitUserEmail.trim())
}

/** First-install language defaults to the browser/OS language, not a prior app preference. */
export function detectSystemLocale(): AppLocale {
  if (typeof navigator === 'undefined') return 'zh-CN'
  return (navigator.language || '').toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

export function gitConfigured(draft: OnboardingDraft): boolean {
  if (draft.gitSkipped || !draft.gitCredentialType) return false
  if (draft.gitCredentialType === 'github_https') return Boolean(draft.githubToken.trim())
  if (draft.gitCredentialType === 'gitlab_https') return Boolean(draft.gitlabToken.trim())
  if (draft.gitCredentialType === 'ssh') return Boolean(draft.gitSshPrivateKey.trim())
  return false
}

export function assembleBootstrapBody(draft: OnboardingDraft): OnboardingBootstrapBody {
  const body: OnboardingBootstrapBody = {
    acpBackend: draft.acpBackend,
    apiKey: draft.apiKey.trim(),
  }
  const policy = getRegionPolicy(draft.acpBackend)
  if (policy && draft.region.trim()) {
    body.region = draft.region.trim()
  }
  if (!draft.gitSkipped) {
    if (draft.gitCredentialType) {
      body.gitCredentialType = draft.gitCredentialType
    }
    if (draft.githubToken.trim()) body.githubToken = draft.githubToken.trim()
    if (draft.gitlabToken.trim()) body.gitlabToken = draft.gitlabToken.trim()
    if (draft.gitlabUrl.trim()) body.gitlabUrl = draft.gitlabUrl.trim()
    if (draft.gitSshPrivateKey.trim()) body.gitSshPrivateKey = draft.gitSshPrivateKey.trim()
    if (draft.gitSshKnownHosts.trim()) body.gitSshKnownHosts = draft.gitSshKnownHosts.trim()
    if (draft.repoUrl.trim()) {
      body.repoUrl = draft.repoUrl.trim()
      if (draft.repoBranch.trim()) body.repoBranch = draft.repoBranch.trim()
    }
  }
  if (draft.gitUserName.trim()) body.gitUserName = draft.gitUserName.trim()
  if (draft.gitUserEmail.trim()) body.gitUserEmail = draft.gitUserEmail.trim()
  body.vncPreview = draft.vncPreview
  body.browserMcp = draft.browserMcp
  if (draft.acpBackend === 'opencode') {
    body.openCodeProvider = draft.openCodeProvider || 'openai'
    if (draft.openCodeBaseURL.trim()) body.openCodeBaseURL = draft.openCodeBaseURL.trim()
    if (draft.openCodeModel.trim()) body.openCodeModel = draft.openCodeModel.trim()
    body.openCodeModelVision = draft.openCodeModelVision
  }
  body.agents = draft.team
    .filter((m) => m.enabled || isRequiredTemplate(m.templateId))
    .map((m) => {
      const choice: OnboardingAgentChoice = { templateId: m.templateId }
      const name = normalizeAgentName(m.name)
      if (name) choice.name = name
      if (m.model.trim()) choice.model = m.model.trim()
      return choice
    })
  return body
}
