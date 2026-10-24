import type {
  AcpEvent,
  ClarifyImage,
  McpCall,
  NodeRunStatus,
  TokenUsage,
  TokenUsageByModel,
} from '../shared/types'
import type { BackendId } from '../shared/regionPolicy'

export type AgentTestRepo = { name: string; url: string; branch?: string }

export type CreateAgentTestPayload = {
  repos?: AgentTestRepo[]
  repoUrl?: string
}

export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  hasMore: boolean
}

export interface PreviewPort {
  runId: string
  nodeId: string
  kind: 'port' | 'url' | string
  port: number
  /** External absolute URL when kind=url. */
  url?: string
  label?: string
  proxyUrl: string
  healthy: boolean
  registeredAt?: string
  /** "direct" when node switch direct_preview is on. */
  mode?: 'direct' | 'vnc' | string
  /** Browser-facing http://IP:port/ when mode=direct. */
  directUrl?: string
}

export interface PreviewIssue {
  id: string
  runId: string
  nodeId: string
  body: string
  selector?: string
  port?: number
  images?: ClarifyImage[]
  status: string
  createdAt: string
}

export interface EventPaginatedResponse {
  events: AcpEvent[]
  nextCursor: string
  hasMore: boolean
  live?: boolean
  /** Live sandbox registered but bridge read failed transiently. */
  unavailable?: boolean
  error?: string
}

export interface NodeEventsResponse {
  events: AcpEvent[]
  live: boolean
  /** Agent node's task preview (same helper as currentNode.task); absent on paging requests. */
  task?: string
  unavailable?: boolean
  error?: string
}

/** One node execution as served by GET /runs/:id/llm-transcript. */
export interface LlmTranscriptExecution {
  id: number
  nodeId: string
  nodeType?: string
  iteration: number
  status: NodeRunStatus
  startedAt?: string
  durationSec?: number
  events?: AcpEvent[] | null
  mcpCalls?: McpCall[] | null
  usage?: TokenUsage | null
  usageByModel?: TokenUsageByModel | null
  error?: string
}

export interface LlmInflightPrompt {
  prompt: string
  imageCount?: number
  at: string
}

export interface LlmTranscriptResponse {
  /** Oldest first (by startedAt). */
  executions: LlmTranscriptExecution[]
  /** Prompt of the turn currently streaming, keyed by node id. */
  inflight?: Record<string, LlmInflightPrompt>
}

export interface MCPServer {
  name: string
  url?: string
  headers?: Record<string, string>
  command?: string
  args?: string[]
  env?: Record<string, string>
}

export interface AgentFile {
  path: string
  content: string
}

export interface WorkspaceRevisionChange {
  path: string
  op: string
  fromPath?: string
}

export interface WorkspaceRevision {
  sha: string
  parentSha?: string
  createdAt?: string
  author: string
  source: string
  reason: string
  changes?: WorkspaceRevisionChange[]
}

export interface AgentLayout {
  configRoot?: string
  workspaceDir?: string
}

export type AgentInteraction = 'auto' | 'clarify'

/** Platform tools an Agent may be granted (set_* product tools follow from writes). */
export type GrantableTool = 'ask_question' | 'ask_form' | 'set_artifact_preview' | 'set_preview' | 'update_plan_status'

/** One structured product an Agent writes (schema name from the node manifest). */
export interface ProductWrite {
  schema: string
  required?: boolean
}

/** What an Agent may do and must deliver when a workflow node runs it (agent.json `capabilities`). */
export interface AgentCapabilities {
  interaction: AgentInteraction
  /** auto only: park for human review after the run. */
  review?: boolean
  tools?: string[]
  /** Readable artifact names; "*" reads all. */
  reads?: string[]
  writes?: ProductWrite[]
  maxRounds?: number
}

/** Built-in workflow Agent template (GET /agent-teams/templates). */
export interface AgentTemplate {
  id: string
  embedName: string
  roleLabelZh: string
  summary: string
  capabilities?: AgentCapabilities
}

