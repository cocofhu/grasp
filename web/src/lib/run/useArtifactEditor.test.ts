// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  saveRunArtifact: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({ api: apiMocks }))
vi.mock('@/lib/shared/i18n', () => ({
  i18n: { global: { t: (k: string, p?: Record<string, unknown>) => `${k}${p ? JSON.stringify(p) : ''}` } },
}))

import {
  hasAnyDirtyArtifactDraft,
  resetArtifactEditorsForTest,
  setArtifactSessionBusy,
  useArtifactEditor,
} from './useArtifactEditor'

const plan = { id: 'a1', name: 'plan.json', kind: 'json' as const, revision: 3 }

function httpError(status: number, body: Record<string, unknown>) {
  return Object.assign(new Error(String(body.message || 'err')), { status, ...body })
}

describe('useArtifactEditor', () => {
  beforeEach(() => {
    resetArtifactEditorsForTest()
    vi.clearAllMocks()
    apiMocks.artifactContent.mockResolvedValue({ content: '{"title":"p"}', etag: 'e3', revision: 3 })
  })
  afterEach(() => vi.restoreAllMocks())

  it('shares one draft per run and tracks dirty state semantically', async () => {
    const a = useArtifactEditor('run-1')
    const b = useArtifactEditor(() => 'run-1')
    const other = useArtifactEditor('run-2')
    const d = await a.begin(plan)
    expect(d?.base).toBe('{"title":"p"}')
    expect(d?.baseEtag).toBe('e3')
    expect(b.draft('plan.json')).toBe(a.draft('plan.json'))
    expect(await b.begin(plan)).toBe(a.draft('plan.json'))
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)
    expect(other.draft('plan.json')).toBeUndefined()

    a.update('plan.json', '{\n  "title": "p"\n}')
    expect(a.isDirty('plan.json')).toBe(false)
    a.update('plan.json', '{"title":"q"}')
    expect(b.isDirty('plan.json')).toBe(true)
    expect(b.hasDirty.value).toBe(true)
    expect(b.dirtyNames.value).toEqual(['plan.json'])
    expect(hasAnyDirtyArtifactDraft()).toBe(true)
    a.discard('plan.json')
    expect(b.hasDirty.value).toBe(false)
    expect(hasAnyDirtyArtifactDraft()).toBe(false)
  })

  it('saves with If-Match and drops the draft on success', async () => {
    const ed = useArtifactEditor('run-1')
    await ed.begin(plan)
    ed.update('plan.json', '{"title":"q"}')
    apiMocks.saveRunArtifact.mockResolvedValue({ name: 'plan.json', revision: 4, changed: true, content: '{"title":"q"}' })
    const r = await ed.save('plan.json')
    expect(r).toMatchObject({ ok: true, result: { revision: 4 } })
    expect(apiMocks.saveRunArtifact).toHaveBeenCalledWith('run-1', 'plan.json', '{"title":"q"}', 'e3')
    expect(ed.draft('plan.json')).toBeUndefined()
    expect(await ed.save('plan.json')).toEqual({ ok: false, code: 'not_editing' })
  })

  it('records conflicts, field errors and missing artifacts', async () => {
    const ed = useArtifactEditor('run-1')
    await ed.begin(plan)
    ed.update('plan.json', '{"title":"q"}')

    apiMocks.saveRunArtifact.mockRejectedValueOnce(httpError(409, { code: 'artifact_conflict', revision: 5, message: 'stale' }))
    expect(await ed.save('plan.json')).toEqual({ ok: false, code: 'artifact_conflict' })
    expect(ed.draft('plan.json')?.conflict).toEqual({ revision: 5 })
    expect(ed.draft('plan.json')?.saving).toBe(false)

    apiMocks.saveRunArtifact.mockResolvedValueOnce({ name: 'plan.json', revision: 6, changed: true, content: '' })
    await ed.save('plan.json', { force: true })
    expect(apiMocks.saveRunArtifact).toHaveBeenLastCalledWith('run-1', 'plan.json', '{"title":"q"}', undefined)

    await ed.begin(plan)
    ed.update('plan.json', '{"goals":[]}')
    apiMocks.saveRunArtifact.mockRejectedValueOnce(httpError(400, { code: 'invalid', field: 'goals', message: 'goals 至少需要 1 条' }))
    await ed.save('plan.json')
    expect(ed.draft('plan.json')?.fieldErrors).toEqual({ goals: 'goals 至少需要 1 条' })
    ed.update('plan.json', '{"goals":[1]}')
    expect(ed.draft('plan.json')?.fieldErrors).toEqual({})
    expect(ed.draft('plan.json')?.error).toBe('')

    apiMocks.saveRunArtifact.mockRejectedValueOnce(httpError(404, { code: 'artifact_not_found' }))
    await ed.save('plan.json')
    expect(ed.draft('plan.json')?.missing).toBe(true)
  })

  it('reloads the latest body and clears the conflict', async () => {
    const ed = useArtifactEditor('run-1')
    await ed.begin(plan)
    ed.update('plan.json', '{"title":"mine"}')
    ed.draft('plan.json')!.conflict = { revision: 5 }
    apiMocks.artifactContent.mockResolvedValueOnce({ content: '{"title":"theirs"}', etag: 'e5', revision: 5 })
    await ed.reloadLatest('plan.json')
    const d = ed.draft('plan.json')!
    expect(d).toMatchObject({ content: '{"title":"theirs"}', baseEtag: 'e5', baseRevision: 5, conflict: null })
    expect(ed.isDirty('plan.json')).toBe(false)
  })

  it('marks a draft missing when its body cannot be loaded', async () => {
    apiMocks.artifactContent.mockRejectedValueOnce(httpError(404, { message: 'gone' }))
    const ed = useArtifactEditor('run-1')
    const d = await ed.begin(plan)
    expect(d?.missing).toBe(true)
    expect(d?.loading).toBe(false)
  })

  it('saveAll stops at the first failure', async () => {
    const ed = useArtifactEditor('run-1')
    await ed.begin(plan)
    await ed.begin({ id: 'a2', name: 'test_result.json', kind: 'json', revision: 1 })
    ed.update('plan.json', '{"title":"q"}')
    ed.update('test_result.json', '{"summary":"s"}')
    apiMocks.saveRunArtifact
      .mockResolvedValueOnce({ name: 'plan.json', revision: 4, changed: true, content: '' })
      .mockRejectedValueOnce(httpError(400, { message: 'bad' }))
    const r = await ed.saveAll()
    expect(r.ok).toBe(false)
    expect(r.failed).toBe('test_result.json')
    expect(r.results).toHaveLength(1)
    expect(ed.dirtyNames.value).toEqual(['test_result.json'])

    apiMocks.saveRunArtifact.mockResolvedValueOnce({ name: 'test_result.json', revision: 2, changed: true, content: '' })
    expect((await ed.saveAll()).ok).toBe(true)
  })

  it('confirmLeave asks only for dirty drafts and discards them on confirm', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const ed = useArtifactEditor('run-1')
    expect(ed.confirmLeave()).toBe(true)
    await ed.begin(plan)
    expect(ed.confirmLeave(['plan.json'])).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
    ed.update('plan.json', '{"title":"q"}')
    confirm.mockReturnValueOnce(false)
    expect(ed.confirmLeave()).toBe(false)
    expect(ed.isDirty('plan.json')).toBe(true)
    confirm.mockReturnValueOnce(true)
    expect(ed.confirmLeave()).toBe(true)
    expect(ed.draft('plan.json')).toBeUndefined()
  })

  it('aggregates chat busy flags per run, null when no chat is mounted', () => {
    const runId = ref('run-1')
    const ed = useArtifactEditor(runId)
    expect(ed.sessionBusy.value).toBeNull()
    ed.setSessionBusy('chat:a', false)
    expect(ed.sessionBusy.value).toBe(false)
    setArtifactSessionBusy('run-1', 'chat:b', true)
    expect(ed.sessionBusy.value).toBe(true)
    ed.clearSessionBusy('chat:b')
    setArtifactSessionBusy('run-1', 'chat:a', null)
    expect(ed.sessionBusy.value).toBeNull()
    setArtifactSessionBusy('', 'x', true)
    runId.value = ''
    expect(ed.sessionBusy.value).toBeNull()
    expect(ed.drafts.value).toEqual({})
  })

  it('warns before unload while a draft is dirty', async () => {
    const ed = useArtifactEditor('run-1')
    await ed.begin(plan)
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    ed.update('plan.json', '{"title":"q"}')
    const dirty = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirty)
    expect(dirty.defaultPrevented).toBe(true)
  })
})
