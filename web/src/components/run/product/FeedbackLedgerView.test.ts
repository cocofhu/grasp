// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import ChatImageThumb from '../../ui/ChatImageThumb.vue'
import FeedbackLedgerView from './FeedbackLedgerView.vue'

const artifactContent = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api/api', () => ({
  api: { artifactContent },
  blobContentUrl: (ref: string) => `/api/blobs/${String(ref).replace(/^blob:/, '')}`,
}))

function mountLedger(name: string, doc: unknown, artifacts: Artifact[] = []) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(FeedbackLedgerView, {
    props: { name, doc, runId: 'run-1', artifacts },
    global: { plugins: [i18n], stubs: { Icon: true, ChatImageThumb: true } },
  })
}

const indexDoc = {
  runId: 'run-1',
  totalRounds: 2,
  counts: { review: 1, clarify: 1 },
  rounds: [
    {
      seq: 1, kind: 'clarify', node: 'clarify-1', iteration: 1, round: 1,
      at: '2026-08-13T15:00:00+08:00', actor: 'system', action: 'auto_answer',
      summary: '自动采纳推荐项:缓存 = Redis',
    },
    {
      seq: 2, kind: 'review', node: 'research-1', iteration: 2, round: 3,
      at: '2026-08-13T15:07:22+08:00', actor: 'alice', action: 'revise',
      annotations: 2, attachments: 1, summary: '第 3 条结论证据不足',
      artifact: 'feedback.review.research-1.i2r3.json',
    },
  ],
}

const roundDoc = {
  runId: 'run-1',
  kind: 'review',
  node: { id: 'research-1', label: '调研', type: 'agent' },
  iteration: 2,
  round: 3,
  at: '2026-08-13T15:07:22+08:00',
  actor: { name: 'alice', callerKind: 'pm' },
  feedback: {
    text: '第 3 条结论证据不足,请补上原始链接。',
    annotations: [{ jsonPath: '$.findings[2].evidence', note: '补链接' }],
    attachments: [{ ref: 'blob:9f2c', name: 'screenshot.png', mimeType: 'image/png' }],
  },
  transcript: [
    { role: 'human', at: '2026-08-13T15:07:22+08:00', text: '补上原始链接' },
    { role: 'agent', at: '2026-08-13T15:08:02+08:00', text: '已补充 3 处引用' },
  ],
  targets: [
    { name: 'research.json', before: 'ab3f1c9d', after: 'cd91e4b7', changed: true },
    { name: 'plan.json', before: 'ee11', after: 'ee11', changed: false },
  ],
  priorRounds: [{ round: 1, kind: 'review', summary: '要求补充竞品对比' }],
  index: 'feedback_index.json',
}

