import '../src/styles/global.css'
import { createApp } from 'vue'
import { createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { useAuth } from '../src/lib/composables/useAuth'
import { createAppRouter } from '../src/router'
import App from '../src/App.vue'

const params = new URLSearchParams(window.location.search)
const assistantMode = params.get('assistant') || 'ready'
const settingsItems = [
  ['max_concurrent_runs', 6, 1, 64], ['run_sandbox_ttl_minutes', 30, 0, 1440],
  ['run_sandbox_max_retention_hours', 24, 1, 168], ['test_sandbox_ttl_minutes', 60, 1, 1440],
  ['max_test_sandboxes', 4, 1, undefined], ['sandbox_memory_mb', 4096, 512, 65536],
  ['agent_idle_timeout_minutes', 10, 1, 120],
].map(([key, value, min, max]) => ({ key, value, min, max, label: key, source: key === 'sandbox_memory_mb' ? 'env' : 'db', locked: key === 'sandbox_memory_mb' }))
const brand = { product_name: 'Grasp', home_subtitle: '让工作流协调每一次交付' }
const names = ['产品研发', '代码审查', '自动化回归', '需求澄清', '数据分析', '发布检查', '文档维护', '安全扫描', '问题排查', 'API 集成', '体验优化', '周报汇总']
const workspace: any = { name: '工作区', description: '团队共用的工作流与执行配置', variables: [{ name: 'ENVIRONMENT', type: 'select', value: 'staging', options: 'staging,production', desc: '默认部署环境', secret: false, ask: false, required: false, editable: true }], unknownModelDisplayName: '', pmLeaderEnabled: true, pmLeaderAgent: 'manager', notifyPolicy: { enabled: false, defaultEvents: ['waiting_human', 'failed'], channelIds: [] } }
const workflows: any[] = names.map((name, index) => ({ id: `wf-${index + 1}`, name, description: ['从需求到交付，协调研发、测试与评审。', '检查变更质量，保留可追踪的评审结论。', '运行关键测试，汇总结果与后续行动。'][index % 3], status: index % 4 === 1 ? 'draft' : 'published', version: 3, publishedVersion: index % 4 === 1 ? 2 : 3, updatedAt: `2026-10-${String(20 - index).padStart(2, '0')}T10:00:00Z`, needsRepo: false, showOnHome: index < 3, notifyPolicy: { mode: 'inherit', events: [] }, nodes: [{ id: 'input', type: 'input', label: '输入', config: { variables: [] } }, { id: 'end', type: 'end', label: '完成', config: {} }], edges: [{ id: 'e1', source: 'input', target: 'end' }] }))
for (const workflow of workflows) workflow.publishedSnapshot = { version: workflow.publishedVersion, name: workflow.name, description: workflow.description, nodes: workflow.nodes, edges: workflow.edges }
const titles = ['优化工作流审批交互', '修复回归测试中的不稳定用例', '补充 API 接口说明', '检查发布前配置', '整理产品需求与验收条件', '更新周报与执行记录']
const runs = titles.map((title, index) => ({ id: `run-${index + 1}`, workflowId: workflows[index].id, workflowName: workflows[index].name, title, status: index === 0 ? 'running' : index === 1 ? 'waiting_human' : 'completed', trigger: 'manual', startedAt: '2026-10-20T09:00:00Z', durationSec: 320, progress: index > 1 ? 100 : 45, currentNodeLabel: '执行', nodeRuns: {}, artifacts: [] }))
const artifactFiles: any[] = params.get('artifacts') === 'empty' ? [] : Array.from({ length: 24 }, (_, index) => ({
  id: `artifact-${index + 1}`, name: index === 0 ? 'release-notes.md' : index === 1 ? 'review.json' : index === 2 ? 'page.html' : index === 3 ? 'feedback_index.json' : `implementation-${index + 1}.md`,
  kind: index === 2 ? 'html' : [1, 3].includes(index) ? 'json' : 'markdown', nodeId: index < 3 ? 'deliver' : 'review',
  runId: index < 12 ? 'run-1' : index < 22 ? 'run-2' : 'session-run', runTitle: index < 12 ? '优化工作流审批交互' : index < 22 ? '修复回归测试中的不稳定用例' : '检查发布前配置',
  workflowId: index < 12 ? 'wf-1' : index < 22 ? 'wf-2' : '', workflowName: index < 12 ? names[0] : index < 22 ? names[1] : '',
  sizeBytes: 2340 + index * 20, createdAt: `2026-10-20T09:${String(59-index).padStart(2, '0')}:00Z`, revision: index === 2 ? 2 : 1,
}))
if (assistantMode === 'conversation' && artifactFiles.length) {
  artifactFiles[0].id = 'art-a1b2c3d4'
  artifactFiles[1].id = 'art-b1b2c3d4'
  artifactFiles[12].name = 'release-notes.md'
}
function artifactContent(file: any) {
  if (file.name === 'review.json') return JSON.stringify({ verdict: 'approve_with_comments', summary: '审批交互已通过回归测试。核心流程完整，可在发布前补齐以下细节。', findings: [{ id: 'F1', severity: 'low', title: '补充键盘操作提示', file: 'ApprovalPanel.vue', line: 42, detail: '审批按钮已支持键盘操作，建议在帮助文档中说明快捷方式。', suggestion: '在操作说明中加入 Tab 与 Enter 的使用方式。' }], action_items: ['补充审批快捷操作说明', '跟进下一轮体验回访'] })
  if (file.name === 'feedback_index.json') return JSON.stringify({ items: [] })
  if (file.kind === 'html') return '<!doctype html><html><body style="font-family:sans-serif;padding:32px"><h1>发布预览</h1><p>清晰的审批交互，让下一步行动更直接。</p></body></html>'
  return '# 发布说明\n\n本次更新优化了工作流审批和文件浏览体验。\n\n## 已完成\n\n- 简化审批入口，明确待处理事项\n- 统一文件目录与内容预览\n- 保留可追溯的版本与执行记录\n\n## 验证结果\n\n关键路径回归通过，发布配置已检查。\n\n| 检查项 | 结果 |\n| --- | --- |\n| 工作流执行 | 通过 |\n| 审批交互 | 通过 |\n| 文件导出 | 通过 |\n'
}
let artifactTreeAttempts = 0
const requirementDrafts: any[] = params.get('drafts') === 'filled' ? [
  { id: 'launch', title: '支付体验改进', bodyMarkdown: '# 目标\n\n减少支付流程中的阻塞。', status: 'open', kind: 'requirement', startAt: '2026-10-15', dueAt: '2026-10-28', progress: 45, parentId: null, createdAt: '2026-10-12T09:00:00Z', updatedAt: '2026-10-20T09:00:00Z' },
  { id: 'release', title: '十月版本交付', bodyMarkdown: '# 验收目标\n\n完成支付体验改进，确保回归通过。', status: 'open', kind: 'milestone', startAt: '', dueAt: '2026-10-30', progress: 0, parentId: null, createdAt: '2026-10-12T09:00:00Z', updatedAt: '2026-10-20T09:00:00Z' },
  { id: 'payment', title: '优化支付失败重试体验', bodyMarkdown: '# 支付失败重试\n\n保留订单状态，并为用户提供明确的重试入口。\n\n## 验收条件\n\n- 支付失败时说明原因\n- 重试后恢复订单流程', status: 'open', kind: 'requirement', startAt: '2026-10-18', dueAt: '2026-10-25', progress: 45, parentId: 'launch', createdAt: '2026-10-14T09:00:00Z', updatedAt: '2026-10-20T09:00:00Z' },
  { id: 'docs', title: '补充集成说明与示例', bodyMarkdown: '# 集成文档\n\n整理配置示例与常见问题。', status: 'open', kind: 'requirement', startAt: '', dueAt: '', progress: 0, parentId: null, createdAt: '2026-10-16T09:00:00Z', updatedAt: '2026-10-19T09:00:00Z' },
] : []
const calls: { path: string; method: string; body?: any }[] = []
const pmThreads: any[] = ['history', 'active-history', 'orphan', 'unavailable-thread', 'conversation'].includes(assistantMode) ? [{ id: 'past-conversation', title: '本周研发进展', agentAvailable: assistantMode !== 'unavailable-thread' }] : []
const conversationMessages = Array.from({ length: 16 }, (_, index) => [
  { id: `question-${index}`, role: 'user', content: `第 ${index + 1} 轮：${['本周的研发进展如何？', '当前有哪些需要优先处理的问题？', '请整理发布检查结果。', '接下来如何安排验证？'][index % 4]}`, status: 'ok', createdAt: '2026-10-20T09:00:00Z' },
  { id: `answer-${index}`, role: 'assistant', content: `## 第 ${index + 1} 轮进展\n\n本周已完成 API 文档与发布配置检查。工作流审批与产物浏览的关键流程已经通过验证。\n\n代码审查正在等待确认，建议优先处理回归测试中的不稳定用例。\n\n- 研发：完成核心交付\n- 测试：继续补充边界场景\n- 发布：确认负责人和上线时间`, status: 'ok', createdAt: '2026-10-20T09:01:00Z', citations: index === 15 ? [{ type: 'artifact', targetId: 'release-notes.md', summarySnippet: '发布说明' }, { type: 'artifact', targetId: 'art-b1b2c3d4', summarySnippet: '评审结论' }, { type: 'artifact', targetId: 'page.html', summarySnippet: '页面预览' }] : [] },
]).flat()
;(window as any).__workspaceFixture = { workspace, workflows, calls, settingsItems, brand, requirementDrafts, artifactFiles }
const fetchActual = window.fetch.bind(window)
window.fetch = async (input, init) => {
  const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, window.location.origin)
  if (!url.pathname.startsWith('/api/')) return fetchActual(input, init)
  const path = url.pathname.slice(4)
  const method = init?.method || 'GET'
  let body: any
  try { body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined } catch { /* fixture request has no JSON body */ }
  calls.push({ path: url.pathname, method, body })
  let data: any = { status: 'ok' }
  if (path === '/workspace') { if (method !== 'GET') Object.assign(workspace, body); data = workspace }
  else if (path === '/workflows/catalog') data = params.get('workflows') === 'empty' ? [] : workflows.map(({ nodes, edges, variables, publishedSnapshot, ...metadata }) => metadata)
  else if (path === '/workflows') data = params.get('workflows') === 'empty' ? [] : workflows
  else if (/^\/workflows\/[^/]+\/runs$/.test(path)) data = { id: 'run-started', status: 'queued' }
  else if (/^\/workflows\/[^/]+$/.test(path)) data = workflows.find((item) => item.id === path.split('/')[2]) || workflows[0]
  else if (/^\/workflows\/[^/]+\/versions$/.test(path)) data = []
  else if (/^\/workflows\/[^/]+\/home-visibility$/.test(path)) { data = workflows.find((item) => item.id === path.split('/')[2]); Object.assign(data, body) }
  else if (/^\/workflows\/[^/]+\/notify-policy$/.test(path)) { data = workflows.find((item) => item.id === path.split('/')[2]); data.notifyPolicy = body.notifyPolicy }
  else if (path === '/runs') { const items = url.searchParams.has('status') ? runs.filter((run) => url.searchParams.get('status')!.split(',').includes(run.status)) : runs; data = { items, total: items.length, page: 1, pageSize: 20 } }
  else if (/^\/runs\/[^/]+$/.test(path)) { const id = path.split('/')[2]; data = { ...(runs.find(run => run.id === id) || runs[0]), id, artifacts: artifactFiles.filter(file => file.runId === id) } }
  else if (path === '/artifacts/tree') {
    if (params.get('artifacts') === 'fail' && artifactTreeAttempts++ === 0) return new Response(JSON.stringify({ error: 'unavailable' }), { status: 503, headers: { 'content-type': 'application/json' } })
    data = { count: artifactFiles.length, sessionCount: artifactFiles.filter(file => !file.workflowId).length, workflows: workflows.slice(0, 2).map(workflow => ({ workflowId: workflow.id, workflowName: workflow.name, count: artifactFiles.filter(file => file.workflowId === workflow.id).length })).filter(workflow => workflow.count) }
  }
  else if (path === '/artifacts') {
    const query = (url.searchParams.get('q') || '').toLocaleLowerCase()
    const items = artifactFiles.filter(file => (!url.searchParams.has('workflowId') || file.workflowId === url.searchParams.get('workflowId')) && (!url.searchParams.has('session') || !file.workflowId) && [file.name, file.nodeId, file.runTitle].some(value => value.toLocaleLowerCase().includes(query)))
    data = { items, total: new Set(items.map(file => file.runId)).size, page: 1, pageSize: 20 }
  }
  else if (/^\/artifacts\/[^/]+\/versions$/.test(path)) data = assistantMode === 'conversation' && path.includes('artifact-3') ? [{ artifactId: 'artifact-3', runId: 'run-1', nodeId: 'deliver', revision: 1, kind: 'html', sizeBytes: 2340, createdAt: '2026-10-20T08:00:00Z' }, { artifactId: 'artifact-3', runId: 'run-1', nodeId: 'deliver', revision: 2, kind: 'html', sizeBytes: 2380, createdAt: '2026-10-20T09:00:00Z' }] : []
  else if (/^\/artifacts\/[^/]+\/content$/.test(path)) { const file = artifactFiles.find(file => file.id === path.split('/')[2]); data = { ...file, content: artifactContent(file) } }
  else if (/^\/artifacts\/[^/]+$/.test(path) && method === 'DELETE') { const index = artifactFiles.findIndex(file => file.id === path.split('/')[2]); if (index >= 0) artifactFiles.splice(index, 1); return new Response(null, { status: 204 }) }
  else if (path === '/gates') data = { items: [], total: 0, page: 1, pageSize: 20 }
  else if (path === '/agents') data = [{ name: 'manager', description: '协调团队与执行工作流', acpBackend: 'opencode', configRoot: '/config', workspaceDir: '/workspace', env: {}, mcp: [], capabilities: { outlets: [] } }]
  else if (path === '/workspace/requirement-drafts') {
    if (method === 'POST') {
      data = { id: `new-draft-${requirementDrafts.length + 1}`, title: '未命名草稿', bodyMarkdown: '', status: 'open', kind: 'requirement', startAt: '', dueAt: '', progress: 0, parentId: null, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), ...body }
      requirementDrafts.unshift(data)
    } else {
      const status = url.searchParams.get('status') || 'all'
      const query = (url.searchParams.get('q') || '').toLocaleLowerCase()
      data = { items: requirementDrafts.filter(draft => (status === 'all' || draft.status === status) && draft.title.toLocaleLowerCase().includes(query)) }
    }
  }
  else if (path.startsWith('/workspace/requirement-drafts/')) {
    const id = path.split('/')[3]
    data = requirementDrafts.find(draft => draft.id === id)
    if (method === 'DELETE') requirementDrafts.splice(requirementDrafts.indexOf(data), 1)
    else if (method !== 'GET') Object.assign(data, body)
  }
  else if (path === '/workspace/pm-leader') data = { enabled: !['disabled', 'history'].includes(assistantMode), agentAvailable: !['disabled', 'unavailable', 'orphan'].includes(assistantMode), agentConfigRef: 'manager', enabledMcps: [] }
  else if (path === '/workspace/pm/threads') {
    if (method === 'POST') { data = { id: 'new-conversation', title: '新会话', agentAvailable: true }; pmThreads.unshift(data) }
    else data = { items: pmThreads }
  }
  else if (path === '/workspace/pm/threads/past-conversation/messages') data = assistantMode === 'conversation'
    ? { items: url.searchParams.has('before') ? conversationMessages.slice(0, 12) : conversationMessages.slice(12), hasMore: !url.searchParams.has('before'), nextBefore: url.searchParams.has('before') ? undefined : 'question-6' }
    : { items: [{ id: 'question', role: 'user', content: '本周的研发进展如何？', status: 'ok', createdAt: '2026-10-20T09:00:00Z' }, { id: 'answer', role: 'assistant', content: '本周已完成 API 文档与发布配置检查。\n\n代码审查正在等待确认，建议优先处理回归测试中的不稳定用例。', status: 'ok', createdAt: '2026-10-20T09:01:00Z' }], hasMore: false }
  else if (path === '/workspace/pm/threads/new-conversation/turns') data = { message: { id: 'first-question', role: 'user', content: body.content, status: 'ok' }, waiting: 1 }
  else if (path === '/workspace/external-mcp') data = { enabled: false, enabledPacks: [], mcpBaseUrl: `${location.origin}/mcp/workspace` }
  else if (path.endsWith('/keys')) data = []
  else if (path === '/workspace/credentials') data = { items: [] }
  else if (path === '/workspace/shared-agent-config') data = { env: {}, mcp: [], files: [], acpBackend: 'opencode', configRoot: '/config', workspaceDir: '/workspace' }
  else if (path === '/workspace/run-tags') data = { tags: ['研发', '测试', '发布'] }
  else if (path === '/settings') {
    if (method !== 'GET') {
      for (const item of settingsItems) if (!item.locked && body[item.key as string] !== undefined) item.value = body[item.key as string]
      if (body.brand_product_name !== undefined) brand.product_name = body.brand_product_name
      if (body.brand_home_subtitle !== undefined) brand.home_subtitle = body.brand_home_subtitle
    }
    data = { items: settingsItems, brand }
  }
  else if (path === '/settings/brand') data = brand
  else if (path === '/sandboxes') data = []
  else if (path === '/notifications') data = { items: [], total: 0, unreadCount: 0, page: 1, pageSize: 20 }
  else if (path === '/workspace/channels') data = { items: [], freeAgents: ['manager'], secretsKeyConfigured: true }
  else if (path.startsWith('/workspace/')) data = { items: [], total: 0, page: 1, pageSize: 20 }
  else if (path === '/stats/dashboard') data = { total: runs.length, running: 1, waitingHuman: 1, completed: 4, failed: 0, totalWorkflows: workflows.length }
  else if (path === '/auth/me') data = { username: 'workspace-preview', expires_at: '2099-01-01T00:00:00Z', is_admin: true }
  else if (path === '/health') data = { status: 'ok', accepting: true }
  return new Response(JSON.stringify(data), { status: 200, headers: { 'content-type': 'application/json' } })
}

async function bootstrap() {
  installIdleScrollbar()
  await initLocale()
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'dark' ? 'dark' : 'light')
  useAuth().setUser({ username: 'workspace-preview', expiresAt: '2099-01-01T00:00:00Z', isAdmin: true })
  const router = createAppRouter(createMemoryHistory())
  await router.push(params.get('view') || '/workflows')
  await router.isReady()
  createApp(App).directive('hover-ink', vHoverInk).use(i18n).use(router).mount('#app')
}
void bootstrap()
