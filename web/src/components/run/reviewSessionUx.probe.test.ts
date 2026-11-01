// @vitest-environment happy-dom
/**
 * Acceptance probes: ClarifyChat reviewMode queue / stream / Cancel
 * (shared by visual + research node-inline review via ReviewComposer).
 */
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyChat from './ClarifyChat.vue'
import { clearDialogues } from '@/lib/chat/dialogueStore'
import { endedTurn, feedDialogue, queuedTurn, runningTurn } from '@/test/dialogueKit'

afterEach(() => clearDialogues())

function mountReview(nodeId: string) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId,
      iteration: 1,
      turns: [],
      done: false,
      active: true,
      reviewMode: true,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
    },
  })
}

function sentIds(wrapper: ReturnType<typeof mountReview>): string[] {
  return (wrapper.emitted('send') ?? []).map((args) => args[3] as string)
}

async function sendText(wrapper: ReturnType<typeof mountReview>, text: string) {
  await wrapper.find('textarea').setValue(text)
  const sendBtn = wrapper.find('[data-testid="clarify-send-icon"], [data-testid="clarify-send-label"]')
  expect(sendBtn.exists()).toBe(true)
  await sendBtn.trigger('click')
  await flushPromises()
}

describe('[approving] ClarifyChat reviewMode UX (visual/research shared)', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('research surface: enqueue shows queue panel; confirm disabled before turn_begin', async () => {
    const w = mountReview('research')
    await sendText(w, '方案意见甲')
    await sendText(w, '方案意见乙')
    const queue = w.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    expect(queue.text()).toMatch(/待发送队列/)
    expect(queue.text()).toContain('方案意见甲')
    expect(queue.text()).toContain('方案意见乙')
    // Busy queue: the single primary is 停止; 确认并流转 is not offered yet.
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
    // No transcript human/agent bubbles until turn_begin (persisted turns empty).
    expect(w.findAll('.rounded-2xl').filter((n) => n.text().includes('方案意见甲')).length).toBe(0)
    w.unmount()
  })

  it('visual surface: turn_begin + acp stream + Cancel clears queue / interrupted', async () => {
    const w = mountReview('visual')
    await sendText(w, '视觉意见1')
    await sendText(w, '视觉意见2')
    const [first, second] = sentIds(w)
    const human = { role: 'human' as const, text: '视觉意见1', at: '1', turnId: 't1', clientMessageId: first }
    const waiter = queuedTurn('t2', 0, { text: '视觉意见2', clientMessageId: second }, { clientMessageId: second })
    const stream = (text: string) => feedDialogue('run-1', 'visual', {
      busy: true,
      rows: [human, { role: 'agent', text, at: '2', turnId: 't1', streaming: true }],
      turns: [runningTurn('t1', { text: '视觉意见1', clientMessageId: first }, { clientMessageId: first }), waiter],
    })
    stream('')
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('视觉意见2')
    expect(w.text()).toContain('视觉意见1')
    stream('增量流式正文')
    await nextTick()
    expect(w.text()).toContain('增量流式正文')
    const cancel = w.find('[data-testid="clarify-review-cancel"]')
    expect(cancel.exists()).toBe(true)
    expect(cancel.attributes('aria-label')).toBe('停止')
    expect(cancel.attributes('title')).toBe('停止')
    expect(cancel.text()).not.toContain('停止')
    expect(cancel.findComponent({ name: 'Icon' }).props('name')).toBe('chat-stop')
    await cancel.trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
    // The server stops the turn and clears the review queue.
    feedDialogue('run-1', 'visual', {
      rows: [human, { role: 'agent', text: '增量流式正文', at: '2', turnId: 't1', interrupted: true }],
      turns: [endedTurn('t1', 'interrupted'), endedTurn('t2', 'dropped', { clientMessageId: second })],
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
    const confirm = w.find('[data-testid="clarify-confirm-flow"]')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
    w.unmount()
  })

  it('enqueue failure: discardLastQueued restores confirm gate (v1)', async () => {
    const w = mountReview('research')
    await sendText(w, '将失败的意见')
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    const vm = w.vm as unknown as { discardLastQueued: () => void }
    vm.discardLastQueued()
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    w.unmount()
  })

  it('remote Cancel: the server dropping the queue clears it here (v2 FR5)', async () => {
    const w = mountReview('visual')
    await sendText(w, '页A意见1')
    await sendText(w, '页A意见2')
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('页A意见1')
    const [first, second] = sentIds(w)
    // Another entry cancelled the review session: both sends were dropped.
    feedDialogue('run-1', 'visual', {
      turns: [endedTurn('a', 'dropped', { clientMessageId: first }), endedTurn('b', 'dropped', { clientMessageId: second })],
    })
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    w.unmount()
  })

  it('ignores the dialogue of another node (v3)', async () => {
    const w = mountReview('research')
    await sendText(w, '本节点意见')
    feedDialogue('run-1', 'other-node', { turns: [endedTurn('x', 'dropped', { clientMessageId: sentIds(w)[0] })] })
    await nextTick()
    // Another node's state must not clear this session's queue.
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('本节点意见')
    w.unmount()
  })

})
