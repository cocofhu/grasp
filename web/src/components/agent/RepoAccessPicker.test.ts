// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Project } from '@/lib/api/api'
import RepoAccessPicker from './RepoAccessPicker.vue'

const mocks = vi.hoisted(() => ({ listProjects: vi.fn() }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, listProjects: mocks.listProjects } }
})

function repo(id: string, over: Partial<Project> = {}): Project {
  return {
    id,
    name: id,
    repoUrl: `https://github.com/acme/${id}.git`,
    gitCredentialType: 'github_https',
    gitCredentialId: `cred-${id}`,
    createdAt: '',
    updatedAt: '',
    ...over,
  }
}

async function mountPicker(props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  const w = mount(RepoAccessPicker, { props, global: { plugins: [i18n, router] } })
  await flushPromises()
  return w
}

function lastEmit(w: Awaited<ReturnType<typeof mountPicker>>) {
  const all = w.emitted('update:modelValue') || []
  return all[all.length - 1]?.[0]
}

describe('RepoAccessPicker', () => {
  beforeEach(() => {
    mocks.listProjects.mockReset()
  })

  it('toggles repositories and disables ones without their own credential', async () => {
    mocks.listProjects.mockResolvedValue([
      repo('app'),
      repo('bare', { gitCredentialType: undefined, gitCredentialId: undefined }),
      repo('wrong', { gitCredentialType: 'ssh', gitCredentialId: 'k' }),
    ])
    const w = await mountPicker({ modelValue: [] })
    await w.get('[data-testid="repo-access-app"] input').setValue(true)
    expect(lastEmit(w)).toEqual(['app'])
    expect(w.get('[data-testid="repo-access-bare"] input').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="repo-access-bare"]').text()).toContain('未配置访问凭据')
    expect(w.get('[data-testid="repo-access-wrong"]').text()).toContain('地址与访问方式不匹配')
    await w.get('[data-testid="repo-access-select-all"]').trigger('click')
    expect(lastEmit(w)).toEqual(['app'])
  })

  it('inherits the workspace default until switched to custom', async () => {
    mocks.listProjects.mockResolvedValue([repo('app'), repo('lib')])
    const w = await mountPicker({ modelValue: undefined, inherited: ['lib'] })
    expect(w.get('[data-testid="repo-access-inherited"]').text()).toContain('lib')
    expect(w.find('[data-testid="repo-access-app"]').exists()).toBe(false)
    await w.get('[data-testid="repo-access-inherit"]').trigger('click')
    expect(lastEmit(w)).toEqual(['lib'])
    await w.setProps({ modelValue: ['lib'] })
    await w.get('[data-testid="repo-access-inherit"]').trigger('click')
    expect(lastEmit(w)).toBeUndefined()
  })

  it('lets a deleted repository be removed from the selection', async () => {
    mocks.listProjects.mockResolvedValue([repo('app')])
    const w = await mountPicker({ modelValue: ['gone', 'app'] })
    const missing = w.get('[data-testid="repo-access-missing"]')
    expect(missing.text()).toContain('gone')
    await missing.get('button').trigger('click')
    expect(lastEmit(w)).toEqual(['app'])
  })

  it('shows an empty state with a link to add a repository', async () => {
    mocks.listProjects.mockResolvedValue([])
    const w = await mountPicker({ modelValue: [], shared: true })
    expect(w.get('[data-testid="repo-access-empty"]').text()).toContain('还没有登记仓库')
  })
})
