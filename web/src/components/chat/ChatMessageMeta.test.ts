// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'
import pages from '@/locales/en/pages.json'
import ChatMessageMeta from './ChatMessageMeta.vue'
import type { ChatMessageMetadata } from '@/lib/shared/types'

const mounted: Array<{ unmount(): void }> = []
function setup(metadata?: ChatMessageMetadata) {
  const wrapper = mount(ChatMessageMeta, { attachTo: document.body, props: { metadata }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: pages } })] } })
  mounted.push(wrapper)
  return wrapper
}
const panel = () => document.querySelector<HTMLElement>('[data-testid="chat-message-info-panel"]')
const usage = { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 30, cacheWriteTokens: 0 }
afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount(); document.body.innerHTML = '' })

describe('shared reply information', () => {
  it('shows only recorded execution, duration and exact token breakdown on hover', async () => {
    const wrapper = setup({ createdAt: '2026-10-28T08:00:25Z', durationMs: 25000, execution: { agentName: 'Original Agent', backend: 'opencode', modelId: 'original/model', configurationId: 'original-group', reasoningEffort: 'high' }, usage, usageByModel: { 'original/model': usage } })
    expect(wrapper.get('time').attributes('title')).toContain('2026-10-28')
    await wrapper.get('button').trigger('pointerenter', { pointerType: 'mouse' })
    await flushPromises()
    expect(wrapper.get('button').attributes('aria-label')).toBe('Statistics')
    expect(panel()?.textContent).toContain('Statistics')
    expect(panel()?.textContent).not.toContain('Reply information')
    expect(panel()?.textContent).toContain('Original Agent')
    expect(panel()?.textContent).toContain('OpenCode')
    expect(panel()?.textContent).toContain('original/model')
    expect(panel()?.textContent).not.toContain('original-group')
    expect(panel()?.querySelector('[data-info-row="group"]')).toBeNull()
    expect(panel()?.textContent).toContain('high')
    expect(panel()?.textContent).toContain('25s')
    expect(panel()?.querySelector('[data-testid="chat-message-token-total"]')?.textContent).toBe('1,230')
    expect(panel()?.querySelector('[data-token-row="input"] dd')?.textContent).toBe('1,000')
    expect(panel()?.querySelector('[data-token-row="output"] dd')?.textContent).toBe('200')
    expect(panel()?.querySelector('[data-token-row="cacheRead"] dd')?.textContent).toBe('30')
    expect(panel()?.querySelector('[data-token-row="cacheWrite"] dd')?.textContent).toBe('0')
    expect(panel()?.querySelector('[data-testid="chat-message-model-usage"]')).toBeNull()
    await wrapper.setProps({ metadata: { createdAt: '2026-10-28T08:00:25Z' } })
    expect(panel()?.textContent).not.toContain('Original Agent')
    expect(panel()?.textContent).not.toContain('original/model')
    expect(panel()?.querySelector('[data-info-row="model"] dd')?.textContent).toBe('Not reported')
  })

  it('distinguishes absent token reports from explicit zeros and can total reported model buckets', async () => {
    const wrapper = setup()
    await wrapper.get('button').trigger('focus'); await flushPromises()
    expect(wrapper.find('time').exists()).toBe(false)
    expect(panel()?.querySelector('[data-testid="chat-message-token-total"]')?.textContent).toBe('Not reported')
    const zero = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }
    await wrapper.setProps({ metadata: { usage: zero, durationMs: 0 } })
    expect(panel()?.querySelector('[data-testid="chat-message-token-total"]')?.textContent).toBe('0')
    expect(panel()?.querySelector('[data-info-row="duration"] dd')?.textContent).toBe('0s')
    await wrapper.setProps({ metadata: { usageByModel: { 'reported/model': usage } } })
    expect(panel()?.querySelector('[data-testid="chat-message-token-total"]')?.textContent).toBe('1,230')
    expect(panel()?.querySelector('[data-info-row="model"] dd')?.textContent).toBe('reported/model')
  })

  it('supports keyboard dismissal, touch-style toggles, outside dismissal and one open panel', async () => {
    const first = setup({ execution: { agentName: 'One', backend: 'codex' } })
    const second = setup({ execution: { agentName: 'Two', backend: 'opencode' } })
    await first.get('button').trigger('focus'); await flushPromises()
    expect(first.get('button').attributes('aria-expanded')).toBe('true')
    await first.get('button').trigger('keydown', { key: 'Escape' })
    expect(panel()).toBeNull()
    await first.get('button').trigger('click'); await flushPromises()
    expect(panel()).not.toBeNull()
    await second.get('button').trigger('click'); await flushPromises()
    expect(document.querySelectorAll('[role="tooltip"]')).toHaveLength(1)
    expect(panel()?.textContent).toContain('Two')
    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(panel()).toBeNull()
  })

  it('keeps token attribution and custom unknown labels distinct from an observed execution model', async () => {
    const wrapper = setup({ usageByModel: {
      'estimated/model': { ...usage, filled: true, source: 'via ACP_BRIDGE_MODEL' },
      '未知/未分桶': usage,
    } })
    await wrapper.setProps({ unknownModelDisplayName: 'Unspecified upstream' })
    await wrapper.get('button').trigger('focus'); await flushPromises()
    expect(panel()?.querySelector('[data-info-row="model"] dd')?.textContent).toBe('Not reported')
    expect(panel()?.textContent).not.toContain('Unspecified upstream')
    expect(panel()?.textContent).not.toContain('Attributed to the execution model')
    expect(panel()?.textContent).not.toContain('Provider reported')
    expect(panel()?.textContent).not.toContain('Model not reported')
    expect(panel()?.textContent).not.toContain('Unattributed tokens')
    expect(panel()?.textContent).not.toContain('estimated/model')
    expect(panel()?.textContent).not.toContain('via ACP_BRIDGE_MODEL')
    expect(panel()?.querySelector('[data-testid="chat-message-model-usage"]')).toBeNull()
    expect(panel()?.querySelector('[data-testid="chat-message-token-total"]')?.textContent).toBe('2,460')
    expect(panel()?.querySelector('[data-token-row="input"] dd')?.textContent).toBe('2,000')
    expect(panel()?.querySelector('[data-token-row="output"] dd')?.textContent).toBe('400')
    expect(panel()?.querySelector('[data-token-row="cacheRead"] dd')?.textContent).toBe('60')
    expect(panel()?.querySelector('[data-token-row="cacheWrite"] dd')?.textContent).toBe('0')
  })
})
