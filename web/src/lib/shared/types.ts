export type NodeType =
  | 'input'
  | 'output'
  | 'react'
  | 'grasp'
  | 'approve' // historical alias of grasp
  | 'preflight'
  | 'agent'
  | 'plan'
  | 'implement'
  | 'research'
  | 'test'
  | 'review'
  | 'proposal'
  | 'proposal_select'
  | 'submit_mr'
  | 'visual'
  | 'human_gate'
  | 'app_preview'
  | 'branch'
  | 'set_var'

export type NodeRunStatus =
  | 'pending'
  | 'running'
  | 'waiting_human'
  | 'completed'
  | 'failed'
  | 'skipped'
  | 'cancelled'

export interface WFNode {
  id: string
  type: NodeType
  label: string
  position: { x: number; y: number }
  config: Record<string, any>
  // FSM: 标记为可回滚检查点(失败可恢复到此处的变量快照重跑)
  checkpoint?: boolean
}

// FSM 转移类型:正常成功路由 / 失败转移 / 回滚到 checkpoint
export type EdgeKind = 'success' | 'failure' | 'rollback'

export interface WFEdge {
  id: string
  source: string
  target: string
  when?: string
  label?: string
  // FSM 转移语义(缺省按 success 处理,兼容旧数据)
  kind?: EdgeKind
  // rollback/failure 时携带回上游的变量/错误(如 last_error)
  carry?: string[]
  // rollback 重试上限,超限走 else 终止
  maxAttempts?: number
}

export interface Workflow {
  id: string
  projectId?: string
  name: string
  description: string
  /** Client-only display name resolved from Project.id (home cards/select). */
  projectName?: string
  status: 'draft' | 'published'
  version: number
  updatedAt: string
  lastRunAt?: string
  needsRepo: boolean
  /** Home cards + Home search; missing/false = hidden (plan g1.1). */
  showOnHome?: boolean
  notifyPolicy?: WorkflowNotifyPolicy
  nodes: WFNode[]
  edges: WFEdge[]
}

/** Project-level Run→IM notification defaults. */
export interface ProjectNotifyPolicy {
  enabled?: boolean
  defaultEvents?: string[]
  /** Explicit 0~N channel fan-out targets (may include primary). Empty = no deliver. */
  channelIds?: string[]
  /** Full QQ body for waiting_human; trim-empty → legacy FormatRunNotifyMessage. */
  waitingHumanTemplate?: string
  /** Full QQ body for failed; trim-empty → legacy FormatRunNotifyMessage. */
  failedTemplate?: string
  /** Full QQ body for completed; trim-empty → FormatRunNotifyMessage. Opt-in. */
  completedTemplate?: string
}

/** Workflow-level notify override: off | inherit | custom. */
export interface WorkflowNotifyPolicy {
  mode?: 'off' | 'inherit' | 'custom' | string
  events?: string[]
}

/** One project sandbox OS environment variable (secret values masked as **** on read). */
export interface ProjectEnvEntry {
  key: string
  value: string
  secret?: boolean
  /** When false, skipped at injection; missing/undefined means enabled (legacy compat). */
  enabled?: boolean
}

/** Project-level workflow variable (vars.*); secret values masked on read. */
export interface ProjectVariable {
  name: string
  type: string
  value?: any
  desc?: string
  ask?: boolean
  required?: boolean
  editable?: boolean
  options?: string
  secret?: boolean
}

export interface Project {
  id: string
  name: string
  description: string
  /**
   * @deprecated Removed from Project API — use project shared Agent config env instead.
   * Kept optional only for transitional mocks; do not read/write in UI.
   */
  sandboxEnv?: ProjectEnvEntry[]
  variables: ProjectVariable[]
  workflowCount?: number
  /**
   * Sum of workflow StateRun.Usage + post-feature PM ChatMessage.Usage.
   * null/undefined = no reported usage (UI "—"); 0 = reported usage totaling zero.
   */
  totalTokens?: number | null
  /** Workflow-only portion of totalTokens (null = no reported workflow usage). */
  workflowTokens?: number | null
  /** PM-only portion of totalTokens (null = no reported PM usage). */
  pmTokens?: number | null
  pmLeaderEnabled?: boolean
  pmLeaderAgent?: string
  /**
   * Optional display alias for the 「未知/未分桶」token bucket.
   * Empty/omitted → show the default label. Does not change persisted keys.
   */
  unknownModelDisplayName?: string
  notifyPolicy?: ProjectNotifyPolicy
  createdAt?: string
  updatedAt?: string
}

