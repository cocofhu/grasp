// @vitest-environment happy-dom
import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ComposerPrimaryAction from './ComposerPrimaryAction.vue'

const stubs = { Icon: true }

describe('ComposerPrimaryAction', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it.each([
    ['send', 'chat-action-primary'],
    ['stop', 'chat-action-stop'],
    ['confirm', 'chat-action-confirm'],
  ] as const)('%s renders one button with the existing %s style', (kind, cls) => {
    const w = mount(ComposerPrimaryAction, { props: { kind, label: '确认并流转' }, global: { stubs } })
    expect(w.findAll('button')).toHaveLength(1)
    expect(w.get('button').classes()).toContain(cls)
    expect(w.get('button').attributes('data-kind')).toBe(kind)
    expect(w.text().includes('确认并流转')).toBe(kind === 'confirm')
  })

  it('morphs in place: kind changes keep the same element and emit the current kind', async () => {
    const w = mount(ComposerPrimaryAction, { props: { kind: 'send', label: 'x' }, global: { stubs } })
    const el = w.get('button').element
    for (const kind of ['stop', 'confirm', 'send'] as const) {
      await w.setProps({ kind })
      expect(w.get('button').element).toBe(el)
      await w.get('button').trigger('click')
      expect(w.emitted('click')!.at(-1)).toEqual([kind])
    }
  })

  it('hands off from the connecting skeleton: holds the disabled send briefly, then morphs', async () => {
    vi.useFakeTimers()
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 36 } as DOMRect)
    const connecting = mount(ComposerPrimaryAction, {
      props: { kind: 'send', disabled: true, label: '发送', handoffOut: true },
      global: { stubs },
    })
    connecting.unmount()
    const chat = mount(ComposerPrimaryAction, { props: { kind: 'stop', label: 'Cancel' }, global: { stubs } })
    expect(chat.get('button').attributes('data-kind')).toBe('send')
    expect((chat.get('button').element as HTMLButtonElement).disabled).toBe(true)
    vi.advanceTimersByTime(300)
    await nextTick()
    expect(chat.get('button').attributes('data-kind')).toBe('stop')
    expect((chat.get('button').element as HTMLButtonElement).disabled).toBe(false)
    chat.unmount()
    // Without handoffOut nothing is left behind.
    const next = mount(ComposerPrimaryAction, { props: { kind: 'confirm', label: 'c' }, global: { stubs } })
    expect(next.get('button').attributes('data-kind')).toBe('confirm')
  })
})

describe('ComposerPrimaryAction icon override', () => {
  it('uses the icon prop for confirm-shaped exits and spins a spinner', async () => {
    const { mount } = await import('@vue/test-utils')
    const { default: C } = await import('./ComposerPrimaryAction.vue')
    const back = mount(C, { props: { kind: 'confirm', label: '退回修改', icon: 'arrow-left' } })
    expect(back.find('[data-kind="confirm"]').exists()).toBe(true)
    expect(back.html()).toContain('退回修改')
    const spin = mount(C, { props: { kind: 'confirm', label: '批准中…', icon: 'spinner', busy: true } })
    await nextTick()
    expect(spin.get('[data-icon="spinner"]').attributes('data-icon-motion')).toBe('busy')
    const plain = mount(C, { props: { kind: 'confirm', label: '确认并流转' } })
    expect(plain.find('[data-icon-motion="busy"]').exists()).toBe(false)
  })

  it('secondary confirm uses main\'s chat-action-secondary instead of the filled primary', async () => {
    const { mount } = await import('@vue/test-utils')
    const { default: C } = await import('./ComposerPrimaryAction.vue')
    const back = mount(C, { props: { kind: 'confirm', label: '退回修改', icon: 'arrow-left', secondary: true } })
    const btn = back.get('button')
    expect(btn.classes()).toContain('chat-action-secondary')
    expect(btn.classes()).not.toContain('chat-action-confirm')
    expect(btn.attributes('data-variant')).toBe('secondary')
    expect(btn.text()).toContain('退回修改')
    // secondary only affects confirm-shaped buttons
    const send = mount(C, { props: { kind: 'send', label: '发送', secondary: true } })
    expect(send.get('button').classes()).toContain('chat-action-primary')
  })
})
