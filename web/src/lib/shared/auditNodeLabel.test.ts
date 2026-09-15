import { describe, expect, it } from 'vitest'
import {
  AUDIT_SYSTEM_LABEL,
  formatAuditNodeName,
  formatAuditNodeTitle,
  matchAuditNodeType,
} from './auditNodeLabel'

describe('matchAuditNodeType (g2.1 longest prefix)', () => {
  it('matches instance ids and prefers longer types', () => {
    expect(matchAuditNodeType('research_2wn4')).toBe('research')
    expect(matchAuditNodeType('proposal_pgna')).toBe('proposal')
    expect(matchAuditNodeType('proposal_select_ab12')).toBe('proposal_select')
    expect(matchAuditNodeType('human_gate_vis01')).toBe('human_gate')
    expect(matchAuditNodeType('gate_vis01')).toBe('gate')
    expect(matchAuditNodeType('app_preview_x1')).toBe('app_preview')
    expect(matchAuditNodeType('submit_mr_zz99')).toBe('submit_mr')
    expect(matchAuditNodeType('research')).toBe('research')
  })

  it('does not match a type as a bare substring', () => {
    expect(matchAuditNodeType('researching_foo')).toBe('')
  })
})

describe('formatAuditNodeName (g2.1 / g2.2 Demo labels)', () => {
  it('formats 阶段名 · 后 4 位', () => {
    expect(formatAuditNodeTitle('research_2wn4')).toBe('调研 · 2wn4')
    expect(formatAuditNodeTitle('proposal_pgna')).toBe('方案 · pgna')
    expect(formatAuditNodeTitle('visual_bqc5')).toBe('视觉 · bqc5')
    expect(formatAuditNodeTitle('gate_vis01')).toBe('门禁 · vis01')
  })

  it('uses Demo stage copy, not nodereg 代码调研 / 视觉网页', () => {
    expect(formatAuditNodeTitle('research_xxxx')).toBe('调研 · xxxx')
    expect(formatAuditNodeTitle('visual_yyyy')).toBe('视觉 · yyyy')
    expect(formatAuditNodeTitle('human_gate_abcd')).toBe('门禁 · abcd')
    expect(formatAuditNodeTitle('app_preview_12ab')).toBe('预览 · 12ab')
    expect(formatAuditNodeTitle('react_qnlc')).toBe('需求澄清 · qnlc')
    expect(formatAuditNodeTitle('approve_ab12')).toBe('Grasp · ab12')
    expect(formatAuditNodeTitle('grasp_ab12')).toBe('Grasp · ab12')
    expect(formatAuditNodeTitle('preflight_x9k2')).toBe('环境确认 · x9k2')
    expect(formatAuditNodeTitle('submit_mr_9k2a')).toBe('提交 MR · 9k2a')
    expect(formatAuditNodeTitle('proposal_select_pgna')).toBe('方案确认 · pgna')
  })

  it('shows full suffix when shorter than 4', () => {
    expect(formatAuditNodeTitle('plan_ab')).toBe('计划 · ab')
    expect(formatAuditNodeTitle('test_1')).toBe('测试 · 1')
  })

  it('keeps a typical suffix intact and clips only runaway ids', () => {
    expect(formatAuditNodeTitle('implement_abcdef')).toBe('实现 · abcdef')
    expect(formatAuditNodeTitle('implement_abcdefghijklmn')).toBe('实现 · klmn')
  })

  it('shows stage only when id is the bare type', () => {
    expect(formatAuditNodeTitle('research')).toBe('调研')
    expect(formatAuditNodeTitle('visual')).toBe('视觉')
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
