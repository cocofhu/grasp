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
  { id: 'docs', name: 'docs', repoUrl: 'https://github.com/acme/docs.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
] as Project[]

function triggerText(wrapper: ReturnType<typeof mountSelect>) {
  return wrapper.get('[data-testid="solo-project-select-trigger"]').text()
}

async function untilTrigger(wrapper: ReturnType<typeof mountSelect>, expected: string) {
  const start = Date.now()
  while (Date.now() - start < 1000) {
    if (triggerText(wrapper) === expected) return
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
  expect(triggerText(wrapper)).toBe(expected)
}

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
    expect(wrapper.find('[data-testid="solo-project-option-none"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="solo-branch-chip"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('空白工作区')
    const panel = document.body.querySelector('[data-testid="solo-project-select-panel"]') as HTMLElement
    expect(panel).toBeTruthy()
    expect(panel.textContent).not.toContain('不绑定仓库')
    expect(panel.textContent).not.toContain('空白工作区')
    expect(panel.textContent).toContain('添加仓库')
    expect(panel.textContent).toContain('管理仓库')
    expect(panel.style.width).toBe(`${Math.min(300, window.innerWidth - 16)}px`)

    await wrapper.get('[data-testid="solo-project-option-grasp"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['grasp']])
    expect(document.body.querySelector('[data-testid="solo-project-select-panel"]')).toBeTruthy()

    await wrapper.setProps({ modelValue: ['grasp', 'web'] })
    await untilTrigger(wrapper, 'grasp +1')
    expect(triggerText(wrapper)).toBe('grasp +1')
    expect(document.body.querySelector('[data-testid="solo-project-select-panel"]')).toBeTruthy()
    expect(wrapper.find('[data-testid="solo-branch-chip"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="solo-project-option-none"]').exists()).toBe(false)

    await wrapper.setProps({ modelValue: ['web'] })
    await wrapper.get('[data-testid="solo-project-option-web"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    await wrapper.setProps({ modelValue: [] })
    expect(wrapper.get('[data-testid="solo-project-select-trigger"]').text()).toContain('不绑定仓库')
    expect(wrapper.get('[data-testid="solo-project-select-trigger"]').text()).not.toContain('+')
    wrapper.unmount()
  })

  it('keeps only the final digit after the count rolls up and back', async () => {
    const wrapper = mountSelect(['grasp', 'web'])
    await flushPromises()
    expect(triggerText(wrapper)).toBe('grasp +1')

    await wrapper.setProps({ modelValue: ['grasp', 'web', 'docs'] })
    await flushPromises()
    await untilTrigger(wrapper, 'grasp +2')
    const up = wrapper.get('.solo-repo-roll-strip').element as HTMLElement
    expect(up.childElementCount).toBe(1)
    expect(up.textContent).toBe('2')
    expect(up.style.transform).toBe('none')
    expect(triggerText(wrapper)).toBe('grasp +2')

    await wrapper.setProps({ modelValue: ['grasp', 'web'] })
    await flushPromises()
    await untilTrigger(wrapper, 'grasp +1')
    const down = wrapper.get('.solo-repo-roll-strip').element as HTMLElement
    expect(down.childElementCount).toBe(1)
    expect(down.textContent).toBe('1')
    expect(down.style.transform).toBe('none')
    expect(triggerText(wrapper)).toBe('grasp +1')
    expect(triggerText(wrapper)).not.toContain('+12')
    wrapper.unmount()
  })
})