/** One project-scoped append-only audit event (masked payload). */
export interface ProjectAuditEvent {
  id: string
  projectId: string
  occurredAt: string
  actor: string
  unattributable: boolean
  /** Product attribution class: pm | apikey | system */
  callerKind?: string
  action: string
  resourceType: string
  resourceId: string
  resource?: string
  runId?: string
  nodeId?: string
  outcome: string
  summary: string
  payload?: Record<string, unknown>
}

export interface ProjectAuditFacetRun {
  runId: string
  label: string
  sub?: string
}

export interface ProjectAuditFacetNode {
  nodeId: string
  label: string
}

export interface ProjectAuditFacetResource {
  resourceType: string
  resourceId: string
  resource: string
}

export interface ProjectAuditFacets {
  runs: ProjectAuditFacetRun[]
  nodes: ProjectAuditFacetNode[]
  resources: ProjectAuditFacetResource[]
  actors?: string[]
}

export interface ProjectAuditStats {
  total: number
  mcp: number
  fail: number
}

/** Preset windows for board TokenStatsPanel (matches GET /token-stats). */
export type TokenStatsWindow = '24h' | '7d' | '30d' | '90d' | 'all'

export interface TokenStatsBucket {
  bucket: string
  total: number
  /** Workflow portion of the bucket (for stacked trend). */
  workflowTotal?: number
  /** PM portion of the bucket (for stacked trend). */
  pmTotal?: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
}

export interface TokenStatsComposition {
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  total: number
}

export type TokenStatsRankKind = 'workflow' | 'pm' | 'other'

export interface TokenStatsWorkflow {
  workflowId?: string
  name: string
  total: number
  inputTokens?: number
  outputTokens?: number
  cacheReadTokens?: number
  cacheWriteTokens?: number
  other?: boolean
  /** Rank row kind: workflow | pm | other (other = non-top workflows only). */
  kind?: TokenStatsRankKind
}

/** One model bucket in project TokenStats (composition / ranking). */
export interface TokenStatsModel {
  modelKey?: string
  name: string
  total: number
  inputTokens?: number
  outputTokens?: number
  cacheReadTokens?: number
  cacheWriteTokens?: number
  /** 「未知/未分桶」. Shown in ranking only when it ranks in Top10; otherwise its usage is folded into other. */
  unknown?: boolean
  /** Top10 remainder (may include unknown usage that did not qualify). other is not unknown. */
  other?: boolean
  /** Includes ACP_BRIDGE_MODEL weak-key backfill. */
  filled?: boolean
  /** upstream | via ACP_BRIDGE_MODEL | unknown */
  source?: string
}

/** Response of GET /projects/:id/token-stats */
export interface ProjectTokenStats {
  window: TokenStatsWindow | string
  bucketWidth: 'hour' | 'day' | 'week' | string
  timezone: string
  /** true when no reported Usage in the window — do not draw forged zero charts */
  empty: boolean
  trend: TokenStatsBucket[]
  composition: TokenStatsComposition
  workflows: TokenStatsWorkflow[]
  modelComposition?: TokenStatsModel[]
  modelRanking?: TokenStatsModel[]
}

/** Response of GET /stats/token — global analytics page. */
export interface GlobalTokenStatsKPI {
  total: number
  prevTotal?: number | null
  deltaPct?: number | null
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  workflowTotal: number
  pmTotal: number
  projectCount: number
  runCount: number
  modelCount: number
}

export interface GlobalTokenStatsProjectRow {
  projectId: string
  name: string
  total: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens?: number
  cacheWriteTokens?: number
  deltaPct?: number | null
}

