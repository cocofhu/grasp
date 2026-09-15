// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentTemplateSelect from './AgentTemplateSelect.vue'

const here = dirname(fileURLToPath(import.meta.url))

const OPTIONS = [
  { id: 'blank', name: '空白', subtitle: '通用身份 Rule' },
  { id: 'test', name: '测试工程师', subtitle: 'TestAgent · set_test_result' },
  { id: 'preflight', name: '环境确认工程师', subtitle: 'PreflightAgent · set_preflight' },
  { id: 'implement', name: '实现工程师', subtitle: 'ImplementAgent · set_implementation_result' },
]

function mountSelect(props: { modelValue?: string; disabled?: boolean } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentTemplateSelect, {
    props: {
      options: OPTIONS,
      modelValue: props.modelValue ?? 'blank',
      disabled: props.disabled ?? false,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Teleport: false },
    },
  })
}

describe('AgentTemplateSelect (plan g1)', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  // plan g1.1 — trigger, search, title+subtitle, selected highlight
  it('opens with search, title+subtitle rows, and selected highlight', async () => {
    const wrapper = mountSelect({ modelValue: 'test' })
    await wrapper.get('[data-testid="agent-template-select-trigger"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-template-select-search"]')).toBeTruthy()
    const testOpt = document.querySelector(
      '[data-testid="agent-template-select-option-test"]',
    ) as HTMLElement
    expect(testOpt).toBeTruthy()
    expect(testOpt.classList.contains('agent-template-select__opt--current')).toBe(true)
    expect(testOpt.textContent).toContain('测试工程师')
    expect(testOpt.textContent).toContain('TestAgent')
    expect(document.querySelector('[data-testid="agent-template-select-list"]')).toBeTruthy()
    wrapper.unmount()
  })

  // plan g1.2 — Teleport to body
  it('teleports panel to body so wizard overflow cannot clip it', async () => {
    const clip = document.createElement('div')
    clip.className = 'scroll-area'
    clip.style.overflowY = 'auto'
    clip.style.height = '80px'
    document.body.appendChild(clip)

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(AgentTemplateSelect, {
      props: { options: OPTIONS, modelValue: 'blank' },
      attachTo: clip,
      global: { plugins: [i18n], stubs: { Teleport: false } },
    })
    await wrapper.get('[data-testid="agent-template-select-trigger"]').trigger('click')
    await flushPromises()
    const panel = document.querySelector(
      '[data-testid="agent-template-select-panel"]',
    ) as HTMLElement
    expect(panel).toBeTruthy()
    expect(panel.parentElement).toBe(document.body)
    expect(panel.getAttribute('data-placement')).toBe('below')
    expect(panel.style.position).toBe('fixed')
    wrapper.unmount()
  })

  // plan g1.3 — list-only scroll max-height 220px in component CSS
  it('declares list max-height 220px for list-only scroll', () => {
    const src = readFileSync(join(here, 'AgentTemplateSelect.vue'), 'utf8')
    expect(src).toMatch(/max-height:\s*220px/)
    expect(src).toMatch(/Teleport\s+to="body"/)
    expect(src).toMatch(/overscroll-behavior:\s*contain/)
  })

  // plan g1.4 — selecting does not emit name; only update:modelValue
  it('mousedown on option emits template id only', async () => {
    const wrapper = mountSelect({ modelValue: 'blank' })
    await wrapper.get('[data-testid="agent-template-select-trigger"]').trigger('click')
    await flushPromises()
    const opt = document.querySelector(
      '[data-testid="agent-template-select-option-test"]',
    ) as HTMLElement
    opt.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await flushPromises()
    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted?.[0]).toEqual(['test'])
    expect(wrapper.emitted()).not.toHaveProperty('update:name')
    wrapper.unmount()
  })

  it('filters by search 测试 and keeps keyboard list-only scroll helpers', async () => {
    const wrapper = mountSelect({})
    await wrapper.get('[data-testid="agent-template-select-trigger"]').trigger('click')
    await flushPromises()
    const search = document.querySelector(
      '[data-testid="agent-template-select-search"]',
    ) as HTMLInputElement
    search.value = '测试'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-template-select-option-test"]')).toBeTruthy()
    expect(
      document.querySelector('[data-testid="agent-template-select-option-implement"]'),
    ).toBeFalsy()
    search.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }),
    )
    await flushPromises()
    wrapper.unmount()
  })
})
