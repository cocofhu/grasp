// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentEnvPanel from './AgentEnvPanel.vue'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'
import AgentMcpPanel from './AgentMcpPanel.vue'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
}

function expectOutlineHelpButton(wrapper: ReturnType<typeof mount>, selector: string) {
  const el = wrapper.get(selector)
  const classes = el.classes().join(' ')
  expect(classes).not.toMatch(/underline/)
  expect(classes).toContain('border')
  expect(el.find('svg').exists()).toBe(true)
  expect(el.text()).toBe('帮助')
}

const baseDraft: AgentStudioDraft = {
  name: 'test-agent',
  env: [],
  mcp: [],
  files: [],
  capabilities: null,
  acpBackend: 'codebuddy',
  layout: { configRoot: '/tmp/agent', workspaceDir: '/tmp/workspace' },
}

describe('Agent Studio help buttons', () => {
  it('环境变量顶栏与 ACP 卡使用描边帮助按钮且非帮助链未误改', () => {
    const draft = structuredClone(baseDraft)
    const wrapper = mount(AgentEnvPanel, {
      props: { draft },
      global: {
        plugins: [createI18nPlugin()],
        stubs: {
          RouterLink: true,
          AgentGitGuide: true,
          EnvCredentialHelpModal: true,
          CodeEditor: true,
        },
      },
    })

    expectOutlineHelpButton(wrapper, '[data-test="env-help-inject"]')

  })

  it('MCP 顶栏帮助为描边按钮', () => {
    const draft = structuredClone(baseDraft)
    const wrapper = mount(AgentMcpPanel, {
      props: { draft },
      global: {
        plugins: [createI18nPlugin()],
        stubs: { McpConfigHelpModal: true, CodeEditor: true },
      },
    })

    expectOutlineHelpButton(wrapper, '[data-test="mcp-help-link"]')
  })

  it('Git 凭据卡帮助为描边按钮', () => {
    const wrapper = mount(AgentGitCredentialPicker, {
      props: {
        modelValue: { items: [] },
        items: [],
      },
      global: {
        plugins: [createI18nPlugin()],
        stubs: { AppModal: true },
      },
    })

    expect(wrapper.get('[data-testid="git-credential-help"]').text()).toBe('Git 配置帮助')
  })
})
