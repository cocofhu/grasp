// @vitest-environment happy-dom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import ClarifyChat from './ClarifyChat.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyTurn } from '@/lib/shared/types'
import { clearDialogues } from '@/lib/chat/dialogueStore'
import { feedDialogue, runningTurn } from '@/test/dialogueKit'

afterEach(() => clearDialogues())

enableAutoUnmount(afterEach)
function createChat(turns: ClarifyTurn[] = []) {
  return mount(ClarifyChat, {
    props: { runId: 'run-1', nodeId: 'node-1', iteration: 1, done: false, turns },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })] },
  })
}

describe('Clarify conversation navigation', () => {
  it('previews only the last visible reply in each question, including its complete Markdown paragraphs', async () => {
    const final = '当前输入无法当作有效需求对齐：请确认**本次目标**。\n\n请补充现象与期望，我再继续澄清。'
    const turns: ClarifyTurn[] = [
      { role: 'human', text: '检查需求', at: 'question' },
      { role: 'agent', text: '先查看已有产物。', at: 'progress' },
      { role: 'agent', text: `过程拼接${final}`, at: 'answer', parts: [
        { kind: 'message', text: '正在检查历史反馈。' },
        { kind: 'tool', title: '读取文件' },
        { kind: 'message', text: final },
      ] },
      { role: 'agent', text: '', thought: '后续思考', tools: [{ title: '只含工具' }], at: 'tool' },
      { role: 'human', text: '另一个问题', at: 'next' },
    ]
    const wrapper = createChat(turns)
    await flushPromises()
    const rail = wrapper.getComponent(ChatMessageRail)
    expect(rail.props('items').map(item => item.preview)).toEqual([final, ''])
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector('[data-testid="chat-message-rail-card"]')!
    expect(tooltip.textContent).toContain('当前输入无法当作有效需求对齐：请确认本次目标。')
    expect(tooltip.textContent).toContain('请补充现象与期望，我再继续澄清。')
    expect(tooltip.textContent).not.toContain('正在检查历史反馈')
    expect(tooltip.textContent).not.toContain('先查看已有产物')
    const id = rail.props('items')[0]!.id
    await wrapper.setProps({ turns: [...turns, { role: 'agent', text: '正在生成', at: 'live', streaming: true }] })
    expect(rail.props('items')[0]!.id).toBe(id)
    expect(rail.props('items').map(item => item.preview)).toEqual([final, '正在生成'])
    await wrapper.setProps({ turns: [...turns, { role: 'agent', text: '生成完成。\n\n下一步请验证。', at: 'live' }] })
    expect(rail.props('items').at(-1)?.preview).toBe('生成完成。\n\n下一步请验证。')
    await wrapper.setProps({ turns: [...turns, { role: 'agent', text: ' \n', at: 'choice', questions: [{ id: 'module', prompt: '请选择缺陷所在功能模块', options: [] }] }] })
    expect(rail.props('items').at(-1)?.preview).toBe('请选择缺陷所在功能模块')
  })

  it('previews Markdown as readable text while the full assistant reply retains its formatting', async () => {
    const wrapper = createChat([
      { role: 'human', text: '我的选择：\n- 修缺陷', at: 'today' },
      { role: 'agent', text: '已记下：本次是**修缺陷**。\n\n- 选择功能模块\n- 查看 `feedback_index.json`', at: 'reply' },
    ])
    await flushPromises()
    await wrapper.get('[data-testid="chat-message-rail-mark"]').trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector('[data-testid="chat-message-rail-card"]')!
    expect(tooltip.querySelector('.is-pointed')?.textContent?.trim()).toBe('我的选择： 修缺陷')
    expect(tooltip.querySelector('.chat-message-rail-preview')?.textContent).toBe('已记下：本次是修缺陷。 选择功能模块 查看 feedback_index.json')
    expect(tooltip.textContent).not.toContain('**')
    expect(wrapper.get('[data-turn-role="agent"] strong').text()).toBe('修缺陷')
  })

  it('indexes user questions and keeps their real DOM anchors through cloned and prepended history', async () => {
    const first: ClarifyTurn[] = [
      { role: 'human', text: '检查发布', at: 'today' },
      { role: 'agent', text: '发布已验证', at: 'reply' },
    ]
    const wrapper = createChat(first)
    await flushPromises()
    const rail = wrapper.getComponent(ChatMessageRail)
    const id = rail.props('items')[0]!.id
    expect(rail.props('items')).toEqual([{ id, title: '检查发布', preview: '发布已验证' }])
    expect(wrapper.get(`[data-chat-message-id="${id}"]`).attributes('data-chat-anchor')).toBe(id)
    const reply = wrapper.get('[data-turn-role="agent"]').element
    await wrapper.setProps({ turns: [
      { role: 'human', text: '更早的问题', at: 'yesterday' },
      { role: 'agent', text: '更早的回答', at: 'earlier' },
      ...first.map(turn => ({ ...turn })),
    ] })
    await flushPromises()
    expect(rail.props('items')[1]!.id).toBe(id)
    expect(wrapper.get(`[data-chat-message-id="${id}"]`).text()).toContain('检查发布')
    expect(wrapper.findAll('[data-turn-role="agent"]')[1]!.element).toBe(reply)
    await wrapper.setProps({ iteration: 2 })
    expect(rail.props('scopeKey')).toBe('run-1:node-1:2')
    expect(rail.props('items').some(item => item.id === id)).toBe(false)
  })

  it('pauses automatic following before navigation so streamed output does not pull the reader to the bottom', async () => {
    const wrapper = createChat()
    const human = { role: 'human' as const, text: '请检查发布', at: '1', turnId: 't1' }
    const reply = (text: string) => feedDialogue('run-1', 'node-1', {
      busy: true,
      rows: [human, { role: 'agent', text, at: '2', turnId: 't1', streaming: true }],
      turns: [runningTurn('t1', { text: '请检查发布' })],
    })
    reply('')
    await flushPromises()
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element as HTMLElement
    Object.defineProperties(scroller, { scrollHeight: { configurable: true, value: 1400 }, clientHeight: { configurable: true, value: 400 } })
    scroller.scrollTop = 200
    const rail = wrapper.getComponent(ChatMessageRail)
    rail.vm.$emit('navigate', rail.props('items')[0]!.id)
    reply('正在继续验证输出')
    await flushPromises()
    expect(wrapper.text()).toContain('正在继续验证输出')
    expect(scroller.scrollTop).toBe(200)
    expect(wrapper.get('[data-testid="clarify-scroller"]').attributes()).toHaveProperty('data-conversation-scroll')
  })
})

