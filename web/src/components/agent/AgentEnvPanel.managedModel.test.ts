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
    name: 'a',
    projectId: '',
    env: [],
    mcp: [],
    files: [],
    capabilities: null,
    gitCredentialType: undefined,
    acpBackend: 'codex',
    layout: { configRoot: '/tmp/agent', workspaceDir: '/tmp/workspace' },
    ...partial,
  }
}

function mountPanel(d: AgentStudioDraft, context: 'agent' | 'shared' = 'agent') {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(AgentEnvPanel, {
    props: { draft: d, context },
    global: { plugins: [i18n], stubs: { CodeEditor: true, EnvCredentialHelpModal: true, AgentGitGuide: true, RouterLink: true } },
  })
}

const pinned = [
  { k: 'ACP_BRIDGE_MODEL', v: 'gpt-5' },
  { k: 'ACP_BRIDGE_REASONING_EFFORT', v: 'high' },
  { k: 'FOO', v: '1' },
]

describe('AgentEnvPanel managed model keys', () => {
  it('locks the model keys and links to the Runtime card', async () => {
    const w = mountPanel(draft({ env: pinned.map((e) => ({ ...e })) }))
    const block = w.get('[data-test="env-managed-model"]')
    expect(block.text()).toContain('运行配置')
    expect(block.findAll('input').map((i) => (i.element as HTMLInputElement).value)).toEqual([
      'ACP_BRIDGE_MODEL',
      'gpt-5',
      'ACP_BRIDGE_REASONING_EFFORT',
      'high',
    ])
    expect(w.findAll('input[placeholder="KEY"]').map((i) => (i.element as HTMLInputElement).value)).toEqual(['FOO'])
    await w.get('[data-test="env-managed-model-edit"]').trigger('click')
    expect(w.emitted('edit-model')).toHaveLength(1)
  })

  it('refuses the managed key typed into a free row', async () => {
    const d = draft({ acpBackend: 'cursor', env: [{ k: '', v: '' }] })
    const w = mountPanel(d)
    await w.get('input[placeholder="KEY"]').setValue('ACP_BRIDGE_MODEL')
    expect(d.env[0]!.k).toBe('')
    expect(w.emitted('toast')?.[0]?.[0]).toContain('运行配置')
  })

  it('leaves the keys editable where no Runtime card exists', () => {
    const shared = mountPanel(draft({ env: pinned.map((e) => ({ ...e })) }), 'shared')
    expect(shared.find('[data-test="env-managed-model"]').exists()).toBe(false)
    expect(shared.findAll('input[placeholder="KEY"]')).toHaveLength(3)
    const oc = mountPanel(draft({ acpBackend: 'opencode', env: pinned.map((e) => ({ ...e })) }))
    expect(oc.find('[data-test="env-managed-model"]').exists()).toBe(false)
  })
})
