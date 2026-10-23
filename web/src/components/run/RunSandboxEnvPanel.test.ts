// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import type { SandboxMode } from '@/lib/shared/types'
import RunSandboxEnvPanel from './RunSandboxEnvPanel.vue'

function mountPanel(sandboxMode?: SandboxMode) {
  return mount(RunSandboxEnvPanel, {
    props: { entries: [{ key: 'A', value: '1' }], sandboxMode },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })] },
  })
}

describe('RunSandboxEnvPanel shared sandbox badge', () => {
  it('shows the badge for shared runs', () => {
    const wrapper = mountPanel('shared')
    expect(wrapper.get('[data-testid="run-sandbox-shared-badge"]').text()).toBe(pages.pages.runDetail.sandboxEnv.sharedBadge)
  })

  it.each([undefined, 'per_node' as const])('has no badge when sandboxMode is %s', (mode) => {
    const wrapper = mountPanel(mode)
    expect(wrapper.find('[data-testid="run-sandbox-shared-badge"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="run-sandbox-env-row"]')).toHaveLength(1)
  })
})
