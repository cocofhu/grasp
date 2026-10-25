// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import StatusPill from './StatusPill.vue'

function mountPill(status: string) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(StatusPill, {
    props: { status, size: 'sm' },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('StatusPill', () => {
  it('renders localized running label', () => {
    const wrapper = mountPill('running')
    expect(wrapper.text().length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('falls back for unknown status', () => {
    const wrapper = mountPill('unknown_xyz')
    expect(wrapper.find('span').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders waiting_human as a neutral pill with the same label', () => {
    const wrapper = mountPill('waiting_human')
    const classes = wrapper.classes()
    expect(classes).toEqual(expect.arrayContaining(['text-txt2', 'bg-elevated', 'border-line']))
    expect(classes.some((cls) => cls.includes('warn'))).toBe(false)
    expect(wrapper.text()).toContain('等待人工')
    expect(wrapper.get('icon-stub').attributes('name')).toBe('user-hold')
    wrapper.unmount()
  })

  it('keeps semantic colors for running, completed, failed, and queued', () => {
    const cases: Array<[string, string[]]> = [
      ['running', ['text-info', 'bg-info/10', 'border-info/30']],
      ['completed', ['text-ok', 'bg-ok/10', 'border-ok/30']],
      ['failed', ['text-err', 'bg-err/10', 'border-err/30']],
      ['queued', ['text-txt2', 'bg-elevated', 'border-line']],
    ]
    for (const [status, expected] of cases) {
      const wrapper = mountPill(status)
      expect(wrapper.classes()).toEqual(expect.arrayContaining(expected))
      expect(wrapper.classes().some((cls) => cls.includes('warn'))).toBe(false)
      wrapper.unmount()
    }
  })
})
