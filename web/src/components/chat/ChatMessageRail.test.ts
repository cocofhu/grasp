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

describe('ChatMessageRail', () => {
  it('shows parsed Markdown summaries and accessible labels without creating rich tooltip controls', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { items: [{ ...messages[0], title: '## 我的选择\n\n- **修缺陷**', preview: '本次是**修缺陷**。请查看 [文档](https://example.com) 与 `feedback_index.json`。' }] })
    const marker = wrapper.get('[data-testid="chat-message-rail-mark"]')
    expect(marker.attributes('aria-label')).toBe('跳转到：我的选择 修缺陷')
    await marker.trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector('[role="tooltip"]')!
    expect(tooltip.querySelector('.chat-message-rail-question')?.textContent).toBe('我的选择 修缺陷')
    expect(tooltip.querySelector('.chat-message-rail-preview')?.textContent).toBe('本次是修缺陷。请查看 文档 与 feedback_index.json。')
    expect(tooltip.querySelector('a, img, strong')).toBeNull()

    await wrapper.setProps({ items: [{ ...messages[0], title: '**新的选择**', preview: `**${'继续验证'.repeat(160)}**` }] })
    expect(marker.attributes('aria-label')).toBe('跳转到：新的选择')
    expect(tooltip.querySelector('.chat-message-rail-preview')?.textContent).toHaveLength(480)
    expect(tooltip.textContent).not.toContain('**')
  })

  it('previews the question and answer as text on hover, then dismisses it', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { items: [{ ...messages[0], preview: '<img src=x onerror=alert(1)>正文' }] })
    const marker = wrapper.get('[data-testid="chat-message-rail-mark"]')
    await marker.trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector('[role="tooltip"]')!
    expect(tooltip.textContent).toContain(messages[0]!.title)
    expect(tooltip.textContent).toContain('<img src=x onerror=alert(1)>正文')
    expect(tooltip.querySelector('img')).toBeNull()
    expect(marker.attributes('aria-describedby')).toBe(tooltip.id)
    await marker.trigger('mouseleave')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('supports keyboard focus, arrows, Home, End and Escape without jumping on focus', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    const markers = wrapper.findAll('[data-testid="chat-message-rail-mark"]')
    ;(markers[0]!.element as HTMLButtonElement).focus()
    await nextTick()
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(messages[0]!.title)
    await markers[0]!.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(markers[1]!.element)
    await markers[1]!.trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(markers[0]!.element)
    await markers[0]!.trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(markers[1]!.element)
    await markers[1]!.trigger('keydown', { key: 'Escape' })
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    expect(scroller.scrollTo).not.toHaveBeenCalled()
    expect(wrapper.emitted('navigate')).toBeUndefined()
  })

  it('notifies the host before scrolling and highlights the selected stable message', async () => {
    const { scroller, targets } = conversation()
    let emittedBeforeScroll = false
    const wrapper = render(scroller)
    scroller.scrollTo = vi.fn(() => { emittedBeforeScroll = !!wrapper.emitted('navigate') })
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[1]!.trigger('mouseenter')
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull()
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[1]!.trigger('click')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    expect(emittedBeforeScroll).toBe(true)
    expect(wrapper.emitted('navigate')).toEqual([['question-2']])
    expect(scroller.scrollTo).toHaveBeenCalledWith({ top: 454, behavior: 'auto' })
    expect(targets[1]!.classList.contains('chat-message-located')).toBe(true)
    await wrapper.setProps({ scopeKey: 'thread-2' })
    expect(targets[1]!.classList.contains('chat-message-located')).toBe(false)
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
  })

  it('labels the loaded window and requests earlier messages without navigating', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller, { hasMore: true })
    const load = wrapper.get('[data-testid="chat-message-rail-earlier"]')
    expect(load.attributes('aria-label')).toBe('加载更早消息')
    await load.trigger('click')
    expect(wrapper.emitted('loadEarlier')).toHaveLength(1)
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('仅显示已加载的提问')
    await wrapper.setProps({ loadingEarlier: true })
    expect(load.attributes('disabled')).toBeDefined()
    expect(load.attributes('aria-label')).toBe('正在加载更早消息…')
  })

  it('clears stale previews when the session or loaded messages change', async () => {
    const { scroller } = conversation()
    const wrapper = render(scroller)
    await wrapper.get('[data-message-id="question-2"]').trigger('mouseenter')
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull()
    await wrapper.setProps({ items: [messages[0]] })
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await wrapper.get('[data-message-id="question-1"]').trigger('mouseenter')
    await wrapper.setProps({ scopeKey: 'another-thread' })
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('clamps the preview within a narrow viewport', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    const { scroller } = conversation()
    const wrapper = render(scroller)
    const marker = wrapper.get('[data-testid="chat-message-rail-mark"]')
    marker.element.getBoundingClientRect = () => ({ ...rect(820), left: 360, right: 386, width: 26 })
    await marker.trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]')!
    expect(Number.parseFloat(tooltip.style.left)).toBeGreaterThanOrEqual(12)
    expect(Number.parseFloat(tooltip.style.left) + Number.parseFloat(tooltip.style.width)).toBeLessThanOrEqual(378)
  })

  it('uses separate tooltip identities for simultaneous conversations', async () => {
    const first = conversation().scroller
    const second = conversation().scroller
    const wrapper = mount({ render: () => h('div', [h(ChatMessageRail, { items: messages, scroller: first, scopeKey: 'thread-1' }), h(ChatMessageRail, { items: messages, scroller: second, scopeKey: 'thread-2' })]) }, { attachTo: document.body, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': locale } })], stubs: { Icon: true } } })
    wrappers.push(wrapper)
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[0]!.trigger('mouseenter')
    await wrapper.findAll('[data-testid="chat-message-rail-mark"]')[2]!.trigger('mouseenter')
    const ids = Array.from(document.querySelectorAll('[role="tooltip"]')).map(el => el.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
  })
})
