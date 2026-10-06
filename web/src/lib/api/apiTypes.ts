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
  projectId?: string
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

export interface Agent {
  name: string
  /** Home project (required). */
  projectId: string
  /** Optional embedded role pack id (e.g. test / preflight); omit for blank. */
  templateId?: string
  acpBackend: BackendId
  gitCredentialType?: 'github_https' | 'gitlab_https' | 'ssh'
  files?: AgentFile[]
  mcp?: MCPServer[]
  env?: Record<string, string>
  layout?: AgentLayout
  /** Absent = the Agent cannot run workflow nodes ("Agent X 未声明能力"). */
  capabilities?: AgentCapabilities
}

/** Project-level shared Agent baseline (extend layer; Agent overlays on top). */
export interface ProjectSharedAgentConfig {
  projectId: string
  acpBackend: BackendId
  gitCredentialType?: string
  files: AgentFile[]
  mcp: MCPServer[]
  env: Record<string, string>
  layout: AgentLayout
}

/** Project-owned credential metadata. Secret values are never returned by GET. */
export interface ProjectCredentialItem {
  id: string
  projectId?: string
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

export interface ProjectCredentialsResponse {
  items: ProjectCredentialItem[]
}

export interface ProjectCredentialPutBody {
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

export type CreateProjectSharedAgentTestPayload = {
  agentName: string
  repos?: AgentTestRepo[]
  repoUrl?: string
}

/** Result of POST /projects/:id/agents/import. */
export interface ProjectAgentsImportResult {
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
  kind: string
  name: string
  detail?: string
}

export interface TeamBootstrapSession {
  id: string
  status: 'starting' | 'running' | 'pulling' | 'ready' | 'failed' | string
  error?: string
  projectId?: string
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
  projectName: string
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
  projectId: string
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

// Channel create/update payload. projectId is implied by the request path.
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
  /** Confirm syncing Project.PmLeaderAgent when rebinding primary. */
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
