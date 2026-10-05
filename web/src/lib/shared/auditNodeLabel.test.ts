import { describe, expect, it } from 'vitest'
import {
  AUDIT_SYSTEM_LABEL,
  formatAuditNodeName,
  formatAuditNodeTitle,
  matchAuditNodeType,
} from './auditNodeLabel'

describe('matchAuditNodeType', () => {
  it('matches instance ids of the current node types and template roles', () => {
    expect(matchAuditNodeType('agent_2wn4')).toBe('agent')
    expect(matchAuditNodeType('human_gate_vis01')).toBe('human_gate')
    expect(matchAuditNodeType('proposal_select_ab12')).toBe('proposal_select')
    expect(matchAuditNodeType('set_var_x1')).toBe('set_var')
    expect(matchAuditNodeType('clarify')).toBe('clarify')
    expect(matchAuditNodeType('test_review')).toBe('test_review')
  })

  it('does not match a type as a bare substring or a retired type', () => {
    expect(matchAuditNodeType('agentic_foo')).toBe('')
    expect(matchAuditNodeType('react_qnlc')).toBe('')
    expect(matchAuditNodeType('app_preview_12ab')).toBe('')
  })
})

describe('formatAuditNodeName', () => {
  it('formats 阶段名 · 后缀', () => {
    expect(formatAuditNodeTitle('agent_2wn4')).toBe('Agent · 2wn4')
    expect(formatAuditNodeTitle('human_gate_abcd')).toBe('门禁 · abcd')
    expect(formatAuditNodeTitle('proposal_select_pgna')).toBe('方案确认 · pgna')
    expect(formatAuditNodeTitle('branch_ab')).toBe('分支 · ab')
  })

  it('shows the stage only for bare template role ids', () => {
    expect(formatAuditNodeTitle('clarify')).toBe('需求澄清')
    expect(formatAuditNodeTitle('implement')).toBe('实现')
    expect(formatAuditNodeTitle('test_review')).toBe('测试评审')
  })

  it('keeps a typical suffix intact and clips only runaway ids', () => {
    expect(formatAuditNodeTitle('implement_abcdef')).toBe('实现 · abcdef')
    expect(formatAuditNodeTitle('implement_abcdefghijklmn')).toBe('实现 · klmn')
  })

  it('maps empty nodeId to 系统/未归属', () => {
    expect(formatAuditNodeName(undefined).title).toBe(AUDIT_SYSTEM_LABEL)
    expect(formatAuditNodeName('').type).toBe('system')
    expect(formatAuditNodeTitle(null)).toBe(AUDIT_SYSTEM_LABEL)
  })

  it('passes through unknown ids', () => {
    expect(formatAuditNodeTitle('custom_node_1')).toBe('custom_node_1')
  })
})
