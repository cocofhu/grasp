// @vitest-environment happy-dom
import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import type { Artifact, Run } from '@/lib/shared/types'
import {
  REACT_STAGE_TAB_GRID,
  REACT_STAGE_TAB_NOVNC,
  REACT_STAGE_TAB_PREVIEW,
  applyPreviewArtifactFromRun,
  applyPreviewArtifactName,
  artifactFingerprint,
  artifactFriendlyNameKey,
  artifactTechnicalDisplayName,
  buildStageCardThumb,
  canAnnotateStageArtifact,
  expandStageArtifacts,
  extractVisualHtmlSummary,
  buildArtifactVersionChoices,
  filterStageGridArtifacts,
  historicalStageArtifactId,
  inboxStageRemoteKind,
  isBookkeepingArtifact,
  isSamePreviewVisualCopy,
  isVisualPreviewArtifactName,
  loadStageOpenState,
  parseStructuredArtifactSummary,
  restoreStageOpenState,
  resetStageOpenStateForTests,
  saveStageOpenState,
  visualNodePageName,
  isHistoricalStageArtifact,
  shouldActivatePinnedPreview,
  artifactKindLabelKey,
  artifactRevision,
  closeStagePreviewTab,
  findArtifactByName,
  isOwnNodeArtifact,
  isAppPreviewRemoteNode,
  isClarifyInteractiveGraphNode,
  latestOwnNodeHtmlName,
  nextTabAfterClose,
  openStagePreviewTab,
  previewTabId,
  approveStageRemoteKind,
  resolveEffectivePreviewPin,
  resolveStageRemoteKind,
  stageGridArtifactsWithPin,
  stageGridArtifactsForNode,
  stageOpenStateStorageKey,
  wantsTextSummaryThumb,
  isAutoPinStageNode,
  isVisibleAutoPinArtifact,
  filterVisibleStageArtifacts,
  buildArtifactFingerprintMap,
  diffArtifactFingerprints,
  isIdleStageTab,
  shouldFocusPinnedOrAutoTab,
  markStageTabUnread,
  clearStageTabUnread,
} from './reactArtifactPreview'

function art(partial: Partial<Artifact> & Pick<Artifact, 'id' | 'name'>): Artifact {
  return {
    kind: 'html',
    nodeId: 'react',
    runId: 'r1',
    workflowName: 'wf',
    sizeBytes: 10,
    createdAt: '2026-08-01T00:00:00Z',
    ...partial,
  }
}

