// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import McpConfigHelpModal from './McpConfigHelpModal.vue'

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
  return mount(McpConfigHelpModal, {
    props: { open: true, configRoot: '/root/.codebuddy', ...props },
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

describe('McpConfigHelpModal', () => {
  it('opens as 640 AppModal on run, with hint, Got-it footer and Esc close', async () => {
    const wrapper = mountHelp()
    const dialog = document.body.querySelector('.fixed.inset-0 .relative.flex.max-h-\\[88vh\\]') as HTMLElement
    expect(dialog).toBeTruthy()
    expect(dialog.style.maxWidth).toBe('640px')
    expect(document.body.textContent).toContain('MCP 配置帮助')
    expect(document.body.textContent).toContain('运行级变量')
    expect(document.body.textContent).toContain('知道了')
    expect(document.body.textContent).toContain('整份 mcp.json 由你配置')
    expect(document.body.textContent).toContain('/root/.codebuddy/mcp.json')
    expect(document.body.textContent).toContain('GRASP_ARTIFACT_URL')
    expect(document.body.textContent).not.toContain('GRASP_MEMORY_URL')
    expect(document.body.querySelector('[data-test="mcp-help-run"]')).toBeTruthy()
    expect(document.body.querySelector('[data-help-chip]')).toBeFalsy()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('renders the run docs again each time it reopens', async () => {
    const wrapper = mountHelp()
    await wrapper.setProps({ open: false })
    await nextTick()
    await wrapper.setProps({ open: true })
    await nextTick()
    expect(document.body.querySelector('[data-test="mcp-help-run"]')).toBeTruthy()
    wrapper.unmount()
  })

  it('English copy uses Help title / Got it / run vars', async () => {
    const wrapper = mountHelp({}, 'en')
    expect(document.body.textContent).toContain('MCP config help')
    expect(document.body.textContent).toContain('Got it')
    expect(document.body.textContent).toContain('Run-scoped variables')
    wrapper.unmount()
  })
})
