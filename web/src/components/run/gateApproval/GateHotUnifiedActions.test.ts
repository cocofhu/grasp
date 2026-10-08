// @vitest-environment happy-dom
import { reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import { gateApprovalKey, type GateApprovalState } from './gateApprovalContext'
import GateHotUnifiedActions from './GateHotUnifiedActions.vue'

function mountHot(
  patch: Record<string, unknown> = {},
  locale: 'zh-CN' | 'en' = 'zh-CN',
) {
  const cancelReactRevise = vi.fn()
  const sendHotReject = vi.fn()
  const recordFeedbackIssue = vi.fn()
  const onComposerPass = vi.fn()
  const s = reactive({
    reactError: null,
    isMobile: false,
    reactText: '',
    reactImages: [],
    reactAnnotations: [],
    reactSending: false,
    pickedElementImage: null,
    canReactRevise: true,
    reactThinking: false,
    reactQueued: [],
    hotRejectAllowEmpty: false,
    canSubmitReact: false,
    showHotReject: true,
    showHotPass: true,
    canRecordIssue: false,
    composerPassDisabled: false,
    passAction: { id: 'approve' },
    actionSubmitting: false,
    resolved: null,
    composerRejectLabel: locale === 'en' ? 'Send' : '发送',
    actionButtonTitle: () => '',
    cancelReactRevise,
    sendHotReject,
    recordFeedbackIssue,
    onComposerPass,
    reactQueueNotice: null,
    reactQueueToast: null,
    cancelReactQueuedItem: vi.fn(),
    editReactQueuedItem: vi.fn(),
    reorderReactQueuedItems: vi.fn(),
    reactStreamText: '',
    reactStreamThought: '',
    reactStreamTools: [],
    reactInterrupted: false,
    reactStreamCompletedAt: null,
    ...patch,
  }) as unknown as GateApprovalState
  const messages =
    locale === 'en' ? { en: { ...commonEn, ...pagesEn } } : { 'zh-CN': { ...common, ...pages } }
  const i18n = createI18n({ legacy: false, locale, messages })
  const wrapper = mount(GateHotUnifiedActions, {
    props: { layout: 'content-fit' },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ParagraphInput: true, GateReactStreamPanel: true, PendingSendQueuePanel: true },
      provide: {
        [gateApprovalKey as symbol]: {
          s,
          productEditorRef: ref(null),
          feedbackChatRef: ref(null),
          gateStageEl: ref(null),
        },
      },
    },
  })
  return { wrapper, s, cancelReactRevise, sendHotReject, recordFeedbackIssue, onComposerPass }
}

describe('GateHotUnifiedActions hot toolbar', () => {
  it('idle empty draft shows a faded send icon, record, and confirm outside the box', async () => {
    const { wrapper } = mountHot()
    await flushPromises()
    const send = wrapper.get('[data-testid="review-composer-send"]')
    expect((send.element as HTMLButtonElement).disabled).toBe(true)
    expect(send.attributes('aria-label')).toBe('发送')
    expect(send.attributes('title')).toBe('发送')
    expect(send.text()).not.toContain('发送')
    expect(send.findComponent({ name: 'Icon' }).props('name')).toBe('send')
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="paragraph-input-attach"]').exists()).toBe(true)
    const footer = wrapper.get('[data-testid="composer-shell-footer"]')
    const record = footer.get('[data-testid="review-record-issue"]')
    expect(record.text()).toBe('记入意见')
    expect((record.element as HTMLButtonElement).disabled).toBe(true)
    expect(footer.get('[data-testid="review-composer-pass"]').text()).toContain('确认并流转')
    expect(wrapper.get('[data-testid="composer-shell-box"]').find('[data-testid="review-composer-pass"]').exists()).toBe(
      false,
    )
    wrapper.unmount()
  })

  it('shows stop alone when busy with nothing to send, and beside send when a draft exists', async () => {
    const busy = mountHot({ reactThinking: true })
    await flushPromises()
    expect(busy.wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(false)
    const stop = busy.wrapper.get('[data-testid="gate-react-cancel"]')
    expect(stop.attributes('aria-label')).toBe('Cancel')
    expect(stop.attributes('title')).toBe('Cancel')
    expect(stop.text()).not.toContain('Cancel')
    expect(stop.findComponent({ name: 'Icon' }).props('name')).toBe('stop')
    await stop.trigger('click')
    expect(busy.cancelReactRevise).toHaveBeenCalled()
    busy.wrapper.unmount()

    const drafted = mountHot({ reactThinking: true, reactText: '改一下', canSubmitReact: true, canRecordIssue: true })
    await flushPromises()
    expect(drafted.wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    const send = drafted.wrapper.get('[data-testid="review-composer-send"]')
    expect((send.element as HTMLButtonElement).disabled).toBe(false)
    await send.trigger('click')
    expect(drafted.sendHotReject).toHaveBeenCalled()
    const record = drafted.wrapper.get('[data-testid="review-record-issue"]')
    expect((record.element as HTMLButtonElement).disabled).toBe(false)
    await record.trigger('click')
    expect(drafted.recordFeedbackIssue).toHaveBeenCalled()
    drafted.wrapper.unmount()

    const allowEmpty = mountHot({ reactThinking: true, hotRejectAllowEmpty: true })
    await flushPromises()
    expect(allowEmpty.wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    expect((allowEmpty.wrapper.get('[data-testid="review-composer-send"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    allowEmpty.wrapper.unmount()
  })

  it('uses English send, record, and confirm copy', async () => {
    const { wrapper } = mountHot({ reactText: 'please change', canSubmitReact: true }, 'en')
    await flushPromises()
    expect(wrapper.get('[data-testid="review-composer-send"]').attributes('aria-label')).toBe('Send')
    expect(wrapper.get('[data-testid="review-record-issue"]').text()).toBe('Record feedback')
    expect(wrapper.get('[data-testid="review-composer-pass"]').text()).toContain('Confirm & continue')
    wrapper.unmount()
  })
})
