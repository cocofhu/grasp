// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import RepoCredentialSelect from './RepoCredentialSelect.vue'

const listCredentials = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api/api', () => ({ api: { listCredentials } }))
const copy = pages.pages.projects.form.credential
const github = { id: 'gh', name: 'GitHub token', purpose: 'git', type: 'git', provider: 'github', secretKind: 'token', configured: true, enabled: true }
const gitlab = { id: 'gl', name: 'GitLab token', purpose: 'git', type: 'git', provider: 'gitlab', secretKind: 'token', configured: true, enabled: true }
const ssh = { id: 'key', name: 'Deploy key', purpose: 'ssh', type: 'ssh', provider: 'ssh', secretKind: 'ssh_private_key', configured: true, enabled: true }
const model = { id: 'model', name: 'Model key', purpose: 'model_api', configured: true }

function setup(props: Record<string, unknown>) {
  return mount(RepoCredentialSelect, {
    props: { modelValue: '', ...props },
    attachTo: document.body,
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })],
      stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true },
    },
  })
}
async function optionIds(w: ReturnType<typeof setup>) {
  await w.get('[data-test="app-select-trigger"]').trigger('click')
  return w.findAll('[role="option"]').map((o) => o.attributes('data-test')?.replace('app-select-option-', ''))
}

beforeEach(() => {
  vi.clearAllMocks()
  listCredentials.mockResolvedValue({ items: [github, gitlab, ssh, model] })
})

describe('RepoCredentialSelect', () => {
  it('loads saved credentials and lists only those matching the repository address', async () => {
    const https = setup({ repoUrl: 'https://github.com/org/web.git' })
    await flushPromises()
    expect(listCredentials).toHaveBeenCalledTimes(1)
    expect(await optionIds(https)).toEqual(['', 'gh'])
    https.unmount()
    const viaSsh = setup({ repoUrl: 'git@github.com:org/web.git' })
    await flushPromises()
    expect(await optionIds(viaSsh)).toEqual(['', 'key'])
    viaSsh.unmount()
  })

  it('uses preloaded items without fetching and emits the chosen credential', async () => {
    const w = setup({ repoUrl: 'https://gitlab.com/org/api.git', items: [github, gitlab] })
    await flushPromises()
    expect(listCredentials).not.toHaveBeenCalled()
    await w.get('[data-test="app-select-trigger"]').trigger('click')
    await w.get('[data-test="app-select-option-gl"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual(['gl'])
    w.unmount()
  })

  it('flags a selection that no longer fits or was deleted, and hints when nothing fits', async () => {
    const mismatched = setup({ repoUrl: 'https://gitlab.com/org/api.git', modelValue: 'gh', items: [github] })
    await flushPromises()
    expect(mismatched.get('[data-testid="repo-credential-mismatch"]').text()).toBe(copy.mismatch)
    mismatched.unmount()
    const deleted = setup({ repoUrl: 'https://github.com/org/web.git', modelValue: 'gone', items: [github] })
    await flushPromises()
    expect(deleted.get('[data-testid="repo-credential-mismatch"]').text()).toBe(copy.missing)
    deleted.unmount()
    const none = setup({ repoUrl: 'https://git.example.test/org/x.git', items: [github] })
    await flushPromises()
    expect(none.find('[data-testid="repo-credential-mismatch"]').exists()).toBe(false)
    expect(none.text()).toContain(copy.noneFits)
    none.unmount()
  })

  it('shows a load error with retry', async () => {
    listCredentials.mockRejectedValueOnce(new Error('offline'))
    const w = setup({ repoUrl: 'https://github.com/org/web.git' })
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('offline')
    await w.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(listCredentials).toHaveBeenCalledTimes(2)
    expect(w.find('[role="alert"]').exists()).toBe(false)
    w.unmount()
  })
})
