// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ProjectSharedAgentPanel from './ProjectSharedAgentPanel.vue'

const apiMocks = vi.hoisted(() => ({
  getProjectSharedAgentConfig: vi.fn(),
  listAgents: vi.fn(),
  putProjectSharedAgentConfig: vi.fn(),
  createProjectSharedAgentTest: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProjectSharedAgentConfig: apiMocks.getProjectSharedAgentConfig,
      listAgents: apiMocks.listAgents,
      putProjectSharedAgentConfig: apiMocks.putProjectSharedAgentConfig,
      createProjectSharedAgentTest: apiMocks.createProjectSharedAgentTest,
    },
  }
})

const sharedCfg = {
  acpBackend: 'cursor',
  projectId: 'proj-a',
  gitCredentialType: '',
  files: [],
  mcp: [],
  env: {},
  layout: {},
}

function mountPanel(projectId = 'proj-a') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ProjectSharedAgentPanel, {
    props: { projectId },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AgentFilesPanel: true,
        AgentMcpPanel: true,
        AgentEnvPanel: true,
        AgentChatTester: {
          props: ['profile', 'homeProjectId', 'createTest'],
          template: '<div data-testid="shared-agent-chat-tester">{{ profile }}</div>',
        },
      },
    },
  })
}

describe('ProjectSharedAgentPanel without chat test (g2.1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getProjectSharedAgentConfig.mockResolvedValue(sharedCfg)
    apiMocks.listAgents.mockResolvedValue([{ name: 'mine', projectId: 'proj-a' }])
  })

  it('keeps config subtabs and removes dialogue-test entry', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="shared-agent-subtab-files"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shared-agent-subtab-mcp"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shared-agent-subtab-env"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shared-agent-subtab-prompts"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="shared-agent-subtab-meta"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shared-agent-subtab-test"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="shared-agent-chat-tester"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="shared-agent-test-pick"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="shared-agent-help-text"]').text()).toContain('Agent Studio「对话测试」')
    expect(wrapper.get('[data-testid="shared-agent-help-text"]').text()).not.toContain('仅在此入口')
    wrapper.unmount()
  })
})
