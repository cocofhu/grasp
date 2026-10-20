import { describe, expect, it, vi } from 'vitest'
import { formatUnreadBadge } from '@/lib/run/useRunTerminalNotifications'
import {
  artifactButtonAriaLabel,
  createCitationUnreadState,
  nextRunArtifactUnread,
  notifyRunArtifactEdit,
  onRunArtifactEdit,
  reduceArtifactCitationUnread,
  runArtifactUnreadOnPanel,
  type ArtifactCitationMessage,
  type CitationUnreadState,
} from './artifactUnread'

const labels = {
  open: '打开产物',
  close: '关闭产物',
  unread: (count: string) => `打开产物，${count} 项未查看`,
  cap: '打开产物，99 项及以上未查看',
}

function settle(threadId: string, messages: ArtifactCitationMessage[], panelOpen = false): CitationUnreadState {
  return reduceArtifactCitationUnread(createCitationUnreadState(), {
    threadId,
    panelOpen,
    loading: false,
    messages,
  })
}

describe('run artifact unread count', () => {
  it('counts closed-panel writes, ignores other events, and caps the label at 99+', () => {
    let count = 0
    let panelOpen = false
    expect(count).toBe(0)
    expect(formatUnreadBadge(count)).toBe('')

    count = nextRunArtifactUnread(count, panelOpen, 'artifact')
    expect(count).toBe(1)
    expect(formatUnreadBadge(count)).toBe('1')
    count = nextRunArtifactUnread(count, panelOpen, 'artifact')
    expect(count).toBe(2)
    expect(formatUnreadBadge(count)).toBe('2')

    count = nextRunArtifactUnread(count, panelOpen, 'other')
    expect(count).toBe(2)

    panelOpen = true
    count = runArtifactUnreadOnPanel(count, panelOpen)
    expect(count).toBe(0)
    count = nextRunArtifactUnread(count, panelOpen, 'artifact')
    expect(count).toBe(0)

    panelOpen = false
    count = runArtifactUnreadOnPanel(count, panelOpen)
    count = nextRunArtifactUnread(count, panelOpen, 'artifact')
    expect(count).toBe(1)

    count = 0
    for (let i = 0; i < 99; i += 1) count = nextRunArtifactUnread(count, false, 'artifact')
    expect(formatUnreadBadge(count)).toBe('99+')
    count = nextRunArtifactUnread(count, false, 'artifact')
    expect(formatUnreadBadge(count)).toBe('99+')
    expect(runArtifactUnreadOnPanel(count, true)).toBe(0)
  })

  it('clears the count when the conversation scope changes', () => {
    let count = nextRunArtifactUnread(0, false, 'artifact')
    count = nextRunArtifactUnread(count, false, 'artifact')
    expect(count).toBe(2)
    count = 0
    expect(formatUnreadBadge(count)).toBe('')
    expect(nextRunArtifactUnread(count, false, 'other')).toBe(0)
  })

  it('notifies only current subscribers', () => {
    const first = vi.fn()
    const second = vi.fn()
    const stopFirst = onRunArtifactEdit(first)
    const stopSecond = onRunArtifactEdit(second)
    notifyRunArtifactEdit()
    stopFirst()
    notifyRunArtifactEdit()
    stopSecond()
    notifyRunArtifactEdit()
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(2)
  })
})

