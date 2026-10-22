// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyHandoffBubble, { type ClarifyNodeAsk } from './ClarifyHandoffBubble.vue'

function mountAsk(ask: ClarifyNodeAsk) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyHandoffBubble, {
    props: { ask },
    global: { plugins: [i18n] },
  })
}

describe('ClarifyHandoffBubble markdown', () => {
  it('renders handoff headings and lists; source line stays metadata', () => {
    const w = mountAsk({
      kind: 'handoff',
      from: '测试评审',
      source: 'agent',
      text: '测试评审通过。\n\n## 上游交接\n\n- 标题层级\n- 列表',
    })
    expect(w.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「测试评审」的交接说明')
    const body = w.get('[data-testid="clarify-incoming-handoff-text"]')
    expect(body.classes()).toContain('chat-prose')
    expect(body.text()).toContain('上游交接')
    expect(body.findAll('li').map((li) => li.text())).toEqual(['标题层级', '列表'])
    expect(body.text()).not.toContain('##')
    expect(w.get('[data-testid="clarify-incoming-handoff"]').attributes('data-source')).toBe('agent')
    w.unmount()
  })

  it('renders a tight ## heading in the handoff body', () => {
    const w = mountAsk({
      kind: 'handoff',
      from: '测试评审',
      source: 'platform',
      text: '##上游交接\n\n走 pass 给交付。',
    })
    expect(w.get('[data-testid="clarify-incoming-handoff-from"]').text()).toBe('「测试评审」未留交接，平台代写')
    const body = w.get('[data-testid="clarify-incoming-handoff-text"]')
    expect(body.text()).toContain('上游交接')
    expect(body.text()).toContain('走 pass 给交付')
    expect(body.text()).not.toContain('##')
    w.unmount()
  })

  it('renders the node task bubble through the same user markdown path', () => {
    const w = mountAsk({
      kind: 'task',
      text: '## 节点任务\n\n1. 合入分支\n2. 写 MR',
    })
    const bubble = w.get('[data-testid="clarify-node-task"]')
    expect(bubble.attributes('data-turn-role')).toBe('human')
    const msg = bubble.get('.chat-user-message')
    expect(msg.text()).toContain('节点任务')
    expect(msg.findAll('li').map((li) => li.text())).toEqual(['合入分支', '写 MR'])
    expect(msg.text()).not.toContain('##')
    w.unmount()
  })

  it('shows raw HTML tags in handoff text and does not execute them', () => {
    const w = mountAsk({
      kind: 'handoff',
      from: '编码',
      source: 'agent',
      text: '说明：<script>关键步骤</script>',
    })
    const body = w.get('[data-testid="clarify-incoming-handoff-text"]')
    expect(body.text()).toContain('关键步骤')
    expect(body.text()).toContain('<script>')
    expect(body.element.querySelector('script')).toBeNull()
    w.unmount()
  })
})
