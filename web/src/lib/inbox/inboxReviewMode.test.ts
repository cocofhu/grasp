import { describe, expect, it } from 'vitest'
import {
  inboxComposerMode,
  pickInboxClarifySession,
  resolveInboxReviewState,
} from './inboxReviewMode'
import type { Run } from '@/lib/shared/types'
import { CLARIFY_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

function runFixture(partial: Partial<Run> & { nodes?: Run['nodes'] }): Run {
  return {
    id: 'r1',
    workflowId: 'w1',
    workflowName: 'wf',
    status: 'waiting_human',
    trigger: 'manual',
    startedAt: '',
    durationSec: 0,
    progress: 0,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

describe('pickInboxClarifySession', () => {
  it('prefers clarifyByNode over top-level clarify', () => {
    const run = runFixture({
      clarifyByNode: {
        research: { nodeId: 'research', turns: [], done: false },
      },
      clarify: { nodeId: 'other', turns: [], done: true },
    })
    expect(pickInboxClarifySession(run, 'research')?.nodeId).toBe('research')
  })

  it('falls back to top-level clarify when node matches', () => {
    const run = runFixture({
      clarify: { nodeId: 'research', turns: [], done: false, previewArtifact: 'note.md' },
    })
    expect(pickInboxClarifySession(run, 'research')?.done).toBe(false)
    expect(pickInboxClarifySession(run, 'research')?.previewArtifact).toBe('note.md')
  })

  it('returns null when node does not match', () => {
    const run = runFixture({
      clarify: { nodeId: 'research', turns: [], done: false },
    })
    expect(pickInboxClarifySession(run, 'other')).toBeNull()
  })
})

describe('resolveInboxReviewState', () => {
  const openConv = { nodeId: 'research', turns: [], done: false }
  const doneConv = { nodeId: 'research', turns: [], done: true }

  it('review agent + open session → reviewActive', () => {
    const run = runFixture({
      nodes: [{ id: 'research', type: 'agent', label: 'Research', position: { x: 0, y: 0 }, config: {}, caps: IMPLEMENT_CAPS }],
    })
    expect(
      resolveInboxReviewState({ type: 'clarify', nodeId: 'research' }, run, openConv),
    ).toEqual({ reviewActive: true, nodeMissing: false })
    expect(inboxComposerMode(true)).toBe('review')
  })

  it('clarify agent + open session → clarify (not review)', () => {
    const run = runFixture({
      nodes: [{ id: 'react1', type: 'agent', label: 'React', position: { x: 0, y: 0 }, config: {}, caps: CLARIFY_CAPS }],
    })
    expect(
      resolveInboxReviewState(
        { type: 'clarify', nodeId: 'react1' },
        run,
        { nodeId: 'react1', turns: [], done: false },
      ),
    ).toEqual({ reviewActive: false, nodeMissing: false })
    expect(inboxComposerMode(false)).toBe('clarify')
  })

  it('done session → not review', () => {
    const run = runFixture({
      nodes: [{ id: 'plan', type: 'agent', label: 'Plan', position: { x: 0, y: 0 }, config: {}, caps: IMPLEMENT_CAPS }],
    })
    expect(
      resolveInboxReviewState({ type: 'clarify', nodeId: 'plan' }, run, doneConv),
    ).toEqual({ reviewActive: false, nodeMissing: false })
  })

  it('missing graph node → nodeMissing, not review', () => {
    const run = runFixture({ nodes: [] })
    expect(
      resolveInboxReviewState({ type: 'clarify', nodeId: 'research' }, run, openConv),
    ).toEqual({ reviewActive: false, nodeMissing: true })
  })

  it('gate inbox item never activates review', () => {
    const run = runFixture({
      nodes: [{ id: 'research', type: 'agent', label: 'Research', position: { x: 0, y: 0 }, config: {}, caps: IMPLEMENT_CAPS }],
    })
    expect(
      resolveInboxReviewState({ type: 'gate', nodeId: 'research' } as any, run, openConv),
    ).toEqual({ reviewActive: false, nodeMissing: false })
  })
})
