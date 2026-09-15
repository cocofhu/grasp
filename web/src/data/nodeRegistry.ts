// Node palette definitions for the workflow editor. Structured-product mappings
// (artifact names, output keys) are generated from server/internal/nodereg —
// see web/src/lib/run/structuredArtifacts.ts and nodeManifest.generated.json.

import type { NodeType, NodeTypeDef } from '@/lib/shared/types'
import { productOutputDefs } from '@/lib/run/productNodeArtifacts'

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
    ],
    outputs: [
      { key: 'outputCards', desc: 'nodes.output.outputs.outputCards.desc' },
      { key: 'results', desc: 'nodes.output.outputs.results.desc' },
    ],
    defaults: {},
  },
  react: {
    type: 'react',
    label: 'nodes.react.label',
    desc: 'nodes.react.desc',
    icon: 'chat',
    color: 'text-n-clarify',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.react.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.react.fields.prompt.label', type: 'prompt', placeholder: 'nodes.react.fields.prompt.placeholder' },
      { key: 'max_rounds', label: 'nodes.react.fields.max_rounds.label', type: 'number', placeholder: 'nodes.react.fields.max_rounds.placeholder' },
      { key: 'auto_var', label: 'nodes.react.fields.auto_var.label', type: 'text', placeholder: 'nodes.react.fields.auto_var.placeholder', optional: true },
      { key: 'timeout', label: 'nodes.react.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.react.fields.conditional_prompt.label', type: 'conditional', optional: true },
    ],
    outputs: productOutputDefs('react', [
      { key: 'transcript', desc: 'nodes.react.outputs.transcript.desc' },
    ]),
    defaults: { max_rounds: 6, prompt: '针对以下需求提出澄清问题,直到信息充分,再调用 set_clarified_requirement 写入结构化需求:\n{{vars.feature}}' },
    help: 'nodes.react.help',
  },
  grasp: {
    type: 'grasp',
    label: 'nodes.grasp.label',
    desc: 'nodes.grasp.desc',
    icon: 'check',
    color: 'text-n-clarify',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.grasp.fields.agent_profile.label', type: 'select' },
      { key: 'timeout', label: 'nodes.grasp.fields.timeout.label', type: 'duration', optional: true },
    ],
    outputs: productOutputDefs('grasp', [
      { key: 'transcript', desc: 'nodes.grasp.outputs.transcript.desc' },
    ]),
    defaults: { timeout: 30 },
    help: 'nodes.grasp.help',
  },
  // Historical type string — same inspector as grasp so old graphs still open.
  approve: {
    type: 'approve',
    label: 'nodes.grasp.label',
    desc: 'nodes.grasp.desc',
    icon: 'check',
    color: 'text-n-clarify',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.grasp.fields.agent_profile.label', type: 'select' },
      { key: 'timeout', label: 'nodes.grasp.fields.timeout.label', type: 'duration', optional: true },
    ],
    outputs: productOutputDefs('grasp', [
      { key: 'transcript', desc: 'nodes.grasp.outputs.transcript.desc' },
    ]),
    defaults: { timeout: 30 },
    help: 'nodes.grasp.help',
  },
  preflight: {
    type: 'preflight',
    label: 'nodes.preflight.label',
    desc: 'nodes.preflight.desc',
    icon: 'ci',
    color: 'text-n-ci',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.preflight.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.preflight.fields.prompt.label', type: 'prompt', placeholder: 'nodes.preflight.fields.prompt.placeholder' },
      { key: 'max_rounds', label: 'nodes.preflight.fields.max_rounds.label', type: 'number', placeholder: 'nodes.preflight.fields.max_rounds.placeholder' },
      { key: 'timeout', label: 'nodes.preflight.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.preflight.fields.conditional_prompt.label', type: 'conditional', optional: true },
    ],
    outputs: productOutputDefs('preflight', [
      { key: 'transcript', desc: 'nodes.preflight.outputs.transcript.desc' },
    ]),
    defaults: {
      max_rounds: 6,
      prompt:
        '对照计划/仓库/变量核对执行环境。有缺口用 ask_question 或 ask_form;确认后 set_preflight 再 node_complete。无缺口则写 confirmed 空 fields 直通。密码明文。\n{{vars.feature}}',
    },
    help: 'nodes.preflight.help',
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
      { key: 'produces', label: 'nodes.agent.fields.produces.label', type: 'text', placeholder: 'nodes.agent.fields.produces.placeholder', optional: true },
      { key: 'conditional_prompt', label: 'nodes.agent.fields.conditional_prompt.label', type: 'conditional', optional: true },
    ],
    outputs: [
      { key: 'content', desc: 'nodes.agent.outputs.content.desc' },
      { key: 'narration_summary', desc: 'nodes.agent.outputs.narration_summary.desc' },
      { key: 'artifact_id', desc: 'nodes.agent.outputs.artifact_id.desc' },
      { key: 'branch', desc: 'nodes.agent.outputs.branch.desc' },
      { key: 'base_branch', desc: 'nodes.agent.outputs.base_branch.desc' },
      { key: 'new_branch', desc: 'nodes.agent.outputs.new_branch.desc' },
      { key: 'commit_sha', desc: 'nodes.agent.outputs.commit_sha.desc' },
      { key: 'pushed', desc: 'nodes.agent.outputs.pushed.desc' },
      { key: 'changed_files', desc: 'nodes.agent.outputs.changed_files.desc' },
      { key: 'diff_stat', desc: 'nodes.agent.outputs.diff_stat.desc' },
    ],
    defaults: {},
    help: 'nodes.agent.help',
  },
  plan: {
    type: 'plan',
    label: 'nodes.plan.label',
    desc: 'nodes.plan.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.plan.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.plan.fields.prompt.label', type: 'prompt', placeholder: 'nodes.plan.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.plan.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.plan.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('plan'),
    defaults: { prompt: '基于上游产物制定实施计划(最多两级:大目标→小目标),用 set_plan 写入' },
    help: 'nodes.plan.help',
  },
  implement: {
    type: 'implement',
    label: 'nodes.implement.label',
    desc: 'nodes.implement.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.implement.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.implement.fields.prompt.label', type: 'prompt', placeholder: 'nodes.implement.fields.prompt.placeholder' },
      { key: 'max_rounds', label: 'nodes.implement.fields.max_rounds.label', type: 'number', placeholder: 'nodes.implement.fields.max_rounds.placeholder' },
      { key: 'timeout', label: 'nodes.implement.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.implement.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('implement', [
      { key: 'branches', desc: 'nodes.implement.outputs.branches.desc' },
      { key: 'pushed', desc: 'nodes.implement.outputs.pushed.desc' },
      { key: 'changed_files', desc: 'nodes.implement.outputs.changed_files.desc' },
    ]),
    defaults: {
      max_rounds: 3,
      prompt:
        '用 get_plan 读取计划逐项实现,用 update_plan_status 标记进度。若存在预览打回请依据 {{vars.preview_issues}}（含 selector 与截图）修改。完成后调用 set_implementation_result 写入实现结果',
    },
    help: 'nodes.implement.help',
  },
  research: {
    type: 'research',
    label: 'nodes.research.label',
    desc: 'nodes.research.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.research.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.research.fields.prompt.label', type: 'prompt', placeholder: 'nodes.research.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.research.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.research.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('research'),
    defaults: { prompt: '围绕上游需求做技术调研,给出问题结论与关键发现,用 set_research 写入' },
    help: 'nodes.research.help',
  },
  test: {
    type: 'test',
    label: 'nodes.test.label',
    desc: 'nodes.test.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.test.fields.agent_profile.label', type: 'select' },
      { key: 'reason_var', label: 'nodes.test.fields.reason_var.label', type: 'text', placeholder: 'nodes.test.fields.reason_var.placeholder', optional: true },
      { key: 'repoScope', label: 'nodes.test.fields.repoScope.label', type: 'text', placeholder: 'nodes.test.fields.repoScope.placeholder', optional: true },
      { key: 'block_on_skipped', label: 'nodes.test.fields.block_on_skipped.label', type: 'switch', optional: true },
      { key: 'prompt', label: 'nodes.test.fields.prompt.label', type: 'prompt', placeholder: 'nodes.test.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.test.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.test.fields.conditional_prompt.label', type: 'conditional', optional: true },
    ],
    outputs: productOutputDefs('test'),
    defaults: { reason_var: 'reason', repoScope: 'all', block_on_skipped: false, exits: { pass: { goto: '' }, fail: { goto: '' } }, prompt: '对上游实现执行测试并如实记录结果,用 set_test_result 写入测试总结' },
    help: 'nodes.test.help',
  },
  review: {
    type: 'review',
    label: 'nodes.review.label',
    desc: 'nodes.review.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.review.fields.agent_profile.label', type: 'select' },
      { key: 'reason_var', label: 'nodes.review.fields.reason_var.label', type: 'text', placeholder: 'nodes.review.fields.reason_var.placeholder', optional: true },
      { key: 'prompt', label: 'nodes.review.fields.prompt.label', type: 'prompt', placeholder: 'nodes.review.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.review.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.review.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('review'),
    defaults: { reason_var: 'reason', exits: { pass: { goto: '' }, fail: { goto: '' } }, prompt: '评审上游实现/设计,给出结论与按严重度排列的意见,用 set_review 写入' },
    help: 'nodes.review.help',
  },
  proposal: {
    type: 'proposal',
    label: 'nodes.proposal.label',
    desc: 'nodes.proposal.desc',
    icon: 'robot',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.proposal.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.proposal.fields.prompt.label', type: 'prompt', placeholder: 'nodes.proposal.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.proposal.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.proposal.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('proposal'),
    defaults: { prompt: '针对上游需求给出 1-3 个候选方案(含优缺点、权衡、工作量/风险),推荐其一,用 set_proposals 写入' },
    help: 'nodes.proposal.help',
  },
  proposal_select: {
    type: 'proposal_select',
    label: 'nodes.proposal_select.label',
    desc: 'nodes.proposal_select.desc',
    icon: 'gate',
    color: 'text-n-gate',
    category: 'nodes.categories.collaboration',
    fields: [
      { key: 'title', label: 'nodes.proposal_select.fields.title.label', type: 'text', placeholder: 'nodes.proposal_select.fields.title.placeholder' },
      { key: 'from', label: 'nodes.proposal_select.fields.from.label', type: 'text', placeholder: 'nodes.proposal_select.fields.from.placeholder', optional: true },
      { key: 'auto_var', label: 'nodes.proposal_select.fields.auto_var.label', type: 'text', placeholder: 'nodes.proposal_select.fields.auto_var.placeholder', optional: true },
      { key: 'output_var', label: 'nodes.proposal_select.fields.output_var.label', type: 'text', placeholder: 'nodes.proposal_select.fields.output_var.placeholder', optional: true },
    ],
    outputs: productOutputDefs('proposal_select', [
      { key: 'selected_proposal', desc: 'nodes.proposal_select.outputs.selected_proposal.desc' },
    ]),
    defaults: { title: '选择方案', from: 'proposals.json', auto_var: 'auto_confirm', output_var: 'selected_proposal' },
    help: 'nodes.proposal_select.help',
  },
  submit_mr: {
    type: 'submit_mr',
    label: 'nodes.submit_mr.label',
    desc: 'nodes.submit_mr.desc',
    icon: 'git',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.submit_mr.fields.agent_profile.label', type: 'select', optional: true },
      { key: 'repo', label: 'nodes.submit_mr.fields.repo.label', type: 'repo_select', placeholder: 'nodes.submit_mr.fields.repo.placeholder', optional: true },
      { key: 'source_branch', label: 'nodes.submit_mr.fields.source_branch.label', type: 'text', placeholder: 'nodes.submit_mr.fields.source_branch.placeholder', optional: true },
      { key: 'target_branch', label: 'nodes.submit_mr.fields.target_branch.label', type: 'text', placeholder: 'nodes.submit_mr.fields.target_branch.placeholder', optional: true },
      { key: 'prompt', label: 'nodes.submit_mr.fields.prompt.label', type: 'prompt', placeholder: 'nodes.submit_mr.fields.prompt.placeholder', optional: true },
      { key: 'conditional_prompt', label: 'nodes.submit_mr.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'timeout', label: 'nodes.submit_mr.fields.timeout.label', type: 'duration', optional: true },
    ],
    outputs: [
      { key: 'mr_url', desc: 'nodes.submit_mr.outputs.mr_url.desc' },
      { key: 'mr_has_conflicts', desc: 'nodes.submit_mr.outputs.mr_has_conflicts.desc' },
      { key: 'mr_mergeable', desc: 'nodes.submit_mr.outputs.mr_mergeable.desc' },
      { key: 'pushed', desc: 'nodes.submit_mr.outputs.pushed.desc' },
      { key: 'pushed_sha', desc: 'nodes.submit_mr.outputs.pushed_sha.desc' },
      { key: 'branch', desc: 'nodes.submit_mr.outputs.branch.desc' },
    ],
    defaults: { prompt: '将目标分支合入源分支并解决所有冲突,推送源分支,然后按托管商用 glab/gh 创建从源分支到目标分支的合并请求（MR/PR）。Git 与对应 CLI 凭据由沙箱提供。' },
    help: 'nodes.submit_mr.help',
  },
  visual: {
    type: 'visual',
    label: 'nodes.visual.label',
    desc: 'nodes.visual.desc',
    icon: 'dashboard',
    color: 'text-n-llm',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.visual.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.visual.fields.prompt.label', type: 'prompt', placeholder: 'nodes.visual.fields.prompt.placeholder' },
      { key: 'timeout', label: 'nodes.visual.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.visual.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
    ],
    outputs: productOutputDefs('visual', [
      { key: 'artifact_id', desc: 'nodes.visual.outputs.artifact_id.desc' },
    ]),
    defaults: { prompt: '根据上游需求,基于仓库中现有业务前端做高保真目标态页面:先只读定位目标路由、组件、设计令牌与文案,再生成改后 page.html;无基线时沿用项目设计系统。不要编造通用 demo。' },
    help: 'nodes.visual.help',
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
      { key: 'preview_issues', desc: 'nodes.app_preview.outputs.preview_issues.desc' },
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
  app_preview: {
    type: 'app_preview',
    label: 'nodes.app_preview.label',
    desc: 'nodes.app_preview.desc',
    icon: 'dashboard',
    color: 'text-n-gate',
    category: 'nodes.categories.agent',
    fields: [
      { key: 'agent_profile', label: 'nodes.app_preview.fields.agent_profile.label', type: 'select' },
      { key: 'prompt', label: 'nodes.app_preview.fields.prompt.label', type: 'prompt', placeholder: 'nodes.app_preview.fields.prompt.placeholder' },
      { key: 'max_rounds', label: 'nodes.app_preview.fields.max_rounds.label', type: 'number', placeholder: 'nodes.app_preview.fields.max_rounds.placeholder' },
      { key: 'timeout', label: 'nodes.app_preview.fields.timeout.label', type: 'duration', optional: true },
      { key: 'conditional_prompt', label: 'nodes.app_preview.fields.conditional_prompt.label', type: 'conditional', optional: true },
      { key: 'review_var', label: 'nodes.shared.reviewVar.label', type: 'text', placeholder: 'nodes.shared.reviewVar.placeholder', optional: true },
      { key: 'direct_preview', label: 'nodes.app_preview.fields.direct_preview.label', type: 'switch', optional: true, help: 'nodes.app_preview.fields.direct_preview.help' },
      { key: 'auto_inject', label: 'nodes.app_preview.fields.auto_inject.label', type: 'switch', optional: true, help: 'nodes.app_preview.fields.auto_inject.help' },
      { key: 'title', label: 'nodes.app_preview.fields.title.label', type: 'text', placeholder: 'nodes.app_preview.fields.title.placeholder', optional: true },
    ],
    outputs: [
      { key: 'action', desc: 'nodes.app_preview.outputs.action.desc' },
      { key: 'preview_issues', desc: 'nodes.app_preview.outputs.preview_issues.desc' },
      { key: 'preview_ready', desc: 'nodes.app_preview.outputs.preview_ready.desc' },
    ],
    defaults: {
      max_rounds: 3,
      direct_preview: false,
      auto_inject: true,
      title: '应用预览',
      prompt: '在沙箱内启动应用并 set_preview(port),或对已部署地址 set_preview(url)(port 与 url 二选一),供人工取点标注并复审确认。',
    },
    help: 'nodes.app_preview.help',
  },
  branch: {
    type: 'branch',
    label: 'nodes.branch.label',
    desc: 'nodes.branch.desc',
    icon: 'branch',
    color: 'text-n-branch',
    category: 'nodes.categories.control',
    fields: [{ key: 'cases', label: 'nodes.branch.fields.cases.label', type: 'cases' }],
    outputs: [
      { key: 'matched', desc: 'nodes.branch.outputs.matched.desc' },
      { key: 'goto', desc: 'nodes.branch.outputs.goto.desc' },
    ],
    defaults: { cases: [{ when: 'exists("design.md")', goto: '' }, { when: 'default', goto: '' }] },
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
}

export const PALETTE_GROUPS: { title: string; types: NodeType[] }[] = [
  { title: 'nodes.palette.control', types: ['input', 'output', 'set_var', 'branch'] },
  { title: 'nodes.palette.agent', types: ['grasp', 'react', 'preflight', 'research', 'proposal', 'plan', 'implement', 'app_preview', 'test', 'review', 'submit_mr', 'visual'] },
  { title: 'nodes.palette.collaboration', types: ['human_gate', 'proposal_select'] },
]

/** True when human_gate body_template binds visual page.html (PreviewIssue path). */
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

export function nodeColorHex(type: NodeType): string {
  const map: Record<NodeType, string> = {
    input: '#94A3B8',
    output: '#34D399',
    react: '#22D3EE',
    grasp: '#10B981',
    approve: '#10B981',
    preflight: '#2DD4BF',
    agent: '#A78BFA',
    plan: '#818CF8',
    implement: '#8B5CF6',
    app_preview: '#FBBF24',
    research: '#38BDF8',
    test: '#2DD4BF',
    review: '#F472B6',
    proposal: '#C084FC',
    proposal_select: '#FBBF24',
    submit_mr: '#FB923C',
    visual: '#5EEAD4',
    human_gate: '#FBBF24',
    branch: '#E879F9',
    set_var: '#F59E0B',
  }
  return map[type]
}
