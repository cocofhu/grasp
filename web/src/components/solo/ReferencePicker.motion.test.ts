// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const search = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api/api', () => ({ api: { searchSoloReferences: search } }))

import ReferencePicker from './ReferencePicker.vue'
import pages from '@/locales/zh-CN/pages.json'

const session = (i: number) => ({ kind: 'solo', target: `s${i}`, title: `会话 ${i}`, status: 'running', lastActiveAt: '2026-10-30T10:00:00Z' })
const gh = (kind: string, n: number, state: string) => ({ kind, target: `acme/web#${n}`, title: `T${n}`, state, url: '', repo: 'acme/web', number: n })

function mountPicker(props: Partial<{ query: string }> = {}) {
  return mount(ReferencePicker, {
    props: { scope: { sessionId: 'me' }, query: '', taken: [], full: false, ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': pages } })],
      stubs: { Icon: true },
    },
  })
}

async function settle() {
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
}

const realMatchMedia = window.matchMedia
const offsetTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetTop')
const offsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetHeight')

beforeEach(() => {
  vi.useFakeTimers()
  search.mockReset()
  Object.defineProperty(HTMLElement.prototype, 'offsetTop', {
    configurable: true,
    get(this: HTMLElement) {
      const i = this.dataset?.refIndex
      return i == null ? 0 : 40 + Number(i) * 30
    },
  })
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 30 })
})
afterEach(() => {
  vi.useRealTimers()
  window.matchMedia = realMatchMedia
  if (offsetTop) Object.defineProperty(HTMLElement.prototype, 'offsetTop', offsetTop)
  if (offsetHeight) Object.defineProperty(HTMLElement.prototype, 'offsetHeight', offsetHeight)
})

