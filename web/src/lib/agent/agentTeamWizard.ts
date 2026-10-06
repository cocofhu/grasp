import type { MCPServer } from '@/lib/api/api'
import {
  ACP_BACKENDS,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  applyAcpBackend,
  applyWizardStartPath,
  configRootFor,
  kvToRec,
  normalizeWizardRegions,
  recToKV,
  type WizardBackendId,
  type WizardDraft,
  type WizardKV,
  type WizardMCP,
  type WizardAuthMode,
  type StartPath,
  freshDraft,
  parseCustomConfigJson,
  stripAuthKeysFromEnv,
} from '@/lib/agent/agentCreateWizard'
import { normalizeAgentName, validateAgentName } from '@/lib/agent/agentIO'
import type { GitCredentialType } from '@/lib/agent/gitCredentialType'
import { authGuideFor, hasAuthKeyConfigured } from '@/lib/agent/backendAuthGuide'
import { stripSecretKeysFromKV, stripSecretKeysFromRecord } from '@/lib/agent/secretEnvKeys'
import { getRegionPolicy } from '@/lib/shared/regionPolicy'
import { APIKEY_BACKEND, CLI_BACKEND_DEFAULT } from '@/lib/shared/startPath'

export type TeamWizardStepId = 'team' | 'acp' | 'apiKey' | 'git' | 'mcp' | 'env' | 'review'

export type TeamWizardStepDef = {
  id: TeamWizardStepId
  labelKey: string
  skip: boolean
}

export const TEAM_WIZARD_STEPS: TeamWizardStepDef[] = [
  { id: 'team', labelKey: 'pages.agentStudio.teamWizard.steps.team', skip: false },
  { id: 'acp', labelKey: 'pages.agentStudio.teamWizard.steps.acp', skip: true },
  { id: 'apiKey', labelKey: 'pages.agentStudio.teamWizard.steps.apiKey', skip: true },
  { id: 'git', labelKey: 'pages.agentStudio.teamWizard.steps.git', skip: true },
  { id: 'mcp', labelKey: 'pages.agentStudio.teamWizard.steps.mcp', skip: true },
  { id: 'env', labelKey: 'pages.agentStudio.teamWizard.steps.env', skip: true },
  { id: 'review', labelKey: 'pages.agentStudio.teamWizard.steps.review', skip: false },
]

/** Mirrors server TeamEngineerTemplates: 需求澄清 / 实现 / 测试评审 / 交付. */
export const TEAM_ENGINEER_COUNT = 4

export type TeamWizardDraft = {
  step: number
  projectName: string
  prefix: string
  pmName: string
  background: string
  prefixTouched: boolean
  pmTouched: boolean
  startPath: StartPath
  acpBackend: WizardBackendId
  cliBackend: WizardBackendId
  authMode: WizardAuthMode
  customConfigContent: string
  gitCredentialType?: GitCredentialType
  gitUrl: string
  configRoot: string
  env: WizardKV[]
  mcp: WizardMCP[]
  skipped: Partial<Record<TeamWizardStepId, boolean>>
}

export function artifactStorePreset(): WizardMCP {
  return {
    name: 'artifact-store',
    transport: 'url',
    url: '${GRASP_ARTIFACT_URL}',
    headers: [{ k: 'Authorization', v: 'Bearer ${GRASP_ARTIFACT_TOKEN}' }],
    command: '',
    args: '',
    env: [],
  }
}

export function freshTeamDraft(): TeamWizardDraft {
  return {
    step: 0,
    projectName: '',
    prefix: '',
    pmName: '',
    background: '',
    prefixTouched: false,
    pmTouched: false,
    startPath: 'apiKey',
    acpBackend: APIKEY_BACKEND,
    cliBackend: CLI_BACKEND_DEFAULT,
    authMode: 'apiKey',
    customConfigContent: '',
    gitCredentialType: undefined,
    gitUrl: '',
    configRoot: configRootFor(APIKEY_BACKEND),
    env: [{ k: 'GIT_REPOS', v: '${vars.repos}' }],
    mcp: [artifactStorePreset()],
    skipped: {},
  }
}

export function syncDerivedNames(d: TeamWizardDraft) {
  const base = (d.prefixTouched ? d.prefix : d.projectName).trim() || d.projectName.trim()
  if (!d.prefixTouched) d.prefix = d.projectName.trim()
  if (!d.pmTouched) d.pmName = base ? `${base}项目经理` : ''
}

