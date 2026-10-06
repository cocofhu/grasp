// Node palette definitions for the workflow editor. The six node types match
// server/internal/nodereg; agent node products and outlets follow the chosen
// Agent's capabilities (see web/src/lib/workflow/).

import type { AgentCapabilities } from '@/lib/api/apiTypes'
import type { NodeType, NodeTypeDef } from '@/lib/shared/types'
import { declaredProducts } from '@/lib/workflow/agentCapabilities'

export const NODE_DEFS: Record<NodeType, NodeTypeDef> = {
  input: {
    type: 'input',
    label: 'nodes.input.label',
    desc: 'nodes.input.desc',
    icon: 'input',
    color: 'text-n-input',
    category: 'nodes.categories.control',
    fields: [
      { key: 'variables', label: 'nodes.input.fields.variables.label', type: 'variables' },
    ],
    outputs: [{ key: 'validated', desc: 'nodes.input.outputs.validated.desc' }],
    defaults: {
      variables: [
        { name: 'feature', type: 'paragraph', value: '', desc: '需求描述', ask: true, required: true, editable: true },
        { name: 'repos', type: 'repos', value: [], desc: '仓库列表(平级,每个 clone 到 /root/workspace/<name>/;留空则纯产物流)', ask: true, required: false, editable: true },
      ],
    },
  },
  output: {
    type: 'output',
    label: 'nodes.output.label',
    desc: 'nodes.output.desc',
    icon: 'output',
    color: 'text-n-input',
    category: 'nodes.categories.control',
    fields: [
      { key: 'results', label: 'nodes.output.fields.results.label', type: 'output_sources', help: 'nodes.output.fields.results.help', optional: true },
      {
        key: 'auto_leftover_draft',
        label: 'nodes.output.fields.auto_leftover_draft.label',
        type: 'switch',
        optional: true,
        help: 'nodes.output.fields.auto_leftover_draft.help',
      },
    ],
    outputs: [
      { key: 'outputCards', desc: 'nodes.output.outputs.outputCards.desc' },
      { key: 'results', desc: 'nodes.output.outputs.results.desc' },
    ],
    defaults: { auto_leftover_draft: false },
  },
  set_var: {
    type: 'set_var',
    label: 'nodes.set_var.label',
    desc: 'nodes.set_var.desc',
    icon: 'edit',
    color: 'text-n-artifact',
    category: 'nodes.categories.control',
    fields: [{ key: 'assignments', label: 'nodes.set_var.fields.assignments.label', type: 'assignments' }],
    outputs: [{ key: 'vars', desc: 'nodes.set_var.outputs.vars.desc' }],
    defaults: { assignments: [{ var: '', expr: '' }] },
  },
  branch: {
    type: 'branch',
    label: 'nodes.branch.label',
    desc: 'nodes.branch.desc',
    icon: 'branch',
    color: 'text-n-branch',
    category: 'nodes.categories.control',
    fields: [{ key: 'cases', label: 'nodes.branch.fields.cases.label', type: 'cases' }],
    outputs: [{ key: 'matched', desc: 'nodes.branch.outputs.matched.desc' }],
    defaults: { cases: [{ id: 'case_1', when: 'exists("design.md")' }] },
  },
  agent: {
    type: 'agent',
    label: 'nodes.agent.label',
    desc: 'nodes.agent.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.agent.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.agent.fields.prompt.label', type: 'prompt', placeholder: 'nodes.agent.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.agent.fields.timeout.label', type: 'duration', optional: true },
    ],
    outputs: [
      { key: 'content', desc: 'nodes.agent.outputs.content.desc' },
      { key: 'narration_summary', desc: 'nodes.agent.outputs.narration_summary.desc' },
      { key: 'branch', desc: 'nodes.agent.outputs.branch.desc' },
      { key: 'commit_sha', desc: 'nodes.agent.outputs.commit_sha.desc' },
      { key: 'pushed', desc: 'nodes.agent.outputs.pushed.desc' },
      { key: 'changed_files', desc: 'nodes.agent.outputs.changed_files.desc' },
      { key: 'diff_stat', desc: 'nodes.agent.outputs.diff_stat.desc' },
    ],
    defaults: { agent_profile: '', prompt: '' },
    help: 'nodes.agent.help',
  },
  human_gate: {
    type: 'human_gate',
    label: 'nodes.human_gate.label',
    desc: 'nodes.human_gate.desc',
    icon: 'gate',
    color: 'text-n-gate',
    category: 'nodes.categories.collaboration',
    fields: [
      { key: 'title', label: 'nodes.human_gate.fields.title.label', type: 'text', placeholder: 'nodes.human_gate.fields.title.placeholder' },
      { key: 'body_template', label: 'nodes.human_gate.fields.body_template.label', type: 'select', help: 'nodes.human_gate.fields.body_template.help' },
      { key: 'actions', label: 'nodes.human_gate.fields.actions.label', type: 'actions' },
      { key: 'output_var', label: 'nodes.human_gate.fields.output_var.label', type: 'text', placeholder: 'nodes.human_gate.fields.output_var.placeholder', optional: true },
      { key: 'form', label: 'nodes.human_gate.fields.form.label', type: 'form' },
      { key: 'timeout', label: 'nodes.human_gate.fields.timeout.label', type: 'duration', optional: true },
    ],
    outputs: [
      { key: 'action', desc: 'nodes.human_gate.outputs.action.desc' },
      { key: 'form', desc: 'nodes.human_gate.outputs.form.desc' },
      { key: 'reviewer_id', desc: 'nodes.human_gate.outputs.reviewer_id.desc' },
      { key: 'preview_issues', desc: 'nodes.human_gate.outputs.preview_issues.desc' },
    ],
    defaults: {
      title: '人工评审',
      output_var: 'action',
      actions: [
        { id: 'approve', label: '批准' },
        { id: 'revise', label: '退回修改' },
      ],
      form: [{ key: 'comment', label: '评审意见', required: false }],
    },
    help: 'nodes.human_gate.help',
  },
}

