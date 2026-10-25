// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import SoloProjectSelect from './SoloProjectSelect.vue'
import type { Project } from '@/lib/api/apiTypes'

const projects = [
  { id: 'grasp', name: 'grasp', repoUrl: 'https://github.com/acme/grasp.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
  { id: 'web', name: 'webapp', repoUrl: 'https://github.com/acme/webapp.git', defaultBranch: 'develop', createdAt: '', updatedAt: '' },
] as Project[]

function mountSelect(modelValue: string[] = []) {
  return mount(SoloProjectSelect, {
    attachTo: document.body,
    props: { projects, modelValue, branches: {} },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages } } })],
      stubs: { Teleport: true, RouterLink: { template: '<a><slot /></a>' } },
    },
  })
}

describe('SoloProjectSelect', () => {
  it('toggles without closing and labels the first name plus the rest', async () => {
    const wrapper = mountSelect()
    await wrapper.get('[data-testid="solo-project-select-trigger"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="solo-project-select-trigger"]').text()).toContain('不绑定仓库')

    await wrapper.get('[data-testid="solo-project-option-grasp"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['grasp']])
    expect(document.body.querySelector('[data-testid="solo-project-select-panel"]')).toBeTruthy()

    await wrapper.setProps({ modelValue: ['grasp', 'web'] })
    expect(wrapper.get('[data-testid="solo-project-select-trigger"]').text()).toContain('grasp +1')
    expect(wrapper.get('[data-testid="solo-branch-chip"]').exists()).toBe(true)

    await wrapper.get('[data-testid="solo-project-option-none"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    wrapper.unmount()
  })
})