export interface GlobalTokenStatsRunRow {
  runId: string
  title: string
  projectId: string
  projectName: string
  workflowName: string
  modelKey: string
  modelName: string
  total: number
}

export interface GlobalTokenStatsNamedBucket {
  name: string
  total: number
  other?: boolean
}

export interface GlobalTokenStatsHeatmap {
  rows: string[]
  cols: string[]
  grid: number[][]
}

export interface GlobalTokenStatsSeries {
  key: string
  name: string
  trend: TokenStatsBucket[]
}

export interface GlobalTokenStatsFilterOption {
  key: string
  name: string
}

export interface GlobalTokenStats {
  window: TokenStatsWindow | string
  bucketWidth: 'hour' | 'day' | 'week' | string
  timezone: string
  empty: boolean
  kpi: GlobalTokenStatsKPI
  trend: TokenStatsBucket[]
  prevTrend: TokenStatsBucket[]
  composition: TokenStatsComposition
  projects: GlobalTokenStatsProjectRow[]
  modelRanking: TokenStatsModel[]
  nodeTypes: GlobalTokenStatsNamedBucket[]
  workflows: TokenStatsWorkflow[]
  heatmap: GlobalTokenStatsHeatmap
  topRuns: GlobalTokenStatsRunRow[]
  projectTrends: GlobalTokenStatsSeries[]
  modelTrends: GlobalTokenStatsSeries[]
  filterOptions: {
    projects: GlobalTokenStatsFilterOption[]
    models: GlobalTokenStatsFilterOption[]
  }
}

export interface PmLeaderBinding {
  enabled: boolean
  agentConfigRef: string
  agentAvailable: boolean
  agentError?: string
  enabledMcps?: string[]
  /** Run variable name that enables gate-auto PM invoke when present+truthy. Empty = off. */
  gateAutoVar?: string
  /** Optional prompt appended after system default gate-auto guidance. */
  gateAutoPrompt?: string
  aclNote: string
}

export interface ProjectMemoryItem {
  id: string
  projectId: string
  agentName?: string
  title: string
  content: string
  source: string
  updatedBy: string
  createdAt: string
  updatedAt: string
}

/** Project-scoped requirement draft (「需求草稿」); status open|done. */
export type RequirementDraftKind = 'requirement' | 'milestone'

export interface RequirementDraft {
  id: string
  projectId: string
  title: string
  bodyMarkdown: string
  status: 'open' | 'done'
  /** requirement|milestone; legacy rows default to requirement. */
  kind: RequirementDraftKind
  /** YYYY-MM-DD begin date (requirements); empty when unscheduled. */
  startAt: string
  /** YYYY-MM-DD end date or milestone date; empty when unscheduled. */
  dueAt: string
  /** 0–100; independent of status. */
  progress: number
  /** Optional top-level requirement id (one-level grouping). */
  parentId: string | null
  createdAt: string
  updatedAt: string
}

export type RequirementDraftStatusFilter = 'open' | 'done' | 'all'

export type RequirementDraftViewMode = 'edit' | 'gantt' | 'milestones'

export interface RequirementDraftSchedulePatch {
  kind?: RequirementDraftKind
  startAt?: string
  dueAt?: string
  progress?: number
  /** Omit to leave unchanged; null or '' clears parent. */
  parentId?: string | null
}

export interface RequirementDraftCreateBody {
  kind?: RequirementDraftKind
  title?: string
  startAt?: string
  dueAt?: string
  progress?: number
  parentId?: string | null
}

export interface AgentCronJob {
  id: string
  agentName: string
  projectId: string
  threadId: string
  name: string
  prompt: string
  scheduleKind: string
  scheduleExpr: string
  timezone?: string
  enabled: boolean
  deliverToChannel: boolean
  nextRunAt?: string
  lastRunAt?: string
  lastStatus?: string
  lastError?: string
  consecutiveErrors?: number
  createdAt: string
  updatedAt: string
}