export const PALETTE_GROUPS: { title: string; types: NodeType[] }[] = [
  { title: 'nodes.palette.control', types: ['input', 'output', 'set_var', 'branch'] },
  { title: 'nodes.palette.agent', types: ['agent'] },
  { title: 'nodes.palette.collaboration', types: ['human_gate'] },
]

/**
 * Output rows of an agent node: the generic agent outputs plus, per declared
 * product, its rendered markdown key and raw `_json` key (page has no JSON).
 * Descriptions are i18n keys.
 */
export function agentOutputDefs(caps: AgentCapabilities | null | undefined): { key: string; desc: string }[] {
  const outs: { key: string; desc: string }[] = []
  for (const p of declaredProducts(caps)) {
    outs.push({ key: p.outputKey, desc: `nodes.schemas.${p.name}.markdown` })
    if (p.outputJsonKey) outs.push({ key: p.outputJsonKey, desc: `nodes.schemas.${p.name}.json` })
  }
  const seen = new Set(outs.map((o) => o.key))
  for (const o of NODE_DEFS.agent.outputs) if (!seen.has(o.key)) outs.push(o)
  return outs
}

/** True when human_gate body_template binds page.html (PreviewIssue path). */
export function isPageHtmlGateBody(bodyTemplate: unknown): boolean {
  const s = String(bodyTemplate ?? '')
  return /\.outputs\.page\b/.test(s) || s.includes('page.html')
}

const HUMAN_GATE_COMMENT_FORM = [{ key: 'comment', label: '评审意见', required: false }] as const

/** Default form for human_gate: empty on page.html path, comment form otherwise. */
export function defaultHumanGateForm(bodyTemplate: unknown) {
  return isPageHtmlGateBody(bodyTemplate) ? [] : [...HUMAN_GATE_COMMENT_FORM]
}

/** Sync human_gate form defaults when body_template selects preview vs structured path. */
export function syncHumanGateFormDefaults(config: Record<string, any>): void {
  if (!config || config.body_template === undefined) return
  config.form = defaultHumanGateForm(config.body_template)
}

const NODE_HUE: Record<NodeType, number> = {
  input: 2,
  output: 5,
  set_var: 7,
  branch: 8,
  agent: 1,
  human_gate: 6,
}

/** Theme-aware accent for a node type (`--c-hue-*` tokens); unknown types use the agent hue. */
export function nodeColor(type: NodeType | string, alpha?: number): string {
  const v = `var(--c-hue-${NODE_HUE[type as NodeType] ?? NODE_HUE.agent})`
  return alpha == null ? `rgb(${v})` : `rgb(${v} / ${alpha})`
}
