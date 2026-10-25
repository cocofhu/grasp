// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChatMessageRail from './ChatMessageRail.vue'

const wrappers: VueWrapper[] = []
const messages = [{ id: 'question-1', title: '如何交付这次变更？', preview: '先完成验证，再检查交付材料。' }, { id: 'question-2', title: '手机如何验收？', preview: '检查滚动、返回与文件内容。' }]
const locale = { pages: { chatNavigation: { ariaLabel: '对话定位', jumpTo: '跳转到：{title}', loadedHistory: '仅显示已加载的提问', loadEarlier: '加载更早消息', loadingEarlier: '正在加载更早消息…' } } }

function rect(top: number, height = 40) {
  return { x: 0, y: top, top, bottom: top + height, left: 0, right: 600, width: 600, height, toJSON: () => ({}) } as DOMRect
}
function conversation() {
  const scroller = document.createElement('div')
  scroller.scrollTop = 0
  scroller.getBoundingClientRect = () => rect(100, 400)
  Object.defineProperty(scroller, 'clientHeight', { value: 400 })
  const targets = messages.map((message, index) => {
    const target = document.createElement('div')
    target.dataset.chatMessageId = message.id
    target.textContent = message.title
    target.getBoundingClientRect = () => rect(120 + index * 450 - scroller.scrollTop)
    scroller.append(target)
    return target
  })
  scroller.scrollTo = vi.fn((options: ScrollToOptions | number) => { scroller.scrollTop = typeof options === 'number' ? options : options.top || 0 })
  document.body.append(scroller)
  return { scroller, targets }
}
function render(scroller: HTMLElement, extra = {}) {
  const wrapper = mount(ChatMessageRail, { attachTo: document.body, props: { items: messages, scroller, scopeKey: 'thread-1', ...extra }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': locale } })], stubs: { Icon: true } } })
  wrappers.push(wrapper)
  return wrapper
}
async function settle() { await nextTick(); await new Promise(resolve => setTimeout(resolve, 24)); await nextTick() }

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => setTimeout(() => callback(0), 0))
  vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; vi.unstubAllGlobals() })

const card = () => document.querySelector<HTMLElement>('[data-testid="chat-message-rail-card"]')
const rows = () => Array.from(document.querySelectorAll<HTMLElement>('[data-testid="chat-message-rail-row"]'))
async function waitClose() { await new Promise(resolve => setTimeout(resolve, 140)); await nextTick() }

