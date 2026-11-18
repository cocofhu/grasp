// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'
import VisitModelSelect from './VisitModelSelect.vue'

const mocks = vi.hoisted(() => ({ getVisitModel: vi.fn(), setVisitModel: vi.fn(), choices: [] as ConfiguredModelChoice[], ready: false, defaultConfigurationId: '' }))
vi.mock('@/lib/api/api', () => ({ api: { getVisitModel: mocks.getVisitModel, setVisitModel: mocks.setVisitModel } }))
vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({
    choices: computed(() => mocks.choices),
    ready: computed(() => mocks.ready),
    blocked: computed(() => !mocks.ready),
    configLoaded: computed(() => false),
    catalogConfirmed: computed(() => false),
    modelInConfiguration: () => null,
    defaultConfigurationId: computed(() => mocks.defaultConfigurationId),
  }),
}))

describe('VisitModelSelect', () => {
  beforeEach(() => {
    mocks.getVisitModel.mockReset()
    mocks.setVisitModel.mockReset()
    mocks.choices = []
    mocks.ready = false
    mocks.defaultConfigurationId = ''
    mocks.getVisitModel.mockResolvedValue({ currentModelSet: true, currentModelId: 'composer-fast', currentConfigurationId: 'group-a' })
  })

  it.each([false, true])('uses catalog Fast support before a route is stored and runtime support afterwards (stored: %s)', async stored => {
    mocks.choices = [{ id: 'gpt-6.2-sol', name: 'GPT-6.2 Sol', hint: 'gpt-6.2-sol', vendor: 'openai', backend: false, unavailable: false, efforts: ['low', 'high'], serviceTiers: ['fast'], configurationId: 'codex-group', modelId: 'gpt-6.2-sol' }]
    mocks.ready = true
    mocks.defaultConfigurationId = 'codex-group'
    mocks.getVisitModel.mockResolvedValue({ currentModelSet: stored, currentModelId: stored ? 'gpt-6.2-sol' : '', currentConfigurationId: stored ? 'codex-group' : '', reasoningEffort: 'low', serviceTier: '', serviceTiers: [] })
    mocks.setVisitModel.mockResolvedValue({ currentModelSet: true, currentModelId: 'gpt-6.2-sol', currentConfigurationId: 'codex-group', reasoningEffort: 'low', serviceTier: 'fast', serviceTiers: ['fast'] })
    const wrapper = mount(VisitModelSelect, {
      attachTo: document.body,
      props: { runId: 'run', nodeId: 'clarify', agentName: 'Codex' },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })],
        stubs: { Icon: true, VendorMark: true },
      },
    })
    try {
      await flushPromises()
      await wrapper.get('[data-testid="solo-model-select-trigger"]').trigger('click')
      await flushPromises()
      const toggle = document.querySelector<HTMLButtonElement>('[data-testid="solo-fast-toggle"]')
      if (stored) {
        expect(toggle).toBeNull()
        return
      }
      expect(toggle).not.toBeNull()
      toggle!.click()
      await flushPromises()
      expect(mocks.setVisitModel).toHaveBeenCalledWith('run', 'clarify', { iteration: 1, configurationId: 'codex-group', modelId: '', reasoningEffort: 'low', serviceTier: 'fast' })
      expect(document.querySelector('[data-testid="solo-effort-slider"]')?.getAttribute('aria-valuetext')).toBe('低')
      expect(document.querySelector('[data-testid="solo-fast-toggle"]')?.getAttribute('aria-pressed')).toBe('true')
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps the selected model when the directory fails to load', async () => {
    const wrapper = mount(VisitModelSelect, {
      props: {
        runId: 'run', nodeId: 'clarify', iteration: 1, agentName: '澄清',
        currentModelSet: true, currentModelId: 'composer-fast', currentConfigurationId: 'group-a',
      },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })],
        stubs: { Icon: true, VendorMark: true },
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).toContain('composer-fast')
    expect(wrapper.get('[data-testid="visit-model-select"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('当前 Agent 还没有可用模型')
  })

  it('keeps the default primary model when the directory fails before a switch', async () => {
    mocks.getVisitModel.mockResolvedValue({ currentModelSet: false })
    const wrapper = mount(VisitModelSelect, {
      props: {
        runId: 'run', nodeId: 'clarify', iteration: 1, agentName: '澄清',
        currentModelSet: false, currentModelId: '',
      },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })],
        stubs: { Icon: true, VendorMark: true },
      },
    })
    await flushPromises()
    expect(wrapper.text()).not.toContain('当前 Agent 还没有可用模型')
    expect(wrapper.get('[data-testid="visit-model-catalog-notice"]').text()).toContain('模型目录没有加载出来，仍使用默认主模型')
  })
})