export interface ProgressCitation {
  type: 'run' | 'gate' | 'artifact' | 'workflow' | 'plan' | string
  targetId: string
  summarySnippet?: string
  detail?: Record<string, unknown>
}

export interface AttachedContext {
  kind: 'run' | 'workflow'
  id: string
  label?: string
}

export interface ChatThread {
  id: string
  projectId: string
  userId: string
  title: string
  /** Channel thread with no source=channel inbound. */
  unspoken?: boolean
  /** user | cron | channel…; omitted on older rows */
  kind?: string
  sandboxRef?: string
  createdAt: string
  updatedAt: string
}

export interface ChatMessage {
  id: string
  threadId: string
  role: 'user' | 'assistant' | 'system' | string
  content: string
  /** ok | failed; empty/legacy treated as ok */
  status?: 'ok' | 'failed' | string
  /** connection | sandbox | empty | unknown | stopped */
  failKind?: 'connection' | 'sandbox' | 'empty' | 'unknown' | 'stopped' | string
  /** user | cron | channel | "" */
  source?: string
  images?: ClarifyImage[]
  citations?: ProgressCitation[]
  attachedContext?: AttachedContext
  /** Assistant turn token total (nil = not reported). */
  usage?: TokenUsage | null
  /** Per-model breakdown after ingest merge / bridge backfill. */
  usageByModel?: TokenUsageByModel | null
  createdAt: string
}

/** In-progress PM consult draft checkpoint (server-persisted). */
export interface PmTurnDraft {
  id: string
  threadId: string
  userMsgId: string
  partialText: string
  chunkIndex: number
  eventSeq: number
  status: 'streaming' | 'done' | 'failed' | string
  failKind?: string
  sandboxId?: number
  createdAt: string
  updatedAt: string
}

export interface PmDraftResponse {
  draft: PmTurnDraft | null
  live: boolean
  hasFinal: boolean
}

// A published, immutable snapshot of a workflow's graph.
export interface WorkflowVersion {
  workflowId: string
  version: number
  publishedAt: string
}

export interface WorkflowGraph {
  nodes: WFNode[]
  edges: WFEdge[]
  variables?: any[]
}

// ---- field schema for inspector ----
export type OutputCardTypeTag = '结构化产物' | '自定义产物' | 'Markdown' | '来源失败'

export interface OutputCard {
  index: number
  template: string
  title: string
  typeTag: OutputCardTypeTag
  status: 'ok' | 'failed'
  errorReason?: string
  /** Server-issued fail heading (e.g. 缺少可展示产出 / 来源状态失败). */
  failTitle?: string
  /** Parsed JSON snapshot for structured framework products. */
  jsonSnapshot?: string
  /** Markdown body for agent content or rendered structured product. */
  markdown?: string
  /** Custom artifact file name (produces / artifact() reference). */
  artifactName?: string
  /** Upstream node id for node output references. */
  nodeId?: string
  /** Upstream output key (e.g. plan, content). */
  outputKey?: string
  /** Reserved artifact name when outputKey maps to a framework product. */
  structuredArtifactName?: string
}

export interface FieldSchema {
  key: string
  label: string
  type: 'text' | 'textarea' | 'prompt' | 'number' | 'select' | 'switch' | 'duration' | 'actions' | 'form' | 'cases' | 'variables' | 'assignments' | 'conditional' | 'output_sources' | 'repo_select'
  placeholder?: string
  help?: string
  options?: { value: string; label: string }[]
  optional?: boolean
}

export interface NodeTypeDef {
  type: NodeType
  label: string
  desc: string
  icon: string
  color: string // tailwind text color class for accent band
  category: string
  fields: FieldSchema[]
  outputs: { key: string; desc: string }[]
  defaults: Record<string, any>
  // Markdown help shown in the inspector's "帮助" tab: what the node does, its
  // MCP contract / structured product, and how to wire it up.
  help?: string
}

// ---- runs ----
export interface AcpEvent {
  t: number // seconds offset
  kind: 'message' | 'thought' | 'plan' | 'tool_call' | 'commands'
  title?: string
  text?: string
  status?: 'running' | 'completed' | 'failed'
  // 当 tool_call 是 artifact-store MCP 写入时,标记产物名/类型,供日志高亮与产物派生
  artifact?: { name: string; kind: 'markdown' | 'json' | 'yaml' }
}