describe('ChatMessageRail', () => {
  it('lists every question as parsed text in the card and previews the pointed reply', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { items: [{ ...messages[0], title: '## 我的选择\n\n- **修缺陷**', preview: '本次是**修缺陷**。请查看 [文档](https://example.com) 与 `feedback_index.json`。' }, messages[1]] })
    const marker = wrapper.get('[data-testid="chat-message-rail-mark"]')
    expect(marker.attributes('aria-label')).toBe('跳转到：我的选择 修缺陷')
    await marker.trigger('mouseenter')
    await flushPromises()
    expect(rows().map(row => row.textContent?.trim())).toEqual(['我的选择 修缺陷', messages[1]!.title])
    expect(rows()[0]!.classList.contains('is-pointed')).toBe(true)
    expect(card()!.querySelector('.chat-message-rail-preview')?.textContent).toBe('本次是修缺陷。请查看 文档 与 feedback_index.json。')
    expect(card()!.querySelector('a, img, strong')).toBeNull()
    expect(marker.attributes('aria-controls')).toBe(card()!.id)

    await rows()[1]!.dispatchEvent(new Event('mouseenter'))
    await nextTick()
    expect(card()!.querySelector('.chat-message-rail-preview')?.textContent).toBe(messages[1]!.preview)
    expect(wrapper.findAll('[data-testid="chat-message-rail-mark"]')[1]!.classes()).toContain('is-pointed')

    await wrapper.setProps({ items: [{ ...messages[0], title: '**新的选择**', preview: `**${'继续验证'.repeat(160)}**` }] })
    await rows()[0]!.dispatchEvent(new Event('mouseenter'))
    await nextTick()
    expect(marker.attributes('aria-label')).toBe('跳转到：新的选择')
    expect(card()!.querySelector('.chat-message-rail-preview')?.textContent).toHaveLength(480)
    expect(card()!.textContent).not.toContain('**')
  })

  it('renders untrusted previews as text and keeps the card open while the pointer crosses to it', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { items: [{ ...messages[0], preview: '<img src=x onerror=alert(1)>正文' }] })
    const track = wrapper.get('[data-testid="chat-message-rail-track"]')
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    await flushPromises()
    expect(card()!.textContent).toContain('<img src=x onerror=alert(1)>正文')
    expect(card()!.querySelector('img')).toBeNull()
    await track.trigger('mouseleave')
    card()!.dispatchEvent(new Event('mouseenter'))
    await waitClose()
    expect(card()).not.toBeNull()
    card()!.dispatchEvent(new Event('mouseleave'))
    await waitClose()
    expect(card()).toBeNull()
  })

  it('does not open the card on touch screens', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(hover: none)' }))
    const { scroller } = conversation()
    const wrapper = render(scroller)
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    expect(card()).toBeNull()
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['question-1']])
  })

  it('supports keyboard focus, arrows, Home, End and Escape without jumping on focus', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    const markers = wrapper.findAll('[data-testid="chat-message-rail-mark"]')
    ;(markers[0]!.element as HTMLButtonElement).focus()
    await nextTick()
    expect(rows()[0]!.classList.contains('is-pointed')).toBe(true)
    await markers[0]!.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(markers[1]!.element)
    await nextTick()
    expect(rows()[1]!.classList.contains('is-pointed')).toBe(true)
    await markers[1]!.trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(markers[0]!.element)
    await markers[0]!.trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(markers[1]!.element)
    await waitClose()
    expect(card()).not.toBeNull()
    await markers[1]!.trigger('keydown', { key: 'Escape' })
    expect(card()).toBeNull()
    expect(scroller.scrollTo).not.toHaveBeenCalled()
    expect(wrapper.emitted('navigate')).toBeUndefined()
  })

  it('notifies the host before scrolling and highlights the selected stable message', async () => {
    const { scroller, targets } = conversation()
    let emittedBeforeScroll = false
    const wrapper = render(scroller)
    scroller.scrollTo = vi.fn(() => { emittedBeforeScroll = !!wrapper.emitted('navigate') })
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[1]!.trigger('mouseenter')
    expect(card()).not.toBeNull()
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[1]!.trigger('click')
    expect(card()).toBeNull()
    expect(emittedBeforeScroll).toBe(true)
    expect(wrapper.emitted('navigate')).toEqual([['question-2']])
    expect(scroller.scrollTo).toHaveBeenCalledWith({ top: 454, behavior: 'auto' })
    expect(targets[1]!.classList.contains('chat-message-located')).toBe(true)
    await wrapper.setProps({ scopeKey: 'thread-2' })
    expect(targets[1]!.classList.contains('chat-message-located')).toBe(false)
  })

  it('jumps from a card row and closes the card', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    rows()[1]!.click()
    await nextTick()
    expect(wrapper.emitted('navigate')).toEqual([['question-2']])
    expect(scroller.scrollTo).toHaveBeenCalledWith({ top: 454, behavior: 'auto' })
    expect(card()).toBeNull()
  })

  it('tracks the active question as the user scrolls', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    await settle()
    expect(wrapper.findAll('[aria-current="step"]')[0]!.attributes('data-message-id')).toBe('question-1')
    scroller.scrollTop = 470
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(wrapper.findAll('[aria-current="step"]')[0]!.attributes('data-message-id')).toBe('question-2')
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    expect(rows()[1]!.getAttribute('aria-selected')).toBe('true')
    expect(rows()[1]!.classList.contains('is-active')).toBe(true)
  })

  it('labels the loaded window and requests earlier messages without navigating', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { hasMore: true })
    const load = wrapper.get('[data-testid="chat-message-rail-earlier"]')
    expect(load.attributes('aria-label')).toBe('加载更早消息')
    await load.trigger('click')
    expect(wrapper.emitted('loadEarlier')).toHaveLength(1)
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    expect(card()?.textContent).toContain('仅显示已加载的提问')
    document.querySelector<HTMLElement>('[data-testid="chat-message-rail-card-earlier"]')!.click()
    expect(wrapper.emitted('loadEarlier')).toHaveLength(2)
    expect(wrapper.emitted('navigate')).toBeUndefined()
    await wrapper.setProps({ loadingEarlier: true })
    expect(load.attributes('disabled')).toBeDefined()
    expect(load.attributes('aria-label')).toBe('正在加载更早消息…')
  })

  it('closes the card when the session or loaded messages change', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    await wrapper.get('[data-message-id="question-2"]').trigger('mouseenter')
    expect(card()).not.toBeNull()
    await wrapper.setProps({ items: [messages[0]] })
    expect(rows()).toHaveLength(1)
    expect(rows()[0]!.classList.contains('is-pointed')).toBe(false)
    await wrapper.setProps({ scopeKey: 'another-thread' })
    expect(card()).toBeNull()
    await wrapper.get('[data-message-id="question-1"]').trigger('mouseenter')
    await wrapper.setProps({ items: [] })
    expect(card()).toBeNull()
  })

  it('clamps the card within a narrow viewport', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    const { scroller } = conversation()
    const wrapper = render(scroller)
    const marker = wrapper.get('[data-testid="chat-message-rail-mark"]')
    marker.element.getBoundingClientRect = () => ({ ...rect(820), left: 360, right: 386, width: 26 })
    wrapper.get('[data-testid="chat-message-rail-track"]').element.getBoundingClientRect = () => ({ ...rect(600, 260), left: 360, right: 386, width: 26 })
    await marker.trigger('mouseenter')
    await flushPromises()
    const element = card()!
    expect(Number.parseFloat(element.style.left)).toBeGreaterThanOrEqual(12)
    expect(Number.parseFloat(element.style.left) + Number.parseFloat(element.style.width)).toBeLessThanOrEqual(378)
    expect(Number.parseFloat(element.style.top)).toBeGreaterThanOrEqual(12)
  })

  function box(top: number, height: number, width = 26) {
    return { x: 0, y: top, top, bottom: top + height, left: 0, right: width, width, height, toJSON: () => ({}) } as DOMRect
  }
  function installScroll(element: HTMLElement, metrics: { clientHeight: number; scrollHeight: number; scrollTop: number }) {
    Object.defineProperty(element, 'clientHeight', { configurable: true, get: () => metrics.clientHeight })
    Object.defineProperty(element, 'scrollHeight', { configurable: true, get: () => metrics.scrollHeight })
    Object.defineProperty(element, 'scrollTop', { configurable: true, get: () => metrics.scrollTop, set: (value: number) => { metrics.scrollTop = Number(value) || 0 } })
  }
  function threaded(count: number, metrics: { clientHeight?: number; scrollHeight: number; scrollTop: number }, tops: number[], omit: string[] = []) {
    const items = Array.from({ length: count }, (_, index) => ({ id: `question-${index + 1}`, title: `提问 ${index + 1}`, preview: `预览 ${index + 1}` }))
    const scroller = document.createElement('div')
    const scroll = { clientHeight: metrics.clientHeight ?? 400, scrollHeight: metrics.scrollHeight, scrollTop: metrics.scrollTop }
    installScroll(scroller, scroll)
    scroller.getBoundingClientRect = () => box(100, scroll.clientHeight, 600)
    const missing = new Set(omit)
    items.forEach((item, index) => {
      if (missing.has(item.id)) return
      const target = document.createElement('div')
      target.dataset.chatMessageId = item.id
      target.getBoundingClientRect = () => box(tops[index]! - scroll.scrollTop, 40, 600)
      scroller.append(target)
    })
    scroller.scrollTo = vi.fn((options: ScrollToOptions | number) => { scroll.scrollTop = typeof options === 'number' ? options : options.top || 0 })
    document.body.append(scroller)
    return { scroller, items, scroll }
  }
  const currentId = (wrapper: VueWrapper) => wrapper.get('[aria-current="step"]').attributes('data-message-id')

  it('selects the last loaded question when a long thread is within 4px of the bottom', async () => {
    // At the bottom the last three tops stay below the reading line (scroller top 100 + 80).
    const tops = [120, 700, 1500, 1900, 2050, 2200]
    const { scroller, items, scroll } = threaded(6, { scrollHeight: 2000, scrollTop: 1600 }, tops)
    const wrapper = render(scroller, { items })
    await settle()
    expect(currentId(wrapper)).toBe('question-6')
    expect(wrapper.get('[data-message-id="question-6"]').classes()).toContain('is-active')

    scroll.scrollTop = 1596
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(currentId(wrapper)).toBe('question-6')

    scroll.scrollTop = 1595
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(currentId(wrapper)).toBe('question-3')

    const missing = threaded(6, { scrollHeight: 2000, scrollTop: 1600 }, tops, ['question-6'])
    const missingRail = render(missing.scroller, { items: missing.items })
    await settle()
    expect(currentId(missingRail)).toBe('question-6')
  })

  it('follows a question appended while the thread stays at the bottom', async () => {
    const { scroller, items, scroll } = threaded(3, { scrollHeight: 2000, scrollTop: 1600 }, [120, 1900, 2100])
    const wrapper = render(scroller, { items })
    await settle()
    expect(currentId(wrapper)).toBe('question-3')
    scroll.scrollTop = 1600
    await wrapper.setProps({ items: [...items, { id: 'question-4', title: '提问 4', preview: '预览 4' }] })
    await settle()
    expect(scroll.scrollTop).toBe(1600)
    expect(currentId(wrapper)).toBe('question-4')
  })

  it('keeps the reading line when the thread is too short to scroll', async () => {
    const { scroller, items, scroll } = threaded(3, { scrollHeight: 404, scrollTop: 0 }, [120, 240, 360])
    const wrapper = render(scroller, { items })
    const track = wrapper.get('[data-testid="chat-message-rail-track"]').element as HTMLElement
    const trackScroll = { clientHeight: 240, scrollHeight: 120, scrollTop: 12 }
    installScroll(track, trackScroll)
    await settle()
    expect(currentId(wrapper)).toBe('question-1')
    expect(trackScroll.scrollTop).toBe(12)
    expect(scroll.scrollTop).toBe(0)

    const alone = threaded(1, { scrollHeight: 80, scrollTop: 0 }, [120])
    const aloneRail = render(alone.scroller, { items: alone.items })
    await settle()
    expect(currentId(aloneRail)).toBe('question-1')

    scroll.scrollHeight = 405
    scroll.scrollTop = 1
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(currentId(wrapper)).toBe('question-3')
  })

  it('pins the last tick to the bottom of an overflowing rail without moving the conversation', async () => {
    const tops = Array.from({ length: 30 }, (_, index) => index < 27 ? 200 + index * 288 : 7900 + (index - 27) * 200)
    const { scroller, items, scroll } = threaded(30, { scrollHeight: 8000, scrollTop: 7595 }, tops)
    const wrapper = render(scroller, { items })
    const track = wrapper.get('[data-testid="chat-message-rail-track"]').element as HTMLElement
    const trackScroll = { clientHeight: 80, scrollHeight: 600, scrollTop: 480 }
    installScroll(track, trackScroll)
    track.getBoundingClientRect = () => box(140, trackScroll.clientHeight)
    const marks = () => wrapper.findAll('[data-testid="chat-message-rail-mark"]').map(mark => mark.element as HTMLElement)
    marks().forEach((mark, index) => { mark.getBoundingClientRect = () => box(140 + index * 20 - trackScroll.scrollTop, 18) })
    await settle()
    expect(currentId(wrapper)).toBe('question-27')
    expect(trackScroll.scrollTop).toBe(480)
    expect(scroll.scrollTop).toBe(7595)
    expect(scroller.scrollTo).not.toHaveBeenCalled()

    trackScroll.scrollTop = 0
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(currentId(wrapper)).toBe('question-27')
    expect(trackScroll.scrollTop).toBe(458)
    expect(scroll.scrollTop).toBe(7595)

    scroll.scrollTop = 7600
    scroller.dispatchEvent(new Event('scroll'))
    await settle()
    expect(currentId(wrapper)).toBe('question-30')
    expect(trackScroll.scrollTop).toBe(520)
    expect(scroll.scrollTop).toBe(7600)
    const column = track.getBoundingClientRect()
    const last = marks().at(-1)!.getBoundingClientRect()
    expect(last.top).toBeGreaterThanOrEqual(column.top)
    expect(last.bottom).toBeLessThanOrEqual(column.bottom)
    expect(column.bottom - last.bottom).toBeLessThanOrEqual(20)
    expect(scroller.scrollTo).not.toHaveBeenCalled()
  })

  it('uses separate card identities for simultaneous conversations', async () => {
    const first = conversation().scroller
    const second = conversation().scroller
    const wrapper = mount({ render: () => h('div', [h(ChatMessageRail, { items: messages, scroller: first, scopeKey: 'thread-1' }), h(ChatMessageRail, { items: messages, scroller: second, scopeKey: 'thread-2' })]) }, { attachTo: document.body, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': locale } })], stubs: { Icon: true } } })
    wrappers.push(wrapper)
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[0]!.trigger('mouseenter')
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[2]!.trigger('mouseenter')
    const ids = Array.from(document.querySelectorAll('[data-testid="chat-message-rail-card"]')).map(el => el.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
  })
})
