// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'

function mountStatus(props: {
  busy?: boolean
  completed?: boolean
  interrupted?: boolean
}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ThoughtSummaryStatus, {
    props,
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('ThoughtSummaryStatus', () => {
  it('replaces the running indicator when the turn completes or is interrupted', async () => {
    const w = mountStatus({ busy: true })
    expect(w.findAll('icon-stub')).toHaveLength(1)
    expect(w.get('icon-stub').attributes('name')).toBe('spinner')
    await w.setProps({ busy: false, completed: true })
    expect(w.find('icon-stub[name="spinner"]').exists()).toBe(false)
    expect(w.get('icon-stub').attributes('name')).toBe('check')
    expect(w.get('[data-testid="thought-summary-state"]').attributes('data-state')).toBe('done')
    await w.setProps({ interrupted: true })
    expect(w.get('icon-stub').attributes('name')).toBe('alert')
    expect(w.text()).toContain('已中断')
    expect(w.text()).not.toContain('已完成')
    w.unmount()
  })

  it('busy → data-state=streaming and 生成中…', () => {
    const w = mountStatus({ busy: true })
    const el = w.find('[data-testid="thought-summary-state"]')
    expect(el.attributes('data-state')).toBe('streaming')
    expect(el.text()).toContain('生成中')
    expect(el.text()).toContain('思考过程')
    w.unmount()
  })

  it('message outputting still generating (busy wins over completed flag)', () => {
    const w = mountStatus({ busy: true, completed: true })
    expect(w.find('[data-testid="thought-summary-state"]').attributes('data-state')).toBe(
      'streaming',
    )
    expect(w.text()).toContain('生成中')
    expect(w.text()).not.toContain('已完成')
    w.unmount()
  })

  it('successful completion → data-state=done + 已完成', () => {
    const w = mountStatus({ busy: false, completed: true })
    const el = w.find('[data-testid="thought-summary-state"]')
    expect(el.attributes('data-state')).toBe('done')
    expect(el.text()).toContain('已完成')
    w.unmount()
  })

  it('interrupted → data-state=interrupted, no 已完成', () => {
    const w = mountStatus({ busy: false, interrupted: true, completed: true })
    const el = w.find('[data-testid="thought-summary-state"]')
    expect(el.attributes('data-state')).toBe('interrupted')
    expect(el.text()).toContain('已中断')
    expect(el.text()).not.toContain('已完成')
    w.unmount()
  })

  // g2.1: revise/cancel failure path must never look like Done (even if completed flagged).
  it('interrupted wins over completed → no Done/已完成', () => {
    const w = mountStatus({ busy: false, interrupted: true, completed: true })
    const el = w.find('[data-testid="thought-summary-state"]')
    expect(el.attributes('data-state')).toBe('interrupted')
    expect(el.text()).not.toMatch(/\bDone\b/)
    expect(el.text()).not.toContain('已完成')
    w.unmount()
  })
})
