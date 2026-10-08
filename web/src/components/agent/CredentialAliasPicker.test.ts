// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import CredentialAliasPicker from './CredentialAliasPicker.vue'

const items = [
  {
    id: 'builtin', type: 'ai', name: 'Cursor', provider: 'cursor',
    envKey: 'GRASP_CURSOR_API_KEY', configured: true,
  },
  {
    id: 'work', type: 'ai', name: '工作号', provider: 'cursor',
    envKey: 'GRASP_CURSOR_API_KEY', configured: true,
  },
  {
    id: 'gh', type: 'git', name: '工作号', provider: 'github',
    envKey: 'GITHUB_TOKEN', configured: true,
  },
]

function mountPicker(selectedId = '') {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
  return mount(CredentialAliasPicker, {
    props: { projectId: 'p1', kind: 'cursor', selectedId, items },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('CredentialAliasPicker', () => {
  it('lists aliases for the matching kind and remembers only the chosen one', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const titles = wrapper.findAll('[data-testid="credential-alias-title"]').map((node) => node.text())
    expect(titles).toEqual(['Cursor', '工作号'])
    expect(wrapper.findAll('[data-testid="credential-alias-subtitle"]').every((node) => node.text() === 'Cursor')).toBe(true)
    expect(wrapper.text()).not.toContain('GitHub')

    await wrapper.get('[data-testid="credential-alias-option-work"]').trigger('click')
    expect(wrapper.emitted('update:selectedId')?.[0]).toEqual(['work'])

    await wrapper.setProps({ selectedId: 'work' })
    await wrapper.get('[data-testid="credential-alias-clear"]').trigger('click')
    expect(wrapper.emitted('update:selectedId')?.at(-1)).toEqual([''])
    wrapper.unmount()
  })
})
