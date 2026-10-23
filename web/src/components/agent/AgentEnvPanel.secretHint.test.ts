// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentEnvPanel from './AgentEnvPanel.vue'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'

function draft(partial: Partial<AgentStudioDraft> = {}): AgentStudioDraft {
  return {
    name: 'test-agent',
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
      stubs: { CodeEditor: true, EnvCredentialHelpModal: true, RouterLink: true },
    },
  })
}

describe('AgentEnvPanel secret key hint', () => {
  const RouterLinkStub = { props: ['to'], template: '<a :data-to="JSON.stringify(to)"><slot /></a>' }

  function mountWithLink(d: AgentStudioDraft, context: 'agent' | 'shared' = 'agent') {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    return mount(AgentEnvPanel, {
      props: { draft: d, context },
      global: {
        plugins: [i18n],
        stubs: { CodeEditor: true, EnvCredentialHelpModal: true, AgentGitGuide: true, RouterLink: RouterLinkStub },
      },
    })
  }

  it.each(['GIT_SSH_PRIVATE_KEY', 'GITHUB_TOKEN', 'GRASP_CURSOR_API_KEY', 'ANTHROPIC_API_KEY', 'GRASP_DEEPSEEK_API_KEY', 'DEEPSEEK_API_KEY', 'ACP_DEEPSEEK_API_KEY', 'ACP_DEEPSEEK_HARNESS_API_KEY'])(
    'shows an inline hint linking to the workspace credentials tab when %s is typed',
    async (key) => {
      const d = draft({ env: [{ k: '', v: '' }] })
      const wrapper = mountWithLink(d)
      expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)

      await wrapper.get('input[placeholder="KEY"]').setValue(key)
      expect(d.env[0].k).toBe(key)
      const hint = wrapper.get('[data-test="env-secret-hint"]')
      expect(hint.text()).toContain(pages.pages.agentStudio.env.secretCredentialsOnly)
      const link = hint.get('[data-test="env-secret-credentials-link"]')
      expect(JSON.parse(link.attributes('data-to')!)).toEqual({ path: '/settings/credentials' })

      await wrapper.get('input[placeholder="KEY"]').setValue('FOO')
      expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)
    },
  )

  it.each(['ACP_DEEPSEEK_API_KEY', 'ACP_DEEPSEEK_HARNESS_API_KEY'])(
    'flags DeepSeek alias %s pasted into the JSON env editor',
    async (key) => {
      const wrapper = mountWithLink(draft({ acpBackend: 'deepseek_harness' }))
      const jsonButton = wrapper.findAll('button').find((button) => button.text() === pages.pages.agentStudio.mcp.rawJson)!
      await jsonButton.trigger('click')
      wrapper.getComponent({ name: 'CodeEditor' }).vm.$emit('update:modelValue', JSON.stringify({ [key]: 'pasted-example-key' }))
      await wrapper.vm.$nextTick()
      expect(wrapper.get('[data-test="env-secret-hint"]').text()).toContain(pages.pages.agentStudio.env.secretCredentialsOnly)
      expect(wrapper.get('[data-test="env-secret-credentials-link"]').exists()).toBe(true)
      wrapper.unmount()
    },
  )

  it('does not suggest a JSON auth file for DeepSeek Harness', () => {
    const wrapper = mountWithLink(draft({ acpBackend: 'deepseek_harness', layout: { configRoot: '/root/.dsh', workspaceDir: '/root/workspace' } }))
    expect(wrapper.find('[data-test="env-custom-config-callout"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="env-open-settings-file"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('links to workspace credentials from both shared and Agent editors', async () => {
    const shared = mountWithLink(draft({ env: [{ k: 'GIT_SSH_KNOWN_HOSTS', v: 'x' }] }), 'shared')
    expect(shared.find('[data-test="env-secret-hint"] [data-test="env-secret-credentials-link"]').exists()).toBe(true)

    const agentEditor = mountWithLink(draft({ env: [{ k: 'GITLAB_TOKEN', v: 'x' }] }))
    expect(agentEditor.find('[data-test="env-secret-hint"]').exists()).toBe(true)
    expect(agentEditor.find('[data-test="env-secret-credentials-link"]').exists()).toBe(true)
  })

  it('does not flag non-secret git env such as GIT_SSH_COMMAND or GITLAB_URL', () => {
    const wrapper = mountWithLink(draft({ env: [{ k: 'GIT_SSH_COMMAND', v: 'x' }, { k: 'GITLAB_URL', v: 'u' }] }))
    expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)
  })
})
