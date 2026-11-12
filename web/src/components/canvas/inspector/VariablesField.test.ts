// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import zhPages from '@/locales/zh-CN/pages.json'
import zhCommon from '@/locales/zh-CN/common.json'
import VariablesField from './VariablesField.vue'

describe('VariablesField help', () => {
  it('renders the help markup from slots without an HTML locale message', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...zhCommon, ...zhPages } } })
    const w = mount(VariablesField, { props: { config: { variables: [] } }, global: { plugins: [i18n] } })
    const help = w.get('p')
    expect(help.text()).toBe('所有节点共享、引擎逐步持久化;在任意节点用 {{vars.名称}} 引用,用「赋值」节点修改。勾选启动时填写的变量即运行入参。')
    expect(help.findAll('code').map((c) => c.text())).toEqual(['{{vars.名称}}'])
    expect(help.get('b').text()).toBe('启动时填写')
    expect(warn.mock.calls.flat().join(' ')).not.toContain('Detected HTML')
    warn.mockRestore()
    w.unmount()
  })

  it('offers no repository list type', () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...zhCommon, ...zhPages } } })
    const w = mount(VariablesField, { props: { config: { variables: [{ name: 'x', type: 'string', value: '' }] } }, global: { plugins: [i18n] } })
    expect(w.findAll('option').map((o) => o.attributes('value'))).not.toContain('repos')
    w.unmount()
  })
})
