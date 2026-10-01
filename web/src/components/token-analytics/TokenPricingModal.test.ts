// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import TokenPricingModal from './TokenPricingModal.vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

vi.mock('@/lib/api/api', () => ({
  api: {
    getTokenPricing: vi.fn(),
    updateTokenPricing: vi.fn(),
  },
}))

import { api } from '@/lib/api/api'

const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })

function mountModal(canEdit: boolean) {
  return mount(TokenPricingModal, {
    props: { open: true, canEdit, knownModels: ['sonnet', 'opus'] },
    global: { plugins: [i18n], stubs: { teleport: true } },
  })
}

describe('TokenPricingModal', () => {
  beforeEach(() => {
    vi.mocked(api.getTokenPricing).mockResolvedValue({
      currency: 'USD',
      models: { sonnet: { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 } },
    })
    vi.mocked(api.updateTokenPricing).mockReset()
  })

  it('lists priced models first and pre-fills unpriced known models', async () => {
    const wrapper = mountModal(true)
    await flushPromises()
    const rows = wrapper.findAll('[data-testid="token-pricing-row"]')
    expect(rows).toHaveLength(2)
    expect((rows[0].find('input').element as HTMLInputElement).value).toBe('sonnet')
    expect((rows[1].find('input').element as HTMLInputElement).value).toBe('opus')
    wrapper.unmount()
  })

  it('saves only non-zero rows and emits saved', async () => {
    vi.mocked(api.updateTokenPricing).mockResolvedValue({ currency: 'CNY', models: {} })
    const wrapper = mountModal(true)
    await flushPromises()
    await wrapper.find('[data-testid="token-pricing-currency"]').setValue('CNY')
    await wrapper.find('[data-testid="token-pricing-output-0"]').setValue('20')
    await wrapper.find('[data-testid="token-pricing-save"]').trigger('click')
    await flushPromises()
    expect(api.updateTokenPricing).toHaveBeenCalledWith({
      currency: 'CNY',
      models: { sonnet: { input: 3, output: 20, cacheRead: 0.3, cacheWrite: 3.75 } },
    })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it('blocks negative prices', async () => {
    const wrapper = mountModal(true)
    await flushPromises()
    await wrapper.find('[data-testid="token-pricing-input-1"]').setValue('-1')
    expect(wrapper.find('[data-testid="token-pricing-save"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('is read-only for non-admins', async () => {
    const wrapper = mountModal(false)
    await flushPromises()
    expect(wrapper.find('[data-testid="token-pricing-readonly"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-pricing-save"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="token-pricing-input-0"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