// 一次内置 MCP 工具调用的记录(入参/结果均已截断,仅供调试)。
export interface McpCall {
  at: string
  tool: string
  args?: string
  result?: string
  isError?: boolean
}

/** Per-execution LLM token usage. Absent/undefined = not reported (UI —). */
export interface TokenUsage {
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
}

/** One model bucket with optional source / backfill semantics. */
export interface ModelTokenUsage extends TokenUsage {
  source?: string
  filled?: boolean
}

/** modelKey → bucket (keys are ingest-merged;「未知/未分桶」for legacy). */
export type TokenUsageByModel = Record<string, ModelTokenUsage>

export interface NodeRun {
  nodeId: string
  // 该节点的第几次执行(1 起)。循环回边/门禁退回会让同一节点执行多次,
  // 每次都是独立记录。
  iteration?: number
  status: NodeRunStatus
  startedAt?: string
  durationSec?: number
  /** Full failure reason for failed executions (e.g. sandbox setup errors). */
  error?: string
  outputMd?: string
  outputs?: Record<string, any>
  // 该次执行结束时的全局变量快照(便于在时间线上按时刻调试)。
  varsSnapshot?: Record<string, any>
  events?: AcpEvent[]
  // 该次执行调用的内置 MCP 工具轨迹(工具名+入出参),便于调试。
  mcpCalls?: McpCall[]
  /**
   * Token usage for this execution. undefined/null = not reported (show —);
   * present (incl. all zeros) = explicitly reported.
   */
  usage?: TokenUsage | null
  /** Per-model breakdown after ingest merge / bridge backfill. */
  usageByModel?: TokenUsageByModel | null
}

export interface Artifact {
  id: string
  name: string
  kind: 'markdown' | 'json' | 'yaml' | 'html' | 'image' | 'text'
  nodeId: string
  runId: string
  runTitle?: string
  workflowId?: string
  workflowName: string
  sizeBytes: number
  createdAt: string
  /** Bumped on every Save upsert; used for external-change detection. */
  updatedAt?: string
  /** Concurrency token from GET /content or gate save; send as If-Match on save. */
  etag?: string
  /** Same-name write_artifact overwrite count (1 on first write). */
  revision?: number
  /** Omitted in run/inbox list responses; load via GET /api/artifacts/:id/content. */
  content?: string
}

export interface ClarifyImage {
  /** Base64 (no data: prefix) on upload / legacy rows; omitted after blob externalization. */
  data?: string
  /** blob:{id} reference after server-side externalization. */
  ref?: string
  mimeType: string
  /** Original filename when known; forwarded through platform → ACP Bridge. */
  name?: string
  /** Decoded byte length when known. */
  sizeBytes?: number
}

/** Paragraph variable composite value: text + optional image attachments. */
export type CompositeText = {
  text: string
  images?: ClarifyImage[]
}

export interface ReactOption {
  id: string
  label: string
  // Agent-suggested choice: highlighted in the UI and preferred by auto-select
  // (auto_var). Single-select: at most one; multi-select: one or more.
  // Unmarked falls back to the first option.
  recommended?: boolean
  /** Optional self-contained HTML document for UI/layout visual decisions. */
  demoHtml?: string
}

export interface ReactQuestion {
  id: string
  prompt: string
  options: ReactOption[]
  allowMultiple?: boolean
}

/** One plaintext form field from ask_form (preflight). Type is text|url only. */
export interface ReactFormField {
  name: string
  label: string
  /** text | url only — never password; passwords use plaintext text. */
  type?: string
  placeholder?: string
  value?: string
  required?: boolean
  /** Hover reason (plan gap); agents may also send `reason`. */
  why?: string
  reason?: string
}

/** One plaintext env-collection form raised via ask_form (preflight). */
export interface ReactForm {
  title?: string
  fields: ReactFormField[]
}

