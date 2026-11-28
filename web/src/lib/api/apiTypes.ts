import type {
  AcpEvent,
  Artifact,
  ArtifactAttachment,
  ClarifyImage,
  ChatExecution,
  McpCall,
  NodeRunStatus,
  ReactAnnotation,
  ReactForm,
  ReactQuestion,
  TokenUsage,
  TokenUsageByModel,
  TokenAccountingStatus,
} from '../shared/types'
import type { BackendId } from '../shared/regionPolicy'

export type CreateAgentTestPayload = {
  repos?: RepoChoice[]
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

/** GET /api/sandboxes/:id/desktop — desktop image shell and control state. */
export interface DesktopStatus {
  mode: 'desktop' | 'browser'
  shellReady: boolean
  agentActive: boolean
  holder: 'agent' | 'user'
  expiresAt?: string
  fallbackReason?: string
  apps: string[]
}

/** Built-in MCP template (GET /api/mcp-presets); the server is the only definition. */
export interface MCPPreset {
  id: 'grasp-tool' | 'browser' | 'desktop' | 'blender'
  /** Works only on the desktop sandbox image. */
  requiresDesktop: boolean
  server: MCPServer
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
export type ModelEfforts = Record<string, string>
export type ConcreteModelBinding =
  | { kind: 'platform'; credentialId: string; modelId?: string; auxiliaryModelIds?: string[]; toolSettings?: { modelRoles?: ModelRoles; routeProfile?: string; modelEfforts?: ModelEfforts } }
  | { kind: 'connection'; connectionId: string; modelId: string; auxiliaryModelIds?: string[]; toolSettings?: { modelRoles?: ModelRoles; routeProfile?: string; modelEfforts?: ModelEfforts } }
export type ModelBinding = { kind: 'configuration'; configurationId: string; configurationIds?: string[]; allConfigurations?: boolean }
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
  modelServiceTiers?: Record<string, string[]>
  state: 'active' | 'disabled'
  revision: number
  discovery?: { checkedAt: string; status: 'ok' | 'unavailable' | 'failed' }
}
export type ModelConnectionBody = Omit<ModelConnection, 'id' | 'revision' | 'discovery' | 'modelServiceTiers'>
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

/** One built-in linear identity icon. Absent means the initial-letter mark. */
export interface AgentIcon {
  iconId: string
  color: 'black' | 'red' | 'orange' | 'yellow' | 'green' | 'blue' | 'purple' | 'pink'
}

export interface Agent {
  inheritModel?: boolean
  modelBinding?: ModelBinding
  /** Missing inherits, null disables injection, and a credential ID binds explicitly. */
  credentialBindings?: Record<string, string | null>
  name: string
  /** Optional embedded role pack id (e.g. test / preflight); omit for blank. */
  templateId?: string
  acpBackend: BackendId
  files?: AgentFile[]
  mcp?: MCPServer[]
  env?: Record<string, string>
  layout?: AgentLayout
  /** Absent = the Agent cannot run workflow nodes ("Agent X 未声明能力"). */
  capabilities?: AgentCapabilities
  /** Optional linear identity icon. Omitted when the Agent uses its initial. */
  icon?: AgentIcon | null
}

/** Workspace-level shared Agent baseline (extend layer; Agent overlays on top). */
export interface SharedAgentConfig {
  acpBackend?: BackendId
  modelBinding?: ModelBinding
  files: AgentFile[]
  mcp: MCPServer[]
  env: Record<string, string>
  layout: AgentLayout
}

/** Workspace-owned credential metadata. Secret values are never returned by GET. */
export interface CredentialItem {
  purpose?: 'tool_auth' | 'model_api' | 'git' | 'ssh' | 'mcp' | 'custom'
  issuer?: string
  toolId?: string
  secretKind?: 'api_key' | 'token' | 'login_file' | 'ssh_private_key'
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
  /** Git hosting origin; HTTPS is assumed when no scheme is supplied. */
  host?: string
  envKey?: string
  /** OpenSSH known_hosts of an SSH key; public host keys, absent when none are set. */
  knownHosts?: string
  configured: boolean
  /** Masked display value (for example, a key prefix); never the plaintext secret. */
  masked?: string
  source?: string
  enabled?: boolean
  metadata?: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
  revokedAt?: string
  /** Codex ChatGPT login state kept by the platform; never carries tokens. */
  codexLogin?: CodexLoginInfo
}

/** The platform renews Codex logins; needs_relogin means the user must sign in again. */
export interface CodexLoginInfo {
  email?: string
  lastRefresh?: string
  status: 'ok' | 'needs_relogin' | 'network_error'
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
  /** Git hosting origin; HTTPS is assumed when no scheme is supplied. */
  host?: string
  envKey?: string
  /** Empty values keep an existing secret; clear/delete removes it. */
  value?: string
  /** SSH keys only. Omitted keeps the stored text; an empty string clears it. */
  knownHosts?: string
  metadata?: Record<string, unknown>
  enabled?: boolean
  clear?: boolean
}

export interface GitHubDeviceLoginStatus {
  enabled: boolean
  host?: string
  scopes?: string[]
}

export interface GitHubDeviceLoginSession {
  sessionId: string
  userCode: string
  verificationUri: string
  expiresIn: number
  interval: number
}

export interface GitHubDeviceLoginPoll {
  status: 'pending' | 'authorized' | 'denied' | 'expired' | 'error'
  interval?: number
  expiresIn?: number
  credential?: CredentialItem
  githubLogin?: string
  scopes?: string[]
  scopeWarning?: boolean
  /** Error code, e.g. alias_taken, credential_unavailable, device_flow_disabled. */
  error?: string
  alias?: string
  updated?: boolean
}

export interface CodexDeviceLoginStatus {
  enabled: boolean
}

export interface CodexDeviceLoginSession {
  sessionId: string
  userCode: string
  verificationUri: string
  expiresIn: number
  interval: number
}

export interface CodexDeviceLoginPoll {
  status: 'pending' | 'success' | 'denied' | 'expired' | 'not_enabled' | 'error'
  interval?: number
  expiresIn?: number
  credential?: CredentialItem
  email?: string
  /** Error code, e.g. alias_taken, credential_unavailable, exchange_failed. */
  error?: string
  alias?: string
  updated?: boolean
}

export type CreateSharedAgentTestPayload = {
  agentName: string
  repos?: RepoChoice[]
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
  status: 'starting' | 'running' | 'ready' | 'failed' | string
  error?: string
  prefix?: string
  agentNames?: string[]
  events: TeamBootstrapEvent[]
  resources: TeamBootstrapResource[]
  createdAt: string
  updatedAt: string
}

export interface TeamBootstrapRequest {
  prefix: string
  modelConfigurationId: string
  modelConfigurationIds?: string[]
  mcp?: MCPServer[]
  env?: Record<string, string>
}

export interface SandboxCapacity {
  used: number
  limit: number
}

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
  /** Earliest idle-reclamation deadline; active work postpones it. */
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
  endpointTransport?: 'gateway'
}

