// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { AgentGitBindings, CredentialItem } from '@/lib/api/api'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'
const mocks = vi.hoisted(() => ({ listCredentials: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const items: CredentialItem[] = [
  { id: 'gh-1', name: '团队 GitHub', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true },
  { id: 'gh-2', name: '个人 GitHub', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true },
  { id: 'gl-1', name: '公司 GitLab', type: 'git', provider: 'gitlab', envKey: 'GITLAB_TOKEN', configured: true },
  { id: 'ssh-1', name: '部署私钥', type: 'ssh', provider: 'ssh', envKey: 'GIT_SSH_PRIVATE_KEY', configured: true },
  { id: 'hosts-1', name: '可信主机', type: 'ssh', provider: 'ssh', envKey: 'GIT_SSH_KNOWN_HOSTS', configured: true },
  { id: 'disabled', name: '已停用', type: 'git', provider: 'github', configured: true, enabled: false },
  { id: 'cleared', name: '已清空', type: 'git', provider: 'gitlab', configured: false },
  { id: 'ai', name: '模型授权', type: 'ai', provider: 'cursor', configured: true },
]
const mounted: ReturnType<typeof mount>[] = []
function setup(props: { modelValue?: AgentGitBindings; legacy?: AgentGitBindings; items?: CredentialItem[]; disabled?: boolean; inherited?: AgentGitBindings } = { modelValue: { items: [] } }) {
  const wrapper = mount(AgentGitCredentialPicker, { props: { items, ...props, 'onUpdate:modelValue': (value) => { void wrapper.setProps({ modelValue: value }) } }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })], stubs: { Teleport: true, RouterLink: { template: '<a><slot/></a>' } } } })
  mounted.push(wrapper)
  return wrapper
}
async function choose(wrapper: ReturnType<typeof setup>, testid: string, value: string, search = '') {
  await wrapper.get(`[data-testid="${testid}"] [data-test="app-select-trigger"]`).trigger('click')
  if (search) await wrapper.get('[data-test="app-select-search"]').setValue(search)
  await wrapper.get(`[data-test="app-select-option-${value}"]`).trigger('click')
  await flushPromises()
}
function value(wrapper: ReturnType<typeof setup>) { return wrapper.props('modelValue')! }
beforeEach(() => { vi.clearAllMocks(); mocks.listCredentials.mockResolvedValue({ items }) })
afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()); document.body.innerHTML = '' })

