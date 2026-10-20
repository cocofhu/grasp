// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import CreateAgentTeamWizard from './CreateAgentTeamWizard.vue'

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      startTeamBootstrap: vi.fn(),
      getSharedAgentConfig: vi.fn().mockResolvedValue({ env: {}, files: [], mcp: [], layout: {} }),
    },
  }
})

describe('CreateAgentTeamWizard', () => {
  it('renders when open and can close', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(CreateAgentTeamWizard, {
      props: { open: true, existingNames: [] },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
          AgentGitGuide: true,
          WizardApiKeyStepPanel: true,
        },
      },
    })
    await flushPromises()
    expect(w.html().length).toBeGreaterThan(10)
    w.unmount()
  })
})
