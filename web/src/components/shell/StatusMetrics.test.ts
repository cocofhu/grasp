// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick, ref } from 'vue'
import shell from '@/locales/zh-CN/shell.json'
import { PLATFORM_STATUS_POLL_MS } from '@/lib/composables/usePlatformStatusMetrics'
import StatusMetrics from './StatusMetrics.vue'

const platformStatus = vi.fn()
const isMobile = ref(false)

vi.mock('@/lib/api/api', () => ({
  api: {
    platformStatus: (...args: unknown[]) => platformStatus(...args),
  },
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile }),
}))

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...shell } },
  })
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      {
        path: '/stats',
        name: 'stats',
        component: { template: '<div data-testid="token-analytics-page" />' },
      },
      {
        path: '/runs',
        name: 'runs',
        component: { template: '<div data-testid="run-list-page" />' },
      },
    ],
  })
}

async function mountMetrics(props?: { variant?: 'auto' | 'full' | 'compact' }) {
  const i18n = makeI18n()
  const router = makeRouter()
  await router.push('/')
  const w = mount(StatusMetrics, {
    props,
    global: { plugins: [i18n, router] },
    attachTo: document.body,
  })
  return { w, router }
}

describe('StatusMetrics', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    isMobile.value = false
    platformStatus.mockReset()
    platformStatus.mockResolvedValue({
      cumulativeTokens: 1240582,
      todayTokens: 4812,
      runningCount: 3,
      queuedCount: 5,
      asOf: '2026-08-12T06:07:00Z',
      timezone: 'Asia/Shanghai',
    })
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('renders four desktop metrics with today tokens (plan g2.2)', async () => {
    const { w } = await mountMetrics()
    await flushPromises()
    expect(w.find('[data-testid="status-metrics"]').exists()).toBe(true)
    expect(w.find('[data-testid="status-metrics-tokens"]').text()).toContain('1.24M')
    expect(w.find('[data-testid="status-metrics-today"]').text()).toContain('4.8K')
    expect(w.find('[data-testid="status-metrics-today"]').text()).not.toContain('/5m')
    expect(w.find('[data-testid="status-metrics-rate"]').exists()).toBe(false)
    expect(w.find('[data-testid="status-metrics-peak"]').exists()).toBe(false)
    expect(w.find('[data-testid="status-metrics-running"]').text()).toContain('3')
    expect(w.find('[data-testid="status-metrics-queued"]').text()).toContain('5')
    w.unmount()
  })

  it('keeps lastSuccess on failure and does not flash 0 (plan g2.2)', async () => {
    const { w } = await mountMetrics()
    await flushPromises()
    expect(w.find('[data-testid="status-metrics-tokens"]').text()).toContain('1.24M')

    platformStatus.mockRejectedValueOnce(new Error('network'))
    vi.advanceTimersByTime(PLATFORM_STATUS_POLL_MS)
    await flushPromises()
    expect(w.find('[data-testid="status-metrics-tokens"]').text()).toContain('1.24M')
    expect(w.find('[data-testid="status-metrics"]').attributes('data-stale')).toBe('true')
    w.unmount()
  })

  it('pauses polling while document is hidden (plan g2.2)', async () => {
    const { w } = await mountMetrics()
    await flushPromises()
    const calls = platformStatus.mock.calls.length

    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(PLATFORM_STATUS_POLL_MS * 3)
    await flushPromises()
    expect(platformStatus.mock.calls.length).toBe(calls)

    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(platformStatus.mock.calls.length).toBeGreaterThan(calls)
    w.unmount()
  })

  it('shows — for null token fields and 0 for true-zero counts', async () => {
    platformStatus.mockResolvedValue({
      cumulativeTokens: null,
      todayTokens: null,
      runningCount: 0,
      queuedCount: 0,
      asOf: '2026-08-12T00:00:00Z',
      timezone: 'UTC',
    })
    const { w } = await mountMetrics()
    await flushPromises()
    await nextTick()
    expect(w.find('[data-testid="status-metrics-tokens"]').text()).toContain('—')
    expect(w.find('[data-testid="status-metrics-today"]').text()).toContain('—')
    expect(w.find('[data-testid="status-metrics-running"]').text()).toContain('0')
    expect(w.find('[data-testid="status-metrics-queued"]').text()).toContain('0')
    w.unmount()
  })

  it('renders Token·RUN/Q two-zone strip under md (plan g1.1)', async () => {
    isMobile.value = true
    const { w } = await mountMetrics()
    await flushPromises()
    expect(w.find('[data-testid="status-metrics-compact"]').exists()).toBe(true)
    expect(w.find('[data-testid="status-metrics-tokens"]').exists()).toBe(false)
    const compact = w.find('[data-testid="status-metrics-compact"]')
    // g1.1: elevated strip container + two zone buttons (not nested buttons)
    expect(compact.classes()).toContain('bg-elevated')
    expect(compact.classes()).toContain('sm-compact')
    expect(compact.element.tagName.toLowerCase()).toBe('div')
    const tokenZone = w.find('[data-testid="status-metrics-compact-token"]')
    const runZone = w.find('[data-testid="status-metrics-compact-run"]')
    expect(tokenZone.exists()).toBe(true)
    expect(runZone.exists()).toBe(true)
    expect(tokenZone.element.tagName.toLowerCase()).toBe('button')
    expect(runZone.element.tagName.toLowerCase()).toBe('button')
    expect(tokenZone.find('.sm-val').classes()).toContain('font-semibold')
    const text = compact.text()
    expect(text).toMatch(/1\.24M/)
    expect(text).toMatch(/3/)
    expect(text).toMatch(/5/)
    w.unmount()
  })

  it('desktop tips are partitioned KPI cards with only that metric (plan g2.1)', async () => {
    const { w } = await mountMetrics()
    await flushPromises()
    const tips = {
      tokens: w.find('[data-testid="status-metrics-tokens"] .sm-tip'),
      today: w.find('[data-testid="status-metrics-today"] .sm-tip'),
      running: w.find('[data-testid="status-metrics-running"] .sm-tip'),
      queued: w.find('[data-testid="status-metrics-queued"] .sm-tip'),
    }
    expect(tips.tokens.classes()).toContain('sm-kpi')
    expect(tips.tokens.text()).toMatch(/累计 Token/)
    expect(tips.tokens.text()).toMatch(/1,240,582/)
    expect(tips.tokens.text()).not.toMatch(/今日 Token/)
    expect(tips.tokens.text()).not.toMatch(/执行中/)
    expect(tips.tokens.text()).not.toMatch(/排队/)

    expect(tips.today.text()).toMatch(/今日 Token/)
    expect(tips.today.text()).toMatch(/4,812/)
    expect(tips.today.text()).not.toMatch(/累计 Token/)
    expect(w.find('[data-testid="status-metrics-today"]').attributes('aria-label')).toMatch(/今日 Token:\s*4,812/)

    expect(tips.running.text()).toMatch(/执行中/)
    expect(tips.running.text()).toMatch(/\b3\b/)
    expect(tips.running.text()).not.toMatch(/排队/)
    expect(tips.running.text()).not.toMatch(/Token/)

    expect(tips.queued.text()).toMatch(/排队/)
    expect(tips.queued.text()).toMatch(/\b5\b/)
    expect(tips.queued.text()).not.toMatch(/执行中/)

    for (const tip of Object.values(tips)) {
      expect(tip.text()).not.toMatch(/完整值/)
      expect(tip.text()).not.toMatch(/totalTokens/i)
      expect(tip.text()).not.toContain('/5m')
      expect(tip.text()).not.toMatch(/\d{2}:\d{2}/)
    }
    w.unmount()
  })

  it('compact token tip has only Token rows; run tip only run rows (plan g1.2)', async () => {
    isMobile.value = true
    const { w } = await mountMetrics()
    await flushPromises()
    const tokenTip = w.find('[data-testid="status-metrics-compact-token-tip"]')
    const runTip = w.find('[data-testid="status-metrics-compact-run-tip"]')
    expect(tokenTip.exists()).toBe(true)
    expect(runTip.exists()).toBe(true)
    expect(tokenTip.classes()).toContain('sm-kpi')
    expect(tokenTip.text()).toMatch(/Token/)
    expect(tokenTip.text()).toMatch(/累计 Token/)
    expect(tokenTip.text()).toMatch(/1,240,582/)
    expect(tokenTip.text()).toMatch(/今日 Token/)
    expect(tokenTip.text()).toMatch(/4,812/)
    expect(tokenTip.text()).not.toMatch(/执行中/)
    expect(tokenTip.text()).not.toMatch(/排队/)

    expect(runTip.text()).toMatch(/运行/)
    expect(runTip.text()).toMatch(/执行中/)
    expect(runTip.text()).toMatch(/\b3\b/)
    expect(runTip.text()).toMatch(/排队/)
    expect(runTip.text()).toMatch(/\b5\b/)
    expect(runTip.text()).not.toMatch(/累计 Token/)
    expect(runTip.text()).not.toMatch(/今日 Token/)
    expect(runTip.text()).not.toMatch(/完整值/)
    expect(runTip.text()).not.toContain('/5m')
    w.unmount()
  })

  it('sidebar compact teleports zone tip above trigger on hover (g1.1/g1.3)', async () => {
    const clip = document.createElement('div')
    clip.style.overflow = 'hidden'
    clip.style.height = '120px'
    document.body.appendChild(clip)

    const i18n = makeI18n()
    const router = makeRouter()
    await router.push('/')
    const w = mount(StatusMetrics, {
      props: { variant: 'compact' },
      global: { plugins: [i18n, router] },
      attachTo: clip,
    })
    await flushPromises()

    const strip = w.find('[data-testid="status-metrics-compact"]')
    expect(strip.element.tagName.toLowerCase()).toBe('div')
    expect(strip.find('.sm-tip').exists()).toBe(false)
    const tokenZone = w.find('[data-testid="status-metrics-compact-token"]')
    const runZone = w.find('[data-testid="status-metrics-compact-run"]')
    expect(tokenZone.attributes('aria-label')).toMatch(/进入统计/)
    expect(runZone.attributes('aria-label')).toMatch(/进入运行/)
    expect(runZone.attributes('aria-label')).not.toMatch(/进入统计/)

    vi.spyOn(tokenZone.element as HTMLElement, 'getBoundingClientRect').mockReturnValue({
      top: 320,
      left: 24,
      right: 100,
      bottom: 352,
      width: 76,
      height: 32,
      x: 24,
      y: 320,
      toJSON: () => ({}),
    } as DOMRect)
    vi.spyOn(runZone.element as HTMLElement, 'getBoundingClientRect').mockReturnValue({
      top: 320,
      left: 110,
      right: 200,
      bottom: 352,
      width: 90,
      height: 32,
      x: 110,
      y: 320,
      toJSON: () => ({}),
    } as DOMRect)

    await tokenZone.trigger('mouseenter')
    await flushPromises()
    await nextTick()

    const tip = document.body.querySelector('[data-testid="status-metrics-compact-tip"]') as HTMLElement
    expect(tip).toBeTruthy()
    expect(tip.getAttribute('data-placement')).toBe('above')
    expect(tip.getAttribute('data-zone')).toBe('token')
    expect(tip.style.position).toBe('fixed')
    expect(tip.textContent).toMatch(/累计 Token/)
    expect(tip.textContent).toMatch(/1,240,582/)
    expect(tip.textContent).toMatch(/今日 Token/)
    expect(tip.textContent).not.toMatch(/执行中/)
    expect(tip.textContent).not.toMatch(/排队/)
    expect(Number.parseInt(tip.style.top, 10)).toBeLessThan(320)

    await runZone.trigger('mouseenter')
    await flushPromises()
    await nextTick()
    expect(tip.getAttribute('data-zone')).toBe('run')
    expect(tip.textContent).toMatch(/执行中/)
    expect(tip.textContent).toMatch(/排队/)
    expect(tip.textContent).not.toMatch(/累计 Token/)
    expect(tip.textContent).not.toMatch(/今日 Token/)

    await runZone.trigger('click')
    await flushPromises()
    // plan g1.1 / g2.1: compact-run → name=runs
    expect(router.currentRoute.value.name).toBe('runs')
    expect(router.currentRoute.value.path).toBe('/runs')
    const afterClick = document.body.querySelector(
      '[data-testid="status-metrics-compact-tip"]',
    ) as HTMLElement | null
    expect(afterClick?.style.display === 'none' || afterClick == null).toBe(true)

    w.unmount()
    clip.remove()
    document.body.innerHTML = ''
  })

  it('click compact token navigates to stats; run zone to runs (plan g1.1/g1.2/g1.3)', async () => {
    const { w, router } = await mountMetrics({ variant: 'compact' })
    await flushPromises()
    const tokenZone = w.find('[data-testid="status-metrics-compact-token"]')
    await tokenZone.trigger('mouseenter')
    await flushPromises()
    expect(document.body.querySelector('[data-testid="status-metrics-compact-tip"]')).toBeTruthy()
    await tokenZone.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('stats')
    expect(router.currentRoute.value.path).toBe('/stats')
    const tip = document.body.querySelector('[data-testid="status-metrics-compact-tip"]') as HTMLElement | null
    expect(tip == null || (tip as HTMLElement).style.display === 'none' || getComputedStyle(tip).display === 'none').toBe(true)

    await router.push('/')
    await flushPromises()
    const runZone = w.find('[data-testid="status-metrics-compact-run"]')
    await runZone.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('runs')
    expect(router.currentRoute.value.path).toBe('/runs')
    w.unmount()
  })

  it('Enter/Space: token→stats, run→runs (plan g1.1/g2.1)', async () => {
    const { w, router } = await mountMetrics({ variant: 'compact' })
    await flushPromises()
    await w.find('[data-testid="status-metrics-compact-token"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('stats')
    w.unmount()

    const again = await mountMetrics({ variant: 'compact' })
    await flushPromises()
    await again.w.find('[data-testid="status-metrics-compact-run"]').trigger('keydown', { key: ' ' })
    await flushPromises()
    expect(again.router.currentRoute.value.name).toBe('runs')
    expect(again.router.currentRoute.value.path).toBe('/runs')
    again.w.unmount()
  })

  it('desktop: token/today→stats; running/queued→runs (plan g1.1/g1.2/g2.1)', async () => {
    const statsIds = ['status-metrics-tokens', 'status-metrics-today'] as const
    for (const id of statsIds) {
      const { w, router } = await mountMetrics()
      await flushPromises()
      expect(w.find(`[data-testid="${id}"]`).attributes('aria-label')).toMatch(/进入统计/)
      await w.find(`[data-testid="${id}"]`).trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.name).toBe('stats')
      expect(w.find(`[data-testid="${id}"]`).classes()).not.toContain('tip-open')
      w.unmount()
    }
    const runIds = ['status-metrics-running', 'status-metrics-queued'] as const
    for (const id of runIds) {
      const { w, router } = await mountMetrics()
      await flushPromises()
      const aria = w.find(`[data-testid="${id}"]`).attributes('aria-label') ?? ''
      expect(aria).toMatch(/进入运行/)
      expect(aria).not.toMatch(/进入统计/)
      await w.find(`[data-testid="${id}"]`).trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.name).toBe('runs')
      expect(router.currentRoute.value.path).toBe('/runs')
      expect(w.find(`[data-testid="${id}"]`).classes()).not.toContain('tip-open')
      w.unmount()
    }
  })

  it('stays on stats when already there for token click (plan g1.2)', async () => {
    const { w, router } = await mountMetrics()
    await router.push({ name: 'stats' })
    await flushPromises()
    await w.find('[data-testid="status-metrics-tokens"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('stats')
    expect(router.currentRoute.value.path).toBe('/stats')
    w.unmount()
  })

  it('from stats, run zone navigates to runs (edge)', async () => {
    const { w, router } = await mountMetrics()
    await router.push({ name: 'stats' })
    await flushPromises()
    await w.find('[data-testid="status-metrics-running"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('runs')
    expect(router.currentRoute.value.path).toBe('/runs')
    w.unmount()
  })

  it('stays on runs when already there for run click (edge)', async () => {
    const { w, router } = await mountMetrics()
    await router.push({ name: 'runs' })
    await flushPromises()
    await w.find('[data-testid="status-metrics-queued"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('runs')
    expect(router.currentRoute.value.path).toBe('/runs')
    w.unmount()
  })

  it('zero counts still show label: 0 in tip', async () => {
    platformStatus.mockResolvedValue({
      cumulativeTokens: 0,
      todayTokens: 0,
      runningCount: 0,
      queuedCount: 0,
      asOf: '2026-08-12T00:00:00Z',
      timezone: 'UTC',
    })
    const { w } = await mountMetrics()
    await flushPromises()
    expect(w.find('[data-testid="status-metrics-running"] .sm-tip').text()).toMatch(/执行中/)
    expect(w.find('[data-testid="status-metrics-running"] .sm-kpi-num').text()).toBe('0')
    expect(w.find('[data-testid="status-metrics-queued"] .sm-tip').text()).toMatch(/排队/)
    expect(w.find('[data-testid="status-metrics-queued"] .sm-kpi-num').text()).toBe('0')
    expect(w.find('[data-testid="status-metrics-tokens"] .sm-tip').text()).toMatch(/累计 Token/)
    expect(w.find('[data-testid="status-metrics-tokens"] .sm-kpi-num').text()).toBe('0')
    expect(w.find('[data-testid="status-metrics-today"] .sm-tip').text()).toMatch(/今日 Token/)
    expect(w.find('[data-testid="status-metrics-today"] .sm-kpi-num').text()).toBe('0')
    w.unmount()
  })

  it('uses A-set stroke icon paths (plan g2.4)', async () => {
    const { w } = await mountMetrics()
    await flushPromises()
    const html = w.html()
    expect(html).toContain('cx="12" cy="6.6" rx="7.2" ry="3.1"')
    expect(html).toContain('M4.8 6.6v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1V6.6')
    expect(html).toContain('M8.2 3v4.2M15.8 3v4.2M3.4 10.2h17.2')
    expect(html).toContain('M10.3 8.7l5.4 3.3-5.4 3.3z')
    expect(html).toContain('M4 7.2h16M4 12h11.5M4 16.8h7')
    expect(html).not.toContain('M13 3L5 14h7l-1 7 8-11h-7l1-7z')
    expect(html).not.toContain('fill="currentColor"')
    expect(html).not.toContain('M5 7h14M5 12h14M5 17h10')
    w.unmount()
  })
})
