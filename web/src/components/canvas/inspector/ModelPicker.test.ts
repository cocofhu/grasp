// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { CredentialItem, ModelConnection } from '@/lib/api/api'
import type { CanvasAgent } from '../composables/outlets'
import ModelPicker from './ModelPicker.vue'

const routingApi = vi.hoisted(() => ({ defaults: vi.fn(), connections: vi.fn(), configurations: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getModelDefaults: routingApi.defaults, listModelConnections: routingApi.connections, listModelConfigurations: routingApi.configurations } }))

const catalogs: Record<string, { id: string; name?: string }[]> = {
  deepseek: [{ id: 'deepseek-chat', name: 'DeepSeek V3' }, { id: 'deepseek-reasoner', name: 'DeepSeek R1' }],
  openai: [{ id: 'gpt-4o', name: 'GPT-4o' }, { id: 'o3', name: 'o3' }],
}
vi.mock('@/lib/agent/openCodeCatalog', () => ({
  loadOpenCodeModels: (id: string) => Promise.resolve(catalogs[id] || []),
}))
vi.mock('@/lib/agent/cliCatalog', () => ({
  loadCliModels: (id: string) =>
    Promise.resolve(
      id === 'cursor'
        ? [
            { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['reasoning'], context: 400 },
            { id: 'grok-4.7-xhigh', name: 'Grok 4.7 Extra High', vendor: 'xai' },
          ]
        : [],
    ),
}))

const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...canvas } } })

const key = (id: string, name: string, model: string): CredentialItem => ({
  id,
  type: 'ai',
  provider: 'opencode',
  name,
  envKey: 'GRASP_OPENCODE_API_KEY',
  configured: true,
  metadata: { provider: model.split('/')[0], model },
})
const credentials = [
  key('k1', '工作号', 'deepseek/deepseek-chat'),
  key('k2', '视觉评审', 'openai/gpt-4o'),
  key('k3', '备用号', 'deepseek/deepseek-reasoner'),
  key('k4', '内网网关', 'custom/glm-4-plus'),
  { id: 'g1', type: 'git', provider: 'github', name: 'GitHub', envKey: 'GITHUB_TOKEN', configured: true },
]
const oc: CanvasAgent = { name: 'Impl', acpBackend: 'opencode', openCodeCredentialId: 'k1' }

type Props = { credentialId?: string; modelId?: string; agent?: CanvasAgent | null; credentials?: CredentialItem[] | null }

async function mountPicker(props: Props) {
  const w = mount(ModelPicker, {
    props: { credentialId: '', modelId: '', agent: oc, credentials, ...props },
    global: { plugins: [i18n], stubs: { Icon: true, RouterLink: RouterLinkStub, CredentialProviderLogo: true } },
    attachTo: document.body,
  })
  await flushPromises()
  return w
}

beforeEach(() => {
  document.body.innerHTML = ''
  routingApi.defaults.mockReset().mockResolvedValue({ bindingsByBackend: {} })
  routingApi.connections.mockReset().mockResolvedValue({ items: [] })
  routingApi.configurations.mockReset().mockResolvedValue({ items: [] })
})

