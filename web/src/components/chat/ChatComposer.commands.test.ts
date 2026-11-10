// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useChatComposerDraft, type ChatComposerDraft } from '@/lib/chat/useChatComposerDraft'
import type { ComposerCommand } from '@/lib/chat/useComposerCommands'
import ChatComposer from './ChatComposer.vue'

const workflow = { id: 'workflow', label: '选择工作流', description: '由用户确认后启动' }
const mounted: ReturnType<typeof mount>[] = []

function render(commands: ComposerCommand[] | undefined = [workflow], disabled = false) {
  let draft!: ChatComposerDraft
  const host = mount(defineComponent({
    setup() {
      draft = useChatComposerDraft()
      return () => h(ChatComposer, {
        draft, commands, disabled, picks: [], placeholder: '继续对话',
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
  async function type(text: string, focused = true) {
    if (focused) { input.element.focus(); await nextTick() }
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
  return { host, composer, input, draft, type, key, menu: () => host.find('[data-testid="composer-command-menu"]') }
}

afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})

describe('ChatComposer opt-in slash commands', () => {
  it('shows known full-input prefixes only while the textarea is focused', async () => {
    const view = render()
    await view.type('/', false)
    expect(view.menu().exists()).toBe(false)
    view.input.element.focus(); await nextTick()
    expect(view.menu().exists()).toBe(true)
    await view.type('/wor')
    expect(view.host.get('[data-testid="composer-command-workflow"]').text()).toContain('/workflow')
    expect(view.input.attributes('aria-controls')).toBe(view.menu().attributes('id'))
    view.input.element.blur(); await nextTick()
    expect(view.menu().exists()).toBe(false)
    expect(view.input.element.value).toBe('/wor')
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it.each(['Enter', 'Tab'])('selects with bare %s without sending a chat message', async key => {
    const view = render()
    await view.type('/wor')
    expect((await view.key(key)).defaultPrevented).toBe(true)
    expect(view.composer.emitted('command')).toEqual([['workflow']])
    expect(view.composer.emitted('send')).toBeUndefined()
    expect(view.input.element.value).toBe('')
    expect(view.menu().exists()).toBe(false)
  })

  it('clicks an option without losing focus first and preserves attached files', async () => {
    const view = render()
    const attachment = { name: 'notes.txt', mimeType: 'text/plain', data: 'bm90ZXM=' }
    view.draft.attach.attachments.value = [attachment]
    await view.type('/')
    const option = view.host.get('[data-testid="composer-command-workflow"]')
    const pointer = new Event('pointerdown', { bubbles: true, cancelable: true })
    option.element.dispatchEvent(pointer)
    expect(pointer.defaultPrevented).toBe(true)
    await option.trigger('click')
    expect(view.composer.emitted('command')).toEqual([['workflow']])
    expect(view.composer.emitted('send')).toBeUndefined()
    expect(view.draft.attach.attachments.value).toEqual([attachment])
    expect(view.host.text()).toContain('notes.txt')
  })

  it('moves the active option with arrow keys before keyboard selection', async () => {
    const view = render([workflow, { id: 'help', label: '帮助', description: '查看命令' }])
    await view.type('/')
    await view.key('ArrowDown')
    expect(view.host.get('[data-testid="composer-command-help"]').attributes('aria-selected')).toBe('true')
    expect(view.input.attributes('aria-activedescendant')).toBe(view.host.get('[data-testid="composer-command-help"]').attributes('id'))
    await view.key('ArrowUp')
    expect(view.host.get('[data-testid="composer-command-workflow"]').attributes('aria-selected')).toBe('true')
    await view.key('ArrowUp')
    await view.key('Tab')
    expect(view.composer.emitted('command')).toEqual([['help']])
  })

  it('rechecks selection before consuming Tab and closes the menu when text is selected', async () => {
    const view = render()
    await view.type('/')
    expect(view.menu().exists()).toBe(true)
    // A native selection can change before Vue receives a select event.
    view.input.element.setSelectionRange(0, 1)
    expect((await view.key('Tab')).defaultPrevented).toBe(false)
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.input.element.value).toBe('/')

    view.input.element.setSelectionRange(1, 1)
    await view.input.trigger('input')
    expect(view.menu().exists()).toBe(true)
    view.input.element.setSelectionRange(0, 1)
    await view.input.trigger('select')
    expect(view.menu().exists()).toBe(false)
    view.input.element.setSelectionRange(1, 1)
    await view.input.trigger('input')
    await view.key('Tab')
    expect(view.composer.emitted('command')).toEqual([['workflow']])
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it('Escape dismisses the current query until it changes or the input is focused again', async () => {
    const view = render()
    await view.type('/wor')
    expect((await view.key('Escape')).defaultPrevented).toBe(true)
    expect(view.menu().exists()).toBe(false)
    expect(view.input.element.value).toBe('/wor')
    await view.input.trigger('input')
    expect(view.menu().exists()).toBe(false)
    await view.type('/work')
    expect(view.menu().exists()).toBe(true)
    await view.key('Escape')
    view.input.element.blur(); view.input.element.focus(); await nextTick()
    expect(view.menu().exists()).toBe(true)
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it('leaves Shift+Enter, composition Enter and keyCode 229 to the textarea', async () => {
    const view = render()
    await view.type('/')
    expect((await view.key('Enter', { shiftKey: true })).defaultPrevented).toBe(false)
    expect((await view.key('Enter', { isComposing: true })).defaultPrevented).toBe(false)
    expect((await view.key('Enter', { keyCode: 229 })).defaultPrevented).toBe(false)
    await view.input.trigger('compositionstart')
    expect(view.menu().exists()).toBe(false)
    expect((await view.key('Enter')).defaultPrevented).toBe(false)
    await view.input.trigger('compositionend')
    expect(view.menu().exists()).toBe(true)
    expect(view.input.element.value).toBe('/')
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.composer.emitted('send')).toBeUndefined()
  })

  it.each(['/unknown', '/workspace/file', '查看 /workflow', 'https://example.test/workflow', '/workflow\n正文'])('keeps ordinary input %j out of the command menu', async text => {
    const view = render()
    await view.type(text)
    expect(view.menu().exists()).toBe(false)
    expect((await view.key('Tab')).defaultPrevented).toBe(false)
    await view.key('Enter')
    expect(view.composer.emitted('send')).toEqual([[]])
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.input.element.value).toBe(text)
  })

  it('does not treat a slash inside a referenced draft as a command or remove its reference', async () => {
    const view = render()
    view.draft.mention.load('查看 @历史 /workflow', [{ kind: 'solo', target: 'previous', title: '历史' }])
    await flushPromises()
    await view.input.trigger('input')
    expect(view.menu().exists()).toBe(false)
    expect(view.host.findAll('[data-testid="solo-reference-chip"]')).toHaveLength(1)
    await view.key('Enter')
    expect(view.composer.emitted('send')).toEqual([[]])
    expect(view.composer.emitted('command')).toBeUndefined()
    expect(view.draft.mention.refs.value).toEqual([{ kind: 'solo', target: 'previous', title: '历史' }])
    expect(view.input.element.value).toBe('查看 @历史 /workflow')
  })

  it('does not intercept prefixes when commands are absent or the composer is disabled', async () => {
    const plain = render([])
    await plain.type('/')
    expect(plain.menu().exists()).toBe(false)
    expect((await plain.key('Tab')).defaultPrevented).toBe(false)
    await plain.key('Enter')
    expect(plain.composer.emitted('send')).toEqual([[]])
    expect(plain.composer.emitted('command')).toBeUndefined()

    const disabled = render([workflow], true)
    await disabled.input.setValue('/')
    await disabled.input.trigger('focus')
    await disabled.key('Enter')
    expect(disabled.menu().exists()).toBe(false)
    expect(disabled.composer.emitted('send')).toBeUndefined()
    expect(disabled.composer.emitted('command')).toBeUndefined()
  })
})
