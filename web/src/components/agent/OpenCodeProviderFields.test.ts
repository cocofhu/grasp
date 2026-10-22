// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import OpenCodeProviderFields from './OpenCodeProviderFields.vue'
import { resetOpenCodeCatalog } from '@/lib/agent/openCodeCatalog'

const openCodeProviders = vi.fn()
const openCodeModels = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    openCodeProviders: (...args: unknown[]) => openCodeProviders(...args),
    openCodeModels: (...args: unknown[]) => openCodeModels(...args),
  },
}))

const CATALOG = [
  { id: 'anthropic', name: 'Anthropic', models: 2 },
  { id: 'deepseek', name: 'DeepSeek', api: 'https://api.deepseek.com', models: 3 },
]
const DEEPSEEK_MODELS = [
  { id: 'deepseek-v4-flash', name: 'DeepSeek V4 Flash' },
  { id: 'deepseek-v4-pro', name: 'DeepSeek V4 Pro' },
]

function mountFields(props: Record<string, unknown> = {}) {
  return mount(OpenCodeProviderFields, {
    props: { provider: 'deepseek', baseUrl: '', model: '', ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })],
      stubs: { Teleport: true, Icon: true },
    },
  })
}

async function openPanel(wrapper: ReturnType<typeof mountFields>, field: string) {
  await wrapper.get(`[data-test="${field}"] [data-test="app-select-trigger"]`).trigger('click')
}

