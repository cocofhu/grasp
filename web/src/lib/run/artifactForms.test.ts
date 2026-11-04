import { describe, expect, it } from 'vitest'
import {
  ARTIFACT_FORMS,
  artifactFormFor,
  emptyListItem,
  fromForm,
  matchFieldPath,
  parseFormContent,
  serializeForm,
  toForm,
} from './artifactForms'
import { isStructuredArtifactName } from '@/components/run/StructuredArtifactView.vue'

const FIXTURES: Record<string, Record<string, unknown>> = {
  'clarified_requirement.json': {
    title: '运行产物人工编辑',
    summary: '让操作者在暂停时修改产物',
    background: '实机验收项无法在沙箱完成',
    work_kind: 'feature',
    goals: ['可编辑'],
    success_metrics: ['每次保存生成新版本'],
    in_scope: ['运行详情'],
    out_of_scope: ['分享页'],
    personas: [{ id: 'u1', name: '操作者', description: '运行负责人', goals: ['改计划'] }],
    user_scenarios: [{ id: 's1', name: '删掉实机项', actor: '操作者', trigger: '复审', flow: ['打开', '删除'], outcome: '流转' }],
    functional_requirements: [
      { id: 'f1', title: '编辑', detail: '表单编辑', priority: 'must', acceptance_criteria: ['保存成功'], scenario_ids: ['s1'] },
    ],
    non_functional_requirements: [{ id: 'n1', category: 'usability', detail: '撤销', metric: '1 步' }],
    external_interfaces: [{ id: 'i1', name: 'PUT', kind: 'software', direction: 'in', description: '保存' }],
    data_entities: [{ id: 'd1', name: 'Artifact', description: '产物', attributes: ['revision'] }],
    business_rules: ['归属不变'],
    edge_cases: ['冲突'],
    assumptions: ['已登录'],
    dependencies: ['版本表'],
    constraints: ['不改门禁'],
    limitations: ['不支持图片'],
    risks: [{ id: 'r1', description: '误删', mitigation: '撤销' }],
    glossary: [{ term: '产物', definition: '节点输出' }],
    open_questions: ['无'],
  },
  'research.json': {
    title: '调研',
    summary: '结论',
    questions: [{ id: 'q1', question: '能否复用', answer: '能' }],
    findings: [{ id: 'r1', title: '版本机制', detail: 'Save 已归档' }],
    recommendation: '复用',
    references: ['store.go'],
    follow_ups: ['补测试'],
  },
  'root_cause.json': {
    title: '根因',
    summary: '契约未满足',
    symptom: '报错',
    expected: '通过',
    actual: '失败',
    reproduction: ['步骤 1'],
    impact: '阻塞',
    root_cause: '未标记访问写入',
    evidence: [{ title: '日志', detail: 'trace' }],
    diagrams: [{ kind: 'flowchart', title: '链路', format: 'mermaid', source: 'flowchart TD\n a-->b', caption: '图' }],
    ruled_out: ['网络'],
    contributing_factors: ['缓存'],
    affected_scope: ['复审'],
    causal_chain: ['a', 'b'],
  },
  'plan.json': {
    title: '计划',
    architecture: { summary: '分层', diagrams: [{ kind: 'flowchart', title: '总览', format: 'mermaid', source: 'flowchart TD\n a-->b' }] },
    data_design: {
      summary: '不涉及',
      entities: [
        {
          name: 'Artifact',
          fields: [
            { name: 'id', type: 'uuid', pk: true },
            { name: 'note', type: 'text', nullable: false, description: '备注' },
          ],
          relationships: ['has many versions'],
        },
      ],
    },
    interfaces: [{ name: 'PUT', kind: 'software', direction: 'in', summary: '保存' }],
    components: [{ name: 'engine', responsibility: '保存', dependencies: ['store'] }],
    interaction: { summary: '时序' },
    test_design: '单测',
    goals: [
      {
        id: 'g1',
        title: '服务端',
        status: 'done',
        subgoals: [
          { id: 'g1.1', title: '判定', detail: '状态', status: 'done' },
          { id: 'g1.2', title: '保存', status: 'pending' },
        ],
      },
      { id: 'g2', title: '实机验收', status: 'pending', subgoals: [{ id: 'g2.1', title: 'vm', status: 'pending' }] },
    ],
  },
  'implementation_result.json': {
    summary: '完成',
    change_type: 'feature',
    changed_areas: [{ title: 'server', detail: 'engine' }],
    tests: ['go test'],
    breaking_changes: ['无'],
    follow_ups: ['e2e'],
  },
  'test_result.json': {
    summary: '通过',
    cases: [
      { id: 't1', name: 'save', status: 'passed', detail: 'ok' },
      { id: 't2', name: 'vm', status: 'skipped', detail: '沙箱无实机' },
    ],
    defects: [{ id: 'd1', title: '无', severity: 'low', detail: '-', status: 'open' }],
    variances: '无',
    assessment: '可交付',
    screenshots: [{ artifact: 'shot.png', mimeType: 'image/png', caption: '截图' }],
    plan_coverage: [{ plan_id: 'g1.1', title: '判定', passed: true, evidence: '单测', cases: ['save'] }],
    passed: 1,
    failed: 0,
    skipped: 1,
  },
  'review.json': {
    summary: '可以合并',
    verdict: 'approve_with_comments',
    findings: [
      { id: 'v1', severity: 'low', file: 'a.go', line: 12, title: '命名', detail: '建议改名', suggestion: 'rename' },
    ],
    action_items: ['改名'],
  },
  'merge_request.json': {
    summary: '已创建',
    items: [
      {
        repo: 'grasp',
        sourceBranch: 'feat/x',
        targetBranch: 'main',
        url: 'https://example.com/mr/1',
        provider: 'gitlab',
        state: 'created',
        note: '首个',
      },
    ],
  },
  'preflight.json': {
    summary: '已确认',
    confirmed: true,
    fields: [
      { name: 'repo', label: '仓库', value: 'grasp', verified: true, verification: 'sandbox_probe', source: 'form', notes: '探测' },
      { name: 'branch', label: '分支', value: 'main', source: 'chat' },
    ],
    unresolved: ['无'],
  },
}