export function validateTeamBasics(d: TeamWizardDraft, existingNames: string[]): string {
  if (!d.projectName.trim()) return 'projectRequired'
  if (!d.prefix.trim()) return 'prefixRequired'
  if (!d.background.trim()) return 'backgroundRequired'
  const code = validateAgentName(d.pmName)
  if (code === 'required') return 'pmRequired'
  if (code === 'invalid') return 'pmInvalid'
  const normalized = normalizeAgentName(d.pmName)
  if (existingNames.some((n) => normalizeAgentName(n) === normalized)) return 'pmExists'
  return ''
}

function teamDraftAsWizardDraft(d: TeamWizardDraft): WizardDraft {
  const base = freshDraft()
  base.startPath = d.startPath
  base.acpBackend = d.acpBackend
  base.cliBackend = d.cliBackend
  base.configRoot = d.configRoot
  base.authMode = d.authMode
  base.customConfigContent = d.customConfigContent
  base.env = d.env.map((e) => ({ ...e }))
  base.gitCredentialType = d.gitCredentialType
  base.mcp = d.mcp.map((m) => ({
    ...m,
    headers: m.headers.map((h) => ({ ...h })),
    env: m.env.map((e) => ({ ...e })),
  }))
  return base
}

export function applyTeamAcpBackend(d: TeamWizardDraft, id: WizardBackendId) {
  const w = teamDraftAsWizardDraft(d)
  applyAcpBackend(w, id)
  d.startPath = w.startPath
  d.cliBackend = w.cliBackend
  d.acpBackend = w.acpBackend
  d.configRoot = w.configRoot
  d.env = w.env
}

export function applyTeamStartPath(d: TeamWizardDraft, path: StartPath) {
  const w = teamDraftAsWizardDraft(d)
  applyWizardStartPath(w, path)
  d.startPath = w.startPath
  d.cliBackend = w.cliBackend
  d.acpBackend = w.acpBackend
  d.configRoot = w.configRoot
  d.env = w.env
}

function mcpToApi(m: WizardMCP): MCPServer {
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

export type TeamBootstrapPayload = {
  projectName: string
  prefix: string
  pmName: string
  background: string
  acpBackend: WizardBackendId
  apiKey?: string
  customConfig?: string
  region?: string
  gitUrl?: string
  gitCredentialType?: string
  mcp: MCPServer[]
  env: Record<string, string>
}

export function assembleTeamBootstrapPayload(d: TeamWizardDraft): TeamBootstrapPayload {
  const w = teamDraftAsWizardDraft(d)
  const policy = getRegionPolicy(d.acpBackend)
  const region = policy
    ? w.env.find((e) => e.k.trim() === policy.regionEnvKey)?.v?.trim() || undefined
    : undefined
  const guide = authGuideFor(d.acpBackend, region || '')
  const primaryKey = guide.keys[0]?.key || ''
  const customParsed = parseCustomConfigJson(d.customConfigContent)
  const customConfig =
    d.authMode === 'customConfig' && customParsed.ok && customParsed.normalized
      ? customParsed.normalized
      : undefined
  // Capture API Key for project/shared layer before stripping Token keys from agent env.
  const rawEnv = kvToRec(w.env)
  const apiKey =
    !customConfig && primaryKey ? rawEnv[primaryKey]?.trim() || undefined : undefined

  if (w.authMode === 'customConfig') {
    w.env = stripAuthKeysFromEnv(w.env, w.acpBackend)
  }
  w.env = stripSecretKeysFromKV(w.env)
  const env = stripSecretKeysFromRecord(normalizeWizardRegions(w))
  d.env = w.env
  return {
    projectName: d.projectName.trim(),
    prefix: d.prefix.trim(),
    pmName: normalizeAgentName(d.pmName),
    background: d.background.trim(),
    acpBackend: d.acpBackend || APIKEY_BACKEND,
    ...(apiKey ? { apiKey } : {}),
    ...(customConfig ? { customConfig } : {}),
    ...(region ? { region } : {}),
    ...(d.gitUrl.trim() ? { gitUrl: d.gitUrl.trim() } : {}),
    ...(d.gitCredentialType ? { gitCredentialType: d.gitCredentialType } : {}),
    mcp: d.mcp.filter((m) => m.name.trim()).map(mcpToApi),
    env,
  }
}

export function teamHasAuth(d: TeamWizardDraft): boolean {
  if (d.authMode === 'customConfig') {
    const parsed = parseCustomConfigJson(d.customConfigContent)
    return parsed.ok && parsed.normalized !== ''
  }
  return hasAuthKeyConfigured(d.env, d.acpBackend)
}

export { ACP_BACKENDS, CLI_BACKENDS, START_PATH_OPTIONS, recToKV, kvToRec }
export type { WizardBackendId, StartPath }
