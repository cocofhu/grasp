// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ReviewComposer from './ReviewComposer.vue'

function mountGate(opts: {
  thinking?: boolean
  streamText?: string
  streamThought?: string
  interrupted?: boolean
  streamCompletedAt?: string | null
} = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ReviewComposer, {
    props: {
      mode: 'gate',
      thinking: opts.thinking ?? false,
      streamText: opts.streamText ?? '',
      streamThought: opts.streamThought ?? '',
      interrupted: opts.interrupted ?? false,
      streamCompletedAt: opts.streamCompletedAt ?? null,
      canReject: true,
      canPass: true,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyChat: true },
    },
  })
}

/** Clarify mode with real ClarifyChat — for pass-through applyAcpEvents contract. */
function mountClarify(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ReviewComposer, {
    props: {
      mode: 'clarify',
      runId: 'run-1',
      nodeId: 'react-1',
      iteration: 1,
      turns: [],
      done: false,
      active: true,
      ...extra,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyDemoFrame: true },
    },
  })
}

describe('ReviewComposer gate busy status (C-tier)', () => {
  it('thinking with empty stream shows 思考中… placeholder (no air bubble)', async () => {
    const wrapper = mountGate({ thinking: true })
    await flushPromises()
    const placeholder = wrapper.find('[data-testid="gate-busy-placeholder"]')
    expect(wrapper.find('[data-testid="gate-react-stream"]').exists()).toBe(true)
    expect(placeholder.exists()).toBe(true)
    expect(placeholder.text()).toContain('思考中')
    expect(placeholder.find('.typing-dots').exists()).toBe(true)
    wrapper.unmount()
  })

  it('thought is visible and default-open', async () => {
    const wrapper = mountGate({
      thinking: true,
      streamThought: '旁路思考过程',
    })
    await flushPromises()
    const thought = wrapper.find('[data-testid="gate-react-thought"]')
    expect(thought.exists()).toBe(true)
    expect(thought.attributes('open')).toBeDefined()
    expect(thought.text()).toContain('旁路思考过程')
    expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
      'streaming',
    )
    expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('生成中')
    expect(wrapper.find('[data-testid="gate-busy-status"]').text()).toContain('思考中')
    wrapper.unmount()
  })

  // plan coverage: g2.1 — GateReactStreamPanel renders full Thought, no secondary truncate
  it('renders full long thought without truncated suffix (g2.1)', async () => {
    const longThought = `${'思'.repeat(2000)}结尾标记END`
    expect(new TextEncoder().encode(longThought).length).toBeGreaterThan(4000)
    const wrapper = mountGate({
      thinking: true,
      streamThought: longThought,
    })
    await flushPromises()
    const thoughtBody = wrapper.find('[data-testid="gate-react-thought"] .whitespace-pre-wrap')
    expect(thoughtBody.exists()).toBe(true)
    expect(thoughtBody.text()).toBe(longThought)
    expect(thoughtBody.text()).toContain('结尾标记END')
    expect(thoughtBody.text()).not.toContain('…(truncated)')
    expect(thoughtBody.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)
    expect(thoughtBody.classes()).toContain('break-words')
    wrapper.unmount()
  })

  it('message switches status to 输出中, collapses thought, shows caret', async () => {
    const wrapper = mountGate({
      thinking: true,
      streamThought: '旁路思考',
      streamText: '旁路正文流',
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-status"]').text()).toContain('输出中')
    expect(wrapper.find('[data-testid="gate-react-thought"]').text()).toContain('旁路思考')
    expect(wrapper.find('[data-testid="gate-react-thought"]').attributes('open')).toBeUndefined()
    // Message outputting — summary stays generating (not done).
    expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
      'streaming',
    )
    expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('生成中')
    expect(wrapper.find('[data-testid="gate-stream-caret"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('旁路正文流')
    wrapper.unmount()
  })

  it('thinking with neither text nor thought still shows placeholder (tool-dense safe)', async () => {
    const wrapper = mountGate({ thinking: true, streamText: '', streamThought: '' })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(true)
    expect(wrapper.text()).not.toMatch(/正在调用工具|读文件/)
    wrapper.unmount()
  })

  it('completed footnote shows without caret; interrupted never shows 已完成', async () => {
    const done = mountGate({
      thinking: false,
      streamThought: '思考',
      streamText: '正文',
      streamCompletedAt: new Date().toISOString(),
    })
    await flushPromises()
    expect(done.find('[data-testid="gate-turn-completed"]').exists()).toBe(true)
    expect(done.find('[data-testid="gate-turn-completed"]').text()).toContain('已完成')
    expect(done.find('[data-testid="gate-stream-caret"]').exists()).toBe(false)
    expect(done.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('done')
    expect(done.find('[data-testid="thought-summary-state"]').text()).toContain('已完成')
    done.unmount()

    const bad = mountGate({
      thinking: false,
      streamThought: '半截思考',
      streamText: '半截',
      interrupted: true,
      streamCompletedAt: null,
    })
    await flushPromises()
    expect(bad.find('[data-testid="gate-turn-completed"]').exists()).toBe(false)
    expect(bad.text()).toContain('interrupted')
    expect(bad.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
      'interrupted',
    )
    expect(bad.find('[data-testid="thought-summary-state"]').text()).toContain('已中断')
    expect(bad.find('[data-testid="thought-summary-state"]').text()).not.toContain('已完成')
    bad.unmount()
  })

  // g2.1: failure body + interrupted shares cancel UI; never Done/已完成.
  it('revise failure stream text + interrupted never shows 已完成/Done', async () => {
    const fail = mountGate({
      thinking: false,
      streamThought: '半截思考',
      streamText: '(复审修改失败:acp chat idle timeout after 10m0s)',
      interrupted: true,
      streamCompletedAt: null,
    })
    await flushPromises()
    expect(fail.text()).toContain('复审修改失败')
    expect(fail.find('[data-testid="gate-turn-completed"]').exists()).toBe(false)
    expect(fail.text()).not.toMatch(/\bDone\b/)
    expect(fail.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
      'interrupted',
    )
    expect(fail.find('[data-testid="thought-summary-state"]').text()).toContain('已中断')
    expect(fail.find('[data-testid="thought-summary-state"]').text()).not.toContain('已完成')
    fail.unmount()
  })

  it('idle (not thinking, no completed) hides stream panel', async () => {
    const wrapper = mountGate({
      thinking: false,
      streamText: '残留',
      streamThought: '残留思考',
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-stream"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('ReviewComposer nested ClarifyChat delivery', () => {
  it('applyAcpEvents returns false when ClarifyChat not mounted (gate mode)', async () => {
    const wrapper = mountGate({ thinking: true })
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.applyAcpEvents?.([{ kind: 'message', text: 'x' }])).toBe(false)
    expect(vm.isChatReady?.()).toBe(false)
    wrapper.unmount()
  })

  it('applyAcpEvents passes through false when inner slot not ready (g1.3)', async () => {
    // Clarify mode mounts ClarifyChat; without queue_state/turn_begin, slot missing.
    const wrapper = mountClarify()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.isChatReady?.()).toBe(true)
    // chatRef exists but liveAgentIdx < 0 → must not fake true.
    expect(vm.applyAcpEvents?.([{ kind: 'thought', text: 'buffer me' }], 'react-1')).toBe(false)
    wrapper.unmount()
  })

  it('exposes isSessionBusy from nested ClarifyChat (g1.3)', async () => {
    const wrapper = mountClarify()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(typeof vm.isSessionBusy).toBe('function')
    expect(vm.isSessionBusy()).toBe(false)
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'react-1',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '还是有很多直角按钮和面板啊' },
    })
    await flushPromises()
    expect(vm.isSessionBusy()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('ReviewComposer gate review semantics (send + confirm)', () => {
  it('shows 发送 + 确认并流转, not 打回/通过', async () => {
    const wrapper = mountGate()
    await flushPromises()
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-pass"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-send"]').text()).toContain('发送')
    expect(wrapper.find('[data-testid="review-composer-pass"]').text()).toContain('确认并流转')
    expect(wrapper.text()).not.toContain('打回修改')
    expect(wrapper.text()).not.toContain('通过并流转')
    wrapper.unmount()
  })

  it('cold session unmounts input/send and omits cold note/hint; confirm remains', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(ReviewComposer, {
      props: {
        mode: 'gate',
        canReject: false,
        canPass: true,
        coldSession: true,
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyChat: true },
      },
    })
    await flushPromises()
    // plan g4.2: cold silent — no note/hint, no ParagraphInput/send (incl. disabled)
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-pass"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-pass"]').text()).toContain('确认并流转')
    expect(wrapper.find('[data-testid="review-composer-cold-note"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-footer-hint"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'ParagraphInput' }).exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/ReAct|热会话|就地改|恢复热会话|无法继续就地改码/)
    wrapper.unmount()
  })

  it('hot session keeps send and has no cold note', async () => {
    const wrapper = mountGate()
    await flushPromises()
    // plan g4.2: hot path — send present, no cold note
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-pass"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-cold-note"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-footer-hint"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-footer-hint"]').classes().join(' ')).toContain(
      '[overflow-wrap:anywhere]',
    )
    // g2.1 walkthrough: gate action row already flex-wrap; send/confirm min-w-0 so they
    // wrap instead of colliding with ParagraphInput (buttons stay below input).
    expect(wrapper.findComponent({ name: 'ParagraphInput' }).exists()).toBe(true)
    const send = wrapper.find('[data-testid="review-composer-send"]')
    expect(send.classes().join(' ')).not.toMatch(/\bflex-1\b/)
    expect(wrapper.get('[data-testid="composer-shell-box"]').find('[data-testid="review-composer-send"]').exists()).toBe(
      true,
    )
    expect(wrapper.get('[data-testid="composer-shell-footer"]').find('[data-testid="review-composer-pass"]').exists()).toBe(
      true,
    )
    expect(wrapper.get('[data-testid="review-composer-pass"]').classes().join(' ')).toContain('h-9')
    wrapper.unmount()
  })

  it('emits send/finish only (no reject/pass dual-channel)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(ReviewComposer, {
      props: {
        mode: 'gate',
        canReject: true,
        canPass: true,
        rejectAllowEmpty: true,
        draft: '',
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyChat: true },
      },
    })
    await flushPromises()
    await w.find('[data-testid="review-composer-send"]').trigger('click')
    expect(w.emitted('send')).toBeTruthy()
    expect(w.emitted('reject')).toBeFalsy()
    await w.find('[data-testid="review-composer-pass"]').trigger('click')
    expect(w.emitted('finish')).toBeTruthy()
    expect(w.emitted('pass')).toBeFalsy()
    w.unmount()
  })
})

