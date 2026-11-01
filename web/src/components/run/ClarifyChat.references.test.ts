// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const search = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api/api', async (orig) => {
  const m = await orig<typeof import('@/lib/api/api')>()
  return { ...m, api: { ...m.api, searchRunNodeReferences: search } }
})

import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyChat from './ClarifyChat.vue'
import ReviewComposer from './ReviewComposer.vue'
import { clearDialogues } from '@/lib/chat/dialogueStore'

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.replaceChildren()
  clearDialogues()
})

const session = { kind: 'solo', target: 'sess-1', title: '登录重构', status: 'running', lastActiveAt: '2026-10-30T10:00:00Z' }

beforeEach(() => {
  search.mockReset()
  search.mockImplementation(async (_run: string, _node: string, p: any) =>
    p.lane === 'solo' ? { lane: 'solo', items: [session] } : { lane: 'github', items: [], source: 'none' },
  )
})

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })
}

function mountChat(props: Record<string, unknown> = {}) {
  return mount(ClarifyChat, {
    attachTo: document.body,
    props: { runId: 'run-1', nodeId: 'react-1', iteration: 1, turns: [], done: false, active: true, draft: '', ...props },
    global: { plugins: [i18n()], stubs: { Icon: true, ClarifyDemoFrame: true, AppModal: true, RouterLink: { props: ['to'], template: '<a><slot /></a>' } } },
  })
}

async function type(w: VueWrapper<any>, selector: string, value: string) {
  const ta = w.find(selector)
  const el = ta.element as HTMLTextAreaElement
  await ta.setValue(value)
  el.setSelectionRange(value.length, value.length)
  await ta.trigger('input')
  await new Promise((r) => setTimeout(r, 160))
  await flushPromises()
}

async function pickSession(w: VueWrapper<any>, selector: string) {
  await type(w, selector, '@登')
  expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
  await w.find('[data-testid="solo-reference-option-sess-1"]').trigger('click')
  await flushPromises()
}

describe('ClarifyChat @ references', () => {
  it('searches the run node and sends the picked reference', async () => {
    const w = mountChat()
    await pickSession(w, '[data-testid="clarify-input"]')
    expect(search).toHaveBeenCalledWith('run-1', 'react-1', expect.objectContaining({ lane: 'solo' }), expect.anything())
    expect(w.find('[data-testid="clarify-reference-chips"]').text()).toContain('登录重构')
    await w.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    const sent = w.emitted('send')![0]!
    expect(sent[0]).toContain('@登录重构')
    expect(sent[4]).toEqual([{ kind: 'solo', target: 'sess-1' }])
  })

  it('keeps the text and chip when the send request is refused', async () => {
    const sendRequest = vi.fn(async () => false)
    const w = mountChat({ sendRequest })
    await pickSession(w, '[data-testid="clarify-input"]')
    await w.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(sendRequest).toHaveBeenCalledWith(expect.stringContaining('@登录重构'), [], [], expect.any(String), [{ kind: 'solo', target: 'sess-1' }])
    expect((w.find('[data-testid="clarify-input"]').element as HTMLTextAreaElement).value).toContain('@登录重构')
    expect(w.find('[data-testid="clarify-reference-chips"]').exists()).toBe(true)
  })

  it('sends plain replies without a references argument', async () => {
    const w = mountChat({ draft: '普通回复' })
    await w.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(w.emitted('send')![0]).toHaveLength(4)
  })

  it('Enter without a highlighted row still sends', async () => {
    const w = mountChat()
    await type(w, '[data-testid="clarify-input"]', '看看 @登')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    await w.find('[data-testid="clarify-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(w.emitted('send')![0]![0]).toBe('看看 @登')
  })

  it('opens the picker from the + menu', async () => {
    const w = mountChat()
    await w.find('[data-testid="clarify-attach-btn"]').trigger('click')
    expect(w.find('[data-testid="clarify-plus-menu"]').exists()).toBe(true)
    await w.find('[data-testid="clarify-plus-session"]').trigger('click')
    await new Promise((r) => setTimeout(r, 160))
    await flushPromises()
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    expect(w.find('[data-testid="solo-reference-tab-solo"]').classes()).toContain('ref-picker__tab--on')
  })

  it.each([
    ['referencesEnabled=false', { referencesEnabled: false }],
    ['an inactive node', { active: false }],
    ['a finished node', { done: true }],
  ])('does not open the picker for %s', async (_label, props) => {
    const w = mountChat(props)
    const ta = w.find('[data-testid="clarify-input"]')
    if (!ta.exists() || (ta.element as HTMLTextAreaElement).disabled) {
      expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
      return
    }
    await type(w, '[data-testid="clarify-input"]', '@登')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    expect(search).not.toHaveBeenCalled()
  })

  it('renders reference chips on human turns', async () => {
    const w = mountChat({
      turns: [{ role: 'human', text: '@登录重构 看下', at: '2026-10-30T10:00:00Z', references: [{ id: 'r1', kind: 'solo', target: 'sess-1', title: '登录重构' }] }],
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-turn-references"]').text()).toContain('登录重构')
  })
})

describe('ReviewComposer gate @ references', () => {
  function mountGate(props: Record<string, unknown> = {}) {
    return mount(ReviewComposer, {
      attachTo: document.body,
      props: { mode: 'gate', runId: 'run-1', nodeId: 'gate-1', canReject: true, canPass: false, draft: '', ...props },
      global: { plugins: [i18n()], stubs: { Icon: true, AnnotationChip: true, ClarifyChat: true } },
    })
  }

  it('searches the gate node and emits references with the revise', async () => {
    const w = mountGate({ 'onUpdate:draft': (v: string) => w.setProps({ draft: v }) })
    await pickSession(w, '[data-testid="paragraph-input"]')
    expect(search).toHaveBeenCalledWith('run-1', 'gate-1', expect.anything(), expect.anything())
    expect(w.find('[data-testid="review-reference-chips"]').text()).toContain('登录重构')
    await w.find('[data-testid="review-composer-send"]').trigger('click')
    const sent = w.emitted('send')![0]!
    expect(sent[0]).toContain('@登录重构')
    expect(sent[4]).toEqual([{ kind: 'solo', target: 'sess-1' }])
  })

  it('opens the + menu and keeps upload behind it', async () => {
    const w = mountGate()
    await w.find('[data-testid="paragraph-input-attach"]').trigger('click')
    expect(w.find('[data-testid="gate-plus-menu"]').exists()).toBe(true)
    await w.find('[data-testid="gate-plus-github"]').trigger('click')
    await new Promise((r) => setTimeout(r, 300))
    await flushPromises()
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
  })

  it('stays plain text when references are disabled', async () => {
    const w = mountGate({ referencesEnabled: false, 'onUpdate:draft': (v: string) => w.setProps({ draft: v }) })
    await type(w, '[data-testid="paragraph-input"]', '@登')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    await w.find('[data-testid="review-composer-send"]').trigger('click')
    expect(w.emitted('send')![0]).toHaveLength(3)
  })
})