export type ModelProtocol = 'openai-chat-completions' | 'openai-responses' | 'anthropic-messages'
export type ModelRoles = Partial<Record<'main' | 'fast' | 'reasoning' | 'subagent' | 'opus' | 'sonnet' | 'haiku', string>>
export type ConcreteModelBinding =
  | { kind: 'platform'; credentialId: string; modelId?: string; toolSettings?: { modelRoles?: ModelRoles; routeProfile?: string } }
  | { kind: 'connection'; connectionId: string; modelId: string; toolSettings?: { modelRoles?: ModelRoles; routeProfile?: string } }
export type ModelBinding = { kind: 'inherit' } | { kind: 'configuration'; configurationId: string } | ConcreteModelBinding
export interface ModelConfiguration {
  id: string
  name: string
  backend: string
  binding: ConcreteModelBinding
  state: 'active' | 'disabled'
  revision: number
  createdAt?: string
  updatedAt?: string
}
export type ModelConfigurationBody = Pick<ModelConfiguration, 'name' | 'backend' | 'binding'> & { state?: ModelConfiguration['state'] }
export interface ModelConnection {
  id: string
  name: string
  providerId: string
  protocol: ModelProtocol
  baseUrl: string
  credentialId: string
  auth: { scheme: 'bearer' | 'x-api-key' | 'custom-header'; headerName?: string }
  service?: { region?: string; product?: string }
  models: { id: string; name?: string; source: 'manual' | 'discovered'; capabilities?: { toolCalls?: boolean; vision?: boolean; reasoning?: boolean }; input?: ('text' | 'image')[]; contextWindow?: number; maxTokens?: number; reasoningEfforts?: string[] }[]
  state: 'active' | 'disabled'
  revision: number
  discovery?: { checkedAt: string; status: 'ok' | 'unavailable' | 'failed' }
}
export type ModelConnectionBody = Omit<ModelConnection, 'id' | 'revision' | 'discovery'>
export interface ToolCapability {
  toolId: string
  versionConstraint?: string
  transport?: string
  modes: ('platform' | 'connection')[]
  protocols?: ModelProtocol[]
  roleNames?: string[]
  requiredRoles?: string[]
  routeProfiles?: string[]
  adapterState: 'existing' | 'planned' | 'unsupported'
  verification?: string
  evidence?: { url: string; scope?: string; checkedAt?: string; kind?: string }[]
}
export interface WorkspaceModelDefaults { bindingsByBackend: Record<string, Exclude<ModelBinding, { kind: 'inherit' }>> }

export interface AgentGitBinding {
  credentialId: string
  /** Host or repository URL; empty uses the credential provider's default. */
  scope?: string
  sshHostsCredentialId?: string
}

export interface AgentGitBindings {
  items: AgentGitBinding[]
  /** Required for a nonempty list and must refer to one of its items. */
  preferredCredentialId?: string
}

export interface Agent {
  /** Missing preserves legacy Git authorization; an empty list explicitly disables it. */
  gitBindings?: AgentGitBindings
  modelBinding?: ModelBinding
  /** Missing inherits, null disables injection, and a credential ID binds explicitly. */
  credentialBindings?: Record<string, string | null>
  name: string
  /** Optional embedded role pack id (e.g. test / preflight); omit for blank. */
  templateId?: string
  acpBackend: BackendId
  /** Workspace credential selected for OpenCode. The secret is not stored here. */
  openCodeCredentialId?: string
  /** Workspace credential selected for a non-OpenCode coding backend. */
  aiCredentialId?: string
  /** GitHub, GitLab, or SSH private-key credential selected for the Git method. */
  gitCredentialId?: string
  /** Known-hosts credential selected when Git uses SSH. */
  sshHostsCredentialId?: string
  gitCredentialType?: 'github_https' | 'gitlab_https' | 'ssh'
  /** Repositories whose own Git credentials are injected. Missing inherits the workspace default; [] injects none. */
  repoIds?: string[]
  files?: AgentFile[]
  mcp?: MCPServer[]
  env?: Record<string, string>
  layout?: AgentLayout
  /** Absent = the Agent cannot run workflow nodes ("Agent X 未声明能力"). */
  capabilities?: AgentCapabilities
}

