// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import enCommon from '@/locales/en/common.json'
import type { AgentTool } from '@/lib/shared/types'
import { registerStageLinks } from '@/lib/run/stageLinks'
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
    expect(w.find('[data-testid="agent-tool-group-names"]').text()).toBe('读取文件 · 搜索代码 · 运行命令 …')
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
    expect(rows.map((r) => r.text())).toEqual(['读取文件完成', '运行命令运行中', '写入文件失败'])
    expect(w.findAll('[data-testid="agent-tool-label"]').map((l) => l.attributes('title'))).toEqual(['read_file', 'Shell', 'write'])
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

describe('AgentToolGroup Grasp-aware rows', () => {
  it('names Grasp MCP tools whatever prefix the agent adds, keeping the raw name in the tooltip', async () => {
    const w = mountGroup([{ title: 'mcp__grasp__set_plan', status: 'completed' }, { title: 'grasp-write_artifact', summary: 'spec.md' }, { title: 'custom_tool' }])
    expect(w.find('[data-testid="agent-tool-group-names"]').text()).toBe('写入计划 · 写入产物 · custom_tool')
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    const labels = w.findAll('[data-testid="agent-tool-label"]')
    expect(labels.map((l) => l.text())).toEqual(['写入计划', '写入产物', 'custom_tool'])
    expect(labels[0]!.attributes('title')).toBe('mcp__grasp__set_plan')
    expect(w.findAll('[data-testid="agent-tool-icon"]').map((i) => i.attributes('name'))).toEqual(['flag', 'artifact', 'sparkles'])
  })

  it('shows durations of a second or more', async () => {
    const w = mountGroup([
      { title: 'Shell', durationMs: 42_000 },
      { title: 'Read', durationMs: 300 },
    ])
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    expect(w.findAll('[data-testid="agent-tool-duration"]').map((d) => d.text())).toEqual(['42s'])
  })

  it('the head names the call still running', () => {
    const w = mountGroup([{ title: 'Read', status: 'completed' }, { title: 'Shell', status: 'in_progress', summary: 'npm ci' }], true)
    expect(w.find('[data-testid="agent-tool-group-current"]').text()).toBe('运行命令 · npm ci')
    expect(w.find('[data-testid="agent-tool-group-names"]').exists()).toBe(false)
  })

  it('counts failures and opens a failed call with its error', async () => {
    const w = mountGroup([
      { title: 'Shell', status: 'completed', output: 'ok' },
      { title: 'Shell', status: 'failed', summary: 'ls /missing', output: 'No such file' },
    ])
    expect(w.find('[data-testid="agent-tool-group-failed"]').text()).toBe('1 个失败')
    await w.find('[data-testid="agent-tool-group-head"]').trigger('click')
    const details = w.findAll('[data-testid="agent-tool-detail"]')
    expect(details).toHaveLength(1)
    expect(details[0]!.text()).toContain('No such file')
    expect(details[0]!.find('[data-testid="agent-tool-raw-name"]').text()).toBe('Shell')
    const toggles = w.findAll('[data-testid="agent-tool-row-toggle"]')
    await toggles[1]!.trigger('click')
    expect(w.findAll('[data-testid="agent-tool-detail"]')).toHaveLength(0)
    await toggles[0]!.trigger('click')
    expect(w.findAll('[data-testid="agent-tool-detail"]')).toHaveLength(1)
  })
})

describe('AgentToolGroup stage links', () => {
  function mountWithStage(tools: AgentTool[], artifacts: string[], preview: boolean) {
    const opened: string[] = []
    const unregister = registerStageLinks('run-1', {
      hasArtifact: (n) => artifacts.includes(n),
      openArtifact: (n) => opened.push(n),
      canOpenPreview: () => preview,
      openPreview: () => opened.push('preview'),
    })
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    const w = mount(AgentToolGroup, { props: { tools, runId: 'run-1', expanded: true }, global: { plugins: [i18n], stubs: { Icon: true } } })
    return { w, opened, unregister }
  }

  it('opens the artifact a finished call wrote and the preview it registered', async () => {
    const { w, opened, unregister } = mountWithStage(
      [
        { title: 'set_plan', status: 'completed' },
        { title: 'write_artifact', status: 'completed', summary: 'spec.md' },
        { title: 'write_artifact', status: 'completed', summary: 'gone.md' },
        { title: 'set_preview', status: 'completed' },
        { title: 'set_research', status: 'failed' },
      ],
      ['plan.json', 'spec.md', 'research.json'],
      true,
    )
    const views = w.findAll('[data-testid="agent-tool-open-artifact"]')
    expect(views.map((v) => v.attributes('title'))).toEqual(['plan.json', 'spec.md'])
    expect(views[0]!.text()).toBe('查看')
    await views[1]!.trigger('click')
    await w.find('[data-testid="agent-tool-open-preview"]').trigger('click')
    expect(opened).toEqual(['spec.md', 'preview'])
    unregister()
    await w.setProps({ tools: [{ title: 'set_plan', status: 'completed' }] })
    expect(w.find('[data-testid="agent-tool-open-artifact"]').exists()).toBe(false)
  })

  it('offers no preview link when the stage cannot show one', () => {
    const { w, unregister } = mountWithStage([{ title: 'set_preview', status: 'completed' }], [], false)
    expect(w.find('[data-testid="agent-tool-open-preview"]').exists()).toBe(false)
    unregister()
  })
})
