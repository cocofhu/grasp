// @vitest-environment happy-dom
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'
import kimiLogo from '@/assets/provider-logos/kimi.svg'
import kimiDarkLogo from '@/assets/provider-logos/kimi-dark.svg'
import { theme } from '@/lib/shared/theme'
import SoloModelSelect from './SoloModelSelect.vue'

const base: ConfiguredModelChoice = { id: 'kimi-k3.1-pro', name: 'Kimi', hint: 'kimi-k3.1-pro', vendor: 'kimi', backend: false, unavailable: false }
const fast: ConfiguredModelChoice = { id: 'aux-fast', name: 'Fast', hint: 'aux-fast', vendor: 'moonshot', backend: false, unavailable: false }
const mounted: Array<{ unmount: () => void }> = []
function mountSelect(props: { models?: ConfiguredModelChoice[]; modelValue?: string; reasoningEffort?: string; disabled?: boolean } = {}) {
  const wrapper = mount(SoloModelSelect, {
    attachTo: document.body,
    props: { models: [base, fast], modelValue: '', ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { Icon: true } },
  })
  mounted.push(wrapper)
  return wrapper
}
const element = (id: string) => document.querySelector<HTMLElement>(`[data-testid="${id}"]`)
async function open(wrapper: ReturnType<typeof mountSelect>) {
  await wrapper.get('[data-testid="solo-model-select-trigger"]').trigger('click')
  await flushPromises()
}
async function openEffort(wrapper: ReturnType<typeof mountSelect>) {
  await open(wrapper)
  if (!element('solo-effort-panel')) {
    element('solo-effort-open')!.click()
    await flushPromises()
  }
}
async function search(value: string) {
  await new DOMWrapper(element('solo-model-select-search') as HTMLInputElement).setValue(value)
  await flushPromises()
}
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  document.body.innerHTML = ''
})