describe('ModelPicker independent bindings', () => {
  const serviceKey: CredentialItem = { id: 'shared-key', type: 'ai', name: '共享模型密钥', purpose: 'model_api', issuer: 'deepseek', configured: true }
  const service: ModelConnection = { id: 'service', name: 'DeepSeek 服务连接', providerId: 'deepseek', protocol: 'anthropic-messages', baseUrl: 'https://api.deepseek.com/anthropic', credentialId: serviceKey.id, auth: { scheme: 'bearer' }, models: [{ id: 'original/vendor/model-v1', source: 'manual' }], state: 'active', revision: 1 }

  it('expands a named model configuration into its credential and raw model, ignoring legacy fields', async () => {
    routingApi.configurations.mockResolvedValue({ items: [{ id: 'named', name: '研发模型', backend: 'opencode', state: 'active', revision: 1, binding: { kind: 'connection', connectionId: service.id, modelId: 'original/vendor/model-v1' } }] })
    routingApi.connections.mockResolvedValue({ items: [service] })
    const w = await mountPicker({ agent: { name: 'Impl', acpBackend: 'opencode', openCodeCredentialId: 'k1', modelBinding: { kind: 'configuration', configurationId: 'named' }, env: { ACP_BRIDGE_MODEL: 'stale-model' } }, credentials: [serviceKey] })
    expect(w.get('[data-testid="model-picker-binding-configuration"]').text()).toBe('研发模型')
    expect(w.get('[data-testid="model-picker-binding-source"]').text()).toContain(serviceKey.name)
    expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('original/vendor/model-v1')
    expect(w.text()).not.toContain('stale-model')
    expect(w.find('[data-testid="model-picker-trigger"]').exists()).toBe(false)
    w.unmount()
  })

  it('expands inherit to a platform configuration and fails closed if its tool changes', async () => {
    const platformKey: CredentialItem = { id: 'platform', type: 'ai', name: 'Cursor 团队授权', purpose: 'tool_auth', toolId: 'cursor', configured: true }
    routingApi.defaults.mockResolvedValue({ bindingsByBackend: { cursor: { kind: 'configuration', configurationId: 'named' }, claude_code: { kind: 'configuration', configurationId: 'named' } } })
    routingApi.configurations.mockResolvedValue({ items: [{ id: 'named', name: '团队平台模型', backend: 'cursor', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: platformKey.id, modelId: 'platform/model-v1' } }] })
    const w = await mountPicker({ agent: { name: 'Inherited', acpBackend: 'cursor', modelBinding: { kind: 'inherit' } }, credentials: [platformKey] })
    expect(w.get('[data-testid="model-picker-binding-configuration"]').text()).toBe('团队平台模型')
    expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('platform/model-v1')
    expect(w.text()).toContain('继承工作区默认')
    await w.setProps({ agent: { name: 'Inherited', acpBackend: 'claude_code', modelBinding: { kind: 'inherit' }, env: { ACP_BRIDGE_MODEL: 'stale' } } }); await flushPromises()
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('不匹配')
    expect(w.find('[data-testid="model-picker-binding-model"]').exists()).toBe(false)
    expect(w.text()).not.toContain('stale')
    w.unmount()
  })

  it('reports missing, disabled and failed configuration lookups without falling back', async () => {
    const w = await mountPicker({ agent: { name: 'Gone', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'missing' }, env: { ACP_BRIDGE_MODEL: 'stale' } } })
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('不存在或已停用')
    routingApi.configurations.mockResolvedValueOnce({ items: [{ id: 'disabled', name: 'Disabled', backend: 'cursor', state: 'disabled', binding: { kind: 'platform', credentialId: 'key' } }] })
    await w.setProps({ agent: { name: 'Gone', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'disabled' } } }); await flushPromises()
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('不存在或已停用')
    routingApi.configurations.mockRejectedValueOnce(new Error('configurations unavailable'))
    await w.setProps({ agent: { name: 'Gone', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'unavailable' } } }); await flushPromises()
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toBe('configurations unavailable')
    expect(w.find('[data-testid="model-picker-cli-model"]').exists()).toBe(false)
    w.unmount()
  })

  it('shows a platform binding read-only instead of a stale model environment variable', async () => {
    const platformKey: CredentialItem = { id: 'platform', type: 'ai', name: 'Cursor 团队授权', purpose: 'tool_auth', toolId: 'cursor', configured: true }
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor', modelBinding: { kind: 'platform', credentialId: platformKey.id, modelId: 'chosen-model' }, env: { ACP_BRIDGE_MODEL: 'stale-model' } }, credentials: [platformKey] })
    expect(w.attributes('data-state')).toBe('binding')
    expect(w.get('[data-testid="model-picker-binding-source"]').text()).toContain('Cursor 团队授权')
    expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('chosen-model')
    expect(w.text()).not.toContain('stale-model')
    expect(w.find('[data-testid="model-picker-own-auth"]').exists()).toBe(false)
    w.unmount()
  })

  it('resolves a connection name, raw model ID and role models without exposing legacy key selectors', async () => {
    routingApi.connections.mockResolvedValue({ items: [service] })
    const w = await mountPicker({ agent: { name: 'Impl', acpBackend: 'opencode', openCodeCredentialId: 'k1', modelBinding: { kind: 'connection', connectionId: service.id, modelId: 'original/vendor/model-v1', toolSettings: { modelRoles: { subagent: 'original/vendor/model-v1' } } } }, credentials: [serviceKey] })
    expect(w.get('[data-testid="model-picker-binding-source"]').text()).toContain('DeepSeek 服务连接')
    expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('original/vendor/model-v1')
    expect(w.text()).toContain('子 Agent')
    expect(w.find('[data-testid="model-picker-trigger"]').exists()).toBe(false)
    const link = w.findComponent(RouterLinkStub)
    expect(link.props('to')).toEqual({ path: '/agents', query: { agent: 'Impl', studioTab: 'meta' } })
    w.unmount()
  })

  it('inherits only the current tool default and reports a missing default after changing tools', async () => {
    routingApi.defaults.mockResolvedValue({ bindingsByBackend: { claude_code: { kind: 'connection', connectionId: service.id, modelId: 'original/vendor/model-v1' } } })
    routingApi.connections.mockResolvedValue({ items: [service] })
    const w = await mountPicker({ agent: { name: 'Inherited', acpBackend: 'claude_code', modelBinding: { kind: 'inherit' } }, credentials: [serviceKey] })
    expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('original/vendor/model-v1')
    expect(w.text()).toContain('继承工作区默认')
    await w.setProps({ agent: { name: 'Inherited', acpBackend: 'cursor', modelBinding: { kind: 'inherit' }, env: { ACP_BRIDGE_MODEL: 'old-cursor-model' } } })
    await flushPromises()
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('当前工具没有可继承的工作区默认')
    expect(w.find('[data-testid="model-picker-binding-model"]').exists()).toBe(false)
    expect(w.text()).not.toContain('old-cursor-model')
    w.unmount()
  })

  it('shows API failures and disabled connections explicitly without falling back to legacy fields', async () => {
    routingApi.defaults.mockRejectedValueOnce(new Error('defaults unavailable'))
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor', modelBinding: { kind: 'inherit' }, env: { ACP_BRIDGE_MODEL: 'stale' } } })
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toBe('defaults unavailable')
    expect(w.find('[data-testid="model-picker-cli-model"]').exists()).toBe(false)
    routingApi.connections.mockResolvedValue({ items: [{ ...service, state: 'disabled' }] })
    await w.setProps({ agent: { name: 'C', acpBackend: 'claude_code', modelBinding: { kind: 'connection', connectionId: service.id, modelId: 'original/vendor/model-v1' } } })
    await flushPromises()
    expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('启用的模型连接')
    w.unmount()
  })
})

