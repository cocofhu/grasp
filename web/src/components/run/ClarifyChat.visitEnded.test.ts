// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import ClarifyChat from './ClarifyChat.vue'

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.replaceChildren()
})

type ChatApi = {
  restoreUnsentQueued: (
    sent: { text: string; images?: ClarifyImage[]; annotations?: ReactAnnotation[] },
    notice: string,
  ) => void
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

  it('shows the notice even when the composer has closed because the run ended', async () => {
    const { wrapper, chat, isActive, draft } = mountHost()
    await typeAndSend(wrapper, '还在吗')
    isActive.value = false
    await flushPromises()
    chat.value!.restoreUnsentQueued({ text: '还在吗' }, RESEND)
    await flushPromises()
    expect(draft.value).toBe('还在吗')
    expect(wrapper.find('[data-testid="clarify-send-notice"]').text()).toBe(RESEND)
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
})
