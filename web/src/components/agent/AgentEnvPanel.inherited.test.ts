// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentEnvPanel from './AgentEnvPanel.vue'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'

const getProjectSharedAgentConfig = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    getProjectSharedAgentConfig: (...args: unknown[]) => getProjectSharedAgentConfig(...args),
  },
}))

function draft(partial: Partial<AgentStudioDraft> = {}): AgentStudioDraft {
  return {
    name: 'test-agent',
    projectId: '',
    env: [],
    mcp: [],
    files: [],
    capabilities: null,
    gitCredentialType: undefined,
    acpBackend: 'cursor',
    layout: { configRoot: '/tmp/agent', workspaceDir: '/tmp/workspace' },
    ...partial,
  }
}

function mountPanel(d: AgentStudioDraft, context: 'agent' | 'shared' = 'agent') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentEnvPanel, {
    props: { draft: d, context },
    global: {
      plugins: [i18n],
      stubs: { CodeEditor: true, EnvCredentialHelpModal: true },
    },
  })
}

describe('AgentEnvPanel inherited Git env', () => {
  beforeEach(() => {
    getProjectSharedAgentConfig.mockReset()
  })

  it('agent 上下文且已绑定项目时读取共享 env，引导仍因继承 Token 可见并可预选（plan g3.3）', async () => {
    getProjectSharedAgentConfig.mockResolvedValue({
      projectId: 'proj-1',
      env: { GITLAB_TOKEN: '${vars.gitlab_pat}' },
      files: [],
      mcp: [],
      layout: {},
    })
    const wrapper = mountPanel(draft({ projectId: 'proj-1' }))
    await vi.waitFor(() => {
      expect(getProjectSharedAgentConfig).toHaveBeenCalledWith('proj-1')
    })
    await vi.waitFor(() => {
      expect(wrapper.find('[data-test="git-guide"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="git-choice-gitlab_https"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="git-choice-github_https"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="git-choice-ssh"]').exists()).toBe(true)
    })
    wrapper.unmount()
  })

  it('shared 上下文不请求项目共享配置，只看本面 env（g2.1）', async () => {
    const wrapper = mountPanel(draft({ projectId: 'proj-1' }), 'shared')
    await new Promise((r) => setTimeout(r, 20))
    expect(getProjectSharedAgentConfig).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="git-guide"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('未绑定项目不请求共享配置，显示三选', async () => {
    const wrapper = mountPanel(draft({ projectId: '' }))
    await new Promise((r) => setTimeout(r, 20))
    expect(getProjectSharedAgentConfig).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="git-choice-github_https"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
