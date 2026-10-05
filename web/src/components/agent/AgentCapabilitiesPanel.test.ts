// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import type { AgentCapabilities } from '@/lib/api/apiTypes'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentCapabilitiesPanel from './AgentCapabilitiesPanel.vue'

function mountPanel(capabilities: AgentCapabilities | null) {
  const draft = reactive({ capabilities }) as unknown as AgentStudioDraft
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...nodes, ...pages } },
  })
  const wrapper = mount(AgentCapabilitiesPanel, { props: { draft }, global: { plugins: [i18n] } })
  return { wrapper, draft }
}

async function check(wrapper: ReturnType<typeof mountPanel>['wrapper'], testid: string, on: boolean) {
  const el = wrapper.get(`[data-testid="${testid}"]`)
  ;(el.element as HTMLInputElement).checked = on
  await el.trigger('change')
}

describe('AgentCapabilitiesPanel', () => {
  it('declares default capabilities for an undeclared Agent', async () => {
    const { wrapper, draft } = mountPanel(null)
    await wrapper.get('[data-testid="caps-declare"]').trigger('click')
    expect(draft.capabilities).toEqual({ interaction: 'auto', reads: ['*'] })
    expect(wrapper.find('[data-testid="caps-interaction-auto"]').attributes('aria-checked')).toBe('true')
  })

  it('switching to clarify grants ask_question and turns review off', async () => {
    const { wrapper, draft } = mountPanel({ interaction: 'auto', review: true, reads: ['*'] })
    await wrapper.get('[data-testid="caps-interaction-clarify"]').trigger('click')
    expect(draft.capabilities).toMatchObject({ interaction: 'clarify', review: false, tools: ['ask_question'] })
    expect(wrapper.get('[data-testid="caps-review"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="caps-error"]').exists()).toBe(false)
  })

  it('shows the server-mirrored error when clarify lacks ask_question', async () => {
    const { wrapper } = mountPanel({ interaction: 'clarify', tools: ['ask_question'] })
    await check(wrapper, 'caps-tool-ask_question', false)
    expect(wrapper.get('[data-testid="caps-error"]').text()).toContain('ask_question')
  })

  it('rejects duplicate writes and flags clarify + review', () => {
    const dup = mountPanel({ interaction: 'auto', writes: [{ schema: 'plan' }, { schema: 'plan' }] })
    expect(dup.wrapper.get('[data-testid="caps-error"]').text()).toContain('plan')
    const rev = mountPanel({ interaction: 'clarify', review: true, tools: ['ask_question'] })
    expect(rev.wrapper.get('[data-testid="caps-error"]').text()).toContain('复审')
  })

  it('edits review, tools, reads, writes and maxRounds', async () => {
    const { wrapper, draft } = mountPanel({ interaction: 'auto', reads: ['*'] })
    await wrapper.get('[data-testid="caps-review"]').trigger('click')
    expect(draft.capabilities?.review).toBe(true)

    await check(wrapper, 'caps-tool-set_preview', true)
    expect(draft.capabilities?.tools).toEqual(['set_preview'])

    await check(wrapper, 'caps-reads-all', false)
    expect(draft.capabilities?.reads).toEqual([])
    await check(wrapper, 'caps-read-plan', true)
    expect(draft.capabilities?.reads).toEqual(['plan.json'])
    await wrapper.get('[data-testid="caps-reads-other"]').setValue('notes.md, extra.txt')
    expect(draft.capabilities?.reads).toEqual(['plan.json', 'notes.md', 'extra.txt'])

    await check(wrapper, 'caps-write-test_result', true)
    expect(draft.capabilities?.writes).toEqual([{ schema: 'test_result' }])
    expect(wrapper.find('[data-testid="caps-gated"]').exists()).toBe(true)
    await check(wrapper, 'caps-write-required-test_result', true)
    expect(draft.capabilities?.writes).toEqual([{ schema: 'test_result', required: true }])
    await check(wrapper, 'caps-write-test_result', false)
    expect(draft.capabilities?.writes).toEqual([])

    await wrapper.get('[data-testid="caps-max-rounds"]').setValue('5')
    expect(draft.capabilities?.maxRounds).toBe(5)
    await wrapper.get('[data-testid="caps-max-rounds"]').setValue('')
    expect(draft.capabilities?.maxRounds).toBeUndefined()
  })
})
