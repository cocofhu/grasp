import { describe, expect, it } from 'vitest'
import {
  applyInboxReplyingState,
  inboxBadgeLabelKey,
  inboxBadgeTone,
  inboxRunLabel,
  inboxSecondaryLine,
  conversationToolbarRunName,
  sidebarRunTitle,
  isInboxProgressItem,
  isReplyingInboxItem,
  isStartingInboxItem,
  mergeInboxReplyingFromRemote,
} from './inboxDisplay'

describe('sidebarRunTitle', () => {
  it('uses the readable run title, then the workflow name, and never a Run # stand-in', () => {
    expect(sidebarRunTitle({ runTitle: '把登录做清楚', workflowName: 'wf' })).toBe('把登录做清楚')
    expect(sidebarRunTitle({ runTitle: '  ', workflowName: '需求交付' })).toBe('需求交付')
    expect(sidebarRunTitle({ runTitle: '', workflowName: '' })).toBe('')
    expect(sidebarRunTitle({})).toBe('')
  })

  it('shows repo names and hides other raw JSON', () => {
    expect(
      sidebarRunTitle({
        runTitle: '[{"name":"approving","url":"https://git.example/approving.git"}]',
      }),
    ).toBe('approving')
    expect(sidebarRunTitle({ runTitle: '{"foo":1}', workflowName: 'wf' })).toBe('wf')
  })
})

describe('conversationToolbarRunName', () => {
  it('matches sidebarRunTitle for a loaded run and falls through when the name is blank', () => {
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '把登录做清楚', workflowName: '产品迭代' },
    })).toBe(sidebarRunTitle({ runTitle: '把登录做清楚', workflowName: '产品迭代' }))
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '  ', workflowName: '产品迭代' },
    })).toBe('产品迭代')
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '', workflowName: '' },
      fallback: { runId: 'run-1', runTitle: '', workflowName: '' },
    })).toBe('')
  })

  it('uses the loaded GetRun title when there is no inbox pending row', () => {
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '这个行高有点高了', workflowName: '自我迭代' },
      fallback: { runId: 'run-1', runTitle: '这个行高有点高了', workflowName: '自我迭代' },
      pendingRows: [],
    })).toBe('这个行高有点高了')
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '', workflowName: '自我迭代' },
      fallback: { runId: 'run-1', runTitle: '', workflowName: '自我迭代' },
      pendingRows: [],
    })).toBe('自我迭代')
  })

  it('uses a renamed pending row, then the loaded run, and hides raw JSON', () => {
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '甲', workflowName: '产品迭代' },
      fallback: { runId: 'run-1', runTitle: '甲', workflowName: '产品迭代' },
      pendingRows: [{ runId: 'run-1', runTitle: '丙', workflowName: '产品迭代' }],
    })).toBe('丙')
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '优化这里的段间距', workflowName: '产品迭代' },
      fallback: { runId: 'run-1', runTitle: '旧快照', workflowName: '其它' },
    })).toBe(sidebarRunTitle({ runTitle: '优化这里的段间距', workflowName: '产品迭代' }))
    expect(conversationToolbarRunName({
      run: { id: 'run-1', title: '{"foo":1}', workflowName: '产品迭代' },
    })).toBe('产品迭代')
    expect(conversationToolbarRunName({
      fallback: {
        runId: 'run-1',
        runTitle: '[{"name":"approving","url":"https://git.example/approving.git"}]',
        workflowName: '产品迭代',
      },
    })).toBe('approving')
  })
})

describe('inboxRunLabel', () => {
  it('shows runTitle when non-empty', () => {
    expect(inboxRunLabel({ runId: 'run-abc', runTitle: '需求摘要' })).toBe('需求摘要')
  })

  it('falls back to Run #id when title missing or blank', () => {
    expect(inboxRunLabel({ runId: 'run-abc123' })).toBe('Run #abc123')
    expect(inboxRunLabel({ runId: 'run-abc123', runTitle: '  ' })).toBe('Run #abc123')
  })

  it('truncates long titles at 60 chars', () => {
    const long = '甲'.repeat(80)
    const got = inboxRunLabel({ runId: 'run-x', runTitle: long })
    expect(got.length).toBeLessThanOrEqual(61)
    expect(got.endsWith('…') || got.endsWith('...')).toBe(true)
  })
})

describe('inboxSecondaryLine', () => {
  it('joins workflow name with run label', () => {
    expect(
      inboxSecondaryLine({ workflowName: 'github-feature', runId: 'run-1', runTitle: '标题' }),
    ).toBe('github-feature · 标题')
    expect(inboxSecondaryLine({ workflowName: 'wf', runId: 'run-zz' })).toBe('wf · Run #zz')
  })
})

