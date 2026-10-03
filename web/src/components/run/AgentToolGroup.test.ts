// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import enCommon from '@/locales/en/common.json'
import type { AgentTool } from '@/lib/shared/types'
import AgentToolGroup from './AgentToolGroup.vue'

function mountGroup(tools: AgentTool[], busy = false, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: { 'zh-CN': { ...common, ...pages }, en: { ...enCommon, ...enPages } },
  })
  return mount(AgentToolGroup, {
    props: { tools, busy },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('AgentToolGroup', () => {
  it('renders nothing without tools', () => {
    expect(mountGroup([]).find('[data-testid="agent-tool-group"]').exists()).toBe(false)
  })

  it('folds into one line with count and the first distinct names', () => {
    const w = mountGroup([
      { title: 'read_file', status: 'completed' },
      { title: 'read_file', status: 'completed' },
      { title: 'grep', status: 'completed' },
      { title: 'Shell', status: 'completed' },
      { title: 'write', status: 'completed' },
    ])
    const g = w.find('[data-testid="agent-tool-group"]')
    expect(g.attributes('data-state')).toBe('done')
    expect(g.text()).toContain('使用了 5 个工具')
    expect(w.find('[data-testid="agent-tool-group-names"]').text()).toBe('read_file · grep · Shell …')
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(false)
  })

  it('expands to every call with its status', async () => {
    const w = mountGroup(
      [
        { title: 'read_file', status: 'completed' },
        { title: 'Shell', status: 'running' },
        { title: 'write', status: 'failed' },
      ],
      true,
    )
    expect(w.find('[data-testid="agent-tool-group"]').attributes('data-state')).toBe('running')
    const head = w.find('[data-testid="agent-tool-group-head"]')
    expect(head.attributes('aria-expanded')).toBe('false')
    await head.trigger('click')
    expect(head.attributes('aria-expanded')).toBe('true')
    const rows = w.findAll('[data-testid="agent-tool-row"]')
    expect(rows.map((r) => r.attributes('data-state'))).toEqual(['done', 'running', 'failed'])
    expect(rows.map((r) => r.text())).toEqual(['read_file完成', 'Shell运行中', 'write失败'])
    await head.trigger('click')
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(false)
  })

  it('a still-running tool no longer spins once the turn ended', () => {
    const w = mountGroup([{ title: 'Shell', status: 'running' }, { title: 'x', status: 'pending' }], false)
    expect(w.find('[data-testid="agent-tool-group"]').attributes('data-state')).toBe('done')
  })

  it('failed wins over done when idle', () => {
    const w = mountGroup([{ title: 'a', status: 'completed' }, { title: 'b', status: 'failed' }])
    expect(w.find('[data-testid="agent-tool-group"]').attributes('data-state')).toBe('failed')
  })

  it('pluralises in English', () => {
    expect(mountGroup([{ title: 'a' }], false, 'en').text()).toContain('Used 1 tool')
    expect(mountGroup([{ title: 'a' }, { title: 'b' }], false, 'en').text()).toContain('Used 2 tools')
  })
})

describe('AgentToolGroup expanded prop', () => {
  it('follows the parent open state and still toggles locally', async () => {
    const w = mountGroup([{ title: 'a' }])
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(false)
    await w.setProps({ expanded: true })
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(true)
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(false)
    await w.setProps({ expanded: false })
    await w.setProps({ expanded: true })
    expect(w.find('[data-testid="agent-tool-list"]').exists()).toBe(true)
  })
})

describe('AgentToolGroup details', () => {
  it('shows the summary and expands a call with input/output on its own', async () => {
    const w = mountGroup([
      { title: 'Shell', status: 'completed', summary: 'curl -sS http://x', input: '{\n  "command": "curl"\n}', output: 'ok' },
      { title: 'Read', status: 'completed', summary: 'a.ts' },
    ])
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    expect(w.findAll('[data-testid="agent-tool-summary"]').map((s) => s.text())).toEqual(['curl -sS http://x', 'a.ts'])
    const toggles = w.findAll('[data-testid="agent-tool-row-toggle"]')
    expect(toggles).toHaveLength(1)
    expect(toggles[0]!.attributes('aria-expanded')).toBe('false')
    expect(w.find('[data-testid="agent-tool-detail"]').exists()).toBe(false)
    await toggles[0]!.trigger('click')
    expect(toggles[0]!.attributes('aria-expanded')).toBe('true')
    expect(w.find('[data-testid="agent-tool-input"]').text()).toContain('"command": "curl"')
    expect(w.find('[data-testid="agent-tool-output"]').text()).toBe('ok')
    expect(w.text()).toContain('入参')
    await toggles[0]!.trigger('click')
    expect(w.find('[data-testid="agent-tool-detail"]').exists()).toBe(false)
  })

  it('a call with only output shows just that section', async () => {
    const w = mountGroup([{ title: 'Shell', output: 'done' }])
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    await w.find('[data-testid="agent-tool-row-toggle"]').trigger('click')
    expect(w.find('[data-testid="agent-tool-input"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-tool-output"]').text()).toBe('done')
  })
})
