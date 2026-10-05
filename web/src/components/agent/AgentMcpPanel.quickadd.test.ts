// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { ARTIFACT_STORE, LEGACY_PM_LEADER, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentMcpPanel from './AgentMcpPanel.vue'

/** Reactive so component-side mutations and test-side edits both re-render. */
function draft(over: Partial<AgentStudioDraft> = {}): AgentStudioDraft {
  return reactive({
    name: 'agent-a',
    projectId: 'p1',
    acpBackend: 'cursor',
    files: [],
    mcp: [],
    env: [],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    capabilities: null,
    ...over,
  }) as AgentStudioDraft
}

function mountPanel(d: AgentStudioDraft, isProjectBound = true) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentMcpPanel, {
    props: { draft: d, isProjectBound },
    global: {
      plugins: [i18n],
      stubs: {
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

    expect(w.find('[data-test="mcp-add-artifact"]').exists()).toBe(true)
    await w.get('[data-test="mcp-add-artifact"]').trigger('click')
    await flushPromises()

    expect(d.mcp[0].name).toBe(ARTIFACT_STORE)
    expect(d.mcp[0].headers[0].k).toBe('Authorization')
    expect(w.find('[data-test="mcp-add-artifact"]').exists()).toBe(false)

    // A direct second call is still a no-op.
    ;(w.vm as any).addArtifactStore()
    expect(d.mcp).toHaveLength(1)
    w.unmount()
  })

  it('refuses platform MCPs until the agent is bound to a project', async () => {
    const d = draft({ projectId: '' })
    const w = mountPanel(d, false)
    await flushPromises()

    expect(w.find('[data-test="mcp-project-required-warn"]').exists()).toBe(true)
    expect(w.get('[data-test="mcp-add-memory"]').attributes('disabled')).toBeDefined()

    ;(w.vm as any).addAgentPlatformMcp('memory-store')
    await flushPromises()
    expect(d.mcp).toHaveLength(0)
    expect(w.emitted('toast')).toHaveLength(1)
    w.unmount()
  })

  it('adds each platform MCP at most once', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()

    await w.get('[data-test="mcp-add-memory"]').trigger('click')
    await w.get('[data-test="mcp-add-context"]').trigger('click')
    await w.get('[data-test="mcp-add-scheduler"]').trigger('click')
    await flushPromises()
    expect(d.mcp.map((m) => m.name)).toEqual(['memory-store', 'context-store', 'task-scheduler'])
    expect(d.mcp[0].url).toBe('${GRASP_MEMORY_URL}')

    await w.get('[data-test="mcp-add-memory"]').trigger('click')
    await flushPromises()
    expect(d.mcp).toHaveLength(3)
    expect(w.emitted('toast')).toHaveLength(1)

    // Preset cards render their friendly name plus a scope note.
    expect(w.findAll('[data-test="mcp-display-name"]')).toHaveLength(3)
    expect(w.findAll('[data-test="mcp-scope-note"]')).toHaveLength(3)
    expect(w.find('[data-test="mcp-custom-name"]').exists()).toBe(false)
    w.unmount()
  })

  it('ignores an unknown platform preset name', async () => {
    const d = draft()
    const w = mountPanel(d)
    await flushPromises()
    ;(w.vm as any).addAgentPlatformMcp('nope' as never)
    expect(d.mcp).toHaveLength(0)
    expect(w.emitted('toast')).toBeFalsy()
    w.unmount()
  })

  it('upgrades a legacy pm-leader entry into the platform presets', async () => {
    const d = draft({
      mcp: [
        { name: LEGACY_PM_LEADER, transport: 'url', url: 'u', headers: [], command: '', args: '', env: [] },
      ] as never,
    })
    const w = mountPanel(d)
    await flushPromises()

    expect(w.find('[data-test="mcp-legacy-pm-hint"]').exists()).toBe(true)
    await w.get('[data-test="mcp-upgrade-legacy"]').trigger('click')
    await flushPromises()

    expect(d.mcp.map((m) => m.name)).toEqual(['memory-store', 'context-store', 'task-scheduler'])
    expect(w.find('[data-test="mcp-legacy-pm-hint"]').exists()).toBe(false)
    expect(w.emitted('toast')).toHaveLength(1)
    w.unmount()
  })

  it('keeps presets that already exist while upgrading', async () => {
    const d = draft({
      mcp: [
        { name: LEGACY_PM_LEADER, transport: 'url', url: 'u', headers: [], command: '', args: '', env: [] },
        { name: 'memory-store', transport: 'url', url: 'm', headers: [], command: '', args: '', env: [] },
      ] as never,
    })
    const w = mountPanel(d)
    await flushPromises()
    await w.get('[data-test="mcp-upgrade-legacy"]').trigger('click')
    await flushPromises()

    expect(d.mcp.filter((m) => m.name === 'memory-store')).toHaveLength(1)
    expect(d.mcp[0].url).toBe('m')
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