describe('project manager artifact citation count', () => {
  const history: ArtifactCitationMessage[] = [
    { id: 'm1', citations: [{ type: 'artifact', targetId: 'art-aaaaaaaa' }] },
    { id: 'm2', citations: [{ type: 'run', targetId: 'run-aaaaaaaa' }] },
  ]

  it('ignores citations already on the thread and counts only new artifact references', () => {
    let state = reduceArtifactCitationUnread(createCitationUnreadState(), {
      threadId: 'thread-1',
      panelOpen: false,
      loading: true,
      messages: [],
    })
    expect(state.count).toBe(0)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: history,
    })
    expect(state.count).toBe(0)
    expect(formatUnreadBadge(state.count)).toBe('')

    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [
        ...history,
        { id: 'm3', citations: [{ type: 'workflow', targetId: 'wf-aaaaaaaa' }, { type: 'artifact', targetId: 'research.json' }] },
      ],
    })
    expect(state.count).toBe(1)

    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [
        ...history,
        {
          id: 'm3',
          citations: [
            { type: 'workflow', targetId: 'wf-aaaaaaaa' },
            { type: 'artifact', targetId: 'research.json' },
            { type: 'gate', targetId: 'run-aaaaaaaa:human_gate' },
            { type: 'plan', targetId: 'g1.2' },
            { type: 'artifact', targetId: 'plan.json' },
          ],
        },
      ],
    })
    expect(state.count).toBe(2)
    expect(formatUnreadBadge(state.count)).toBe('2')
  })

  it('does not count while the panel is open, then starts again at 1', () => {
    let state = settle('thread-1', history)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: true,
      loading: false,
      messages: [...history, { id: 'm3', citations: [{ type: 'artifact', targetId: 'a.md' }] }],
    })
    expect(state.count).toBe(0)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [...history, { id: 'm3', citations: [{ type: 'artifact', targetId: 'a.md' }] }],
    })
    expect(state.count).toBe(0)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [
        ...history,
        { id: 'm3', citations: [{ type: 'artifact', targetId: 'a.md' }] },
        { id: 'm4', citations: [{ type: 'artifact', targetId: 'b.md' }] },
      ],
    })
    expect(state.count).toBe(1)
  })

  it('adopts older pages without counting and clears on a thread switch', () => {
    let state = settle('thread-1', history)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [...history, { id: 'm3', citations: [{ type: 'artifact', targetId: 'new.md' }] }],
    })
    expect(state.count).toBe(1)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: false,
      loading: false,
      messages: [{ id: 'older', citations: [{ type: 'artifact', targetId: 'old.md' }] }, ...history, { id: 'm3', citations: [{ type: 'artifact', targetId: 'new.md' }] }],
    })
    expect(state.count).toBe(1)

    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-2',
      panelOpen: false,
      loading: true,
      messages: [],
    })
    expect(state.count).toBe(0)
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-2',
      panelOpen: false,
      loading: false,
      messages: [{ id: 'n1', citations: [{ type: 'artifact', targetId: 'kept.md' }] }],
    })
    expect(state.count).toBe(0)
  })

  it('shows 99+ and still clears when the panel opens', () => {
    let state = settle('thread-1', [])
    const messages: ArtifactCitationMessage[] = []
    for (let i = 0; i < 100; i += 1) {
      messages.push({ id: `m${i}`, citations: [{ type: 'artifact', targetId: `file-${i}.md` }] })
      state = reduceArtifactCitationUnread(state, {
        threadId: 'thread-1',
        panelOpen: false,
        loading: false,
        messages: messages.slice(),
      })
    }
    expect(state.count).toBe(100)
    expect(formatUnreadBadge(state.count)).toBe('99+')
    state = reduceArtifactCitationUnread(state, {
      threadId: 'thread-1',
      panelOpen: true,
      loading: false,
      messages,
    })
    expect(state.count).toBe(0)
    expect(formatUnreadBadge(state.count)).toBe('')
  })
})

describe('artifact button accessible name', () => {
  it('includes the unseen count only while the panel is closed and the count is positive', () => {
    expect(artifactButtonAriaLabel(false, 0, labels)).toBe('打开产物')
    expect(artifactButtonAriaLabel(true, 2, labels)).toBe('关闭产物')
    expect(artifactButtonAriaLabel(false, 2, labels)).toBe('打开产物，2 项未查看')
    expect(artifactButtonAriaLabel(false, 99, labels)).toBe('打开产物，99 项及以上未查看')
    expect(artifactButtonAriaLabel(false, 120, labels)).toBe('打开产物，99 项及以上未查看')
  })
})