describe('ReferencePicker motion', () => {
  it('keeps the same row element when a refresh returns the same key', async () => {
    search.mockImplementation(async (_id: string, p: any) => (p.lane === 'solo' ? { lane: 'solo', items: [session(0), session(1)] } : { lane: 'github', items: [] }))
    const w = mountPicker()
    await settle()
    const before = w.find('[data-testid="solo-reference-option-s1"]').element
    await w.setProps({ query: '会' })
    await settle()
    expect(w.find('[data-testid="solo-reference-option-s1"]').element).toBe(before)
  })

  it('keeps the highlight on the same item when a reply reorders the rows', async () => {
    let items = [session(0), session(1), session(2)]
    search.mockImplementation(async (_id: string, p: any) => (p.lane === 'solo' ? { lane: 'solo', items } : { lane: 'github', items: [] }))
    const w = mountPicker()
    await settle()
    const vm = w.vm as unknown as { move: (d: number) => void; confirm: () => boolean }
    await w.setProps({ query: '会' })
    vm.move(1)
    vm.move(1)
    items = [session(1), session(2)]
    await settle()
    expect(w.find('[aria-selected="true"]').attributes('data-testid')).toBe('solo-reference-option-s1')
    items = [session(2)]
    await w.setProps({ query: '会话' })
    vm.move(1)
    items = [session(0)]
    await settle()
    expect(vm.confirm()).toBe(false)
  })

  it('keeps the old rows and dims the list while a refresh is pending', async () => {
    let call = 0
    search.mockImplementation((_id: string, p: any) => {
      if (p.lane === 'github') return Promise.resolve({ lane: 'github', items: [] })
      return call++ === 0 ? Promise.resolve({ lane: 'solo', items: [session(0)] }) : new Promise(() => {})
    })
    const w = mountPicker()
    await settle()
    await w.setProps({ query: 'x' })
    await settle()
    expect(w.find('[data-testid="solo-reference-option-s0"]').exists()).toBe(true)
    expect(w.find('.ref-picker__list').classes()).toContain('ref-picker__list--stale')
  })

  it('tints icons by kind and state and uses the ghost scrollbar', async () => {
    search.mockImplementation(async (_id: string, p: any) =>
      p.lane === 'solo'
        ? { lane: 'solo', items: [session(0)] }
        : { lane: 'github', items: [gh('github_issue', 1, 'open'), gh('github_issue', 2, 'closed'), gh('github_pr', 3, 'open'), gh('github_pr', 4, 'merged')] },
    )
    const w = mountPicker()
    await settle()
    const icon = (target: string) => w.find(`[data-testid="solo-reference-option-${target}"] .ref-picker__icon`)
    expect(icon('s0').attributes('data-icon')).toBe('chat')
    expect(icon('s0').classes().some((c) => c.startsWith('ref-tone'))).toBe(false)
    expect(icon('acme/web#1').attributes('data-icon')).toBe('circle')
    expect(icon('acme/web#1').classes()).toContain('ref-tone--open')
    expect(icon('acme/web#2').attributes('data-icon')).toBe('check')
    expect(icon('acme/web#2').classes()).toContain('ref-tone--closed')
    expect(icon('acme/web#3').attributes('data-icon')).toBe('branch')
    expect(icon('acme/web#4').attributes('data-icon')).toBe('git')
    expect(icon('acme/web#4').classes()).toContain('ref-tone--merged')
    expect(w.find('.ref-picker__list').classes()).toContain('scroll-area')
  })

  it('slides the highlight to the selected row and scrolls it into view smoothly', async () => {
    search.mockImplementation(async (_id: string, p: any) => (p.lane === 'solo' ? { lane: 'solo', items: [session(0), session(1), session(2)] } : { lane: 'github', items: [] }))
    const w = mountPicker()
    await settle()
    const list = w.find('.ref-picker__list').element as HTMLElement
    const scrollTo = vi.fn()
    list.scrollTo = scrollTo as any
    Object.defineProperty(list, 'clientHeight', { configurable: true, get: () => 60 })
    const vm = w.vm as unknown as { move: (d: number) => void }
    vm.move(1)
    vm.move(1)
    vm.move(1)
    await flushPromises()
    const hl = w.find('[data-testid="solo-reference-highlight"]')
    expect(hl.classes()).toContain('ref-picker__hl--on')
    expect(hl.attributes('style')).toContain('translateY(100px)')
    expect(scrollTo).toHaveBeenCalledWith(expect.objectContaining({ behavior: 'smooth' }))
    expect(w.find('.ref-picker').classes()).not.toContain('ref-picker--still')
  })

  it('measures the highlight from the list content when each row is its own offset parent', async () => {
    const offsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetParent')
    Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
      configurable: true,
      get(this: HTMLElement) {
        if (this.dataset?.refIndex != null) return this.parentElement
        if (this.classList?.contains('ref-picker__row')) return this.closest('.ref-picker__content')
        return null
      },
    })
    Object.defineProperty(HTMLElement.prototype, 'offsetTop', {
      configurable: true,
      get(this: HTMLElement) {
        const i = this.classList?.contains('ref-picker__row') ? this.querySelector<HTMLElement>('[data-ref-index]')?.dataset.refIndex : undefined
        return i == null ? 0 : 40 + Number(i) * 30
      },
    })
    try {
      search.mockImplementation(async (_id: string, p: any) => (p.lane === 'solo' ? { lane: 'solo', items: [session(0), session(1), session(2)] } : { lane: 'github', items: [] }))
      const w = mountPicker()
      await settle()
      const vm = w.vm as unknown as { move: (d: number) => void }
      vm.move(-1)
      await flushPromises()
      expect(w.find('[data-testid="solo-reference-highlight"]').attributes('style')).toContain('translateY(100px)')
    } finally {
      if (offsetParent) Object.defineProperty(HTMLElement.prototype, 'offsetParent', offsetParent)
    }
  })

  it('drops transitions under prefers-reduced-motion', async () => {
    window.matchMedia = ((q: string) => ({ matches: q.includes('reduce'), media: q, addEventListener() {}, removeEventListener() {} })) as any
    search.mockImplementation(async (_id: string, p: any) => (p.lane === 'solo' ? { lane: 'solo', items: [session(0), session(1), session(2)] } : { lane: 'github', items: [] }))
    const w = mountPicker()
    await settle()
    const list = w.find('.ref-picker__list').element as HTMLElement
    const scrollTo = vi.fn()
    list.scrollTo = scrollTo as any
    Object.defineProperty(list, 'clientHeight', { configurable: true, get: () => 60 })
    expect(w.find('.ref-picker').classes()).toContain('ref-picker--still')
    const vm = w.vm as unknown as { move: (d: number) => void }
    vm.move(-1)
    await flushPromises()
    expect(scrollTo).toHaveBeenCalledWith(expect.objectContaining({ behavior: 'auto' }))
  })
})
