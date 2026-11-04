import { describe, expect, it } from 'vitest'
import type { Run } from '@/lib/shared/types'
import {
  countArtifactChanges,
  editReasonKey,
  isEditableArtifact,
  jsonErrorLocation,
  resolveRunEditState,
  sameArtifactContent,
} from './artifactEdit'

function run(partial: Partial<Run>): Run {
  return { id: 'run-1', status: 'waiting_human', ...partial } as Run
}

describe('isEditableArtifact', () => {
  it('accepts text artifacts and rejects images, ledgers, history and paths', () => {
    expect(isEditableArtifact({ id: 'a', name: 'plan.json', kind: 'json' })).toBe(true)
    expect(isEditableArtifact({ id: 'a', name: 'page.html', kind: 'html' })).toBe(true)
    expect(isEditableArtifact({ id: 'a', name: 'notes.md', kind: 'markdown' })).toBe(true)
    expect(isEditableArtifact({ id: 'a', name: 'shot.png', kind: 'image' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: 'shot.png', kind: 'text' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: 'feedback_index.json', kind: 'json' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: 'feedback.review.n1.i1.json', kind: 'json' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: 'page.html#iter-2', kind: 'html' })).toBe(false)
    expect(isEditableArtifact({ id: 'historical-page:visual:1', name: 'page.html', kind: 'html' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: 'dir/x.md', kind: 'markdown' })).toBe(false)
    expect(isEditableArtifact({ id: 'a', name: '', kind: 'markdown' })).toBe(false)
    expect(isEditableArtifact(null)).toBe(false)
  })
})

describe('resolveRunEditState', () => {
  it('follows run status', () => {
    expect(resolveRunEditState(null)).toEqual({ editable: false, reason: 'run_running' })
    expect(resolveRunEditState(run({ status: 'failed' }))).toEqual({ editable: true, reason: '' })
    expect(resolveRunEditState(run({ status: 'completed' }))).toEqual({ editable: false, reason: 'run_ended' })
    expect(resolveRunEditState(run({ status: 'cancelled' }))).toEqual({ editable: false, reason: 'run_ended' })
    expect(resolveRunEditState(run({ status: 'running' }))).toEqual({ editable: false, reason: 'run_running' })
    expect(resolveRunEditState(run({ status: 'queued' as Run['status'] }))).toEqual({ editable: false, reason: 'run_running' })
  })

  it('waiting_human: live chat flag wins, then the server snapshot, then react sessions', () => {
    const busySnap = run({ artifactEdit: { editable: false, reason: 'session_busy' } })
    expect(resolveRunEditState(busySnap)).toEqual({ editable: false, reason: 'session_busy' })
    expect(resolveRunEditState(busySnap, false)).toEqual({ editable: true, reason: '' })
    expect(resolveRunEditState(run({ artifactEdit: { editable: true, reason: '' } }), true)).toEqual({
      editable: false,
      reason: 'session_busy',
    })
    expect(resolveRunEditState(run({ reactSessions: { n1: { busy: true } } }))).toEqual({
      editable: false,
      reason: 'session_busy',
    })
    expect(resolveRunEditState(run({ reactSessions: { n1: { waiting: 2 } } })).reason).toBe('session_busy')
    expect(resolveRunEditState(run({ reactSessions: { n1: { busy: false, waiting: 0 } } })).editable).toBe(true)
  })

  it('maps reasons to copy keys', () => {
    expect(editReasonKey('run_ended')).toBe('pages.artifactEditor.reasonRunEnded')
    expect(editReasonKey('run_running')).toBe('pages.artifactEditor.reasonRunRunning')
    expect(editReasonKey('session_busy')).toBe('pages.artifactEditor.reasonSessionBusy')
    expect(editReasonKey('')).toBe('')
  })
})

describe('content comparison', () => {
  it('compares JSON by value and text by normalized lines', () => {
    expect(sameArtifactContent('json', '{"a":1,"b":[1,2]}', '{\n  "b": [1, 2],\n  "a": 1\n}')).toBe(true)
    expect(sameArtifactContent('json', '{"a":1}', '{"a":2}')).toBe(false)
    expect(sameArtifactContent('json', '{bad', '{bad')).toBe(true)
    expect(sameArtifactContent('markdown', 'a\r\nb\n', 'a\nb')).toBe(true)
    expect(sameArtifactContent('markdown', 'a', 'b')).toBe(false)
  })

  it('counts changed JSON leaves or text lines', () => {
    expect(countArtifactChanges('json', '{"a":1}', '{"a":1}')).toBe(0)
    expect(countArtifactChanges('json', '{"a":1,"b":{"c":1}}', '{"a":2,"b":{"c":2},"d":1}')).toBe(3)
    expect(countArtifactChanges('json', '{"l":[1,2,3]}', '{"l":[1]}')).toBe(2)
    expect(countArtifactChanges('json', '{"a":1}', '{bad')).toBe(1)
    expect(countArtifactChanges('markdown', 'a\nb\nc', 'a\nB\nc\nd')).toBe(2)
  })

  it('locates JSON syntax errors', () => {
    expect(jsonErrorLocation('{"a":1}')).toBeNull()
    const loc = jsonErrorLocation('{\n  "a": 1,\n  oops\n}')
    expect(loc?.line).toBe(3)
    expect(loc?.column).toBeGreaterThan(0)
    expect(jsonErrorLocation('')?.line).toBe(1)
  })
})
