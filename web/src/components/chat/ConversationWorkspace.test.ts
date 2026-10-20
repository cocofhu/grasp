// @vitest-environment happy-dom
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConversationWorkspace from './ConversationWorkspace.vue'
import pages from '@/locales/zh-CN/pages.json'

let wrapper: VueWrapper | undefined
let resizeObserver: ResizeObserverCallback
const i18n = () => createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
function mountWorkspace(open = false) {
  wrapper = mount(ConversationWorkspace, {
    attachTo: document.body,
    props: { scopeKey: 'run-a/node-a', open, 'onUpdate:open': value => { void wrapper?.setProps({ open: value }) } },
    slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
    global: { plugins: [i18n()] },
  })
  return wrapper
}
beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { constructor(callback: ResizeObserverCallback) { resizeObserver = callback } observe() {} disconnect() {} })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })

describe('ConversationWorkspace', () => {
  it('opens artifacts to the right without remounting the conversation or preview', async () => {
    const w = mountWorkspace()
    const chat = w.get('textarea').element
    const preview = w.get('input').element
    await w.get('textarea').setValue('保留我的草稿')
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(w.get('textarea').element).toBe(chat)
    expect(w.get('input').element).toBe(preview)
    expect((chat as HTMLTextAreaElement).value).toBe('保留我的草稿')
  })

  it('supports keyboard panel resizing and persists the preference', async () => {
    const w = mountWorkspace(true)
    await flushPromises()
    const sash = w.get('[role="separator"]')
    const before = Number(sash.attributes('aria-valuenow'))
    await sash.trigger('keydown', { key: 'ArrowLeft' })
    expect(Number(sash.attributes('aria-valuenow'))).toBeGreaterThan(before)
    expect(Number(localStorage.getItem('grasp.conversation.panelWidth'))).toBe(Number(sash.attributes('aria-valuenow')))
    await sash.trigger('keydown', { key: 'Home' })
    expect(sash.attributes('aria-valuenow')).toBe('280')
  })

  it('uses a dismissible modal preview on a narrow canvas and returns focus', async () => {
    const w = mountWorkspace()
    resizeObserver([{ contentRect: { width: 600 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    const trigger = w.get('[data-testid="conversation-panel-toggle"]')
    ;(trigger.element as HTMLButtonElement).focus()
    await trigger.trigger('click')
    await flushPromises()
    const dialog = w.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    await dialog.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(w.get('[data-testid="conversation-main"]').attributes('aria-hidden')).toBeUndefined()
  })

  it('closes the old preview when switching conversation scope', async () => {
    const w = mountWorkspace(true)
    await flushPromises()
    await w.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
  })
})