describe('SoloModelSelect', () => {
  it('shows the saved name and sends upstream ids when selecting auxiliary or primary models', async () => {
    const wrapper = mountSelect()
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).toContain('Kimi')
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).not.toContain('kimi-k3.1-pro')
    const asset = theme.value === 'dark' ? kimiDarkLogo : kimiLogo
    expect(wrapper.get('[data-testid="solo-model-select-trigger"] [data-vendor-mark="kimi"] img').attributes('src')).toBe(asset)
    await open(wrapper)
    expect(element('solo-model-option-kimi-k3.1-pro')?.textContent).toContain('Kimi')
    expect(element('solo-model-select-panel')?.textContent).not.toContain('kimi-k3.1-pro')
    expect(element('solo-model-select-panel')?.textContent).not.toContain('aux-fast')
    expect(element('solo-model-option-kimi-k3.1-pro')?.querySelector('img')?.getAttribute('src')).toBe(asset)
    expect(element('solo-model-option-kimi-k3.1-pro')?.getAttribute('aria-selected')).toBe('true')
    element('solo-model-option-aux-fast')!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['aux-fast'])
    await wrapper.setProps({ modelValue: 'aux-fast' })
    await open(wrapper)
    element('solo-model-option-kimi-k3.1-pro')!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([''])
  })

  it('searches both saved names and upstream ids when the model list needs search', async () => {
    const wrapper = mountSelect({ models: [base, fast, ...Array.from({ length: 4 }, (_, index) => ({ ...fast, id: `other-${index}`, name: `Other ${index}`, hint: `other-${index}` }))] })
    await open(wrapper)
    await search('Kimi')
    expect(element('solo-model-option-kimi-k3.1-pro')).not.toBeNull()
    expect(element('solo-model-option-aux-fast')).toBeNull()
    await search('kimi-k3.1-pro')
    expect(element('solo-model-option-kimi-k3.1-pro')).not.toBeNull()
    await search('aux-fast')
    expect(element('solo-model-option-kimi-k3.1-pro')).toBeNull()
    expect(element('solo-model-option-aux-fast')).not.toBeNull()
  })

  it('offers advertised effort levels even when the coding tool chooses the only model', async () => {
    const wrapper = mountSelect({ models: [{ ...base, id: '', hint: '', name: 'Codex', vendor: 'codex', backend: true, efforts: ['low', 'medium', 'high'] }] })
    await open(wrapper)
    expect(element('solo-effort-panel')).not.toBeNull()
    const slider = element('solo-effort-slider') as HTMLInputElement
    expect(slider.max).toBe('3')
    expect(slider.value).toBe('0')
    expect(slider.getAttribute('aria-valuetext')).toBe(element('solo-effort-current')?.textContent)
    expect(document.querySelectorAll('button[data-testid^="solo-effort-option-"]')).toHaveLength(0)
    await new DOMWrapper(slider).setValue('3')
    expect(wrapper.emitted('update:reasoningEffort')).toEqual([['high']])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ reasoningEffort: 'high' })
    expect(wrapper.get('[data-testid="solo-model-effort-label"]').text()).toBe(element('solo-effort-current')?.textContent)
  })

  it('resets the effort to automatic without changing the selected model', async () => {
    const wrapper = mountSelect({ models: [{ ...base, efforts: ['low', 'high'] }], reasoningEffort: 'high' })
    await openEffort(wrapper)
    element('solo-effort-reset')!.click()
    await flushPromises()
    expect(wrapper.emitted('update:reasoningEffort')).toEqual([['']])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ reasoningEffort: '' })
    expect((element('solo-effort-reset') as HTMLButtonElement).disabled).toBe(true)
  })

  it('preserves native slider arrow handling and closes with Escape', async () => {
    const wrapper = mountSelect({ models: [{ ...base, efforts: ['low', 'high'] }] })
    await openEffort(wrapper)
    const slider = element('solo-effort-slider') as HTMLInputElement
    expect(document.activeElement).toBe(slider)
    const arrow = new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })
    slider.dispatchEvent(arrow)
    expect(arrow.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(slider)
    slider.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(element('solo-effort-panel')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="solo-model-select-trigger"]').element)
    await open(wrapper)
    expect(element('solo-effort-panel')).not.toBeNull()
    element('solo-effort-back')!.click()
    await flushPromises()
    expect(element('solo-effort-panel')).toBeNull()
    expect(element('solo-model-option-kimi-k3.1-pro')).not.toBeNull()
  })

  it('returns from the effort view to model selection', async () => {
    const wrapper = mountSelect({ models: [{ ...base, efforts: ['high'] }, fast] })
    await openEffort(wrapper)
    element('solo-effort-back')!.click()
    await flushPromises()
    expect(element('solo-effort-panel')).toBeNull()
    expect(element('solo-model-option-aux-fast')).not.toBeNull()
    expect(document.activeElement).toBe(element('solo-effort-open'))
  })

  it.each([1, 2])('does not invent effort controls for %s models that advertise none', async (count) => {
    const wrapper = mountSelect({ models: [base, fast].slice(0, count), reasoningEffort: 'high' })
    expect(wrapper.find('[data-testid="solo-model-effort-label"]').exists()).toBe(false)
    await open(wrapper)
    expect(element('solo-effort-open')).toBeNull()
    expect(element('solo-effort-slider')).toBeNull()
  })

  it('uses unknown advertised effort values literally and blocks unavailable models', async () => {
    const wrapper = mountSelect({ models: [{ ...base, efforts: ['economy', 'full'] }], reasoningEffort: 'economy' })
    await openEffort(wrapper)
    const slider = element('solo-effort-slider') as HTMLInputElement
    expect(element('solo-effort-current')?.textContent).toBe('economy')
    expect(slider.max).toBe('2')
    expect(slider.value).toBe('1')
    expect(slider.getAttribute('aria-valuetext')).toBe('economy')
    await new DOMWrapper(slider).setValue('2')
    expect(wrapper.emitted('update:reasoningEffort')?.[0]).toEqual(['full'])
    await wrapper.setProps({ models: [{ ...base, efforts: ['economy', 'full'], unavailable: true }] })
    await flushPromises()
    expect((element('solo-effort-slider') as HTMLInputElement).disabled).toBe(true)
  })
})
