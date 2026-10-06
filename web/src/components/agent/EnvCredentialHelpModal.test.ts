// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import EnvCredentialHelpModal from './EnvCredentialHelpModal.vue'

function mountHelp(
  props: Record<string, unknown> = {},
  locale: 'zh-CN' | 'en' = 'zh-CN',
) {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...commonEn, ...pagesEn },
    },
  })
  return mount(EnvCredentialHelpModal, {
    props: { open: true, section: 'inject', ...props },
    global: {
      plugins: [i18n],
      stubs: { Transition: false },
    },
    attachTo: document.body,
  })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('EnvCredentialHelpModal', () => {
  it('opens as 640 AppModal with title, chips, Got-it footer and Esc close', async () => {
    const wrapper = mountHelp({ section: 'inject' })
    const dialog = document.body.querySelector('.fixed.inset-0 .relative.flex.max-h-\\[88vh\\]') as HTMLElement
    expect(dialog).toBeTruthy()
    expect(dialog.style.maxWidth).toBe('640px')
    expect(document.body.textContent).toContain('环境变量与凭据')
    expect(document.body.textContent).toContain('注入说明')
    expect(document.body.textContent).toContain('Git 凭据')
    expect(document.body.textContent).toContain('ACP 鉴权')
    expect(document.body.textContent).toContain('知道了')
    expect(document.body.textContent).toContain('环境变量会注入该 Agent 的沙箱容器')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('positions inject / git / acp sections and keeps a single instance when section changes', async () => {
    const wrapper = mountHelp({ section: 'git' })
    await nextTick()
    expect(document.body.querySelectorAll('[data-test="env-credential-help"]')).toHaveLength(1)
    expect(document.body.querySelector('[data-help-chip="git"]')?.className).toContain('border-accent')
    expect(document.body.textContent).toContain('不会验证变量引用的实际值')
    expect(document.body.textContent).toContain('远端 clone / push 权限')
    expect(document.body.textContent).toContain('本页不逐仓展开')

    await wrapper.setProps({ section: 'acp', backend: 'cursor' })
    await nextTick()
    expect(document.body.querySelectorAll('[data-test="env-credential-help"]')).toHaveLength(1)
    expect(document.body.querySelector('[data-help-chip="acp"]')?.className).toContain('border-accent')
    expect(document.body.textContent).toContain('GRASP_CURSOR_API_KEY')
    expect(document.body.textContent).toContain('Cursor ACP 鉴权')
    expect(document.body.textContent).not.toMatch(/sk-[a-z0-9]{8,}/i)
    wrapper.unmount()
  })

  it('invalid section still opens and stays on inject', async () => {
    const wrapper = mountHelp({ section: 'nope' })
    await nextTick()
    expect(document.body.textContent).toContain('环境变量与凭据')
    expect(document.body.querySelector('[data-help-chip="inject"]')?.className).toContain('border-accent')
    wrapper.unmount()
  })

  it('chip click jumps chapter without closing', async () => {
    const wrapper = mountHelp({ section: 'inject' })
    await nextTick()
    const gitChip = document.body.querySelector('[data-help-chip="git"]') as HTMLButtonElement
    gitChip.click()
    await nextTick()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(document.body.querySelector('[data-help-chip="git"]')?.className).toContain('border-accent')
    wrapper.unmount()
  })

  it('English copy uses Help / Got it / Env & credentials', async () => {
    const wrapper = mountHelp({ section: 'inject' }, 'en')
    expect(document.body.textContent).toContain('Env & credentials')
    expect(document.body.textContent).toContain('Got it')
    expect(document.body.textContent).toContain('Injection')
    expect(document.body.textContent).toContain('Git credentials')
    expect(document.body.textContent).toContain('ACP auth')
    wrapper.unmount()
  })

  it('elevated overlay sits at z-index 60 for wizard stacking', async () => {
    const wrapper = mountHelp({ section: 'git', elevated: true })
    await nextTick()
    await nextTick()
    const overlay = document.body.querySelector('.fixed.inset-0.flex.items-center') as HTMLElement
    expect(overlay?.style.zIndex).toBe('60')
    wrapper.unmount()
  })
})
