// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import DirectPreviewFrame from './DirectPreviewFrame.vue'

const DIRECT = 'http://127.0.0.1:18081/'

function mountFrame(opts: { answerPing?: boolean } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const posted: unknown[] = []
  const fakeWin = {
    postMessage: (msg: unknown) => {
      posted.push(msg)
      if (opts.answerPing && (msg as { type?: string })?.type === 'direct-preview-ping') {
        dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
      }
    },
  }
  const wrapper = mount(DirectPreviewFrame, {
    props: { directUrl: DIRECT, title: '前端' },
    global: { plugins: [i18n] },
  })
  const iframe = wrapper.get('[data-testid="app-preview-direct-frame"]').element as HTMLIFrameElement
  Object.defineProperty(iframe, 'contentWindow', { value: fakeWin, configurable: true })
  return { wrapper, posted, fakeWin }
}

function dispatchFromPreview(data: unknown, origin = 'http://127.0.0.1:18081') {
  window.dispatchEvent(new MessageEvent('message', { data, origin }))
}

describe('DirectPreviewFrame', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows address bar and inspect control', () => {
    const { wrapper } = mountFrame()
    expect(wrapper.get('[data-testid="direct-preview-address"]').element).toBeTruthy()
    expect((wrapper.get('[data-testid="direct-preview-address"]').element as HTMLInputElement).value).toBe(
      DIRECT,
    )
    expect(wrapper.find('[data-testid="direct-preview-inspect"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="app-preview-direct-frame"]').attributes('src')).toBe(DIRECT)
    wrapper.unmount()
  })

  it('rejects cross-origin address bar goto', async () => {
    const { wrapper } = mountFrame()
    const input = wrapper.get('[data-testid="direct-preview-address"]')
    await input.setValue('https://evil.example.com/phish')
    expect((input.element as HTMLInputElement).value).toBe('https://evil.example.com/phish')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const tip = wrapper.get('[data-testid="direct-preview-tip"]')
    expect(tip.text()).toMatch(/同一预览 origin/)
    expect(wrapper.get('[data-testid="app-preview-direct-frame"]').attributes('src')).toBe(DIRECT)
    wrapper.unmount()
  })

  it('navigates iframe src for same-origin goto', async () => {
    const { wrapper } = mountFrame()
    await wrapper.get('[data-testid="direct-preview-address"]').setValue('/dash')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="app-preview-direct-frame"]').attributes('src')).toBe(
      'http://127.0.0.1:18081/dash',
    )
    wrapper.unmount()
  })

  it('updates address from cooperative ready message', async () => {
    const { wrapper } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: 'http://127.0.0.1:18081/home' })
    await flushPromises()
    expect((wrapper.get('[data-testid="direct-preview-address"]').element as HTMLInputElement).value).toBe(
      'http://127.0.0.1:18081/home',
    )
    wrapper.unmount()
  })

  it('keeps ready across iframe load without needing a ping', async () => {
    const { wrapper, posted } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    await wrapper.get('[data-testid="app-preview-direct-frame"]').trigger('load')
    await flushPromises()
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(posted).toEqual([{ type: 'direct-preview-host' }])
    expect(wrapper.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
    await wrapper.get('[data-testid="direct-preview-inspect"]').trigger('click')
    expect(wrapper.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('recovers via ping when the announcement was missed', async () => {
    const { wrapper, posted } = mountFrame({ answerPing: true })
    await wrapper.get('[data-testid="app-preview-direct-frame"]').trigger('load')
    await flushPromises()
    expect(posted).toEqual([{ type: 'direct-preview-host' }, { type: 'direct-preview-ping' }])
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(wrapper.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
    await wrapper.get('[data-testid="direct-preview-inspect"]').trigger('click')
    expect(posted).toContainEqual({ type: 'direct-preview-inspect', on: true })
    wrapper.unmount()
  })

  it('pings again before reporting a missing script', async () => {
    const { wrapper, posted } = mountFrame()
    vi.advanceTimersByTime(2500)
    await flushPromises()
    expect(posted).toEqual([{ type: 'direct-preview-host' }, { type: 'direct-preview-ping' }])
    expect(wrapper.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    vi.advanceTimersByTime(500)
    await flushPromises()
    expect(wrapper.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('drops stale ready when a later page has no script', async () => {
    const { wrapper } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    await wrapper.get('[data-testid="app-preview-direct-frame"]').trigger('load')
    await flushPromises()
    await wrapper.get('[data-testid="direct-preview-address"]').setValue('/no-script')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-testid="app-preview-direct-frame"]').trigger('load')
    await flushPromises()
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(wrapper.get('[data-testid="direct-preview-tip"]').text()).toMatch(/未加载取点脚本/)
    wrapper.unmount()
  })

  it('ignores pick messages from other origins', async () => {
    const { wrapper } = mountFrame()
    dispatchFromPreview(
      { type: 'direct-preview-picked', selector: 'button', tagName: 'button', outerHTML: '<button>' },
      'http://evil.example',
    )
    await flushPromises()
    expect(wrapper.emitted('pick')).toBeUndefined()
    wrapper.unmount()
  })

  it('emits every in-page pick right away and stays in inspect mode', async () => {
    const { wrapper } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    await wrapper.get('[data-testid="direct-preview-inspect"]').trigger('click')
    for (const selector of ['#ok', '#other']) {
      dispatchFromPreview({
        type: 'direct-preview-picked',
        selector,
        tagName: 'button',
        text: 'OK',
        outerHTML: `<button id="${selector.slice(1)}">`,
        url: 'http://127.0.0.1:18081/a',
      })
    }
    await flushPromises()
    expect(wrapper.emitted('pick')?.map((e) => e[0])).toEqual([
      { selector: '#ok', tagName: 'button', text: 'OK', outerHTML: '<button id="ok">', url: 'http://127.0.0.1:18081/a' },
      expect.objectContaining({ selector: '#other' }),
    ])
    expect(wrapper.get('[data-testid="direct-preview-inspect"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="direct-preview-pick-result"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('follows the page Pick bar toggle', async () => {
    const { wrapper } = mountFrame()
    const btn = wrapper.get('[data-testid="direct-preview-inspect"]')
    dispatchFromPreview({ type: 'direct-preview-inspect-state', on: true })
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')
    dispatchFromPreview({ type: 'direct-preview-inspect-state', on: false })
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    wrapper.unmount()
  })

  it('shows script-missing tip after wait without ready', async () => {
    const { wrapper } = mountFrame()
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(wrapper.get('[data-testid="direct-preview-tip"]').text()).toMatch(/未加载取点脚本/)
    wrapper.unmount()
  })

  it('inspect without script shows missing-script tip', async () => {
    const { wrapper, posted } = mountFrame()
    await wrapper.get('[data-testid="direct-preview-inspect"]').trigger('click')
    expect(wrapper.get('[data-testid="direct-preview-tip"]').text()).toMatch(/未加载取点脚本/)
    expect(posted).toEqual([])
    wrapper.unmount()
  })

  it('inspect after ready posts inspect command', async () => {
    const { wrapper, posted } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    await wrapper.get('[data-testid="direct-preview-inspect"]').trigger('click')
    expect(posted).toEqual([{ type: 'direct-preview-inspect', on: true }])
    wrapper.unmount()
  })

  it('toggles label to 取消标注; second click posts on:false', async () => {
    const { wrapper, posted } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    const btn = wrapper.get('[data-testid="direct-preview-inspect"]')
    expect(btn.text()).toContain('取点标注')
    expect(btn.attributes('aria-pressed')).toBe('false')

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.text()).toContain('取消标注')
    expect(btn.text()).not.toContain('取点标注')
    expect(posted).toContainEqual({ type: 'direct-preview-inspect', on: true })

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')
    expect(wrapper.text()).not.toContain('取消标注')
    expect(posted).toContainEqual({ type: 'direct-preview-inspect', on: false })
    wrapper.unmount()
  })

  it('nav buttons post nav actions', async () => {
    const { wrapper, posted } = mountFrame()
    dispatchFromPreview({ type: 'direct-preview-ready', url: DIRECT })
    await flushPromises()
    await wrapper.get('[data-testid="direct-preview-back"]').trigger('click')
    await wrapper.get('[data-testid="direct-preview-reload"]').trigger('click')
    expect(posted).toEqual([
      { type: 'direct-preview-nav', action: 'back' },
      { type: 'direct-preview-nav', action: 'reload' },
    ])
    wrapper.unmount()
  })
})
