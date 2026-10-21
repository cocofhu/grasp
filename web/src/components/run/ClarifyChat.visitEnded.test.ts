// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import {
  clarifyTabId,
  initClarifyTab,
  putClarifyOutbox,
  resetClarifyTabForTest,
  sweepClarifyStash,
} from '@/lib/inbox/clarifyDraftStash'
import ClarifyChat from './ClarifyChat.vue'

enableAutoUnmount(afterEach)
// Ordinary tests run in a tab whose duplicate check is already done.
beforeEach(() => resetClarifyTabForTest({ checked: true }))
afterEach(() => {
  document.body.replaceChildren()
  localStorage.clear()
  sessionStorage.clear()
})

type ChatApi = {
  restoreUnsentQueued: (
    sent: {
      text?: string
      images?: ClarifyImage[]
      annotations?: ReactAnnotation[]
      clientId?: string
      at?: number
    },
    notice: string,
    opts?: { autoRetryOnResume?: boolean },
  ) => void
  applyReviewFrame: (frame: Record<string, unknown>) => void
}
const AUTO = '运行已结束，这条消息没有发送；续跑后会自动发送'
const KEPT = '运行已结束，草稿已保留；续跑后会回到输入框'
/** This tab's stash key (per user + tab + run + node). */
const STASH_KEY = 'grasp-clarify-stash:anon:run-1:react-1'
const OUTBOX_KEY = 'grasp-clarify-outbox:anon:run-1:react-1'

/** What the host (onClarifySend) records for a send from this tab. */
function outbox(id: string, text: string, images: ClarifyImage[] = [], annotations: ReactAnnotation[] = []) {
  putClarifyOutbox('run-1', 'react-1', id, { text, images, annotations })
}

const RESEND = '运行已结束，这条消息没有发送；续跑后请重新发送'

function mountHost(active = true) {
  const draft = ref('')
  const attachments = ref<ClarifyImage[]>([])
  const annotations = ref<ReactAnnotation[]>([])
  const chat = ref<ChatApi | null>(null)
  const isActive = ref(active)
  const Host = defineComponent({
    setup() {
      return () =>
        h(ClarifyChat, {
          ref: chat,
          runId: 'run-1',
          nodeId: 'react-1',
          iteration: 1,
          turns: [],
          active: isActive.value,
          draft: draft.value,
          'onUpdate:draft': (v: string) => (draft.value = v),
          attachments: attachments.value,
          'onUpdate:attachments': (v: ClarifyImage[]) => (attachments.value = v),
          annotations: annotations.value,
          'onUpdate:annotations': (v: ReactAnnotation[]) => (annotations.value = v),
        })
    },
  })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(Host, {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true, ClarifyDemoFrame: true } },
  })
  return { wrapper, draft, attachments, chat, isActive }
}

async function typeAndSend(wrapper: ReturnType<typeof mountHost>['wrapper'], text: string) {
  const input = wrapper.find('[data-testid="clarify-input"]')
  await input.setValue(text)
  await wrapper.find('[data-testid="clarify-send-icon"]').trigger('click')
  await flushPromises()
}

