// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import CommentPinInspectCard from './CommentPinInspectCard.vue'

/** A container whose rect is fixed so placement math is deterministic. */
function container(width = 600, height = 400): HTMLElement {
  const el = document.createElement('div')
  el.getBoundingClientRect = () =>
    ({ width, height, left: 0, top: 0, right: width, bottom: height, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
  document.body.appendChild(el)
  return el
}

/** Pin the rendered card to a known size; happy-dom reports 0 otherwise. */
function sizeCard(w: ReturnType<typeof mountCard>, width: number, height: number) {
  const el = w.get('[data-testid="comment-pin-inspect-card"]').element as HTMLElement
  Object.defineProperty(el, 'offsetWidth', { value: width, configurable: true })
  Object.defineProperty(el, 'offsetHeight', { value: height, configurable: true })
}

function mountCard(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(CommentPinInspectCard, {
    props: { open: true, selector: '#app > .row', ...props },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('CommentPinInspectCard', () => {
  it('normalises css colors into uppercase hex', async () => {
    const w = mountCard()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.cssColorToHex(undefined)).toBeNull()
    expect(vm.cssColorToHex('  ')).toBeNull()
    expect(vm.cssColorToHex('transparent')).toBeNull()
    expect(vm.cssColorToHex('rgba(0, 0, 0, 0)')).toBeNull()
    expect(vm.cssColorToHex('#abc')).toBe('#AABBCC')
    expect(vm.cssColorToHex('#a1b2c3')).toBe('#A1B2C3')
    expect(vm.cssColorToHex('rgb(255, 0, 16)')).toBe('#FF0010')
    expect(vm.cssColorToHex('rgba(300 4 8 / 0.5)')).toBe('#FF0408')
    expect(vm.cssColorToHex('papayawhip')).toBe('papayawhip')
    w.unmount()
  })

  it('compacts a font stack down to its first family', async () => {
    const w = mountCard()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.compactFontFamily(undefined)).toBeNull()
    expect(vm.compactFontFamily('')).toBeNull()
    expect(vm.compactFontFamily('"Inter", sans-serif')).toBe('Inter')
    expect(vm.compactFontFamily("'Fira Code'")).toBe('Fira Code')
    w.unmount()
  })

  it('builds style rows from the anchor size and picked style', async () => {
    const w = mountCard({
      anchor: { left: 10, top: 20, width: 120.4, height: 40.6 },
      styleInfo: {
        color: 'rgb(17, 34, 51)',
        fontSize: '14px',
        fontWeight: '600',
        fontFamily: '"Inter", sans-serif',
        lineHeight: '20px',
      },
    })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.styleRows).toEqual([
      { label: 'Size', value: '120x41' },
      { label: 'Color', value: '#112233', swatch: '#112233' },
      { label: 'Font', value: '14px 600 Inter' },
      { label: 'Line', value: '20px' },
    ])
    expect(w.find('[data-testid="comment-pin-style-rows"]').exists()).toBe(true)
    w.unmount()
  })

  it('omits the default font weight and skips absent style fields', async () => {
    const w = mountCard({
      anchor: { left: 0, top: 0, width: 0, height: 0 },
      styleInfo: { color: 'transparent', fontSize: '12px', fontWeight: '400', fontFamily: '', lineHeight: '' },
    })
    await flushPromises()
    expect((w.vm as any).styleRows).toEqual([{ label: 'Font', value: '12px' }])
    w.unmount()
  })

  it('renders no style rows without an anchor or style info', async () => {
    const w = mountCard({ anchor: null, styleInfo: null })
    await flushPromises()
    expect((w.vm as any).styleRows).toEqual([])
    expect(w.find('[data-testid="comment-pin-style-rows"]').exists()).toBe(false)
    w.unmount()
  })

  it('places the card below the anchor when it fits', async () => {
    const wrap = container(600, 400)
    const w = mountCard({
      containerEl: wrap,
      anchor: { left: 40, top: 30, width: 100, height: 20 },
    })
    sizeCard(w, 320, 200)
    ;(w.vm as any).placeCardNear()
    await flushPromises()

    expect((w.vm as any).top).toBe(58)
    expect((w.vm as any).left).toBe(40)
    w.unmount()
  })

  it('flips above the anchor when there is no room below', async () => {
    const wrap = container(600, 400)
    const w = mountCard({
      containerEl: wrap,
      anchor: { left: 40, top: 300, width: 100, height: 20 },
    })
    sizeCard(w, 320, 200)
    ;(w.vm as any).placeCardNear()
    await flushPromises()

    expect((w.vm as any).top).toBe(92)
    w.unmount()
  })

  it('clamps to the container when neither side fits', async () => {
    const wrap = container(600, 220)
    const w = mountCard({
      containerEl: wrap,
      anchor: { left: 500, top: 150, width: 100, height: 20 },
    })
    sizeCard(w, 320, 200)
    ;(w.vm as any).placeCardNear()
    await flushPromises()

    expect((w.vm as any).top).toBe(12)
    expect((w.vm as any).left).toBe(272)
    w.unmount()
  })

  it('never places the card past the left edge', async () => {
    const wrap = container(200, 400)
    const w = mountCard({
      containerEl: wrap,
      anchor: { left: 10, top: 10, width: 10, height: 10 },
    })
    sizeCard(w, 320, 100)
    ;(w.vm as any).placeCardNear()
    await flushPromises()
    expect((w.vm as any).left).toBe(8)
    w.unmount()
  })

  it('falls back to the default corner without an anchor', async () => {
    const wrap = container(600, 400)
    const w = mountCard({ containerEl: wrap, anchor: null })
    sizeCard(w, 320, 200)
    ;(w.vm as any).placeCardNear()
    await flushPromises()
    expect((w.vm as any).top).toBe(8)
    expect((w.vm as any).left).toBe(8)
    w.unmount()
  })

  it('does nothing when there is no positioning container', async () => {
    const w = mountCard({ containerEl: null, anchor: { left: 99, top: 99, width: 1, height: 1 } })
    ;(w.vm as any).placeCardNear()
    await flushPromises()
    expect((w.vm as any).top).toBe(8)
    w.unmount()
  })

  it('re-places on window resize and detaches the listener on unmount', async () => {
    const wrap = container(600, 400)
    const w = mountCard({ containerEl: wrap, anchor: { left: 40, top: 30, width: 100, height: 20 } })
    sizeCard(w, 320, 200)

    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect((w.vm as any).top).toBe(58)

    w.unmount()
    // No throw once the listener is gone.
    window.dispatchEvent(new Event('resize'))
  })

  it('seeds the composer from initialComment when it opens', async () => {
    const w = mountCard({ open: false, initialComment: '旧意见' })
    await flushPromises()
    expect((w.vm as any).comment).toBe('')

    await w.setProps({ open: true })
    await flushPromises()
    expect((w.vm as any).comment).toBe('旧意见')

    await w.setProps({ initialComment: '改过的意见' })
    await flushPromises()
    expect((w.vm as any).comment).toBe('改过的意见')
    w.unmount()
  })

  it('ignores anchor changes while closed', async () => {
    const w = mountCard({ open: false, initialComment: 'x' })
    await w.setProps({ anchor: { left: 1, top: 1, width: 1, height: 1 } })
    await flushPromises()
    expect((w.vm as any).comment).toBe('')
    w.unmount()
  })

  it('blocks save and chat until the comment is non-empty', async () => {
    const w = mountCard()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.canSubmit).toBe(false)
    expect(w.get('[data-testid="comment-pin-save"]').attributes('disabled')).toBeDefined()

    vm.onSave()
    vm.onSendChat()
    vm.onComposerEnter()
    expect(w.emitted('save')).toBeFalsy()
    expect(w.emitted('send-chat')).toBeFalsy()

    await w.get('[data-testid="comment-pin-input"]').setValue('  这里对齐有问题  ')
    expect(vm.canSubmit).toBe(true)

    await w.get('[data-testid="comment-pin-save"]').trigger('click')
    expect(w.emitted('save')![0]).toEqual(['这里对齐有问题'])

    await w.get('[data-testid="comment-pin-send-chat"]').trigger('click')
    expect(w.emitted('send-chat')![0]).toEqual(['这里对齐有问题'])
    w.unmount()
  })

  it('submits on a plain Enter in the composer', async () => {
    const w = mountCard()
    await flushPromises()
    const input = w.get('[data-testid="comment-pin-input"]')
    await input.setValue('回车提交')
    await input.trigger('keydown.enter')
    expect(w.emitted('save')![0]).toEqual(['回车提交'])
    w.unmount()
  })

  it('closes on the close button and on Escape, but only while open', async () => {
    const w = mountCard()
    await flushPromises()

    await w.get('[data-testid="comment-pin-card-close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(w.emitted('close')).toHaveLength(2)

    // Other keys are ignored.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    await flushPromises()
    expect(w.emitted('close')).toHaveLength(2)

    await w.setProps({ open: false })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(w.emitted('close')).toHaveLength(2)
    w.unmount()
  })

  it('switches the thumbnail between image, missing and placeholder states', async () => {
    const w = mountCard({ imageDataUrl: 'data:image/png;base64,AA' })
    await flushPromises()
    expect(w.find('[data-testid="comment-pin-thumb"] img').exists()).toBe(true)

    await w.setProps({ screenshotMissing: true })
    expect(w.find('[data-testid="comment-pin-thumb"] img').exists()).toBe(false)
    expect(w.get('[data-testid="comment-pin-thumb"]').classes()).toContain('text-warn')

    await w.setProps({ screenshotMissing: false, imageDataUrl: undefined })
    expect(w.get('[data-testid="comment-pin-thumb"]').classes()).toContain('text-txt3')
    w.unmount()
  })

  it('shows a dash when no selector was picked', async () => {
    const w = mountCard({ selector: '' })
    await flushPromises()
    expect(w.text()).toContain('—')
    expect((w.vm as any).cardStyle.colorScheme).toBe('dark')
    w.unmount()
  })
})
