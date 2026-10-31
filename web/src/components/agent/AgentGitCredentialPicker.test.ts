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
  { id: 'gh-1', name: '团队 GitHub', type: 'git', purpose: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true },
  { id: 'gh-2', name: '个人 GitHub', type: 'git', purpose: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true },
  { id: 'gl-1', name: '公司 GitLab', type: 'git', purpose: 'git', provider: 'gitlab', envKey: 'GITLAB_TOKEN', configured: true },
  { id: 'ssh-1', name: '部署私钥', type: 'ssh', provider: 'ssh', purpose: 'ssh', secretKind: 'ssh_private_key', envKey: 'GIT_SSH_PRIVATE_KEY', knownHosts: 'git.example.test ssh-ed25519 AAAA', configured: true },
  { id: 'ssh-2', name: '镜像私钥', type: 'ssh', provider: 'ssh', purpose: 'ssh', secretKind: 'ssh_private_key', envKey: 'GIT_SSH_PRIVATE_KEY', configured: true },
  { id: 'hosts-1', name: '可信主机', type: 'ssh', provider: 'ssh', purpose: 'ssh', secretKind: 'ssh_known_hosts' as CredentialItem['secretKind'], envKey: 'GIT_SSH_KNOWN_HOSTS', configured: true },
  { id: 'disabled', name: '已停用', type: 'git', purpose: 'git', provider: 'github', configured: true, enabled: false },
  { id: 'cleared', name: '已清空', type: 'git', purpose: 'git', provider: 'gitlab', configured: false },
  { id: 'old-gitlab', name: 'Old GitLab', type: 'git', provider: 'gitlab', configured: true },
  { id: 'ai', name: '模型授权', type: 'ai', provider: 'cursor', configured: true },
]
const mounted: ReturnType<typeof mount>[] = []
function setup(props: { modelValue?: AgentGitBindings; items?: CredentialItem[]; disabled?: boolean; } = { modelValue: { items: [] } }) {
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
    for (const id of ['hosts-1', 'ai', 'disabled', 'cleared', 'old-gitlab']) expect(wrapper.find(`[data-test="app-select-option-${id}"]`).exists()).toBe(false)
    expect(wrapper.get('[data-test="app-select-option-gh-1"]').find('[data-provider-logo="github"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="app-select-option-gl-1"]').find('[data-provider-logo="gitlab"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="app-select-option-ssh-1"]').find('[data-provider-logo="ssh-key"]').exists()).toBe(true)
    await wrapper.get('[data-test="app-select-search"]').setValue('公司')
    expect(wrapper.find('[data-test="app-select-option-gh-1"]').exists()).toBe(false)
    await wrapper.get('[data-test="app-select-option-gl-1"]').trigger('click')
    expect(value(wrapper)).toEqual({ items: [{ credentialId: 'gl-1' }] })
    expect(wrapper.text()).not.toContain('可添加多个已保存的授权，并设置首选。')
    expect(wrapper.text()).not.toContain('所选授权均可在沙箱中使用。')
    expect(wrapper.text()).not.toContain('首选')
    expect(wrapper.get('[data-testid="git-credential-row-gl-1"]').find('[data-provider-logo="gitlab"]').exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/GIT_REPOS|GITHUB_TOKEN|GITLAB_TOKEN|secret|推荐变量/)
  })
  it('searches a self-hosted credential by Host and only allows a scope on that origin', async () => {
    const wrapper = setup({ items: [{ id: 'self-hosted', type: 'git', purpose: 'git', provider: 'gitlab', host: 'http://git.company.test:8080', name: 'Team GitLab', configured: true }], modelValue: { items: [] } })
    await choose(wrapper, 'git-credential-add', 'self-hosted', 'company.test')
    expect(wrapper.get('[data-testid="git-credential-row-self-hosted"]').text()).toContain('http://git.company.test:8080')
    await wrapper.get('[data-testid="git-scope-self-hosted"]').setValue('git.company.test:8080')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
    await wrapper.get('[data-testid="git-scope-self-hosted"]').setValue('https://git.company.test:8080/team/repo')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    await wrapper.get('[data-testid="git-scope-self-hosted"]').setValue('http://git.company.test:8080/team/repo')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
  })
  it('keeps multiple credentials from the same provider and makes deliberate removal explicit', async () => {
    const wrapper = setup()
    expect(wrapper.text()).toContain('尚未添加 Git 授权')
    expect(wrapper.text()).not.toContain('可添加多个已保存的授权，并设置首选。')
    await choose(wrapper, 'git-credential-add', 'gh-1')
    await choose(wrapper, 'git-credential-add', 'gh-2')
    await choose(wrapper, 'git-credential-add', 'ssh-1')
    expect(value(wrapper).items.map((item) => item.credentialId)).toEqual(['gh-1', 'gh-2', 'ssh-1'])
    expect(value(wrapper)).not.toHaveProperty('preferredCredentialId')
    expect(wrapper.find('[data-testid^="git-preferred-"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('所选授权均可在沙箱中使用。')
    expect(wrapper.text()).not.toContain('首选')
    await wrapper.get('[data-testid="git-options-gh-1"]').trigger('click')
    expect(wrapper.get('[data-testid="git-scope-gh-1"]').exists()).toBe(true)
    await wrapper.get('[data-testid="git-remove-gh-2"]').trigger('click')
    expect(value(wrapper).items.map((item) => item.credentialId)).toEqual(['gh-1', 'ssh-1'])
    await wrapper.get('[data-testid="git-remove-ssh-1"]').trigger('click')
    await wrapper.get('[data-testid="git-remove-gh-1"]').trigger('click')
    expect(value(wrapper)).toEqual({ items: [] })
    expect(wrapper.text()).toContain('尚未添加 Git 授权')
  })
  it('adds several SSH keys, each with its own scope and known-hosts status', async () => {
    const wrapper = setup()
    await choose(wrapper, 'git-credential-add', 'ssh-1')
    await choose(wrapper, 'git-credential-add', 'ssh-2')
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('git@git.example.test:team/repo.git')
    expect(value(wrapper)).toEqual({ items: [{ credentialId: 'ssh-1', scope: 'git@git.example.test:team/repo.git' }, { credentialId: 'ssh-2' }] })
    expect(wrapper.get('[data-testid="git-credential-subtitle-ssh-1"]').text()).toBe('git@git.example.test:team/repo.git · 已配置已知主机')
    expect(wrapper.get('[data-testid="git-credential-subtitle-ssh-2"]').text()).toBe('SSH · 未配置已知主机')
    expect(wrapper.find('[data-testid^="git-hosts-"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('SSH 主机校验')
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('https://token:secret@git.example.test/team/repo')
    expect(wrapper.get('[data-testid="git-scope-ssh-1"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('')
    expect(value(wrapper).items[0]).toEqual({ credentialId: 'ssh-1' })
  })
  it('retains missing and revoked references without choosing an alternative', async () => {
    const initial: AgentGitBindings = { items: [{ credentialId: 'missing' }, { credentialId: 'gh-1' }] }
    const wrapper = setup({ modelValue: initial })
    expect(wrapper.text()).toContain('授权不可用')
    expect(wrapper.find('[data-testid="git-preferred-missing"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('首选')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ items: items.map((item) => item.id === 'gh-1' ? { ...item, state: 'revoked' as const } : item) })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(value(wrapper)).toEqual(initial)
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
  })
  it('starts empty when no bindings are supplied and does not emit a selection on mount', () => {
    const wrapper = setup({})
    expect(wrapper.find('[data-testid="git-credential-row-gh-1"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
  })
  it('ignores a retired known-hosts binding and drops it on the next change', async () => {
    const retired = { items: [{ credentialId: 'ssh-1', sshHostsCredentialId: 'lost-hosts' }] } as AgentGitBindings
    const wrapper = setup({ modelValue: retired })
    expect(wrapper.text()).not.toContain('授权不可用')
    expect(wrapper.text()).not.toContain('lost-hosts')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
    await wrapper.get('[data-testid="git-scope-ssh-1"]').setValue('git.example.test')
    expect(value(wrapper)).toEqual({ items: [{ credentialId: 'ssh-1', scope: 'git.example.test' }] })
  })
  it('loads, reports failure and retries without losing saved selections', async () => {
    mocks.listCredentials.mockRejectedValueOnce(new Error('offline'))
    const wrapper = setup({ items: undefined, modelValue: { items: [{ credentialId: 'gh-1' }] } })
    await flushPromises()
    expect(wrapper.text()).toContain('offline')
    expect(value(wrapper).items).toEqual([{ credentialId: 'gh-1' }])
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('团队 GitHub')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
  })
  it('locks every mutating control while saving and retains help access', async () => {
    const wrapper = setup({ modelValue: { items: [{ credentialId: 'ssh-1' }] }, disabled: true })
    expect(wrapper.get('[data-testid="git-remove-ssh-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="git-preferred-ssh-1"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="git-scope-ssh-1"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="git-credential-help"]').trigger('click')
    expect(wrapper.emitted('help')).toHaveLength(1)
  })
})
