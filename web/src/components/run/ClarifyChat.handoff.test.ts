// @vitest-environment happy-dom
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

const { nodeEvents } = vi.hoisted(() => ({ nodeEvents: vi.fn() }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, nodeEvents } }
})
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n as appI18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import { confirmFlowCeremonyKey } from '@/lib/inbox/confirmFlowCeremony'
import { beginBoot, bootKey, clearBootRecords, markBootLive, seedBoot } from '@/lib/run/bootRecord'
import type { ClarifyTurn } from '@/lib/shared/types'
import ClarifyChat from './ClarifyChat.vue'

beforeAll(async () => {
  const zh = await loadLocaleMessages('zh-CN')
  appI18n.global.setLocaleMessage('zh-CN', zh)
  appI18n.global.locale.value = 'zh-CN'
})

const turns: ClarifyTurn[] = [
  { role: 'human', text: '实现导出 CSV', at: '2026-10-14T00:00:00Z' },
  { role: 'agent', text: '已完成，请确认。', at: '2026-10-14T00:01:00Z' },
]

function mountChat(props: Record<string, unknown> = {}, play = vi.fn(async () => {})) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const ceremony = { phase: ref('idle'), reduceMotion: ref(false), playToken: ref(0), playing: ref(false), play, reset: vi.fn() }
  return mount(ClarifyChat, {
    props: { runId: 'run-1', nodeId: 'implement', iteration: 1, turns, done: false, active: true, reviewMode: true, ...props },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true, ClarifyDemoFrame: true }, provide: { [confirmFlowCeremonyKey as symbol]: ceremony } },
  })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ClarifyChat inline hand-off step row (方案 B)', () => {
  it('host pending → done renders one status row at the end of the transcript', async () => {
    const w = mountChat({ handoff: { phase: 'pending', at: '' } })
    await flushPromises()
    const rows = w.findAll('[data-testid="clarify-handoff"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].attributes('data-state')).toBe('pending')
    expect(rows[0].attributes('role')).toBe('status')
    expect(rows[0].text()).toContain('流转到下一节点')
    expect(rows[0].text()).toContain('校验中')
    expect(rows[0].find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('streaming')
    expect(rows[0].get('icon-stub').attributes('name')).toBe('arc-spinner')
    await w.setProps({ handoff: { phase: 'done', at: new Date().toISOString() } })
    const done = w.get('[data-testid="clarify-handoff"]')
    expect(done.attributes('data-state')).toBe('done')
    expect(done.text()).toContain('已确认并流转')
    expect(done.text()).toContain('已完成')
    expect(done.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('done')
    expect(done.get('icon-stub').attributes('name')).toBe('check-draw')
    await w.setProps({ handoff: null })
    expect(w.find('[data-testid="clarify-handoff"]').exists()).toBe(false)
    w.unmount()
  })

  it('names the next node when the host knows it', async () => {
    const w = mountChat({ handoff: { phase: 'pending', at: '', target: '验收测试' } })
    await flushPromises()
    expect(w.get('[data-testid="clarify-handoff"]').text()).toContain('验收测试')
    w.unmount()
  })

  it('确认并流转 click plays no full-desk overlay; the row follows the chat until done (run detail)', async () => {
    const play = vi.fn(async () => {})
    const w = mountChat({}, play)
    await flushPromises()
    const confirm = w.findAll('button').find((b) => b.text().includes('确认并流转'))
    expect(confirm).toBeTruthy()
    await confirm!.trigger('click')
    await flushPromises()
    // #868: 确认并流转 asks in an in-app dialog first.
    expect(w.emitted('finish')).toBeUndefined()
    const accept = (w.find('[data-testid="confirm-flow-dialog-accept"]').exists() ? w.get('[data-testid="confirm-flow-dialog-accept"]').element : document.querySelector('[data-testid="confirm-flow-dialog-accept"]')) as HTMLElement
    accept.click()
    await flushPromises()
    expect(w.emitted('finish')?.length).toBe(1)
    expect(play).not.toHaveBeenCalled()
    expect(w.get('[data-testid="clarify-handoff"]').attributes('data-state')).toBe('pending')
    await w.setProps({ done: true })
    await flushPromises()
    expect(w.get('[data-testid="clarify-handoff"]').attributes('data-state')).toBe('done')
    w.unmount()
  })
})

describe('ClarifyChat continuous run transcript (v2)', () => {
  const prior = [
    {
      nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat' as const, status: 'completed',
      at: '2026-10-14T00:00:00Z', open: true,
      turns: [
        { role: 'human' as const, text: '做个导出', at: '2026-10-14T00:00:00Z' },
        { role: 'agent' as const, text: '需求已整理', at: '2026-10-14T00:00:10Z' },
      ],
    },
    { nodeId: 'code', iteration: 1, label: '编码', kind: 'auto' as const, status: 'completed', at: '2026-10-14T00:01:00Z', turns: [], open: false },
  ]

  it('renders earlier nodes above the conversation and a separator into this node', async () => {
    const w = mountChat({ priorSegments: prior, nodeLabel: '验收测试' })
    await flushPromises()
    const segs = w.findAll('[data-testid="clarify-prior-segment"]')
    expect(segs).toHaveLength(2)
    expect(segs[0].element.tagName).toBe('DETAILS')
    expect((segs[0].element as HTMLDetailsElement).open).toBe(true)
    expect(segs[0].text()).toContain('「需求澄清」· 2 条消息')
    expect(segs[0].text()).toContain('需求已整理')
    expect(segs[1].attributes('data-kind')).toBe('auto')
    expect(segs[1].text()).toContain('「编码」自动执行')
    // #837: no「Agent交互 · 多轮 ReAct · 共 N 条」thread header, chain or not.
    expect(w.find('.clarify-thread-heading').exists()).toBe(false)
    expect(w.text()).not.toContain('多轮 ReAct')
    const divider = w.get('[data-testid="clarify-segment-divider"]')
    expect(divider.attributes('role')).toBe('separator')
    expect(divider.text()).toContain('已流转到「验收测试」')
    expect(w.findAll('.clarify-status-row')).toHaveLength(3)
    // Order: segments → separator → this node's turns.
    const html = w.html()
    expect(html.indexOf('clarify-segment-divider')).toBeGreaterThan(html.indexOf('需求已整理'))
    expect(html.indexOf('clarify-human-message')).toBeGreaterThan(html.indexOf('clarify-segment-divider'))
    w.unmount()
  })

  it('handed-off node opens with one user bubble saying what the step does', async () => {
    const w = mountChat({ priorSegments: prior, nodeLabel: '验收测试', nodeTask: '根据需求编写并执行验收用例' })
    await flushPromises()
    const bubble = w.get('[data-testid="clarify-node-task"]')
    expect(bubble.attributes('data-turn-role')).toBe('human')
    expect(bubble.find('.chat-user-message').text()).toBe('根据需求编写并执行验收用例')
    const html = w.html()
    expect(html.indexOf('clarify-node-task')).toBeGreaterThan(html.indexOf('clarify-segment-divider'))
    expect(html.indexOf('clarify-human-message')).toBeGreaterThan(html.indexOf('clarify-node-task'))
    // No chain above: the host only passes a task for a taken-over node (#856); same single
    // bubble, no separator. Without a task there is no bubble at all.
    await w.setProps({ priorSegments: [] })
    expect(w.findAll('[data-testid="clarify-node-task"]')).toHaveLength(1)
    expect(w.find('[data-testid="clarify-segment-divider"]').exists()).toBe(false)
    await w.setProps({ nodeTask: '' })
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    w.unmount()
  })

  it('opens with the note the node above handed over, in place of the step task', async () => {
    const handedOver = [prior[0]!, { ...prior[1]!, handoff: '  导出已实现,请重点验收编码  ', handoffSource: 'agent' as const }]
    const w = mountChat({ priorSegments: handedOver, nodeLabel: '验收测试', nodeTask: '根据需求编写并执行验收用例' })
    await flushPromises()
    const note = w.get('[data-testid="clarify-incoming-handoff"]')
    expect(note.attributes('data-turn-role')).toBe('human')
    expect(note.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「编码」的交接说明')
    expect(note.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe('导出已实现,请重点验收编码')
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    const html = w.html()
    expect(html.indexOf('clarify-incoming-handoff')).toBeGreaterThan(html.indexOf('clarify-segment-divider'))
    await w.setProps({ priorSegments: [prior[0]!, { ...handedOver[1]!, handoffSource: 'platform' as const }] })
    expect(w.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「编码」未留交接，平台代写')
    w.unmount()
  })

  it('times each upstream reply from the question it answers, folded like this node\'s replies', async () => {
    const w = mountChat({
      priorSegments: [{
        ...prior[0]!,
        at: '2026-10-14T00:00:00Z',
        turns: [
          { role: 'agent' as const, text: '先问一句', at: '2026-10-14T00:00:30Z', parts: [{ kind: 'thought' as const, text: '想想' }, { kind: 'message' as const, text: '先问一句' }] },
          { role: 'human' as const, text: '启动服务 我来指定需求', at: '2026-10-14T00:01:00Z' },
          { role: 'agent' as const, text: '好的', at: '2026-10-14T00:13:44Z', parts: [{ kind: 'thought' as const, text: '启动' }, { kind: 'message' as const, text: '好的' }] },
        ],
      }],
    })
    await flushPromises()
    const times = w.findAll('[data-testid="clarify-prior-segment"] [data-testid="agent-timeline-elapsed-time"]')
    expect(times.map((x) => x.text())).toEqual(['用时 30s', '用时 12m 44s'])
    expect(w.find('[data-testid="clarify-prior-segment"] [data-testid="agent-timeline-work"]').exists()).toBe(false)
    w.unmount()
  })

  it('puts 环境已就绪 on the 已流转到 line instead of a row of its own', async () => {
    clearBootRecords()
    beginBoot(bootKey('run-1', 'implement', 1), null)
    markBootLive(bootKey('run-1', 'implement', 1))
    const w = mountChat({ priorSegments: prior, nodeLabel: '交付', nodeTask: '合入目标分支' })
    await flushPromises()
    const divider = w.get('[data-testid="clarify-segment-divider"]')
    expect(divider.attributes('data-boot')).toBe('merged')
    expect(divider.get('[data-testid="clarify-boot-done-title"]').text()).toBe('已流转到「交付」')
    expect(divider.get('[data-testid="clarify-boot-done-meta"]').text()).toBe('环境已就绪 · 3 步')
    expect(w.findAll('[data-testid="clarify-boot-done"]')).toHaveLength(1)
    await divider.get('[data-testid="clarify-boot-done-head"]').trigger('click')
    expect(divider.findAll('[data-testid="clarify-boot-done-step"]')).toHaveLength(3)
    // No chain above: the summary keeps its own row.
    await w.setProps({ priorSegments: [] })
    expect(w.find('[data-testid="clarify-segment-divider"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-boot-done-title"]').text()).toBe('环境已就绪')
    w.unmount()
    clearBootRecords()
  })

  it('folds older nodes by default and renders nothing without a chain', async () => {
    const w = mountChat({ priorSegments: [{ ...prior[0], open: false }] })
    await flushPromises()
    expect((w.get('[data-testid="clarify-prior-segment"]').element as HTMLDetailsElement).open).toBe(false)
    await w.setProps({ priorSegments: [] })
    expect(w.find('[data-testid="clarify-segment-divider"]').exists()).toBe(false)
    w.unmount()
  })

  it('follow tail: every state says what the run does and where the chat continues', async () => {
    const at = new Date().toISOString()
    const w = mountChat({ handoff: { phase: 'done', at }, followTail: { kind: 'auto', label: '部署', at } })
    await flushPromises()
    const tail = () => w.findAll('[data-testid="clarify-follow-tail"]').at(-1)!
    const st = () => tail().find('[data-testid="thought-summary-state"]').attributes('data-state')
    const icon = () => tail().get('icon-stub').attributes('name')
    expect(tail().text()).toContain('「部署」正在自动执行')
    expect(tail().text()).toContain('完成后在此继续')
    expect(st()).toBe('streaming')
    expect(icon()).toBe('breath-dot')
    await w.setProps({ followTail: { kind: 'waiting', label: '验收测试', at } })
    await flushPromises()
    expect(tail().text()).toContain('「验收测试」准备中')
    expect(tail().text()).toContain('就绪后可直接输入')
    expect(icon()).toBe('arc-spinner')
    await w.setProps({ followTail: { kind: 'waiting', at } })
    await flushPromises()
    expect(tail().text()).toContain('等待下一节点开始')
    await w.setProps({ followTail: { kind: 'error', at } })
    await flushPromises()
    expect(tail().text()).toContain('暂时无法获取运行进度')
    expect(tail().text()).toContain('重试中')
    expect(icon()).toBe('arc-spinner')
    await w.setProps({ followTail: { kind: 'ended', status: 'completed', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行已完成')
    expect(st()).toBe('done')
    expect(icon()).toBe('check-draw')
    await w.setProps({ followTail: { kind: 'ended', status: 'failed', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行失败')
    expect(tail().text()).not.toContain('已中断')
    expect(st()).toBe('interrupted')
    expect(icon()).toBe('warn-pulse')
    await w.setProps({ followTail: { kind: 'ended', status: 'cancelled', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行已取消')
    expect(icon()).toBe('warn-pulse')
    w.unmount()
  })

  it('folds consecutive auto nodes into one row and shows 用时 for ended nodes', async () => {
    const base = { iteration: 1, kind: 'auto', status: 'completed', turns: [], open: false }
    const w = mountChat({
      priorSegments: [
        { ...base, nodeId: 'design', label: '方案设计', at: '2026-10-21T05:00:00Z', endedAt: '2026-10-21T05:00:06Z' },
        { ...base, nodeId: 'deploy', label: '部署预览', at: '2026-10-21T05:00:07Z', endedAt: '2026-10-21T05:01:30Z' },
      ],
      nodeLabel: '实现',
    })
    await flushPromises()
    const segs = w.findAll('[data-testid="clarify-prior-segment"]')
    expect(segs).toHaveLength(1)
    expect(segs[0].text()).toContain('「方案设计、部署预览」自动执行')
    expect(segs[0].text()).toContain('用时 1m 30s')
    w.unmount()
  })

  it('shows the choice, the question options, and the tool timeline inside 需求澄清', async () => {
    const w = mountChat({
      priorSegments: [{
        nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', open: true,
        at: '2026-10-14T00:00:00Z',
        turns: [
          {
            role: 'agent', text: '请选工作类型', at: '2026-10-14T00:00:00Z',
            questions: [{ id: 'q1', prompt: '这次做什么', options: [{ id: 'a', label: '其他' }, { id: 'b', label: '修缺陷' }] }],
            parts: [
              { kind: 'tool', title: 'Read', status: 'completed' },
              { kind: 'message', text: '请选工作类型' },
            ],
          },
          { role: 'human', text: '我的选择:\n- 这次做什么 → 其他', at: '2026-10-14T00:00:01Z' },
        ],
      }],
      nodeLabel: '交付',
    })
    await flushPromises()
    expect(w.get('[data-testid="clarify-prior-choice-answer"]').text()).toBe('其他')
    expect(w.get('[data-testid="clarify-prior-choice"]').text()).not.toContain('→')
    expect(w.get('[data-testid="clarify-prior-option"][data-selected="true"]').text()).toContain('其他')
    expect(w.get('[data-testid="clarify-prior-option"][data-selected="false"]').text()).toContain('修缺陷')
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    expect(w.get('[data-testid="agent-tool-group-names"]').text()).toContain('读取文件')
    w.unmount()
  })

  it('reads Q/A all the way down: every node opens with the note handed to it', async () => {
    nodeEvents.mockImplementation(async (_run: string, nodeId: string) => ({
      events: [{ kind: 'timeline', parts: [{ kind: 'thought', text: '想' }, { kind: 'message', text: `${nodeId} 完成` }] }],
      live: false,
    }))
    const at = (m: number) => `2026-10-21T05:0${m}:00Z`
    const w = mountChat({
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: at(0), endedAt: at(1), open: true,
          task: '澄清需求', handoff: '做导出', turns: [{ role: 'human', text: '我要导出', at: at(0) }, { role: 'agent', text: '好', at: at(1) }] },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: at(1), endedAt: at(2), turns: [], open: false,
          handoff: '已实现,请测' },
        { nodeId: 'test_review', iteration: 1, label: '测试评审', kind: 'auto', status: 'completed', at: at(2), endedAt: at(3), turns: [], open: false,
          handoff: '测试通过', handoffSource: 'platform' },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    // The run's first node starts from the user's own message: no bubble of its own.
    const first = w.findAll('[data-testid="clarify-prior-segment"]')[0]!
    expect(first.find('[data-testid="clarify-incoming-handoff"]').exists()).toBe(false)
    expect(first.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    const auto = w.findAll('[data-testid="clarify-prior-segment"]')[1]!
    ;(auto.element as HTMLDetailsElement).open = true
    await auto.trigger('toggle')
    await flushPromises()
    const records = w.findAll('[data-testid="clarify-auto-record"]')
    const asks = records.map((r) => r.get('[data-testid="clarify-incoming-handoff-text"]').text())
    expect(asks).toEqual(['做导出', '已实现,请测'])
    for (const r of records) {
      const order = r.findAll('[data-turn-role], [data-testid="agent-timeline"]').map((x) => x.attributes('data-turn-role') || 'agent')
      expect(order).toEqual(['human', 'agent'])
    }
    // Below the separator this node asks with 测试评审's (platform-written) note.
    const current = w.findAll('[data-testid="clarify-incoming-handoff-from"]').at(-1)!
    expect(current.text()).toBe('「测试评审」未留交接，平台代写')
    w.unmount()
  })

  it('a rerun asks with the node that routed here, not its own failed platform handoff', async () => {
    const w = mountChat({
      nodeId: 'test_review',
      nodeLabel: '测试评审',
      nodeTask: '按交接说明核验取消运行实现',
      priorSegments: [
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-22T08:01:03Z', endedAt: '2026-10-22T08:07:44Z', turns: [], open: false,
          handoff: '侧栏已加取消运行' },
        { nodeId: 'test_review', iteration: 1, label: '测试评审', kind: 'auto', status: 'failed', at: '2026-10-22T08:07:44Z', endedAt: '2026-10-22T08:15:36Z', turns: [], open: false,
          handoff: '上游节点执行失败,未留下交接说明。失败原因:未调用 node_complete 标记完成', handoffSource: 'platform' },
      ],
    })
    await flushPromises()
    const current = w.findAll('[data-testid="clarify-incoming-handoff-from"]').at(-1)!
    expect(current.text()).toBe('「实现」的交接说明')
    expect(w.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe('侧栏已加取消运行')
    w.unmount()
  })

  const implementTask = '按需求与计划实现,本地测试通过后提交并推送工作分支。若测试评审未通过,先读 test_result 与 review 逐条修复,复用已有工作分支。'
  const statusIconNote = '状态图标按进行中、完成和等人区分。'

  it('a rerun after an empty failed visit still opens with that upstream note, matching the folded visit', async () => {
    nodeEvents.mockImplementation(async () => ({
      events: [{ kind: 'timeline', parts: [{ kind: 'message', text: '中断' }] }],
      live: false,
    }))
    const at = (m: number) => `2026-10-22T09:0${m}:00Z`
    const w = mountChat({
      nodeId: 'implement',
      nodeLabel: '实现',
      nodeTask: implementTask,
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: at(0), endedAt: at(1), open: true,
          turns: [{ role: 'human', text: '做导出', at: at(0) }], handoff: statusIconNote, handoffSource: 'agent' },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'failed', at: at(1), endedAt: at(2), turns: [], open: false },
        { nodeId: 'implement', iteration: 2, label: '实现', kind: 'auto', status: 'failed', at: at(2), endedAt: at(3), turns: [], open: false },
      ],
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    const current = w.get('[data-testid="clarify-incoming-handoff"]')
    expect(current.attributes('data-source')).toBe('agent')
    expect(current.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「需求澄清」的交接说明')
    expect(current.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(statusIconNote)
    const rows = w.findAll('[data-testid="clarify-prior-segment"][data-node-id="implement"]')
    expect(rows).toHaveLength(2)
    for (const row of rows) {
      ;(row.element as HTMLDetailsElement).open = true
      await row.trigger('toggle')
    }
    await flushPromises()
    const folded = w.findAll('[data-testid="clarify-auto-record"] [data-testid="clarify-incoming-handoff"]')
    expect(folded).toHaveLength(2)
    for (const bubble of folded) {
      expect(bubble.attributes('data-source')).toBe('agent')
      expect(bubble.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「需求澄清」的交接说明')
      expect(bubble.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(statusIconNote)
    }
    w.unmount()
  })

  it('keeps walking past another node that left an empty note', async () => {
    const w = mountChat({
      nodeId: 'implement',
      nodeLabel: '实现',
      nodeTask: implementTask,
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-22T09:00:00Z', open: false,
          turns: [], handoff: statusIconNote },
        { nodeId: 'design', iteration: 1, label: '方案设计', kind: 'auto', status: 'failed', at: '2026-10-22T09:01:00Z', turns: [], open: false, handoff: '   ' },
      ],
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「需求澄清」的交接说明')
    expect(w.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(statusIconNote)
    w.unmount()
  })

  it('a rerun ignores its own platform failure note and keeps the upstream that routed it in', async () => {
    const w = mountChat({
      nodeId: 'implement',
      nodeLabel: '实现',
      nodeTask: implementTask,
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-22T09:00:00Z', open: false,
          turns: [], handoff: statusIconNote, handoffSource: 'agent' },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'failed', at: '2026-10-22T09:01:00Z', turns: [], open: false,
          handoff: '上游节点执行失败,未留下交接说明。失败原因:未调用 node_complete 标记完成', handoffSource: 'platform' },
      ],
    })
    await flushPromises()
    const current = w.get('[data-testid="clarify-incoming-handoff"]')
    expect(current.attributes('data-source')).toBe('agent')
    expect(current.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「需求澄清」的交接说明')
    expect(current.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(statusIconNote)
    expect(w.text()).not.toContain('上游节点执行失败')
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    w.unmount()
  })

  it('a return from test review opens with that review, not an earlier clarify note or this node\'s failed note', async () => {
    const clarifyNote = '需求澄清更早的交接,关于状态图标。'
    const reviewNote = '测试未通过:开场气泡仍是任务句,请改选择规则。'
    const failedNote = '上游节点执行失败,未留下交接说明。失败原因:未调用 node_complete 标记完成'
    const chain = [
      { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat' as const, status: 'completed', at: '2026-10-22T09:00:00Z', open: false,
        turns: [], handoff: clarifyNote, handoffSource: 'agent' as const },
      { nodeId: 'test_review', iteration: 1, label: '测试评审', kind: 'auto' as const, status: 'completed', at: '2026-10-22T09:02:00Z', turns: [], open: false,
        handoff: reviewNote, handoffSource: 'agent' as const },
      { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto' as const, status: 'failed', at: '2026-10-22T09:03:00Z', turns: [], open: false,
        handoff: failedNote, handoffSource: 'platform' as const },
    ]
    const w = mountChat({ nodeId: 'implement', nodeLabel: '实现', nodeTask: implementTask, priorSegments: chain })
    await flushPromises()
    const current = w.get('[data-testid="clarify-incoming-handoff"]')
    expect(current.attributes('data-source')).toBe('agent')
    expect(current.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「测试评审」的交接说明')
    expect(current.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(reviewNote)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    await w.setProps({
      priorSegments: [
        chain[0],
        { ...chain[1], handoff: '上游未写交接,平台按测试结论代写。', handoffSource: 'platform' as const },
        { ...chain[2], handoff: '' },
      ],
    })
    await flushPromises()
    const platform = w.get('[data-testid="clarify-incoming-handoff"]')
    expect(platform.attributes('data-source')).toBe('platform')
    expect(platform.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「测试评审」未留交接，平台代写')
    expect(platform.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe('上游未写交接,平台按测试结论代写。')
    expect(w.text()).not.toContain(clarifyNote)
    w.unmount()
  })

  it('shows the node task only when no visit in the chain left a note', async () => {
    const w = mountChat({
      nodeId: 'implement',
      nodeLabel: '实现',
      nodeTask: implementTask,
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-22T09:00:00Z', open: false, turns: [] },
        { nodeId: 'design', iteration: 1, label: '方案设计', kind: 'auto', status: 'completed', at: '2026-10-22T09:01:00Z', turns: [], open: false, handoff: '  ' },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'failed', at: '2026-10-22T09:02:00Z', turns: [], open: false },
      ],
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-incoming-handoff"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-node-task"] .chat-user-message').text()).toBe(implementTask)
    await w.setProps({ nodeTask: '   ' })
    await flushPromises()
    expect(w.find('[data-testid="clarify-incoming-handoff"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    w.unmount()
  })

  it('opens an auto row and shows each node record', async () => {
    nodeEvents.mockImplementation(async (_run: string, nodeId: string) => {
      if (nodeId === 'implement') {
        return { events: [{ kind: 'timeline', parts: [{ kind: 'tool', title: 'Bash', status: 'completed' }, { kind: 'message', text: '实现完成' }] }], live: false }
      }
      return { events: [{ kind: 'timeline', parts: [{ kind: 'thought', text: '跑测试' }, { kind: 'message', text: '测试通过' }] }], live: false }
    })
    const w = mountChat({
      priorSegments: [
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:00Z', endedAt: '2026-10-21T05:00:06Z', turns: [], open: false },
        { nodeId: 'test_review', iteration: 1, label: '测试评审', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:07Z', endedAt: '2026-10-21T05:01:30Z', turns: [], open: false },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    const row = w.get('[data-testid="clarify-prior-segment"]')
    expect(row.element.tagName).toBe('DETAILS')
    expect(row.text()).toContain('「实现、测试评审」自动执行')
    expect(row.find('[data-testid="clarify-auto-record"]').exists()).toBe(false)
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    const records = w.findAll('[data-testid="clarify-auto-record"]')
    expect(records.map((r) => r.attributes('data-node-id'))).toEqual(['implement', 'test_review'])
    expect(records[0].text()).toContain('实现完成')
    expect(records[1].text()).toContain('测试通过')
    // Each node reads like a chat reply: its own 用时 fold, no extra node caption.
    expect(w.find('[data-testid="clarify-auto-record-label"]').exists()).toBe(false)
    expect(records.map((r) => r.get('[data-testid="agent-timeline-elapsed-time"]').text())).toEqual(['用时 6s', '用时 1m 23s'])
    expect(nodeEvents).toHaveBeenCalledWith('run-1', 'implement', expect.anything())
    expect(nodeEvents).toHaveBeenCalledWith('run-1', 'test_review', expect.anything())
    w.unmount()
  })

  it('shows the three boot steps while this node boots, and each finished node keeps its own', async () => {
    clearBootRecords()
    nodeEvents.mockResolvedValue({ events: [{ kind: 'timeline', parts: [{ kind: 'message', text: '实现完成' }] }], live: false })
    seedBoot(bootKey('run-1', 'implement', 1), '2026-10-21T05:00:00Z', { status: 'ready', readyAt: '2026-10-21T05:00:28Z' })
    seedBoot(bootKey('run-1', 'test_review', 1), '2026-10-21T05:01:00Z', { status: 'creating' })
    const live: ClarifyTurn[] = [{ role: 'agent', text: '', at: '2026-10-21T05:01:00Z', streaming: true }]
    const w = mountChat({
      nodeId: 'test_review',
      turns: live,
      observe: true,
      priorSegments: [
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:00Z', endedAt: '2026-10-21T05:00:59Z', turns: [], open: false },
      ],
      nodeLabel: '测试评审',
    })
    await flushPromises()
    // Booting: the checklist stands in for 思考中.
    const loader = w.get('[data-testid="clarify-boot-loader"]')
    expect(loader.findAll('[data-testid="clarify-boot-step"]').map((s) => s.attributes('data-state'))).toEqual(['running', 'pending', 'pending'])
    expect(w.text()).not.toContain('思考中')
    // History: the finished node opens with its boot row (time from the server).
    const row = w.get('[data-testid="clarify-prior-segment"]')
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    const record = w.get('[data-testid="clarify-auto-record"]')
    expect(record.get('[data-testid="clarify-boot-done-meta"]').text()).toBe('3 步 · 28s')
    w.unmount()
    clearBootRecords()
  })

  it('fills the reply from the narration when the timeline has no message', async () => {
    const handoff = '交接说明只留在气泡里'
    const narration = '这次执行的完整回复'
    nodeEvents.mockResolvedValue({
      events: [
        { kind: 'message', text: narration },
        { kind: 'timeline', parts: [
          { kind: 'thought', text: '想过' },
          { kind: 'tool', title: 'Shell', status: 'completed' },
          { kind: 'message', text: '   ' },
        ] },
      ],
      live: false,
    })
    const w = mountChat({
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-21T05:00:00Z', open: false,
          turns: [{ role: 'human', text: '需求', at: '2026-10-21T05:00:00Z' }], handoff },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:10Z', endedAt: '2026-10-21T05:10:34Z', turns: [], open: false },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    const row = w.get('[data-testid="clarify-prior-segment"][data-node-id="implement"]')
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    const record = w.get('[data-testid="clarify-auto-record"]')
    expect(record.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(handoff)
    expect(record.get('[data-testid="clarify-prior-agent-message"]').text()).toBe(narration)
    expect(record.findAll('[data-testid="clarify-prior-agent-message"]')).toHaveLength(1)
    w.unmount()
  })

  it('does not invent a reply when the timeline and the narration are both empty', async () => {
    const handoff = '不要把交接说明当成回复'
    nodeEvents.mockResolvedValue({
      events: [
        { kind: 'message', text: '上一段的回复' },
        { kind: 'segment' },
        { kind: 'message', text: '  \n' },
        { kind: 'timeline', parts: [
          { kind: 'tool', title: 'Shell', status: 'failed' },
          { kind: 'tool', title: 'Read', status: 'completed' },
        ] },
      ],
      live: false,
    })
    const w = mountChat({
      priorSegments: [
        { nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-21T05:00:00Z', open: false,
          turns: [], handoff },
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:10Z', endedAt: '2026-10-21T05:10:47Z', turns: [], open: false },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    const row = w.get('[data-testid="clarify-prior-segment"][data-node-id="implement"]')
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    const record = w.get('[data-testid="clarify-auto-record"]')
    expect(record.get('[data-testid="clarify-incoming-handoff-text"]').text()).toBe(handoff)
    expect(record.find('[data-testid="clarify-prior-agent-message"]').exists()).toBe(false)
    expect(record.get('[data-testid="agent-timeline-elapsed-failed"]').text()).toBe('1 个失败')
    expect(record.text()).not.toContain('上一段的回复')
    w.unmount()
  })

  it('does not append the narration again when the timeline already has a message', async () => {
    nodeEvents.mockResolvedValue({
      events: [
        { kind: 'message', text: '完整叙述不该再贴一份' },
        { kind: 'timeline', parts: [
          { kind: 'message', text: '时间线里的回复' },
          { kind: 'tool', title: 'Read', status: 'completed' },
        ] },
      ],
      live: false,
    })
    const w = mountChat({
      priorSegments: [
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:00Z', endedAt: '2026-10-21T05:00:20Z', turns: [], open: false },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    const row = w.get('[data-testid="clarify-prior-segment"]')
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    const record = w.get('[data-testid="clarify-auto-record"]')
    expect(record.findAll('[data-testid="clarify-prior-agent-message"]').map((m) => m.text())).toEqual(['时间线里的回复'])
    expect(record.text()).not.toContain('完整叙述不该再贴一份')
    w.unmount()
  })

  it('says when an auto record cannot be loaded', async () => {
    nodeEvents.mockRejectedValue(new Error('down'))
    const w = mountChat({
      priorSegments: [
        { nodeId: 'implement', iteration: 1, label: '实现', kind: 'auto', status: 'completed', at: '2026-10-21T05:00:00Z', turns: [], open: false },
      ],
      nodeLabel: '交付',
    })
    await flushPromises()
    const row = w.get('[data-testid="clarify-prior-segment"]')
    ;(row.element as HTMLDetailsElement).open = true
    await row.trigger('toggle')
    await flushPromises()
    expect(w.get('[data-testid="clarify-auto-record-status"]').text()).toContain('执行记录没有加载出来')
    w.unmount()
  })

})

describe('ClarifyChat locked composer while following the run (v3)', () => {
  it('gate segment: 人工审批 line, foldable turns when present, line only without turns', async () => {
    const at = '2026-10-21T05:00:00+08:00'
    const gate = { nodeId: 'gate', iteration: 1, label: '发布审批', kind: 'gate', status: 'completed', at, open: false }
    const w = mountChat({
      priorSegments: [
        { ...gate, decision: 'reject', turns: [{ role: 'human', text: '按钮颜色不对，打回', at }, { role: 'agent', text: '已按意见修改。', at }] },
        { ...gate, iteration: 2, turns: [] },
      ],
      nodeLabel: '实现',
    })
    await flushPromises()
    const segs = w.findAll('[data-testid="clarify-prior-segment"]')
    expect(segs).toHaveLength(2)
    expect(segs[0].element.tagName).toBe('DETAILS')
    expect(segs[0].attributes('data-kind')).toBe('gate')
    expect(segs[0].get('[data-testid="clarify-prior-summary"]').text()).toContain('「发布审批」人工审批 · 已打回')
    expect(segs[0].findAll('[data-turn-role]')).toHaveLength(2)
    expect(segs[1].element.tagName).toBe('DIV')
    expect(segs[1].text()).toContain('「发布审批」人工审批')
    expect(segs[1].find('[data-turn-role]').exists()).toBe(false)
    w.unmount()
  })

  it('validating / pending handoff keeps input and attach available; draft does not send', async () => {
    const w = mountChat({ handoff: { phase: 'pending', at: '' } })
    await flushPromises()
    const input = w.get('[data-testid="clarify-input"]')
    const attach = w.get('[data-testid="clarify-attach-btn"]')
    expect(input.attributes('disabled')).toBeUndefined()
    expect(attach.attributes('disabled')).toBeUndefined()
    const picker = w.get('input[type="file"]').element as HTMLInputElement
    const openPicker = vi.spyOn(picker, 'click').mockImplementation(() => {})
    await attach.trigger('click')
    expect(openPicker).toHaveBeenCalledOnce()
    openPicker.mockRestore()
    await input.setValue('补充目标描述')
    expect((input.element as HTMLTextAreaElement).value).toBe('补充目标描述')
    const confirm = w.get('[data-testid="clarify-confirm-flow"]')
    expect(confirm.text()).toContain('校验中')
    expect(confirm.attributes('disabled')).toBeDefined()
    await confirm.trigger('click')
    await flushPromises()
    expect(w.emitted('finish')).toBeUndefined()
    expect(w.emitted('send')).toBeUndefined()
    expect((input.element as HTMLTextAreaElement).value).toBe('补充目标描述')
    w.unmount()
  })

  it('after 确认并流转 click, typing stays local until validation fails', async () => {
    const w = mountChat()
    await flushPromises()
    const confirm = w.findAll('button').find((b) => b.text().includes('确认并流转'))
    expect(confirm).toBeTruthy()
    await confirm!.trigger('click')
    await flushPromises()
    const accept = (w.find('[data-testid="confirm-flow-dialog-accept"]').exists()
      ? w.get('[data-testid="confirm-flow-dialog-accept"]').element
      : document.querySelector('[data-testid="confirm-flow-dialog-accept"]')) as HTMLElement
    accept.click()
    await flushPromises()
    expect(w.emitted('finish')?.length).toBe(1)
    const input = w.get('[data-testid="clarify-input"]')
    expect(input.attributes('disabled')).toBeUndefined()
    expect(w.get('[data-testid="clarify-attach-btn"]').attributes('disabled')).toBeUndefined()
    await input.setValue('校验中补充的草稿')
    const busy = w.get('[data-testid="clarify-confirm-flow"]')
    expect(busy.text()).toContain('校验中')
    expect((busy.element as HTMLButtonElement).disabled).toBe(true)
    await busy.trigger('click')
    await flushPromises()
    expect(w.emitted('send')).toBeUndefined()
    expect(w.emitted('finish')?.length).toBe(1)
    await w.setProps({ confirmError: '产物契约未满足' })
    await flushPromises()
    expect(w.text()).not.toContain('校验中…')
    expect((input.element as HTMLTextAreaElement).value).toBe('校验中补充的草稿')
    const send = w.get('[data-testid="clarify-send-icon"]')
    expect((send.element as HTMLButtonElement).disabled).toBe(false)
    await send.trigger('click')
    await flushPromises()
    expect(w.emitted('send')?.[0]?.[0]).toBe('校验中补充的草稿')
    w.unmount()
  })

  it('keeps the same composer mounted, disabled — never the closed / done footer', async () => {
    const at = new Date().toISOString()
    const w = mountChat({ handoff: { phase: 'done', at }, followTail: { kind: 'ended', status: 'completed', at }, composerLocked: true })
    await flushPromises()
    expect(w.find('[data-testid="clarify-hot-actions"]').exists()).toBe(true)
    expect(w.get('[data-testid="clarify-input"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="clarify-attach-btn"]').attributes('disabled')).toBeDefined()
    expect(w.text()).not.toContain('交互已完成')
    expect(w.text()).not.toContain('交互已关闭')
    await w.setProps({ composerLocked: false, handoff: null, followTail: null })
    expect(w.get('[data-testid="clarify-input"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('composerLocked locks the single primary action (确认并流转 and 发送) while following', async () => {
    const at = new Date().toISOString()
    const w = mountChat({ handoff: { phase: 'done', at }, followTail: { kind: 'auto', label: '实现', at }, composerLocked: true })
    await flushPromises()
    const confirm = w.get('[data-testid="clarify-confirm-flow"]')
    expect(confirm.attributes('disabled')).toBeDefined()
    await confirm.trigger('click')
    await flushPromises()
    expect(w.emitted('finish')).toBeUndefined()
    // A draft turns the primary into 发送; it stays locked as well.
    await w.setProps({ draft: '还有一点' })
    const send = w.get('[data-testid="clarify-send-icon"]')
    expect(send.attributes('disabled')).toBeDefined()
    await send.trigger('click')
    await flushPromises()
    expect(w.emitted('send')).toBeUndefined()
    // The next node waits for the user: unlocked again.
    await w.setProps({ composerLocked: false, handoff: null, followTail: null })
    expect(w.get('[data-testid="clarify-send-icon"]').attributes('disabled')).toBeUndefined()
    await w.setProps({ draft: '' })
    expect(w.get('[data-testid="clarify-confirm-flow"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })
})

describe('ClarifyChat done footer: specific ended state (run live page)', () => {
  it.each([
    [{ text: '「上线审批」人工审批 · 待审批', tone: 'wait' }, 'text-txt3'],
    [{ text: '运行已完成', tone: 'ok' }, 'text-ok'],
    [{ text: '运行失败', tone: 'muted' }, 'text-txt3'],
  ] as const)('%o replaces the generic 「交互已完成，进入下一步」 in the same footer', async (note, cls) => {
    const w = mountChat({ done: true, doneNote: note })
    await flushPromises()
    const f = w.get('[data-testid="clarify-done-note"]')
    expect(f.text()).toBe(note.text)
    expect(f.classes()).toContain(cls)
    expect(f.classes()).toEqual(expect.arrayContaining(['border-t', 'border-line', 'p-3', 'text-center', 'text-[12px]']))
    expect(w.text()).not.toContain('交互已完成，进入下一步')
    w.unmount()
  })

  it('without a note the generic done footer stays (/gates and older callers)', async () => {
    const w = mountChat({ done: true })
    await flushPromises()
    expect(w.find('[data-testid="clarify-done-note"]').exists()).toBe(false)
    expect(w.text()).toContain('交互已完成，进入下一步')
    w.unmount()
  })
})
