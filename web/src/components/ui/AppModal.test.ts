// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import AppButton from './AppButton.vue'
import AppModal from './AppModal.vue'

function mountModal(props: Record<string, unknown> = {}, body = '<p data-testid="body">内容</p>') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(AppModal, {
    props: { open: true, title: '标题', ...props },
    slots: { default: body },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: { template: '<span data-testid="icon-close" />' },
        Transition: false,
      },
    },
    attachTo: document.body,
  })
}

describe('AppModal', () => {
  it('shows title and body when open', () => {
    const wrapper = mountModal({ open: true })
    expect(document.body.textContent).toContain('标题')
    expect(document.body.querySelector('[data-testid="body"]')).toBeTruthy()
    wrapper.unmount()
  })

  it('keeps modal body p-5 (ArtifactPreview zoom ancestor, g3.3)', () => {
    const wrapper = mountModal({ open: true })
    const body = document.body.querySelector('.modal-scroll-area') as HTMLElement | null
    expect(body).toBeTruthy()
    expect(body!.classList.contains('p-5')).toBe(true)
    wrapper.unmount()
  })

  it('emits close on backdrop click by default', async () => {
    const wrapper = mountModal({ open: true })
    const overlay = document.body.querySelector('.bg-black\\/60') as HTMLElement
    expect(overlay).toBeTruthy()
    overlay.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not emit close on backdrop click when closeOnBackdrop is false', async () => {
    const wrapper = mountModal({ open: true, closeOnBackdrop: false })
    const overlay = document.body.querySelector('.bg-black\\/60') as HTMLElement
    expect(overlay).toBeTruthy()
    overlay.click()
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('still emits close from the header close button when closeOnBackdrop is false', async () => {
    const wrapper = mountModal({ open: true, closeOnBackdrop: false })
    const closeBtn = document.body.querySelector(
      '.relative.flex.max-h-\\[88vh\\] button',
    ) as HTMLButtonElement | null
    expect(closeBtn).toBeTruthy()
    closeBtn!.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not emit close on Escape by default', async () => {
    const wrapper = mountModal({ open: true })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('emits close on Escape when closeOnEsc is true', async () => {
    const wrapper = mountModal({ open: true, closeOnEsc: true })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('exposes dialog semantics with accessible name from title', () => {
    const wrapper = mountModal({ open: true, title: '图片预览 · 看板.png' })
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).toBeTruthy()
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    const labelledBy = dialog?.getAttribute('aria-labelledby')
    expect(labelledBy).toBeTruthy()
    const titleEl = document.getElementById(labelledBy!)
    expect(titleEl?.textContent).toContain('图片预览 · 看板.png')
    wrapper.unmount()
  })

  it('labels the close button and leaves focus untouched unless opted in', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const wrapper = mountModal({ closeLabel: '关闭窗口' })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    const closeButton = document.body.querySelector('button[aria-label="关闭窗口"]') as HTMLButtonElement
    expect(closeButton).toBeTruthy()
    closeButton.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
    trigger.remove()
  })

  it('focuses an enabled autofocus field, loops Tab and Shift+Tab, and restores the opener on close', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const wrapper = mountModal({ open: false, trapFocus: true }, '<input autofocus data-testid="first-field" /><button disabled>Disabled</button><button hidden>Hidden</button><button data-testid="last-action">Save</button>')
    await wrapper.setProps({ open: true })
    await nextTick()
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    const closeButton = dialog.querySelector('button[aria-label="Close"]') as HTMLButtonElement
    const firstField = dialog.querySelector('[data-testid="first-field"]') as HTMLInputElement
    const lastAction = dialog.querySelector('[data-testid="last-action"]') as HTMLButtonElement
    expect(document.activeElement).toBe(firstField)
    lastAction.focus()
    const forward = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    lastAction.dispatchEvent(forward)
    expect(forward.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(closeButton)
    const backward = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })
    closeButton.dispatchEvent(backward)
    expect(backward.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(lastAction)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.setProps({ open: false })
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
    trigger.remove()
  })

  it('lets only the top modal trap focus and restores focus into the underlying dialog', async () => {
    const lower = mountModal({ trapFocus: true, closeOnEsc: true }, '<button data-testid="lower-action">Next</button>')
    await nextTick()
    const lowerAction = document.body.querySelector('[data-testid="lower-action"]') as HTMLButtonElement
    lowerAction.focus()
    const upper = mountModal({ trapFocus: true, closeOnEsc: true, title: 'Upper' }, '<button data-testid="upper-action">Save</button>')
    await nextTick()
    const upperAction = document.body.querySelector('[data-testid="upper-action"]') as HTMLButtonElement
    upperAction.focus()
    const topDialog = upperAction.closest('[role="dialog"]') as HTMLElement
    const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    upperAction.dispatchEvent(tab)
    expect(document.activeElement).toBe(topDialog.querySelector('button[aria-label="Close"]'))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    expect(upper.emitted('close')).toHaveLength(1)
    expect(lower.emitted('close')).toBeUndefined()
    await upper.setProps({ open: false })
    expect(document.activeElement).toBe(lowerAction)
    upper.unmount()
    lower.unmount()
  })

  it('does not steal focus when the opener has been removed before the modal closes', async () => {
    const trigger = document.createElement('button')
    const fallback = document.createElement('button')
    document.body.append(trigger, fallback)
    trigger.focus()
    const wrapper = mountModal({ trapFocus: true })
    await nextTick()
    trigger.remove()
    fallback.focus()
    await wrapper.setProps({ open: false })
    expect(document.activeElement).toBe(fallback)
    wrapper.unmount()
    fallback.remove()
  })

  it('tabs to a closed details summary and includes its controls only after it opens', async () => {
    const wrapper = mountModal({ trapFocus: true }, '<input data-testid="main-field" /><details><summary>Advanced</summary><button data-testid="advanced-action">Advanced setting</button></details>')
    await nextTick()
    const advancedAction = document.body.querySelector('[data-testid="advanced-action"]') as HTMLButtonElement
    const dialog = advancedAction.closest('[role="dialog"]') as HTMLElement
    const closeButton = dialog.querySelector('button[aria-label="Close"]') as HTMLButtonElement
    const details = dialog.querySelector('details') as HTMLDetailsElement
    const summary = dialog.querySelector('summary') as HTMLElement
    // Happy DOM omits summary's native tabIndex=0 (verified in Chromium).
    summary.tabIndex = 0
    summary.focus()
    const closedTab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    summary.dispatchEvent(closedTab)
    expect(closedTab.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(closeButton)
    details.open = true
    summary.focus()
    const openedTab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    summary.dispatchEvent(openedTab)
    expect(openedTab.defaultPrevented).toBe(false)
    advancedAction.focus()
    const lastTab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    advancedAction.dispatchEvent(lastTab)
    expect(lastTab.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(closeButton)
    wrapper.unmount()
  })

  it('hides the leaving overlay from assistive technology while a replacement dialog opens', async () => {
    const leaving = mountModal({ trapFocus: true, title: 'Leaving dialog' }, '<button data-testid="leaving-action">Next</button>')
    await nextTick()
    const leavingAction = document.body.querySelector('[data-testid="leaving-action"]') as HTMLElement
    const leavingOverlay = leavingAction.closest('[role="dialog"]')!.parentElement!
    await leaving.setProps({ open: false })
    expect(leavingOverlay.getAttribute('aria-hidden')).toBe('true')
    expect(leavingOverlay.inert).toBe(true)

    const entering = mountModal({ open: false, trapFocus: true, title: 'Replacement dialog' }, '<input autofocus data-testid="entering-field" />')
    await entering.setProps({ open: true })
    await nextTick()
    const enteringField = document.body.querySelector('[data-testid="entering-field"]') as HTMLElement
    const enteringOverlay = enteringField.closest('[role="dialog"]')!.parentElement!
    expect(enteringOverlay.hasAttribute('aria-hidden')).toBe(false)
    expect(enteringOverlay.inert).toBe(false)
    expect(document.activeElement).toBe(enteringField)
    entering.unmount()
    leaving.unmount()
  })

  it('neutralizes footer primary only, and leaves page primary purple', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'), 'utf8')
    expect(css).toMatch(/html\.light \.app-modal-footer button\.bg-accent[\s\S]*background-color:\s*#18181b/i)
    expect(css).toMatch(/html\.light \.app-modal-footer button\.bg-accent[\s\S]*color:\s*#ffffff/i)
    expect(css).toMatch(/html\.light \.app-modal-footer button\.bg-accent[\s\S]*--hover-ink-color:\s*#52525b/i)
    expect(css).toMatch(/\.app-modal-footer button\.bg-accent \{[\s\S]*background-color:\s*#ededf0/i)
    expect(css).toMatch(/\.app-modal-footer button\.bg-accent \{[\s\S]*color:\s*#141417/i)
    expect(css).toMatch(/\.app-modal-footer button\.bg-accent \{[\s\S]*--hover-ink-color:\s*#a1a1aa/i)
    expect(css).toMatch(/\.app-modal-footer button\.bg-accent \{[\s\S]*--tw-shadow:\s*0 0 #0000/)
    expect(css).toMatch(/\.app-modal-footer button\.bg-accent \.app-icon/)
    const drawer = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppDrawer.vue'), 'utf8')
    expect(drawer).not.toContain('app-modal-footer')
    const button = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppButton.vue'), 'utf8')
    expect(button).toContain("primary: 'bg-accent text-white shadow-glow'")
  })

  it('puts footer primary, ghost, danger, and outline in the footer without adding icons', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
    const wrapper = mount(AppModal, {
      props: { open: true, title: '保存' },
      slots: {
        footer: () => [
          h(AppButton, { variant: 'ghost' }, () => '取消'),
          h(AppButton, { variant: 'outline' }, () => '线框'),
          h(AppButton, { variant: 'danger' }, () => '删除'),
          h(AppButton, { variant: 'primary' }, () => '保存'),
        ],
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: { props: ['name'], template: '<span :data-icon="name" />' } },
        directives: { 'hover-ink': vHoverInk },
      },
      attachTo: document.body,
    })
    await nextTick()
    const footer = document.body.querySelector('.app-modal-footer') as HTMLElement
    expect(footer).toBeTruthy()
    const buttons = [...footer.querySelectorAll('button')]
    expect(buttons).toHaveLength(4)
    expect(buttons[0].className).toContain('text-txt2')
    expect(buttons[0].className).not.toContain('bg-accent')
    expect(buttons[1].className).toContain('border')
    expect(buttons[1].className).not.toContain('bg-accent')
    expect(buttons[2].className).toContain('text-err')
    expect(buttons[2].className).not.toContain('bg-accent')
    expect(buttons[3].className).toContain('bg-accent')
    expect(buttons[3].className).toContain('shadow-glow')
    expect(footer.querySelector('[data-icon]')).toBeNull()
    const page = document.createElement('div')
    document.body.append(page)
    const pageBtn = mount(AppButton, {
      props: { variant: 'primary' },
      slots: { default: '新建' },
      global: { plugins: [i18n], stubs: { Icon: true }, directives: { 'hover-ink': vHoverInk } },
      attachTo: page,
    })
    expect(pageBtn.classes()).toContain('bg-accent')
    expect(pageBtn.classes()).toContain('shadow-glow')
    expect(pageBtn.element.closest('.app-modal-footer')).toBeNull()
    pageBtn.unmount()
    page.remove()
    wrapper.unmount()
  })

  it('does not spread hover ink on a disabled or loading footer primary', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
    const wrapper = mount(AppModal, {
      props: { open: true, title: '保存' },
      slots: {
        footer: () => [
          h(AppButton, { variant: 'primary', disabled: true }, () => '禁用'),
          h(AppButton, { variant: 'primary', loading: true }, () => '加载'),
        ],
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true },
        directives: { 'hover-ink': vHoverInk },
      },
      attachTo: document.body,
    })
    await nextTick()
    const buttons = [...document.body.querySelectorAll('.app-modal-footer button')] as HTMLButtonElement[]
    expect(buttons).toHaveLength(2)
    for (const button of buttons) {
      button.dispatchEvent(new PointerEvent('pointerenter', { clientX: 8, clientY: 8, pointerType: 'mouse', bubbles: true }))
      expect(button.classList.contains('is-hover-ink')).toBe(false)
    }
    expect(buttons[1].getAttribute('aria-busy')).toBe('true')
    expect(buttons[1].querySelector('.app-spinner, .icon-spinner, [data-icon="spinner"]')).toBeTruthy()
    wrapper.unmount()
    vi.unstubAllGlobals()
  })
})

