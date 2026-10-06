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

  it.each(['GIT_SSH_PRIVATE_KEY', 'GITHUB_TOKEN', 'GRASP_CURSOR_API_KEY', 'ANTHROPIC_API_KEY'])(
    'shows an inline hint linking to the project credentials tab when %s is typed',
    async (key) => {
      const d = draft({ projectId: 'proj-1', env: [{ k: '', v: '' }] })
      const wrapper = mountWithLink(d)
      expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)

      await wrapper.get('input[placeholder="KEY"]').setValue(key)
      expect(d.env[0].k).toBe(key)
      const hint = wrapper.get('[data-test="env-secret-hint"]')
      expect(hint.text()).toContain(pages.pages.agentStudio.env.secretCredentialsOnly)
      const link = hint.get('[data-test="env-secret-credentials-link"]')
      expect(JSON.parse(link.attributes('data-to')!)).toEqual({
        name: 'project-detail',
        params: { id: 'proj-1' },
        query: { tab: 'credentials' },
      })

      await wrapper.get('input[placeholder="KEY"]').setValue('FOO')
      expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)
    },
  )

  it('also hints in the shared editor and omits the link without a project', async () => {
    const shared = mountWithLink(draft({ projectId: 'proj-1', env: [{ k: 'GIT_SSH_KNOWN_HOSTS', v: 'x' }] }), 'shared')
    expect(shared.find('[data-test="env-secret-hint"] [data-test="env-secret-credentials-link"]').exists()).toBe(true)

    const noProject = mountWithLink(draft({ env: [{ k: 'GITLAB_TOKEN', v: 'x' }] }))
    expect(noProject.find('[data-test="env-secret-hint"]').exists()).toBe(true)
    expect(noProject.find('[data-test="env-secret-credentials-link"]').exists()).toBe(false)
  })

  it('does not flag non-secret git env such as GIT_SSH_COMMAND or GITLAB_URL', () => {
    const wrapper = mountWithLink(draft({ env: [{ k: 'GIT_SSH_COMMAND', v: 'x' }, { k: 'GITLAB_URL', v: 'u' }] }))
    expect(wrapper.find('[data-test="env-secret-hint"]').exists()).toBe(false)
  })
})