describe('reactArtifactPreview helpers', () => {
  it('treats missing revision as v1 and increments from stored values', () => {
    expect(artifactRevision(undefined)).toBe(1)
    expect(artifactRevision({ revision: 0 })).toBe(1)
    expect(artifactRevision({ revision: 3 })).toBe(3)
  })

  it('finds artifacts by name and fingerprints identity+version', () => {
    const a = art({ id: 'a1', name: 'page.html', revision: 2, updatedAt: 't2', sizeBytes: 8 })
    expect(findArtifactByName([a], 'page.html')?.id).toBe('a1')
    expect(findArtifactByName([a], 'missing')).toBeNull()
    expect(artifactFingerprint(a)).toBe('a1:t2:2:8:')
  })

  it('maps kind to i18n keys', () => {
    expect(artifactKindLabelKey('html')).toContain('kindHtml')
    expect(artifactKindLabelKey('unknown')).toContain('kindFile')
  })

  it('detects clarify-interactive graph nodes', () => {
    const run = {
      nodes: [{ id: 'c1', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} }],
    } as Run
    expect(isClarifyInteractiveGraphNode(run, 'c1')).toBe(true)
    expect(isClarifyInteractiveGraphNode(run, 'other')).toBe(false)
    const approveRun = {
      nodes: [{ id: 'a1', type: 'approve', label: 'Approve', position: { x: 0, y: 0 }, config: {} }],
    } as Run
    expect(isClarifyInteractiveGraphNode(approveRun, 'a1')).toBe(true)
    expect(isAppPreviewRemoteNode('app_preview')).toBe(true)
    expect(isAppPreviewRemoteNode('approve')).toBe(false)
    expect(isAppPreviewRemoteNode('react')).toBe(false)
    expect(approveStageRemoteKind(false)).toBe('off')
    expect(approveStageRemoteKind(true)).toBe('app')
  })

  it('treats foreign-node artifacts as read-only unless nodeId is empty', () => {
    expect(isOwnNodeArtifact({ nodeId: 'research' }, '')).toBe(true)
    expect(isOwnNodeArtifact({ nodeId: 'research' }, 'research')).toBe(true)
    expect(isOwnNodeArtifact({ nodeId: 'plan' }, 'research')).toBe(false)
    expect(isOwnNodeArtifact({ nodeId: '' }, 'research')).toBe(true)
    expect(canAnnotateStageArtifact(true, { nodeId: 'plan' }, 'research')).toBe(false)
    expect(canAnnotateStageArtifact(true, { nodeId: 'research' }, 'research')).toBe(true)
    expect(canAnnotateStageArtifact(false, { nodeId: 'research' }, 'research')).toBe(false)
    expect(canAnnotateStageArtifact(true, { id: historicalStageArtifactId('visual', 1), nodeId: 'visual' }, 'visual')).toBe(false)
    expect(isHistoricalStageArtifact({ id: historicalStageArtifactId('visual', 1) })).toBe(true)
  })

  it('does not flatten historical visual snapshots into extra grid cards', () => {
    const live = art({ id: 'live', name: 'page.html', nodeId: 'visual_1', content: '<p>new</p>' })
    const run = {
      id: 'r1',
      nodes: [{ id: 'visual_1', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} }],
      nodeExecutions: {
        visual_1: [
          { nodeId: 'visual_1', iteration: 1, status: 'completed', outputs: { page: '<p>old</p>' } },
          { nodeId: 'visual_1', iteration: 2, status: 'waiting_human', outputs: { page: '<p>new</p>' } },
        ],
      },
    } as unknown as Run
    const node = run.nodes![0]
    expect(expandStageArtifacts([live], run, node).map((a) => a.name)).toEqual(['page.html'])
    expect(expandStageArtifacts([live], run, { ...node, type: 'research' }).map((a) => a.name)).toEqual(['page.html'])
  })

  it('hides the same-preview visual node page copy when page.html is present', () => {
    const page = art({ id: 'p', name: 'page.html', nodeId: 'visual_bqc5', content: '<p>same</p>' })
    const alias = art({ id: 'a', name: visualNodePageName('visual_bqc5'), nodeId: 'visual_bqc5', content: '<p>same</p>' })
    const other = art({ id: 'o', name: visualNodePageName('visual_other'), nodeId: 'visual_other', content: '<p>diff</p>' })
    const json = art({ id: 'j', name: 'node_complete.json', kind: 'json', nodeId: 'visual_bqc5' })
    const collapsed = expandStageArtifacts([page, alias, other, json])
    expect(collapsed.map((a) => a.name)).toEqual(['page.html', visualNodePageName('visual_other'), 'node_complete.json'])
    expect(isSamePreviewVisualCopy(alias, [page, alias])).toBe(true)
    expect(isSamePreviewVisualCopy(other, [page, alias, other])).toBe(false)
  })

  it('keeps a visual_*.page.html card when page.html is absent', () => {
    const alias = art({ id: 'a', name: visualNodePageName('visual_1'), nodeId: 'visual_1' })
    expect(expandStageArtifacts([alias]).map((a) => a.name)).toEqual([visualNodePageName('visual_1')])
  })

  it('builds version choices from archived snapshots plus the live revision', () => {
    const archived = [
      { artifactId: 'live', revision: 1, nodeId: 'visual_1', sizeBytes: 8, createdAt: 't1' },
      { artifactId: 'live', revision: 2, nodeId: 'visual_1', sizeBytes: 9, createdAt: 't2' },
    ]
    const choices = buildArtifactVersionChoices(3, archived)
    expect(choices).toEqual([
      { index: 1, revision: 1, latest: false, available: true },
      { index: 2, revision: 2, latest: false, available: true },
      { index: 3, revision: 3, latest: true, available: true },
    ])
    expect(buildArtifactVersionChoices(1, [])).toEqual([
      { index: 1, revision: 1, latest: true, available: true },
    ])
    expect(buildArtifactVersionChoices(2, [{ artifactId: 'x', revision: 2, nodeId: 'n', sizeBytes: 1, createdAt: 't' }])).toEqual([
      { index: 2, revision: 2, latest: true, available: true },
    ])
  })

  it('uses sandbox remote only for ReAct inbox nodes', () => {
    const run = {
      nodes: [
        { id: 'c1', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} },
        { id: 'visual', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
      ],
    } as unknown as Run
    expect(inboxStageRemoteKind({ appPreview: true, run, nodeId: 'preview' })).toBe('app')
    expect(inboxStageRemoteKind({ appPreview: false, run, nodeId: 'c1' })).toBe('sandbox')
    expect(inboxStageRemoteKind({ appPreview: false, run, nodeId: 'visual' })).toBe('off')
    const approveRun = {
      nodes: [{ id: 'a1', type: 'approve', label: 'Approve', position: { x: 0, y: 0 }, config: {} }],
    } as unknown as Run
    // Approve without registered preview stays off (not app/sandbox).
    expect(inboxStageRemoteKind({ appPreview: false, run: approveRun, nodeId: 'a1' })).toBe('off')
    expect(
      inboxStageRemoteKind({ appPreview: false, run: approveRun, nodeId: 'a1', hasRegisteredPreview: true }),
    ).toBe('app')
    const appPreviewRun = {
      nodes: [{ id: 'p1', type: 'app_preview', label: '预览', position: { x: 0, y: 0 }, config: {} }],
    } as unknown as Run
    expect(inboxStageRemoteKind({ appPreview: false, run: appPreviewRun, nodeId: 'p1' })).toBe('app')
  })

  it('resolves remoteKind with explicit override over sandbox default', () => {
    expect(resolveStageRemoteKind({ runId: 'r', nodeId: 'n' })).toBe('sandbox')
    expect(resolveStageRemoteKind({ runId: 'r', nodeId: 'n', inlineContent: true })).toBe('off')
    expect(resolveStageRemoteKind({ runId: 'r', nodeId: 'n', remoteKind: 'app' })).toBe('app')
    expect(resolveStageRemoteKind({ inlineContent: true, remoteKind: 'public' })).toBe('public')
    expect(resolveStageRemoteKind({})).toBe('off')
  })

  it('opens preview tabs without replacing already-open ones', () => {
    expect(openStagePreviewTab([], 'a.html')).toEqual(['a.html'])
    expect(openStagePreviewTab(['a.html'], 'b.md')).toEqual(['a.html', 'b.md'])
    expect(openStagePreviewTab(['a.html', 'b.md'], 'a.html')).toEqual(['a.html', 'b.md'])
    expect(closeStagePreviewTab(['a.html', 'b.md'], 'a.html')).toEqual(['b.md'])
    expect(nextTabAfterClose(['a.html', 'b.md'], 'b.md', previewTabId('b.md'))).toBe(previewTabId('a.html'))
    expect(nextTabAfterClose(['a.html'], 'a.html', previewTabId('a.html'))).toBe(REACT_STAGE_TAB_PREVIEW)
    expect(nextTabAfterClose(['a.html', 'b.md'], 'a.html', previewTabId('b.md'))).toBe(previewTabId('b.md'))
    expect(nextTabAfterClose(['a.html'], 'a.html', previewTabId('a.html'), true)).toBe(REACT_STAGE_TAB_NOVNC)
    expect(nextTabAfterClose(['a.html'], 'a.html', REACT_STAGE_TAB_NOVNC)).toBe(REACT_STAGE_TAB_NOVNC)
  })

  it('diffs fingerprints for create/update and filters auto-pin visibility (g1.1)', () => {
    expect(isAutoPinStageNode('react')).toBe(true)
    expect(isAutoPinStageNode('approve')).toBe(true)
    expect(isAutoPinStageNode('visual')).toBe(false)
    const research = art({ id: 'r', name: 'research.json', kind: 'json', nodeId: 'approve_1' })
    const complete = art({ id: 'n', name: 'node_complete.json', kind: 'json', nodeId: 'approve_1' })
    const feedback = art({ id: 'f', name: 'feedback.clarify.x.json', kind: 'json', nodeId: 'approve_1' })
    const index = art({ id: 'i', name: 'feedback_index.json', kind: 'json', nodeId: 'approve_1' })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_1' })
    const alias = art({ id: 'a', name: visualNodePageName('visual_1'), kind: 'html', nodeId: 'visual_1' })
    const hist = art({
      id: historicalStageArtifactId('visual_1', 1),
      name: 'page.html#iter-1',
      kind: 'html',
      nodeId: 'visual_1',
    })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'approve_1' })
    const list = [research, complete, feedback, index, page, alias, hist, demo]
    expect(filterVisibleStageArtifacts(list).map((a) => a.name)).toEqual([
      'research.json',
      'page.html',
      'brand-row-preview.html',
    ])
    expect(isVisibleAutoPinArtifact(complete, list)).toBe(false)
    expect(isVisibleAutoPinArtifact(feedback, list)).toBe(false)
    expect(isVisibleAutoPinArtifact(alias, list)).toBe(false)
    expect(isVisibleAutoPinArtifact(hist, list)).toBe(false)

    const prev = buildArtifactFingerprintMap([research, demo])
    const next = buildArtifactFingerprintMap([
      { ...research, revision: 2, updatedAt: 't2', sizeBytes: 99 },
      demo,
      art({ id: 'p1', name: 'plan.json', kind: 'json', nodeId: 'approve_1' }),
    ])
    expect(diffArtifactFingerprints(null, next)).toEqual({ created: [], updated: [] })
    expect(diffArtifactFingerprints(prev, next)).toEqual({
      created: ['plan.json'],
      updated: ['research.json'],
    })
  })

  it('keeps unread marks and idle focus helpers for auto-pin (g1.3 / g2.1)', () => {
    expect(isIdleStageTab(REACT_STAGE_TAB_GRID)).toBe(true)
    expect(isIdleStageTab(REACT_STAGE_TAB_PREVIEW)).toBe(true)
    expect(isIdleStageTab(previewTabId('plan.json'))).toBe(false)
    expect(isIdleStageTab(REACT_STAGE_TAB_NOVNC)).toBe(false)
    expect(shouldFocusPinnedOrAutoTab({ userMoved: false, activeTab: previewTabId('a') })).toBe(true)
    expect(
      shouldFocusPinnedOrAutoTab({
        userMoved: true,
        activeTab: REACT_STAGE_TAB_GRID,
        onlyIdle: true,
      }),
    ).toBe(true)
    expect(
      shouldFocusPinnedOrAutoTab({
        userMoved: false,
        activeTab: previewTabId('a'),
        onlyIdle: true,
      }),
    ).toBe(false)
    expect(
      shouldFocusPinnedOrAutoTab({
        userMoved: true,
        activeTab: previewTabId('a'),
      }),
    ).toBe(false)
    let marks = markStageTabUnread({}, 'plan.json', 'new')
    marks = markStageTabUnread(marks, 'plan.json', 'updated')
    expect(marks['plan.json']).toBe('new')
    marks = markStageTabUnread(marks, 'research.json', 'updated')
    expect(marks['research.json']).toBe('updated')
    marks = clearStageTabUnread(marks, 'plan.json')
    expect(marks['plan.json']).toBeUndefined()
  })

  it('shows all visible products on react/approve grids including custom names (g1.2 / f5)', () => {
    const run = {
      nodes: [{ id: 'approve_1', type: 'approve', label: 'Approve', position: { x: 0, y: 0 }, config: {} }],
    } as unknown as Run
    const requirement = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'approve_1',
    })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'approve_1' })
    const complete = art({ id: 'n', name: 'node_complete.json', kind: 'json', nodeId: 'approve_1' })
    expect(
      stageGridArtifactsForNode([requirement, demo, complete], run, '', 'approve').map((a) => a.name),
    ).toEqual(['clarified_requirement.json', 'brand-row-preview.html'])
    expect(
      stageGridArtifactsForNode([requirement, demo, complete], run, 'brand-row-preview.html', 'visual').map(
        (a) => a.name,
      ),
    ).toEqual(['clarified_requirement.json', 'brand-row-preview.html'])
  })

  it('patches previewArtifact without replacing turns', () => {
    const current = {
      id: 'r1',
      artifacts: [art({ id: 'a1', name: 'old.html' })],
      clarify: { nodeId: 'c1', turns: [{ role: 'agent', text: 'hi', at: 't' }], done: false },
      clarifyByNode: {
        c1: { nodeId: 'c1', turns: [{ role: 'agent', text: 'hi', at: 't' }], done: false },
      },
    } as unknown as Run
    const named = applyPreviewArtifactName(current, 'c1', 'page.html')
    expect(named.clarify?.previewArtifact).toBe('page.html')
    expect(named.clarifyByNode?.c1.turns).toHaveLength(1)

    const onlyTop = {
      id: 'r2',
      artifacts: [],
      clarify: { nodeId: 'c2', turns: [{ role: 'agent', text: 'keep', at: 't' }], done: false },
    } as unknown as Run
    const filled = applyPreviewArtifactName(onlyTop, 'c2', 'note.md')
    expect(filled.clarifyByNode?.c2.previewArtifact).toBe('note.md')
    expect(filled.clarifyByNode?.c2.turns[0].text).toBe('keep')
    const incoming = {
      artifacts: [art({ id: 'a2', name: 'page.html', revision: 4 })],
      clarifyByNode: { c1: { nodeId: 'c1', turns: [], done: false, previewArtifact: 'page.html' } },
    } as unknown as Run
    const merged = applyPreviewArtifactFromRun(named, incoming)
    expect(merged.artifacts[0].name).toBe('page.html')
    expect(merged.clarifyByNode?.c1.previewArtifact).toBe('page.html')
    expect(merged.clarifyByNode?.c1.turns[0].text).toBe('hi')
  })

  it('activates a pin when it changes or the named artifact first appears', () => {
    expect(shouldActivatePinnedPreview('page.html', ['note.md'])).toBe(false)
    expect(shouldActivatePinnedPreview('page.html', ['page.html'])).toBe(true)
    expect(shouldActivatePinnedPreview('page.html', ['page.html'], '', [])).toBe(true)
    expect(shouldActivatePinnedPreview('page.html', ['page.html'], 'page.html', ['note.md'])).toBe(true)
    expect(shouldActivatePinnedPreview('b.md', ['a.html', 'b.md'], 'a.html', ['a.html', 'b.md'])).toBe(true)
    expect(shouldActivatePinnedPreview('page.html', ['page.html', 'extra.md'], 'page.html', ['page.html'])).toBe(
      false,
    )
    expect(shouldActivatePinnedPreview('page.html', ['page.html'], '', [], true)).toBe(false)
    expect(shouldActivatePinnedPreview('page.html', ['page.html'], undefined, undefined, true)).toBe(false)
  })

  it('prefers an on-stage pin, then visual page.html, then newest own-node HTML', () => {
    const pin = art({ id: 'pin', name: 'brief.md', kind: 'markdown', nodeId: 'react' })
    const live = art({ id: 'live', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const copy = art({ id: 'copy', name: 'visual_bqc5.page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const hist = art({ id: 'hist', name: 'page.html#iter-1', kind: 'html', nodeId: 'visual_bqc5' })
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: 'brief.md',
        artifacts: [live, pin],
        nodeType: 'visual',
        nodeId: 'visual_bqc5',
      }),
    ).toBe('brief.md')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: 'missing.html',
        artifacts: [live],
        nodeType: 'visual',
        nodeId: 'visual_bqc5',
      }),
    ).toBe('')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: '',
        artifacts: [copy, hist, live],
        nodeType: 'visual',
        nodeId: 'visual_bqc5',
      }),
    ).toBe('page.html')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: '',
        artifacts: [copy],
        nodeType: 'visual',
        nodeId: 'visual_bqc5',
      }),
    ).toBe('')
  })

  it('picks the newest own-node HTML for unpinned react and ignores upstream page.html', () => {
    const upstream = art({
      id: 'up',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_bqc5',
      updatedAt: '2026-08-19T20:00:00Z',
      revision: 9,
    })
    const older = art({
      id: 'old',
      name: 'a.html',
      kind: 'html',
      nodeId: 'react_ymx0',
      updatedAt: '2026-08-19T10:00:00Z',
      revision: 2,
    })
    const newer = art({
      id: 'new',
      name: 'brand-row-preview.html',
      kind: 'html',
      nodeId: 'react_ymx0',
      updatedAt: '2026-08-19T12:00:00Z',
      revision: 1,
    })
    const json = art({ id: 'j', name: 'research.json', kind: 'json', nodeId: 'react_ymx0' })
    expect(latestOwnNodeHtmlName([upstream, older, newer, json], 'react_ymx0')).toBe('brand-row-preview.html')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: '',
        artifacts: [upstream, older, newer, json],
        nodeType: 'react',
        nodeId: 'react_ymx0',
      }),
    ).toBe('brand-row-preview.html')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: 'research.json',
        artifacts: [upstream, older, newer, json],
        nodeType: 'react',
        nodeId: 'react_ymx0',
      }),
    ).toBe('research.json')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: '',
        artifacts: [json],
        nodeType: 'react',
        nodeId: 'react_ymx0',
      }),
    ).toBe('')
    expect(
      resolveEffectivePreviewPin({
        previewArtifact: '',
        artifacts: [newer],
        nodeType: 'research',
        nodeId: 'research',
      }),
    ).toBe('')
    expect(latestOwnNodeHtmlName([newer], '')).toBe('')
  })

  it('hides bookkeeping artifacts and keeps agent-written products on the pipeline grid', () => {
    const run = {
      nodes: [
        { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
        { id: 'react_ymx0', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} },
        { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
        { id: 'clarify', type: 'react', label: '需求', position: { x: 0, y: 0 }, config: {} },
        { id: 'approve_7gl6', type: 'approve', label: 'Approve', position: { x: 0, y: 0 }, config: {} },
        { id: 'human_gate_x1', type: 'human_gate', label: '门禁', position: { x: 0, y: 0 }, config: {} },
      ],
    } as unknown as Run
    const research = art({ id: 'r', name: 'research.json', kind: 'json', nodeId: 'research' })
    const requirement = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'clarify',
    })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const approvePage = art({ id: 'ap', name: 'page.html', kind: 'html', nodeId: 'approve_7gl6' })
    const hist = art({
      id: historicalStageArtifactId('visual_bqc5', 1),
      name: 'page.html#iter-1',
      kind: 'html',
      nodeId: 'visual_bqc5',
    })
    const complete = art({ id: 'n', name: 'node_complete.json', kind: 'json', nodeId: 'visual_bqc5' })
    const feedback = art({ id: 'f', name: 'feedback_index.json', kind: 'json', nodeId: 'react_ymx0' })
    const feedbackRound = art({
      id: 'fr',
      name: 'feedback.clarify.approve_7gl6.i1.json',
      kind: 'json',
      nodeId: 'approve_7gl6',
    })
    const runError = art({ id: 're', name: 'run_error.json', kind: 'json', nodeId: 'approve_7gl6' })
    const annotations = art({
      id: 'pa',
      name: 'preview_annotations.json',
      kind: 'json',
      nodeId: 'human_gate_x1',
    })
    const copy = art({ id: 'v', name: visualNodePageName('visual_bqc5'), kind: 'html', nodeId: 'visual_bqc5' })
    const otherCopy = art({
      id: 'vo',
      name: visualNodePageName('visual_other'),
      kind: 'html',
      nodeId: 'visual_other',
    })
    const gateBody = art({ id: 'gm', name: 'human_gate_x1.md', kind: 'markdown', nodeId: 'human_gate_x1' })
    const designMd = art({ id: 'dm', name: 'design.md', kind: 'markdown', nodeId: 'approve_7gl6' })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'react_ymx0' })
    const screenshot = art({
      id: 's',
      name: 'screenshot-home.html',
      kind: 'html',
      nodeId: 'approve_7gl6',
    })
    const names = filterStageGridArtifacts(
      [
        research,
        requirement,
        page,
        approvePage,
        hist,
        complete,
        feedback,
        feedbackRound,
        runError,
        annotations,
        copy,
        otherCopy,
        gateBody,
        designMd,
        demo,
        screenshot,
      ],
      run,
    ).map((a) => a.name)
    expect(names).toEqual([
      'research.json',
      'clarified_requirement.json',
      'page.html',
      'page.html',
      'page.html#iter-1',
      'design.md',
      'brand-row-preview.html',
      'screenshot-home.html',
    ])
    expect(isBookkeepingArtifact(complete, run)).toBe(true)
    expect(isBookkeepingArtifact(feedbackRound, run)).toBe(true)
    expect(isBookkeepingArtifact(runError, run)).toBe(true)
    expect(isBookkeepingArtifact(annotations, run)).toBe(true)
    expect(isBookkeepingArtifact(copy, run)).toBe(true)
    expect(isBookkeepingArtifact(otherCopy, run)).toBe(true)
    expect(isBookkeepingArtifact(gateBody, run)).toBe(true)
    expect(isBookkeepingArtifact(designMd, run)).toBe(false)
    expect(isBookkeepingArtifact(demo, run)).toBe(false)
    expect(isBookkeepingArtifact(approvePage, run)).toBe(false)
    expect(isBookkeepingArtifact(page)).toBe(false)
    expect(
      stageGridArtifactsWithPin(
        [research, requirement, page, complete, feedback, copy, demo],
        run,
        'brand-row-preview.html',
      ).map((a) => a.name),
    ).toEqual(['research.json', 'clarified_requirement.json', 'page.html', 'brand-row-preview.html'])
    expect(
      stageGridArtifactsWithPin([research, requirement, page, demo], run, 'page.html').map((a) => a.name),
    ).toEqual(['research.json', 'clarified_requirement.json', 'page.html', 'brand-row-preview.html'])
  })

  it('maps every reserved product to the shared friendly display-name keys', () => {
    expect(artifactFriendlyNameKey('research.json')).toBe('common.gateBodyLabels.research')
    expect(artifactFriendlyNameKey('clarified_requirement.json')).toBe(
      'common.gateBodyLabels.clarifiedRequirement',
    )
    expect(artifactFriendlyNameKey('plan.json')).toBe('common.gateBodyLabels.plan')
    expect(artifactFriendlyNameKey('proposals.json')).toBe('common.gateBodyLabels.proposals')
    expect(artifactFriendlyNameKey('proposal.json')).toBe('common.gateBodyLabels.proposal')
    expect(artifactFriendlyNameKey('test_result.json')).toBe('common.gateBodyLabels.testResult')
    expect(artifactFriendlyNameKey('review.json')).toBe('common.gateBodyLabels.review')
    expect(artifactFriendlyNameKey('implementation_result.json')).toBe(
      'common.gateBodyLabels.implementationResult',
    )
    expect(artifactFriendlyNameKey('page.html')).toBe('common.gateBodyLabels.pagePreview')
    expect(artifactFriendlyNameKey(visualNodePageName('visual_bqc5'))).toBe(
      'common.gateBodyLabels.pagePreview',
    )
    expect(artifactFriendlyNameKey('notes.json')).toBeNull()
    expect(artifactTechnicalDisplayName('page.html#iter-2')).toBe('page.html')
    expect(isVisualPreviewArtifactName('page.html')).toBe(true)
    expect(isVisualPreviewArtifactName('visual_1.page.html')).toBe(true)
    expect(isVisualPreviewArtifactName('research.json')).toBe(false)
  })

  it('parses structured JSON root title/summary and falls back when missing', () => {
    expect(
      parseStructuredArtifactSummary(
        JSON.stringify({ title: '调研标题', summary: '一段摘要', extra: 1 }),
      ),
    ).toEqual({ title: '调研标题', summary: '一段摘要' })
    expect(parseStructuredArtifactSummary(JSON.stringify({ title: '仅标题' }))).toEqual({
      title: '仅标题',
      summary: '',
    })
    expect(parseStructuredArtifactSummary(JSON.stringify({ summary: '仅摘要' }))).toEqual({
      title: '',
      summary: '仅摘要',
    })
    expect(parseStructuredArtifactSummary(JSON.stringify({ name: 'nope' }))).toBeNull()
    expect(parseStructuredArtifactSummary('not-json')).toBeNull()
    expect(parseStructuredArtifactSummary('')).toBeNull()
  })

  it('extracts visual HTML title/summary from title/h1 and meta/banner', () => {
    const html =
      '<!doctype html><html><head><title>Grasp · Demo</title>' +
      '<meta name="description" content="meta 摘要"/>' +
      '</head><body><div class="banner"><h1>主标题</h1><p>banner 段落</p></div></body></html>'
    expect(extractVisualHtmlSummary(html)).toEqual({
      title: 'Grasp · Demo',
      summary: 'meta 摘要',
    })
    const noMeta =
      '<html><body><div class="banner"><h1>流水线产物</h1><p>友好名 + 简单预览</p></div></body></html>'
    expect(extractVisualHtmlSummary(noMeta)).toEqual({
      title: '流水线产物',
      summary: '友好名 + 简单预览',
    })
    expect(extractVisualHtmlSummary('<html><body><div>x</div></body></html>')).toBeNull()
    expect(extractVisualHtmlSummary('<title>A &amp;lt; B &amp; C</title>')).toEqual({
      title: 'A &lt; B & C',
      summary: '',
    })
  })

  it('builds text thumbs for JSON/visual HTML and html thumbs for other HTML', () => {
    expect(wantsTextSummaryThumb({ kind: 'json', name: 'research.json' })).toBe(true)
    expect(wantsTextSummaryThumb({ kind: 'html', name: 'page.html' })).toBe(true)
    expect(wantsTextSummaryThumb({ kind: 'html', name: 'brand-row-preview.html' })).toBe(false)
    expect(
      buildStageCardThumb(
        { kind: 'json', name: 'research.json' },
        JSON.stringify({ title: 'T', summary: 'S' }),
      ),
    ).toEqual({ kind: 'text', title: 'T', summary: 'S' })
    expect(
      buildStageCardThumb(
        { kind: 'html', name: 'page.html' },
        '<html><head><title>视觉</title></head><body><p>摘要行</p></body></html>',
      ),
    ).toEqual({ kind: 'text', title: '视觉', summary: '摘要行' })
    expect(
      buildStageCardThumb({ kind: 'html', name: 'demo.html' }, '<html>thumb</html>'),
    ).toEqual({ kind: 'html', html: '<html>thumb</html>' })
    expect(buildStageCardThumb({ kind: 'json', name: 'research.json' }, '{}')).toBeNull()
  })

  describe('stage open-state sessionStorage', () => {
    beforeEach(() => {
      resetStageOpenStateForTests('ss-unit-run')
    })
    afterEach(() => {
      resetStageOpenStateForTests('ss-unit-run')
    })

    it('persists and restores openNames + activeTab under runId+nodeId', () => {
      expect(stageOpenStateStorageKey('ss-unit-run', 'node-b')).toBe('appr.reactStageOpen:ss-unit-run:node-b')
      saveStageOpenState('ss-unit-run', 'node-b', {
        openNames: ['page.html', 'research.json'],
        activeTab: previewTabId('research.json'),
        novncOpen: false,
      })
      expect(loadStageOpenState('ss-unit-run', 'node-b')).toEqual({
        openNames: ['page.html', 'research.json'],
        activeTab: previewTabId('research.json'),
        novncOpen: false,
      })
      expect(loadStageOpenState('ss-unit-run', 'other')).toBeNull()
      expect(loadStageOpenState('', 'node-b')).toBeNull()
    })

    it('skips gone artifacts and recalculates the active tab', () => {
      const saved = {
        openNames: ['page.html', 'gone.json', 'research.json'],
        activeTab: previewTabId('gone.json'),
        novncOpen: false,
      }
      expect(restoreStageOpenState(saved, ['page.html', 'research.json'])).toEqual({
        openNames: ['page.html', 'research.json'],
        activeTab: previewTabId('research.json'),
        novncOpen: false,
      })
      expect(restoreStageOpenState(saved, [])).toEqual({
        openNames: ['page.html', 'gone.json', 'research.json'],
        activeTab: previewTabId('gone.json'),
        novncOpen: false,
      })
      expect(
        restoreStageOpenState(
          { openNames: [], activeTab: REACT_STAGE_TAB_GRID, novncOpen: false },
          ['page.html'],
        ),
      ).toBeNull()
    })
  })
})
