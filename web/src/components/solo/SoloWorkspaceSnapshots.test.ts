// @vitest-environment happy-dom
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SoloWorkspaceSnapshots from './SoloWorkspaceSnapshots.vue'
import { useAuth } from '@/lib/composables/useAuth'
import { readWorkspaceRequest, workspaceRequestScope } from '@/lib/solo/workspaceRequestJournal'
import messages from '@/locales/zh-CN/workspaceSnapshots.json'

const api = vi.hoisted(() => Object.fromEntries(['list', 'listCheckpoints', 'reconcileCheckpoint', 'listRestores', 'currentBinding', 'listBindingOperations', 'get', 'capture', 'resolveCapture', 'reconcileCapture', 'restore', 'getRestore', 'resolveRestore', 'reconcileRestore', 'changeBinding', 'getBindingOperation', 'resolveBindingOperation', 'reconcileBinding'].map(name => [name, vi.fn()])))
vi.mock('@/lib/api/clients/workspaceSnapshotsClient', () => ({ workspaceSnapshotsClient: api }))

let counter = 0
let sessionId = ''
let wrapper: VueWrapper | undefined
const head = () => ({ available: true, id: 'workspace-original', revision: 1, fingerprint: 'original-binding', canActivate: true, canReturn: true, canCapture: true, canRestore: true, returnSnapshotId: 'return-save-original', retainedBindings: [], returnPoints: [] })
const restored = () => ({ id: 'restore-original', snapshotId: 'snapshot-original', state: 'restored', createdAt: '2026-11-09T08:00:00Z', updatedAt: '2026-11-09T08:00:00Z' })
function show(extraProps: { runId?: string } = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': messages }, missingWarn: false, fallbackWarn: false })
  wrapper = mount(SoloWorkspaceSnapshots, { props: { sessionId, sandboxId: 7, ...extraProps }, global: { plugins: [i18n], directives: { 'hover-ink': {} }, stubs: { Icon: true, Teleport: true, Transition: false } } })
  return wrapper
}
async function ready() { await vi.waitFor(() => expect(wrapper!.find('[data-testid="workspace-capture"]').attributes('disabled')).toBeUndefined()) }

beforeEach(() => {
  sessionId = `workspace-chat-${++counter}`
  for (const fn of Object.values(api)) fn.mockReset()
  useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
  api.list!.mockResolvedValue({ items: [{ id: 'snapshot-original', scopeKind: 'solo', scopeId: sessionId, state: 'ready', bytes: 1024, entries: 2, createdAt: '2026-11-09T08:00:00Z' }], hasMore: false })
  api.listRestores!.mockResolvedValue({ items: [], hasMore: false })
  api.currentBinding!.mockResolvedValue(head())
  api.listBindingOperations!.mockResolvedValue([])
  api.listCheckpoints!.mockResolvedValue({ items: [] })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; useAuth().clearUser() })

