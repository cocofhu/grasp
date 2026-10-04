import type { AgentTool } from '../shared/types'

/** Kind of a tool call; picks its icon and (for Grasp tools) its stage link. */
export type ToolKind = 'shell' | 'read' | 'edit' | 'search' | 'web' | 'artifact' | 'plan' | 'preview' | 'page' | 'ask' | 'mcp' | 'other'

export interface ToolMeta {
  /** i18n key under pages.clarify.tools, or '' to show the raw title. */
  labelKey: string
  kind: ToolKind
  /** Stage artifact this call wrote or read (opened by "查看"). */
  artifact?: string
  /** The call registered an app preview (opened by "打开预览"). */
  preview?: boolean
}

/** Grasp MCP tools that write a fixed structured artifact. */
const STRUCTURED: Record<string, string> = {
  set_plan: 'plan.json',
  set_clarified_requirement: 'clarified_requirement.json',
  set_research: 'research.json',
  set_root_cause: 'root_cause.json',
  set_proposals: 'proposals.json',
  set_test_result: 'test_result.json',
  set_review: 'review.json',
  set_implementation_result: 'implementation_result.json',
  set_preflight: 'preflight.json',
}

/** Grasp MCP tools whose summary is the artifact name. */
const NAMED_ARTIFACT = new Set(['write_artifact', 'read_artifact', 'set_artifact_preview', 'upload_image_artifact'])

const GRASP_KIND: Record<string, ToolKind> = {
  write_artifact: 'artifact',
  read_artifact: 'artifact',
  list_artifacts: 'artifact',
  upload_image_artifact: 'artifact',
  set_artifact_preview: 'preview',
  set_preview: 'preview',
  set_plan: 'plan',
  get_plan: 'plan',
  update_plan_status: 'plan',
  ask_question: 'ask',
  ask_form: 'ask',
  page_state: 'page',
  page_click: 'page',
  page_input: 'page',
  page_select: 'page',
  page_scroll: 'page',
  list_run_history: 'read',
  get_history_detail: 'read',
}
for (const n of Object.keys(STRUCTURED)) GRASP_KIND[n] ??= 'artifact'
for (const n of ['get_clarified_requirement', 'get_research', 'get_root_cause', 'get_proposals', 'get_test_result', 'get_review', 'get_implementation_result', 'get_preflight']) {
  GRASP_KIND[n] = 'read'
}
const GRASP_NAMES = Object.keys(GRASP_KIND).sort((a, b) => b.length - a.length)

/** Agent built-ins by squashed lowercase title (cursor / claude / codex spellings). */
const BUILTIN: Record<string, { key: string; kind: ToolKind }> = {
  shell: { key: 'shell', kind: 'shell' },
  bash: { key: 'shell', kind: 'shell' },
  terminal: { key: 'shell', kind: 'shell' },
  runterminalcmd: { key: 'shell', kind: 'shell' },
  read: { key: 'read', kind: 'read' },
  readfile: { key: 'read', kind: 'read' },
  edit: { key: 'edit', kind: 'edit' },
  multiedit: { key: 'edit', kind: 'edit' },
  strreplace: { key: 'edit', kind: 'edit' },
  editfile: { key: 'edit', kind: 'edit' },
  write: { key: 'write', kind: 'edit' },
  writefile: { key: 'write', kind: 'edit' },
  delete: { key: 'delete', kind: 'edit' },
  deletefile: { key: 'delete', kind: 'edit' },
  grep: { key: 'grep', kind: 'search' },
  search: { key: 'grep', kind: 'search' },
  semsearch: { key: 'grep', kind: 'search' },
  codebasesearch: { key: 'grep', kind: 'search' },
  glob: { key: 'glob', kind: 'search' },
  globfilesearch: { key: 'glob', kind: 'search' },
  ls: { key: 'ls', kind: 'search' },
  listdir: { key: 'ls', kind: 'search' },
  webfetch: { key: 'webFetch', kind: 'web' },
  fetch: { key: 'webFetch', kind: 'web' },
  websearch: { key: 'webSearch', kind: 'web' },
  getmcptools: { key: 'mcpTools', kind: 'mcp' },
  listmcpresources: { key: 'mcpTools', kind: 'mcp' },
  readlints: { key: 'lints', kind: 'read' },
  task: { key: 'task', kind: 'other' },
}

/**
 * The Grasp MCP tool a title names, whatever prefix the agent puts on it
 * (`set_plan`, `mcp__grasp__set_plan`, `grasp-set_plan`, `artifact-store.write_artifact`).
 */
export function graspToolName(title: string): string {
  const s = title.trim().toLowerCase()
  if (GRASP_KIND[s]) return s
  for (const n of GRASP_NAMES) {
    if (s.length > n.length && s.endsWith(n) && /[-_.:/\s]/.test(s[s.length - n.length - 1]!)) return n
  }
  return ''
}

export function toolMeta(tool: Pick<AgentTool, 'title' | 'summary'>): ToolMeta {
  const g = graspToolName(tool.title)
  if (g) {
    const meta: ToolMeta = { labelKey: `grasp.${g}`, kind: GRASP_KIND[g]! }
    if (STRUCTURED[g]) meta.artifact = STRUCTURED[g]
    else if (NAMED_ARTIFACT.has(g) && tool.summary) meta.artifact = tool.summary
    if (g === 'set_preview') meta.preview = true
    return meta
  }
  const b = BUILTIN[tool.title.toLowerCase().replace(/[\s_-]+/g, '')]
  if (b) return { labelKey: b.key, kind: b.kind }
  return { labelKey: '', kind: /^mcp|mcp_|__/i.test(tool.title) ? 'mcp' : 'other' }
}

export const TOOL_ICONS: Record<ToolKind, string> = {
  shell: 'terminal',
  read: 'doc',
  edit: 'edit',
  search: 'search',
  web: 'globe',
  artifact: 'artifact',
  plan: 'flag',
  preview: 'monitor',
  page: 'crosshair',
  ask: 'chat',
  mcp: 'connector',
  other: 'sparkles',
}

/** "1.5s" / "42s" / "2m 5s"; '' under a second (not worth the noise) or unknown. */
export function formatToolDuration(ms: number | undefined): string {
  if (!ms || ms < 1000) return ''
  if (ms < 10_000) return `${(Math.floor(ms / 100) / 10).toFixed(1)}s`
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return s % 60 ? `${m}m ${s % 60}s` : `${m}m`
  const h = Math.floor(m / 60)
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`
}