describe('Clarify conversation navigation across earlier nodes', () => {
  it('shows the rail for earlier visits after a hand-off and opens the fold on navigate', async () => {
    const wrapper = mount(ClarifyChat, {
      props: {
        runId: 'run-1', nodeId: 'test', iteration: 1, done: false, turns: [],
        priorSegments: [{
          nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: '2026-10-14T00:00:00Z', open: false,
          turns: [
            { role: 'human', text: '做个导出', at: '2026-10-14T00:00:00Z' },
            { role: 'agent', text: '需求已整理', at: '2026-10-14T00:00:10Z' },
          ],
        }],
      },
      attachTo: document.body,
      global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })] },
    })
    await flushPromises()
    const rail = wrapper.getComponent(ChatMessageRail)
    expect(rail.props('items')).toEqual([{ id: 'prior:clarify:1:0', title: '做个导出', preview: '需求已整理' }])
    expect(wrapper.find('[data-testid="chat-message-rail"]').exists()).toBe(true)
    const fold = wrapper.get('[data-testid="clarify-prior-segment"]').element as HTMLDetailsElement
    expect(fold.open).toBe(false)
    expect(fold.querySelector('[data-chat-message-id="prior:clarify:1:0"]')).not.toBeNull()
    rail.vm.$emit('navigate', 'prior:clarify:1:0')
    expect(fold.open).toBe(true)
  })
})
