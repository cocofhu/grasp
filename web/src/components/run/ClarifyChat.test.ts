// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import type { ClarifyTurn, ReactAnnotation } from '@/lib/shared/types'
import ClarifyChat from './ClarifyChat.vue'

beforeAll(async () => {
  // relTime() reads global i18n; load zh-CN so completion footer shows「刚刚」
  const zh = await loadLocaleMessages('zh-CN')
  i18n.global.setLocaleMessage('zh-CN', zh)
  i18n.global.locale.value = 'zh-CN'
})

function mountChat(opts: {
  turns?: ClarifyTurn[] | null
  done?: boolean
  active?: boolean
  draft?: string
  reviewMode?: boolean
  nodeType?: string
  confirmError?: string | null
  annotateEnabled?: boolean
  annotations?: ReactAnnotation[]
  attachments?: { data: string; mimeType: string; name?: string }[]
  seedHumanText?: string
  seedHumanImages?: { data: string; mimeType: string; name?: string }[]
  hideFinish?: boolean
  sendLabel?: string
} = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId: 'react-1',
      iteration: 1,
      turns: opts.turns === undefined ? [] : opts.turns,
      done: opts.done ?? false,
      active: opts.active ?? true,
      draft: opts.draft ?? '',
      reviewMode: opts.reviewMode ?? false,
      nodeType: opts.nodeType ?? '',
      confirmError: opts.confirmError ?? null,
      annotateEnabled: opts.annotateEnabled ?? false,
      annotations: opts.annotations ?? [],
      attachments: opts.attachments ?? [],
      seedHumanText: opts.seedHumanText ?? '',
      seedHumanImages: opts.seedHumanImages ?? [],
      hideFinish: opts.hideFinish ?? false,
      sendLabel: opts.sendLabel,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        ClarifyDemoFrame: true,
        AppModal: {
          props: ['open', 'title', 'width'],
          emits: ['close'],
          template: `
            <div v-if="open" data-testid="clarify-image-preview-modal">
              <div data-testid="clarify-image-preview-title">{{ title }}</div>
              <button type="button" data-testid="clarify-image-preview-close" @click="$emit('close')">×</button>
              <button type="button" data-testid="clarify-image-preview-backdrop" @click="$emit('close')">backdrop</button>
              <slot />
            </div>
          `,
        },
      },
    },
  })
}

async function clickSend(wrapper: ReturnType<typeof mountChat>) {
  const sendBtn = wrapper.find('button[class*="bg-accent"]')
  expect(sendBtn.exists()).toBe(true)
  await sendBtn.trigger('click')
  await flushPromises()
}

