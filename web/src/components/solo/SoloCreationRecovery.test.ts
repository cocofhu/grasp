// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import SoloCreationRecovery from './SoloCreationRecovery.vue'
import type { SoloCreationOutbox } from '@/lib/solo/soloOutbox'

const request: SoloCreationOutbox = {
  id: 'request-1', scope: 'owner-scope', kind: 'creation', createdAt: '',
  request: { clientRequestId: 'request-1', agentName: 'Solo', text: 'Keep the original message', repos: [{ projectId: 'repo-1', branch: 'feature/saved' }], initialModelId: 'saved-model', reasoningEffort: 'high' },
}

function render(props: Partial<InstanceType<typeof SoloCreationRecovery>['$props']> = {}) {
  return mount(SoloCreationRecovery, {
    props: { request, busy: false, checked: true, resolved: false, ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })],
      directives: { 'hover-ink': {} },
    },
  })
}

describe('SoloCreationRecovery', () => {
  it('shows the frozen request and separates reconciliation from explicit retry', async () => {
    const wrapper = render()
    expect(wrapper.text()).toContain('Keep the original message')
    expect(wrapper.text()).toContain('1 个仓库')
    expect(wrapper.text()).toContain('repo-1 (feature/saved)')
    expect(wrapper.text()).toContain('saved-model')
    expect(wrapper.text()).toContain('high')
    expect(wrapper.text()).toContain(pages.pages.solo.stopLocalRetryHint)
    await wrapper.get('[data-testid="solo-creation-check"]').trigger('click')
    expect(wrapper.emitted('check')).toHaveLength(1)
    expect(wrapper.emitted('retry')).toBeUndefined()
    await wrapper.get('[data-testid="solo-creation-retry"]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    await wrapper.get('[data-testid="solo-creation-stop"]').trigger('click')
    expect(wrapper.emitted('stop')).toHaveLength(1)
    // These controls also sit inside the create dialog's form. None may
    // trigger its separate "create a new session" submit action.
    for (const button of wrapper.findAll('button')) expect(button.attributes('type')).toBe('button')
    wrapper.unmount()
  })

  it('requires a successful lookup before retry and disables every control while busy', () => {
    const unknown = render({ checked: false })
    expect(unknown.get('[data-testid="solo-creation-retry"]').attributes('disabled')).toBeDefined()
    expect(unknown.get('[data-testid="solo-creation-check"]').attributes('disabled')).toBeUndefined()
    unknown.unmount()
    const busy = render({ busy: true })
    for (const button of busy.findAll('button')) expect(button.attributes('disabled')).toBeDefined()
    busy.unmount()
  })

  it('offers the already created chat and renders nothing without a pending request', () => {
    const resolved = render({ resolved: true })
    expect(resolved.get('[data-testid="solo-creation-retry"]').text()).toBe(pages.pages.solo.creationRecovery.open)
    resolved.unmount()
    const empty = render({ request: null })
    expect(empty.find('section').exists()).toBe(false)
    empty.unmount()
  })
})
