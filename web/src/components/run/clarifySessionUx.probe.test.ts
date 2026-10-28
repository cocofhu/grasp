// @vitest-environment happy-dom
/**
 * Acceptance probes: ClarifyChat non-reviewMode (需求澄清) queue / stream /
 * Cancel keep-queue / refresh resume / IME — aligned with Demo「修复后」.
 */
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyChat from './ClarifyChat.vue'
import AnnotationChip from './AnnotationChip.vue'
import type { ReactAnnotation } from '@/lib/shared/types'

function mountClarify(nodeId = 'clarify') {
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
      reviewMode: false,
      annotateEnabled: true,
      hideFinish: true,
      sendLabel: '发送澄清回复',
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
    },
  })
}

async function sendText(wrapper: ReturnType<typeof mountClarify>, text: string) {
  await wrapper.find('textarea').setValue(text)
  await wrapper.find('[data-testid="clarify-send-label"]').trigger('click')
  await flushPromises()
}

describe('[approving] ClarifyChat clarify-path UX (non-reviewMode)', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('enqueue shows queue; Cancel keeps queue (Demo keep-queue)', async () => {
    const w = mountClarify()
    await sendText(w, '澄清意见甲')
    await sendText(w, '澄清意见乙')
    const queue = w.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    expect(queue.text()).toContain('澄清意见甲')
    expect(queue.text()).toContain('澄清意见乙')

    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({ event: 'turn_begin', item: { text: '澄清意见甲' }, nodeId: 'clarify' })
    await nextTick()
    vm.applyAcpEvents([{ kind: 'message', text: '流式正文增量' }])
    await nextTick()
    expect(w.text()).toContain('流式正文增量')

    await w.find('[data-testid="clarify-review-cancel"]').trigger('click')
    await flushPromises()
    // Demo: queue kept (乙 still pending); interrupted marker on current turn.
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('澄清意见乙')
    expect(w.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
    expect(w.emitted('cancel')).toBeTruthy()
    w.unmount()
  })

  it('real pump order: queue_state(remaining) then turn_begin(active) binds 甲 not 乙', async () => {
    // Server pump: queue_state items=剩余(不含 active) → turn_begin item=当前轮.
    // Both accepted rows stay visible. The live stream binds to 甲; 乙 waits
    // as a user row and is not drawn as an empty failure (review v1).
    const w = mountClarify()
    await sendText(w, '澄清意见甲')
    await sendText(w, '澄清意见乙')
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
    }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 1,
      items: [{ text: '澄清意见乙' }],
      busy: true,
    })
    await nextTick()
    vm.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'clarify',
      item: { text: '澄清意见甲' },
    })
    await nextTick()
    const scroller = w.find('[data-testid="clarify-scroller"]')
    const humans = scroller.findAll('[data-testid="clarify-human-message"]').map((node) => node.text())
    expect(humans.filter((text) => text.includes('澄清意见甲'))).toHaveLength(1)
    expect(humans.filter((text) => text.includes('澄清意见乙'))).toHaveLength(1)
    expect(scroller.find('[data-testid="clarify-empty-fail"]').exists()).toBe(false)
    const body = scroller.text()
    const jia = body.indexOf('澄清意见甲')
    const busy = body.indexOf('思考中')
    const yi = body.indexOf('澄清意见乙')
    expect(jia).toBeGreaterThanOrEqual(0)
    expect(busy).toBeGreaterThan(jia)
    expect(yi).toBeGreaterThan(busy)
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('澄清意见乙')
    expect(w.find('[data-testid="clarify-review-queue"]').text()).not.toContain('澄清意见甲')
    w.unmount()
  })

  it('refresh resume via queue_state busy+activeItem recreates live stream bubble', async () => {
    const w = mountClarify()
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 1,
      items: [{ text: '排队中的下一条' }],
      busy: true,
      activeItem: { text: '刷新前进行中的提问' },
    })
    await nextTick()
    expect(w.text()).toContain('刷新前进行中的提问')
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('排队中的下一条')
    vm.applyAcpEvents([{ kind: 'message', text: '续上的流式正文' }])
    await nextTick()
    expect(w.text()).toContain('续上的流式正文')
    w.unmount()
  })

  /**
   * Screenshot repro (g1.1 / g3.1): Inbox 需求对齐 — persisted human only,
   * session still busy, no agent body → must show 思考中… then resume message.
   */
  it('screenshot: persisted human + busy + no agent → thinking then resume message (g1.1/g3.1)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const humanText = '还是有很多直角按钮和面板啊'
    const w = mount(ClarifyChat, {
      props: {
        runId: 'run-1',
        nodeId: 'clarify',
        iteration: 1,
        turns: [{ role: 'human', text: humanText, at: '2026-09-09T01:00:00Z' }],
        done: false,
        active: true,
        reviewMode: true,
        annotateEnabled: true,
        hideFinish: false,
        sendLabel: '发送回复',
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ClarifyDemoFrame: true },
      },
    })
    await nextTick()
    // Broken UI: header would stay at 共 1 条 with only the human bubble.
    expect(w.text()).toContain(humanText)
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)

    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: humanText },
    })
    await nextTick()
    // Live slot rebuilt despite human already in persisted turns.
    const placeholder = w.find('[data-testid="clarify-busy-placeholder"]')
    expect(placeholder.exists()).toBe(true)
    expect(placeholder.text()).toContain('思考中')
    // Must not stop at 共 1 条 — agent streaming bubble is present.
    const humans = (w.text().match(new RegExp(humanText, 'g')) || []).length
    expect(humans).toBeGreaterThanOrEqual(1)

    vm.applyAcpEvents([{ kind: 'message', text: '好的，我会按圆角口径把按钮和面板改掉…' }])
    await nextTick()
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
    expect(w.text()).toContain('好的，我会按圆角口径把按钮和面板改掉…')
    expect(w.find('[data-testid="clarify-busy-status"]').text()).toContain('输出中')
    expect(w.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
    w.unmount()
  })

  it('hard-refresh seed: thought-only then message after remount (g4.1)', async () => {
    // Simulate host seed-then-live: rebuild slot → seed ACP after (re)mount.
    const w = mountClarify()
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '硬刷新前的提问' },
    })
    await nextTick()
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)

    // Seed thought recovered from LiveNodeEvents / pending ACP buffer.
    vm.applyAcpEvents([{ kind: 'thought', text: '已恢复的思考增量' }])
    await nextTick()
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-thought"]').text()).toContain('已恢复的思考增量')
    expect(w.find('[data-testid="clarify-busy-status"]').text()).toContain('思考中')

    vm.applyAcpEvents([
      { kind: 'thought', text: '已恢复的思考增量' },
      { kind: 'message', text: '续流正文' },
    ])
    await nextTick()
    expect(w.text()).toContain('续流正文')
    expect(w.find('[data-testid="clarify-busy-status"]').text()).toContain('输出中')
    expect(w.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
    w.unmount()
  })

  it('IME composing Enter does not send', async () => {
    const w = mountClarify()
    const ta = w.find('[data-testid="clarify-input"]')
    await ta.setValue('中文输入')
    await ta.trigger('compositionstart')
    await ta.trigger('keydown', { key: 'Enter', keyCode: 229, isComposing: true })
    await flushPromises()
    expect(w.emitted('send')).toBeFalsy()
    await ta.trigger('compositionend')
    await ta.trigger('keydown', { key: 'Enter', keyCode: 13, isComposing: false })
    await flushPromises()
    expect(w.emitted('send')).toBeTruthy()
    w.unmount()
  })

  it('turn_begin prefers queue item id over duplicate text', async () => {
    const w = mountClarify()
    // Two identical texts — text-only match would be ambiguous.
    await sendText(w, '同一文案')
    await sendText(w, '同一文案')
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
    }
    // Authoritative reconcile assigns server ids before turn_begin.
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 2,
      items: [
        { id: 'id-a', text: '同一文案' },
        { id: 'id-b', text: '同一文案' },
      ],
      busy: false,
    })
    await nextTick()
    // Pump: remaining = id-b only, then turn_begin starts id-a (not in queue).
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 1,
      items: [{ id: 'id-b', text: '同一文案' }],
      busy: true,
    })
    await nextTick()
    vm.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'clarify',
      item: { id: 'id-a', text: '同一文案' },
    })
    await nextTick()
    const queue = w.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    // id-a was already trimmed; must NOT steal id-b via text fallback.
    expect(queue.findAll('[data-testid="clarify-queue-item"]').length).toBe(1)
    expect(queue.text()).toContain('同一文案')
    w.unmount()
  })

  it('turn_begin removes matching id from local queue when still present', async () => {
    const w = mountClarify()
    await sendText(w, '同一文案')
    await sendText(w, '同一文案')
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
    }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'clarify',
      waiting: 2,
      items: [
        { id: 'id-a', text: '同一文案' },
        { id: 'id-b', text: '同一文案' },
      ],
      busy: false,
    })
    await nextTick()
    // turn_begin before trim (id still in local queue) — remove by id.
    vm.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'clarify',
      item: { id: 'id-a', text: '同一文案' },
    })
    await nextTick()
    expect(w.findAll('[data-testid="clarify-queue-item"]').length).toBe(1)
    w.unmount()
  })

  function mountChip(ann: ReactAnnotation, extra: { removable?: boolean; testId?: string } = {}) {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    return mount(AnnotationChip, {
      props: { ann, ...extra },
      global: { plugins: [i18n], stubs: { Icon: true } },
    })
  }

  it('AnnotationChip path===label shows once', () => {
    const ann: ReactAnnotation = { label: '#launchInput', selector: '#launchInput' }
    const w = mountChip(ann, { testId: 'chip-dup' })
    const text = w.text()
    const matches = text.match(/#launchInput/g) || []
    expect(matches.length).toBe(1)
    const title = w.attributes('title') || ''
    expect(title).toContain('整字段')
    expect(title.match(/#launchInput/g) || []).toHaveLength(1)
    expect(w.attributes('aria-label')).toBe(title)
    w.unmount()
  })

  it('AnnotationChip path!==label shows path in body and label in title', () => {
    const ann: ReactAnnotation = { label: '启动输入', jsonPath: '#launchInput' }
    const w = mountChip(ann)
    expect(w.text()).toContain('#launchInput')
    expect(w.text()).not.toContain('启动输入')
    expect(w.text()).not.toContain('整字段')
    const title = w.attributes('title') || ''
    expect(title).toContain('#launchInput')
    expect(title).toContain('启动输入')
    expect(title).toContain('整字段')
    expect(w.attributes('aria-label')).toBe(title)
    expect(w.find('.font-mono').exists()).toBe(true)
    w.unmount()
  })

  it('AnnotationChip renders field, quote, and unbound as one neutral line', () => {
    const field = mountChip({ label: '启动输入', jsonPath: '#launchInput' })
    expect(field.attributes('data-chip-kind')).toBe('field')
    expect(field.classes()).toEqual(expect.arrayContaining(['chip', 'max-w-full', 'min-w-0']))
    expect(field.classes().join(' ')).not.toMatch(/accent|border-ok|bg-ok/)
    expect(field.html()).not.toMatch(/uppercase|text-\[9px\]/)
    expect(field.html()).toContain('crosshair')
    expect(field.find('.truncate').exists()).toBe(true)
    expect(field.text()).not.toContain('整字段')
    field.unmount()

    const labelOnly = mountChip({ label: '仅标签' })
    expect(labelOnly.attributes('data-chip-kind')).toBe('field')
    expect(labelOnly.text()).toContain('仅标签')
    expect(labelOnly.find('.font-mono').exists()).toBe(false)
    expect((labelOnly.attributes('title') || '').match(/仅标签/g) || []).toHaveLength(1)
    labelOnly.unmount()

    const quote = mountChip({
      label: 'Hero',
      jsonPath: 'summary',
      selector: '#hero',
      quote: '第一行\n第二行',
    })
    expect(quote.attributes('data-chip-kind')).toBe('quote')
    expect(quote.text()).toContain('第一行 第二行')
    expect(quote.text()).not.toContain('段落摘录')
    expect(quote.text()).not.toContain('summary')
    expect(quote.text()).not.toContain('Hero')
    expect(quote.text()).toContain('❝')
    expect(quote.find('.font-mono').exists()).toBe(false)
    const quoteTitle = quote.attributes('title') || ''
    expect(quoteTitle).toContain('段落摘录')
    expect(quoteTitle).toContain('第一行 第二行')
    expect(quoteTitle).toContain('summary')
    expect(quoteTitle).toContain('Hero')
    expect(quote.attributes('aria-label')).toBe(quoteTitle)
    quote.unmount()

    const unbound = mountChip({ quote: '未绑定原文', label: '未绑定原文' })
    expect(unbound.attributes('data-chip-kind')).toBe('unbound')
    expect(unbound.text().match(/未绑定原文/g) || []).toHaveLength(1)
    expect(unbound.text()).not.toContain('未绑定字段')
    const unboundTitle = unbound.attributes('title') || ''
    expect(unboundTitle).toContain('未绑定字段')
    expect(unboundTitle.match(/未绑定原文/g) || []).toHaveLength(1)
    expect(unbound.find('.font-mono').exists()).toBe(false)
    unbound.unmount()
  })

  it('AnnotationChip removable toggle only shows remove on pending chips', async () => {
    const ann: ReactAnnotation = { label: '字段', selector: '#field' }
    const pending = mountChip(ann, { removable: true })
    const remove = pending.get('button')
    expect(remove.attributes('aria-label')).toBe('移除引用')
    await remove.trigger('click')
    expect(pending.emitted('remove')).toHaveLength(1)
    pending.unmount()

    const readonly = mountChip(ann)
    expect(readonly.find('button').exists()).toBe(false)
    readonly.unmount()
  })

  it('AnnotationChip ellipsizes long text and keeps soft truncation in the title', () => {
    const longPath = 'selector.'.repeat(20) + 'end'
    const long = mountChip({ jsonPath: longPath, label: longPath })
    expect(long.get('.truncate').text()).toBe(longPath)
    expect(long.find('.truncate').classes()).toContain('truncate')
    expect((long.attributes('title') || '').split(longPath).length - 1).toBe(1)
    long.unmount()

    const quote = '已软截断的摘录原文'
    const cut = mountChip({ quote, jsonPath: 'body', truncated: true })
    expect(cut.text()).toContain(quote)
    expect(cut.text()).not.toContain('已截断')
    expect(cut.html()).not.toContain('chipTruncated')
    const title = cut.attributes('title') || ''
    expect(title).toContain('段落摘录')
    expect(title).toContain(quote)
    expect(title).toContain('body')
    expect(title).toContain('已截断')
    expect(cut.attributes('aria-label')).toContain('已截断')
    cut.unmount()
  })

  it('props.turns catching human while live streaming does not double human bubble', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(ClarifyChat, {
      props: {
        runId: 'run-1',
        nodeId: 'clarify',
        iteration: 1,
        turns: [],
        done: false,
        active: true,
        reviewMode: false,
        annotateEnabled: true,
        hideFinish: true,
        sendLabel: '发送澄清回复',
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ClarifyDemoFrame: true },
      },
    })
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({
      event: 'turn_begin',
      item: { text: '澄清意见甲' },
      nodeId: 'clarify',
    })
    await nextTick()
    vm.applyAcpEvents([{ kind: 'message', text: '流式正文' }])
    await nextTick()
    // Host softRefresh/loadRun caught up with persisted human mid-stream.
    await w.setProps({
      turns: [{ role: 'human', text: '澄清意见甲', at: '2026-07-27T00:00:00Z' }],
    })
    await nextTick()
    const body = w.find('[data-testid="clarify-scroller"]').text()
    const matches = body.match(/澄清意见甲/g) || []
    expect(matches.length).toBe(1)
    expect(body).toContain('流式正文')
    w.unmount()
  })

  it('queue row cancel removes item without confirm dialog', async () => {
    const w = mountClarify()
    await sendText(w, '甲')
    await sendText(w, '乙')
    expect(w.findAll('[data-testid="clarify-queue-item"]').length).toBe(2)
    await w.find('[data-testid="clarify-queue-cancel"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="clarify-queue-item"]').length).toBe(1)
    expect(w.find('[data-testid="clarify-queue-item"]').text()).toContain('乙')
    expect(w.emitted('queue-remove')).toBeFalsy()
    w.unmount()
  })

  it('edit refills composer; draft with chips is stashed onto queue', async () => {
    const w = mountClarify()
    await sendText(w, '排队原文')
    await w.find('textarea').setValue('未发送草稿')
    await w.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    // Draft stashed onto queue; queued item refilled into composer.
    expect(w.find('textarea').element.value).toBe('排队原文')
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(w.find('[data-testid="clarify-queue-item"]').text()).toContain('未发送草稿')
    w.unmount()
  })

  it('edit refills annotation chips from queued item (plan g1.3)', async () => {
    const anns: ReactAnnotation[] = [
      { label: 'Hero', selector: '#hero', jsonPath: 'summary', quote: '摘录' },
    ]
    const w = mount(ClarifyChat, {
      props: {
        runId: 'run-1',
        nodeId: 'clarify',
        iteration: 1,
        turns: [],
        done: false,
        active: true,
        reviewMode: false,
        annotateEnabled: true,
        hideFinish: true,
        sendLabel: '发送澄清回复',
        draft: '带标注排队',
        annotations: anns,
      },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'zh-CN',
            messages: { 'zh-CN': { ...common, ...pages } },
          }),
        ],
        stubs: { Icon: true, ClarifyDemoFrame: true },
      },
    })
    await w.find('[data-testid="clarify-send-label"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toMatch(/批注/)
    await w.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(w.find('textarea').element.value).toBe('带标注排队')
    // Composer chips restored (AnnotationChip renders path/quote; label may be omitted when quote set).
    const chip = w.find('[data-testid="clarify-annotation-chip"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toMatch(/summary|摘录|Hero/)
    expect(w.emitted('queue-remove')).toBeFalsy() // local ghost has no server id
    w.unmount()
  })

  it('applyQueueState prefers frame annotations without local hit (plan g1.2)', async () => {
    const w = mountClarify()
    const vm = w.vm as unknown as {
      applyQueueState: (
        waiting: number,
        items: {
          id?: string
          text?: string
          images?: { data: string; mimeType: string }[]
          annotations?: ReactAnnotation[]
        }[] | null,
        busy?: boolean,
        activeItem?: unknown,
      ) => void
    }
    vm.applyQueueState(
      1,
      [
        {
          id: 'q-frame-1',
          text: '仅帧权威',
          annotations: [{ label: '字段', jsonPath: 'goals[0]', selector: '.goal' }],
          images: [],
        },
      ],
      false,
      null,
    )
    await nextTick()
    const queue = w.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    expect(queue.text()).toContain('仅帧权威')
    expect(queue.text()).toMatch(/批注/)
    await w.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(w.find('textarea').element.value).toBe('仅帧权威')
    const restored = w.find('[data-testid="clarify-annotation-chip"]')
    expect(restored.text()).toContain('goals[0]')
    expect(restored.attributes('title')).toContain('字段')
    w.unmount()
  })

  it('edit with composer chips stashes them; queued chips refill (plan g2.1)', async () => {
    const queuedAnns: ReactAnnotation[] = [{ label: '队列点', selector: '#queued' }]
    const draftAnns: ReactAnnotation[] = [{ label: '草稿点', selector: '#draft' }]
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(ClarifyChat, {
      props: {
        runId: 'run-1',
        nodeId: 'clarify',
        iteration: 1,
        turns: [],
        done: false,
        active: true,
        reviewMode: false,
        annotateEnabled: true,
        hideFinish: true,
        sendLabel: '发送澄清回复',
        draft: '队列正文',
        annotations: queuedAnns,
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true, ClarifyDemoFrame: true },
      },
    })
    await w.find('[data-testid="clarify-send-label"]').trigger('click')
    await flushPromises()
    // Put new chips into composer while queue holds the first item.
    await w.setProps({ draft: '新草稿', annotations: draftAnns })
    await nextTick()
    await w.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    // Composer has queued item chips; draft chips live on the queue row.
    expect(w.find('textarea').element.value).toBe('队列正文')
    const restored = w.find('[data-testid="clarify-annotation-chip"]')
    expect(restored.text()).toContain('#queued')
    expect(restored.attributes('title')).toContain('队列点')
    expect(w.find('[data-testid="clarify-queue-item"]').text()).toContain('新草稿')
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toMatch(/批注/)
    w.unmount()
  })

  it('drag reorder updates visible order', async () => {
    const w = mountClarify()
    await sendText(w, '第一条')
    await sendText(w, '第二条')
    const vm = w.vm as unknown as { reorderQueuedItems: (from: number, to: number) => void }
    vm.reorderQueuedItems(0, 1)
    await nextTick()
    const rows = w.findAll('[data-testid="clarify-queue-item"]')
    expect(rows[0]?.text()).toContain('第二条')
    expect(rows[1]?.text()).toContain('第一条')
    w.unmount()
  })
})
