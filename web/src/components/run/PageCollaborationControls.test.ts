// @vitest-environment happy-dom
import { h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import PageCollaborationControls from './PageCollaborationControls.vue'

const cleanups: Array<() => void> = []

function setup(labels: string[] = []) {
  const enabled = ref(false)
  const target = document.createElement('div')
  document.body.appendChild(target)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(PageCollaborationControls, {
    attachTo: target,
    props: { activeLabels: labels },
    global: { plugins: [i18n] },
    slots: {
      default: () => [
        h('button', { role: 'switch', disabled: true, 'aria-checked': 'false' }, 'Unavailable control'),
        h('button', {
          role: 'switch',
          'data-testid': 'test-page-switch',
          'aria-checked': String(enabled.value),
          onClick: () => { enabled.value = !enabled.value },
        }, 'Page candidates'),
      ],
    },
  })
  cleanups.push(() => { wrapper.unmount(); target.remove() })
  const trigger = () => wrapper.get('[data-testid="page-collaboration-toggle"]')
  const panel = () => document.querySelector<HTMLElement>('[data-testid="page-collaboration-controls"]')
  const toggle = () => panel()!.querySelector<HTMLButtonElement>('[data-testid="test-page-switch"]')!
  const open = async () => {
    await trigger().trigger('click')
    await flushPromises()
  }
  return { wrapper, trigger, panel, toggle, open, enabled }
}

afterEach(() => {
  cleanups.splice(0).reverse().forEach((cleanup) => cleanup())
  vi.restoreAllMocks()
})

describe('PageCollaborationControls', () => {
  it('starts collapsed and opens a dialog that focuses the first available switch', async () => {
    const { trigger, panel, toggle, open } = setup()
    expect(trigger().attributes('aria-expanded')).toBe('false')
    expect(trigger().attributes('aria-haspopup')).toBe('dialog')
    expect(panel()).toBeNull()
    await open()
    expect(trigger().attributes('aria-expanded')).toBe('true')
    expect(panel()).not.toBeNull()
    expect(panel()!.getAttribute('role')).toBe('dialog')
    expect(document.activeElement).toBe(toggle())
  })

  it('keeps the dialog open when changing a switch and preserves its state across collapse', async () => {
    const { wrapper, trigger, panel, toggle, open, enabled } = setup()
    await open()
    toggle().click()
    await nextTick()
    expect(enabled.value).toBe(true)
    expect(toggle().getAttribute('aria-checked')).toBe('true')
    expect(panel()).not.toBeNull()
    await wrapper.setProps({ activeLabels: ['页面候选'] })
    await trigger().trigger('click')
    expect(panel()).toBeNull()
    expect(wrapper.get('[data-testid="page-collaboration-summary"]').text()).toBe('页面候选')
    await wrapper.setProps({ activeLabels: ['页面候选', '页面操作'] })
    expect(wrapper.get('[data-testid="page-collaboration-summary"]').text()).toBe('页面候选 · 页面操作')
    await open()
    expect(toggle().getAttribute('aria-checked')).toBe('true')
    expect(enabled.value).toBe(true)
  })

  it('uses native button activation and restores trigger focus on Escape or the close button', async () => {
    const { trigger, panel, open } = setup()
    expect(trigger().element.tagName).toBe('BUTTON')
    expect(trigger().attributes('type')).toBe('button')
    ;(trigger().element as HTMLElement).focus()
    // Happy DOM does not synthesize a native button click from Enter. A
    // keyboard-generated click has detail=0; real key activation is covered by E2E.
    await trigger().trigger('click', { detail: 0 })
    await flushPromises()
    expect(panel()).not.toBeNull()
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(trigger().element)
    await open()
    panel()!.querySelector<HTMLButtonElement>('[data-testid="page-collaboration-close"]')!.click()
    await flushPromises()
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(trigger().element)
  })

  it('dismisses on outside pointer or focus movement while leaving outside controls usable', async () => {
    const { open, panel, enabled } = setup()
    const outside = document.createElement('button')
    outside.textContent = 'Chat input action'
    document.body.appendChild(outside)
    cleanups.push(() => outside.remove())
    await open()
    outside.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await nextTick()
    expect(panel()).toBeNull()
    await open()
    outside.focus()
    await flushPromises()
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(outside)
    expect(enabled.value).toBe(false)
  })

  it('removes the teleported dialog and document listeners when unmounted', async () => {
    const added = vi.spyOn(document, 'addEventListener')
    const removed = vi.spyOn(document, 'removeEventListener')
    const { wrapper, open, panel } = setup()
    await open()
    const listeners = added.mock.calls.filter(([event]) => ['pointerdown', 'focusin', 'keydown'].includes(event))
    expect(listeners.length).toBeGreaterThan(0)
    wrapper.unmount()
    expect(panel()).toBeNull()
    for (const [event, listener] of listeners) {
      expect(removed.mock.calls.some(([removedEvent, removedListener]) => removedEvent === event && removedListener === listener)).toBe(true)
    }
  })
})
