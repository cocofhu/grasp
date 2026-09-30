// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import LiveVariantCard from './LiveVariantCard.vue'
import { LIVE_CARD_HOST, createLiveStore, type LiveCardHost } from '@/lib/inbox/liveVariants'

function mountCard(liveRef: { sid: string; op: string; variant?: number }, host?: Partial<LiveCardHost>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const provide = host ? { [LIVE_CARD_HOST as symbol]: host } : {}
  return mount(LiveVariantCard, { props: { liveRef }, global: { plugins: [i18n], provide } })
}

function readyStore() {
  const live = createLiveStore()
  live.apply({ sid: 'sid001', state: 'ready', summary: 'section「Dispatch」', variants: [{ n: 1, label: '层级' }, { n: 2, label: '紧凑' }, { n: 3 }] })
  return live
}

describe('LiveVariantCard', () => {
  it('shows only the request without a host', () => {
    const w = mountCard({ sid: 'sid001', op: 'generate' })
    expect(w.text()).toContain('Live 变体')
    expect(w.text()).toContain('生成变体')
    expect(w.find('[data-testid="live-variant-state"]').exists()).toBe(false)
    w.unmount()
  })

  it('drives the page from the drawer', async () => {
    const live = readyStore()
    live.setView('sid001', { current: 2, mode: 'inplace' })
    const command = vi.fn()
    const w = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command })
    expect(w.attributes('data-state')).toBe('ready')
    expect(w.text()).toContain('section「Dispatch」')
    expect(w.get('[data-testid="live-variant-viewing"]').text()).toContain('2 / 3')
    const chips = w.findAll('[data-testid="live-variant-chip"]')
    expect(chips).toHaveLength(3)
    expect(chips[1].attributes('aria-pressed')).toBe('true')
    await chips[2].trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 3)
    await w.get('[data-testid="live-variant-next"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 3)
    await w.get('[data-testid="live-variant-prev"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 1)
    await w.get('[data-testid="live-variant-mode"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'compare', undefined)
    await w.get('[data-testid="live-variant-accept"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'accept', 2)
    await w.get('[data-testid="live-variant-discard"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'discard', undefined)
    live.setView('sid001', { current: 2, mode: 'compare' })
    await w.vm.$nextTick()
    await w.get('[data-testid="live-variant-mode"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'inplace', undefined)
    w.unmount()
  })

  it('is read-only outside the drawer and shows failures and results', async () => {
    const live = readyStore()
    const w = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: false, command: vi.fn() })
    expect(w.find('[data-testid="live-variant-accept"]').exists()).toBe(false)
    expect(w.text()).toContain('在预览页中')
    live.apply({ sid: 'sid001', state: 'failed', error: 'no source' })
    await w.vm.$nextTick()
    expect(w.get('[data-testid="live-variant-error"]').text()).toContain('no source')
    live.apply({ sid: 'sid001', state: 'accepted', selected: 2 })
    await w.vm.$nextTick()
    expect(w.text()).toContain('已采用变体 2')
    w.unmount()
  })

  it('locks controls while busy and hides them on accept turns', async () => {
    const live = readyStore()
    live.apply({ sid: 'sid001', state: 'refining', variants: [{ n: 1 }, { n: 2 }] })
    const w = mountCard({ sid: 'sid001', op: 'refine' }, { store: live.store, interactive: true, command: vi.fn() })
    expect(w.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="live-variant-discard"]').attributes('disabled')).toBeDefined()
    w.unmount()
    const acc = mountCard({ sid: 'sid001', op: 'accept', variant: 1 }, { store: live.store, interactive: true, command: vi.fn() })
    expect(acc.find('[data-testid="live-variant-chip"]').exists()).toBe(false)
    acc.unmount()
    const odd = mountCard({ sid: 'sid009', op: 'weird' }, { store: live.store, interactive: true, command: vi.fn() })
    expect(odd.text()).toContain('生成变体')
    odd.unmount()
  })

  it('waits for the page view before adopting a candidate', () => {
    const live = readyStore()
    const card = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command: vi.fn() })
    expect(card.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeDefined()
    expect(card.get('[data-testid="live-variant-viewing"]').text()).toContain('0 / 3')
    card.unmount()
  })

  it('recovers a failed steer without implying a rollback, including read-only hosts', async () => {
    const live = createLiveStore()
    live.apply({ sid: 'steer01', mode: 'steer', state: 'failed', error: 'compile failed', prompt: 'Make this quieter' })
    const command = vi.fn()
    const card = mountCard({ sid: 'steer01', op: 'steer' }, { store: live.store, interactive: true, command })
    expect(card.get('[data-testid="live-steer-partial"]').text()).toContain('保留')
    await card.get('[data-testid="live-variant-retry"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('steer01', 'retry', undefined)
    await card.get('[data-testid="live-variant-discard"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('steer01', 'discard', undefined)
    card.unmount()
    const readOnly = mountCard({ sid: 'steer01', op: 'steer' }, { store: live.store, interactive: false, command })
    expect(readOnly.find('[data-testid="live-variant-retry"]').exists()).toBe(false)
    expect(readOnly.find('[data-testid="live-variant-discard"]').exists()).toBe(false)
    readOnly.unmount()
  })

  it('offers explicit adoption recovery on the accept turn without a mounted candidate', async () => {
    const live = createLiveStore()
    live.apply({ sid: 'sid001', mode: 'replace', state: 'failed', selected: 2, retryAccept: true, error: 'interrupted cleanup' })
    const command = vi.fn()
    const card = mountCard({ sid: 'sid001', op: 'accept', variant: 2 }, { store: live.store, interactive: true, command })
    expect(card.get('[data-testid="live-variant-retry-accept"]').text()).toBe('重试采用')
    await card.get('[data-testid="live-variant-retry-accept"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'retry-accept', undefined)
    live.apply({ sid: 'sid001', mode: 'replace', state: 'failed', selected: 2 })
    await card.vm.$nextTick()
    expect(card.find('[data-testid="live-variant-retry-accept"]').exists()).toBe(false)
    card.unmount()
    live.apply({ sid: 'sid001', state: 'failed', selected: 2, retryAccept: true })
    const readOnly = mountCard({ sid: 'sid001', op: 'accept', variant: 2 }, { store: live.store, interactive: false, command })
    expect(readOnly.find('[data-testid="live-variant-retry-accept"]').exists()).toBe(false)
    readOnly.unmount()
  })
})
