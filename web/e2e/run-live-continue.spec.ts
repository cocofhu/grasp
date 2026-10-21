/**
 * #850 run live page: when the watched auto node ends and the next node is
 * also auto (实现 → 验证), the conversation continues in place. The re-keyed
 * surface must get the next node's stream: streamed text stays, one busy
 * indicator that clears at the end, no 「本轮没有输出」, no navigation.
 * Real RunLiveView; API and run WS mocked.
 */
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

const RUN = 'run-live-e2e'
type Phase = 'impl' | 'verify' | 'done'

const nodes = [
  { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: {} },
  { id: 'impl', type: 'agent', label: '实现', position: { x: 200, y: 0 }, config: {} },
  { id: 'verify', type: 'agent', label: '验证', position: { x: 400, y: 0 }, config: {} },
  { id: 'output', type: 'output', label: '输出', position: { x: 600, y: 0 }, config: {} },
]
const at = (s: number) => new Date(Date.UTC(2026, 9, 21, 3, 0, s)).toISOString()
const cur = (id: 'impl' | 'verify') => ({
  id,
  name: id === 'impl' ? '实现' : '验证',
  iteration: 1,
  kind: 'agent',
  mode: 'auto',
  state: 'running',
  task: id === 'impl' ? '实现登录页改版' : '验证改动并回报',
})

function runFor(phase: Phase) {
  const done = (id: string, s: number, e: number) => ({ nodeId: id, status: 'completed', startedAt: at(s), finishedAt: at(e), durationSec: e - s, varsSnapshot: {}, outputs: {} })
  const running = (id: string, s: number) => ({ nodeId: id, status: 'running', startedAt: at(s), varsSnapshot: {}, outputs: {} })
  return {
    id: RUN,
    workflowId: 'wf-live-e2e',
    workflowName: '实现+验证',
    title: '登录页改版',
    status: phase === 'done' ? 'completed' : 'running',
    trigger: 'manual',
    startedAt: at(0),
    nodes,
    edges: [
      { id: 'e0', source: 'input', target: 'impl' },
      { id: 'e1', source: 'impl', target: 'verify' },
      { id: 'e2', source: 'verify', target: 'output' },
    ],
    nodeRuns: {
      input: done('input', 0, 1),
      impl: phase === 'impl' ? running('impl', 1) : done('impl', 1, 13),
      ...(phase === 'impl' ? {} : { verify: phase === 'verify' ? running('verify', 14) : done('verify', 14, 30) }),
    },
    nodeExecutions: {},
    artifacts: [],
    trace: [],
    vars: [],
    ...(phase === 'done' ? {} : { currentNode: cur(phase) }),
  }
}

function chainFor(phase: Phase) {
  const seg = (id: 'impl' | 'verify', status: string, s: number, e?: number) => ({
    nodeId: id, iteration: 1, name: id === 'impl' ? '实现' : '验证', kind: 'agent', mode: 'auto', status,
    startedAt: at(s), ...(e ? { endedAt: at(e) } : {}), task: cur(id).task, eventCount: 3,
  })
  const segments = [seg('impl', phase === 'impl' ? 'running' : 'completed', 1, phase === 'impl' ? undefined : 13)]
  if (phase !== 'impl') segments.push(seg('verify', phase === 'verify' ? 'running' : 'completed', 14, phase === 'verify' ? undefined : 30))
  return { runId: RUN, status: phase === 'done' ? 'completed' : 'running', ...(phase === 'done' ? {} : { currentNode: cur(phase) }), segments }
}

async function setup(page: Page) {
  const state = { phase: 'impl' as Phase, ws: null as WebSocketRoute | null }
  await page.routeWebSocket(new RegExp(`/api/runs/${RUN}/events`), (ws) => { state.ws = ws })
  await page.route((u) => u.pathname.startsWith('/api/'), async (route) => {
    const url = new URL(route.request().url())
    const json = (body: unknown) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
    if (url.pathname === `/api/runs/${RUN}`) return json(runFor(state.phase))
    if (url.pathname === `/api/runs/${RUN}/conversation-chain`) return json(chainFor(state.phase))
    if (/\/nodes\/[^/]+\/events$/.test(url.pathname)) return json({ events: [], live: true })
    return json({})
  })
  const navs: string[] = []
  await page.goto(`/run-live-continue.html?run=${RUN}&node=impl`)
  await page.waitForFunction(() => !!(window as unknown as { __router?: unknown }).__router)
  await page.evaluate(() => {
    const w = window as unknown as { __router: { afterEach: (f: (to: { fullPath: string }) => void) => void }; __navs: string[] }
    w.__navs = []
    w.__router.afterEach((to) => w.__navs.push(to.fullPath))
  })
  await expect.poll(() => state.ws !== null).toBe(true)
  const send = (frame: unknown) => state.ws!.send(JSON.stringify(frame))
  return { state, send, navs }
}

