// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import AttachmentDropZone from './AttachmentDropZone.vue'

function mountZone(disabled = false) {
  return mount(AttachmentDropZone, {
    props: { disabled },
    slots: { default: '<textarea /><button><span>attach</span></button>' },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': common } })] },
  })
}

async function drag(element: Element, type: string, dataTransfer: Partial<DataTransfer>) {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'dataTransfer', { value: dataTransfer })
  element.dispatchEvent(event)
  await nextTick()
  return event
}

describe('AttachmentDropZone', () => {
  it('keeps the hint across nested elements and clears it on leave, Escape, blur and external drop', async () => {
    const w = mountZone()
    const data = { types: ['Files'], dropEffect: 'none' } as Partial<DataTransfer>
    expect((await drag(w.element, 'dragenter', data)).defaultPrevented).toBe(true)
    await drag(w.get('span').element, 'dragenter', data)
    await drag(w.get('textarea').element, 'dragleave', data)
    expect(w.find('[data-testid="attachment-drop-hint"]').exists()).toBe(true)
    await drag(w.element, 'dragleave', data)
    expect(w.find('[data-testid="attachment-drop-hint"]').exists()).toBe(false)
    for (const event of [new KeyboardEvent('keydown', { key: 'Escape' }), new Event('blur'), new Event('drop'), new Event('dragend')]) {
      await drag(w.element, 'dragover', data)
      expect(data.dropEffect).toBe('copy')
      window.dispatchEvent(event)
      await nextTick()
      expect(w.find('[data-testid="attachment-drop-hint"]').exists()).toBe(false)
    }
    w.unmount()
  })

  it('passes multiple files once while leaving text drags to the editor', async () => {
    const w = mountZone()
    const files = [new File(['a'], 'a.txt'), new File(['b'], 'b.mp4')]
    const data = { types: ['Files'], files } as unknown as DataTransfer
    await drag(w.element, 'dragenter', data)
    expect((await drag(w.get('textarea').element, 'drop', data)).defaultPrevented).toBe(true)
    expect(w.emitted('files')).toEqual([[files]])
    expect(w.find('[role="status"]').exists()).toBe(false)
    expect((await drag(w.element, 'dragover', { types: ['text/plain'] })).defaultPrevented).toBe(false)
    expect((await drag(w.element, 'drop', { types: ['text/plain'] })).defaultPrevented).toBe(false)
    w.unmount()
  })

  it('prevents native navigation when disabled and resets when disabled during a drag', async () => {
    const w = mountZone()
    const data = { types: ['Files'], files: [new File(['a'], 'a.txt')], dropEffect: 'copy' } as unknown as DataTransfer
    await drag(w.element, 'dragenter', data)
    await w.setProps({ disabled: true })
    await drag(w.element, 'dragover', data)
    expect(data.dropEffect).toBe('none')
    expect(w.find('[role="status"]').exists()).toBe(false)
    expect((await drag(w.element, 'drop', data)).defaultPrevented).toBe(true)
    expect(w.emitted('files')).toBeUndefined()
    w.unmount()
  })

  it('rejects directories without discarding the other files', async () => {
    const w = mountZone()
    const file = new File(['a'], 'a.txt')
    await drag(w.element, 'drop', {
      types: ['Files'],
      items: [
        { kind: 'file', webkitGetAsEntry: () => ({ isDirectory: true }), getAsFile: () => new File([], 'folder') },
        { kind: 'file', webkitGetAsEntry: () => ({ isDirectory: false }), getAsFile: () => file },
      ],
    } as unknown as DataTransfer)
    expect(w.emitted('files')).toEqual([[[file]]])
    expect(w.get('[role="alert"]').text()).toContain('文件夹')
    w.unmount()
  })
})
