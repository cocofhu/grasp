/** Audit stage labels keyed by node-id prefix: the 7 node types plus the default template's role ids. */
const AUDIT_STAGE_LABEL: Record<string, string> = {
  input: '输入',
  output: '输出',
  set_var: '赋值',
  branch: '分支',
  agent: 'Agent',
  human_gate: '门禁',
  proposal_select: '方案确认',
  clarify: '需求澄清',
  implement: '实现',
  test_review: '测试评审',
}

/** Longest prefix first so a type never shadows a longer one sharing its head. */
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