/** Workspace-level shared Agent baseline (extend layer; Agent overlays on top). */
export interface SharedAgentConfig {
  gitBindings?: AgentGitBindings
  modelBinding?: ModelBinding
  acpBackend: BackendId
  gitCredentialType?: string
  /** Workspace credential id for a non-OpenCode generic authorization. Empty means unset. */
  aiCredentialId?: string
  /** Workspace credential id for the OpenCode generic authorization. Empty means unset. */
  openCodeCredentialId?: string
  /** Repositories whose own Git credentials every Agent inherits unless it overrides. */
  repoIds?: string[]
  files: AgentFile[]
  mcp: MCPServer[]
  env: Record<string, string>
  layout: AgentLayout
}

/** Workspace-owned credential metadata. Secret values are never returned by GET. */
export interface CredentialItem {
  purpose?: 'tool_auth' | 'model_api' | 'git' | 'ssh'
  issuer?: string
  toolId?: string
  secretKind?: 'api_key' | 'token' | 'login_file' | 'ssh_private_key' | 'ssh_known_hosts'
  secretRevision?: number
  hasSecret?: boolean
  state?: 'active' | 'cleared' | 'revoked'
  id: string
  /** Credential type (ai, git, ssh, mcp, custom, …). */
  type: string
  name: string
  target?: string
  targetType?: string
  targetId?: string
  provider?: string
  envKey?: string
  configured: boolean
  /** Masked display value (for example, a key prefix); never the plaintext secret. */
  masked?: string
  source?: string
  enabled?: boolean
  metadata?: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
  revokedAt?: string
}

export interface CredentialsResponse {
  items: CredentialItem[]
}

export interface CredentialPutBody {
  purpose?: CredentialItem['purpose']
  issuer?: string
  toolId?: string
  secretKind?: CredentialItem['secretKind']
  type?: string
  name?: string
  target?: string
  targetType?: string
  targetId?: string
  provider?: string
  envKey?: string
  /** Empty values keep an existing secret; clear/delete removes it. */
  value?: string
  metadata?: Record<string, unknown>
  enabled?: boolean
  clear?: boolean
}

export type CreateSharedAgentTestPayload = {
  agentName: string
  repos?: AgentTestRepo[]
  repoUrl?: string
}

/** Result of POST /workspace/agents/import. */
export interface WorkspaceAgentsImportResult {
  created?: string[]
  overwritten?: string[]
  renamed?: Record<string, string>
}

/** Create Agent Team bootstrap progress (GET/POST /agent-teams/bootstrap). */
export interface TeamBootstrapEvent {
  kind: string
  message: string
  at: string
}

export interface TeamBootstrapResource {
  kind: 'workspace' | 'agent'
  name: string
  detail?: string
}

export interface TeamBootstrapSession {
  id: string
  status: 'starting' | 'running' | 'pulling' | 'ready' | 'failed' | string
  error?: string
  pmAgent?: string
  sandboxId?: string
  /** Gateway/local sandbox lifecycle while bootstrap waits (pulling|creating|running|…). */
  sandboxStatus?: string
  prefix?: string
  background?: string
  agentNames?: string[]
  events: TeamBootstrapEvent[]
  resources: TeamBootstrapResource[]
  createdAt: string
  updatedAt: string
}

export interface TeamBootstrapRequest {
  prefix: string
  pmName: string
  background: string
  acpBackend: BackendId
  apiKey?: string
  customConfig?: string
  region?: string
  gitUrl?: string
  gitCredentialType?: string
  mcp?: MCPServer[]
  env?: Record<string, string>
}

export type SandboxLeaseKind = 'debug' | 'activity' | 'preview' | 'manual'

/** One reason a run sandbox is kept; no `until` = until the run ends (then the run TTL). */
export interface SandboxLease {
  kind: SandboxLeaseKind | string
  until?: string
  reason?: string
  by?: string
  createdAt: string
}

export type SandboxLeaseExtend = { minutes: number } | { until: 'run_end' }

