// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import type { ClarifyImage, ClarifyTurn, ReactAnnotation } from '@/lib/shared/types'
import ClarifyChat from './ClarifyChat.vue'
import { LIVE_CARD_HOST, createLiveStore, type LiveCardHost } from '@/lib/inbox/liveVariants'

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.replaceChildren()
})

function liveHost(state = 'ready'): LiveCardHost {
  const live = createLiveStore()
  live.apply({ sid: 'sid001', state, summary: '页面候选', variants: [{ n: 1, label: '入场' }, { n: 2, label: '聚焦' }] })
  live.setView('sid001', { current: 1, mode: 'inplace' })
  return { store: live.store, interactive: true, command: vi.fn() }
}

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
  coldSession?: boolean
  draft?: string
  reviewMode?: boolean
  confirmError?: string | null
  confirmCanAbort?: boolean
  annotateEnabled?: boolean
  annotations?: ReactAnnotation[]
  attachments?: { data: string; mimeType: string; name?: string }[]
  seedHumanText?: string
  seedHumanImages?: { data: string; mimeType: string; name?: string }[]
  hideFinish?: boolean
  sendLabel?: string
  sendRequest?: (text: string, images: ClarifyImage[], annotations: ReactAnnotation[]) => Promise<boolean>
  liveHost?: LiveCardHost
  observe?: boolean
  stopping?: boolean
} = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    attachTo: document.body,
    props: {
      runId: 'run-1',
      nodeId: 'react-1',
      iteration: 1,
      turns: opts.turns === undefined ? [] : opts.turns,
      done: opts.done ?? false,
      active: opts.active ?? true,
      coldSession: opts.coldSession ?? false,
      draft: opts.draft ?? '',
      reviewMode: opts.reviewMode ?? false,
      confirmError: opts.confirmError ?? null,
      confirmCanAbort: opts.confirmCanAbort ?? false,
      annotateEnabled: opts.annotateEnabled ?? false,
      annotations: opts.annotations ?? [],
      attachments: opts.attachments ?? [],
      seedHumanText: opts.seedHumanText ?? '',
      seedHumanImages: opts.seedHumanImages ?? [],
      hideFinish: opts.hideFinish ?? false,
      sendLabel: opts.sendLabel,
      sendRequest: opts.sendRequest,
      observe: opts.observe ?? false,
      stopping: opts.stopping ?? false,
    },
    global: {
      plugins: [i18n],
      provide: opts.liveHost ? { [LIVE_CARD_HOST as symbol]: opts.liveHost } : {},
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


const takeoverTurns: ClarifyTurn[] = [
  { role: 'human', text: '', at: '2026-10-21T00:00:00Z' },
  { role: 'agent', text: '已确认改动范围：LoginView.vue。\n\n---\n已中断自动执行，本节点转为人工接管（不会再自动执行）。', at: '2026-10-21T00:00:01Z' },
]

describe('ClarifyChat observe mode (auto node, read-only + Stop)', () => {
  beforeEach(() => sessionStorage.clear())

  it('disables input and attach, shows only Stop, hides confirm and hint', () => {
    const wrapper = mountChat({ reviewMode: true, observe: true, turns: [{ role: 'agent', text: '正在实现', at: '2026-10-21T00:00:00Z' }] })
    const input = wrapper.get('[data-testid="clarify-input"]')
    expect((input.element as HTMLTextAreaElement).disabled).toBe(true)
    expect(input.attributes('placeholder')).toBe('自动运行中，停止后可接管对话')
    expect((wrapper.get('[data-testid="clarify-attach-btn"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="clarify-observe-stop"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="clarify-confirm-hint"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="clarify-send-icon"]').exists()).toBe(false)
  })

  it('Stop emits takeover once; stopping shows a spinner and blocks repeats', async () => {
    const wrapper = mountChat({ reviewMode: true, observe: true })
    await wrapper.get('[data-testid="clarify-observe-stop"]').trigger('click')
    expect(wrapper.emitted('takeover')).toHaveLength(1)
    await wrapper.setProps({ stopping: true })
    const stop = wrapper.get('[data-testid="clarify-observe-stop"]')
    expect((stop.element as HTMLButtonElement).disabled).toBe(true)
    expect(stop.attributes('aria-busy')).toBe('true')
    expect(wrapper.get('[data-testid="clarify-input"]').attributes('placeholder')).toBe('正在停止…')
    await stop.trigger('click')
    expect(wrapper.emitted('takeover')).toHaveLength(1)
  })

  it('hides the blank human turn that opens an auto stream', () => {
    const wrapper = mountChat({ observe: true, turns: [{ role: 'human', text: '', at: '2026-10-21T00:00:00Z' }, { role: 'agent', text: '开始执行', at: '2026-10-21T00:00:01Z' }] })
    expect(wrapper.findAll('[data-turn-role="human"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="chat-message-rail-mark"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('开始执行')
  })
})

describe('ClarifyChat takeover turns (re-review after Stop)', () => {
  beforeEach(() => sessionStorage.clear())

  it('renders narration, the divider and the takeover note with an enabled 确认并流转', async () => {
    const wrapper = mountChat({ reviewMode: true, turns: takeoverTurns })
    await flushPromises()
    expect(wrapper.text()).toContain('已确认改动范围')
    expect(wrapper.text()).toContain('转为人工接管')
    expect(wrapper.find('.clarify-turn hr, [data-turn-role="agent"] hr').exists()).toBe(true)
    const confirm = wrapper.get('[data-testid="clarify-confirm-flow"]')
    expect(confirm.text()).toContain('确认并流转')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
    expect((wrapper.get('[data-testid="clarify-input"]').element as HTMLTextAreaElement).disabled).toBe(false)
    expect(wrapper.find('[data-testid="clarify-observe-stop"]').exists()).toBe(false)
    await confirm.trigger('click')
    expect(wrapper.emitted('finish')).toHaveLength(1)
  })

  it('lets the user send a revision on the taken-over node', async () => {
    const wrapper = mountChat({ reviewMode: true, turns: takeoverTurns })
    await wrapper.get('[data-testid="clarify-input"]').setValue('主按钮文案改成「保存」')
    await wrapper.find('[data-testid="clarify-send-icon"], [data-testid="clarify-send-label"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('send')?.[0]?.[0]).toBe('主按钮文案改成「保存」')
  })
})

describe('ClarifyChat takeover opening turn (turns[0].events = auto phase)', () => {
  beforeEach(() => sessionStorage.clear())

  it('continues the auto-phase timeline with the existing thought / tool rows before the narration', () => {
    const wrapper = mountChat({
      reviewMode: true,
      turns: [{
        role: 'agent',
        text: '已中断自动执行，本节点转为人工接管（不会再自动执行）。',
        at: '2026-10-21T00:00:01Z',
        events: [
          { t: 1, kind: 'thought', text: '先看登录页结构' },
          { t: 2, kind: 'tool_call', title: 'Read LoginView.vue', status: 'completed' },
          { t: 3, kind: 'tool_call', title: 'Edit LoginView.vue', status: 'completed' },
          { t: 4, kind: 'message', text: '表单布局已调整一半' },
        ],
      }],
    })
    const agentRows = wrapper.findAll('[data-turn-role="agent"]')
    expect(agentRows).toHaveLength(2)
    expect(agentRows[0]!.find('[data-testid="clarify-thought"]').exists()).toBe(true)
    expect(agentRows[0]!.find('[data-testid="agent-tool-group"]').exists()).toBe(true)
    expect(agentRows[0]!.get('[data-testid="clarify-agent-message"]').text()).toContain('表单布局已调整一半')
    expect(agentRows[1]!.get('[data-testid="clarify-agent-message"]').text()).toContain('转为人工接管')
    expect(wrapper.find('[data-testid="clarify-empty-fail"]').exists()).toBe(false)
  })

  it('without events the opening turn renders as before (single narration row)', () => {
    const wrapper = mountChat({ reviewMode: true, turns: [{ role: 'agent', text: '已中断自动执行。', at: '2026-10-21T00:00:01Z' }] })
    expect(wrapper.findAll('[data-turn-role="agent"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="agent-tool-group"]').exists()).toBe(false)
  })
})
