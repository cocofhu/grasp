// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { GRASP_TOOL, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentMcpPanel from './AgentMcpPanel.vue'

vi.mock('@/lib/agent/mcpPresets', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/lib/agent/mcpPresets')>()
  return {
    ...real,
    loadMcpPresets: async () => [
      { id: 'grasp-tool', requiresDesktop: false, server: { name: 'grasp-tool', url: '${GRASP_ARTIFACT_URL}', headers: { Authorization: 'Bearer ${GRASP_ARTIFACT_TOKEN}' } } },
      { id: 'browser', requiresDesktop: false, server: { name: 'chrome-devtools', command: 'chrome-devtools-mcp', args: ['--browser-url=http://127.0.0.1:9222'] } },
      { id: 'desktop', requiresDesktop: true, server: { name: 'desktop', command: '/opt/grasp-runtime/current/bin/grasp-desktop', args: ['mcp'] } },
      { id: 'blender', requiresDesktop: true, server: { name: 'blender', command: '/usr/local/bin/blender-mcp', env: { BLENDER_MCP_PORT: '6092' } } },
    ],
  }
})

/** Reactive so component-side mutations and test-side edits both re-render. */
function draft(over: Partial<AgentStudioDraft> = {}): AgentStudioDraft {
  return reactive({
    name: 'agent-a',
    acpBackend: 'cursor',
    files: [],
    mcp: [],
    env: [],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    capabilities: null,
    ...over,
  }) as AgentStudioDraft
}

function mountPanel(d: AgentStudioDraft) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentMcpPanel, {
    props: { draft: d },
    global: {
      plugins: [i18n],
      stubs: {
        Teleport: true,
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        McpConfigHelpModal: { props: ['open', 'configRoot'], template: '<div v-if="open" data-test="mcp-help-modal" />' },
        CodeEditor: {
          props: ['modelValue', 'language'],
          emits: ['update:modelValue'],
          template: '<textarea data-test="mcp-raw" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
        },
      },
    },
  })
}

