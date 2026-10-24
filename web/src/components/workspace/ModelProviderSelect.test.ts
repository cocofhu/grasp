// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/en/common.json'
import ModelProviderSelect from './ModelProviderSelect.vue'

const wrappers: VueWrapper[] = []
function setup(props: Partial<{ modelValue: string; disabled: boolean; extraProviders: string[] }> = {}) {
  const wrapper = mount(ModelProviderSelect, {
    props: { modelValue: 'deepseek', ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: common } })],
      stubs: { Teleport: true, Icon: true },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) if (wrapper.exists()) wrapper.unmount()
})

describe('ModelProviderSelect', () => {
  it('shows the selected provider name and logo alongside branded preset options', async () => {
    const wrapper = setup()
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    expect(trigger.text()).toContain('DeepSeek')
    expect(trigger.get('[data-provider-logo="deepseek"]').attributes('aria-hidden')).toBe('true')
    await trigger.trigger('click')
    for (const provider of ['deepseek', 'anthropic', 'openai']) {
      const option = wrapper.get(`[data-test="app-select-option-${provider}"]`)
      expect(option.attributes('role')).toBe('option')
      expect(option.find('[data-provider-logo]').exists()).toBe(true)
    }
    expect(wrapper.get('[data-test="app-select-option-custom"]').text()).toContain('Custom provider')
  })

  it('searches provider names and forwards the selected provider ID', async () => {
    const wrapper = setup()
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('KIMI')
    expect(wrapper.find('[data-test="app-select-option-deepseek"]').exists()).toBe(false)
    await wrapper.get('[data-test="app-select-option-moonshot"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['moonshot']])
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)
  })

  it('preserves existing custom provider IDs without duplicating presets or extra options', async () => {
    const wrapper = setup({ modelValue: 'private-provider', extraProviders: ['private-provider', 'private-provider', 'deepseek'] })
    expect(wrapper.get('[data-test="app-select-trigger"]').text()).toContain('private-provider')
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.findAll('[data-test="app-select-option-private-provider"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-test="app-select-option-deepseek"]')).toHaveLength(1)
    await wrapper.get('[data-test="app-select-option-custom"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['custom']])
    await wrapper.setProps({ modelValue: 'custom' })
    expect(wrapper.get('[data-test="app-select-trigger"]').text()).toContain('Custom provider')
    expect(wrapper.get('[data-test="app-select-trigger"] icon-stub[name="server"]').exists()).toBe(true)
  })

  it('allows keyboard selection through search and respects a disabled provider field', async () => {
    const wrapper = setup()
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const search = wrapper.get('[data-test="app-select-search"]')
    await search.setValue('anthropic')
    await search.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['anthropic']])
    await wrapper.setProps({ disabled: true })
    expect(trigger.attributes('disabled')).toBeDefined()
    await trigger.trigger('click')
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)
  })
})