// ReactAnnotation is one precise reference a human pins to a review turn:
// a JSON path into a structured product (jsonPath), a DOM CSS selector into a
// visual page (selector), and/or a paragraph quote excerpt from product text.
// Optional label + note. Sent with api.reactReply and rendered into the agent's
// review prompt. quote uses a ~500-char soft cap (truncated marks truncation).
export interface ReactAnnotation {
  jsonPath?: string
  selector?: string
  /** Page location.href at DOM pick time (SPA navigations). */
  url?: string
  label?: string
  note?: string
  /** Paragraph excerpt from a text selection ("添加到聊天"). */
  quote?: string
  /** True when quote was soft-truncated to the ~500-char limit. */
  truncated?: boolean
}

export interface ClarifyTurn {
  role: 'agent' | 'human'
  text: string
  at: string
  images?: ClarifyImage[]
  // Structured choice questions the agent raised this turn (ask_question MCP
  // tool). The UI renders the latest such turn as selectable cards.
  questions?: ReactQuestion[]
  // Plaintext env-collection forms the agent raised this turn (ask_form MCP
  // tool, preflight). Persisted like questions so the inbox can re-render.
  forms?: ReactForm[]
  // Precise field/element annotations the human attached this review turn.
  annotations?: ReactAnnotation[]
  /** Agent turn stopped mid-stream by 轮级 Cancel; partial text retained. */
  interrupted?: boolean
  /** Live streaming agent bubble (not yet persisted). */
  streaming?: boolean
  /**
   * Streaming / persisted agent thought (ACP kind=thought). Kept separate from
   * `text` so message arrival does not erase the thought block.
   */
  thought?: string
}

export interface Gate {
  runId: string
  nodeId: string
  // 该门禁对应节点的第几次执行(1 起);回退重开会递增,用于取上游那次执行的产物。
  iteration?: number
  workflowId?: string
  workflowName: string
  /** Engine-computed run title (first ask variable); absent when empty. */
  runTitle?: string
  title: string
  bodyMd: string
  actions: { id: string; label: string; requireForm?: boolean }[]
  form?: { key: string; label: string; required?: boolean }[]
  /** Upstream execution bound at gate create (page preferred). Absent on legacy gates. */
  upstreamNodeId?: string
  upstreamIteration?: number
  // The upstream producer node whose still-alive review session a ReAct reject
  // edits, and whether that session is live. Present only when the backend can
  // offer the in-place ReAct reject entry (else the UI shows plain approve/reject).
  reactUpstreamNodeId?: string
  reactSessionAlive?: boolean
  requestedAt: string
  tags?: string[]
}

/** Leak-free human_gate share-link chip (no plaintext token). */
export interface GateShareInboxStatus {
  state: 'none' | 'active' | 'used' | 'revoked' | 'expired' | string
  ttlTier?: string
  /** Link-level capability: full | react_only. Absent/empty ⇒ full. */
  permissionPreset?: 'full' | 'react_only' | string
  expiresAt?: string
  remainingSec?: number
  usedAt?: string
  revokedAt?: string
  canCreate?: boolean
  canManage?: boolean
  hasPass?: boolean
  hasFail?: boolean
}

export interface GateInboxItem extends Gate {
  type: 'gate'
  /** Graph node type (human_gate / proposal_select). Share entry only for human_gate. */
  nodeType?: string
  shareLink?: GateShareInboxStatus
}

/** Generic share-panel target (human_gate or inbox review). */
export type ShareLinkTarget = {
  runId: string
  nodeId: string
  iteration?: number
  shareLink?: GateShareInboxStatus
  kind?: 'human_gate' | 'review' | string
}