const busy = (page: Page) => page.locator('text=/^(输出中|思考中…|生成中…)$/ >> visible=true')
type Step = { kind: 'thought' | 'message'; text: string } | { kind: 'tool'; title: string; status: string }
/** Same shape as the server's run WS acp frame: cumulative flat events plus one timeline snapshot. */
const acp = (nodeId: string, steps: Step[], b = true) => ({
  type: 'acp',
  runId: RUN,
  nodeId,
  busy: b,
  events: flat(steps),
})
/** Flat rails as the server sends them: latest thought, latest message, every tool call, then the ordered timeline. */
function flat(steps: Step[]) {
  const latest = (k: 'thought' | 'message') => [...steps].reverse().find((p) => p.kind === k)
  const out: unknown[] = []
  for (const k of ['thought', 'message'] as const) { const p = latest(k); if (p) out.push({ t: 0, ...p }) }
  for (const p of steps) if (p.kind === 'tool') out.push({ t: 0, kind: 'tool_call', title: p.title, status: p.status })
  out.push({ t: 0, kind: 'timeline', parts: steps })
  return out
}
const T1: Step = { kind: 'thought', text: '先读一下改动和测试说明。' }
const M1: Step = { kind: 'message', text: '开始验证：先跑单元测试。' }
const tool = (title: string, status = 'completed'): Step => ({ kind: 'tool', title, status })
const M2: Step = { kind: 'message', text: '验证完成：登录页改版无回归。' }

for (const theme of ['light', 'dark'] as const) {
  test(`live page: 实现 → 验证 (auto → auto) continues in place and keeps the stream (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme })
    await page.addInitScript((t) => localStorage.setItem('grasp-theme', t), theme)
    const { state, send } = await setup(page)
    await expect(page.getByTestId('clarify-observe-stop')).toBeVisible()
    send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }]))
    await expect(page.getByText('实现完成，登录页已改版。')).toBeVisible()

    // 实现 ends, 验证 starts. As on the server: node_enter, then 验证's acp frames
    // right away (before the page has switched and re-keyed its surface); the
    // GET that follows reports 验证 as the current node.
    state.phase = 'verify'
    send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }], false))
    send({ type: 'node_enter', runId: RUN, nodeId: 'verify', iteration: 1, kind: 'agent', mode: 'auto', name: '验证' })
    send(acp('verify', [T1]))
    send(acp('verify', [T1, M1]))
    await expect(page.getByTestId('clarify-segment-divider')).toContainText('验证')
    await expect(page.getByText('开始验证：先跑单元测试。')).toHaveCount(1)
    await expect(busy(page)).toHaveCount(1)
    send(acp('verify', [T1, M1, tool('运行测试 npm test', 'in_progress')]))
    send(acp('verify', [T1, M1, tool('运行测试 npm test')]))
    send(acp('verify', [T1, M1, tool('运行测试 npm test'), M2]))
    await expect(page.getByText('验证完成：登录页改版无回归。')).toHaveCount(1)
    // Streamed text is kept (not replaced by a fresh empty row per frame).
    await expect(page.getByText('开始验证：先跑单元测试。')).toHaveCount(1)
    await expect(busy(page)).toHaveCount(1)

    // Run ends: the page ends in place, the answer stays, nothing busy, no false empty-turn error.
    state.phase = 'done'
    send(acp('verify', [T1, M1, tool('运行测试 npm test'), M2], false))
    send({ type: 'node_enter', runId: RUN, nodeId: 'output', iteration: 1, kind: 'agent', mode: 'auto', name: '输出' })
    send({ type: 'status', runId: RUN, status: 'completed' })
    await expect(page.getByTestId('clarify-observe-stop')).toHaveCount(0)
    await expect(busy(page)).toHaveCount(0)
    await expect(page.getByText('本轮没有输出')).toHaveCount(0)
    await expect(page.getByText('验证完成：登录页改版无回归。')).toBeVisible()
    await expect(page.getByTestId('clarify-segment-divider')).toHaveCount(1)
    expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([])
  })
}

