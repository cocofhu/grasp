/**
 * /gates auto-refresh harness: full app (workspace-main mock) with a mutable
 * pending-gate list. window.__addGate(title) / __removeGate(i) mutate the
 * server list; window.__peek() fires the same focus peek the inbox listens to.
 * ?view=/gates
 */
import './workspace-main'

type GateRow = Record<string, unknown>
const gates: GateRow[] = []
let seq = 0
function makeGate(title: string, minutesAgo: number): GateRow {
  seq++
  return {
    type: 'gate', nodeType: 'human_gate',
    runId: `run-g${seq}`, nodeId: 'review', iteration: 1,
    workflowId: 'wf-1', workflowName: '产品研发', runTitle: title,
    title: '人工评审', bodyMd: `请审阅「${title}」的交付内容。`,
    actions: [{ id: 'approve', label: '通过' }, { id: 'reject', label: '打回', requireForm: true }],
    form: [{ key: 'comment', label: '评审意见' }],
    requestedAt: new Date(Date.now() - minutesAgo * 60_000).toISOString(),
  }
}
;['优化工作流审批交互', '检查发布前配置', '补充 API 接口说明', '修复回归测试中的不稳定用例', '整理产品需求与验收条件', '更新周报与执行记录', '安全扫描结果复核', '体验优化走查']
  .forEach((t, i) => gates.push(makeGate(t, 30 + i * 7)))

const w = window as unknown as Record<string, unknown>
w.__addGate = (title: string) => { gates.unshift(makeGate(title, 0)) }
w.__removeGate = (i: number) => { gates.splice(i, 1) }
w.__peek = () => window.dispatchEvent(new Event('focus'))

const inner = window.fetch
window.fetch = async (input, init) => {
  const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, window.location.origin)
  const path = url.pathname.replace(/^\/api/, '')
  const json = (data: unknown) => new Response(JSON.stringify(data), { status: 200, headers: { 'content-type': 'application/json' } })
  if (path === '/gates') {
    const page = Number(url.searchParams.get('page') || 1)
    const size = Number(url.searchParams.get('pageSize') || 20)
    return json({ items: gates.slice((page - 1) * size, page * size), total: gates.length, page, pageSize: size })
  }
  const ctx = path.match(/^\/runs\/([^/]+)\/inbox-context$/)
  if (ctx) {
    const g = gates.find((it) => it.runId === ctx[1])
    if (!g) return new Response(JSON.stringify({ error: 'not pending' }), { status: 404, headers: { 'content-type': 'application/json' } })
    return json({ type: 'gate', nodes: [{ id: 'review', type: 'human_gate', label: '人工评审', position: { x: 0, y: 0 }, config: {} }], artifacts: [], nodeExecutions: {}, gate: g })
  }
  return inner(input, init)
}