describe('AgentMcpPanel', () => {
  it('searches transport choices and keeps URL and command drafts when switching', async () => {
    const d = draft({ mcp: [{ name: 'custom', transport: 'url', url: 'https://mcp.example.test', headers: [{ k: 'Authorization', v: 'Bearer test' }], command: 'npx', args: '-y\nexample', env: [{ k: 'TEST', v: 'value' }] }] })
    const w = mountPanel(d)
    const picker = w.get('[data-testid="mcp-transport-0"]')
    await picker.get('[data-test="app-select-trigger"]').trigger('click')
    await w.get('[data-test="app-select-search"]').setValue('stdio')
    expect(w.find('[data-test="app-select-option-url"]').exists()).toBe(false)
    await w.get('[data-test="app-select-option-command"]').trigger('click')
    expect(d.mcp[0]!.transport).toBe('command')
    expect(d.mcp[0]!.url).toBe('https://mcp.example.test')
    expect(d.mcp[0]!.headers).toEqual([{ k: 'Authorization', v: 'Bearer test' }])
    expect(w.find('textarea').exists()).toBe(true)
    await picker.get('[data-test="app-select-trigger"]').trigger('click')
    await w.get('[data-test="app-select-option-url"]').trigger('click')
    expect(d.mcp[0]!.transport).toBe('url')
    expect(d.mcp[0]!.command).toBe('npx')
    expect(d.mcp[0]!.args).toBe('-y\nexample')
    expect(d.mcp[0]!.env).toEqual([{ k: 'TEST', v: 'value' }])
    w.unmount()
  })

  it('adds and removes blank service cards', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()
    const vm = w.vm as any

    vm.addMcp()
    vm.addMcp()
    await flushPromises()
    expect(d.mcp).toHaveLength(2)
    expect(w.findAll('[data-test="mcp-card"]')).toHaveLength(2)

    await w.findAll('[data-test="mcp-remove"]')[0].trigger('click')
    expect(d.mcp).toHaveLength(1)
    w.unmount()
  })

  it('adds the artifact store once and hides the shortcut afterwards', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()

    expect(w.find('[data-test="mcp-add-grasp-tool"]').exists()).toBe(true)
    await w.get('[data-test="mcp-add-grasp-tool"]').trigger('click')
    await flushPromises()

    expect(d.mcp[0].name).toBe(GRASP_TOOL)
    expect(d.mcp[0].headers[0].k).toBe('Authorization')
    expect(w.find('[data-test="mcp-add-grasp-tool"]').exists()).toBe(false)
    expect(w.get('[data-test="mcp-display-name"]').text()).toBe('Grasp Tool')
    expect(w.find('[data-test="mcp-scope-note"]').exists()).toBe(true)
    expect(w.find('[data-test="mcp-custom-name"]').exists()).toBe(false)

    // A direct second call is still a no-op.
    ;(w.vm as any).addPreset((w.vm as any).presets[0])
    expect(d.mcp).toHaveLength(1)
    w.unmount()
  })

  it('quick-adds browser, desktop and Blender presets with a desktop-image note', async () => {
    const d = draft({ mcp: [{ name: 'custom', transport: 'url', url: 'https://x', headers: [], command: '', args: '', env: [] }] })
    const w = mountPanel(d)
    await flushPromises()

    expect(w.get('[data-test="mcp-add-desktop"]').attributes('title')).toBe('需要桌面镜像')
    expect(w.get('[data-test="mcp-add-browser"]').attributes('title')).toBeUndefined()
    await w.get('[data-test="mcp-add-browser"]').trigger('click')
    await w.get('[data-test="mcp-add-desktop"]').trigger('click')
    await w.get('[data-test="mcp-add-blender"]').trigger('click')
    await flushPromises()

    expect(d.mcp.map((m) => m.name)).toEqual(['custom', 'chrome-devtools', 'desktop', 'blender'])
    expect(d.mcp[2]).toMatchObject({ transport: 'command', command: '/opt/grasp-runtime/current/bin/grasp-desktop', args: 'mcp' })
    expect(d.mcp[3].env).toEqual([{ k: 'BLENDER_MCP_PORT', v: '6092' }])
    expect(w.findAll('[data-test="mcp-desktop-note"]')).toHaveLength(2)
    expect(w.find('[data-test="mcp-add-browser"]').exists()).toBe(false)
    expect(w.find('[data-test="mcp-add-grasp-tool"]').exists()).toBe(true)
    w.unmount()
  })

  it('round-trips the draft through the raw JSON editor', async () => {
    const d = draft({
      mcp: [
        { name: 'a', transport: 'url', url: 'https://a', headers: [{ k: 'H', v: '1' }], command: '', args: '', env: [] },
      ] as never,
    })
    const w = mountPanel(d)
    await flushPromises()
    const vm = w.vm as any

    expect(w.find('[data-test="mcp-raw"]').exists()).toBe(false)
    vm.toggleMcpRaw()
    await flushPromises()
    expect(vm.mcpRaw).toBe(true)
    expect(JSON.parse(vm.mcpRawText)).toEqual([{ name: 'a', url: 'https://a', headers: { H: '1' } }])
    expect(w.find('[data-test="mcp-raw"]').exists()).toBe(true)

    const next = JSON.stringify([{ name: 'b', command: 'npx', args: ['-y', 'pkg'], env: { K: 'v' } }])
    await w.get('[data-test="mcp-raw"]').setValue(next)
    await flushPromises()
    expect(vm.rawError).toBe('')
    expect(d.mcp).toHaveLength(1)
    expect(d.mcp[0].transport).toBe('command')
    expect(d.mcp[0].args).toBe('-y\npkg')
    expect(d.mcp[0].env).toEqual([{ k: 'K', v: 'v' }])
    w.unmount()
  })

  it('reports malformed and non-array raw JSON', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()
    const vm = w.vm as any
    vm.toggleMcpRaw()
    await flushPromises()

    vm.onMcpRaw('{ nope')
    await flushPromises()
    expect(vm.rawError).toBeTruthy()
    expect(w.text()).toContain(vm.rawError)

    vm.onMcpRaw('{"a":1}')
    await flushPromises()
    expect(vm.rawError).toBeTruthy()
    expect(d.mcp).toEqual([])

    vm.onMcpRaw('[]')
    await flushPromises()
    expect(vm.rawError).toBe('')
    w.unmount()
  })

  it('leaves raw mode when the edited agent changes', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()
    const vm = w.vm as any

    vm.toggleMcpRaw()
    vm.onMcpRaw('{ bad')
    await flushPromises()
    expect(vm.mcpRaw).toBe(true)

    d.name = 'agent-b'
    await flushPromises()
    expect(vm.mcpRaw).toBe(false)
    expect(vm.rawError).toBe('')

    // Toggling back off also clears the error.
    vm.toggleMcpRaw()
    vm.toggleMcpRaw()
    expect(vm.mcpRaw).toBe(false)
    w.unmount()
  })

  it('edits headers, args and env rows on a custom service', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()
    const vm = w.vm as any
    vm.addMcp()
    await flushPromises()

    const row = d.mcp[0]
    await w.get('[data-test="mcp-custom-name"]').setValue('custom')
    expect(row.name).toBe('custom')

    // URL transport exposes header rows.
    const addHeader = w.findAll('button').find((b) => b.text().includes('Header') || b.text().includes('请求头'))
    expect(addHeader).toBeTruthy()
    await addHeader!.trigger('click')
    expect(row.headers).toHaveLength(1)

    // Switching to stdio swaps in command/args/env inputs.
    row.transport = 'command'
    await flushPromises()
    expect(w.find('textarea').exists()).toBe(true)
    row.env.push({ k: 'K', v: 'v' })
    await flushPromises()
    expect(w.html()).toContain('KEY')
    w.unmount()
  })

  it('opens the config help modal with the draft config root', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()

    expect(w.find('[data-test="mcp-help-modal"]').exists()).toBe(false)
    await w.get('[data-test="mcp-help-link"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="mcp-help-modal"]').exists()).toBe(true)
    w.unmount()
  })
})
