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
        : [
            { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['reasoning'] },
            { id: 'grok-4.7-medium', name: 'Grok 4.7', vendor: 'xai' },
          ],
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
    expect(w.find('[data-test="agent-cli-model-mode"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    await w.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector('[data-testid="cli-model-option-gpt-5"]') as HTMLElement).click()
    await flushPromises()
    expect(envOf(draft).ACP_BRIDGE_MODEL).toBe('gpt-5')
    await w.get('[data-testid="cli-model-reset"]').trigger('click')
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
  })

  it('defaults to following the workspace model, and pins a separate choice beside it', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'codex',
      env: { ACP_BRIDGE_MODEL: 'gpt-6-luna' },
    })
    const draft = draftFor({ acpBackend: 'codex' })
    const w = mountPanel(draft)
    await flushPromises()
    const model = w.get('[data-test="agent-cli-model"]')
    expect(w.get('[data-test="agent-cli-model-follow"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="agent-cli-model-follow"]').text()).toContain('默认')
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('false')
    const card = w.get('[data-test="agent-cli-model-inherited"]')
    expect(w.get('[data-test="agent-cli-model-inherited-name"]').text()).toBe('GPT-6 Luna')
    expect(card.text()).toContain('工作区共享配置')
    expect(card.text()).not.toContain('--model')
    expect(card.text()).not.toContain('cursor-agent')
    expect(model.text()).not.toContain('自动')
    expect(model.text()).not.toContain('CLI 默认')
    expect(model.text()).not.toContain('未单独选择')
    expect(w.find('[data-testid="cli-model-select"]').exists()).toBe(false)

    draft.env = [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' }]
    await flushPromises()
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="agent-cli-model-follow"]').text()).toContain('默认')
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('GPT-6.1 Sol')

    const cursor = mountPanel(draftFor({ acpBackend: 'cursor' }))
    await flushPromises()
    expect(cursor.find('[data-test="agent-cli-model-mode"]').exists()).toBe(false)
    expect(cursor.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect(cursor.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
  })

  it('shows an unlisted shared model id as-is, not a similar catalog name', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' },
    })
    const w = mountPanel(draftFor())
    await flushPromises()
    const card = w.get('[data-test="agent-cli-model-inherited"]')
    expect(w.get('[data-test="agent-cli-model-inherited-name"]').text()).toBe('grok-4.7-xhigh')
    expect(card.text()).not.toContain('Grok 4.7')
    expect(card.text()).not.toContain('cursor-agent')
    expect(w.find('[data-testid="cli-model-select"]').exists()).toBe(false)
  })

  it('writes a model only after a pick, and either way back clears it', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' },
    })
    const draft = draftFor()
    const w = mountPanel(draft)
    await flushPromises()
    await w.get('[data-test="agent-cli-model-own"]').trigger('click')
    await flushPromises()
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('选择一个模型')
    expect(w.get('[data-test="agent-cli-model"]').text()).not.toContain('自动')
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
    expect(w.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    await w.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[data-testid="cli-model-option-auto"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="cli-model-option-gpt-5"]')).not.toBeNull()
    ;(document.body.querySelector('[data-testid="cli-model-option-gpt-5"]') as HTMLElement).click()
    await flushPromises()
    expect(envOf(draft).ACP_BRIDGE_MODEL).toBe('gpt-5')
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('true')
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)

    await w.get('[data-test="agent-cli-model-follow"]').trigger('click')
    await flushPromises()
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
    expect(w.get('[data-test="agent-cli-model-follow"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="agent-cli-model-inherited-name"]').text()).toBe('grok-4.7-xhigh')

    await w.get('[data-test="agent-cli-model-own"]').trigger('click')
    await w.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    const input = document.body.querySelector('[data-testid="cli-model-search"]') as HTMLInputElement
    input.value = 'grok-4.7-xhigh'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    ;(document.body.querySelector('[data-testid="cli-model-empty-use"]') as HTMLElement).click()
    await flushPromises()
    expect(envOf(draft).ACP_BRIDGE_MODEL).toBe('grok-4.7-xhigh')
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('true')

    await w.get('[data-test="agent-cli-model-back"]').trigger('click')
    await flushPromises()
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
    expect(w.get('[data-test="agent-cli-model-follow"]').attributes('aria-checked')).toBe('true')
    expect(w.find('[data-testid="cli-model-select"]').exists()).toBe(false)
  })

  it('keeps a pinned model even when it matches the workspace model', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'gpt-5' },
    })
    const draft = draftFor({ env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-5' }] })
    const w = mountPanel(draft)
    await flushPromises()
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="agent-cli-model-follow"]').text()).toContain('默认')
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('GPT-5')
  })

  it('keeps the pinned model when the workspace model is a different id', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' },
    })
    const draft = draftFor({ env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-5' }] })
    const w = mountPanel(draft)
    await flushPromises()
    const trigger = w.get('[data-testid="cli-model-trigger"]').text()
    expect(w.get('[data-test="agent-cli-model-own"]').attributes('aria-checked')).toBe('true')
    expect(trigger).toContain('GPT-5')
    expect(trigger).not.toContain('grok-4.7-xhigh')
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
  })

  it('stays on auto when the same CLI has no shared model', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
    const w = mountPanel(draftFor())
    await flushPromises()
    expect(w.find('[data-test="agent-cli-model-mode"]').exists()).toBe(false)
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    expect(w.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(w.text()).not.toContain('不指定 --model，使用 CLI 默认')
  })

  it('returns to follow after leaving the separate choice without a pick', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' },
    })
    const draft = draftFor()
    const w = mountPanel(draft)
    await flushPromises()
    await w.get('[data-test="agent-cli-model-own"]').trigger('click')
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
    mounted.pop()
    w.unmount()
    const again = mountPanel(draft)
    await flushPromises()
    expect(again.get('[data-test="agent-cli-model-follow"]').attributes('aria-checked')).toBe('true')
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
  })

  it('comes back to follow when a CLI switch lands on a shared model', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({
      acpBackend: 'cursor',
      env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' },
    })
    const draft = draftFor()
    const w = mountPanel(draft)
    await flushPromises()
    await w.get('[data-test="agent-cli-model-own"]').trigger('click')
    await w.findAll('button').find((b) => b.text().includes('Codex'))!.trigger('click')
    await flushPromises()
    expect(draft.acpBackend).toBe('codex')
    expect(w.find('[data-test="agent-cli-model-mode"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    await w.findAll('button').find((b) => b.text().includes('Cursor'))!.trigger('click')
    await flushPromises()
    expect(draft.acpBackend).toBe('cursor')
    expect('ACP_BRIDGE_MODEL' in envOf(draft)).toBe(false)
    expect(w.get('[data-test="agent-cli-model-follow"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="agent-cli-model-inherited-name"]').text()).toBe('grok-4.7-xhigh')
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