describe('ModelPicker credential', () => {
  it('follows the Agent by default and shows its current key', async () => {
    const w = await mountPicker({})
    const text = w.get('[data-testid="model-picker-trigger"]').text()
    expect(text).toContain('跟随 Agent')
    expect(text).toContain('工作号')
  })

  it('lists only model keys and emits the picked id', async () => {
    const w = await mountPicker({})
    await w.get('[data-testid="model-picker-trigger"]').trigger('click')
    expect(w.find('[data-testid="model-option-g1"]').exists()).toBe(false)
    await w.get('[data-testid="model-option-k2"]').trigger('click')
    expect(w.emitted('update:credentialId')?.[0]).toEqual(['k2'])
  })

  it('flags a selection that no longer exists and disables the model', async () => {
    const w = await mountPicker({ credentialId: 'gone' })
    expect(w.get('[data-testid="model-picker-trigger"]').text()).toContain('已失效')
    expect(w.get('[data-testid="model-id-trigger"]').attributes('disabled')).toBeDefined()
  })

  it('warns when following an Agent without a model key', async () => {
    const w = await mountPicker({ agent: { ...oc, openCodeCredentialId: '' } })
    expect(w.get('[data-testid="model-picker-follow-line"]').text()).toContain('运行会失败')
  })

  it('clears the node model when the credential switches vendor', async () => {
    const w = await mountPicker({ modelId: 'deepseek/deepseek-reasoner' })
    await w.get('[data-testid="model-picker-trigger"]').trigger('click')
    await w.get('[data-testid="model-option-k2"]').trigger('click')
    expect(w.emitted('update:modelId')?.[0]).toEqual([''])
  })

  it('keeps the node model when the new credential has the same vendor', async () => {
    const w = await mountPicker({ modelId: 'deepseek/deepseek-reasoner' })
    await w.get('[data-testid="model-picker-trigger"]').trigger('click')
    await w.get('[data-testid="model-option-k3"]').trigger('click')
    expect(w.emitted('update:modelId')).toBeUndefined()
  })
})

