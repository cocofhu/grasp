// @vitest-environment happy-dom
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConversationWorkspace from './ConversationWorkspace.vue'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'

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

  it('keeps a single labeled artifact button unless the sandbox drawer is split', () => {
    const w = mountWorkspace()
    expect(w.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-panel-toggle"]').text()).toContain('产物')
  })
})

describe('ConversationWorkspace split modes', () => {
  function mountSplit(locale: 'zh-CN' | 'en' = 'zh-CN') {
    const state = { open: false, mode: null as 'resources' | 'artifacts' | null, scopeKey: 'run-a' }
    const messages = locale === 'en' ? { en: enPages } : { 'zh-CN': pages }
    wrapper = mount(ConversationWorkspace, {
      attachTo: document.body,
      props: {
        scopeKey: state.scopeKey,
        splitModes: true,
        open: state.open,
        mode: state.mode,
        'onUpdate:open': (value: boolean) => { state.open = value; void wrapper?.setProps({ open: value }) },
        'onUpdate:mode': (value: 'resources' | 'artifacts' | null) => { state.mode = value; void wrapper?.setProps({ mode: value }) },
        'onUpdate:scopeKey': (value: string) => { state.scopeKey = value },
      },
      slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
      global: { plugins: [createI18n({ legacy: false, locale, messages })] },
    })
    return { wrapper, state }
  }

  it('toggles one shared drawer from two icon buttons and restores neither after a new conversation', async () => {
    const { wrapper: w, state } = mountSplit()
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    const artifact = w.get('[data-testid="conversation-panel-toggle"]')
    expect(resource.text().trim()).toBe('')
    expect(artifact.text().trim()).toBe('')
    expect(resource.attributes('title')).toBe('资源')
    expect(artifact.attributes('title')).toBe('产物')
    expect(resource.attributes('aria-label')).toBe('打开资源')
    expect(artifact.attributes('aria-label')).toBe('打开产物')
    expect(resource.classes()).toContain('conversation-mode-toggle')
    expect(artifact.classes()).toContain('conversation-mode-toggle')
    await resource.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('resources')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('资源')
    expect(resource.attributes('aria-pressed')).toBe('true')
    expect(artifact.attributes('aria-pressed')).toBe('false')
    expect(resource.attributes('aria-label')).toBe('关闭资源')
    await artifact.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('artifacts')
    expect(w.get('[data-testid="conversation-artifact-panel"]').text()).toContain('产物')
    expect(artifact.attributes('aria-pressed')).toBe('true')
    expect(resource.attributes('aria-pressed')).toBe('false')
    await artifact.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(artifact.attributes('aria-pressed')).toBe('false')
    expect(resource.attributes('aria-pressed')).toBe('false')
    await resource.trigger('click')
    await w.setProps({ scopeKey: 'run-b' })
    await flushPromises()
    expect(state.open).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(resource.attributes('aria-pressed')).toBe('false')
    expect(artifact.attributes('aria-pressed')).toBe('false')
  })

  it('uses the English resource tip and a single Resources title', async () => {
    const { wrapper: w } = mountSplit('en')
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    expect(resource.attributes('title')).toBe('Resource')
    expect(resource.attributes('aria-label')).toBe('Open resources')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('title')).toBe('Artifacts')
    await resource.trigger('click')
    await flushPromises()
    expect(resource.attributes('aria-label')).toBe('Close resources')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('Resources')
    const titles = w.get('[data-testid="conversation-artifact-panel"]').findAll('header span').map((n) => n.text())
    expect(titles.filter((text) => text === 'Resources')).toHaveLength(1)
  })

  it('covers the conversation with a full-screen drawer on a narrow canvas', async () => {
    const { wrapper: w } = mountSplit()
    resizeObserver([{ contentRect: { width: 600 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    await resource.trigger('click')
    await flushPromises()
    const dialog = w.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    await dialog.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(resource.attributes('aria-pressed')).toBe('false')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('false')
  })
})
