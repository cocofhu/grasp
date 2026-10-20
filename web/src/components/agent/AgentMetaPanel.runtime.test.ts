// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useToast } from '@/lib/composables/useToast'
import AgentMetaPanel from './AgentMetaPanel.vue'

const mocks = vi.hoisted(() => ({ listCredentials: vi.fn(), getSharedAgentConfig: vi.fn() }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listCredentials: mocks.listCredentials,
      getSharedAgentConfig: mocks.getSharedAgentConfig,
    },
  }
})
vi.mock('@/lib/agent/cliCatalog', () => ({
  loadCliModels: (b: string) =>
    Promise.resolve(
      b === 'codex'
        ? [
            {
              id: 'gpt-6.1-sol',
              name: 'GPT-6.1 Sol',
              vendor: 'openai',
              tags: ['reasoning'],
              efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'],
            },
            {
              id: 'gpt-6-luna',
              name: 'GPT-6 Luna',
              vendor: 'openai',
              tags: ['reasoning'],
              efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
            },
            { id: 'plain', name: 'Plain', vendor: 'openai', tags: ['vision'] },
          ]
        : [{ id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['reasoning'] }],
    ),
  cliCatalogFailed: () => false,
}))

function draftFor(overrides: Record<string, unknown> = {}) {
  return reactive({
    name: 'a',
    acpBackend: 'cursor',
    aiCredentialId: '',
    openCodeCredentialId: '',
    gitCredentialId: '',
    sshHostsCredentialId: '',
    gitCredentialType: undefined,
    files: [],
    mcp: [],
    env: [] as { k: string; v: string }[],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/workspace' },
    capabilities: null,
    ...overrides,
  })
}

const mounted: { unmount: () => void }[] = []

function mountPanel(draft: ReturnType<typeof draftFor>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const w = mount(AgentMetaPanel, {
    props: { draft, agentName: 'a' },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, AppModal: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' } },
    },
    attachTo: document.body,
  })
  mounted.push(w)
  return w
}

function envOf(draft: ReturnType<typeof draftFor>) {
  return Object.fromEntries(draft.env.map((e) => [e.k, e.v]))
}

beforeEach(() => {
  mocks.listCredentials.mockResolvedValue({ items: [] })
  mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: '', env: {} })
})
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount())
  document.body.innerHTML = ''
  const toast = useToast()
  toast.toasts.value = []
})

describe('AgentMetaPanel runtime card', () => {
  it('writes the picked model to ACP_BRIDGE_MODEL and removes it for auto', async () => {
    const draft = draftFor()
    const w = mountPanel(draft)
    await flushPromises()
    expect(w.find('[data-test="agent-cli-runtime"]').exists()).toBe(true)
    expect(w.find('[data-test="agent-cli-effort"]').exists()).toBe(false)
    await w.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector('[data-testid="cli-model-option-gpt-5"]') as HTMLElement).click()
    await flushPromises()
    expect(envOf(draft).ACP_BRIDGE_MODEL).toBe('gpt-5')
    await w.get('[data-testid="cli-model-reset"]').trigger('click')
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
  })

  it('names the workspace model an Agent on auto inherits, only for the same CLI', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'codex',
      env: { ACP_BRIDGE_MODEL: 'gpt-6-luna' },
    })
    const draft = draftFor({ acpBackend: 'codex' })
    const w = mountPanel(draft)
    await flushPromises()
    expect(w.get('[data-test="agent-cli-model-inherited"]').text()).toContain('GPT-6 Luna')

    draft.env = [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' }]
    await flushPromises()
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)

    const cursor = mountPanel(draftFor({ acpBackend: 'cursor' }))
    await flushPromises()
    expect(cursor.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
  })

  it('is not shown for OpenCode, which keeps its model on the credential', async () => {
    const w = mountPanel(draftFor({ acpBackend: 'opencode' }))
    await flushPromises()
    expect(w.find('[data-test="agent-cli-runtime"]').exists()).toBe(false)
  })

  it('clears the pin on a backend switch and offers undo', async () => {
    const draft = draftFor({ env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-5' }, { k: 'FOO', v: '1' }] })
    const w = mountPanel(draft)
    await flushPromises()
    const claude = w.findAll('button').find((b) => b.text().includes('Claude Code'))!
    await claude.trigger('click')
    expect(draft.acpBackend).toBe('claude_code')
    expect(envOf(draft)).toEqual({ FOO: '1' })
    const toast = useToast()
    const item = toast.toasts.value.at(-1)!
    expect(item.message).toContain('Claude Code')
    toast.runAction(item.id)
    expect(draft.acpBackend).toBe('cursor')
    expect(envOf(draft).ACP_BRIDGE_MODEL).toBe('gpt-5')
  })

  it('stays quiet on a switch when no model was pinned', async () => {
    const draft = draftFor()
    const w = mountPanel(draft)
    await flushPromises()
    await w.findAll('button').find((b) => b.text().includes('Claude Code'))!.trigger('click')
    expect(useToast().toasts.value).toHaveLength(0)
  })

  async function pickModel(w: ReturnType<typeof mountPanel>, id: string) {
    await w.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector(`[data-testid="cli-model-option-${id}"]`) as HTMLElement).click()
    await flushPromises()
  }

  function effortOptions(w: ReturnType<typeof mountPanel>) {
    return w.findAll('[data-test^="agent-cli-effort-"]').map((b) => b.attributes('data-test')!.replace('agent-cli-effort-', ''))
  }

  it('offers the levels the picked Codex model accepts', async () => {
    const draft = draftFor({ acpBackend: 'codex', env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' }] })
    const w = mountPanel(draft)
    await flushPromises()
    expect(effortOptions(w)).toEqual(['auto', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    await w.get('[data-test="agent-cli-effort-ultra"]').trigger('click')
    expect(envOf(draft).ACP_BRIDGE_REASONING_EFFORT).toBe('ultra')
    await w.get('[data-test="agent-cli-effort-auto"]').trigger('click')
    expect('ACP_BRIDGE_REASONING_EFFORT' in envOf(draft)).toBe(false)
  })

  it('offers the common levels for auto', async () => {
    const w = mountPanel(draftFor({ acpBackend: 'codex' }))
    await flushPromises()
    expect(effortOptions(w)).toEqual(['auto', 'low', 'medium', 'high'])
  })

  it('drops an effort the newly picked model does not accept and keeps one it does', async () => {
    const draft = draftFor({ acpBackend: 'codex', env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' }] })
    const w = mountPanel(draft)
    await flushPromises()
    await w.get('[data-test="agent-cli-effort-ultra"]').trigger('click')
    await pickModel(w, 'gpt-6-luna')
    expect(envOf(draft)).toEqual({ ACP_BRIDGE_MODEL: 'gpt-6-luna' })
    expect(effortOptions(w)).not.toContain('ultra')

    await w.get('[data-test="agent-cli-effort-xhigh"]').trigger('click')
    await pickModel(w, 'gpt-6.1-sol')
    expect(envOf(draft).ACP_BRIDGE_REASONING_EFFORT).toBe('xhigh')
  })

  it('greys effort out for a model that has no reasoning levels', async () => {
    const draft = draftFor({ acpBackend: 'codex', env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' }] })
    const w = mountPanel(draft)
    await flushPromises()
    await w.get('[data-test="agent-cli-effort-high"]').trigger('click')
    await pickModel(w, 'plain')
    expect(envOf(draft)).toEqual({ ACP_BRIDGE_MODEL: 'plain' })
    expect(w.get('[data-test="agent-cli-effort-high"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-test="agent-cli-effort"]').text()).toContain('不支持')
  })
})
