// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ChatUserMessage from './ChatUserMessage.vue'

describe('ChatUserMessage markdown', () => {
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

  it('keeps tag-like characters inside a fenced code block', () => {
    const w = mount(ChatUserMessage, {
      props: { text: '```html\n<script>alert(1)</script>\n```' },
    })
    expect(w.find('code').text()).toContain('<script>alert(1)</script>')
    expect(w.element.querySelector('script')).toBeNull()
    w.unmount()
  })
})
