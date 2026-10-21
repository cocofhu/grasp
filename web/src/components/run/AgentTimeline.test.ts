// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { AgentPart } from '@/lib/shared/types'
import AgentTimeline from './AgentTimeline.vue'

const parts: AgentPart[] = [
  { kind: 'thought', text: 'plan it' },
  { kind: 'tool', title: 'Shell', status: 'completed', summary: 'ls' },
  { kind: 'tool', title: 'Read', status: 'completed' },
  { kind: 'message', text: 'first **bold**' },
  { kind: 'tool', title: 'Write', status: 'running' },
  { kind: 'message', text: 'second' },
]

function mountTimeline(props: Record<string, unknown>, slots: Record<string, string> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(AgentTimeline, { props: { parts, ...props }, slots, global: { plugins: [i18n], stubs: { Icon: true } } })
}

const order = (w: ReturnType<typeof mountTimeline>) =>
  Array.from(w.find('[data-testid="agent-timeline"]').element.children).map((el) => el.getAttribute('data-testid'))

const startedAt = '2026-10-20T00:00:00.000Z'
const endedAt = '2026-10-20T00:03:34.000Z'

describe('AgentTimeline', () => {
  it('renders steps in arrival order with consecutive tools folded', async () => {
    const w = mountTimeline({ completed: true, startedAt, endedAt })
    expect(order(w)).toEqual(['agent-timeline-elapsed', 'agent-timeline-rule', 'agent-timeline-message'])
    expect(w.get('[data-testid="agent-timeline-elapsed"]').text()).toContain('用时 3m 34s')
    expect(w.get('[data-testid="agent-timeline-elapsed"]').text()).not.toContain('使用了')
    expect(w.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="agent-timeline-message"]').map((m) => m.text())).toEqual(['second'])
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    const work = w.get('[data-testid="agent-timeline-work"]')
    expect(Array.from(work.element.children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'agent-timeline-thought', 'agent-tool-group', 'agent-timeline-message', 'agent-tool-group',
    ])
    expect(work.findAll('[data-testid="agent-tool-group"]')[0]!.text()).toContain('使用了 2 个工具')
    const msgs = w.findAll('[data-testid="agent-timeline-message"]')
    expect(msgs[0]!.html()).toContain('<strong>bold</strong>')
    expect(msgs.map((m) => m.text())).toEqual(['first bold', 'second'])
    expect(w.find('[data-testid="stream-md"]').exists()).toBe(false)
    expect(w.text()).not.toContain('已完成')
    expect(w.text()).not.toContain('思考过程')
  })

  it('streams only the last message and keeps the caret on it', () => {
    const w = mountTimeline({ streaming: true, messageTestId: 'm' }, { caret: '<i data-testid="caret" />' })
    const msgs = w.findAll('[data-testid="m"]')
    expect(msgs).toHaveLength(2)
    expect(msgs[0]!.find('[data-testid="caret"]').exists()).toBe(false)
    expect(msgs[1]!.find('[data-testid="caret"]').exists()).toBe(true)
    expect(w.findAll('[data-testid="stream-md"]')).toHaveLength(1)
    expect(msgs[1]!.find('[data-testid="stream-md"]').text()).toContain('second')
    expect(w.findAll('[data-testid="agent-tool-group"]')[1]!.attributes('data-state')).toBe('running')
  })

  it('reveals the message being written gradually and shows it whole once the turn ends', async () => {
    vi.stubEnv('VITEST', '')
    vi.useFakeTimers({ toFake: ['setTimeout', 'requestAnimationFrame', 'cancelAnimationFrame'] })
    try {
      const text = 'x'.repeat(200)
      const w = mountTimeline({ streaming: true, parts: [{ kind: 'message', text }] })
      const shown = () => w.find('[data-testid="agent-timeline-message"]').text().length
      await vi.advanceTimersByTimeAsync(100)
      expect(shown()).toBeGreaterThan(0)
      expect(shown()).toBeLessThan(text.length)
      await w.setProps({ streaming: false, completed: true })
      expect(shown()).toBe(text.length)
    } finally {
      vi.useRealTimers()
      vi.unstubAllEnvs()
    }
  })

  it('keeps the thought being written open and closes it when the next step lands', async () => {
    const w = mountTimeline({ streaming: true, parts: [{ kind: 'thought', text: 'hmm' }] })
    const details = () => w.find('[data-testid="agent-timeline-thought"]').element as HTMLDetailsElement
    expect(details().open).toBe(true)
    await w.find('[data-testid="agent-timeline-thought"]').trigger('toggle')
    await w.setProps({ parts: [{ kind: 'thought', text: 'hmm' }, { kind: 'message', text: 'ok' }] })
    expect(details().open).toBe(false)
  })

  it('remembers a reply the user opened', async () => {
    const w = mountTimeline({ completed: true, startedAt, endedAt })
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(true)
    await w.setProps({
      completed: true,
      startedAt,
      endedAt,
      parts: [...parts, { kind: 'tool', title: 'Grep', status: 'completed' }, { kind: 'message', text: 'third' }],
    })
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(true)
    expect(w.findAll('[data-testid="agent-timeline-message"]').at(-1)!.text()).toBe('third')
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(false)
    await w.setProps({ completed: true })
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(false)
  })

  // plan coverage: g2.2 — timeline parts render thought and message in full.
  it('renders long thought and message without a truncated suffix (g2.2)', async () => {
    const longThought = `${'思'.repeat(3000)}思考结尾END`
    const longMessage = `${'回'.repeat(3000)}结尾标记END`
    expect(new TextEncoder().encode(longThought).length).toBeGreaterThan(8000)
    expect(new TextEncoder().encode(longMessage).length).toBeGreaterThan(8000)
    const w = mountTimeline({
      completed: true,
      startedAt,
      endedAt,
      parts: [
        { kind: 'thought', text: longThought },
        { kind: 'message', text: longMessage },
      ],
    })
    await w.get('[data-testid="agent-timeline-elapsed"]').trigger('click')
    const thought = w.get('[data-testid="agent-timeline-thought"]')
    expect(thought.text()).toBe(longThought)
    expect(thought.text()).not.toContain('…(truncated)')
    expect(thought.text()).not.toContain('...(truncated)')
    const message = w.get('[data-testid="agent-timeline-message"]')
    expect(message.text()).toBe(longMessage)
    expect(message.text()).not.toContain('…(truncated)')
    expect(message.text()).not.toContain('...(truncated)')
  })

  it('keeps script text in assistant markdown and still renders bold and code', () => {
    const text = '说明如下：<script>关键步骤</script>\n\n**加粗**\n\n```js\nconst x = 1\n```'
    const w = mountTimeline({
      completed: true,
      parts: [{ kind: 'message', text }],
    })
    const message = w.get('[data-testid="agent-timeline-message"]')
    expect(message.text()).toContain('说明如下：')
    expect(message.text()).toContain('关键步骤')
    expect(message.text()).toContain('<script>')
    expect(message.find('strong').text()).toBe('加粗')
    expect(message.find('code').text()).toContain('const x = 1')
    expect(message.element.querySelector('script')).toBeNull()
  })

  it('shows script text while the assistant message is still streaming', () => {
    const w = mountTimeline({
      streaming: true,
      parts: [{ kind: 'message', text: '说明如下：<script>关键步骤</script>\n\n**加粗**' }],
    })
    const message = w.get('[data-testid="agent-timeline-message"]')
    expect(message.find('[data-testid="stream-md"]').exists()).toBe(true)
    expect(message.text()).toContain('关键步骤')
    expect(message.text()).toContain('加粗')
    expect(message.element.querySelector('script')).toBeNull()
  })

  it('follows expand-all, hides thoughts on request, and draws bare messages', () => {
    const w = mountTimeline({ expanded: true, bare: true, startedAt, endedAt })
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(true)
    expect(w.find('[data-testid="agent-timeline-thought"]').text()).toContain('plan it')
    expect(w.findAll('[data-testid="agent-tool-list"]')).toHaveLength(2)
    expect(w.find('[data-testid="agent-timeline-message"]').classes()).not.toContain('rounded-lg')
    const hidden = mountTimeline({ hideThought: true, expanded: true, startedAt, endedAt })
    expect(hidden.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
    expect(hidden.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(true)
  })

  it('keeps a plain elapsed line: no card, no tool count, body once under the rule', () => {
    const w = mountTimeline({ completed: true, startedAt, endedAt })
    const btn = w.get('[data-testid="agent-timeline-elapsed"]')
    expect(btn.element.tagName).toBe('BUTTON')
    expect(btn.attributes('aria-expanded')).toBe('false')
    expect(btn.classes().some((c) => c.startsWith('bg-'))).toBe(false)
    expect(btn.text()).toBe('用时 3m 34s')
    expect(btn.text()).not.toMatch(/使用了|已完成|个工具/)
    const rule = w.get('[data-testid="agent-timeline-rule"]').element
    const msg = w.get('[data-testid="agent-timeline-message"]').element
    expect(rule.compareDocumentPosition(msg) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(w.findAll('[data-testid="agent-timeline-message"]')).toHaveLength(1)
    const bg = getComputedStyle(btn.element).backgroundColor
    expect(bg === 'rgba(0, 0, 0, 0)' || bg === 'transparent' || bg === '').toBe(true)
  })

  it('does not invent a duration or fold a body-only reply', () => {
    const missing = mountTimeline({ completed: true })
    const btn = missing.get('[data-testid="agent-timeline-elapsed"]')
    expect(btn.text()).not.toMatch(/用时|0s|分钟前/)
    expect(missing.find('[data-testid="agent-timeline-rule"]').exists()).toBe(true)
    const bare = mountTimeline({
      completed: true,
      startedAt,
      endedAt,
      parts: [{ kind: 'message', text: '只有正文' }],
    })
    expect(bare.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(false)
    expect(bare.find('[data-testid="agent-timeline-rule"]').exists()).toBe(false)
    expect(bare.get('[data-testid="agent-timeline-message"]').text()).toBe('只有正文')
  })

  it('does not fold while the reply is still streaming', () => {
    const w = mountTimeline({ streaming: true, startedAt, endedAt })
    expect(w.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-timeline-rule"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-timeline-thought"]').exists()).toBe(true)
  })

  it('folds a finished reply with failed tool calls and says how many failed', async () => {
    const failed = mountTimeline({
      completed: true,
      startedAt,
      endedAt,
      parts: [
        { kind: 'thought', text: '先跑测试' },
        { kind: 'tool', title: 'Read', status: 'completed' },
        { kind: 'tool', title: 'Shell', status: 'failed', output: 'exit 1' },
        { kind: 'message', text: '没做成' },
      ],
    })
    const line = failed.get('[data-testid="agent-timeline-elapsed"]')
    expect(line.find('[data-testid="agent-timeline-elapsed-time"]').exists()).toBe(true)
    expect(line.get('[data-testid="agent-timeline-elapsed-failed"]').text()).toBe('1 个失败')
    expect(line.attributes('aria-label')).toContain('1 个失败')
    expect(failed.find('[data-testid="agent-timeline-work"]').exists()).toBe(false)
    expect(failed.get('[data-testid="agent-timeline-message"]').text()).toBe('没做成')
    await line.trigger('click')
    const group = failed.get('[data-testid="agent-tool-group"]')
    expect(group.text()).toContain('1 个失败')
    expect(group.text()).toContain('运行命令')
    const ok = mountTimeline({ completed: true, startedAt, endedAt })
    expect(ok.find('[data-testid="agent-timeline-elapsed-failed"]').exists()).toBe(false)
    const stopped = mountTimeline({
      interrupted: true,
      startedAt: '2026-10-20T00:00:00.000Z',
      endedAt: '2026-10-20T00:00:42.000Z',
      parts: [
        { kind: 'thought', text: '半截' },
        { kind: 'message', text: '停在这里' },
      ],
    })
    expect(stopped.get('[data-testid="agent-timeline-elapsed"]').text()).toContain('用时 42s')
    expect(stopped.get('[data-testid="agent-timeline-elapsed"]').text()).toContain('已中断')
    expect(stopped.text()).not.toContain('已完成')
    const sub = mountTimeline({
      completed: true,
      startedAt: '2026-10-20T00:00:00.000Z',
      endedAt: '2026-10-20T00:00:00.400Z',
      parts: [
        { kind: 'tool', title: 'Read', status: 'completed', durationMs: 9_999_000 },
        { kind: 'message', text: '很快' },
      ],
    })
    expect(sub.get('[data-testid="agent-timeline-elapsed"]').text()).toBe('用时 1s')
  })

  it('names the fold when the reply has no start/end, instead of a bare chevron', () => {
    const w = mountTimeline({ completed: true })
    const line = w.get('[data-testid="agent-timeline-elapsed"]')
    expect(line.find('[data-testid="agent-timeline-elapsed-time"]').exists()).toBe(false)
    expect(line.get('[data-testid="agent-timeline-elapsed-process"]').text()).toBe('本轮过程')
  })

  it('opens the fold when expand-all is on, ahead of the default collapse', () => {
    const w = mountTimeline({ expanded: true, startedAt, endedAt })
    expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(true)
    expect(w.get('[data-testid="agent-timeline-elapsed"]').attributes('aria-expanded')).toBe('true')
  })

  // Root cause of "streaming looks finished": earlier thoughts were marked 已完成
  // per segment while the turn was still running.
  it('never marks a thought 已完成 while the turn is still streaming', async () => {
    const midTurn: AgentPart[] = [
      { kind: 'thought', text: 'first thought' },
      { kind: 'message', text: 'first note' },
      { kind: 'tool', title: 'Read', status: 'completed' },
      { kind: 'thought', text: 'second thought' },
      { kind: 'tool', title: 'Grep', status: 'completed' },
    ]
    const w = mountTimeline({ parts: midTurn, streaming: true })
    const states = () => w.findAll('[data-testid="thought-summary-state"]').map((el) => el.attributes('data-state'))
    expect(states()).toEqual(['idle', 'idle'])
    expect(w.text()).not.toContain('已完成')
    await w.setProps({ parts: [...midTurn, { kind: 'thought', text: 'third thought' }] })
    expect(states()).toEqual(['idle', 'idle', 'streaming'])
    // Once the turn ends, main (#843) folds the whole turn into the elapsed line.
    await w.setProps({ streaming: false, completed: true })
    expect(w.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(true)
    expect(states()).toEqual([])
  })
})