describe('Agent Git credentials', () => {
  it('searches saved Git authorization with logos and excludes hosts, model, disabled and cleared credentials', async () => {
    const wrapper = setup()
    await wrapper.get('[data-testid="git-credential-add"] [data-test="app-select-trigger"]').trigger('click')
    for (const id of ['hosts-1', 'ai', 'disabled', 'cleared']) expect(wrapper.find(`[data-test="app-select-option-${id}"]`).exists()).toBe(false)
    expect(wrapper.get('[data-test="app-select-option-gh-1"]').find('[data-provider-logo="github"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="app-select-option-gl-1"]').find('[data-provider-logo="gitlab"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="app-select-option-ssh-1"]').find('[data-provider-logo="ssh-key"]').exists()).toBe(true)
    await wrapper.get('[data-test="app-select-search"]').setValue('公司')
    expect(wrapper.find('[data-test="app-select-option-gh-1"]').exists()).toBe(false)
    await wrapper.get('[data-test="app-select-option-gl-1"]').trigger('click')
    expect(value(wrapper)).toEqual({ items: [{ credentialId: 'gl-1' }], preferredCredentialId: 'gl-1' })
    expect(wrapper.get('[data-testid="git-credential-row-gl-1"]').find('[data-provider-logo="gitlab"]').exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/GIT_REPOS|GITHUB_TOKEN|GITLAB_TOKEN|secret|推荐变量/)
  })
  it('keeps multiple credentials from the same provider, changes preference and makes deliberate removal explicit', async () => {
    const wrapper = setup()
    await choose(wrapper, 'git-credential-add', 'gh-1')
    await choose(wrapper, 'git-credential-add', 'gh-2')
    await choose(wrapper, 'git-credential-add', 'ssh-1')
    expect(value(wrapper).items.map((item) => item.credentialId)).toEqual(['gh-1', 'gh-2', 'ssh-1'])
    await wrapper.get('[data-testid="git-preferred-gh-2"]').trigger('click')
    expect(value(wrapper).preferredCredentialId).toBe('gh-2')
    await wrapper.get('[data-testid="git-remove-gh-2"]').trigger('click')
    expect(value(wrapper).preferredCredentialId).toBe('gh-1')
    expect(wrapper.get('[data-testid="git-preferred-gh-1"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-testid="git-remove-ssh-1"]').trigger('click')
    await wrapper.get('[data-testid="git-remove-gh-1"]').trigger('click')
    expect(value(wrapper)).toEqual({ items: [] })
  })
  it('pairs saved host verification with an SSH key and preserves per-key scope', async () => {
    const wrapper = setup()
    await choose(wrapper, 'git-credential-add', 'ssh-1')
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('git@git.example.test:team/repo.git')
    await choose(wrapper, 'git-hosts-ssh-1', 'hosts-1')
    expect(value(wrapper)).toEqual({ items: [{ credentialId: 'ssh-1', scope: 'git@git.example.test:team/repo.git', sshHostsCredentialId: 'hosts-1' }], preferredCredentialId: 'ssh-1' })
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('https://token:secret@git.example.test/team/repo')
    expect(wrapper.get('[data-testid="git-scope-ssh-1"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('')
    await choose(wrapper, 'git-hosts-ssh-1', '')
    expect(value(wrapper).items[0]).toEqual({ credentialId: 'ssh-1' })
  })
  it('retains missing and revoked references without choosing an alternative', async () => {
    const initial: AgentGitBindings = { items: [{ credentialId: 'missing' }, { credentialId: 'gh-1' }], preferredCredentialId: 'missing' }
    const wrapper = setup({ modelValue: initial })
    expect(wrapper.text()).toContain('授权不可用')
    expect(wrapper.get('[data-testid="git-preferred-missing"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ items: items.map((item) => item.id === 'gh-1' ? { ...item, state: 'revoked' as const } : item) })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(value(wrapper)).toEqual(initial)
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
  })
  it('does not migrate legacy or inherited authorization on mount and makes empty selection override defaults', async () => {
    const legacy: AgentGitBindings = { items: [{ credentialId: 'gh-1' }], preferredCredentialId: 'gh-1' }
    const wrapper = setup({ legacy })
    expect(wrapper.text()).toContain('当前使用原有授权')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await choose(wrapper, 'git-credential-add', 'gl-1')
    expect(value(wrapper).items).toHaveLength(2)
    expect(legacy.items).toHaveLength(1)
    const inherited = setup({ inherited: legacy })
    expect(inherited.text()).toContain('使用工作区默认授权')
    await inherited.get('[data-testid="git-remove-gh-1"]').trigger('click')
    expect(value(inherited)).toEqual({ items: [] })
  })
  it('reports a missing preferred member and unavailable hosts without repairing them silently', async () => {
    const wrapper = setup({ modelValue: { items: [{ credentialId: 'ssh-1', sshHostsCredentialId: 'lost-hosts' }], preferredCredentialId: 'absent' } })
    expect(wrapper.text()).toContain('请选择一条可用的首选授权')
    expect(wrapper.get('[data-testid="git-hosts-ssh-1"] [data-test="app-select-trigger"]').text()).toContain('lost-hosts')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="git-preferred-ssh-1"]').trigger('click')
    await choose(wrapper, 'git-hosts-ssh-1', '')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
  })
  it('loads, reports failure and retries without losing saved selections', async () => {
    mocks.listCredentials.mockRejectedValueOnce(new Error('offline'))
    const wrapper = setup({ items: undefined, modelValue: { items: [{ credentialId: 'gh-1' }], preferredCredentialId: 'gh-1' } })
    await flushPromises()
    expect(wrapper.text()).toContain('offline')
    expect(value(wrapper).preferredCredentialId).toBe('gh-1')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('团队 GitHub')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
  })
  it('locks every mutating control while saving and retains help access', async () => {
    const wrapper = setup({ modelValue: { items: [{ credentialId: 'ssh-1' }], preferredCredentialId: 'ssh-1' }, disabled: true })
    expect(wrapper.get('[data-testid="git-remove-ssh-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="git-preferred-ssh-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="git-scope-ssh-1"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="git-credential-help"]').trigger('click')
    expect(wrapper.emitted('help')).toHaveLength(1)
  })
})