export interface SandboxView {
  id: number
  name: string
  profile: string
  purpose: string
  status: string
  error?: string
  repoUrl?: string
  runId?: string
  workflowId?: string
  workflowName?: string
  nodeId?: string
  /** Why a retired run sandbox is still alive; destroyAt is derived from these. */
  leases?: SandboxLease[]
  destroyAt?: string
  createdAt: string
  updatedAt: string
  containerStatus: string
  busy: boolean
  connected: boolean
  hasCodeServer: boolean
  hasAcp: boolean
  /** Same secret as container ACP_BRIDGE_PASSWORD for direct host:port login. */
  password?: string
  /** Gateway host:port map; only present on getSandbox (GetView), not list. */
  endpoints?: Record<string, string>
}

export interface DashboardStats {
  running: number
  waitingHuman: number
  failed: number
  completed: number
  workflows: number
  artifacts: number
  /** Platform-wide cumulative tokens; null = never reported (UI "—"). */
  totalTokens?: number | null
  workflowTokens?: number | null
  pmTokens?: number | null
}

/** GET /stats/platform-status — StatusMetrics snapshot (shell chrome). */
export interface PlatformStatusMetrics {
  /** Platform cumulative tokens; null = never reported (UI "—"). */
  cumulativeTokens: number | null
  /** Client-timezone calendar-day token sum; null when unavailable. */
  todayTokens: number | null
  runningCount: number
  queuedCount: number
  asOf: string
  timezone?: string
}

// SettingItem is one platform scheduling knob: its effective value, where it
// came from (env|db|config) and whether it's pinned by an env var (read-only).
export interface SettingItem {
  key: string
  label: string
  unit?: string
  value: number
  min: number
  // max is the ceiling, absent when the knob has none.
  max?: number
  source: 'env' | 'db' | 'config'
  locked: boolean
}

export interface BrandSettings {
  product_name: string
  home_subtitle: string
}

export interface ChannelConfig {
  id: string
  type: string
  name: string
  enabled: boolean
  agentName: string
  isPrimary: boolean
  enabledMcps: string[]
  appId: string
  appSecretSet: boolean
  turnTimeoutSeconds: number
  cronDeliver: boolean
  cronDeliverTarget?: string
  config?: Record<string, unknown>
  /** Long-connection subscribe success (computed). */
  online?: boolean
  createdAt: string
  updatedAt: string
  connectionState?: string
  connectionDetail?: string
}

// Channel create/update payload.
export interface ChannelConfigInput {
  type: 'qq' | 'wecom' | 'feishu' | 'dingtalk'
  name: string
  enabled: boolean
  agentName: string
  isPrimary: boolean
  enabledMcps?: string[]
  appId: string
  appSecret?: string
  turnTimeoutSeconds: number
  cronDeliver: boolean
  cronDeliverTarget?: string
  config?: Record<string, unknown>
  /** Confirm syncing Workspace.PmLeaderAgent when rebinding primary. */
  syncPmLeader?: boolean
}

export interface NotifyDeliveryReceipt {
  id?: number
  runId: string
  nodeId: string
  iteration: number
  kind: string
  status?: string
  error?: string
  createdAt: string
}

export interface ChannelDeleteOpts {
  newPrimaryId?: string
  confirmNoPrimary?: boolean
  syncPmLeader?: boolean
}

export type HealthResponse = {
  status: string
  ready: boolean
  vnc_preview?: boolean
  /** Optional 7-char (or longer) service-program SHA; omitted when unavailable. */
  commit?: string
}

export interface AuthMeResponse {
  username: string
  expires_at: string
  is_admin?: boolean
}

export interface AuthLoginResponse {
  username: string
  expires_at: string
  redirect?: string
}

/** One inbox row from GET /api/notifications. Unread is computed on the server. */
export interface NotificationListItem {
  runId: string
  status: 'completed' | 'failed' | string
  title: string
  titleNeutral: boolean
  workflowName: string
  startedAt: string
  finishedApprox: string
  unread: boolean
  beforeBaseline: boolean
}

/** One vendor of the OpenCode model catalog (models.dev). */
export interface OpenCodeCatalogProvider {
  id: string
  name?: string
  /** Vendor default base URL, when the catalog publishes one. */
  api?: string
  /** Environment variable OpenCode reads this vendor's key from. */
  keyEnv?: string
  /** How many models the catalog lists for this vendor. */
  models: number
}

export interface OpenCodeCatalogModel {
  id: string
  name?: string
}