describe('ModelPicker model', () => {
  it('uses the effective credential default and lists its vendor catalog', async () => {
    const w = await mountPicker({ credentialId: 'k2' })
    expect(w.get('[data-testid="model-id-trigger"]').text()).toContain('gpt-4o')
    await w.get('[data-testid="model-id-trigger"]').trigger('click')
    expect(w.find('[data-testid="model-id-option-o3"]').exists()).toBe(true)
    expect(w.find('[data-testid="model-id-option-deepseek-chat"]').exists()).toBe(false)
    await w.get('[data-testid="model-id-option-o3"]').trigger('click')
    expect(w.emitted('update:modelId')?.[0]).toEqual(['openai/o3'])
  })

  it('filters by search and offers a typed id the catalog lacks', async () => {
    const w = await mountPicker({})
    await w.get('[data-testid="model-id-trigger"]').trigger('click')
    await w.get('[data-testid="model-id-search"]').setValue('reason')
    expect(w.find('[data-testid="model-id-option-deepseek-chat"]').exists()).toBe(false)
    expect(w.find('[data-testid="model-id-option-deepseek-reasoner"]').exists()).toBe(true)
    await w.get('[data-testid="model-id-search"]').setValue('deepseek-v9')
    await w.get('[data-testid="model-id-option-typed"]').trigger('click')
    expect(w.emitted('update:modelId')?.[0]).toEqual(['deepseek/deepseek-v9'])
  })

  it('picking the credential default stores nothing', async () => {
    const w = await mountPicker({ modelId: 'deepseek/deepseek-reasoner' })
    await w.get('[data-testid="model-id-trigger"]').trigger('click')
    await w.get('[data-testid="model-id-option-deepseek-chat"]').trigger('click')
    expect(w.emitted('update:modelId')?.[0]).toEqual([''])
  })

  it('marks a model the vendor catalog does not list', async () => {
    const w = await mountPicker({ modelId: 'deepseek/deepseek-v9' })
    expect(w.get('[data-testid="model-id-sub"]').text()).toContain('厂商目录里没有')
  })

  it('asks for a typed id on a self-hosted endpoint', async () => {
    const w = await mountPicker({ credentialId: 'k4' })
    await w.get('[data-testid="model-id-trigger"]').trigger('click')
    expect(w.find('[data-testid="model-id-no-catalog"]').exists()).toBe(true)
  })
})

describe('ModelPicker states', () => {
  it('explains that non-OpenCode Agents ignore the node model and clears both picks', async () => {
    const w = await mountPicker({ credentialId: 'k1', modelId: 'deepseek/deepseek-chat', agent: { name: 'C', acpBackend: 'cursor' } })
    expect(w.find('[data-testid="model-picker-trigger"]').exists()).toBe(false)
    expect(w.get('[data-testid="model-picker-own-auth"]').text()).toContain('Cursor')
    await w.get('[data-testid="model-picker-clear-ignored"]').trigger('click')
    expect(w.emitted('update:credentialId')?.[0]).toEqual([''])
    expect(w.emitted('update:modelId')?.[0]).toEqual([''])
  })

  it('shows a CLI Agent model read-only and links to its settings', async () => {
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor', env: { ACP_BRIDGE_MODEL: 'gpt-5' } } })
    const chip = w.get('[data-testid="model-picker-cli-model"]')
    expect(chip.text()).toContain('GPT-5')
    expect(chip.text()).toContain('--model gpt-5')
    const link = w.findAllComponents(RouterLinkStub).find((l) => l.attributes('data-testid') === 'model-picker-cli-model-edit')!
    expect(link.props('to')).toEqual({ path: '/agents', query: { agent: 'C', studioTab: 'meta' } })
  })

  it('says auto when a CLI Agent pins no model', async () => {
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor' } })
    expect(w.get('[data-testid="model-picker-cli-model"]').text()).toContain('自动')
  })

  it('shows a listed Cursor model name and its vendor mark', async () => {
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor', env: { ACP_BRIDGE_MODEL: 'grok-4.7-xhigh' } } })
    const chip = w.get('[data-testid="model-picker-cli-model"]')
    expect(chip.text()).toContain('Grok 4.7 Extra High')
    expect(chip.text()).toContain('--model grok-4.7-xhigh')
    expect(chip.get('[data-vendor-mark]').attributes('data-vendor-mark')).toBe('xai')
  })

  it('keeps an unlisted Cursor id as the raw id with an unknown mark', async () => {
    const w = await mountPicker({ agent: { name: 'C', acpBackend: 'cursor', env: { ACP_BRIDGE_MODEL: 'my-private-model' } } })
    const chip = w.get('[data-testid="model-picker-cli-model"]')
    expect(chip.text()).toContain('my-private-model')
    expect(chip.get('[data-vendor-mark]').attributes('data-vendor-mark')).toBe('unknown')
  })

  it('asks for an Agent first', async () => {
    const w = await mountPicker({ agent: null })
    expect(w.attributes('data-state')).toBe('no-agent')
  })
})