describe('inboxBadgeLabelKey', () => {
  it('maps gate to gateType', () => {
    expect(inboxBadgeLabelKey({ type: 'gate' })).toBe('pages.gatesInbox.gateType')
  })

  it('maps clarify kind to clarifyType', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'clarify' })).toBe('pages.gatesInbox.clarifyType')
  })

  it('maps review kind to reviewType', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'review' })).toBe('pages.gatesInbox.reviewType')
  })

  it('falls back to clarifyType when kind omitted', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify' })).toBe('pages.gatesInbox.clarifyType')
  })

  it('maps a booting sandbox to startingType regardless of kind', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'clarify', state: 'starting' })).toBe(
      'pages.gatesInbox.startingType',
    )
  })

  it('maps parked sessionBusy to replyingType (plan g1.2 / g2.2)', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'clarify', state: 'replying' })).toBe(
      'pages.gatesInbox.replyingType',
    )
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'review', state: 'replying' })).toBe(
      'pages.gatesInbox.replyingType',
    )
  })

  it('keeps starting above replying above type badges', () => {
    expect(
      inboxBadgeLabelKey({ type: 'clarify', kind: 'review', state: 'starting' }),
    ).toBe('pages.gatesInbox.startingType')
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'review', state: 'replying' })).toBe(
      'pages.gatesInbox.replyingType',
    )
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'review' })).toBe('pages.gatesInbox.reviewType')
    expect(inboxBadgeLabelKey({ type: 'gate', state: 'replying' })).toBe('pages.gatesInbox.gateType')
  })
})

describe('isStartingInboxItem', () => {
  it('is true only for clarify items whose sandbox is still booting', () => {
    expect(isStartingInboxItem({ type: 'clarify', state: 'starting' })).toBe(true)
    expect(isStartingInboxItem({ type: 'clarify' })).toBe(false)
    expect(isStartingInboxItem({ type: 'gate', state: 'starting' })).toBe(false)
    expect(isStartingInboxItem(null)).toBe(false)
  })
})

describe('isReplyingInboxItem / isInboxProgressItem', () => {
  it('is true for parked clarify state=replying and never for gates or starting', () => {
    expect(isReplyingInboxItem({ type: 'clarify', state: 'replying' })).toBe(true)
    expect(isReplyingInboxItem({ type: 'clarify', state: 'starting' })).toBe(false)
    expect(isReplyingInboxItem({ type: 'clarify' })).toBe(false)
    expect(isReplyingInboxItem({ type: 'gate', state: 'replying' })).toBe(false)
    expect(isInboxProgressItem({ type: 'clarify', state: 'replying' })).toBe(true)
    expect(isInboxProgressItem({ type: 'clarify', state: 'starting' })).toBe(true)
    expect(isInboxProgressItem({ type: 'clarify' })).toBe(false)
  })
})

describe('applyInboxReplyingState / mergeInboxReplyingFromRemote', () => {
  it('sets and clears replying without touching starting', () => {
    const idle = { type: 'clarify' as const, runId: 'r', nodeId: 'n', state: undefined }
    expect(applyInboxReplyingState(idle, true).state).toBe('replying')
    expect(applyInboxReplyingState({ ...idle, state: 'replying' }, false).state).toBeUndefined()
    expect(applyInboxReplyingState({ ...idle, state: 'starting' }, true).state).toBe('starting')
  })

  it('merges remote replying onto matching keys only (plan g2.1)', () => {
    const displayed = [
      { type: 'clarify' as const, runId: 'run-a', nodeId: 'n-a' },
      { type: 'clarify' as const, runId: 'run-b', nodeId: 'n-b', state: 'replying' as const },
      { type: 'clarify' as const, runId: 'run-c', nodeId: 'n-c', state: 'starting' as const },
    ]
    const remote = [
      { type: 'clarify' as const, runId: 'run-a', nodeId: 'n-a', state: 'replying' as const },
      { type: 'clarify' as const, runId: 'run-b', nodeId: 'n-b' },
      { type: 'clarify' as const, runId: 'run-c', nodeId: 'n-c', state: 'starting' as const },
    ]
    const merged = mergeInboxReplyingFromRemote(displayed, remote)
    expect(merged[0].state).toBe('replying')
    expect(merged[1].state).toBeUndefined()
    expect(merged[2].state).toBe('starting')
    expect(merged).toHaveLength(3)
  })
})

describe('inboxBadgeTone', () => {
  it('splits gate / review / clarify for Demo badge colors', () => {
    expect(inboxBadgeTone({ type: 'gate' })).toBe('gate')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'review' })).toBe('review')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify' })).toBe('clarify')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify', state: 'replying' })).toBe('replying')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify', state: 'starting' })).toBe('clarify')
  })
})