describe('artifactForms', () => {
  it('covers every structured artifact the preview renders', () => {
    for (const name of Object.keys(ARTIFACT_FORMS)) expect(isStructuredArtifactName(name), name).toBe(true)
    expect(Object.keys(ARTIFACT_FORMS).sort()).toEqual(Object.keys(FIXTURES).sort())
    expect(artifactFormFor('notes.md')).toBeNull()
    expect(artifactFormFor(null)).toBeNull()
  })

  for (const [name, doc] of Object.entries(FIXTURES)) {
    it(`round-trips ${name} without losing fields`, () => {
      const desc = artifactFormFor(name)!
      const form = toForm(desc, doc)
      expect(fromForm(desc, form)).toEqual(doc)
      expect(JSON.parse(serializeForm(desc, parseFormContent(desc, JSON.stringify(doc))!))).toEqual(doc)
    })
  }

  it('keeps unknown keys and server-computed counts', () => {
    const desc = artifactFormFor('test_result.json')!
    const form = toForm(desc, { summary: 's', passed: 3, extra: { keep: true } })
    form.summary = 'changed'
    expect(fromForm(desc, form)).toEqual({ summary: 'changed', passed: 3, extra: { keep: true } })
  })

  it('drops empty optional values and fills missing ones for the form', () => {
    const desc = artifactFormFor('plan.json')!
    const form = toForm(desc, { title: 'p', goals: [{ title: 'a' }] })
    expect(form.interfaces).toEqual([])
    expect(form.architecture).toBeUndefined()
    const goal = (form.goals as Record<string, unknown>[])[0]!
    expect(goal.detail).toBe('')
    expect(goal.subgoals).toEqual([])
    ;(form.goals as unknown[]).push(emptyListItem(desc.fields.find((f) => f.key === 'goals')!))
    const out = fromForm(desc, form)
    expect(out).toEqual({ title: 'p', goals: [{ title: 'a' }, { title: '' }] })
  })

  it('removing a plan goal removes its subgoals and keeps the rest', () => {
    const desc = artifactFormFor('plan.json')!
    const form = toForm(desc, FIXTURES['plan.json'])
    form.goals = (form.goals as Record<string, unknown>[]).filter((g) => g.id !== 'g2')
    const out = fromForm(desc, form) as { goals: { id: string; subgoals: unknown[] }[] }
    expect(out.goals.map((g) => g.id)).toEqual(['g1'])
    expect(out.goals[0]!.subgoals).toHaveLength(2)
  })

  it('keeps tri-state booleans absent unless set and coerces numbers', () => {
    const plan = artifactFormFor('plan.json')!
    const form = toForm(plan, FIXTURES['plan.json'])
    const fields = ((form.data_design as Record<string, unknown>).entities as Record<string, unknown>[])[0]!
      .fields as Record<string, unknown>[]
    expect('nullable' in fields[0]!).toBe(false)
    expect(fields[1]!.nullable).toBe(false)

    const review = artifactFormFor('review.json')!
    const rf = toForm(review, { summary: 's', verdict: 'approve', findings: [{ title: 'x', severity: 'low', line: '7' }] })
    expect(fromForm(review, rf)).toMatchObject({ findings: [{ line: 7 }] })
    ;(rf.findings as Record<string, unknown>[])[0]!.line = ''
    expect((fromForm(review, rf).findings as Record<string, unknown>[])[0]).not.toHaveProperty('line')
  })

  it('drops blank string-list entries and unchecked optional flags', () => {
    const desc = artifactFormFor('preflight.json')!
    const form = toForm(desc, { confirmed: false, unresolved: ['a', '  ', ''], fields: [{ name: 'x', verified: false }] })
    expect(fromForm(desc, form)).toEqual({ confirmed: false, unresolved: ['a'], fields: [{ name: 'x' }] })
  })

  it('returns null for content that is not a JSON object', () => {
    const desc = artifactFormFor('plan.json')!
    expect(parseFormContent(desc, '{bad')).toBeNull()
    expect(parseFormContent(desc, '[1]')).toBeNull()
    expect(parseFormContent(desc, '')).toEqual(toForm(desc, {}))
  })

  it('maps server field errors to the closest rendered path', () => {
    const paths = ['goals', 'goals[0]', 'goals[0].title', 'functional_requirements', 'data_design.entities[0].fields[1].name']
    expect(matchFieldPath('goals', paths)).toBe('goals')
    expect(matchFieldPath('goals[0].title', paths)).toBe('goals[0].title')
    expect(matchFieldPath('goals[0].subgoals[2].title', paths)).toBe('goals[0]')
    expect(matchFieldPath('functional_requirements["编辑"]', paths)).toBe('functional_requirements')
    expect(matchFieldPath('data_design.entities[0].fields[1].name', paths)).toBe('data_design.entities[0].fields[1].name')
    expect(matchFieldPath('unknown', paths)).toBeNull()
  })
})
