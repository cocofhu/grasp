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

describe('AgentTimeline', () => {
  it('renders steps in arrival order with consecutive tools folded', () => {
    const w = mountTimeline({ completed: true })
    expect(order(w)).toEqual(['agent-timeline-thought', 'agent-tool-group', 'agent-timeline-message', 'agent-tool-group', 'agent-timeline-message'])
    expect(w.findAll('[data-testid="agent-tool-group"]')[0]!.text()).toContain('使用了 2 个工具')
    const msgs = w.findAll('[data-testid="agent-timeline-message"]')
    expect(msgs[0]!.html()).toContain('<strong>bold</strong>')
    expect(msgs[0]!.classes()).toContain('rounded-lg')
    expect(w.find('[data-testid="stream-md"]').exists()).toBe(false)
    expect((w.find('[data-testid="agent-timeline-thought"]').element as HTMLDetailsElement).open).toBe(false)
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

  it('remembers a thought the user opened', async () => {
    const w = mountTimeline({ completed: true })
    const el = w.find('[data-testid="agent-timeline-thought"]')
    ;(el.element as HTMLDetailsElement).open = true
    await el.trigger('toggle')
    await w.setProps({ completed: true, parts: [...parts, { kind: 'message', text: 'third' }] })
    expect((el.element as HTMLDetailsElement).open).toBe(true)
  })

  // plan coverage: g2.2 — timeline parts render thought and message in full.
  it('renders long thought and message without a truncated suffix (g2.2)', () => {
    const longThought = `${'思'.repeat(3000)}思考结尾END`
    const longMessage = `${'回'.repeat(3000)}结尾标记END`
    expect(new TextEncoder().encode(longThought).length).toBeGreaterThan(8000)
    expect(new TextEncoder().encode(longMessage).length).toBeGreaterThan(8000)
    const w = mountTimeline({
      completed: true,
      parts: [
        { kind: 'thought', text: longThought },
        { kind: 'message', text: longMessage },
      ],
    })
    const thought = w.get('[data-testid="agent-timeline-thought"] .whitespace-pre-wrap')
    expect(thought.text()).toBe(longThought)
    expect(thought.text()).not.toContain('…(truncated)')
    expect(thought.text()).not.toContain('...(truncated)')
    const message = w.get('[data-testid="agent-timeline-message"]')
    expect(message.text()).toBe(longMessage)
    expect(message.text()).not.toContain('…(truncated)')
    expect(message.text()).not.toContain('...(truncated)')
  })

  it('follows expand-all, hides thoughts on request, and draws bare messages', () => {
    const w = mountTimeline({ expanded: true, bare: true })
    expect((w.find('[data-testid="agent-timeline-thought"]').element as HTMLDetailsElement).open).toBe(true)
    expect(w.findAll('[data-testid="agent-tool-list"]')).toHaveLength(2)
    expect(w.find('[data-testid="agent-timeline-message"]').classes()).not.toContain('rounded-lg')
    const hidden = mountTimeline({ hideThought: true })
    expect(hidden.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
  })
})