/** GET /stats/platform-status — StatusMetrics snapshot (shell chrome). */
export interface PlatformStatusMetrics {
  accounting?: TokenAccountingStatus
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
  is_admin?: boolean
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

export type CliModelTag = 'vision' | 'reasoning' | 'fast'

/** One entry of a coding CLI's shipped model shortlist. `id` goes to `--model` verbatim. */
export interface CliCatalogModel {
  id: string
  name: string
  vendor: string
  tags?: CliModelTag[]
  /** Exact saved IDs still handled by the coding-tool adapter. */
  aliases?: string[]
  /** Context window in thousands of tokens. */
  context?: number
  recommended?: boolean
  /** Reasoning levels the CLI accepts for this model; absent means none. */
  efforts?: string[]
  /** The level the coding tool runs the model at when none is chosen. */
  defaultEffort?: string
  /** Fast mode is declared by the coding-tool catalog. */
  serviceTiers?: string[]
}

export interface CliModelsResponse {
  models: CliCatalogModel[]
  /** Catalog read failed; an empty list does not confirm saved models are missing. */
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

/** A registered Git repository with its own access credential and commit author. */
export interface Project {
  id: string
  name: string
  description?: string
  repoUrl: string
  defaultBranch?: string
  repoDirName?: string
  /** Git token or SSH key from workspace credentials; empty clones without authorization. */
  credentialId?: string
  /** Commit author in this repository; empty keeps the sandbox default. */
  commitName?: string
  commitEmail?: string
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
  credentialId?: string
  commitName?: string
  commitEmail?: string
}

export type SoloStatus = 'starting' | 'running' | 'stopping' | 'stopped' | 'closed' | 'error'

export interface SoloSandboxInfo {
  id: number
  name: string
  /** pulling | creating | running | error | stopped */
  status: string
  containerStatus?: string
  connected: boolean
  hasCodeServer: boolean
  /** Live views only: a running session whose sandbox the platform paused. */
  paused?: boolean
  error?: string
  createdAt?: string
  /** "snapshot" (E2B, idle-reclaimed) or "files" (Docker/Kubernetes). */
  recoveryKind?: string
  /** Start of the idle interval; IDE and terminal traffic do not move it. */
  modelActiveAt?: string
  /** When idle reclaim saves and pauses the sandbox (live views only). */
  destroyAt?: string
  /** Open local IDE connections through the SSH gateway. */
  externalSessions?: number
}

/** GET /api/ssh-gateway. Host empty means the host name the page was opened with. */
export interface SSHGatewayInfo {
  enabled: boolean
  host?: string
  port?: number
  hostKeyFingerprint?: string
  adminsOnly?: boolean
  /** False when the gateway is limited to admins and the user is not one. */
  allowed?: boolean
  hasKey?: boolean
}

export interface UserSSHKey {
  id: number
  name: string
  publicKey: string
  fingerprint: string
  createdAt: string
  lastUsedAt?: string
}

/** One repository frozen into a Solo session at launch. */
export interface SessionRepo {
  position: number
  projectId: string
  name: string
  repoUrl: string
  repoDirName: string
  branch?: string
}

export interface SoloSession {
  id: string
  userId: string
  title: string
  agentName: string
  projectId?: string
  /** Project config frozen when a single-repo session started. */
  project?: Project
  branch?: string
  /** Opening model. Empty follows the Agent configuration's primary model. */
  initialModelId?: string
  initialConfigurationId?: string
  /**
   * Model later turns use. Empty means the primary model.
   * Absent currentModelSet means an old session still follows initialModelId.
   */
  currentModelId?: string
  currentConfigurationId?: string
  currentModelSet?: boolean
  /** Session reasoning effort; empty follows the coding tool's default. */
  reasoningEffort?: string
  /** Exact supported levels for the current model. Empty/absent means unsupported. */
  reasoningEfforts?: string[]
  /** The listed level the coding tool itself runs the model at, when known. */
  defaultReasoningEffort?: string
  serviceTier?: string
  serviceTiers?: string[]
  /** Stable code set when a model switch is rolled back. */
  modelNotice?: string
  /** Ordered repositories frozen at launch. Empty means an empty workspace. */
  repos?: SessionRepo[]
  sandboxId?: number
  status: SoloStatus
  error?: string
  /** The environment skips automatic idle reclaim. */
  keepAlive?: boolean
  /** Docker/Kubernetes environments are never idle-reclaimed; keep-alive does not apply. */
  idleReclaimExempt?: boolean
  restarts: number
  /** Newest successful recovery point (archive bytes when available). */
  backupSize?: number
  backupAt?: string
  lastActiveAt?: string
  closedAt?: string
  createdAt: string
  updatedAt: string
  busy: boolean
  waiting: number
  steerable?: boolean
  sandbox?: SoloSandboxInfo
  backend?: BackendId | string
  skills?: string[]
  model?: string
  artifactRunId?: string
  capabilities?: AgentCapabilities
}

export interface SoloResources {
  runId: string
  artifacts: Artifact[]
  previewArtifact: string
  /** The active preview, null when none is registered. */
  preview: PreviewPort | null
}

export interface SoloTaskInfo {
  usage: TokenUsage & {
    total: number
    reported: boolean
    incomplete: boolean
    byModel?: TokenUsageByModel
    cost?: number
    currency?: string
    unpricedTotal?: number
  }
  workflowRuns: Array<{ id: string; workflowName: string }>
}

export interface SoloSessionList {
  sessions: SoloSession[]
  maxSandboxes: number
  /** Global sandbox slots in use across all backends and purposes. */
  usedSandboxes?: number
}

/** A registered repository picked for a Solo session, workflow binding or run. */
export interface RepoChoice {
  projectId: string
  /** Empty uses the repository's default branch. */
  branch?: string
}

export interface SoloCreateBody {
  agentName: string
  projectId?: string
  branch?: string
  /** Ordered selection. An empty list starts an empty workspace. */
  repos?: RepoChoice[]
  /** Empty follows the Agent configuration's primary model. */
  initialModelId?: string
  initialConfigurationId?: string
  reasoningEffort?: string
  serviceTier?: string
  title?: string
  text?: string
  images?: ClarifyImage[]
  references?: ReferenceInput[]
}

export type ReferenceKind = 'solo' | 'github_issue' | 'github_pr' | 'github_discussion' | 'artifact'

/** What the composer submits; the server resolves everything else. */
export interface ReferenceInput {
  kind: ReferenceKind
  target: string
}

/** A reference as stored on a user message (or shown on a queued/local one). */
export interface MessageReference extends ReferenceInput {
  id?: string
  title: string
  url?: string
  anchor?: string
  messages?: number
  state?: string
  access?: 'ok' | 'denied' | 'unchecked'
  error?: string
}

export interface SoloReferenceCandidate {
  kind: 'solo'
  target: string
  title: string
  status: string
  lastActiveAt?: string
}

export interface GitHubReferenceCandidate {
  kind: 'github_issue' | 'github_pr' | 'github_discussion'
  target: string
  title: string
  state: string
  url: string
  comments: number
  updatedAt: string
  repo: string
  number: number
}

export interface ArtifactReferenceCandidate {
  kind: 'artifact'
  target: string
  title: string
  fileKind: Artifact['kind']
  source?: string
}

export type ReferenceSearchLane = 'solo' | 'github' | 'discussion' | 'artifact'

export interface ReferenceSearchResult<T> {
  lane: ReferenceSearchLane
  items: T[]
  total?: number
  nextPage?: number
  incomplete?: boolean
  source?: string
  hint?: string
  rateLimit?: { remaining: number; resetAt: string }
  /** GitHub only: matches per kind for the repos, state and query, whatever kind was asked. */
  counts?: { issue?: number; pr?: number }
  error?: { code: string; message: string; retryAfterMs?: number }
}

/**
 * How a steered queue item was handled: joined the running turn; runs next
 * (a turn that cannot take input at all was interrupted); or queued, waiting
 * for a running turn that handed it back to finish.
 */
export type SteerOutcome = 'joined' | 'next' | 'queued'

/**
 * A queue item's steer state from the server: being offered to the running
 * turn, or handed back and waiting for that turn to finish.
 */
export type QueueSteerMark = 'pending' | 'queued'

export function queueSteerMark(raw: unknown): QueueSteerMark | undefined {
  return raw === 'pending' || raw === 'queued' ? raw : undefined
}

export interface SoloMessage {
  files?: ArtifactAttachment[]
  turnId?: string
  id: string
  sessionId: string
  role: 'user' | 'assistant' | 'system'
  content: string
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
  references?: MessageReference[]
  /** Raw ACP frames of an assistant turn, replayed into the timeline. */
  events?: unknown[]
  /** Assistant turn: raised through ask_question / ask_form, answered by the next user message. */
  questions?: ReactQuestion[]
  forms?: ReactForm[]
  status?: '' | 'ok' | 'failed' | 'stopped' | 'context_new' | 'context_compact'
  error?: string
  usage?: TokenUsage
  usageByModel?: TokenUsageByModel
  durationMs?: number
  execution?: ChatExecution
  createdAt: string
}
