// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentEnvPanel from './AgentEnvPanel.vue'
import { toDraft } from '@/lib/agent/agentStudioDraft'
describe('model selection owns runtime model environment', () => {
 it.each(['cursor', 'opencode', 'codex'])('rejects model variables in generic env for %s', async backend => {
  const draft = toDraft({ name: 'dev', acpBackend: backend, env: { FEATURE: 'on', ACP_BRIDGE_MODEL: 'derived' } })
  const w = mount(AgentEnvPanel, { props: { draft }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { RouterLink: true, EnvCredentialHelpModal: true, CodeEditor: true } } })
  expect(w.findAll('input[placeholder="KEY"]').map(input => (input.element as HTMLInputElement).value)).toEqual(['FEATURE'])
  await w.get('input[placeholder="KEY"]').setValue('ACP_BRIDGE_MODEL')
  expect(draft.env[0].k).toBe(''); expect(w.emitted('toast')).toBeTruthy()
  expect(w.find('[data-test="env-managed-model"]').exists()).toBe(false); w.unmount()
 })
})