describe('FeedbackLedgerView', () => {
  beforeEach(() => {
    artifactContent.mockReset()
  })

  it('renders the index as a timeline of every round', () => {
    const w = mountLedger('feedback_index.json', indexDoc)
    expect(w.text()).toContain('第 3 条结论证据不足')
    expect(w.text()).toContain('自动采纳推荐项')
    expect(w.findAll('li')).toHaveLength(2)
    // Bodies stay collapsed until asked for.
    expect(w.text()).not.toContain('已补充 3 处引用')
    w.unmount()
  })

  // Depth on demand is the whole point of one file per round: expanding is what
  // fetches the body, so a long ledger costs nothing until it is read.
  it('fetches a round product only when its row is expanded', async () => {
    artifactContent.mockResolvedValue({ content: JSON.stringify(roundDoc) })
    const artifacts = [
      { id: 'art-9', name: 'feedback.review.research-1.i2r3.json' } as Artifact,
    ]
    const w = mountLedger('feedback_index.json', indexDoc, artifacts)
    expect(artifactContent).not.toHaveBeenCalled()

    await w.get('[data-testid="feedback-round-2"]').trigger('click')
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()

    expect(artifactContent).toHaveBeenCalledWith('art-9')
    expect(w.text()).toContain('已补充 3 处引用')
    expect(w.text()).toContain('research.json')
    // Unchanged products are not listed as touched by this round.
    expect(w.text()).not.toContain('plan.json')

    // Collapsing and re-expanding reuses the loaded body.
    await w.get('[data-testid="feedback-round-2"]').trigger('click')
    await w.get('[data-testid="feedback-round-2"]').trigger('click')
    expect(artifactContent).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('says so when a round has no standalone product', async () => {
    const w = mountLedger('feedback_index.json', indexDoc)
    await w.get('[data-testid="feedback-round-1"]').trigger('click')
    expect(w.text()).toContain(pages.pages.product.feedback.indexOnly)
    expect(artifactContent).not.toHaveBeenCalled()
    w.unmount()
  })

  it('reports a round that cannot be fetched instead of rendering blank', async () => {
    artifactContent.mockRejectedValueOnce(new Error('boom'))
    const artifacts = [
      { id: 'art-9', name: 'feedback.review.research-1.i2r3.json' } as Artifact,
    ]
    const w = mountLedger('feedback_index.json', indexDoc, artifacts)
    await w.get('[data-testid="feedback-round-2"]').trigger('click')
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()
    expect(w.text()).toContain('feedback.review.research-1.i2r3.json')
    w.unmount()
  })

  it('renders a single round product on its own', () => {
    const w = mountLedger('feedback.review.research-1.i2r3.json', roundDoc)
    expect(w.text()).toContain('research-1')
    expect(w.text()).toContain('第 3 条结论证据不足')
    expect(w.text()).toContain('已补充 3 处引用')
    expect(w.text()).toContain('要求补充竞品对比')
    // Attachments are references, never inlined bytes.
    expect(w.findComponent(ChatImageThumb).props('src')).toBe('/api/blobs/9f2c')
    // Legacy / no-summary rounds must not render the Agent summary section.
    expect(w.find('[data-testid="feedback-agent-summary"]').exists()).toBe(false)
    expect(w.text()).not.toContain(pages.pages.product.feedback.agentSummary)
    w.unmount()
  })

  it('places Agent summary before full text and transcript when present', () => {
    const withSummary = {
      ...roundDoc,
      agentSummary: '用户希望在聊天记录前增加 Agent 对反馈的总结。',
    }
    const w = mountLedger('feedback.review.research-1.i2r3.json', withSummary)
    const summary = w.get('[data-testid="feedback-agent-summary"]')
    expect(summary.text()).toContain(pages.pages.product.feedback.agentSummary)
    expect(summary.text()).toContain(pages.pages.product.feedback.agentSummaryTag)
    expect(summary.text()).toContain('用户希望在聊天记录前增加 Agent 对反馈的总结。')
    // Must not use index gist as the card summary body.
    expect(summary.text()).not.toContain('第 3 条结论证据不足')
    const root = summary.element.parentElement
    expect(root).toBeTruthy()
    const kids = Array.from(root!.children)
    const iSummary = kids.indexOf(summary.element)
    const iFull = kids.findIndex((el) => el.textContent?.includes(pages.pages.product.feedback.fullText))
    const iTranscript = kids.findIndex((el) => el.textContent?.includes(pages.pages.product.feedback.transcript))
    expect(iSummary).toBeGreaterThanOrEqual(0)
    expect(iFull).toBeGreaterThan(iSummary)
    expect(iTranscript).toBeGreaterThan(iFull)
    w.unmount()
  })

  // The ReAct ledger product is cumulative: `summary` lists every round's gist
  // while `agentSummary` is the single induction produced at「确认并流转」.
  it('renders the confirm-time summary on a cumulative ReAct product', () => {
    const cumulative = {
      runId: 'run-1',
      kind: 'clarify',
      node: { id: 'approve-1', label: '需求对齐', type: 'agent' },
      iteration: 1,
      roundCount: 3,
      latestRound: 3,
      at: '2026-08-13T15:20:00+08:00',
      summary: '截至第3轮的归纳结论：第1轮：视觉跟截图；第2轮：去掉下拉；第3轮：就这样',
      agentSummary: '用户确认按截图保留视觉，去掉下拉与紫色选中，卡片只读。',
      index: 'feedback_index.json',
    }
    const w = mountLedger('feedback.clarify.approve-1.i1.json', cumulative)
    const summary = w.get('[data-testid="feedback-agent-summary"]')
    expect(summary.text()).toContain('用户确认按截图保留视觉，去掉下拉与紫色选中，卡片只读。')
    expect(w.get('[data-testid="feedback-round-body"]').text()).toContain('第2轮：去掉下拉')
    w.unmount()
  })

  it('shows an empty ledger without rows', () => {
    const w = mountLedger('feedback_index.json', { runId: 'run-1', totalRounds: 0, rounds: [] })
    expect(w.text()).toContain(pages.pages.product.feedback.empty)
    expect(w.findAll('li')).toHaveLength(0)
    w.unmount()
  })

  // Structured UI must render Markdown like ClarifyChat (not literal **…** / - lists).
  it('renders feedback.text and transcript Markdown as rich HTML (g3.1)', () => {
    const mdRound = {
      ...roundDoc,
      feedback: {
        text: '主意见含 **粗体** 与列表：\n\n- 第一项\n- 第二项',
        annotations: [],
        attachments: [],
      },
      transcript: [
        {
          role: 'human',
          at: '2026-08-13T15:07:22+08:00',
          text: '感觉不太行，**左右对照**读不动',
        },
        {
          role: 'agent',
          at: '2026-08-13T15:08:02+08:00',
          text: '已按反馈重做：\n\n- **左右对照**：修复前 vs 修复后\n- **虚线对齐参考**：复制按钮左缘',
        },
      ],
    }
    const w = mountLedger('feedback.review.research-1.i2r3.json', mdRound)
    const body = w.get('[data-testid="feedback-round-body"]')
    expect(body.classes()).toContain('md')
    expect(body.html()).toContain('<strong>')
    expect(body.html()).toMatch(/<ul[\s>]/)
    expect(body.text()).toContain('粗体')
    expect(body.html()).not.toContain('**粗体**')

    const turns = w.findAll('[data-testid="feedback-turn-body"]')
    expect(turns).toHaveLength(2)
    expect(turns[0].html()).toContain('<strong>')
    expect(turns[0].html()).not.toContain('**左右对照**')
    expect(turns[1].html()).toMatch(/<ul[\s>]/)
    expect(turns[1].html()).toContain('<strong>')
    expect(turns[1].html()).not.toContain('**左右对照**')
    // priorRounds.summary stays plain text (out of scope)
    expect(w.text()).toContain('要求补充竞品对比')
    w.unmount()
  })

  it('omits empty or whitespace-only Markdown bodies (g3.1)', () => {
    const emptyRound = {
      ...roundDoc,
      feedback: { text: '   \n\t  ', annotations: [], attachments: [] },
      transcript: [
        { role: 'human', at: '2026-08-13T15:07:22+08:00', text: '' },
        { role: 'agent', at: '2026-08-13T15:08:02+08:00', text: '   ' },
        { role: 'agent', at: '2026-08-13T15:09:00+08:00', text: '正常回复' },
      ],
    }
    const w = mountLedger('feedback.review.research-1.i2r3.json', emptyRound)
    expect(w.find('[data-testid="feedback-round-body"]').exists()).toBe(false)
    const turns = w.findAll('[data-testid="feedback-turn-body"]')
    expect(turns).toHaveLength(1)
    expect(turns[0].text()).toContain('正常回复')
    // Three transcript bubbles remain (role headers); only non-blank bodies render.
    expect(w.findAll('[data-testid="feedback-turn-0"]')).toHaveLength(1)
    expect(w.findAll('[data-testid="feedback-turn-1"]')).toHaveLength(1)
    expect(w.findAll('[data-testid="feedback-turn-2"]')).toHaveLength(1)
    w.unmount()
  })
})
