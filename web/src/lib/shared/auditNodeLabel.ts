/** Demo-first stage labels for audit (plan g2.2). Conflicts with nodereg
 *  (视觉网页 / 代码调研 / 人工门禁 / 应用预览) resolve to Demo copy. */
const AUDIT_STAGE_LABEL: Record<string, string> = {
  research: '调研',
  proposal: '方案',
  human_gate: '门禁',
  gate: '门禁',
  visual: '视觉',
  react: '需求澄清',
  grasp: 'Grasp',
  approve: 'Grasp',
  preflight: '环境确认',
  plan: '计划',
  implement: '实现',
  test: '测试',
  review: '评审',
  app_preview: '预览',
  submit_mr: '提交 MR',
  proposal_select: '方案确认',
  input: '输入',
  output: '输出',
  set_var: '赋值',
  branch: '分支',
  agent: '通用',
}

/** Longest-prefix first so proposal_select beats proposal, human_gate beats gate. */
const AUDIT_NODE_TYPES = Object.keys(AUDIT_STAGE_LABEL).sort((a, b) => b.length - a.length)

export const AUDIT_SYSTEM_LABEL = '系统/未归属'

export type AuditNodeName = {
  title: string
  type: string
  short: string
  fullId: string
}

export function matchAuditNodeType(nodeId: string): string {
  const id = nodeId.trim()
  if (!id) return ''
  for (const t of AUDIT_NODE_TYPES) {
    if (id === t || id.startsWith(`${t}_`)) return t
  }
  return ''
}

export function formatAuditNodeName(nodeId?: string | null): AuditNodeName {
  const id = (nodeId || '').trim()
  if (!id) {
    return { title: AUDIT_SYSTEM_LABEL, type: 'system', short: '', fullId: '' }
  }
  const type = matchAuditNodeType(id)
  if (!type) {
    return { title: id, type: 'unknown', short: '', fullId: id }
  }
  const stage = AUDIT_STAGE_LABEL[type] || type
  const suffix = id === type ? '' : id.slice(type.length).replace(/^_/, '')
  if (!suffix) {
    return { title: stage, type, short: '', fullId: id }
  }
  // Typical instance tails are 4 chars (调研 · 2wn4). Show the full suffix when
  // shorter or slightly longer (门禁 · vis01); only clip runaway ids.
  const short = suffix.length <= 12 ? suffix : suffix.slice(-4)
  return { title: `${stage} · ${short}`, type, short, fullId: id }
}

export function formatAuditNodeTitle(nodeId?: string | null): string {
  return formatAuditNodeName(nodeId).title
}
