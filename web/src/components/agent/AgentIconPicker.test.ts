// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import pagesZh from '@/locales/zh-CN/pages.json'
import pagesEn from '@/locales/en/pages.json'
import AgentIconPicker from './AgentIconPicker.vue'

function mountPicker(props: Record<string, unknown> = {}, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: { 'zh-CN': pagesZh, en: pagesEn },
  })
  return mount(AgentIconPicker, {
    attachTo: document.body,
    props: { name: '', size: 'lg', purpose: 'choose', ...props },
    global: { plugins: [i18n] },
  })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('AgentIconPicker', () => {
  it('filters by Chinese or English, defaults to black, and has no emoji page', async () => {
    const wrapper = mountPicker()
    expect(document.body.textContent).not.toContain('表情')
    expect(document.querySelector('[data-test="agent-identity-mark"] svg')).toBeTruthy()
    await wrapper.get('[data-test="agent-identity-mark"]').trigger('click')
    await flushPromises()
    const search = document.querySelector<HTMLInputElement>('[data-test="agent-identity-search"]')
    expect(search?.placeholder).toBe('搜索图标')
    expect(document.activeElement).toBe(search)
    expect(document.querySelector('[data-test="agent-identity-color-black"]')?.getAttribute('aria-pressed')).toBe('true')
    search!.value = 'flask'
    search!.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('[data-test="agent-identity-icon-flask"]')).toBeTruthy()
    expect(document.querySelector('[data-test="agent-identity-icon-plane"]')).toBeNull()
    search!.value = '没有'
    search!.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('[data-test="agent-identity-empty"]')?.textContent).toContain('没有匹配的图标')
    wrapper.unmount()
  })

  it('uses English copy and opens an existing icon on its saved color', async () => {
    const wrapper = mountPicker({ modelValue: { iconId: 'flask', color: 'purple' }, purpose: 'change', size: 'sm', name: '实验室' }, 'en')
    expect(document.querySelector('[data-test="agent-identity-mark"]')?.getAttribute('aria-label')).toBe('Change or clear the icon')
    expect(document.querySelector('[data-test="agent-identity-mark"]')?.textContent).not.toContain('实')
    await wrapper.get('[data-test="agent-identity-mark"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="agent-identity-search"]')?.getAttribute('placeholder')).toBe('Search icons')
    expect(document.querySelector('[data-test="agent-identity-color-purple"]')?.getAttribute('aria-pressed')).toBe('true')
    wrapper.unmount()
  })

  it('closes on Escape without changing the selection, and commits a click or clear', async () => {
    const wrapper = mountPicker({ modelValue: { iconId: 'book', color: 'blue' }, name: '阅读' })
    await wrapper.get('[data-test="agent-identity-mark"]').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-color-red"]')!.click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('[data-test="agent-identity-picker"]')).toBeNull()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.get('[data-test="agent-identity-mark"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="agent-identity-color-blue"]')?.getAttribute('aria-pressed')).toBe('true')
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-color-green"]')!.click()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-icon-plant"]')!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ iconId: 'plant', color: 'green' }])
    expect(document.querySelector('[data-test="agent-identity-picker"]')).toBeNull()

    await wrapper.get('[data-test="agent-identity-mark"]').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-clear"]')!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    wrapper.unmount()
  })
})
