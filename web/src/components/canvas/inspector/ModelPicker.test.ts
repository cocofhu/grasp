// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { CredentialItem } from '@/lib/api/api'
import type { CanvasAgent } from '../composables/outlets'
import ModelPicker from './ModelPicker.vue'

const catalogs: Record<string, { id: string; name?: string }[]> = {
  deepseek: [{ id: 'deepseek-chat', name: 'DeepSeek V3' }, { id: 'deepseek-reasoner', name: 'DeepSeek R1' }],
  openai: [{ id: 'gpt-4o', name: 'GPT-4o' }, { id: 'o3', name: 'o3' }],
}
vi.mock('@/lib/agent/openCodeCatalog', () => ({
  loadOpenCodeModels: (id: string) => Promise.resolve(catalogs[id] || []),
}))
vi.mock('@/lib/agent/cliCatalog', () => ({
  loadCliModels: (id: string) =>
    Promise.resolve(id === 'cursor' ? [{ id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['reasoning'], context: 400 }] : []),
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

  it('asks for an Agent first', async () => {
    const w = await mountPicker({ agent: null })
    expect(w.attributes('data-state')).toBe('no-agent')
  })
})
