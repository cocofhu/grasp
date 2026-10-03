// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
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

  it('follows expand-all, hides thoughts on request, and draws bare messages', () => {
    const w = mountTimeline({ expanded: true, bare: true })
    expect((w.find('[data-testid="agent-timeline-thought"]').element as HTMLDetailsElement).open).toBe(true)
    expect(w.findAll('[data-testid="agent-tool-list"]')).toHaveLength(2)
    expect(w.find('[data-testid="agent-timeline-message"]').classes()).not.toContain('rounded-lg')
    const hidden = mountTimeline({ hideThought: true })
    expect(hidden.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
  })
})