describe('ClarifyChat', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.unstubAllGlobals()
  })

  it('renders turns and composer on active dialogue', () => {
    const turns: ClarifyTurn[] = [
      { role: 'agent', text: '你好，请描述需求', at: '2026-07-18T00:00:00Z' },
    ]
    const wrapper = mountChat({ turns })
    expect(wrapper.text()).toContain('你好，请描述需求')
    expect(wrapper.find('textarea').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows done banner and hides composer when done', () => {
    const wrapper = mountChat({ done: true, turns: [{ role: 'agent', text: '完成', at: '2026-07-18T00:00:00Z' }] })
    expect(wrapper.text()).toMatch(/交互已完成|已完成/)
    expect(wrapper.find('textarea').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows closed hint when inactive', () => {
    const wrapper = mountChat({ active: false })
    expect(wrapper.text()).toMatch(/已关闭|不可再回复/)
    expect(wrapper.find('textarea').exists()).toBe(false)
    wrapper.unmount()
  })

  it('emits send when user submits text', async () => {
    const wrapper = mountChat()
    await wrapper.find('textarea').setValue('我的需求说明')
    await wrapper.find('button[class*="bg-accent"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('send')).toBeTruthy()
    const payload = wrapper.emitted('send')![0]
    expect(payload[0]).toBe('我的需求说明')
    wrapper.unmount()
  })

  it('renders structured questions and submits choice', async () => {
    const turns: ClarifyTurn[] = [
      {
        role: 'agent',
        text: '',
        at: '2026-07-18T00:00:00Z',
        questions: [
          {
            id: 'q1',
            prompt: '选择部署方式',
            options: [
              { id: 'k8s', label: 'Kubernetes', recommended: true },
              { id: 'vm', label: '虚拟机' },
            ],
          },
        ],
      },
    ]
    const wrapper = mountChat({ turns })
    expect(wrapper.text()).toContain('选择部署方式')
    const optionBtn = wrapper.findAll('button').find((b) => b.text().includes('Kubernetes'))
    expect(optionBtn).toBeTruthy()
    await optionBtn!.trigger('click')
    await flushPromises()
    const submitBtn = wrapper.findAll('button').find((b) => b.text().includes('确认选择'))
    expect(submitBtn).toBeTruthy()
    await submitBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('send')).toBeTruthy()
    expect(String(wrapper.emitted('send')![0][0])).toContain('Kubernetes')
    wrapper.unmount()
  })

  const OTHER_QUESTION_TURN: ClarifyTurn = {
    role: 'agent',
    text: '',
    at: '2026-07-18T00:00:00Z',
    questions: [
      {
        id: 'q1',
        prompt: '选择部署方式',
        options: [
          { id: 'k8s', label: 'Kubernetes' },
          { id: 'vm', label: '虚拟机' },
        ],
      },
    ],
  }

  function submitChoicesBtn(wrapper: ReturnType<typeof mountChat>) {
    return wrapper.findAll('button').find((b) => b.text().includes('确认选择'))
  }

  it('other: typed text without checkbox cannot submit alone (g1.2)', async () => {
    const wrapper = mountChat({ turns: [OTHER_QUESTION_TURN] })
    await wrapper.find('[data-testid="clarify-other-input"]').setValue('自由补充内容')
    await flushPromises()
    const submitBtn = submitChoicesBtn(wrapper)
    expect(submitBtn).toBeTruthy()
    expect((submitBtn!.element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('other: checked + non-empty text enables submit and appears in summary (g1.2/g1.3)', async () => {
    const wrapper = mountChat({ turns: [OTHER_QUESTION_TURN] })
    await wrapper.find('[data-testid="clarify-other-checkbox"]').trigger('click')
    await wrapper.find('[data-testid="clarify-other-input"]').setValue('仅其他补充')
    await flushPromises()
    const submitBtn = submitChoicesBtn(wrapper)!
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(false)
    await submitBtn.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('send')).toBeTruthy()
    const sent = String(wrapper.emitted('send')![0][0])
    expect(sent).toContain('仅其他补充')
    expect(sent).not.toContain('Kubernetes')
    wrapper.unmount()
  })

  it('other: checked but empty text does not count as answered (g1.2)', async () => {
    const wrapper = mountChat({ turns: [OTHER_QUESTION_TURN] })
    await wrapper.find('[data-testid="clarify-other-checkbox"]').trigger('click')
    await flushPromises()
    const submitBtn = submitChoicesBtn(wrapper)!
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('other: unchecked text is omitted when option + other both present (g1.2)', async () => {
    const wrapper = mountChat({ turns: [OTHER_QUESTION_TURN] })
    const optionBtn = wrapper.findAll('button').find((b) => b.text().includes('Kubernetes'))
    await optionBtn!.trigger('click')
    await wrapper.find('[data-testid="clarify-other-input"]').setValue('不应出现')
    await flushPromises()
    const submitBtn = submitChoicesBtn(wrapper)!
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(false)
    await submitBtn.trigger('click')
    await flushPromises()
    const sent = String(wrapper.emitted('send')![0][0])
    expect(sent).toContain('Kubernetes')
    expect(sent).not.toContain('不应出现')
    wrapper.unmount()
  })

  it('other row shows checkbox control matching question mode (g1.1)', () => {
    const wrapper = mountChat({ turns: [OTHER_QUESTION_TURN] })
    expect(wrapper.find('[data-testid="clarify-other-checkbox"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-other-row"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('emits finish on early finish click', async () => {
    const wrapper = mountChat()
    const finishBtn = wrapper.findAll('button').find((b) => b.text().includes('结束交互'))
    expect(finishBtn).toBeTruthy()
    await finishBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('finish')).toBeTruthy()
    wrapper.unmount()
  })

  it('review mode: confirm disabled while thinking/queued (FR4 ready gate)', async () => {
    const wrapper = mountChat({ reviewMode: true })
    // Enqueue a revise: thinking/queue must block 确认并流转 until ready.
    await wrapper.find('textarea').setValue('补充修订')
    await wrapper.find('button[class*="bg-accent"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    const confirmBtn = wrapper.find('[data-testid="clarify-confirm-flow"]')
    expect(confirmBtn.exists()).toBe(true)
    expect((confirmBtn.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="clarify-confirm-hint"]').text()).toContain(
      '接受当前已落盘产物并流转（不触发 Agent）',
    )
    expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('review mode: confirmError shows status bar and re-enables confirm', async () => {
    const wrapper = mountChat({ reviewMode: true })
    const confirmBtn = wrapper.find('[data-testid="clarify-confirm-flow"]')
    await confirmBtn.trigger('click')
    await flushPromises()
    expect((confirmBtn.element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.setProps({ confirmError: '产物契约不满足' })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-confirm-error"]').text()).toContain('产物契约不满足')
    expect((wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    wrapper.unmount()
  })

  // An approve node confirms in clarify mode (not reviewMode), where the only
  // spinner is `thinking`. The server can reject the confirm (open questions /
  // unfinished wrap-up) and the dialogue stays open, so props.done never flips —
  // without releasing the spinner the user is stranded on「正在思考下一轮」.
  it('approve confirm rejection releases the thinking placeholder and shows why', async () => {
    const wrapper = mountChat({
      nodeType: 'approve',
      turns: [{ role: 'agent', text: '要做登录吗', at: '2026-08-21T17:00:00+08:00' }],
    })
    const confirmBtn = wrapper.find('[data-testid="clarify-confirm-flow"]')
    await confirmBtn.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Agent 正在思考下一轮')
    expect((confirmBtn.element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.setProps({ confirmError: '仍有待确认问题或收尾未完成，无法确认并流转' })
    await flushPromises()

    expect(wrapper.text()).not.toContain('Agent 正在思考下一轮')
    expect(wrapper.find('[data-testid="clarify-confirm-error"]').text()).toContain(
      '无法确认并流转',
    )
    expect(
      (wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled,
    ).toBe(false)
    wrapper.unmount()
  })

  it('enqueue shows queue panel (not optimistic transcript bubble)', async () => {
    const anns: ReactAnnotation[] = [{ label: '提案标题', jsonPath: 'proposals[0].title' }]
    const wrapper = mountChat({
      annotateEnabled: true,
      annotations: anns,
      draft: '请看这里',
    })
    await clickSend(wrapper)

    expect(wrapper.emitted('send')).toBeTruthy()
    const payload = wrapper.emitted('send')![0]
    expect(payload[0]).toBe('请看这里')
    expect(payload[2]).toEqual(anns)

    // Session UX: message lives in queue panel until turn_begin
    const queue = wrapper.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    expect(queue.text()).toContain('请看这里')
    expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
    expect(wrapper.text()).toMatch(/思考/)
    wrapper.unmount()
  })

  it('enqueue annotation-only shows queue row without transcript bubble', async () => {
    const anns: ReactAnnotation[] = [{ label: 'Hero', selector: '#hero' }]
    const wrapper = mountChat({
      annotateEnabled: true,
      annotations: anns,
    })
    await clickSend(wrapper)

    expect(wrapper.emitted('send')).toBeTruthy()
    const payload = wrapper.emitted('send')![0]
    expect(payload[0]).toBe('')
    expect(payload[2]).toEqual(anns)

    expect(wrapper.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(wrapper.findAll('.md.rounded-lg').length).toBe(0)
    expect(wrapper.text()).toMatch(/思考/)
    wrapper.unmount()
  })

  it('keeps queued text after composer annotations are cleared', async () => {
    const anns: ReactAnnotation[] = [{ label: '字段路径', jsonPath: 'summary' }]
    const wrapper = mountChat({
      annotateEnabled: true,
      annotations: anns,
      draft: '核对引用',
    })
    await clickSend(wrapper)

    // Parent applies composer clear from update:annotations (same as RunDetail)
    const updates = wrapper.emitted('update:annotations')
    expect(updates).toBeTruthy()
    const cleared = updates![updates!.length - 1][0] as ReactAnnotation[]
    await wrapper.setProps({ annotations: cleared, draft: '' })
    await flushPromises()

    // Composer staging cleared; queue panel still shows the enqueued text
    expect(wrapper.props('annotations')).toEqual([])
    expect(wrapper.find('[data-testid="clarify-review-queue"]').text()).toContain('核对引用')
    expect(wrapper.text()).toMatch(/思考/)
    wrapper.unmount()
  })

  it('uses clarify placeholder by default', () => {
    const wrapper = mountChat()
    expect(wrapper.find('[data-testid="clarify-input"]').attributes('placeholder')).toBe('补充信息…')
    wrapper.unmount()
  })

  it('uses review placeholder in reviewMode', () => {
    const wrapper = mountChat({ reviewMode: true })
    expect(wrapper.find('[data-testid="clarify-input"]').attributes('placeholder')).toBe(
      '补充批注或修改说明…',
    )
    wrapper.unmount()
  })

  it('uses approve first-speaker placeholder and empty hint', () => {
    const wrapper = mountChat({ nodeType: 'approve', turns: null })
    const openingHint = '请先描述目标…'
    expect(wrapper.find('[data-testid="clarify-input"]').attributes('placeholder')).toBe(
      openingHint,
    )
    // Same opening hint only as placeholder — not copied into empty-hint / scroller (g2.2).
    expect(wrapper.find('[data-testid="clarify-approve-empty-hint"]').text()).toContain('先说明本次要做的目标')
    expect(wrapper.find('[data-testid="clarify-approve-empty-hint"]').text()).not.toContain(openingHint)
    expect(wrapper.find('[data-testid="clarify-scroller"]').text()).toContain('共 0 条')
    expect(wrapper.find('[data-testid="clarify-scroller"]').text()).not.toContain(openingHint)
    wrapper.unmount()
  })

  it('seeds the first human bubble and hides the approve empty hint', () => {
    const wrapper = mountChat({
      nodeType: 'approve',
      turns: [],
      seedHumanText: '把登录做清楚',
      seedHumanImages: [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }],
    })
    expect(wrapper.find('[data-testid="clarify-approve-empty-hint"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="clarify-scroller"]').text()).toContain('把登录做清楚')
    expect(wrapper.find('[data-testid="clarify-history-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-confirm-flow"]').text()).toContain('确认并流转')
    wrapper.unmount()
  })

  it('seeds file chips with the first bubble and keeps them after idle queue_state', async () => {
    const wrapper = mountChat({
      nodeType: 'approve',
      turns: [],
      seedHumanText: '把登录做清楚',
      seedHumanImages: [
        { data: 'abc', mimeType: 'image/png', name: 'shot.png' },
        { data: 'QQ==', mimeType: 'application/pdf', name: 'brief.pdf' },
      ],
    })
    expect(wrapper.find('[data-testid="clarify-history-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-history-file-chip"]').text()).toContain('brief.pdf')
    const vm = wrapper.vm as unknown as { applyReviewFrame: (f: Record<string, unknown>) => void }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'react-1',
      waiting: 0,
      items: [],
      busy: false,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-history-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-history-file-chip"]').text()).toContain('brief.pdf')
    await wrapper.setProps({
      turns: [{ role: 'human', text: '把登录做清楚', at: '2026-08-21T00:00:00Z' }],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-history-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-history-file-chip"]').text()).toContain('brief.pdf')
    wrapper.unmount()
  })

  it('does not duplicate an image-only seed once the persisted human turn lands', async () => {
    const shot = { data: 'abc', mimeType: 'image/png', name: 'shot.png' }
    const wrapper = mountChat({
      nodeType: 'approve',
      turns: [{ role: 'human', text: '', at: '2026-08-21T00:00:00Z', images: [shot] }],
      seedHumanText: '',
      seedHumanImages: [shot],
    })
    expect(wrapper.findAll('[data-testid="clarify-history-image-thumb"]')).toHaveLength(1)
    wrapper.unmount()
  })

  function mockScrollHeight(el: HTMLTextAreaElement, height: number) {
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => height })
  }

  it('autoGrow: empty/single-line keeps ~40px and hides overflow', async () => {
    const wrapper = mountChat()
    const ta = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    mockScrollHeight(ta, 40)
    await wrapper.find('[data-testid="clarify-input"]').setValue('短句')
    await flushPromises()
    expect(ta.style.height).toBe('40px')
    expect(ta.classList.contains('overflow-y-hidden')).toBe(true)
    expect(ta.classList.contains('overflow-y-auto')).toBe(false)
    wrapper.unmount()
  })

  it('autoGrow: grows with content up to 128px max', async () => {
    const wrapper = mountChat()
    const ta = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    mockScrollHeight(ta, 96)
    await wrapper.find('[data-testid="clarify-input"]').setValue('第一行\n第二行\n第三行')
    await flushPromises()
    expect(ta.style.height).toBe('96px')
    expect(ta.classList.contains('overflow-y-hidden')).toBe(true)

    mockScrollHeight(ta, 200)
    await wrapper.find('[data-testid="clarify-input"]').setValue('a\n'.repeat(20))
    await flushPromises()
    expect(ta.style.height).toBe('128px')
    expect(ta.classList.contains('overflow-y-auto')).toBe(true)
    expect(ta.classList.contains('max-h-[128px]')).toBe(true)
    wrapper.unmount()
  })

  it('autoGrow: clears draft and collapses to ~40px instantly', async () => {
    const wrapper = mountChat({ draft: '多行\n内容\n说明' })
    const ta = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    mockScrollHeight(ta, 160)
    await wrapper.find('[data-testid="clarify-input"]').trigger('input')
    await flushPromises()
    expect(ta.style.height).toBe('128px')
    expect(ta.classList.contains('overflow-y-auto')).toBe(true)

    mockScrollHeight(ta, 40)
    await wrapper.setProps({ draft: '' })
    await flushPromises()
    expect(ta.style.height).toBe('40px')
    expect(ta.classList.contains('overflow-y-hidden')).toBe(true)
    wrapper.unmount()
  })

  it('keeps Enter send and Shift+Enter keybinding on textarea', async () => {
    const wrapper = mountChat({ draft: '发送我' })
    const ta = wrapper.find('[data-testid="clarify-input"]')
    await ta.trigger('keydown', { key: 'Enter', shiftKey: false })
    await flushPromises()
    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')![0][0]).toBe('发送我')
    // Shift+Enter is not bound to send (exact modifier); no extra emit
    const before = wrapper.emitted('send')!.length
    await ta.trigger('keydown', { key: 'Enter', shiftKey: true })
    await flushPromises()
    expect(wrapper.emitted('send')!.length).toBe(before)
    wrapper.unmount()
  })

  it('reviewMode confirm bar remains visible with tall input', async () => {
    const wrapper = mountChat({ reviewMode: true, draft: 'a\n'.repeat(30) })
    const ta = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    mockScrollHeight(ta, 240)
    await wrapper.find('[data-testid="clarify-input"]').trigger('input')
    await flushPromises()
    expect(ta.style.height).toBe('128px')
    expect(wrapper.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-confirm-hint"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps send and cancel inside the input chrome; confirm stays in the footer', async () => {
    const wrapper = mountChat({ sendLabel: '发送澄清回复' })
    const inputRow = wrapper.get('[data-testid="clarify-input-row"]')
    const actionRow = wrapper.get('[data-testid="clarify-action-row"]')
    expect(inputRow.find('[data-testid="clarify-input"]').exists()).toBe(true)
    expect(inputRow.find('[data-testid="clarify-attach-btn"]').exists()).toBe(true)
    expect(inputRow.find('[data-testid="clarify-send-label"]').exists()).toBe(true)
    expect(actionRow.find('[data-testid="clarify-send-label"]').exists()).toBe(true)
    expect(actionRow.find('[data-testid="clarify-send-label"]').text()).toContain('发送澄清回复')
    expect(wrapper.find('[data-testid="clarify-input"]').classes()).toContain('composer-hint-wrap')
    wrapper.unmount()
  })

  it('confirm hint wraps by width and stays outside the input chrome', async () => {
    const wrapper = mountChat({ reviewMode: true })
    const hint = wrapper.get('[data-testid="clarify-confirm-hint"]')
    expect(hint.classes().join(' ')).toContain('[overflow-wrap:anywhere]')
    expect(wrapper.find('[data-testid="clarify-input-row"]').find('[data-testid="clarify-confirm-flow"]').exists()).toBe(
      false,
    )
    expect(wrapper.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-confirm-flow"]').classes().join(' ')).toContain('h-9')
    wrapper.unmount()
  })

  it('shows Cancel inside the input chrome while session is busy', async () => {
    const wrapper = mountChat()
    await wrapper.find('[data-testid="clarify-input"]').setValue('排队')
    await clickSend(wrapper)
    expect(wrapper.find('[data-testid="clarify-input-row"]').find('[data-testid="clarify-review-cancel"]').exists()).toBe(
      true,
    )
    expect(wrapper.find('[data-testid="clarify-action-row"]').find('[data-testid="clarify-review-cancel"]').exists()).toBe(
      true,
    )
    wrapper.unmount()
  })

  it('keeps paperclip SVG box and a fixed toolbar when draft is multiline (g1.1 g3.1)', async () => {
    const wrapper = mountChat({ draft: 'line1\nline2\nline3' })
    const ta = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    expect(ta.tagName).toBe('TEXTAREA')
    expect(ta.value).toContain('\n')
    const attach = wrapper.get('[data-testid="clarify-attach-btn"]')
    expect(attach.classes()).toEqual(expect.arrayContaining(['h-10', 'w-10', 'shrink-0']))
    const toolbar = wrapper.get('[data-testid="clarify-action-row"]')
    expect(toolbar.classes()).toEqual(expect.arrayContaining(['h-11', 'shrink-0']))
    wrapper.unmount()
  })

  function scrollerOf(wrapper: ReturnType<typeof mountChat>) {
    return wrapper.find('[data-testid="clarify-scroller"]').element as HTMLElement
  }

  /** Mock scroller metrics so near-bottom / leave-bottom can be simulated in happy-dom. */
  function mockScrollerMetrics(
    el: HTMLElement,
    opts: { scrollHeight: number; clientHeight: number; scrollTop: number },
  ) {
    let top = opts.scrollTop
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => opts.scrollHeight })
    Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => opts.clientHeight })
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
  }

  /** Drain mount enterStickSequence (nextTick + rAF second pin) before leave-bottom assertions. */
  async function settleEnterStick() {
    await flushPromises()
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => resolve())
    })
    await flushPromises()
  }

  async function leaveBottom(wrapper: ReturnType<typeof mountChat>) {
    await settleEnterStick()
    const el = scrollerOf(wrapper)
    mockScrollerMetrics(el, { scrollHeight: 800, clientHeight: 300, scrollTop: 100 })
    await wrapper.find('[data-testid="clarify-scroller"]').trigger('scroll')
    await flushPromises()
  }

  async function scrollNearBottom(wrapper: ReturnType<typeof mountChat>) {
    const el = scrollerOf(wrapper)
    mockScrollerMetrics(el, { scrollHeight: 800, clientHeight: 300, scrollTop: 760 })
    await wrapper.find('[data-testid="clarify-scroller"]').trigger('scroll')
    await flushPromises()
  }

  function agentTurn(text: string, at: string): ClarifyTurn {
    return { role: 'agent', text, at }
  }

  it('stick-to-bottom: stays put when off-bottom and new turns arrive', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('历史消息', '2026-07-18T00:00:00Z')],
    })
    await leaveBottom(wrapper)
    const el = scrollerOf(wrapper)
    const before = el.scrollTop
    await wrapper.setProps({
      turns: [
        agentTurn('历史消息', '2026-07-18T00:00:00Z'),
        agentTurn('新消息', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    expect(el.scrollTop).toBe(before)
    wrapper.unmount()
  })

  it('stick-to-bottom: auto-follows when near bottom', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('历史消息', '2026-07-18T00:00:00Z')],
    })
    const el = scrollerOf(wrapper)
    mockScrollerMetrics(el, { scrollHeight: 800, clientHeight: 300, scrollTop: 760 })
    await wrapper.find('[data-testid="clarify-scroller"]').trigger('scroll')
    await flushPromises()
    await wrapper.setProps({
      turns: [
        agentTurn('历史消息', '2026-07-18T00:00:00Z'),
        agentTurn('贴底新消息', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    // force/stick path sets scrollTop = scrollHeight
    expect(el.scrollTop).toBe(el.scrollHeight)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('unread FAB: accumulates exact count while off-bottom', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('seed', '2026-07-18T00:00:00Z')],
    })
    await leaveBottom(wrapper)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)

    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('a', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    const fab = wrapper.find('[data-testid="clarify-unread-fab"]')
    expect(fab.exists()).toBe(true)
    expect(fab.text()).toContain('1')
    expect(fab.attributes('aria-label')).toContain('1')
    expect(fab.attributes('title')).toContain('1')

    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('a', '2026-07-18T00:00:01Z'),
        agentTurn('b', '2026-07-18T00:00:02Z'),
        agentTurn('c', '2026-07-18T00:00:03Z'),
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').text()).toContain('3')
    wrapper.unmount()
  })

  it('unread FAB: thinking alone does not show or increment unread', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('seed', '2026-07-18T00:00:00Z')],
    })
    await leaveBottom(wrapper)
    // Simulate send-triggered thinking without new props.turns
    await wrapper.find('textarea').setValue('回复')
    // Leave bottom again after send force-stick, then re-enter off-bottom with no new turns
    await wrapper.find('button[class*="bg-accent"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/思考/)
    // After send we force-stick — FAB must be hidden even while thinking
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)

    await leaveBottom(wrapper)
    // Still no new props.turns → still no FAB (thinking does not count)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('unread FAB: click clears unread and scrolls to bottom', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('seed', '2026-07-18T00:00:00Z')],
    })
    await leaveBottom(wrapper)
    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('new', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    const fab = wrapper.find('[data-testid="clarify-unread-fab"]')
    expect(fab.exists()).toBe(true)
    await fab.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    expect(scrollerOf(wrapper).scrollTop).toBe(scrollerOf(wrapper).scrollHeight)
    wrapper.unmount()
  })

  it('unread FAB: manual scroll near bottom clears unread', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('seed', '2026-07-18T00:00:00Z')],
    })
    await leaveBottom(wrapper)
    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('new', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(true)
    await scrollNearBottom(wrapper)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('unread FAB: user send force-sticks and clears unread', async () => {
    const wrapper = mountChat({
      turns: [agentTurn('seed', '2026-07-18T00:00:00Z')],
      draft: '我看完了',
    })
    await leaveBottom(wrapper)
    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('agent-new', '2026-07-18T00:00:01Z'),
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(true)

    await clickSend(wrapper)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    expect(scrollerOf(wrapper).scrollTop).toBe(scrollerOf(wrapper).scrollHeight)

    // Backend catch-up with user turn must not re-show FAB while stuck
    await wrapper.setProps({
      turns: [
        agentTurn('seed', '2026-07-18T00:00:00Z'),
        agentTurn('agent-new', '2026-07-18T00:00:01Z'),
        { role: 'human', text: '我看完了', at: '2026-07-18T00:00:02Z' },
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('unread FAB: shows exact three-digit count without capping', async () => {
    const seed = Array.from({ length: 5 }, (_, i) =>
      agentTurn(`t${i}`, `2026-07-18T00:00:0${i}Z`),
    )
    const wrapper = mountChat({ turns: seed })
    await leaveBottom(wrapper)
    const more = [
      ...seed,
      ...Array.from({ length: 128 }, (_, i) =>
        agentTurn(`n${i}`, `2026-07-18T01:${String(i % 60).padStart(2, '0')}:00Z`),
      ),
    ]
    await wrapper.setProps({ turns: more })
    await flushPromises()
    const fab = wrapper.find('[data-testid="clarify-unread-fab"]')
    expect(fab.exists()).toBe(true)
    expect(fab.text()).toContain('128')
    expect(fab.text()).not.toMatch(/\+|99\+/)
    expect(fab.attributes('aria-label')).toContain('128')
    wrapper.unmount()
  })

  /** Historical turns long enough that an unpinned scroller would leave latest off-screen. */
  function historyTurns(n = 12): ClarifyTurn[] {
    return Array.from({ length: n }, (_, i) =>
      agentTurn(`历史消息-${i}`, `2026-07-18T00:${String(i).padStart(2, '0')}:00Z`),
    )
  }

  /**
   * Install scroller metrics before enterStickSequence's nextTick flush, then
   * flush nextTick + sync rAF second pin (plan g2.1 / g2.2 enter evidence).
   */
  async function flushEnterStick(wrapper: ReturnType<typeof mountChat>, scrollHeight = 2000) {
    const el = scrollerOf(wrapper)
    mockScrollerMetrics(el, { scrollHeight, clientHeight: 300, scrollTop: 0 })
    await flushPromises()
    await flushPromises()
    return el
  }

  it('enter stick: mounts with history and pins latest to bottom (g2.1)', async () => {
    const rAF = vi.fn((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    vi.stubGlobal('requestAnimationFrame', rAF)

    const turns = historyTurns()
    const wrapper = mountChat({ turns })
    const el = await flushEnterStick(wrapper)
    expect(el.scrollTop).toBe(el.scrollHeight)
    expect(rAF).toHaveBeenCalled()
    expect(wrapper.text()).toContain('历史消息-11')
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('enter stick: unmount then remount with history still pins bottom (g2.1 leave/re-enter)', async () => {
    const rAF = vi.fn((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    vi.stubGlobal('requestAnimationFrame', rAF)

    const turns = historyTurns()
    const first = mountChat({ turns })
    await flushEnterStick(first)
    first.unmount()

    const second = mountChat({ turns })
    const el = await flushEnterStick(second)
    expect(el.scrollTop).toBe(el.scrollHeight)
    expect(second.text()).toContain('历史消息-11')
    second.unmount()
    vi.unstubAllGlobals()
  })

  it('enter stick: session identity change force-pins twice via rAF (g2.2)', async () => {
    let height = 2000
    const rAF = vi.fn((cb: FrameRequestCallback) => {
      // Browser layout lag: scrollHeight grows before the second force pin paints.
      height = 2400
      cb(0)
      return 0
    })
    vi.stubGlobal('requestAnimationFrame', rAF)

    const turns = historyTurns()
    const wrapper = mountChat({ turns })
    const el = scrollerOf(wrapper)
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => height })
    Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => 300 })
    let top = 0
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
    await flushPromises()
    await flushPromises()
    expect(el.scrollTop).toBe(2400)
    const mountRafCalls = rAF.mock.calls.length
    expect(mountRafCalls).toBeGreaterThanOrEqual(1)

    // Leave bottom, then same-panel identity switch must force-pin again (g2.2).
    height = 1800
    top = 100
    await wrapper.find('[data-testid="clarify-scroller"]').trigger('scroll')
    await flushPromises()

    await wrapper.setProps({ runId: 'run-2', nodeId: 'react-2', iteration: 2 })
    await flushPromises()
    await flushPromises()
    expect(el.scrollTop).toBe(2400)
    expect(rAF.mock.calls.length).toBeGreaterThan(mountRafCalls)
    expect(wrapper.find('[data-testid="clarify-unread-fab"]').exists()).toBe(false)
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  describe('ACP delivery contract (false applied / hard-refresh resume)', () => {
    it('applyAcpEvents returns false when streaming slot not ready (g1.1)', async () => {
      const wrapper = mountChat()
      const vm = wrapper.vm as unknown as {
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => boolean
      }
      // Mounted but no turn_begin / queue_state busy slot yet.
      expect(vm.applyAcpEvents([{ kind: 'thought', text: 'should buffer' }], 'react-1')).toBe(false)
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('applyAcpEvents returns true after queue_state rebuilds slot (g1.1 flush path)', async () => {
      const wrapper = mountChat()
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => boolean
      }
      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 0,
        items: [],
        busy: true,
        activeItem: { text: '硬刷新前的提问' },
      })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
      expect(vm.applyAcpEvents([{ kind: 'thought', text: '已恢复的思考' }], 'react-1')).toBe(true)
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-thought"]').text()).toContain('已恢复的思考')
      wrapper.unmount()
    })

    it('authority idle tears down empty streaming placeholder (g2.2/f3)', async () => {
      const wrapper = mountChat()
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
      }
      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 0,
        items: [],
        busy: true,
        activeItem: { text: '进行中' },
      })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 0,
        items: [],
        busy: false,
      })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('does not duplicate human when persisted already completed the live turn', async () => {
      const wrapper = mountChat({
        reviewMode: true,
        turns: [
          { role: 'human', text: '改成绿的', at: '2026-08-01T00:01:00Z' },
          { role: 'agent', text: '标题已改为绿色（#16a34a）', at: '2026-08-01T00:02:00Z' },
        ],
      })
      const vm = wrapper.vm as unknown as {
        applyQueueState: (
          waiting: number,
          items: unknown[] | null,
          busy?: boolean,
          activeItem?: { text?: string } | null,
        ) => void
      }
      vm.applyQueueState(0, [], true, { text: '改成绿的' })
      await flushPromises()
      expect((wrapper.text().match(/改成绿的/g) || []).length).toBe(1)
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
      expect(wrapper.text()).toContain('标题已改为绿色')
      wrapper.unmount()
    })

    it('tears down streaming placeholder when persisted catches up mid-stream', async () => {
      const wrapper = mountChat({ reviewMode: true, turns: [{ role: 'agent', text: '请复审', at: 't0' }] })
      const vm = wrapper.vm as unknown as {
        applyQueueState: (
          waiting: number,
          items: unknown[] | null,
          busy?: boolean,
          activeItem?: { text?: string } | null,
        ) => void
      }
      vm.applyQueueState(0, [], true, { text: '改成绿的' })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
      await wrapper.setProps({
        turns: [
          { role: 'agent', text: '请复审', at: 't0' },
          { role: 'human', text: '改成绿的', at: 't1' },
          { role: 'agent', text: '标题已改为绿色', at: 't2' },
        ],
      })
      await flushPromises()
      expect((wrapper.text().match(/改成绿的/g) || []).length).toBe(1)
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
      expect(wrapper.text()).toContain('标题已改为绿色')
      wrapper.unmount()
    })
  })

  describe('busy status C-tier (no air bubble)', () => {
    it('turn_begin shows 思考中… + typing-dots placeholder (no air bubble)', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      await flushPromises()

      const placeholder = wrapper.find('[data-testid="clarify-busy-placeholder"]')
      expect(placeholder.exists()).toBe(true)
      expect(placeholder.text()).toContain('思考中')
      expect(placeholder.find('.typing-dots').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-agent-message"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-thought"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('thought is visible and default-open; collapses when message starts', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      await flushPromises()
      vm.applyAcpEvents([{ kind: 'thought', text: '先核对边界与分轨' }], 'react-1')
      await flushPromises()

      const thought = wrapper.find('[data-testid="clarify-thought"]')
      expect(thought.exists()).toBe(true)
      expect(thought.attributes('open')).toBeDefined()
      expect(thought.text()).toContain('先核对边界与分轨')
      expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
        'streaming',
      )
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('生成中')
      expect(wrapper.find('[data-testid="clarify-busy-status"]').text()).toContain('思考中')
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)

      vm.applyAcpEvents(
        [
          { kind: 'thought', text: '先核对边界与分轨' },
          { kind: 'message', text: '已核对完成' },
        ],
        'react-1',
      )
      await flushPromises()

      expect(wrapper.find('[data-testid="clarify-thought"]').text()).toContain('先核对边界与分轨')
      // Demo: collapse thought once message streaming starts.
      expect(wrapper.find('[data-testid="clarify-thought"]').attributes('open')).toBeUndefined()
      // Message outputting — summary stays generating (not done).
      expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
        'streaming',
      )
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('生成中')
      expect(wrapper.find('[data-testid="clarify-agent-message"]').text()).toContain('已核对完成')
      expect(wrapper.find('[data-testid="clarify-busy-status"]').text()).toContain('输出中')
      expect(wrapper.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
      wrapper.unmount()
    })

    // plan coverage: g2.1 — frontend must render full Thought (>4000 bytes), no …(truncated)
    it('renders full long thought without truncated suffix (g2.1)', async () => {
      const longThought = `${'思'.repeat(2000)}结尾标记END`
      expect(new TextEncoder().encode(longThought).length).toBeGreaterThan(4000)
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      await flushPromises()
      vm.applyAcpEvents([{ kind: 'thought', text: longThought }], 'react-1')
      await flushPromises()

      const thoughtBody = wrapper.find('[data-testid="clarify-thought"] .whitespace-pre-wrap')
      expect(thoughtBody.exists()).toBe(true)
      expect(thoughtBody.text()).toBe(longThought)
      expect(thoughtBody.text()).toContain('结尾标记END')
      expect(thoughtBody.text()).not.toContain('…(truncated)')
      expect(thoughtBody.text()).not.toContain('...(truncated)')
      wrapper.unmount()
    })

    it('turn_done removes 输出中 status while keeping thought+message', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      vm.applyAcpEvents(
        [
          { kind: 'thought', text: '思考内容' },
          { kind: 'message', text: '正文内容' },
        ],
        'react-1',
      )
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-status"]').text()).toContain('输出中')

      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-busy-status"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-stream-caret"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-thought"]').text()).toContain('思考内容')
      expect(wrapper.find('[data-testid="clarify-agent-message"]').text()).toContain('正文内容')
      const completed = wrapper.find('[data-testid="clarify-turn-completed"]')
      expect(completed.exists()).toBe(true)
      expect(completed.text()).toContain('已完成')
      // keep_footer_hide_bottom: footer keeps one relTime; completed agent has no bottom time row
      expect(completed.text()).toContain('刚刚')
      expect(completed.text().match(/刚刚/g)?.length).toBe(1)
      // human turn still has bottom time; completed agent turn must not
      expect(wrapper.findAll('[data-testid="clarify-turn-bottom-time"]').length).toBe(1)
      expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
        'done',
      )
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('已完成')
      wrapper.unmount()
    })

    it('historical completed turn keeps single relTime channel (no bottom time)', () => {
      const wrapper = mountChat({
        turns: [{ role: 'agent', text: '历史已完成正文', at: new Date().toISOString() }],
      })
      const completed = wrapper.find('[data-testid="clarify-turn-completed"]')
      expect(completed.exists()).toBe(true)
      expect(completed.text()).toContain('已完成')
      expect(completed.text()).toContain('刚刚')
      expect(completed.text().match(/刚刚/g)?.length).toBe(1)
      expect(wrapper.find('[data-testid="clarify-turn-bottom-time"]').exists()).toBe(false)
      expect(wrapper.text().match(/刚刚/g)?.length).toBe(1)
      wrapper.unmount()
    })

    it('streaming agent keeps bottom time without 已完成 footnote', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      vm.applyAcpEvents([{ kind: 'message', text: '流式中正文' }], 'react-1')
      await flushPromises()

      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
      // human + streaming agent both keep bottom time channel
      expect(wrapper.findAll('[data-testid="clarify-turn-bottom-time"]').length).toBe(2)
      wrapper.unmount()
    })

    it('interrupted turn_done does not show 已完成 footnote', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      vm.applyAcpEvents(
        [
          { kind: 'thought', text: '半截思考' },
          { kind: 'message', text: '半截正文' },
        ],
        'react-1',
      )
      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1', interrupted: true })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
      // interrupted is non-completed: keep bottom time (human + interrupted agent)
      expect(wrapper.findAll('[data-testid="clarify-turn-bottom-time"]').length).toBe(2)
      expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
        'interrupted',
      )
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('已中断')
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).not.toContain('已完成')
      wrapper.unmount()
    })

    // g2.1: revise failure body must not look like success (live stream + history).
    it('revise failure text + interrupted turn_done does not show 已完成/Done', async () => {
      const failText = '(复审修改失败:acp chat idle timeout after 10m0s)'
      const wrapper = mountChat({ draft: '请复审', reviewMode: true })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      vm.applyAcpEvents(
        [
          { kind: 'thought', text: '半截思考' },
          { kind: 'message', text: failText },
        ],
        'react-1',
      )
      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1', interrupted: true })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-agent-message"]').text()).toContain('复审修改失败')
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      expect(wrapper.text()).not.toMatch(/\bDone\b/)
      expect(wrapper.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
        'interrupted',
      )
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).toContain('已中断')
      expect(wrapper.find('[data-testid="thought-summary-state"]').text()).not.toContain('已完成')
      wrapper.unmount()
    })

    it('historical Interrupted failure message does not show 已完成/Done', () => {
      const wrapper = mountChat({
        reviewMode: true,
        turns: [
          {
            role: 'agent',
            text: '(复审修改失败:acp chat idle timeout after 10m0s)',
            at: new Date().toISOString(),
            interrupted: true,
          },
        ],
      })
      expect(wrapper.text()).toContain('复审修改失败')
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
      expect(wrapper.text()).not.toMatch(/\bDone\b/)
      const summary = wrapper.find('[data-testid="thought-summary-state"]')
      if (summary.exists()) {
        expect(summary.attributes('data-state')).toBe('interrupted')
        expect(summary.text()).not.toContain('已完成')
      }
      wrapper.unmount()
    })

    it('tool_call alone keeps 思考中… placeholder (no tool UI, no air bubble)', async () => {
      const wrapper = mountChat({ draft: '请复审' })
      await clickSend(wrapper)
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text?: string }[], nodeId?: string) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '请复审' },
      })
      await flushPromises()
      vm.applyAcpEvents([{ kind: 'tool_call', text: 'read_file' }], 'react-1')
      await flushPromises()

      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-busy-placeholder"]').text()).toContain('思考中')
      expect(wrapper.text()).not.toMatch(/正在调用工具|读文件/)
      expect(wrapper.find('[data-testid="clarify-agent-message"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })

  describe('human history image AppModal preview (g4)', () => {
    const PNG_A = 'AAAApreviewA'
    const PNG_B = 'BBBBpreviewB'

    it('opens AppModal with title fallback「图片」and closes via × / backdrop (g4.1)', async () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text: '修改 你到底看了项目吗',
            at: '2026-07-28T00:00:00Z',
            images: [{ data: PNG_A, mimeType: 'image/png' }],
          },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)
      const thumb = wrapper.find('[data-testid="clarify-history-image-thumb"]')
      expect(thumb.exists()).toBe(true)
      expect(thumb.classes().join(' ')).toMatch(/hover:border-accent/)
      expect(thumb.text()).toContain('点击放大')
      await thumb.trigger('click')
      await flushPromises()

      const modal = wrapper.find('[data-testid="clarify-image-preview-modal"]')
      expect(modal.exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-image-preview-title"]').text()).toBe('图片预览 · 图片')
      const previewImg = wrapper.find('[data-testid="clarify-image-preview-img"]')
      expect(previewImg.exists()).toBe(true)
      expect(previewImg.attributes('src')).toBe(`data:image/png;base64,${PNG_A}`)

      await wrapper.find('[data-testid="clarify-image-preview-close"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)

      await thumb.trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(true)
      await wrapper.find('[data-testid="clarify-image-preview-backdrop"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('multi-image opens only the clicked index label「图片 N」(g4.2)', async () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text: '两张附图',
            at: '2026-07-28T00:00:00Z',
            images: [
              { data: PNG_A, mimeType: 'image/png' },
              { data: PNG_B, mimeType: 'image/png' },
            ],
          },
        ],
      })
      const thumbs = wrapper.findAll('[data-testid="clarify-history-image-thumb"]')
      expect(thumbs).toHaveLength(2)
      await thumbs[1].trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-title"]').text()).toBe('图片预览 · 图片 2')
      expect(wrapper.find('[data-testid="clarify-image-preview-img"]').attributes('src')).toBe(
        `data:image/png;base64,${PNG_B}`,
      )
      expect(wrapper.find('[data-testid="clarify-image-preview-prev"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-image-preview-next"]').exists()).toBe(false)

      await wrapper.find('[data-testid="clarify-image-preview-close"]').trigger('click')
      await flushPromises()
      await thumbs[0].trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-title"]').text()).toBe('图片预览 · 图片 1')
      expect(wrapper.find('[data-testid="clarify-image-preview-img"]').attributes('src')).toBe(
        `data:image/png;base64,${PNG_A}`,
      )
      wrapper.unmount()
    })

    it('agent history thumbs stay locked; composer drafts open preview (g2.2 / g2.3)', async () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'agent',
            text: '请附图',
            at: '2026-07-28T00:00:00Z',
            images: [{ data: PNG_A, mimeType: 'image/png' }],
          },
        ],
        attachments: [{ data: PNG_B, mimeType: 'image/png', name: '草稿图.png' }],
      })
      expect(wrapper.find('[data-testid="clarify-history-image-thumb"]').exists()).toBe(false)
      const agentThumb = wrapper.find('[data-testid="clarify-agent-image-thumb"]')
      expect(agentThumb.exists()).toBe(true)
      expect(agentThumb.text()).toContain('不可预览')
      expect(agentThumb.text()).not.toContain('点击放大')
      await agentThumb.trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)

      const draftThumb = wrapper.find('[data-testid="clarify-draft-image-thumb"]')
      expect(draftThumb.exists()).toBe(true)
      expect(draftThumb.text()).toContain('点击放大')
      expect(draftThumb.text()).not.toContain('不可预览')
      await draftThumb.trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-image-preview-title"]').text()).toBe('图片预览 · 草稿图.png')
      expect(wrapper.find('[data-testid="clarify-image-preview-img"]').attributes('src')).toBe(
        `data:image/png;base64,${PNG_B}`,
      )
      await wrapper.find('[data-testid="clarify-image-preview-close"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-draft-image-thumb"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('shows load-failed placeholder and still closes (f8)', async () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text: '坏图',
            at: '2026-07-28T00:00:00Z',
            images: [{ data: PNG_A, mimeType: 'image/png' }],
          },
        ],
      })
      await wrapper.find('[data-testid="clarify-history-image-thumb"]').trigger('click')
      await flushPromises()
      await wrapper.find('[data-testid="clarify-image-preview-img"]').trigger('error')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-failed"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-image-preview-failed"]').text()).toContain('图片加载失败')
      expect(wrapper.find('[data-testid="clarify-image-preview-img"]').exists()).toBe(false)
      await wrapper.find('[data-testid="clarify-image-preview-close"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-image-preview-modal"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })

  /**
   * Demo「修复后」长链折行：人类/Agent 走全局 .md；思考块单独补 anywhere。
   * plan g3.1 / g3.2 — href 不变，思考容器带 [overflow-wrap:anywhere]。
   */
  describe('long URL wrap (Demo after / plan g3)', () => {
    const LONG_URL =
      'http://approving.k3s.cc/api/blobs/333932fedb2e4ce9a1b7c8d0e2f4567890abcdef1234567890abcdef1234'

    it('human bubble uses .md; rendered link keeps full href (g3.1/f1)', () => {
      const wrapper = mountChat({
        turns: [{ role: 'human', text: LONG_URL, at: '2026-07-18T00:00:00Z' }],
      })
      const humanMd = wrapper.findAll('.md').find((n) => n.html().includes('/api/blobs/'))
      expect(humanMd).toBeTruthy()
      expect(humanMd!.classes()).toContain('md')
      const anchor = humanMd!.find('a')
      expect(anchor.exists()).toBe(true)
      expect(anchor.attributes('href')).toBe(LONG_URL)
      expect(anchor.text()).toBe(LONG_URL)
      wrapper.unmount()
    })

    it('agent .md + thought body have wrap classes; thought shows full long url (g3.1/g3.2/f3)', () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'agent',
            text: `产物地址：${LONG_URL}`,
            thought: `url=${LONG_URL}\n无需改 API`,
            at: '2026-07-18T00:00:00Z',
          },
        ],
      })
      const agentMsg = wrapper.find('[data-testid="clarify-agent-message"]')
      expect(agentMsg.exists()).toBe(true)
      expect(agentMsg.classes()).toContain('md')
      const agentLink = agentMsg.find('a')
      expect(agentLink.exists()).toBe(true)
      expect(agentLink.attributes('href')).toBe(LONG_URL)

      const thought = wrapper.find('[data-testid="clarify-thought"]')
      expect(thought.exists()).toBe(true)
      const thoughtBody = thought.find('.whitespace-pre-wrap')
      expect(thoughtBody.exists()).toBe(true)
      expect(thoughtBody.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)
      expect(thoughtBody.classes()).toContain('break-words')
      expect(thoughtBody.text()).toContain(LONG_URL)
      wrapper.unmount()
    })
  })

  describe('authoritative idle clears sticky thinking (plan g1/g4.1)', () => {
    it('ghost queued + turn_begin(with id) + turn_done → thinking/Cancel/confirm idle', async () => {
      const wrapper = mountChat({ reviewMode: true, draft: '请按意见修改' })
      await wrapper.find('textarea').setValue('请按意见修改')
      await clickSend(wrapper)
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
      expect(wrapper.text()).toContain('Agent 正在思考下一轮')

      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
        isSessionBusy: () => boolean
      }
      // Server id present → no text fallback; optimistic no-id row stays as ghost.
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { id: 'srv-1', text: '请按意见修改' },
      })
      vm.applyAcpEvents([{ kind: 'message', text: '已按意见改完' }], 'react-1')
      await flushPromises()
      expect(wrapper.text()).toContain('已按意见改完')
      // Live turn hides「思考下一轮」(condition requires liveAgentIdx < 0).
      expect(wrapper.text()).not.toContain('Agent 正在思考下一轮')

      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
      await flushPromises()

      expect(wrapper.text()).not.toContain('Agent 正在思考下一轮')
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
      expect(vm.isSessionBusy()).toBe(false)
      const confirm = wrapper.find('[data-testid="clarify-confirm-flow"]')
      expect(confirm.exists()).toBe(true)
      expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
      wrapper.unmount()
    })

    it('applyQueueState idle tears down live slot with body even when streaming===false', async () => {
      const wrapper = mountChat({ reviewMode: true })
      const vm = wrapper.vm as unknown as {
        applyQueueState: (
          waiting: number,
          items: unknown[] | null,
          busy?: boolean,
          activeItem?: { text?: string } | null,
        ) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => boolean
        isSessionBusy: () => boolean
      }
      vm.applyQueueState(0, [], true, { text: '改标题' })
      await flushPromises()
      expect(vm.applyAcpEvents([{ kind: 'message', text: '标题已改' }], 'react-1')).toBe(true)
      await flushPromises()
      // Authoritative idle must clear live occupancy (incl. body + streaming===false path).
      vm.applyQueueState(0, [], false, null)
      await flushPromises()
      expect(wrapper.text()).not.toContain('Agent 正在思考下一轮')
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(false)
      expect(vm.isSessionBusy()).toBe(false)
      expect((wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
        false,
      )
      wrapper.unmount()
    })

    it('real remaining queue via queue_state keeps busy (no false idle)', async () => {
      const wrapper = mountChat({ reviewMode: true })
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        isSessionBusy: () => boolean
      }
      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 1,
        items: [{ id: 'q2', text: '第二条意见' }],
        busy: false,
      })
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-review-queue"]').text()).toContain('第二条意见')
      expect(wrapper.text()).toContain('Agent 正在思考下一轮')
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
      expect(vm.isSessionBusy()).toBe(true)
      expect((wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
        true,
      )
      wrapper.unmount()
    })

    it('real remaining id waiter + turn_done keeps busy until authoritative idle', async () => {
      const wrapper = mountChat({ reviewMode: true })
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
        isSessionBusy: () => boolean
      }
      // Seed next waiter with server id, then start current turn.
      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 1,
        items: [{ id: 'q2', text: '第二条意见' }],
        busy: false,
      })
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { id: 'srv-1', text: '第一条意见' },
      })
      vm.applyAcpEvents([{ kind: 'message', text: '第一轮已改完' }], 'react-1')
      await flushPromises()

      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
      await flushPromises()

      // Must NOT synthesize idle while real id waiter remains (review v1/v2).
      expect(wrapper.find('[data-testid="clarify-review-queue"]').text()).toContain('第二条意见')
      expect(wrapper.text()).toContain('Agent 正在思考下一轮')
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
      expect(vm.isSessionBusy()).toBe(true)
      expect((wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
        true,
      )

      vm.applyReviewFrame({
        event: 'queue_state',
        nodeId: 'react-1',
        waiting: 0,
        items: [],
        busy: false,
        activeItem: null,
      })
      await flushPromises()
      expect(wrapper.text()).not.toContain('Agent 正在思考下一轮')
      expect(wrapper.find('[data-testid="clarify-review-cancel"]').exists()).toBe(false)
      expect(vm.isSessionBusy()).toBe(false)
      expect((wrapper.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
        false,
      )
      wrapper.unmount()
    })
  })

  describe('choice card answered from transcript not sessionStorage', () => {
    const deployQ: ClarifyTurn = {
      role: 'agent',
      text: '',
      at: '2026-07-18T00:00:00Z',
      questions: [
        {
          id: 'q1',
          prompt: '选择部署方式',
          options: [
            { id: 'k8s', label: 'Kubernetes', recommended: true },
            { id: 'vm', label: '虚拟机' },
          ],
        },
      ],
    }

    function submitChoices(wrapper: ReturnType<typeof mountChat>) {
      return wrapper.findAll('button').find((b) => b.text().includes('确认选择'))
    }

    async function pickAndSubmit(wrapper: ReturnType<typeof mountChat>, label: string) {
      const optionBtn = wrapper.findAll('button').find((b) => b.text().includes(label))
      expect(optionBtn).toBeTruthy()
      await optionBtn!.trigger('click')
      await flushPromises()
      const submitBtn = submitChoices(wrapper)
      expect(submitBtn).toBeTruthy()
      await submitBtn!.trigger('click')
      await flushPromises()
    }

    it('stale sessionStorage key does not hide a new prompt with reused q1', () => {
      sessionStorage.setItem(
        'clarify.submitted.run-1.react-1.1.q1',
        JSON.stringify({ text: '我的选择:\n- 旧题面 → 旧答案' }),
      )
      const wrapper = mountChat({
        turns: [
          {
            role: 'agent',
            text: '',
            at: '2026-08-26T00:00:00Z',
            questions: [
              {
                id: 'q1',
                prompt: '片段如何落地为模板?',
                options: [
                  { id: 'a', label: '6 个独立模板', recommended: true },
                  { id: 'b', label: '合并模板' },
                ],
              },
            ],
          },
        ],
      })
      expect(wrapper.text()).toContain('片段如何落地为模板?')
      expect(submitChoices(wrapper)?.exists()).toBe(true)
      expect(wrapper.text()).not.toContain('旧答案')
      expect(wrapper.text()).not.toContain('我的选择')
      wrapper.unmount()
    })

    it('persisted choice after this round hides the interactive card', () => {
      const wrapper = mountChat({
        turns: [
          deployQ,
          { role: 'human', text: '我的选择:\n- 选择部署方式 → Kubernetes', at: '2026-07-18T00:01:00Z' },
        ],
      })
      expect(submitChoices(wrapper)).toBeUndefined()
      wrapper.unmount()
    })

    it('turn_begin then turn_done without persisted catch-up does not flash the card', async () => {
      const wrapper = mountChat({ turns: [deployQ] })
      await pickAndSubmit(wrapper, 'Kubernetes')
      expect(wrapper.emitted('send')).toBeTruthy()
      expect(submitChoices(wrapper)).toBeUndefined()

      const sent = String(wrapper.emitted('send')![0][0])
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        discardLastQueued: () => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { id: 'srv-choice', text: sent },
      })
      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
      await flushPromises()
      expect(submitChoices(wrapper)).toBeUndefined()
      wrapper.unmount()
    })

    it('discardLastQueued after failed send restores the card', async () => {
      const wrapper = mountChat({ turns: [deployQ] })
      await pickAndSubmit(wrapper, 'Kubernetes')
      expect(submitChoices(wrapper)).toBeUndefined()

      const vm = wrapper.vm as unknown as { discardLastQueued: () => void }
      vm.discardLastQueued()
      await flushPromises()
      expect(submitChoices(wrapper)?.exists()).toBe(true)
      wrapper.unmount()
    })
  })

  // plan g2.1 / f1–f3: choice summary width chain — long answers wrap, short tags stay chips
  describe('choice summary long-answer wrap (g1.1/g1.2/g2.1)', () => {
    const longAnswer =
      '先设计 token 体系(--ci 语义层 + 清理硬编码 hex 与像素),再抽出共享组件与布局约束，不把长句撕出气泡。'
    const noSpaceToken =
      'Harness--dsw-alias-tokens-with-very-long-continuous-css-classname-and--ci-flag'

    it('long answer and question carry min-w-0 max-w-full overflow-wrap:anywhere (g1.1/g1.2)', () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text: `我的选择:\n- 重构路径怎么走? → ${longAnswer}`,
            at: '2026-08-30T00:00:00Z',
          },
        ],
      })
      const summary = wrapper.find('[data-testid="clarify-choice-summary"]')
      expect(summary.exists()).toBe(true)
      expect(summary.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))

      const row = wrapper.find('[data-testid="clarify-choice-row"]')
      expect(row.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))

      const q = row.find('.text-txt3')
      expect(q.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full', 'break-words']))
      expect(q.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)

      const answer = wrapper.find('[data-testid="clarify-choice-answer"]')
      expect(answer.exists()).toBe(true)
      expect(answer.text()).toBe(longAnswer)
      expect(answer.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full', 'break-words']))
      expect(answer.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)
      wrapper.unmount()
    })

    it('no-space token answer still gets overflow-wrap:anywhere (g2.1)', () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text: `我的选择:\n- 视觉风格的目标是什么? → ${noSpaceToken}`,
            at: '2026-08-30T00:00:00Z',
          },
        ],
      })
      const answer = wrapper.find('[data-testid="clarify-choice-answer"]')
      expect(answer.text()).toBe(noSpaceToken)
      expect(answer.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)
      expect(answer.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full', 'break-words']))
      wrapper.unmount()
    })

    it('multiple short answers remain independent tags with wrap constraints (f3/g2.1)', () => {
      const wrapper = mountChat({
        turns: [
          {
            role: 'human',
            text:
              '我的选择:\n- 这次「UI 统一/优化」覆盖哪些卡片? → Domain、Certificate、COS、TKE、CVM、CLB',
            at: '2026-08-30T00:00:00Z',
          },
        ],
      })
      const answers = wrapper.findAll('[data-testid="clarify-choice-answer"]')
      expect(answers).toHaveLength(6)
      expect(answers.map((a) => a.text())).toEqual([
        'Domain',
        'Certificate',
        'COS',
        'TKE',
        'CVM',
        'CLB',
      ])
      for (const a of answers) {
        expect(a.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full', 'break-words']))
        expect(a.classes().join(' ')).toMatch(/overflow-wrap:anywhere/)
      }
      const wrapBox = wrapper.find('[data-testid="clarify-choice-row"] .flex.flex-wrap')
      expect(wrapBox.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
      wrapper.unmount()
    })
  })

  describe('empty fail card + retry (plan g1.1 / g1.2 / g2.3)', () => {
    it('persisted empty agent shows failure card and retry (plan g1.1)', () => {
      const wrapper = mountChat({
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '', at: 't2' },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').text()).toContain('本轮没有输出')
      expect(wrapper.find('[data-testid="clarify-empty-fail-desc"]').text()).toContain(
        '可重试上一轮',
      )
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('success body has 已完成 and no retry (plan g1.2)', () => {
      const wrapper = mountChat({
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '好的，开始对齐。', at: 't2' },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('interrupted (已中断) is not an empty-fail retry card (plan g2.3)', () => {
      const wrapper = mountChat({
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '(已中断)', at: 't2', interrupted: true },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('done session keeps failure visible without clickable retry (plan g1.2)', () => {
      const wrapper = mountChat({
        done: true,
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '', at: 't2' },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('empty-fail buried under a later turn keeps card but no retry (retryLast trailing-only)', () => {
      const wrapper = mountChat({
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '', at: 't2' },
          { role: 'human', text: '下一条', at: 't3' },
          { role: 'agent', text: '好的，开始对齐。', at: 't4' },
        ],
      })
      expect(wrapper.findAll('[data-testid="clarify-empty-fail"]')).toHaveLength(1)
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="clarify-turn-completed"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('retry emits retry-last and does not change draft (plan g1.2)', async () => {
      const wrapper = mountChat({
        draft: 'keep-draft',
        turns: [
          { role: 'human', text: '做登录', at: 't1' },
          { role: 'agent', text: '(澄清回复失败:timeout)', at: 't2' },
        ],
      })
      expect(wrapper.find('[data-testid="clarify-empty-fail-desc"]').text()).toContain(
        '澄清回复失败',
      )
      await wrapper.get('[data-testid="clarify-empty-fail-retry"]').trigger('click')
      expect(wrapper.emitted('retry-last')).toBeTruthy()
      expect(wrapper.vm.$props.draft ?? wrapper.props('draft')).toBe('keep-draft')
      wrapper.unmount()
    })

    it('live empty turn_done keeps failure card (plan g1.1)', async () => {
      const wrapper = mountChat({ draft: '做登录' })
      const vm = wrapper.vm as unknown as {
        applyReviewFrame: (f: Record<string, unknown>) => void
        applyQueueState: (
          w: number,
          items: unknown[],
          busy?: boolean,
        ) => void
      }
      vm.applyReviewFrame({
        event: 'turn_begin',
        nodeId: 'react-1',
        item: { text: '做登录' },
      })
      vm.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
      vm.applyQueueState(0, [], false)
      await flushPromises()
      expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="clarify-empty-fail-retry"]').exists()).toBe(true)
      wrapper.unmount()
    })
  })
})
