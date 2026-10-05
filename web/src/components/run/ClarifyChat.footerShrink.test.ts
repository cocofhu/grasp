// @vitest-environment happy-dom
/**
 * plan g1 / g2 — active-session footer must not shrink under a fixed-height sidebar.
 * Screenshot path: page-control status + empty input clipped「确认并流转」at card bottom.
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyChat from './ClarifyChat.vue'
import ReviewComposer from './ReviewComposer.vue'

const here = dirname(fileURLToPath(import.meta.url))
const chatSrc = readFileSync(join(here, 'ClarifyChat.vue'), 'utf8')
const composerSrc = readFileSync(join(here, 'ReviewComposer.vue'), 'utf8')

function mountChat(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId: 'approve_1',
      iteration: 1,
      turns: [{ role: 'agent', text: '请确认', at: '2026-09-29T00:00:00Z' }],
      done: false,
      active: true,
      ...extra,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
    },
  })
}

function mountComposer(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ReviewComposer, {
    props: {
      mode: 'clarify',
      runId: 'run-1',
      nodeId: 'approve_1',
      iteration: 1,
      turns: [{ role: 'agent', text: '请确认', at: '2026-09-29T00:00:00Z' }],
      done: false,
      active: true,
      canPass: true,
      ...extra,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true, AnnotationChip: true },
    },
  })
}

describe('hot-path footer shrink-0 (plan g2.1 / g1.1 / g1.2)', () => {
  it('source: hot actions and confirm-error match cold-session shrink-0', () => {
    // Cold session already locked shrink-0; hot path must align (root cause).
    expect(chatSrc).toMatch(
      /class="shrink-0 border-t border-line p-3" data-testid="clarify-cold-actions"/,
    )
    expect(chatSrc).toMatch(
      /class="shrink-0 border-t border-line p-3" data-testid="clarify-hot-actions"/,
    )
    expect(chatSrc).toMatch(
      /class="flex shrink-0 items-center gap-1\.5 border-t border-err\/30[^"]*"[\s\S]*?data-testid="clarify-confirm-error"/,
    )
  })

  it('mount: hot actions + confirm-error carry shrink-0; cold stays shrink-0', async () => {
    const hot = mountChat()
    await flushPromises()
    const hotActions = hot.get('[data-testid="clarify-hot-actions"]')
    expect(hotActions.classes()).toContain('shrink-0')
    expect(hot.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(hot.find('[data-testid="clarify-confirm-flow"]').classes()).toContain('h-9')
    hot.unmount()

    const withErr = mountChat({ confirmError: '产物契约不满足' })
    await flushPromises()
    expect(withErr.get('[data-testid="clarify-confirm-error"]').classes()).toContain('shrink-0')
    expect(withErr.get('[data-testid="clarify-hot-actions"]').classes()).toContain('shrink-0')
    withErr.unmount()

    const cold = mountChat({ coldSession: true, active: false })
    await flushPromises()
    expect(cold.get('[data-testid="clarify-cold-actions"]').classes()).toContain('shrink-0')
    expect(cold.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(cold.find('[data-testid="clarify-hot-actions"]').exists()).toBe(false)
    cold.unmount()
  })
})

describe('message list absorbs leftover height (plan g1.3)', () => {
  it('scroller stays flex-1 overflow-y-auto while footers are shrink-0', () => {
    const scrollerIdx = chatSrc.indexOf('data-testid="clarify-scroller"')
    expect(scrollerIdx).toBeGreaterThanOrEqual(0)
    const scrollerBlock = chatSrc.slice(Math.max(0, scrollerIdx - 160), scrollerIdx + 40)
    expect(scrollerBlock).toMatch(/\bflex-1\b/)
    expect(scrollerBlock).toMatch(/\boverflow-y-auto\b/)
    // Parent of scroller keeps min-h-0 flex-1 so the column can shrink the list.
    expect(chatSrc).toMatch(/class="relative flex min-h-0 flex-1 flex-col"/)
    // Multi-root: bake min-h-0 flex-1 on chat root (parent fallthrough is ignored).
    expect(chatSrc).toMatch(
      /class="flex h-full min-h-0 flex-1 flex-col" data-review-composer/,
    )
  })

  it('ReviewComposer page-control row is shrink-0 and does not own the flex grow', () => {
    expect(composerSrc).toMatch(
      /v-if="pageControl" class="shrink-0 border-b border-line px-3 py-1\.5"/,
    )
    expect(composerSrc).toMatch(/class="min-h-0 flex-1"[\s\S]*?:confirm-error="confirmError"/)
  })
})

describe('constrained sidebar with page-control status (plan g2.2)', () => {
  it('screenshot path: offline page-control + empty input keeps full confirm button in column', async () => {
    // Fixed-height overflow-hidden host mirrors GatesInbox card + ReviewShell sidebar.
    const host = document.createElement('div')
    host.style.cssText =
      'display:flex;flex-direction:column;height:320px;overflow:hidden;position:relative'
    document.body.appendChild(host)

    const wrapper = mountComposer({ pageControl: 'offline' })
    await flushPromises()
    host.appendChild(wrapper.element)

    expect(wrapper.get('[data-testid="page-control-status"]').text()).toContain('未连接')
    expect(wrapper.get('[data-testid="page-control-status"]').attributes('data-state')).toBe(
      'offline',
    )

    const statusRow = wrapper.element.querySelector('.shrink-0.border-b') as HTMLElement | null
    expect(statusRow).toBeTruthy()
    expect(statusRow!.className).toMatch(/\bshrink-0\b/)

    const hotActions = wrapper.get('[data-testid="clarify-hot-actions"]')
    expect(hotActions.classes()).toContain('shrink-0')

    const confirm = wrapper.get('[data-testid="clarify-confirm-flow"]')
    expect(confirm.exists()).toBe(true)
    expect(confirm.text()).toContain('确认并流转')
    expect(confirm.classes()).toEqual(
      expect.arrayContaining(['h-9', 'shrink-0', 'inline-flex']),
    )
    // Disabled gate unchanged: idle approve session allows confirm.
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)

    // Full confirm control stays under the fixed overflow-hidden host (not clipped out of tree).
    expect(host.contains(confirm.element)).toBe(true)
    expect(host.contains(hotActions.element)).toBe(true)
    // Column contract: status + footer refuse to shrink; scroller flex-grows.
    const shell = wrapper.get('[data-testid="review-composer-shell"]')
    expect(shell.classes()).toEqual(expect.arrayContaining(['flex', 'h-full', 'min-h-0', 'flex-col']))

    // Scroller is the flex grow sibling — remaining height goes to messages.
    const scroller = wrapper.get('[data-testid="clarify-scroller"]')
    expect(scroller.classes()).toEqual(expect.arrayContaining(['flex-1', 'overflow-y-auto']))

    wrapper.unmount()
    host.remove()
  })

  it('confirm stays enabled/disabled per existing rules when page-control is present', async () => {
    const idle = mountComposer({ pageControl: 'offline', passDisabled: false })
    await flushPromises()
    expect((idle.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    idle.unmount()

    const locked = mountComposer({ pageControl: 'offline', passDisabled: true })
    await flushPromises()
    expect(
      (locked.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled,
    ).toBe(true)
    locked.unmount()
  })
})
