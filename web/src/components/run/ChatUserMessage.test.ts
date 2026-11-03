// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ChatUserMessage from './ChatUserMessage.vue'

describe('ChatUserMessage markdown', () => {
  it('shows the recorded time on hover without inventing a time for older rows', () => {
    const known = mount(ChatUserMessage, { props: { text: 'A question', createdAt: '2026-10-28T08:00:00Z' } })
    expect(known.attributes('title')).toMatch(/2026-10-28/)
    const old = mount(ChatUserMessage, { props: { text: 'Old question' } })
    expect(old.attributes('title')).toBeUndefined()
    known.unmount(); old.unmount()
  })
  it('renders headings, emphasis, lists and inline code like assistant replies', () => {
    const w = mount(ChatUserMessage, {
      props: {
        testId: 'clarify-human-message',
        text: '请按 **Markdown** 验收这三项:\n\n1. 标题层级\n2. 列表\n3. `code`\n',
      },
    })
    const bubble = w.get('[data-testid="clarify-human-message"]')
    expect(bubble.classes()).toContain('chat-user-message')
    expect(bubble.find('.chat-prose').exists()).toBe(true)
    expect(bubble.find('strong').text()).toBe('Markdown')
    expect(bubble.findAll('li').map((li) => li.text())).toEqual(['标题层级', '列表', 'code'])
    expect(bubble.find('code').text()).toBe('code')
    expect(bubble.text()).not.toContain('**')
    w.unmount()
  })

  it('renders ## headings with or without a space after the hashes', () => {
    for (const text of ['## 上游交接\n\n请重点验收', '##上游交接\n\n请重点验收']) {
      const w = mount(ChatUserMessage, { props: { text } })
      expect(w.text()).toContain('上游交接')
      expect(w.text()).toContain('请重点验收')
      expect(w.text()).not.toContain('##')
      expect(w.find('p').text()).toContain('请重点验收')
      w.unmount()
    }
  })

  it('keeps plain text line breaks', () => {
    const w = mount(ChatUserMessage, { props: { text: '第一行\n第二行' } })
    expect(w.text()).toContain('第一行')
    expect(w.text()).toContain('第二行')
    expect(w.find('br').exists()).toBe(true)
    w.unmount()
  })

  it('shows raw HTML tags as text and does not execute them', () => {
    const tags = ['script', 'style', 'iframe', 'noscript', 'xmp'] as const
    for (const tag of tags) {
      const w = mount(ChatUserMessage, {
        props: { text: `说明：<${tag}>关键步骤</${tag}>` },
      })
      expect(w.text()).toContain('说明：')
      expect(w.text()).toContain('关键步骤')
      expect(w.text()).toContain(`<${tag}>`)
      expect(w.text()).toContain(`</${tag}>`)
      expect(w.element.querySelector(tag)).toBeNull()
      w.unmount()
    }
  })

  it('renders referenced @labels as chips in place', async () => {
    const references = [
      { kind: 'github_issue' as const, target: 'acme/web#815', title: 'Bug', url: 'https://github.com/acme/web/issues/815', state: 'open' },
      { kind: 'github_pr' as const, target: 'acme/web#814', title: 'Fix', url: 'https://github.com/acme/web/pull/814', state: 'open' },
    ]
    const w = mount(ChatUserMessage, {
      props: { text: '@#815 @#814 这两个看看', references, defaultRepo: 'acme/web' },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: {} })] },
    })
    await nextTick()
    expect(w.find('h1').exists()).toBe(false)
    const chips = w.findAll('[data-testid="solo-reference-chip"]')
    expect(chips.map((c) => c.text())).toEqual(['#815', '#814'])
    expect(chips[1]!.attributes('href')).toBe('https://github.com/acme/web/pull/814')
    expect(w.text()).toBe('#815 #814 这两个看看')
    w.unmount()
  })

  it.each([
    { text: '调整标题', annotations: [{ selector: '#title' }] },
    { text: '调整标题\n\nSelected page elements:\n/page · #title' },
    { text: '调整标题\n\nSelected page elements:\n{"selector":"#title","label":"/page · #title"}' },
  ])('renders structured and legacy picks as separate chips', (props) => {
    const w = mount(ChatUserMessage, {
      props,
      global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: {} })] },
    })
    expect(w.get('.chat-prose').text()).toBe('调整标题')
    expect(w.get('[data-testid="chat-message-annotations"]').text()).toBe('#title')
    w.unmount()
  })

  it('preserves a similar heading written as normal message text', () => {
    const w = mount(ChatUserMessage, { props: { text: 'Selected page elements:\nA normal sentence' } })
    expect(w.get('.chat-prose').text()).toContain('Selected page elements:')
    expect(w.find('[data-testid="chat-message-annotations"]').exists()).toBe(false)
    w.unmount()
  })

  it('keeps tag-like characters inside a fenced code block', () => {
    const w = mount(ChatUserMessage, {
      props: { text: '```html\n<script>alert(1)</script>\n```' },
    })
    expect(w.find('code').text()).toContain('<script>alert(1)</script>')
    expect(w.element.querySelector('script')).toBeNull()
    w.unmount()
  })
})
