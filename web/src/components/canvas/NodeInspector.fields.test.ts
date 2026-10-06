// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import NodeInspector from './NodeInspector.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

function node(type: string, config: Record<string, any> = {}, id = type, label = type): WFNode {
  return { id, type, label, position: { x: 0, y: 0 }, config } as WFNode
}

function mountInspector(target: WFNode, allNodes: WFNode[] = [target], edges: WFEdge[] = []) {
  return mount(NodeInspector, {
    props: { node: target, allNodes, edges, agents: [], agentsLoaded: true },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        RouterLink: RouterLinkStub,
        AppButton: { template: '<button type="button" class="app-btn"><slot /></button>' },
        OutputSourcesEditor: { template: '<div data-testid="output-sources" />' },
      },
    },
  })
}

const buttonByText = (w: ReturnType<typeof mountInspector>, text: string) =>
  w.findAll('button').find((b) => b.text().includes(text))!

describe('NodeInspector · control and collaboration fields', () => {
  it('does not show the agent sections for non-agent nodes', () => {
    const w = mountInspector(node('set_var', { assignments: [] }))
    expect(w.find('[data-testid="agent-picker"]').exists()).toBe(false)
    expect(w.find('[data-testid="inspector-capabilities"]').exists()).toBe(false)
    expect(w.find('[data-testid="field-assignments"]').exists()).toBe(true)
  })

  it('edits input variables', async () => {
    const target = node('input', {})
    const w = mountInspector(target)
    expect(w.find('[data-testid="field-variables"]').exists()).toBe(true)
    await buttonByText(w, t('pages.workflowEditor.inspector.variables.add')).trigger('click')
    expect(target.config.variables).toHaveLength(1)
    await w.find('[data-testid="field-variables"] input').setValue('feature')
    expect(target.config.variables[0].name).toBe('feature')
  })

  it('maintains set_var assignments against declared globals', async () => {
    const input = node('input', { variables: [{ name: 'feature', type: 'string' }] }, 'in')
    const target = node('set_var', { assignments: [] })
    const w = mountInspector(target, [input, target])
    await buttonByText(w, t('pages.workflowEditor.inspector.assignments.add')).trigger('click')
    expect(target.config.assignments).toEqual([{ var: '', expr: '' }])
    const [v, expr] = w.findAll('[data-testid="field-assignments"] input')
    await v!.setValue('feature')
    await expr!.setValue('"x"')
    expect(target.config.assignments[0]).toEqual({ var: 'feature', expr: '"x"' })
    expect(w.findAll('#insp-vars option').map((o) => o.attributes('value'))).toContain('feature')
    await w.find(`[data-testid="field-assignments"] button[aria-label="${t('common.buttons.delete')}"]`).trigger('click')
    expect(target.config.assignments).toEqual([])
  })

  it('adds branch cases with fresh ids and renames outlet edges with the case id', async () => {
    const target = node('branch', { cases: [{ id: 'case_1', when: 'a' }] }, 'b')
    const edges: WFEdge[] = [{ id: 'e1', source: 'b', sourceHandle: 'case_1', target: 'x' }]
    const w = mountInspector(target, [target], edges)
    const list = w.find('[data-testid="field-cases-list"]')
    expect(list.text()).toContain('ELSE')
    await buttonByText(w, t('canvas.inspector.cases.add')).trigger('click')
    expect(target.config.cases.map((c: any) => c.id)).toEqual(['case_1', 'case_2'])

    const idInput = w.find('[data-testid="field-cases-list"] input')
    await idInput.setValue('has spaces')
    await idInput.trigger('change')
    expect(target.config.cases[0].id).toBe('has_spaces')
    expect(edges[0]!.sourceHandle).toBe('has_spaces')
  })

  it('edits human gate actions, form fields and body template options', async () => {
    const up = node('agent', { agent_profile: 'a', prompt: '' }, 'impl', '实现')
    const target = node('human_gate', { actions: [{ id: 'approve', label: '通过' }], form: [] }, 'g')
    const w = mountInspector(target, [up, target], [{ id: 'e', source: 'impl', target: 'g' }])
    await buttonByText(w, t('canvas.inspector.actions.add')).trigger('click')
    expect(target.config.actions.map((a: any) => a.id)).toEqual(['approve', 'action2'])
    await buttonByText(w, t('pages.workflowEditor.inspector.actions.requireForm')).trigger('click')
    expect(target.config.actions[0].requireForm).toBe(true)

    await buttonByText(w, t('pages.workflowEditor.inspector.form.add')).trigger('click')
    expect(target.config.form).toHaveLength(1)
    expect(target.config.form[0].key).toBe('field')

    const opts = w.findAll('[data-testid="field-body_template"] option').map((o) => o.attributes('value'))
    expect(opts).toContain('{{nodes.impl.outputs.content}}')
  })

  it('keeps a custom body template as a selectable option', () => {
    const target = node('human_gate', { actions: [], body_template: 'custom.md' }, 'g')
    const opts = mountInspector(target).findAll('[data-testid="field-body_template"] option')
    expect(opts.map((o) => o.attributes('value'))).toEqual(['', 'custom.md'])
    expect(opts[0]!.text()).toBe(t('pages.workflowEditor.inspector.selectBody'))

    const bare = node('human_gate', { actions: [] }, 'g')
    const hint = mountInspector(bare).findAll('[data-testid="field-body_template"] option')
    expect(hint.map((o) => o.text())).toEqual([t('pages.workflowEditor.inspector.connectUpstreamForBody')])
  })

  it('renders the output sources editor and switch fields for output', async () => {
    const target = node('output', { results: [] })
    const w = mountInspector(target)
    expect(w.find('[data-testid="output-sources"]').exists()).toBe(true)
    const sw = w.find('[data-testid="node-switch-auto_leftover_draft"]')
    expect(sw.exists()).toBe(true)
    await sw.trigger('click')
    expect(target.config.auto_leftover_draft).toBe(true)
  })

  it('offers human gate outputs as goal variables to agents', async () => {
    const gate = node('human_gate', { actions: [], form: [{ key: 'notes' }] }, 'g')
    const target = node('agent', { agent_profile: '', prompt: '' }, 'a')
    const w = mountInspector(target, [gate, target])
    const ta = w.find('[data-testid="goal-input"]')
    const el = ta.element as HTMLTextAreaElement
    el.value = '{{'
    el.setSelectionRange(2, 2)
    await ta.trigger('input')
    const text = w.find('[data-testid="goal-suggest"]').text()
    for (const v of ['action', 'notes']) expect(text).toContain(`{{vars.${v}}}`)
  })
})