describe('Solo workspace snapshots', () => {
  it('retains a failed switch requiring reconciliation until explicit confirmation clears its barrier', async () => {
    api.listRestores!.mockResolvedValue({ items: [restored()], hasMore: false })
    api.changeBinding!.mockResolvedValue({ id: 'failed-switch', action: 'activate', state: 'failed', requiresReconciliation: true, createdAt: restored().createdAt })
    api.getBindingOperation!.mockResolvedValue({ id: 'failed-switch', action: 'activate', state: 'failed', requiresReconciliation: true, createdAt: restored().createdAt })
    api.reconcileBinding!.mockResolvedValue({ id: 'failed-switch', action: 'activate', state: 'failed', requiresReconciliation: false, createdAt: restored().createdAt })
    show(); await ready()
    await wrapper!.get('[data-testid="workspace-activate"]').trigger('click')
    await wrapper!.findAll('button').find(button => button.text() === '确认切换')!.trigger('click')
    await vi.waitFor(() => expect(wrapper!.get('[data-testid="workspace-pending-request"]').text()).toContain('失败'))
    expect((await readWorkspaceRequest(workspaceRequestScope('alice', sessionId)))?.receiptId).toBe('failed-switch')
    expect(api.reconcileBinding).not.toHaveBeenCalled()
    await wrapper!.get('[data-testid="workspace-pending-request"]').find('button').trigger('click')
    await vi.waitFor(() => expect(wrapper!.find('[data-testid="workspace-pending-request"]').exists()).toBe(false))
    expect(api.reconcileBinding).toHaveBeenCalledWith(sessionId, 'failed-switch')
    expect(api.changeBinding).toHaveBeenCalledTimes(1)
    expect(wrapper!.emitted('bindingChanged')).toBeUndefined()
  })
  it('shows unknown workflow outcomes without replay controls and reconciles only an original capture', async () => {
    api.listCheckpoints!.mockResolvedValue({ items: [
      { id: 'outcome-unknown', nodeId: '实现', iteration: 2, runAttempt: 3, fileState: 'unknown', routeState: 'unknown', reason: 'provider_outcome_unconfirmed' },
      { id: 'capture-unknown', nodeId: '评审', iteration: 1, runAttempt: 1, snapshotId: 'captured-original', fileState: 'unknown', routeState: 'sealed', reason: 'snapshot_unknown' },
    ] })
    api.reconcileCheckpoint!.mockResolvedValue({})
    show({ runId: 'run-original' }); await ready()
    const unknown = wrapper!.get('[data-testid="workspace-checkpoint-outcome-unknown"]')
    expect(unknown.text()).toContain('节点执行结果尚未确认')
    expect(unknown.find('button').exists()).toBe(false)
    expect(api.reconcileCheckpoint).not.toHaveBeenCalled()
    await wrapper!.get('[data-testid="workspace-checkpoint-check"]').trigger('click')
    await vi.waitFor(() => expect(api.reconcileCheckpoint).toHaveBeenCalledWith('run-original', 'capture-unknown'))
    expect(api.capture).not.toHaveBeenCalled()
    expect(api.changeBinding).not.toHaveBeenCalled()
  })
  it('saves before POST and reopening a pending restore only reads its original receipt', async () => {
    api.restore!.mockImplementation(async (_id, body) => {
      const saved = await readWorkspaceRequest(workspaceRequestScope('alice', sessionId))
      expect(saved?.request.body).toEqual(body)
      return { ...restored(), state: 'queued' }
    })
    api.getRestore!.mockResolvedValue({ ...restored(), state: 'unknown' })
    api.reconcileRestore!.mockResolvedValue(restored())
    show(); await ready()
    await wrapper!.get('[data-testid="workspace-restore-copy"]').trigger('click')
    await vi.waitFor(() => expect(api.restore).toHaveBeenCalledTimes(1))
    await vi.waitFor(async () => expect((await readWorkspaceRequest(workspaceRequestScope('alice', sessionId)))?.receiptId).toBe('restore-original'))
    wrapper!.unmount(); wrapper = undefined
    show()
    await vi.waitFor(() => expect(wrapper!.text()).toContain('结果待核对'))
    expect(api.restore).toHaveBeenCalledTimes(1)
    expect(api.changeBinding).not.toHaveBeenCalled()
    await wrapper!.get('[data-testid="workspace-pending-request"]').find('button').trigger('click')
    await vi.waitFor(() => expect(api.reconcileRestore).toHaveBeenCalledWith(sessionId, 'restore-original'))
    expect(api.restore).toHaveBeenCalledTimes(1)
    expect(api.changeBinding).not.toHaveBeenCalled()
  })

  it('retries a missing response only by explicit click with its complete original request', async () => {
    api.capture!.mockRejectedValue(new Error('reply lost'))
    api.resolveCapture!.mockRejectedValue(Object.assign(new Error('not found'), { status: 404 }))
    show(); await ready()
    await wrapper!.get('[data-testid="workspace-capture"]').trigger('click')
    await vi.waitFor(() => expect(wrapper!.find('[data-testid="workspace-retry-original"]').exists()).toBe(true))
    const original = JSON.parse(JSON.stringify(api.capture!.mock.calls[0]![1]))
    api.currentBinding!.mockResolvedValue({ ...head(), fingerprint: 'changed-binding' })
    await wrapper!.findAll('button').find(button => button.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(api.capture).toHaveBeenCalledTimes(1)
    await wrapper!.get('[data-testid="workspace-retry-original"]').trigger('click')
    await vi.waitFor(() => expect(api.capture).toHaveBeenCalledTimes(2))
    expect(api.capture!.mock.calls[1]![1]).toEqual(original)
    expect(api.capture!.mock.calls[1]![1].expectedBinding).toBe('original-binding')
  })

  it('requires a separate manual switch and reconciles unknown switches without submitting them again', async () => {
    api.listRestores!.mockResolvedValue({ items: [restored()], hasMore: false })
    api.changeBinding!.mockImplementation(async (_id, body) => {
      expect((await readWorkspaceRequest(workspaceRequestScope('alice', sessionId)))?.request.body).toEqual(body)
      return { id: 'switch-original', action: 'activate', state: 'unknown', createdAt: restored().createdAt }
    })
    api.getBindingOperation!.mockResolvedValue({ id: 'switch-original', action: 'activate', state: 'unknown', createdAt: restored().createdAt })
    api.reconcileBinding!.mockResolvedValue({ id: 'switch-original', action: 'activate', state: 'completed', createdAt: restored().createdAt })
    show(); await ready()
    expect(api.changeBinding).not.toHaveBeenCalled()
    await wrapper!.get('[data-testid="workspace-activate"]').trigger('click')
    expect(api.changeBinding).not.toHaveBeenCalled()
    await wrapper!.findAll('button').find(button => button.text() === '确认切换')!.trigger('click')
    await vi.waitFor(() => expect(api.changeBinding).toHaveBeenCalledTimes(1))
    const request = api.changeBinding!.mock.calls[0]![1]
    expect(request).toMatchObject({ action: 'activate', restoreId: 'restore-original', returnSnapshotId: 'return-save-original', expectedBinding: 'original-binding' })
    await vi.waitFor(() => expect(wrapper!.get('[data-testid="workspace-pending-request"]').text()).toContain('结果待核对'))
    await wrapper!.get('[data-testid="workspace-pending-request"]').find('button').trigger('click')
    await vi.waitFor(() => expect(api.reconcileBinding).toHaveBeenCalledWith(sessionId, 'switch-original'))
    expect(api.changeBinding).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(wrapper!.emitted('bindingChanged')).toHaveLength(1))
  })

  it('keeps stopped history readable and disables mutations', async () => {
    api.currentBinding!.mockResolvedValue({ ...head(), available: false, canActivate: false, canReturn: false })
    show()
    await vi.waitFor(() => expect(wrapper!.find('[data-testid="workspace-snapshot-snapshot-original"]').exists()).toBe(true))
    expect(wrapper!.get('[data-testid="workspace-capture"]').attributes('disabled')).toBeDefined()
    expect(wrapper!.get('[data-testid="workspace-restore-copy"]').attributes('disabled')).toBeDefined()
    expect(api.capture).not.toHaveBeenCalled()
    expect(api.restore).not.toHaveBeenCalled()
  })

  it('does not attach an old owner response to the account opened while it was in flight', async () => {
    let complete!: (value: ReturnType<typeof restored>) => void
    api.restore!.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    show(); await ready()
    await wrapper!.get('[data-testid="workspace-restore-copy"]').trigger('click')
    await vi.waitFor(() => expect(api.restore).toHaveBeenCalledTimes(1))
    useAuth().setUser({ username: 'bob', expiresAt: '2099-01-01T00:00:00Z' })
    await flushPromises()
    complete(restored())
    await flushPromises()
    expect(await readWorkspaceRequest(workspaceRequestScope('bob', sessionId))).toBeNull()
    expect((await readWorkspaceRequest(workspaceRequestScope('alice', sessionId)))?.receiptId).toBeUndefined()
    expect(wrapper!.emitted('bindingChanged')).toBeUndefined()
  })
})