describe('ReviewComposer share panel entry removed', () => {
  it('does not render Agent-area open-share entry (plan g1.1 / g3.1)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(ReviewComposer, {
      props: {
        mode: 'review',
        runId: 'run-1',
        nodeId: 'app_preview',
        iteration: 1,
        turns: [],
        done: false,
        active: true,
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ParagraphInput: true, AnnotationChip: true, ClarifyChat: true },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="review-composer-share-panel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-open-share"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clarify mode also has no Agent-area share entry', async () => {
    const wrapper = mountClarify()
    await flushPromises()
    expect(wrapper.find('[data-testid="review-composer-share-panel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-open-share"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('classic clarify hides 确认并流转; Grasp and approve alias show it', async () => {
    const classic = mountClarify({ nodeType: 'react' })
    await flushPromises()
    expect(classic.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    classic.unmount()

    const grasp = mountClarify({ nodeType: 'grasp', nodeId: 'grasp_1' })
    await flushPromises()
    expect(grasp.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(grasp.find('[data-testid="clarify-confirm-flow"]').text()).toContain('确认并流转')
    grasp.unmount()

    const approve = mountClarify({ nodeType: 'approve', nodeId: 'ap' })
    await flushPromises()
    expect(approve.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(approve.find('[data-testid="clarify-confirm-flow"]').text()).toContain('确认并流转')
    approve.unmount()
  })
})
