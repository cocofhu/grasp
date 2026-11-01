// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const search = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api/api', () => ({ api: { searchSoloReferences: search } }))

import ReferencePicker from './ReferencePicker.vue'
import pages from '@/locales/zh-CN/pages.json'

const sessions = (n: number, more = false) => ({
  lane: 'solo',
  items: Array.from({ length: n }, (_, i) => ({ kind: 'solo', target: `s${i}`, title: `会话 ${i}`, status: 'running', lastActiveAt: '2026-10-30T10:00:00Z' })),
  nextPage: more ? 2 : undefined,
})
const gh = (items: object[], extra: object = {}) => ({ lane: 'github', items, ...extra })
const issue = (n: number, kind = 'github_issue', state = 'open') => ({ kind, target: `acme/web#${n}`, title: `T${n}`, state, url: `https://github.com/acme/web/issues/${n}`, comments: 2, repo: 'acme/web', number: n })

function respond(map: Record<string, (params: any) => object>) {
  search.mockImplementation(async (_id: string, params: any) => {
    const fn = map[params.lane]
    if (!fn) throw new Error('boom')
    return fn(params)
  })
}

function mountPicker(props: Partial<{ query: string; taken: string[]; full: boolean; initialTab: any }> = {}) {
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

beforeEach(() => {
  vi.useFakeTimers()
  search.mockReset()
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => vi.useRealTimers())

describe('ReferencePicker', () => {
  it('groups both lanes in "all", caps each section and hides taken targets', async () => {
    respond({ solo: () => sessions(7), github: () => gh([issue(1), issue(2, 'github_pr', 'merged')]) })
    const w = mountPicker({ taken: ['s0'] })
    await settle()
    const text = w.text()
    expect(text).toContain('会话 1')
    expect(text).not.toContain('会话 0')
    expect(w.findAll('[role="option"]').length).toBe(5 + 1 + 1 + 1)
    expect(text).toContain('#2 T2')
    expect(search).toHaveBeenCalledWith('me', expect.objectContaining({ lane: 'github', state: 'open', page: 1 }), expect.anything())
  })

  it('has no default highlight; arrows move it and Enter picks', async () => {
    respond({ solo: () => sessions(2), github: () => gh([]) })
    const w = mountPicker()
    await settle()
    const vm = w.vm as unknown as { move: (d: number) => void; confirm: () => boolean; cycleTab: (d: number) => void }
    expect(vm.confirm()).toBe(false)
    vm.move(-1)
    vm.move(1)
    await w.vm.$nextTick()
    expect(w.find('[aria-selected="true"]').text()).toContain('会话 0')
    expect(vm.confirm()).toBe(true)
    expect(w.emitted('pick')![0]![0]).toMatchObject({ kind: 'solo', target: 's0', title: '会话 0' })
  })

  it('switches tabs, skips the other lane and pages with load more', async () => {
    respond({ solo: (p) => sessions(2, p.page === 1), github: () => gh([]) })
    const w = mountPicker({ initialTab: 'solo' })
    await settle()
    expect(search.mock.calls.every((c) => c[1].lane === 'solo')).toBe(true)
    await w.findAll('[role="option"]').at(-1)!.trigger('click')
    await settle()
    expect(search).toHaveBeenLastCalledWith('me', expect.objectContaining({ lane: 'solo', page: 2 }), expect.anything())
    expect(w.findAll('[role="option"]').length).toBe(4)
    ;(w.vm as any).cycleTab(1)
    await settle()
    expect(search).toHaveBeenLastCalledWith('me', expect.objectContaining({ lane: 'github', kind: 'github_issue' }), expect.anything())
  })

  it('jumps from "more" to the tab and filters by state', async () => {
    respond({ solo: () => sessions(6), github: () => gh([issue(3)], { total: 9, nextPage: 2 }) })
    const w = mountPicker()
    await settle()
    const more = w.findAll('[role="option"]').find((o) => o.text().includes('查看更多'))!
    await more.trigger('click')
    await settle()
    expect(w.find('[data-testid="solo-reference-tab-solo"]').classes()).toContain('ref-picker__tab--on')
    await w.find('[data-testid="solo-reference-tab-pr"]').trigger('click')
    await settle()
    expect(w.find('[data-testid="solo-reference-tab-pr"]').text()).toContain('9')
    await w.findAll('[role="radio"]')[1]!.trigger('click')
    await settle()
    expect(search).toHaveBeenLastCalledWith('me', expect.objectContaining({ kind: 'github_pr', state: 'all' }), expect.anything())
  })

  it('shows lane errors and hints without failing the other lane', async () => {
    respond({ solo: () => sessions(0), github: () => gh([], { error: { code: 'rate_limited', message: 'GitHub 限流' } }) })
    const w = mountPicker()
    await settle()
    expect(w.text()).toContain('GitHub 限流')
    respond({ solo: () => sessions(0), github: () => gh([], { hint: '至少输入 2 个字符' }) })
    await w.setProps({ query: 'x' })
    await settle()
    expect(w.text()).toContain('至少输入 2 个字符')
    respond({ github: () => gh([]) })
    await w.setProps({ query: 'xy' })
    await settle()
    expect(w.text()).toContain('boom')
  })

  it('searches GitHub only for #n and says when full', async () => {
    respond({ solo: () => sessions(1), github: () => gh([issue(5)]) })
    const w = mountPicker({ query: '#5' })
    await settle()
    expect(search.mock.calls.some((c) => c[1].lane === 'solo')).toBe(false)
    await w.setProps({ full: true })
    expect(w.text()).toContain('最多引用 5 个')
  })

  it('serves a repeated query from cache and drops stale replies', async () => {
    let resolveSlow: (v: object) => void = () => {}
    search.mockImplementation((_id: string, p: any) => {
      if (p.lane === 'github') return Promise.resolve(gh([]))
      if (p.q === 'slow') return new Promise((r) => (resolveSlow = r))
      return Promise.resolve(sessions(1))
    })
    const w = mountPicker({ query: 'slow' })
    await vi.advanceTimersByTimeAsync(130)
    await w.setProps({ query: 'fast' })
    await settle()
    resolveSlow(sessions(3))
    await flushPromises()
    expect(w.findAll('[role="option"]').length).toBe(1)
    const calls = search.mock.calls.length
    await w.setProps({ query: 'other' })
    await settle()
    await w.setProps({ query: 'fast' })
    await settle()
    expect(search.mock.calls.length).toBe(calls + 2)
  })

  it('shows a skeleton only on the first load and keeps old rows while a new query loads', async () => {
    let release: () => void = () => {}
    respond({ solo: () => sessions(2), github: () => gh([]) })
    const w = mountPicker({ initialTab: 'solo' })
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('.ref-picker__skeleton').exists()).toBe(true)
    expect(w.text()).not.toContain('没有匹配')
    await settle()
    expect(w.find('.ref-picker__skeleton').exists()).toBe(false)

    search.mockImplementation(() => new Promise((r) => { release = () => r(sessions(0)) }))
    await w.setProps({ query: 'zz' })
    await vi.advanceTimersByTimeAsync(300)
    expect(w.find('.ref-picker__skeleton').exists()).toBe(false)
    expect(w.find('[data-testid="solo-reference-option-s0"]').exists()).toBe(true)
    expect(w.find('.ref-picker__list--stale').exists()).toBe(true)
    release()
    await flushPromises()
    expect(w.find('[data-testid="solo-reference-option-s0"]').exists()).toBe(false)
    expect(w.find('.ref-picker__list--stale').exists()).toBe(false)
    expect(w.text()).toContain(pages.pages.solo.reference.noMatch)
  })
})
