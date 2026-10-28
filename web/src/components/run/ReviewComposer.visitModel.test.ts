// @vitest-environment happy-dom
import { computed } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ReviewComposer from './ReviewComposer.vue'

const mocks = vi.hoisted(() => ({ getVisitModel: vi.fn(), setVisitModel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getVisitModel: mocks.getVisitModel, setVisitModel: mocks.setVisitModel } }))
vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({
    choices: computed(() => [{ id: 'kimi-k2.5', name: 'Kimi K2.5', hint: 'kimi-k2.5', vendor: '', backend: false, unavailable: false, configurationId: 'group', modelId: 'kimi-k2.5' }]),
    ready: computed(() => true),
    blocked: computed(() => false),
    configLoaded: computed(() => true),
    catalogConfirmed: computed(() => true),
    modelInConfiguration: () => true,
    defaultConfigurationId: computed(() => 'group'),
  }),
}))

const here = dirname(fileURLToPath(import.meta.url))

function mountComposer(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    missingWarn: false,
    fallbackWarn: false,
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ReviewComposer, {
    props: {
      mode: 'clarify',
      runId: 'run-1',
      nodeId: 'clarify',
      iteration: 1,
      turns: [],
      done: false,
      active: true,
      agentName: '澄清',
      modelSwitch: true,
      currentModelSet: true,
      currentModelId: 'kimi-k2.5',
      currentConfigurationId: 'group',
      ...extra,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyDemoFrame: true },
    },
  })
}

describe('ReviewComposer visit model select', () => {
  beforeEach(() => {
    mocks.getVisitModel.mockReset()
    mocks.setVisitModel.mockReset()
    mocks.getVisitModel.mockResolvedValue({ currentModelSet: true, currentModelId: 'kimi-k2.5', currentConfigurationId: 'group' })
  })

  it('shows the selector on a sendable member chat', async () => {
    const wrapper = mountComposer()
    await flushPromises()
    expect(wrapper.find('[data-testid="visit-model-select"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides the selector while watching, after the visit, on a cold session, and when switching is off', async () => {
    for (const extra of [{ observe: true }, { done: true }, { coldSession: true }, { modelSwitch: false }, { active: false }, { composerLocked: true }]) {
      const wrapper = mountComposer(extra)
      await flushPromises()
      expect(wrapper.find('[data-testid="visit-model-select"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('does not offer switching on the visitor share page', () => {
    const src = readFileSync(join(here, '../../views/PublicGateApprovalView.vue'), 'utf8')
    expect(src).not.toMatch(/model-switch|modelSwitch/)
  })
})
