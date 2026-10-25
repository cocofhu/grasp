// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import pagesZh from '@/locales/zh-CN/pages.json'
import commonZh from '@/locales/zh-CN/common.json'
import type { Agent } from '@/lib/api/apiTypes'
import SoloAgentSelect from './SoloAgentSelect.vue'

const agents: Agent[] = [
  { name: '实现', acpBackend: 'cursor', icon: { iconId: 'code', color: 'green' } },
  { name: '测试评审', acpBackend: 'cursor' },
  { name: '坏图标', acpBackend: 'claude_code', icon: { iconId: 'not-real', color: 'purple' } },
]

function mountSelect(modelValue = '实现') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...commonZh, ...pagesZh } },
  })
  return mount(SoloAgentSelect, {
    attachTo: document.body,
    props: { agents, modelValue },
    global: { plugins: [i18n], stubs: { RouterLink: { template: '<a data-picker-opt><slot /></a>' } } },
  })
}

describe('SoloAgentSelect identity icons', () => {
  it('mixes icons and initials on the trigger and rows, and keeps name search', async () => {
    const wrapper = mountSelect()
    await flushPromises()
    const trigger = document.querySelector('[data-testid="solo-agent-select-trigger"]')!
    expect(trigger.querySelector('[data-testid="solo-agent-glyph"] svg')).toBeTruthy()
    expect(trigger.querySelector('[data-testid="solo-agent-glyph"]')?.textContent).not.toContain('实')
    expect(trigger.textContent).toContain('实现')

    trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    const coded = document.querySelector('[data-testid="solo-agent-option-实现"]')!
    const plain = document.querySelector('[data-testid="solo-agent-option-测试评审"]')!
    const broken = document.querySelector('[data-testid="solo-agent-option-坏图标"]')!
    expect(coded.querySelector('[data-testid="solo-agent-glyph"] svg')).toBeTruthy()
    expect(plain.querySelector('[data-testid="solo-agent-initial"]')?.textContent).toContain('测')
    expect(broken.querySelector('[data-testid="solo-agent-initial"]')?.textContent).toContain('坏')
    expect(document.body.textContent).not.toMatch(/[\u{1F300}-\u{1FAFF}]/u)
    expect(document.body.textContent).toContain('管理')

    const search = document.querySelector<HTMLInputElement>('[data-testid="solo-agent-select-search"]')!
    search.value = '测试'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('[data-testid="solo-agent-option-测试评审"]')).toBeTruthy()
    expect(document.querySelector('[data-testid="solo-agent-option-实现"]')).toBeNull()
    wrapper.unmount()
  })
})
