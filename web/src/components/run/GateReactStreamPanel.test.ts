// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import GateReactStreamPanel from './GateReactStreamPanel.vue'
import { registerStageLinks } from '@/lib/run/stageLinks'

function mountStream(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(GateReactStreamPanel, { props, global: { plugins: [i18n], stubs: { Icon: true } } })
}

describe('GateReactStreamPanel', () => {
  it('keeps the complete thought, output and tool details through completion', async () => {
    const w = mountStream({ thinking: true })
    expect(w.get('[data-testid="gate-busy-placeholder"]').text()).toBeTruthy()
    const thought = 'Planning the response. '.repeat(50)
    await w.setProps({ streamThought: thought })
    const details = w.get('[data-testid="gate-react-thought"]')
    expect((details.element as HTMLDetailsElement).open).toBe(true)
    expect(details.text()).toContain(thought.trim())
    await w.setProps({ streamText: '**Complete response**', streamTools: [{ title: 'Read', status: 'completed', output: 'source contents' }] })
    expect((details.element as HTMLDetailsElement).open).toBe(false)
    expect(w.get('[data-testid="gate-react-message"]').html()).toContain('<strong>Complete response</strong>')
    expect(w.find('[data-testid="gate-stream-caret"]').exists()).toBe(true)
    await w.get('[data-testid="agent-tool-group-head"]').trigger('click')
    await w.get('[data-testid="agent-tool-row-toggle"]').trigger('click')
    expect(w.get('[data-testid="agent-tool-output"]').text()).toBe('source contents')
    await w.setProps({ thinking: false, completedAt: new Date().toISOString() })
    expect(w.find('[data-testid="gate-stream-caret"]').exists()).toBe(false)
    expect(w.find('[data-testid="gate-turn-completed"]').exists()).toBe(true)
    expect(w.get('[data-testid="gate-react-message"]').text()).toBe('Complete response')
    w.unmount()
  })

  it('preserves interrupted output and hides an unused stream', async () => {
    const w = mountStream()
    expect(w.find('[data-testid="gate-react-stream"]').exists()).toBe(false)
    await w.setProps({ interrupted: true, streamText: 'Partial response', streamThought: 'Partial thought' })
    expect(w.get('[data-testid="gate-react-message"]').text()).toContain('Partial response')
    expect(w.get('[data-testid="gate-react-thought"]').text()).toContain('Partial thought')
    expect(w.find('[data-testid="gate-turn-completed"]').exists()).toBe(false)
    w.unmount()
  })

  it('keeps tool artifact links attached to the explicitly supplied run', async () => {
    const openArtifact = vi.fn()
    const unregister = registerStageLinks('gate-run', {
      hasArtifact: name => name === 'plan.json', openArtifact,
      canOpenPreview: () => false, openPreview: vi.fn(),
    })
    try {
      const w = mountStream({ thinking: true, runId: 'gate-run', streamTools: [{ title: 'set_plan', status: 'completed' }] })
      await w.get('[data-testid="agent-tool-group-head"]').trigger('click')
      await w.get('[data-testid="agent-tool-open-artifact"]').trigger('click')
      expect(openArtifact).toHaveBeenCalledWith('plan.json')
      await w.setProps({ runId: undefined })
      expect(w.find('[data-testid="agent-tool-open-artifact"]').exists()).toBe(false)
      w.unmount()
    } finally {
      unregister()
    }
  })
})
