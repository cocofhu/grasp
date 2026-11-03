// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import LiveVariantCard from './LiveVariantCard.vue'
import { LIVE_CARD_HOST, createLiveStore, type LiveCardHost } from '@/lib/inbox/liveVariants'

function mountCard(liveRef: { sid: string; op: string; variant?: number; prompt?: string }, host?: Partial<LiveCardHost>) {
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
    expect(w.get('[data-testid="live-variant-headline"]').text()).toBe('为页面出 3 个候选')
    expect(w.find('[data-testid="live-variant-state"]').exists()).toBe(false)
    w.unmount()
  })

  it('compact cards name the request without controls', () => {
    const live = readyStore()
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    const w = mount(LiveVariantCard, {
      props: { liveRef: { sid: 'sid001', op: 'generate' }, compact: true },
      global: { plugins: [i18n], provide: { [LIVE_CARD_HOST as symbol]: { store: live.store, interactive: true, command: vi.fn() } } },
    })
    expect(w.get('[data-testid="live-variant-headline"]').text()).toBe('为「Dispatch」出 3 个候选')
    expect(w.find('[data-testid="live-variant-chip"]').exists()).toBe(false)
    expect(w.find('[data-testid="live-variant-accept"]').exists()).toBe(false)
    w.unmount()
  })

  it('drives the page from the drawer', async () => {
    const live = readyStore()
    live.setView('sid001', { current: 2, mode: 'inplace' })
    const command = vi.fn()
    const w = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command })
    expect(w.attributes('data-state')).toBe('ready')
    expect(w.get('[data-testid="live-variant-headline"]').text()).toBe('为「Dispatch」出 3 个候选')
    expect(w.text()).not.toContain('section「')
    expect(w.get('[data-testid="live-variant-state"]').text()).toBe('可以挑选')
    expect(w.find('[data-testid="live-variant-viewing"]').exists()).toBe(false)
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

  it('offers the original for comparison on replace sessions only', async () => {
    const live = createLiveStore()
    live.apply({ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] })
    live.setView('sid001', { current: 1, mode: 'inplace' })
    const command = vi.fn()
    const w = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command })
    const original = w.get('[data-testid="live-variant-original"]')
    expect(original.text()).toBe('原版')
    expect(original.attributes('aria-pressed')).toBe('false')
    await original.trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 0)
    await w.get('[data-testid="live-variant-prev"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 0)

    // The page reports it is showing the original: navigation stays live, adopting does not.
    live.setView('sid001', { current: 0, mode: 'inplace', original: true })
    await w.vm.$nextTick()
    expect(w.get('[data-testid="live-variant-original"]').attributes('aria-pressed')).toBe('true')
    expect(w.findAll('[data-testid="live-variant-chip"]').every((c) => c.attributes('aria-pressed') === 'false')).toBe(true)
    expect(w.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="live-variant-next"]').attributes('disabled')).toBeUndefined()
    await w.get('[data-testid="live-variant-next"]').trigger('click')
    expect(command).toHaveBeenLastCalledWith('sid001', 'goto', 1)

    // current 0 without the flag means nothing is mounted: controls stay off.
    live.setView('sid001', { current: 0, mode: 'inplace' })
    await w.vm.$nextTick()
    expect(w.get('[data-testid="live-variant-next"]').attributes('disabled')).toBeDefined()
    w.unmount()

    const ins = createLiveStore()
    ins.apply({ sid: 'ins001', state: 'ready', mode: 'insert', variants: [{ n: 1 }, { n: 2 }] })
    const wi = mountCard({ sid: 'ins001', op: 'insert' }, { store: ins.store, interactive: true, command })
    expect(wi.find('[data-testid="live-variant-original"]').exists()).toBe(false)
    wi.unmount()
  })

  it('is read-only outside the drawer and shows failures and results', async () => {
    const live = readyStore()
    const w = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: false, command: vi.fn() })
    expect(w.find('[data-testid="live-variant-accept"]').exists()).toBe(false)
    expect(w.get('[data-testid="live-variant-readonly-hint"]').text()).toBe('在预览页切换、采用或放弃候选')
    live.apply({ sid: 'sid001', state: 'failed', error: 'no source' })
    await w.vm.$nextTick()
    expect(w.get('[data-testid="live-variant-error"]').text()).toContain('no source')
    live.apply({ sid: 'sid001', state: 'accepted', selected: 2 })
    await w.vm.$nextTick()
    expect(w.text()).toContain('最终采用了候选 2')
    expect(w.get('[data-testid="live-variant-state"]').text()).toBe('已写入代码')
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
    expect(odd.get('[data-testid="live-variant-headline"]').text()).toBe('为页面出 3 个候选')
    odd.unmount()
  })

  it('shows an accepted session as read-only chips with the adopted one marked', () => {
    const live = createLiveStore()
    live.apply({ sid: 'sid001', state: 'accepted', selected: 2, variants: [{ n: 1, label: '层级' }, { n: 2, label: '紧凑' }, { n: 3 }] })
    const card = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command: vi.fn() })
    expect(card.get('[data-testid="live-variant-state"]').text()).toBe('已写入代码')
    const chips = card.findAll('[data-testid="live-variant-result"] [data-testid="live-variant-chip"]')
    expect(chips).toHaveLength(3)
    expect(chips[1]!.classes()).toContain('bg-accent')
    expect(card.find('button').exists()).toBe(false)
    card.unmount()
  })

  it('waits for the page view before adopting a candidate', () => {
    const live = readyStore()
    const card = mountCard({ sid: 'sid001', op: 'generate' }, { store: live.store, interactive: true, command: vi.fn() })
    expect(card.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeDefined()
    expect(card.findAll('[data-testid="live-variant-chip"]').every((c) => c.attributes('aria-pressed') === 'false')).toBe(true)
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

  it('reads as a sentence per request and shows what the user typed', async () => {
    const live = readyStore()
    const opts = { store: live.store, interactive: true, command: vi.fn() }
    const head = (w: ReturnType<typeof mountCard>) => w.get('[data-testid="live-variant-headline"]').text()
    const acc = mountCard({ sid: 'sid001', op: 'accept', variant: 1 }, opts)
    expect(head(acc)).toBe('采用候选 1 · 层级')
    expect(acc.text()).toContain('「Dispatch」')
    acc.unmount()
    const ref = mountCard({ sid: 'sid001', op: 'refine', variant: 2, prompt: '按钮再大一点' }, opts)
    expect(head(ref)).toBe('继续改候选 2')
    expect(ref.get('[data-testid="live-variant-prompt"]').text()).toBe('按钮再大一点')
    ref.unmount()
    const more = mountCard({ sid: 'sid001', op: 'refine' }, opts)
    expect(head(more)).toBe('再来几个候选')
    more.unmount()
    const dis = mountCard({ sid: 'sid001', op: 'discard' }, opts)
    expect(head(dis)).toBe('放弃候选，恢复原样')
    dis.unmount()
    live.apply({ sid: 'page01', state: 'accepting', summary: '页面候选', count: 4 })
    const page = mountCard({ sid: 'page01', op: 'generate' }, opts)
    expect(head(page)).toBe('为页面出 4 个候选')
    expect(page.get('[data-testid="live-variant-state"]').text()).toBe('正在写入代码…')
    page.unmount()
  })

  describe('read-only host (main app chat)', () => {
    const HINT = '[data-testid="live-variant-readonly-hint"]'

    function readOnly(live = readyStore()) {
      return { store: live.store, interactive: false, command: vi.fn(), ensure: vi.fn() }
    }

    it('disables candidates and the original and never sends a command', async () => {
      const live = readyStore()
      live.apply({ sid: 'sid001', state: 'ready', mode: 'replace', summary: 'section「Dispatch」', variants: [{ n: 1 }, { n: 2 }] })
      live.setView('sid001', { current: 1, mode: 'inplace' })
      const host = readOnly(live)
      const w = mountCard({ sid: 'sid001', op: 'generate' }, host)
      const chips = w.findAll('[data-testid="live-variant-chip"]')
      expect(chips).toHaveLength(2)
      expect(chips.every((c) => c.attributes('disabled') !== undefined)).toBe(true)
      expect(w.get('[data-testid="live-variant-original"]').attributes('disabled')).toBeDefined()
      for (const el of w.findAll('button')) await el.trigger('click')
      expect(host.command).not.toHaveBeenCalled()
      for (const id of ['prev', 'next', 'mode', 'accept', 'discard', 'retry', 'retry-accept']) {
        expect(w.find(`[data-testid="live-variant-${id}"]`).exists()).toBe(false)
      }
      expect(w.get(HINT).text()).toBe('在预览页切换、采用或放弃候选')
      w.unmount()
    })

    it('shows the hint only while the session is open', async () => {
      const live = readyStore()
      const w = mountCard({ sid: 'sid001', op: 'generate' }, readOnly(live))
      for (const state of ['generating', 'ready', 'refining', 'failed']) {
        live.apply({ sid: 'sid001', state, summary: 'section「Dispatch」' })
        await w.vm.$nextTick()
        expect(w.find(HINT).exists(), state).toBe(true)
      }
      for (const state of ['accepted', 'discarded', 'done']) {
        live.apply({ sid: 'sid001', state, summary: 'section「Dispatch」' })
        await w.vm.$nextTick()
        expect(w.find(HINT).exists(), state).toBe(false)
      }
      w.unmount()
    })

    it('keeps accept and discard cards quiet unless they failed', async () => {
      const live = readyStore()
      for (const op of ['accept', 'discard']) {
        live.apply({ sid: 'sid001', state: op === 'accept' ? 'accepting' : 'discarding' })
        const w = mountCard({ sid: 'sid001', op, variant: 1 }, readOnly(live))
        expect(w.find(HINT).exists(), op).toBe(false)
        live.apply({ sid: 'sid001', state: 'failed', error: '放弃后源码里仍有预览标记' })
        await w.vm.$nextTick()
        expect(w.get('[data-testid="live-variant-state"]').text()).toBe('没有完成')
        expect(w.get('[data-testid="live-variant-error"]').text()).toContain('放弃后源码里仍有预览标记')
        expect(w.find(HINT).exists(), op).toBe(true)
        expect(w.find('[data-testid="live-variant-retry-accept"]').exists()).toBe(false)
        w.unmount()
      }
    })

    it('hides the hint on compact cards but keeps it on the loading request', () => {
      const live = readyStore()
      live.apply({ sid: 'sid001', state: 'generating' })
      const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
      const mountWith = (props: Record<string, unknown>) =>
        mount(LiveVariantCard, {
          props: { liveRef: { sid: 'sid001', op: 'generate' }, ...props },
          global: { plugins: [i18n], provide: { [LIVE_CARD_HOST as symbol]: readOnly(live) } },
        })
      const compact = mountWith({ compact: true })
      expect(compact.find(HINT).exists()).toBe(false)
      compact.unmount()
      const loading = mountWith({ compact: true, loading: true })
      expect(loading.find(HINT).exists()).toBe(true)
      loading.unmount()
    })

    it('asks the host for a missing session and falls back to the request headline', async () => {
      const live = createLiveStore()
      const host = readOnly(live)
      const heads: Record<string, string> = {
        generate: '为页面出 3 个候选',
        insert: '在页面旁边插入 3 个候选',
        steer: '整页调整',
        refine: '继续改候选 2',
        accept: '采用候选 2',
        discard: '放弃候选，恢复原样',
        mount_failed: '候选没有显示在页面上',
      }
      for (const [op, head] of Object.entries(heads)) {
        const w = mountCard({ sid: 'miss01', op, variant: 2 }, host)
        expect(w.get('[data-testid="live-variant-headline"]').text()).toBe(head)
        expect(w.find('[data-testid="live-variant-state"]').exists()).toBe(false)
        expect(w.find(HINT).exists()).toBe(false)
        w.unmount()
      }
      expect(host.ensure).toHaveBeenCalledWith('miss01')
      host.ensure.mockClear()
      const w = mountCard({ sid: 'miss01', op: 'generate' }, host)
      live.apply({ sid: 'miss01', state: 'ready', variants: [{ n: 1 }] })
      await w.vm.$nextTick()
      expect(w.get('[data-testid="live-variant-state"]').text()).toBe('可以挑选')
      expect(host.ensure).toHaveBeenCalledTimes(1)
      w.unmount()
    })

    it('does not ask the drawer host or a present session', () => {
      const host = readOnly()
      const w = mountCard({ sid: 'sid001', op: 'generate' }, host)
      expect(host.ensure).not.toHaveBeenCalled()
      w.unmount()
      const drawer = mountCard({ sid: 'miss01', op: 'generate' }, { store: createLiveStore().store, interactive: true, command: vi.fn() })
      expect(drawer.find(HINT).exists()).toBe(false)
      drawer.unmount()
    })
  })
})
