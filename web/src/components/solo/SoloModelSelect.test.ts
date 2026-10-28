// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
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

const here = dirname(fileURLToPath(import.meta.url))
const source = (name: string) => readFileSync(join(here, name), 'utf8')

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
    expect(element('solo-model-option-kimi-k3.1-pro')?.querySelector('.solo-model-option-note')?.textContent).toBe('kimi-k3.1-pro')
    expect(element('solo-model-option-aux-fast')?.querySelector('.solo-model-option-note')?.textContent).toBe('aux-fast')
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).not.toContain('aux-fast')
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
    expect(element('solo-model-select-search')).toBeNull()
    const slider = element('solo-effort-slider') as HTMLInputElement
    expect(slider.max).toBe('2')
    expect(slider.value).toBe('2')
    expect(document.querySelectorAll('.solo-effort-tick')).toHaveLength(3)
    expect(element('solo-effort-current')?.textContent).toBe('高')
    expect(wrapper.get('[data-testid="solo-model-effort-label"]').text()).toBe('高')
    expect(element('solo-effort-panel')?.textContent).not.toContain('默认')
    expect(slider.getAttribute('aria-valuetext')).toBe(element('solo-effort-current')?.textContent)
    expect(document.querySelectorAll('button[data-testid^="solo-effort-option-"]')).toHaveLength(0)
    expect((element('solo-effort-reset') as HTMLButtonElement).disabled).toBe(true)
    await new DOMWrapper(slider).setValue('0')
    expect(wrapper.emitted('update:reasoningEffort')).toEqual([['low']])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ reasoningEffort: 'low' })
    expect(wrapper.get('[data-testid="solo-model-effort-label"]').text()).toBe(element('solo-effort-current')?.textContent)
  })

  it('keeps a saved lower effort and resets it to the highest level', async () => {
    const argon = { ...base, id: 'gemini-4-argon', name: 'Gemini 4 Argon', efforts: ['low', 'medium', 'high'] }
    const wrapper = mountSelect({ models: [argon], reasoningEffort: 'low' })
    await openEffort(wrapper)
    const slider = element('solo-effort-slider') as HTMLInputElement
    expect(slider.max).toBe('2')
    expect(slider.value).toBe('0')
    expect(document.querySelectorAll('.solo-effort-tick')).toHaveLength(3)
    expect(element('solo-effort-current')?.textContent).toBe('低')
    expect(wrapper.get('[data-testid="solo-model-effort-label"]').text()).toBe('低')
    expect(element('solo-effort-panel')?.textContent).not.toContain('默认')
    expect((element('solo-effort-reset') as HTMLButtonElement).disabled).toBe(false)
    element('solo-effort-reset')!.click()
    await flushPromises()
    expect(wrapper.emitted('update:reasoningEffort')).toEqual([['high']])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ reasoningEffort: 'high' })
    expect(slider.value).toBe('2')
    expect(element('solo-effort-current')?.textContent).toBe('高')
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
    expect(element('solo-model-select-search')).not.toBeNull()
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
    expect(slider.max).toBe('1')
    expect(slider.value).toBe('0')
    expect(slider.getAttribute('aria-valuetext')).toBe('economy')
    await new DOMWrapper(slider).setValue('1')
    expect(wrapper.emitted('update:reasoningEffort')?.[0]).toEqual(['full'])
    await wrapper.setProps({ models: [{ ...base, efforts: ['economy', 'full'], unavailable: true }] })
    await flushPromises()
    expect((element('solo-effort-slider') as HTMLInputElement).disabled).toBe(true)
  })

  it.each([1, 5])('shows a search box for a model list of %s', async (count) => {
    const models = Array.from({ length: count }, (_, index) => ({ ...fast, id: `model-${index}`, name: `Model ${index}`, hint: `model-${index}` }))
    const wrapper = mountSelect({ models, modelValue: models[0]!.id })
    await open(wrapper)
    const searchBox = element('solo-model-select-search') as HTMLInputElement
    expect(searchBox).not.toBeNull()
    expect(searchBox.placeholder).toBe('搜索模型')
    expect(searchBox.getAttribute('aria-label')).toBe('搜索模型')
    expect(searchBox.value).toBe('')
    expect(document.activeElement).toBe(searchBox)
    const list = document.querySelector('.solo-picker__list')
    const heading = document.querySelector('.solo-picker__heading')
    expect(list?.contains(searchBox)).toBe(false)
    expect(heading?.textContent).toContain('选择模型')
    expect(list?.contains(heading!)).toBe(false)
  })

  it('keeps the trigger disabled when there are no models', () => {
    const wrapper = mountSelect({ models: [], modelValue: '' })
    expect((wrapper.get('[data-testid="solo-model-select-trigger"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(element('solo-model-select-search')).toBeNull()
  })

  it('filters a short list by name, id, group, tool and source, and restores after an empty query', async () => {
    const gemini = { ...base, id: 'gemini-4-argon', name: 'Gemini 4 Argon Contributor', hint: 'gemini-4-argon', configurationId: 'g-opencode', configurationName: 'OpenCode', tool: 'opencode', source: '' }
    const kimi = { ...base, id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', configurationId: 'g-token', configurationName: 'TokenHub', tool: 'deepseek_harness', source: '腾讯混元 / TokenHub · TokenHub' }
    const grok = { ...fast, id: 'grok-4.7-high', name: 'Grok 4.7 High', hint: 'grok-4.7-high', vendor: 'grok', configurationId: 'g-cursor', configurationName: 'CursorX', tool: 'cursor', source: '' }
    const wrapper = mountSelect({ models: [gemini, kimi, grok], modelValue: '' })
    await open(wrapper)
    expect(document.activeElement).toBe(element('solo-model-select-search'))
    element('solo-model-select-search')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(element('solo-model-option-gemini-4-argon'))
    await search('GROK')
    expect(element('solo-model-option-grok-4.7-high')).not.toBeNull()
    expect(element('solo-model-option-kimi-k3.1-pro')).toBeNull()
    expect(document.querySelector('.solo-picker__list')?.textContent).not.toContain('TokenHub')
    await search('gemini-4-argon')
    expect(element('solo-model-option-gemini-4-argon')).not.toBeNull()
    expect(element('solo-model-option-grok-4.7-high')).toBeNull()
    await search('opencode')
    expect(element('solo-model-option-gemini-4-argon')).not.toBeNull()
    await search('腾讯混元')
    expect(element('solo-model-option-kimi-k3.1-pro')).not.toBeNull()
    expect(element('solo-model-option-gemini-4-argon')).toBeNull()
    await search('missing-model')
    expect(document.querySelector('.solo-picker__list')?.textContent).toContain('没有匹配的模型')
    expect(document.querySelectorAll('.solo-picker__list .solo-model-menu-title')).toHaveLength(0)
    await search('   ')
    expect(element('solo-model-option-gemini-4-argon')).not.toBeNull()
    expect(element('solo-model-option-kimi-k3.1-pro')).not.toBeNull()
    expect(element('solo-model-option-grok-4.7-high')).not.toBeNull()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('selects the first available filtered model with Enter and ignores unavailable matches', async () => {
    const blocked = { ...base, id: 'blocked-model', name: 'Blocked', hint: 'blocked-model', unavailable: true }
    const wrapper = mountSelect({ models: [blocked, fast] })
    await open(wrapper)
    expect((element('solo-model-option-blocked-model') as HTMLButtonElement).disabled).toBe(true)
    await new DOMWrapper(element('solo-model-select-search') as HTMLInputElement).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['aux-fast'])
    await wrapper.setProps({ modelValue: 'aux-fast' })
    await open(wrapper)
    element('solo-model-select-search')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(element('solo-model-select-panel')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="solo-model-select-trigger"]').element)
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })

  it('shows only the user-defined group name and does not merge groups that share it', async () => {
    const opencode = { ...base, id: 'gemini-4-argon', name: 'Gemini 4 Argon Contributor', hint: 'gemini-4-argon', configurationId: 'cfg-opencode', configurationName: 'OpenCode', tool: 'opencode', source: '' }
    const token = { ...base, id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', configurationId: 'cfg-token', configurationName: 'TokenHub', tool: 'deepseek_harness', source: '腾讯混元 / TokenHub · TokenHub' }
    const tokenAgain = { ...fast, id: 'deepseek-v4', name: 'DeepSeek-V4.1-Flash-1017', hint: 'deepseek-v4', configurationId: 'cfg-token-2', configurationName: 'TokenHub', tool: 'cursor', source: '' }
    const cursorx = { ...fast, id: 'grok-4.7-high', name: 'Grok 4.7 High', hint: 'grok-4.7-high', configurationId: 'cfg-cursor', configurationName: 'CursorX', tool: 'cursor', source: '' }
    const wrapper = mountSelect({ models: [opencode, token, tokenAgain, cursorx], modelValue: 'kimi-k3.1-pro' })
    await open(wrapper)
    const titles = [...document.querySelectorAll('.solo-picker__list .solo-model-menu-title')].map((node) => node.textContent)
    expect(titles).toEqual(['OpenCode', 'TokenHub', 'TokenHub', 'CursorX'])
    expect(document.querySelector('.solo-picker__list')?.textContent).not.toContain('DeepSeek Harness')
    expect(document.querySelector('.solo-picker__list')?.textContent).not.toContain('OpenCode · OpenCode')
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).toContain('TokenHub')
  })

  it('gives every model row the same height and a source or hint caption', async () => {
    const gemini = { ...base, id: 'gemini-4-argon', name: 'Gemini 4 Argon Contributor', hint: 'gemini-4-argon', configurationId: 'cfg-opencode', configurationName: 'OpenCode', tool: 'opencode', source: '' }
    const kimi = { ...base, id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', configurationId: 'cfg-token', configurationName: 'TokenHub', tool: 'deepseek_harness', source: '腾讯混元 / TokenHub · TokenHub' }
    const wrapper = mountSelect({ models: [gemini, kimi] })
    await open(wrapper)
    const geminiRow = element('solo-model-option-gemini-4-argon') as HTMLElement
    const kimiRow = element('solo-model-option-kimi-k3.1-pro') as HTMLElement
    expect(geminiRow.querySelector('.solo-model-option-note')?.textContent).toBe('gemini-4-argon')
    expect(kimiRow.querySelector('.solo-model-option-note')?.textContent).toBe('腾讯混元 / TokenHub · TokenHub')
    expect(geminiRow.classList.contains('solo-model-option')).toBe(true)
    expect(kimiRow.classList.contains('solo-model-option')).toBe(true)
    const modelCss = source('SoloModelSelect.vue')
    const shell = source('SoloPickerShell.vue')
    expect(modelCss).toMatch(/\.solo-model-option-name,\s*\.solo-model-option-note\s*\{[^}]*overflow:\s*hidden[^}]*text-overflow:\s*ellipsis[^}]*white-space:\s*nowrap/s)
    expect(modelCss).toMatch(/\.solo-model-option-name\s*\{[^}]*font-size:\s*13px[^}]*line-height:\s*18px/s)
    expect(modelCss).toMatch(/\.solo-model-option-note\s*\{[^}]*font-size:\s*10px[^}]*line-height:\s*14px/s)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list :deep\(\.solo-model-option\)\s*\{[^}]*height:\s*48px[^}]*min-height:\s*48px[^}]*max-height:\s*48px/s)
  })

  it('caps the model list at 240px and reveals a 4px scrollbar only while hovered', async () => {
    const models = Array.from({ length: 8 }, (_, index) => ({ ...fast, id: `model-${index}`, name: `Model ${index}`, hint: `model-${index}` }))
    const wrapper = mountSelect({ models })
    await open(wrapper)
    const panel = element('solo-model-select-panel')!
    expect(panel.classList.contains('solo-picker__panel--compact')).toBe(true)
    const list = panel.querySelector('.solo-picker__list')!
    expect(list.contains(element('solo-model-select-search')!)).toBe(false)
    expect(list.contains(panel.querySelector('.solo-picker__heading')!)).toBe(false)
    const shell = source('SoloPickerShell.vue')
    const listRule = shell.match(/\.solo-picker__panel--compact \.solo-picker__list \{[^}]*\}/)?.[0] || ''
    expect(listRule).toMatch(/max-height:\s*240px/)
    expect(listRule).toMatch(/overflow-y:\s*auto/)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list::-webkit-scrollbar\s*\{[^}]*width:\s*4px/s)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list::-webkit-scrollbar-button\s*\{[^}]*display:\s*none/s)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list::-webkit-scrollbar-track\s*\{[^}]*background:\s*transparent/s)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list::-webkit-scrollbar-thumb\s*\{[^}]*background:\s*transparent/s)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list:hover::-webkit-scrollbar-thumb\s*\{[^}]*background:\s*rgb\(var\(--c-line\)\)/s)
    const compactListCss = shell.slice(shell.indexOf('.solo-picker__panel--compact .solo-picker__list {'))
    expect(compactListCss).not.toMatch(/scrollbar-width\s*:/)
    expect(compactListCss).not.toMatch(/scrollbar-color\s*:/)
    expect(shell).toMatch(/\.solo-picker__panel--compact \.solo-picker__list :deep\(\.solo-model-option\)\s*\{[^}]*height:\s*48px/s)
    const project = source('SoloProjectSelect.vue')
    expect(project).toContain('projects.length > 5')
  })
})
