// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import VisitModelSelect from './VisitModelSelect.vue'

const mocks = vi.hoisted(() => ({ getVisitModel: vi.fn(), setVisitModel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getVisitModel: mocks.getVisitModel, setVisitModel: mocks.setVisitModel } }))
vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({
    choices: computed(() => []),
    ready: computed(() => false),
    blocked: computed(() => true),
    configLoaded: computed(() => false),
    catalogConfirmed: computed(() => false),
    modelInConfiguration: () => null,
    defaultConfigurationId: computed(() => ''),
  }),
}))

describe('VisitModelSelect', () => {
  beforeEach(() => {
    mocks.getVisitModel.mockReset()
    mocks.setVisitModel.mockReset()
    mocks.getVisitModel.mockResolvedValue({ currentModelSet: true, currentModelId: 'composer-fast', currentConfigurationId: 'group-a' })
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
