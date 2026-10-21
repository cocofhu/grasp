// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import ConfirmFlowDialog from './ConfirmFlowDialog.vue'
import type { ConfirmFlowDiscard } from '@/lib/inbox/confirmFlowDiscard'

function mountDialog(locale: 'zh-CN' | 'en', props: { open?: boolean; discard?: ConfirmFlowDiscard } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...commonEn, ...pagesEn },
    },
  })
  return mount(ConfirmFlowDialog, {
    props: { open: true, discard: 'none', ...props },
    global: {
      plugins: [i18n],
      directives: { 'hover-ink': vHoverInk },
      stubs: {
        Icon: { template: '<span data-testid="icon-close" />' },
        Transition: false,
      },
    },
    attachTo: document.body,
  })
}

function dialogEl() {
  return document.body.querySelector('[data-testid="confirm-flow-dialog-body"]')?.closest('[role="dialog"]') as HTMLElement | null
}

describe('ConfirmFlowDialog', () => {
  it('shows the Chinese consequence copy and focuses cancel', async () => {
    const wrapper = mountDialog('zh-CN')
    await flushPromises()
    expect(document.body.textContent).toContain('确认流转')
    expect(document.body.querySelector('[data-testid="confirm-flow-dialog-body"]')?.textContent).toBe(
      '将接受当前已落盘产物并进入下一节点。本次点击不会再触发 Agent。',
    )
    expect(document.body.querySelector('[data-testid="confirm-flow-dialog-discard"]')).toBeNull()
    expect(document.body.textContent).not.toContain('将被丢弃')
    const cancel = document.body.querySelector('[data-testid="confirm-flow-dialog-cancel"]') as HTMLButtonElement
    const accept = document.body.querySelector('[data-testid="confirm-flow-dialog-accept"]') as HTMLButtonElement
    expect(cancel.textContent).toContain('取消')
    expect(accept.textContent).toContain('确认流转')
    expect(document.activeElement).toBe(cancel)
    wrapper.unmount()
  })

  it('uses English copy without leaking key names', async () => {
    const wrapper = mountDialog('en')
    await flushPromises()
    expect(document.body.textContent).toContain('Confirm continuation')
    expect(document.body.querySelector('[data-testid="confirm-flow-dialog-body"]')?.textContent).toContain(
      'will not trigger the Agent again',
    )
    expect(document.body.textContent).not.toContain('confirmFlowDialog')
    wrapper.unmount()
  })

  it.each([
    ['edits', '未保存的编辑将被丢弃。'],
    ['draft', '未发送的草稿将被丢弃。'],
    ['both', '未保存的编辑和未发送的草稿将被丢弃。'],
  ] as const)('appends the %s discard sentence', async (discard, sentence) => {
    const wrapper = mountDialog('zh-CN', { discard })
    await flushPromises()
    expect(document.body.querySelector('[data-testid="confirm-flow-dialog-discard"]')?.textContent).toBe(sentence)
    wrapper.unmount()
  })

  it('cancel, close, backdrop and Escape do not confirm', async () => {
    const wrapper = mountDialog('zh-CN')
    await flushPromises()
    const cancel = document.body.querySelector('[data-testid="confirm-flow-dialog-cancel"]') as HTMLButtonElement
    cancel.click()
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toBeUndefined()

    const dialog = dialogEl()!
    dialog.querySelector('button')!.click()
    expect(wrapper.emitted('cancel')).toHaveLength(2)

    const backdrop = dialog.parentElement!.querySelector('.absolute') as HTMLElement
    backdrop.click()
    expect(wrapper.emitted('cancel')).toHaveLength(3)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.emitted('cancel')).toHaveLength(4)
    expect(wrapper.emitted('confirm')).toBeUndefined()
    wrapper.unmount()
  })

  it('Enter on the focused cancel button cancels and does not confirm', async () => {
    const wrapper = mountDialog('zh-CN')
    await flushPromises()
    const cancel = document.body.querySelector('[data-testid="confirm-flow-dialog-cancel"]') as HTMLButtonElement
    expect(document.activeElement).toBe(cancel)
    cancel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toBeUndefined()
    wrapper.unmount()
  })

  it('confirm emits only from the confirm button', async () => {
    const wrapper = mountDialog('zh-CN')
    await flushPromises()
    const accept = document.body.querySelector('[data-testid="confirm-flow-dialog-accept"]') as HTMLButtonElement
    accept.click()
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    expect(wrapper.emitted('cancel')).toBeUndefined()
    wrapper.unmount()
  })
})