export type CliModelTag = 'vision' | 'reasoning' | 'fast'

/** One entry of a coding CLI's shipped model shortlist. `id` goes to `--model` verbatim. */
export interface CliCatalogModel {
  id: string
  name: string
  vendor: string
  tags?: CliModelTag[]
  /** Context window in thousands of tokens. */
  context?: number
  recommended?: boolean
  /** Reasoning levels the CLI accepts for this model; absent means none. */
  efforts?: string[]
}

export interface CliModelsResponse {
  models: CliCatalogModel[]
}

export interface OpenCodeProvidersResponse {
  providers: OpenCodeCatalogProvider[]
  fetchedAt?: string
  /** Set when the catalog could not be refreshed; the picker then falls back. */
  error?: string
}

export interface OpenCodeModelsResponse {
  models: OpenCodeCatalogModel[]
  error?: string
}

export interface NotificationListResponse {
  items: NotificationListItem[]
  page?: number
  pageSize?: number
  total?: number
  allCount?: number
  unreadCount?: number
  readCount?: number
}

/** Lightweight metadata for the workspace workflow directory. Load graph details when opening or running. */
export interface WorkflowCatalogItem {
  id: string
  name: string
  description: string
  status: 'draft' | 'published'
  version: number
  publishedVersion?: number
  updatedAt: string
  lastRunAt?: string
  showOnHome?: boolean
  needsRepo?: boolean
}

export type GitCredentialType = '' | 'github_https' | 'gitlab_https' | 'ssh'

/** A Git repository registered for Solo sessions. Env never holds secrets. */
export interface Project {
  id: string
  name: string
  description?: string
  repoUrl: string
  defaultBranch?: string
  repoDirName?: string
  /** This repository's own access method. Empty means none configured. */
  gitCredentialType?: GitCredentialType
  gitCredentialId?: string
  sshHostsCredentialId?: string
  env?: Record<string, string>
  credentialBindings?: Record<string, string | null>
  createdBy?: string
  lastUsedAt?: string
  createdAt: string
  updatedAt: string
}

export interface ProjectInput {
  name: string
  description?: string
  repoUrl: string
  defaultBranch?: string
  repoDirName?: string
  gitCredentialType?: GitCredentialType
  gitCredentialId?: string
  sshHostsCredentialId?: string
  env?: Record<string, string>
  credentialBindings?: Record<string, string | null>
}

export type SoloStatus = 'starting' | 'running' | 'closed' | 'error'

export interface SoloSandboxInfo {
  id: number
  name: string
  /** pulling | creating | running | error | stopped */
  status: string
  containerStatus?: string
  connected: boolean
  hasCodeServer: boolean
  error?: string
  createdAt?: string
}

export interface SoloSession {
  id: string
  userId: string
  title: string
  agentName: string
  projectId?: string
  /** Project config frozen when the session started. */
  project?: Project
  branch?: string
  sandboxId?: number
  status: SoloStatus
  error?: string
  restarts: number
  lastActiveAt?: string
  closedAt?: string
  createdAt: string
  updatedAt: string
  busy: boolean
  waiting: number
  sandbox?: SoloSandboxInfo
  backend?: BackendId | string
  model?: string
  artifactRunId?: string
  capabilities?: AgentCapabilities
}

export interface SoloSessionList {
  sessions: SoloSession[]
  maxSandboxes: number
}

export interface SoloCreateBody {
  agentName: string
  projectId?: string
  branch?: string
  title?: string
  text?: string
  images?: ClarifyImage[]
}

export interface SoloMessage {
  id: string
  sessionId: string
  role: 'user' | 'assistant' | 'system'
  content: string
  images?: ClarifyImage[]
  /** Raw ACP frames of an assistant turn, replayed into the timeline. */
  events?: unknown[]
  status?: '' | 'ok' | 'failed' | 'stopped'
  error?: string
  usage?: TokenUsage
  usageByModel?: TokenUsageByModel
  durationMs?: number
  createdAt: string
}

export type SoloEnvSource = 'platform' | 'agent' | 'project' | 'credential' | 'system'

export interface SoloEnvEntry {
  key: string
  source: SoloEnvSource
}
