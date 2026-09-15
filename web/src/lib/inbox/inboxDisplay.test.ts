import { describe, expect, it } from 'vitest'
import {
  applyInboxReplyingState,
  inboxBadgeLabelKey,
  inboxBadgeTone,
  inboxRunLabel,
  inboxSecondaryLine,
  isInboxProgressItem,
  isReplyingInboxItem,
  isStartingInboxItem,
  mergeInboxReplyingFromRemote,
} from './inboxDisplay'

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
  it('maps gate to gateType (proposal_select / human gate)', () => {
    expect(inboxBadgeLabelKey({ type: 'gate' })).toBe('pages.gatesInbox.gateType')
  })

  it('maps clarify kind to clarifyType (react)', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'clarify' })).toBe('pages.gatesInbox.clarifyType')
  })

  it('maps review kind to reviewType (research / proposal)', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'review' })).toBe('pages.gatesInbox.reviewType')
  })

  it('maps app_preview kind to previewType (application preview)', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'app_preview' })).toBe(
      'pages.gatesInbox.previewType',
    )
  })

  it('maps preflight kind to preflightType (env confirmation)', () => {
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'preflight' })).toBe(
      'pages.gatesInbox.preflightType',
    )
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
    expect(inboxBadgeLabelKey({ type: 'clarify', kind: 'app_preview', state: 'replying' })).toBe(
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
  it('splits gate / preview / review / clarify for Demo badge colors', () => {
    expect(inboxBadgeTone({ type: 'gate' })).toBe('gate')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'app_preview' })).toBe('preview')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'review' })).toBe('review')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'preflight' })).toBe('preflight')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify' })).toBe('clarify')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify', state: 'replying' })).toBe('replying')
    expect(inboxBadgeTone({ type: 'clarify', kind: 'clarify', state: 'starting' })).toBe('clarify')
  })
})