export interface ClarifyInboxItem {
  type: 'clarify'
  /**
   * Badge semantic for list rendering. Channel remains `type: 'clarify'`.
   * `clarify` = react needs clarify; `review` = ReviewCapable product review;
   * `app_preview` = application preview waiting for confirm & continue.
   * Older backends may omit this; UI falls back to clarify.
   */
  kind?: 'clarify' | 'review' | 'app_preview' | 'preflight'
  /**
   * `starting` = the node's sandbox is still booting: no transcript yet and no
   * reply accepted, so the card renders as a loading row.
   * `replying` = parked at waiting_human and the review/clarify session is busy
   * (sessionBusy / queue waiting>0). Absent = parked and idle.
   * starting takes priority over replying.
   */
  state?: 'starting' | 'replying'
  runId: string
  nodeId: string
  iteration?: number
  workflowId?: string
  workflowName: string
  /** Engine-computed run title (first ask variable); absent when empty. */
  runTitle?: string
  label: string
  done: boolean
  requestedAt: string
  updatedAt: string
  tags?: string[]
  /** Present on kind=review only (leak-free chip, no plaintext token). */
  shareLink?: GateShareInboxStatus
}

export type InboxItem = GateInboxItem | ClarifyInboxItem

// FSM 状态轨迹:进入/离开状态、触发的转移、回滚事件
export interface StateTraceEntry {
  at: string
  nodeId: string
  event: 'enter' | 'exit' | 'transition' | 'rollback' | 'pause' | 'resume'
  // 进入事件携带该节点第几次执行(1 起),用于在轨迹上标注「第 N 次」。
  iteration?: number
  detail?: string
  kind?: EdgeKind
  to?: string
}

// 全局变量运行期取值
export interface RunVar {
  name: string
  type: 'int' | 'string' | 'bool'
  value: any
}

export interface Run {
  id: string
  workflowId: string
  workflowName: string
  // The published workflow version this run executed against (pinned at start).
  workflowVersion?: number
  title?: string
  status: 'queued' | 'running' | 'waiting_human' | 'completed' | 'failed' | 'cancelled'
  trigger: string
  startedAt: string
  /** Set when the run was created / enqueued; used for queued rows in the list. */
  createdAt?: string
  durationSec: number
  progress: number
  branch?: string
  /** Current workflow node label for running/waiting_human runs (list summary). */
  currentNodeLabel?: string
  /** Admission priority: high | normal | low (default normal). */
  priority?: 'high' | 'normal' | 'low'
  tags?: string[]
  /** Immutable StartRun sandbox env snapshot (secrets masked as ****). */
  sandboxEnv?: ProjectEnvEntry[]
  attempt?: number
  // The graph snapshot this run executed (pinned at start). Lets the run detail
  // canvas render against exactly what ran, independent of later edits/deletion
  // of the live workflow definition.
  nodes?: WFNode[]
  edges?: WFEdge[]
  // 每个节点的「最新一次」执行(画布状态/默认展示)。
  nodeRuns: Record<string, NodeRun>
  // 每个节点的「全部执行历史」(由旧到新),用于「第 N 次执行」切换与追溯。
  nodeExecutions?: Record<string, NodeRun[]>
  artifacts: Artifact[]
  gate?: Gate
  clarify?: {
    nodeId: string
    iteration?: number
    turns: ClarifyTurn[]
    done: boolean
    previewArtifact?: string
  }
  // Per-node react conversations, keyed by node id (a run may have several
  // react nodes). Lets each react node show its own dialogue immediately.
  clarifyByNode?: Record<
    string,
    { nodeId: string; iteration?: number; turns: ClarifyTurn[]; done: boolean; previewArtifact?: string }
  >
  /** Authoritative busy/queue snapshots for refresh-resume (clarify + review). */
  reactSessions?: Record<
    string,
    {
      kind?: string
      waiting?: number
      busy?: boolean
      items?: { id?: string; text?: string }[]
      activeItem?: {
        id?: string
        text?: string
        images?: ClarifyImage[]
        annotations?: ReactAnnotation[]
      }
    }
  >
  git?: { pushed: boolean; pushedSha?: string; branch?: string; mrUrl?: string } | null
  trace?: StateTraceEntry[]
  vars?: RunVar[]
  /** Run-level human failure reason (failed runs only). */
  error?: string
  /** Alias of error for API consumers expecting failedReason. */
  failedReason?: string
  failedNode?: string
  noSandboxLog?: boolean
  logSummaryOrRef?: string
}
