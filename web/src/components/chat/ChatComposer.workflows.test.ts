// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useChatComposerDraft, type ChatComposerDraft } from '@/lib/chat/useChatComposerDraft'
import ChatComposer from './ChatComposer.vue'

vi.mock('@/lib/api/api', () => ({ api: { searchSoloReferences: vi.fn(async () => ({ items: [] })) } }))
const mounted: ReturnType<typeof mount>[] = []

function render(allowWorkflow = true, disabled = false) {
  let draft!: ChatComposerDraft
  const host = mount(defineComponent({
    setup() {
      draft = useChatComposerDraft()
      return () => h(ChatComposer, {
        draft, allowWorkflow, disabled, picks: [], placeholder: '继续对话',
        scope: { sessionId: 'solo-1' }, testPrefix: 'composer', pickTestId: 'pick',
      })
    },
  }), {
    attachTo: document.body,
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })] },
  })
  mounted.push(host)
  const composer = host.getComponent(ChatComposer)
  const input = host.get<HTMLTextAreaElement>('[data-testid="composer-input"]')
  async function type(text: string) {
    input.element.focus()
    await input.setValue(text)
    input.element.setSelectionRange(text.length, text.length)
    await input.trigger('input')
  }
  async function key(key: string, extra: KeyboardEventInit = {}) {
    const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...extra })
    input.element.dispatchEvent(event)
    await nextTick()
    return event
  }
  return { host, composer, input, draft, type, key, option: () => host.find('[data-testid="solo-reference-workflow"]') }
}

afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})

describe('ChatComposer workflow action in @ references', () => {
  it.each(['@', '@wor', '@工作流'])('offers workflow configuration for %s in the existing picker', async text => {
    const view = render()
    await view.type(text)
    expect(view.host.findAll('[data-testid="solo-reference-picker"]')).toHaveLength(1)
    expect(view.option().text()).toContain('选择工作流')
    expect(view.host.find('[data-testid="composer-command-menu"]').exists()).toBe(false)
    expect(view.composer.emitted('workflow')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it('uses arrow keys and Enter to select without sending a chat message', async () => {
    const view = render()
    await view.type('@wor')
    expect((await view.key('ArrowDown')).defaultPrevented).toBe(true)
    expect(view.option().attributes('aria-selected')).toBe('true')
    expect((await view.key('Enter')).defaultPrevented).toBe(true)
    expect(view.composer.emitted('workflow')).toEqual([[]])
    expect(view.composer.emitted('send')).toBeUndefined()
    expect(view.input.element.value).toBe('')
  })

  it('consumes only the active query and preserves existing references and attachments', async () => {
    const view = render()
    const reference = { kind: 'solo' as const, target: 'previous', title: '历史' }
    const attachment = { name: 'notes.txt', mimeType: 'text/plain', data: 'bm90ZXM=' }
    view.draft.attach.attachments.value = [attachment]
    view.draft.mention.load('查看 @历史', [reference])
    await flushPromises()
    await view.type('查看 @历史 @wor')
    const pointer = new Event('mousedown', { bubbles: true, cancelable: true })
    view.option().element.dispatchEvent(pointer)
    expect(pointer.defaultPrevented).toBe(true)
    await view.option().trigger('click')
    expect(view.composer.emitted('workflow')).toEqual([[]])
    expect(view.composer.emitted('send')).toBeUndefined()
    expect(view.input.element.value).toBe('查看 @历史 ')
    expect(view.draft.mention.refs.value).toEqual([reference])
    expect(view.draft.attach.attachments.value).toEqual([attachment])
  })

  it('keeps workflow configuration available when the reference limit is reached', async () => {
    const view = render()
    view.draft.mention.load('', Array.from({ length: 5 }, (_, i) => ({ kind: 'solo', target: `s${i}`, title: `会话${i}` })))
    await flushPromises()
    await view.type(`${view.input.element.value}@`)
    expect(view.option().exists()).toBe(true)
    await view.option().trigger('click')
    expect(view.composer.emitted('workflow')).toEqual([[]])
    expect(view.draft.mention.refs.value).toHaveLength(5)
  })

  it('keeps Tab cycling reference tabs and Escape dismissing the picker', async () => {
    const view = render()
    await view.type('@')
    await view.key('Tab')
    expect(view.option().exists()).toBe(false)
    await view.key('Tab', { shiftKey: true })
    expect(view.option().exists()).toBe(true)
    await view.key('Escape')
    expect(view.host.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    expect(view.input.element.value).toBe('@')
    expect(view.composer.emitted('workflow')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it('does not select a workflow while composing or using Shift+Enter', async () => {
    const view = render()
    await view.type('@wor')
    await view.key('ArrowDown')
    expect((await view.key('Enter', { shiftKey: true })).defaultPrevented).toBe(false)
    expect((await view.key('Enter', { isComposing: true })).defaultPrevented).toBe(false)
    expect((await view.key('Enter', { keyCode: 229 })).defaultPrevented).toBe(false)
    await view.input.trigger('compositionstart')
    expect(view.option().exists()).toBe(false)
    await view.key('Enter')
    await view.input.trigger('compositionend')
    expect(view.option().exists()).toBe(true)
    expect(view.composer.emitted('workflow')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it.each(['/', '/wor', '/workspace/file', '查看 /workflow', 'mail@example.com', '@unrelated'])('does not offer workflow configuration for ordinary text %j', async text => {
    const view = render()
    await view.type(text)
    expect(view.option().exists()).toBe(false)
    await view.key('Enter')
    expect(view.composer.emitted('send')).toEqual([[]])
    expect(view.composer.emitted('workflow')).toBeUndefined()
  })

  it.each([[false, false], [true, true]])('hides the action when allowWorkflow=%s and disabled=%s', async (allowWorkflow, disabled) => {
    const view = render(allowWorkflow, disabled)
    await view.type('@wor')
    expect(view.option().exists()).toBe(false)
    await view.key('ArrowDown')
    await view.key('Enter')
    expect(view.composer.emitted('workflow')).toBeUndefined()
  })
})