describe('ClarifyChat: reply refused because the run ended (clarify_visit_ended)', () => {
  it('drops the optimistic row, refills the composer, keeps focus and shows the notice', async () => {
    const { wrapper, draft, chat } = mountHost()
    await typeAndSend(wrapper, '请补充登录页')
    expect(draft.value).toBe('')
    expect(wrapper.findAll('[data-testid="clarify-queue-item"]')).toHaveLength(1)
    const sent = wrapper.findComponent(ClarifyChat).emitted('send')!
    expect(sent[0][0]).toBe('请补充登录页')

    chat.value!.restoreUnsentQueued({ text: '请补充登录页' }, RESEND)
    await flushPromises()

    expect(draft.value).toBe('请补充登录页')
    const input = wrapper.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe('请补充登录页'.length)
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(RESEND)
    // No pending-send row and no error reply bubble.
    expect(wrapper.findAll('[data-testid="clarify-queue-item"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="clarify-confirm-error"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="clarify-input"]').length).toBe(1)
  })

  it('never overwrites text typed after sending; puts the old text first and keeps the caret', async () => {
    const { wrapper, draft, chat } = mountHost()
    await typeAndSend(wrapper, '第一条')
    const input = wrapper.find('[data-testid="clarify-input"]')
    await input.setValue('新打的字')
    const el = input.element as HTMLTextAreaElement
    el.focus()
    el.setSelectionRange(4, 4)

    chat.value!.restoreUnsentQueued({ text: '第一条' }, RESEND)
    await flushPromises()

    expect(draft.value).toBe('第一条\n新打的字')
    expect(document.activeElement).toBe(el)
    expect(el.selectionStart).toBe('第一条\n'.length + 4)
  })

  it('restores attachments without duplicating ones already in the composer', async () => {
    const { wrapper, attachments, chat } = mountHost()
    const img: ClarifyImage = { data: 'aGk=', mimeType: 'image/png', name: 'a.png' }
    attachments.value = [img]
    await flushPromises()
    await typeAndSend(wrapper, '看图')
    expect(attachments.value).toEqual([])
    const other: ClarifyImage = { data: 'eW8=', mimeType: 'image/png', name: 'b.png' }
    attachments.value = [other]
    await flushPromises()

    chat.value!.restoreUnsentQueued({ text: '看图', images: [img] }, RESEND)
    await flushPromises()
    expect(attachments.value.map((a) => a.name)).toEqual(['a.png', 'b.png'])
  })

  it('composer already closed: stashes by run+node and refills it once the run resumes', async () => {
    const { wrapper, chat, isActive, draft } = mountHost()
    await typeAndSend(wrapper, '还在吗')
    isActive.value = false
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '还在吗' }, RESEND)
    await flushPromises()
    expect(draft.value).toBe('')
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('还在吗')
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(RESEND)

    isActive.value = true
    await flushPromises()
    expect(draft.value).toBe('还在吗')
    // Resumed and refilled: the "resend after resume" notice is done (S14).
    expect(wrapper.find('[data-testid="clarify-send-notice"]').exists()).toBe(false)
    // Kept until it is actually sent again.
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('还在吗')
    await wrapper.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
  })

  it('composer open: the refill is stashed too, mirrors edits, and clears when emptied', async () => {
    const { wrapper, chat, draft } = mountHost()
    await typeAndSend(wrapper, '刷新也不能丢')
    chat.value!.restoreUnsentQueued({ text: '刷新也不能丢' }, RESEND)
    await flushPromises()
    expect(draft.value).toBe('刷新也不能丢')
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('刷新也不能丢')
    await wrapper.find('[data-testid="clarify-input"]').setValue('刷新也不能丢！')
    await flushPromises()
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('刷新也不能丢！')
    await wrapper.find('[data-testid="clarify-input"]').setValue('')
    await flushPromises()
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
  })

  it('autoRetryOnResume: restores nothing, only says it will be sent after resume', async () => {
    const { wrapper, chat, draft } = mountHost()
    await typeAndSend(wrapper, '用户消息')
    chat.value!.restoreUnsentQueued({ text: '' }, AUTO, { autoRetryOnResume: true })
    await flushPromises()
    expect(draft.value).toBe('')
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(AUTO)
  })

  it('WS turn-start drop matching this tab\'s outbox: no error bubble, outbox text back in the composer', async () => {
    const { wrapper, chat, draft } = mountHost()
    await typeAndSend(wrapper, '排在后面的回复')
    outbox('cm-1', '排在后面的回复')
    chat.value!.applyReviewFrame({ event: 'queue_state', nodeId: 'react-1', waiting: 0, items: [], busy: true })
    chat.value!.applyReviewFrame({ event: 'turn_begin', nodeId: 'react-1', item: { id: 'srv-1', text: '排在后面的回复' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-scroller"]').text()).toContain('排在后面的回复')
    chat.value!.applyReviewFrame({
      event: 'error',
      nodeId: 'react-1',
      message: '运行已结束，这条消息没有发送',
      code: 'clarify_visit_ended',
      autoRetryOnResume: false,
      interrupted: false,
      iteration: 1,
      clientMessageId: 'cm-1',
    })
    await flushPromises()
    const scroller = wrapper.find('[data-testid="clarify-scroller"]').text()
    expect(scroller).not.toContain('运行已结束')
    expect(scroller).not.toContain('排在后面的回复')
    expect(draft.value).toBe('排在后面的回复')
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(RESEND)
    // Consumed: a repeated frame cannot refill twice.
    expect(sessionStorage.getItem(OUTBOX_KEY)).toBeNull()
  })

  it('WS drop for a message another tab/viewer sent: no refill, no stash, no notice', async () => {
    const { wrapper, chat, draft } = mountHost()
    outbox('cm-mine', '我自己的')
    chat.value!.applyReviewFrame({ event: 'turn_begin', nodeId: 'react-1', item: { id: 'srv-9', text: '别人的回复' } })
    await flushPromises()
    chat.value!.applyReviewFrame({
      event: 'error',
      nodeId: 'react-1',
      code: 'clarify_visit_ended',
      autoRetryOnResume: false,
      clientMessageId: 'cm-other',
    })
    await flushPromises()
    expect(draft.value).toBe('')
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
    expect(wrapper.find('[data-testid="clarify-send-notice"]').exists()).toBe(false)
    // The dropped turn's empty slot is gone for every viewer.
    expect(wrapper.find('[data-testid="clarify-scroller"]').text()).not.toContain('别人的回复')
  })

  it('WS frame without clientMessageId (older backend): never refills from a broadcast dropped', async () => {
    const { wrapper, chat, draft } = mountHost()
    chat.value!.applyReviewFrame({
      event: 'error',
      nodeId: 'react-1',
      code: 'clarify_visit_ended',
      dropped: { text: '从别处发出的回复', annotations: [] },
    })
    await flushPromises()
    expect(draft.value).toBe('')
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
    expect(wrapper.find('[data-testid="clarify-send-notice"]').exists()).toBe(false)
  })

  it('WS match with only an outbox copy (after refresh): ref-only attachments come back', async () => {
    const { chat, draft, attachments } = mountHost()
    outbox('cm-2', '', [{ ref: 'blob:abc', mimeType: 'image/png', name: 'a.png' }])
    chat.value!.applyReviewFrame({
      type: 'review',
      event: 'error',
      nodeId: 'react-1',
      code: 'clarify_visit_ended',
      autoRetryOnResume: false,
      interrupted: false,
      iteration: 1,
      clientMessageId: 'cm-2',
    })
    await flushPromises()
    expect(draft.value).toBe('')
    expect(attachments.value).toEqual([{ ref: 'blob:abc', mimeType: 'image/png', name: 'a.png' }])
  })

  it('WS match after turn_begin prefers the local image copy over the ref-only server item', async () => {
    const { wrapper, chat, attachments } = mountHost()
    const local: ClarifyImage = { data: 'aGk=', mimeType: 'image/png', name: 'a.png' }
    attachments.value = [local]
    await flushPromises()
    await typeAndSend(wrapper, '看图')
    outbox('cm-3', '看图', [local])
    chat.value!.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'react-1',
      item: { id: 'srv-2', text: '看图', images: [{ ref: 'blob:abc', mimeType: 'image/png' }] },
    })
    chat.value!.applyReviewFrame({
      event: 'error',
      nodeId: 'react-1',
      code: 'clarify_visit_ended',
      autoRetryOnResume: false,
      interrupted: false,
      clientMessageId: 'cm-3',
    })
    await flushPromises()
    expect(attachments.value).toEqual([local])
  })

  it('turn_done for an outbox message drops its outbox entry', async () => {
    const { wrapper, chat } = mountHost()
    await typeAndSend(wrapper, '正常回复')
    outbox('cm-4', '正常回复')
    chat.value!.applyReviewFrame({ event: 'turn_begin', nodeId: 'react-1', item: { id: 'srv-4', text: '正常回复' } })
    chat.value!.applyReviewFrame({ event: 'turn_done', nodeId: 'react-1' })
    await flushPromises()
    expect(sessionStorage.getItem(OUTBOX_KEY)).toBeNull()
  })

  it('WS error frame for the system first message: removes its bubbles, restores nothing', async () => {
    const { wrapper, chat, draft } = mountHost()
    chat.value!.applyReviewFrame({ event: 'turn_begin', nodeId: 'react-1', item: { text: '做一个待办清单' } })
    await flushPromises()
    chat.value!.applyReviewFrame({
      event: 'error',
      nodeId: 'react-1',
      message: '运行已结束，这条消息没有发送',
      code: 'clarify_visit_ended',
      autoRetryOnResume: true,
      dropped: {},
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-scroller"]').text()).not.toContain('运行已结束')
    expect(draft.value).toBe('')
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(AUTO)
  })

  it('a draft in progress when the run ends is kept (stash + notice) and returns on resume', async () => {
    const { wrapper, isActive, draft } = mountHost()
    await wrapper.find('[data-testid="clarify-input"]').setValue('正在打的草稿')
    isActive.value = false
    await flushPromises()
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('正在打的草稿')
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(KEPT)
    // Simulate a refresh: the in-memory draft is gone, the stash is not.
    draft.value = ''
    isActive.value = true
    await flushPromises()
    expect(draft.value).toBe('正在打的草稿')
  })

  it('stash is restored on mount after a refresh when the composer is open', async () => {
    sessionStorage.setItem(
      STASH_KEY,
      JSON.stringify({ text: '刷新前的草稿', images: [], annotations: [], at: Date.now() }),
    )
    const { draft } = mountHost()
    await flushPromises()
    expect(draft.value).toBe('刷新前的草稿')
    // Still kept until sent or cleared (another refresh must not lose it).
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('刷新前的草稿')
  })

  it('clears the notice on the next send', async () => {
    const { wrapper, chat } = mountHost()
    await typeAndSend(wrapper, 'x')
    chat.value!.restoreUnsentQueued({ text: 'x' }, RESEND)
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-send-notice"]').exists()).toBe(true)
    await wrapper.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-send-notice"]').exists()).toBe(false)
  })

  it('B4: nothing that another tab could refill is written to localStorage', async () => {
    const { wrapper, chat, isActive } = mountHost()
    await typeAndSend(wrapper, '只给发送的标签页')
    isActive.value = false
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '只给发送的标签页' }, RESEND)
    await flushPromises()
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('只给发送的标签页')
    for (let i = 0; i < localStorage.length; i++) expect(localStorage.key(i)).not.toMatch(/^grasp-clarify-/)
  })

  it('B4: a stash left in localStorage by an earlier build is removed, never refilled', async () => {
    localStorage.setItem(
      'grasp-clarify-stash:anon:old-tab:run-1:react-1',
      JSON.stringify({ text: '旧版本留下的', images: [], annotations: [], at: Date.now() }),
    )
    sweepClarifyStash(Date.now(), true)
    const { draft } = mountHost()
    await flushPromises()
    expect(draft.value).toBe('')
    expect(localStorage.getItem('grasp-clarify-stash:anon:old-tab:run-1:react-1')).toBeNull()
  })

  it('B4 + LeaderBot: once sent, a refresh does not bring the text back', async () => {
    const { wrapper, chat } = mountHost()
    await typeAndSend(wrapper, '发出去就不要再回来')
    chat.value!.restoreUnsentQueued({ text: '发出去就不要再回来' }, RESEND)
    await flushPromises()
    expect(sessionStorage.getItem(STASH_KEY)).not.toBeNull()
    await wrapper.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
    wrapper.unmount()
    // "Refresh": a new mount of the same tab.
    const again = mountHost()
    await flushPromises()
    expect(again.draft.value).toBe('')
  })

  it('S22: a duplicated tab (same id answered by a live tab) drops the copied stash and outbox', async () => {
    resetClarifyTabForTest()
    sessionStorage.setItem('grasp-clarify-tab-id', 'dup-id')
    sessionStorage.setItem(
      STASH_KEY,
      JSON.stringify({ text: '复制过来的', images: [], annotations: [], at: Date.now() }),
    )
    outbox('cm-dup', '复制过来的')
    // The original tab, still open, owns "dup-id".
    const original = new BroadcastChannel('grasp-clarify-tab')
    original.onmessage = (e) => {
      if (e.data?.type === 'ping' && e.data.id === 'dup-id') original.postMessage({ type: 'taken', id: 'dup-id' })
    }
    try {
      const { draft } = mountHost()
      await initClarifyTab()
      await flushPromises()
      expect(draft.value).toBe('')
      expect(sessionStorage.getItem(STASH_KEY)).toBeNull()
      expect(sessionStorage.getItem(OUTBOX_KEY)).toBeNull()
      expect(clarifyTabId()).not.toBe('dup-id')
    } finally {
      original.close()
      resetClarifyTabForTest({ checked: true })
    }
  })

  it('S22: a refreshed tab (nobody answers its id) keeps its stash and refills', async () => {
    resetClarifyTabForTest()
    sessionStorage.setItem('grasp-clarify-tab-id', 'my-id')
    sessionStorage.setItem(
      STASH_KEY,
      JSON.stringify({ text: '刷新后还是我的', images: [], annotations: [], at: Date.now() }),
    )
    try {
      const { draft } = mountHost()
      expect(draft.value).toBe('')
      await initClarifyTab()
      await flushPromises()
      expect(draft.value).toBe('刷新后还是我的')
      expect(clarifyTabId()).toBe('my-id')
    } finally {
      resetClarifyTabForTest({ checked: true })
    }
  })

  it('S19: refusals arriving out of order come back in send order, ahead of new typing', async () => {
    const { wrapper, chat, draft } = mountHost()
    await wrapper.find('[data-testid="clarify-input"]').setValue('后来打的')
    await flushPromises()
    // The later send's 409 arrives first, then the earlier turn-start drop.
    // (Each refusal is its own network event, so each lands in its own tick.)
    chat.value!.restoreUnsentQueued({ text: '第二条', at: 2000 }, RESEND)
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '第一条', at: 1000 }, RESEND)
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '第三条', at: 3000 }, RESEND)
    await flushPromises()
    expect(draft.value).toBe('第一条\n第二条\n第三条\n后来打的')
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).text).toBe('第一条\n第二条\n第三条\n后来打的')
  })

  it('S19: same order when the composer is closed (stash) and on resume', async () => {
    const { chat, draft, isActive } = mountHost()
    isActive.value = false
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '乙', at: 2000 }, RESEND)
    chat.value!.restoreUnsentQueued({ text: '甲', at: 1000 }, RESEND)
    await flushPromises()
    isActive.value = true
    await flushPromises()
    expect(draft.value).toBe('甲\n乙')
  })

  it('S21: inline images over the stash budget are reported, not silently lost', async () => {
    const { chat, isActive, wrapper } = mountHost()
    isActive.value = false
    await flushPromises()
    const big: ClarifyImage = { data: 'x'.repeat(300 * 1024), mimeType: 'image/png', name: 'big.png' }
    chat.value!.restoreUnsentQueued({ text: '带大图', images: [big] }, RESEND)
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-stash-notice"]').text()).toBe(
      '有 1 张图片太大，没能一起保留，续跑后请重新添加',
    )
    expect(JSON.parse(sessionStorage.getItem(STASH_KEY)!).images).toEqual([])
    // Still said after resume (the image is not back), gone once sent.
    isActive.value = true
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-stash-notice"]').exists()).toBe(true)
    await wrapper.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-stash-notice"]').exists()).toBe(false)
  })
})
