// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import RepoChoiceList from './RepoChoiceList.vue'
import type { RepoChoice } from '@/lib/api/apiTypes'

const copy = common.common.repoChoices
const projects = [
  { id: 'p1', name: 'web', repoUrl: 'https://github.com/org/web.git', defaultBranch: 'main' },
  { id: 'p2', name: 'api', repoUrl: 'https://github.com/org/api.git' },
  { id: 'p3', name: 'docs', repoUrl: 'git@github.com:org/docs.git' },
]
function setup(modelValue: RepoChoice[], extra: Record<string, unknown> = {}) {
  const w = mount(RepoChoiceList, {
    props: { projects, modelValue, ...extra, 'onUpdate:modelValue': (value: RepoChoice[]) => w.setProps({ modelValue: value }) },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': common } })],
      stubs: { Icon: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  })
  return w
}
const rowIds = (w: ReturnType<typeof setup>) => w.findAll('[data-testid^="repo-choice-p"], [data-testid="repo-choice-gone"]').map((r) => r.attributes('data-testid'))

describe('RepoChoiceList', () => {
  it('lists chosen repositories first in clone order with a branch input defaulting to the project branch', () => {
    const w = setup([{ projectId: 'p3' }, { projectId: 'p1', branch: 'release' }])
    expect(rowIds(w)).toEqual(['repo-choice-p3', 'repo-choice-p1', 'repo-choice-p2'])
    expect((w.get('[data-testid="repo-choice-branch-p1"]').element as HTMLInputElement).value).toBe('release')
    expect(w.get('[data-testid="repo-choice-branch-p1"]').attributes('placeholder')).toBe('main')
    expect(w.get('[data-testid="repo-choice-branch-p3"]').attributes('placeholder')).toBe(copy.defaultBranch)
    expect(w.find('[data-testid="repo-choice-branch-p2"]').exists()).toBe(false)
    expect(w.find('a[href="/settings/projects"]').exists()).toBe(true)
  })

  it('appends on check, removes on uncheck and trims branches, clearing blanks', async () => {
    const w = setup([])
    await w.get('[data-testid="repo-choice-toggle-p2"]').trigger('click')
    await w.get('[data-testid="repo-choice-toggle-p1"]').trigger('click')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p2' }, { projectId: 'p1' }])
    await w.get('[data-testid="repo-choice-branch-p2"]').setValue('  dev ')
    await w.get('[data-testid="repo-choice-branch-p2"]').trigger('change')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p2', branch: 'dev' }, { projectId: 'p1' }])
    await w.get('[data-testid="repo-choice-branch-p2"]').setValue('  ')
    await w.get('[data-testid="repo-choice-branch-p2"]').trigger('change')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p2' }, { projectId: 'p1' }])
    await w.get('[data-testid="repo-choice-toggle-p2"]').trigger('click')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p1' }])
  })

  it('keeps deleted bindings removable, shows empty and loading states, and hides the manage link on request', async () => {
    const w = setup([{ projectId: 'gone' }], { hideManageLink: true })
    expect(w.get('[data-testid="repo-choice-gone"]').text()).toContain(copy.deleted)
    expect(w.find('[data-testid="repo-choice-branch-gone"]').exists()).toBe(false)
    expect(w.find('a').exists()).toBe(false)
    await w.get('[data-testid="repo-choice-toggle-gone"]').trigger('click')
    expect(w.props('modelValue')).toEqual([])
    await w.setProps({ projects: [] })
    expect(w.text()).toContain(copy.empty)
    await w.setProps({ loading: true })
    expect(w.text()).toContain(copy.loading)
  })

  it('ignores toggles while disabled', async () => {
    const w = setup([{ projectId: 'p1' }], { disabled: true })
    expect(w.get('[data-testid="repo-choice-toggle-p2"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="repo-choice-branch-p1"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="repo-choice-p2"] > div').trigger('click')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p1' }])
  })

  it('toggles from the repository name and uses switches instead of checkboxes', async () => {
    const w = setup([])
    expect(w.find('input[type="checkbox"]').exists()).toBe(false)
    await w.get('[data-testid="repo-choice-p3"] > div').trigger('click')
    expect(w.props('modelValue')).toEqual([{ projectId: 'p3' }])
    expect(w.get('[data-testid="repo-choice-toggle-p3"]').attributes('aria-checked')).toBe('true')
  })
})
