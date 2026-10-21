// @vitest-environment happy-dom
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n as appI18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import { confirmFlowCeremonyKey } from '@/lib/inbox/confirmFlowCeremony'
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
    await w.setProps({ handoff: { phase: 'done', at: new Date().toISOString() } })
    const done = w.get('[data-testid="clarify-handoff"]')
    expect(done.attributes('data-state')).toBe('done')
    expect(done.text()).toContain('已确认并流转')
    expect(done.text()).toContain('已完成')
    expect(done.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('done')
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
    expect(tail().text()).toContain('「部署」正在自动执行')
    expect(tail().text()).toContain('完成后在此继续')
    expect(st()).toBe('streaming')
    await w.setProps({ followTail: { kind: 'waiting', label: '验收测试', at } })
    await flushPromises()
    expect(tail().text()).toContain('「验收测试」准备中')
    expect(tail().text()).toContain('就绪后可直接输入')
    await w.setProps({ followTail: { kind: 'waiting', at } })
    await flushPromises()
    expect(tail().text()).toContain('等待下一节点开始')
    await w.setProps({ followTail: { kind: 'error', at } })
    await flushPromises()
    expect(tail().text()).toContain('暂时无法获取运行进度')
    expect(tail().text()).toContain('重试中')
    await w.setProps({ followTail: { kind: 'ended', status: 'completed', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行已完成')
    expect(st()).toBe('done')
    await w.setProps({ followTail: { kind: 'ended', status: 'failed', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行失败')
    expect(tail().text()).not.toContain('已中断')
    expect(st()).toBe('interrupted')
    await w.setProps({ followTail: { kind: 'ended', status: 'cancelled', at } })
    await flushPromises()
    expect(tail().text()).toContain('运行已取消')
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
})
