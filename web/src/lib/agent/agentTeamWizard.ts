import type { AgentGitBindings, MCPServer } from '@/lib/api/api'
import { normalizeAgentName, validateAgentName } from '@/lib/agent/agentIO'
import { cloneGitBindings } from '@/lib/agent/gitBindings'
import { kvToRec, type WizardKV, type WizardMCP } from '@/lib/agent/agentCreateWizard'
import { stripSecretKeysFromRecord } from '@/lib/agent/secretEnvKeys'

export type TeamWizardStepId = 'team' | 'model' | 'git' | 'mcp' | 'env' | 'review'
export type TeamWizardStepDef = { id: TeamWizardStepId; labelKey: string; skip: boolean }
export const TEAM_WIZARD_STEPS: TeamWizardStepDef[] = [
  { id: 'team', labelKey: 'pages.agentStudio.teamWizard.steps.team', skip: false },
  { id: 'model', labelKey: 'pages.agentStudio.wizard.steps.apiKey', skip: false },
  { id: 'git', labelKey: 'pages.agentStudio.teamWizard.steps.git', skip: true },
  { id: 'mcp', labelKey: 'pages.agentStudio.teamWizard.steps.mcp', skip: true },
  { id: 'env', labelKey: 'pages.agentStudio.teamWizard.steps.env', skip: true },
  { id: 'review', labelKey: 'pages.agentStudio.teamWizard.steps.review', skip: false },
]

/** Mirrors server TeamEngineerTemplates: 需求澄清 / 实现 / 测试评审 / 交付. */
export const TEAM_ENGINEER_COUNT = 4

export type TeamWizardDraft = {
  step: number; teamName: string; prefix: string; pmName: string; background: string
  prefixTouched: boolean; pmTouched: boolean
  modelConfigurationId: string; gitBindings: AgentGitBindings; gitUrl: string
  env: WizardKV[]; mcp: WizardMCP[]; skipped: Partial<Record<TeamWizardStepId, boolean>>
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
  return { step: 0, teamName: '', prefix: '', pmName: '', background: '', prefixTouched: false, pmTouched: false, modelConfigurationId: '', gitBindings: { items: [] }, gitUrl: '', env: [{ k: 'GIT_REPOS', v: '${vars.repos}' }], mcp: [artifactStorePreset()], skipped: {} }
}

export function syncDerivedNames(d: TeamWizardDraft) {
  const base = (d.prefixTouched ? d.prefix : d.teamName).trim() || d.teamName.trim()
  if (!d.prefixTouched) d.prefix = d.teamName.trim()
  if (!d.pmTouched) d.pmName = base ? `${base}项目经理` : ''
}

export function validateTeamBasics(d: TeamWizardDraft, existingNames: string[]): string {
  if (!d.teamName.trim()) return 'teamNameRequired'
  if (!d.prefix.trim()) return 'prefixRequired'
  if (!d.background.trim()) return 'backgroundRequired'
  const code = validateAgentName(d.pmName)
  if (code === 'required') return 'pmRequired'
  if (code === 'invalid') return 'pmInvalid'
  const normalized = normalizeAgentName(d.pmName)
  if (existingNames.some((n) => normalizeAgentName(n) === normalized)) return 'pmExists'
  return ''
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
  prefix: string; pmName: string; background: string; modelConfigurationId: string; gitBindings: AgentGitBindings
  gitUrl?: string; mcp: MCPServer[]; env: Record<string, string>
}
export function assembleTeamBootstrapPayload(draft: TeamWizardDraft): TeamBootstrapPayload {
  return { prefix: draft.prefix.trim(), pmName: normalizeAgentName(draft.pmName), background: draft.background.trim(), modelConfigurationId: draft.modelConfigurationId, gitBindings: cloneGitBindings(draft.gitBindings), ...(draft.gitUrl.trim() ? { gitUrl: draft.gitUrl.trim() } : {}), mcp: draft.mcp.filter((row) => row.name.trim()).map(mcpToApi), env: stripSecretKeysFromRecord(kvToRec(draft.env)) }
}
export function teamHasAuth(draft: TeamWizardDraft): boolean { return !!draft.modelConfigurationId }
