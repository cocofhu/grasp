import type { AcpEvent, ClarifyImage } from '../shared/types'

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
  /** port | url; legacy rows may omit kind when only port is set. */
  kind?: 'port' | 'url' | string
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

// AgentPrompts overrides the platform-injected prompt text and sandbox rule
// files for one Agent. Every field is optional; an empty field falls back to
// the platform default. The templated fields use a `{name}` placeholder for the
// declared produces file name.
export interface AgentPrompts {
  upstreamArtifactsHeader?: string
  producesContract?: string
  reactOpenSuffix?: string
  producesRetry?: string
}

export interface Agent {
  name: string
  /** Single home project; empty/undefined = unbound (artifact-store only). */
  projectId?: string
  /** Optional embedded role pack id (e.g. test / preflight); omit for blank. */
  templateId?: string
  acpBackend?: 'cursor' | 'claude_code' | 'codebuddy' | 'trae' | 'opencode'
  gitCredentialType?: 'github_https' | 'gitlab_https' | 'ssh'
  /** known_hosts literal (may contain newlines); no ${vars.*}. */
  gitSshKnownHosts?: string
  /** SSH private key literal; masked in UI; no ${vars.*}. */
  gitSshPrivateKey?: string
  files?: AgentFile[]
  mcp?: MCPServer[]
  env?: Record<string, string>
  layout?: AgentLayout
  prompts?: AgentPrompts
}

/** Project-level shared Agent baseline (extend layer; Agent overlays on top). */
export interface ProjectSharedAgentConfig {
  projectId: string
  acpBackend?: string
  defaultProjectId?: string
  gitCredentialType?: string
  gitSshKnownHosts?: string
  gitSshPrivateKey?: string
  files: AgentFile[]
  mcp: MCPServer[]
  env: Record<string, string>
  layout: AgentLayout
  prompts?: AgentPrompts
}

export type CreateProjectSharedAgentTestPayload = {
  agentName: string
  repos?: AgentTestRepo[]
  repoUrl?: string
}

/** Virtual group in the Agent Studio organization tree (not a disk directory). */
export interface OrgGroup {
  id: string
  name: string
  parentGroupId?: string
}

/** Per-agent organization membership (orthogonal to agent_profile identity). */
export interface OrgAgentMembership {
  groupIds?: string[]
}

/** Central organization index (GET/PUT /agents/org). */
export interface AgentOrg {
  revision: number
  groups: OrgGroup[]
  agents: Record<string, OrgAgentMembership>
}

/** Result of POST /agents/org/import. */
export interface OrgFolderImportResult {
  org: AgentOrg
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
  rootGroupId?: string
  pipelineGroupId?: string
  pmAgent?: string
  sandboxId?: string
  /** Gateway/local sandbox lifecycle while bootstrap waits (pulling|creating|running|…). */
  sandboxStatus?: string
  prefix?: string
  background?: string
  allowedGroupIds?: string[]
  agentNames?: string[]
  events: TeamBootstrapEvent[]
  resources: TeamBootstrapResource[]
  createdAt: string
  updatedAt: string
}

export interface TeamBootstrapRequest {
  projectName: string
  prefix: string
  rootGroupName: string
  pipelineGroupName: string
  pmName: string
  background: string
  acpBackend: string
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
  /** Same secret as container PASSWORD / CURSOR_ACP_PASSWORD for direct host:port login. */
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

export type PlatformRuleSource = 'override' | 'global' | 'embed'

export interface PlatformRuleMeta {
  file: string
  source: PlatformRuleSource
  mtime?: string
}

export interface PlatformRuleContent extends PlatformRuleMeta {
  content: string
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
// Empty type still defaults to "qq" server-side.
export interface ChannelConfigInput {
  type?: string
  name: string
  enabled: boolean
  agentName: string
  isPrimary?: boolean
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