describe('OpenCodeProviderFields', () => {
  beforeEach(() => {
    resetOpenCodeCatalog()
    openCodeProviders.mockReset().mockResolvedValue({ providers: CATALOG })
    openCodeModels.mockReset().mockResolvedValue({ models: DEEPSEEK_MODELS })
  })

  it('lists catalog vendors and keeps custom on the end', async () => {
    const wrapper = mountFields()
    await flushPromises()
    await openPanel(wrapper, 'opencode-provider')
    expect(wrapper.find('[data-test="app-select-option-deepseek"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="app-select-option-anthropic"]').exists()).toBe(true)
    // A private gateway is never in the catalog, so the option is ours to add.
    expect(wrapper.find('[data-test="app-select-option-custom"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it("offers the vendor's models as provider/model ids", async () => {
    const wrapper = mountFields()
    await flushPromises()
    await openPanel(wrapper, 'opencode-model')
    const option = wrapper.get('[data-test="app-select-option-deepseek/deepseek-v4-pro"]')
    expect(option.text()).toContain('deepseek-v4-pro')
    await option.trigger('click')
    expect(wrapper.emitted('update:model')?.[0]).toEqual(['deepseek/deepseek-v4-pro'])
    wrapper.unmount()
  })

  // The catalog really lists `openrouter/auto` under `openrouter`. Treating that
  // first segment as the vendor prefix would ask the endpoint for `auto`.
  it('keeps the vendor prefix on a self-prefixed catalog id', async () => {
    openCodeModels.mockResolvedValue({ models: [{ id: 'openrouter/auto', name: 'Auto' }] })
    const wrapper = mountFields({ provider: 'openrouter' })
    await flushPromises()
    await openPanel(wrapper, 'opencode-model')
    const option = wrapper.get('[data-test="app-select-option-openrouter/openrouter/auto"]')
    await option.trigger('click')
    expect(wrapper.emitted('update:model')?.[0]).toEqual(['openrouter/openrouter/auto'])
    wrapper.unmount()
  })

  // The failure this warning prevents is silent: OpenCode exits 1 with no stderr.
  it('warns about a model the catalog does not list', async () => {
    const wrapper = mountFields({ model: 'deepseek/deepseek-reasoner' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(true)

    await wrapper.setProps({ model: 'deepseek/deepseek-v4-pro' })
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // A hand-typed bare id names the same model as the prefixed one.
  it('accepts a bare id the vendor does list', async () => {
    const wrapper = mountFields({ model: 'deepseek-v4-pro' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // A gateway publishes ids with a slash inside; that slash is not a vendor.
  it('leaves a slashed gateway model id alone', async () => {
    openCodeModels.mockResolvedValue({ models: [] })
    const wrapper = mountFields({
      provider: 'custom',
      model: 'deepseek/deepseek-flash',
      baseUrl: 'https://tokenhub.example/v1',
    })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)
    await wrapper.setProps({ provider: 'deepseek' })
    await flushPromises()
    expect(wrapper.emitted('update:model')).toBeUndefined()
    wrapper.unmount()
  })

  // Typing a vendor the catalog never heard of is a supported way to name your
  // own gateway, so it explains the endpoint rather than rejecting the id.
  it('treats a vendor outside the catalog as a self-hosted endpoint', async () => {
    const wrapper = mountFields({ provider: 'tokenhub', model: 'deepseek/deepseek-flash' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-provider-self-hosted"]').exists()).toBe(true)
    // Its model list is the endpoint's, so no catalog warning applies.
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)

    await wrapper.setProps({ provider: 'deepseek' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-provider-self-hosted"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // plan g1.1: the empty service address hints the picked vendor's own default
  // address instead of one globally fixed sample domain.
  it("shows the picked vendor's catalog address as the service address placeholder", async () => {
    const wrapper = mountFields({ provider: 'deepseek' })
    await flushPromises()
    expect(wrapper.get('[data-test="opencode-base-url"]').attributes('placeholder')).toBe(
      'https://api.deepseek.com',
    )
    wrapper.unmount()
  })

  it('falls back to neutral copy when a catalog vendor has no default address', async () => {
    const wrapper = mountFields({ provider: 'anthropic' })
    await flushPromises()
    const placeholder = wrapper.get('[data-test="opencode-base-url"]').attributes('placeholder') || ''
    expect(placeholder).not.toContain('example')
    expect(placeholder).not.toBe('https://api.deepseek.com')
    expect(placeholder).toBe('留空则使用厂商默认地址')
    wrapper.unmount()
  })

  it('falls back to neutral copy for a self-hosted gateway', async () => {
    const wrapper = mountFields({ provider: 'custom' })
    await flushPromises()
    const placeholder = wrapper.get('[data-test="opencode-base-url"]').attributes('placeholder') || ''
    expect(placeholder).not.toContain('example')
    expect(placeholder).toBe('填写你的接口根地址')
    wrapper.unmount()
  })

  it('falls back to neutral copy when the catalog is unreachable', async () => {
    openCodeProviders.mockRejectedValue(new Error('offline'))
    const wrapper = mountFields({ provider: 'deepseek' })
    await flushPromises()
    const placeholder = wrapper.get('[data-test="opencode-base-url"]').attributes('placeholder') || ''
    expect(placeholder).not.toContain('example')
    expect(placeholder).toBe('留空则使用厂商默认地址')
    wrapper.unmount()
  })

  it('updates the placeholder on a vendor switch without touching the typed address', async () => {
    const wrapper = mountFields({ provider: 'deepseek', baseUrl: 'https://my-proxy.example/v1' })
    await flushPromises()
    const input = wrapper.get('[data-test="opencode-base-url"]')
    expect(input.attributes('placeholder')).toBe('https://api.deepseek.com')

    await wrapper.setProps({ provider: 'anthropic' })
    await flushPromises()
    expect(input.attributes('placeholder')).toBe('留空则使用厂商默认地址')
    expect((input.element as HTMLInputElement).value).toBe('https://my-proxy.example/v1')
    wrapper.unmount()
  })

  it('does not call a required service address optional', async () => {
    const wrapper = mountFields()
    await flushPromises()
    expect(wrapper.get('[data-test="opencode-base-hint"]').text()).toContain('一般不用填写')

    await wrapper.setProps({ provider: 'custom' })
    await flushPromises()
    const hint = wrapper.get('[data-test="opencode-base-hint"]').text()
    expect(hint).not.toContain('一般不用填写')
    expect(hint).toContain('/v1')
    wrapper.unmount()
  })

  it('stays quiet about a custom gateway model', async () => {
    openCodeModels.mockResolvedValue({ models: [] })
    const wrapper = mountFields({ provider: 'custom', model: 'my-model', baseUrl: 'https://llm.example/v1' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('drops a model picked from the vendor left behind', async () => {
    const wrapper = mountFields({ model: 'deepseek/deepseek-v4-pro' })
    await flushPromises()
    await wrapper.setProps({ provider: 'anthropic' })
    await flushPromises()
    expect(wrapper.emitted('update:model')?.[0]).toEqual([''])
    wrapper.unmount()
  })

  it('keeps a bare model id across a vendor switch', async () => {
    const wrapper = mountFields({ provider: 'custom', model: 'my-model' })
    await flushPromises()
    await wrapper.setProps({ provider: 'deepseek' })
    await flushPromises()
    expect(wrapper.emitted('update:model')).toBeUndefined()
    wrapper.unmount()
  })

  it('stops looking loaded when the user leaves a vendor before its models arrive', async () => {
    let resolveModels: (value: { models: typeof DEEPSEEK_MODELS }) => void = () => {}
    openCodeModels.mockImplementation(
      () => new Promise<{ models: typeof DEEPSEEK_MODELS }>((resolve) => {
        resolveModels = resolve
      }),
    )
    const wrapper = mountFields({ provider: 'deepseek' })
    await flushPromises()
    await wrapper.setProps({ provider: 'custom' })
    await flushPromises()
    await openPanel(wrapper, 'opencode-model')
    expect(wrapper.find('[data-test="app-select-loading"]').exists()).toBe(false)
    resolveModels({ models: DEEPSEEK_MODELS })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('offers vision for a catalog vendor when the model is typed in', async () => {
    const wrapper = mountFields({
      provider: 'deepseek',
      model: 'deepseek/deepseek-flash',
      vision: false,
    })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-unknown"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="opencode-model-vision"]').exists()).toBe(true)
    await wrapper.get('[data-test="opencode-model-vision-input"]').setValue(true)
    expect(wrapper.emitted('update:vision')?.[0]).toEqual([true])

    await wrapper.setProps({ model: 'deepseek/deepseek-v4-pro', vision: false })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-vision"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps vision when a catalog vendor has no model list yet', async () => {
    openCodeModels.mockResolvedValue({ models: [] })
    const wrapper = mountFields({
      provider: 'tencent-tokenhub',
      model: 'deepseek/deepseek-flash',
      vision: false,
    })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-vision"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back to the shipped shortlist when the catalog is unreachable', async () => {
    openCodeProviders.mockRejectedValue(new Error('offline'))
    const wrapper = mountFields()
    await flushPromises()
    await openPanel(wrapper, 'opencode-provider')
    expect(wrapper.find('[data-test="app-select-option-openai"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="app-select-option-custom"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g1.1: the env var name belongs in code, not in the default-visible label.
  it('labels the model field without the env var name', async () => {
    const wrapper = mountFields()
    await flushPromises()
    expect(wrapper.text()).not.toContain('ACP_BRIDGE_MODEL')
    expect(
      wrapper.get('[data-test="opencode-model"] [data-test="app-select-trigger"]').attributes('aria-label'),
    ).toBe('模型')
    wrapper.unmount()
  })

  // plan g2.1: slash ids and prefix completion live in an advanced note that is
  // collapsed by default, on both desktop and mobile (native <details>).
  it('keeps technical details in a collapsed advanced note', async () => {
    const wrapper = mountFields()
    await flushPromises()
    const adv = wrapper.get('[data-test="opencode-advanced"]')
    expect(adv.attributes('open')).toBeUndefined()
    expect(adv.element.tagName.toLowerCase()).toBe('details')
    expect(adv.text()).toContain('高级说明')
    expect(adv.text()).toContain('厂商前缀')
    expect(adv.text()).toContain('deepseek/deepseek-flash')
    wrapper.unmount()
  })

  it('hides catalog hints only when asked, and keeps typed-endpoint copy', async () => {
    const shown = mountFields()
    await flushPromises()
    expect(shown.get('[data-test="opencode-base-hint"]').text()).toContain('一般不用填写')
    expect(shown.get('[data-test="opencode-model-hint"]').text()).toContain('deepseek-flash')
    expect(shown.get('[data-test="opencode-vision-hint"]').text()).toContain('勾选后，模型才能看图')
    shown.unmount()

    const wrapper = mountFields({ hideCatalogHints: true })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-model-hint"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="opencode-base-hint"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="opencode-vision-hint"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="opencode-model-vision"]').text()).toContain('视觉理解')
    expect(wrapper.get('[data-test="opencode-model-vision-input"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('一般不用填写')
    expect(wrapper.text()).not.toContain('选择列表里的模型')
    expect(wrapper.text()).not.toContain('勾选后，模型才能看图')

    await wrapper.setProps({ provider: 'custom' })
    await flushPromises()
    expect(wrapper.get('[data-test="opencode-model-hint"]').text()).toContain('请直接填写它提供的模型名')
    const hint = wrapper.get('[data-test="opencode-base-hint"]').text()
    expect(hint).not.toContain('一般不用填写')
    expect(hint).toContain('/v1')
    expect(wrapper.get('[data-test="opencode-model-vision"]').text()).toContain('视觉理解')
    expect(wrapper.find('[data-test="opencode-vision-hint"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('leaves catalog hints visible for credentials, shared agents, and the agent wizard', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    const callers = [
      'WizardApiKeyStepPanel.vue',
      'OpenCodeCredentialPicker.vue',
      '../workspace/WorkspaceCredentialsPanel.vue',
      '../workspace/WorkspaceSharedAgentPanel.vue',
    ]
    for (const rel of callers) {
      expect(readFileSync(join(dir, rel), 'utf8')).not.toContain('hide-catalog-hints')
    }
  })
})
