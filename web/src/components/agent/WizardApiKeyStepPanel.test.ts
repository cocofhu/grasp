// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import commonZh from '@/locales/zh-CN/common.json'
import pagesZh from '@/locales/zh-CN/pages.json'
import { authGuideFor } from '@/lib/agent/backendAuthGuide'
import WizardApiKeyStepPanel from './WizardApiKeyStepPanel.vue'

const CodeEditorStub = {
  name: 'CodeEditor',
  props: ['modelValue', 'language'],
  emits: ['update:modelValue'],
  template:
    '<textarea data-test="custom-config-editor" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

function mountPanel(overrides: Record<string, unknown> = {}) {
  const guide = authGuideFor('claude_code')
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...commonZh, ...pagesZh } },
  })
  return mount(WizardApiKeyStepPanel, {
    props: {
      acpBackend: 'claude_code',
      configRoot: '/root/.claude',
      authMode: 'customConfig',
      apiKeyInput: '',
      customConfigContent: '{\n  "env": {}\n}',
      customConfigError: false,
      authGuide: guide,
      ...overrides,
    },
    global: {
      plugins: [i18n],
      stubs: { CodeEditor: CodeEditorStub },
    },
  })
}

describe('WizardApiKeyStepPanel custom config editor', () => {
  it('uses DeepSeek saved credentials without offering a plaintext config mode', () => {
    const wrapper = mountPanel({
      acpBackend: 'deepseek_harness', configRoot: '/root/.dsh', authMode: 'apiKey',
      authGuide: authGuideFor('deepseek_harness'), pickCredential: true,
      credentialItems: [{ id: 'dsh-credential', name: 'DeepSeek team', type: 'ai', provider: 'deepseek_harness', envKey: 'GRASP_DEEPSEEK_API_KEY', configured: true }],
      selectedCredentialId: 'dsh-credential',
    })
    expect(wrapper.get('[data-kind="deepseek_harness"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="credential-alias-option-dsh-credential"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-test="auth-mode-custom-config"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('gives CodeEditor host an explicit height so Monaco can layout', () => {
    const wrapper = mountPanel()
    const host = wrapper.get('[data-test="custom-config-editor-host"]')
    expect(host.classes()).toContain('h-[220px]')
    expect(host.classes()).not.toContain('min-h-[220px]')
    wrapper.unmount()
  })

  it('shows custom config content and emits edits', async () => {
    const wrapper = mountPanel()
    const editor = wrapper.get('[data-test="custom-config-editor"]')
    expect((editor.element as HTMLTextAreaElement).value).toContain('"env"')

    await editor.setValue('{\n  "ok": true\n}')
    const emitted = wrapper.emitted('update:customConfigContent')
    expect(emitted?.[emitted.length - 1]).toEqual(['{\n  "ok": true\n}'])
    wrapper.unmount()
  })

  it('shows invalid JSON error when customConfigError is set', () => {
    const wrapper = mountPanel({ customConfigError: true })
    expect(wrapper.get('[data-test="custom-config-error"]').text()).toContain('JSON')
    wrapper.unmount()
  })

  it('switches between auth modes without losing custom config section', async () => {
    const wrapper = mountPanel()
    expect(wrapper.find('[data-test="custom-config-editor-host"]').exists()).toBe(true)

    await wrapper.setProps({ authMode: 'apiKey' })
    expect(wrapper.find('[data-test="custom-config-editor-host"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(true)

    await wrapper.setProps({ authMode: 'customConfig' })
    expect(wrapper.find('[data-test="custom-config-editor-host"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('offers saved aliases instead of a paste box that create would drop', async () => {
    const wrapper = mountPanel({
      pickCredential: true,
      authMode: 'apiKey',
      acpBackend: 'cursor',
      configRoot: '/root/.cursor',
      authGuide: authGuideFor('cursor'),
      credentialItems: [
        {
          id: 'c1',
          name: '工作号',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
        },
        {
          id: 'revoked',
          name: '已撤销',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
          revokedAt: '2026-01-01T00:00:00Z',
        },
      ],
      selectedCredentialId: '',
    })
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('选择已有授权')
    expect(wrapper.text()).toContain('工作号')
    expect(wrapper.text()).not.toContain('已撤销')
    expect(wrapper.text()).not.toContain('如何获取')
    await wrapper.get('[data-testid="credential-alias-option-c1"]').trigger('click')
    expect(wrapper.emitted('update:selectedCredentialId')?.[0]).toEqual(['c1'])
    wrapper.unmount()
  })

  it('lets the step be skipped when the backend has no saved credential', () => {
    const wrapper = mountPanel({
      pickCredential: true,
      authMode: 'apiKey',
      acpBackend: 'cursor',
      authGuide: authGuideFor('cursor'),
      credentialItems: [],
    })
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('直接跳过')
    expect(wrapper.find('[data-test="custom-config-editor-host"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('still blocks on invalid custom config while credential picking is available', () => {
    const wrapper = mountPanel({ pickCredential: true, customConfigError: true })
    expect(wrapper.get('[data-test="custom-config-error"]').text()).toContain('JSON')
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('lists an OpenCode model-vendor credential without vendor paste fields', () => {
    const wrapper = mountPanel({
      pickCredential: true,
      acpBackend: 'opencode',
      configRoot: '/root/.config/opencode',
      authMode: 'apiKey',
      authGuide: authGuideFor('opencode'),
      credentialItems: [
        {
          id: 'oc1',
          name: '厂商号',
          type: 'ai',
          provider: 'opencode',
          envKey: 'GRASP_OPENCODE_API_KEY',
          configured: true,
        },
      ],
    })
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="api-key-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('厂商号')
    wrapper.unmount()
  })

  it('shows OpenCode vendor fields and emits base URL updates', async () => {
    const guide = authGuideFor('opencode')
    const wrapper = mountPanel({
      acpBackend: 'opencode',
      configRoot: '/root/.config/opencode',
      authMode: 'apiKey',
      authGuide: guide,
      env: { GRASP_OPENCODE_PROVIDER: 'custom' },
    })
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="opencode-base-required"]').exists()).toBe(true)
    await wrapper.get('[data-test="opencode-base-url"]').setValue('https://llm.example/v1')
    const emitted = wrapper.emitted('update:openCodeBaseUrl')
    expect(emitted?.[emitted.length - 1]).toEqual(['https://llm.example/v1'])
    await wrapper.setProps({ authMode: 'customConfig' })
    expect(wrapper.text()).toContain('opencode.json')
    wrapper.unmount()
  })
})
