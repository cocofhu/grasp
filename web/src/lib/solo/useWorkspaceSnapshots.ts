import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { workspaceSnapshotsClient as client, type WorkflowWorkspaceCheckpoint, type WorkspaceBindingReceipt, type WorkspaceBindingView, type WorkspaceRestoreReceipt, type WorkspaceScope, type WorkspaceSnapshot } from '@/lib/api/clients/workspaceSnapshotsClient'
import { useAuth } from '@/lib/composables/useAuth'
import { newSoloRequestID } from './soloOutbox'
import { attachWorkspaceReceipt, readWorkspaceRequest, removeWorkspaceRequest, saveWorkspaceRequest, workspaceRequestScope, type SavedWorkspaceRequest, type WorkspaceRequest } from './workspaceRequestJournal'

type Receipt = WorkspaceSnapshot | WorkspaceRestoreReceipt | WorkspaceBindingReceipt
type Props = { sessionId: string; runId?: string; sandboxId?: number; busy?: boolean }
const terminal = (state: string) => ['ready', 'restored', 'completed', 'applied', 'activated', 'returned', 'failed', 'cancelled', 'expired'].includes(state)
const bindingComplete = (state: string) => ['completed', 'applied', 'activated', 'returned'].includes(state)
const errorStatus = (cause: unknown) => cause && typeof cause === 'object' ? Number((cause as { status?: number }).status || 0) : 0

export function useWorkspaceSnapshots(props: Props, changed: (bindingChanged?: boolean) => void) {
  const { user } = useAuth()
  const scope = computed(() => user.value?.username && props.sessionId ? workspaceRequestScope(user.value.username, props.sessionId) : '')
  const source = computed<WorkspaceScope>(() => props.runId ? { kind: 'run', id: props.runId } : { kind: 'solo', id: props.sessionId })
  const snapshots = ref<WorkspaceSnapshot[]>([])
  const restores = ref<WorkspaceRestoreReceipt[]>([])
  const operations = ref<WorkspaceBindingReceipt[]>([])
  const checkpoints = ref<WorkflowWorkspaceCheckpoint[]>([])
  const binding = ref<WorkspaceBindingView | null>(null)
  const pending = ref<SavedWorkspaceRequest | null>(null)
  const pendingState = ref('')
  const notReceived = ref(false)
  const rejected = ref(false)
  const loading = ref(false)
  const busy = ref(false)
  const error = ref('')
  const nextCursor = ref('')
  const canMutate = computed(() => !!scope.value && !!binding.value?.available && !!binding.value.fingerprint && !!props.sandboxId && !props.busy && !pending.value && !binding.value.pendingOperationId && !busy.value)
  const canCapture = computed(() => canMutate.value && binding.value?.canCapture === true)
  const canRestore = computed(() => canMutate.value && binding.value?.canRestore === true)
  let generation = 0
  let disposed = false
  let polling = false
  const current = (owner: string, version: number) => !disposed && owner === scope.value && version === generation

  function report(cause: unknown) {
    const status = errorStatus(cause)
    error.value = status === 404 ? 'notFound' : status === 403 ? 'forbidden' : status === 409 ? 'changed' : status === 501 || status === 422 ? 'unsupported' : 'request'
  }

  async function refresh(includeBinding = false) {
    const owner = scope.value, version = generation
    if (!owner) return
    const calls = await Promise.allSettled([
      client.list(source.value), client.listRestores(props.sessionId),
      ...(includeBinding ? [client.currentBinding(props.sessionId), client.listBindingOperations(props.sessionId)] : []),
      ...(props.runId ? [client.listCheckpoints(props.runId)] : []),
    ])
    if (!current(owner, version)) return
    const list = calls[0]!, history = calls[1]!
    if (list.status === 'fulfilled') {
      const page = list.value as Awaited<ReturnType<typeof client.list>>
      const existing = snapshots.value
      const incoming = new Set(page.items.map(row => row.id))
      snapshots.value = [...page.items, ...existing.filter(row => !incoming.has(row.id))]
      if (!existing.length) nextCursor.value = page.hasMore ? page.nextCursor || '' : ''
    } else report(list.reason)
    if (history.status === 'fulfilled') restores.value = (history.value as Awaited<ReturnType<typeof client.listRestores>>).items
    else report(history.reason)
    if (includeBinding) {
      const head = calls[2]!, changes = calls[3]!
      binding.value = head.status === 'fulfilled' ? head.value as WorkspaceBindingView : null
      if (changes.status === 'fulfilled') operations.value = changes.value as WorkspaceBindingReceipt[]
    }
    if (props.runId) {
      const visits = calls[includeBinding ? 4 : 2]!
      if (visits.status === 'fulfilled') checkpoints.value = (visits.value as Awaited<ReturnType<typeof client.listCheckpoints>>).items || []
      else if (errorStatus(visits.reason) !== 501) report(visits.reason)
    }
  }

  async function earlier() {
    if (!nextCursor.value || busy.value) return
    const owner = scope.value, version = generation
    busy.value = true
    try {
      const page = await client.list(source.value, nextCursor.value)
      if (!current(owner, version)) return
      const ids = new Set(snapshots.value.map(row => row.id))
      snapshots.value.push(...page.items.filter(row => !ids.has(row.id)))
      nextCursor.value = page.hasMore ? page.nextCursor || '' : ''
    } catch (cause) { if (current(owner, version)) report(cause) }
    finally { if (current(owner, version)) busy.value = false }
  }

  async function accept(record: SavedWorkspaceRequest, receipt: Receipt, version: number) {
    if (!current(record.scope, version) || pending.value?.id !== record.id) return
    await attachWorkspaceReceipt(record.scope, record.id, receipt.id)
    if (!current(record.scope, version) || pending.value?.id !== record.id) return
    pending.value = { ...record, receiptId: receipt.id }
    pendingState.value = receipt.state
    notReceived.value = false
    if (terminal(receipt.state) && !('requiresReconciliation' in receipt && receipt.requiresReconciliation)) {
      await removeWorkspaceRequest(record.scope, record.id)
      if (!current(record.scope, version) || pending.value?.id !== record.id) return
      pending.value = null; rejected.value = false
      changed((record.request.action === 'activate' || record.request.action === 'return') && bindingComplete(receipt.state))
      await refresh(true)
    }
  }

  async function observe(reconcile = false) {
    const record = pending.value, version = generation
    if (!record || polling) return
    polling = true
    try {
      const action = record.request.action
      let receipt: Receipt
      if (action === 'capture') {
        receipt = record.receiptId ? await client.get(record.request.source, record.receiptId) : await client.resolveCapture(record.request.source, record.id)
        if (!current(record.scope, version)) return
        if (reconcile && receipt.state === 'unknown') receipt = await client.reconcileCapture(record.request.source, receipt.id)
      } else if (action === 'restore') {
        receipt = record.receiptId ? await client.getRestore(props.sessionId, record.receiptId) : await client.resolveRestore(props.sessionId, record.id)
        if (!current(record.scope, version)) return
        if (reconcile && receipt.state === 'unknown') receipt = await client.reconcileRestore(props.sessionId, receipt.id)
      } else {
        receipt = record.receiptId ? await client.getBindingOperation(props.sessionId, record.receiptId) : await client.resolveBindingOperation(props.sessionId, record.id)
        if (!current(record.scope, version)) return
        if (reconcile && (receipt.state === 'unknown' || ('requiresReconciliation' in receipt && receipt.requiresReconciliation))) receipt = await client.reconcileBinding(props.sessionId, receipt.id)
      }
      await accept(record, receipt, version)
    } catch (cause) {
      if (!current(record.scope, version)) return
      if (!record.receiptId && errorStatus(cause) === 404) notReceived.value = true
      else report(cause)
    } finally { polling = false }
  }

  async function postOriginal() {
    const record = pending.value, version = generation
    if (!record || busy.value || !current(record.scope, version)) return
    busy.value = true; error.value = ''; rejected.value = false
    try {
      const request = record.request
      const receipt = request.action === 'capture' ? await client.capture(request.source, request.body)
        : request.action === 'restore' ? await client.restore(props.sessionId, request.body)
          : await client.changeBinding(props.sessionId, request.body)
      await accept(record, receipt, version)
    } catch (cause) {
      if (current(record.scope, version)) {
        report(cause)
        const code = cause && typeof cause === 'object' ? String((cause as { code?: string }).code || '') : ''
        rejected.value = [400, 403, 409, 422, 501].includes(errorStatus(cause)) && !/reconcile|pending|unknown/.test(code)
        await observe()
      }
    } finally { if (current(record.scope, version)) busy.value = false }
  }

  async function submit(request: WorkspaceRequest) {
    const owner = scope.value, version = generation
    if (!canMutate.value || !owner) return
    const record: SavedWorkspaceRequest = JSON.parse(JSON.stringify({ scope: owner, id: request.body.clientRequestId, request }))
    try {
      await saveWorkspaceRequest(record)
      if (!current(owner, version)) return
      pending.value = record; pendingState.value = 'queued'; notReceived.value = false
      await postOriginal()
    } catch {
      if (!current(owner, version)) return
      error.value = 'storage'
      try {
        const saved = await readWorkspaceRequest(owner)
        if (current(owner, version)) pending.value = saved
      } catch { /* No POST without the durable request. */ }
    }
  }

  async function capture() {
    if (!canCapture.value || !binding.value?.fingerprint) return
    await submit({ action: 'capture', source: { kind: 'solo', id: props.sessionId }, body: { clientRequestId: newSoloRequestID(), sandboxId: props.sandboxId!, expectedBinding: binding.value.fingerprint, paths: ['.'], includeIgnored: [], durationMillis: 600000 } })
  }
  async function restore(snapshotId: string) {
    if (!canRestore.value || !binding.value?.fingerprint) return
    await submit({ action: 'restore', body: { clientRequestId: newSoloRequestID(), snapshotId, expectedBinding: binding.value.fingerprint, durationMillis: 600000 } })
  }
  async function switchWorkspace(action: 'activate' | 'return', target: string) {
    const head = binding.value
    if (!canMutate.value || !head?.fingerprint || !head.returnSnapshotId || (action === 'activate' ? !head.canActivate : !head.canReturn)) return
    await submit({ action, body: { clientRequestId: newSoloRequestID(), action, expectedBinding: head.fingerprint, returnSnapshotId: head.returnSnapshotId, durationMillis: 60000, ...(action === 'activate' ? { restoreId: target } : { targetBindingId: target }) } })
  }
  async function checkReceipt(kind: 'restore' | 'binding' | 'capture', id: string, state: string) {
    if (busy.value) return
    const owner = scope.value, version = generation
    busy.value = true
    try {
      const receipt = kind === 'restore' ? await (state === 'unknown' ? client.reconcileRestore(props.sessionId, id) : client.getRestore(props.sessionId, id))
        : kind === 'binding' ? await client.reconcileBinding(props.sessionId, id)
          : await (state === 'unknown' ? client.reconcileCapture(source.value, id) : client.get(source.value, id))
      if (current(owner, version)) { await refresh(true); if (current(owner, version)) changed(kind === 'binding' && bindingComplete(receipt.state)) }
    } catch (cause) { if (current(owner, version)) report(cause) }
    finally { if (current(owner, version)) busy.value = false }
  }
  async function dismissRejected() {
    const record = pending.value, version = generation
    if (!record || !notReceived.value || !rejected.value) return
    try {
      await removeWorkspaceRequest(record.scope, record.id)
      if (current(record.scope, version) && pending.value?.id === record.id) {
        pending.value = null; error.value = ''; rejected.value = false; await refresh(true)
      }
    } catch { if (current(record.scope, version)) error.value = 'storage' }
  }

  async function checkCheckpoint(id: string) {
    if (busy.value || !props.runId) return
    const owner = scope.value, version = generation, runId = props.runId
    busy.value = true
    try {
      await client.reconcileCheckpoint(runId, id)
      if (!current(owner, version)) return
      await refresh(true)
      if (current(owner, version)) changed()
    } catch (cause) { if (current(owner, version)) report(cause) }
    finally { if (current(owner, version)) busy.value = false }
  }

  watch([scope, () => source.value.kind, () => source.value.id], async () => {
    const owner = scope.value, version = ++generation
    pending.value = null; binding.value = null; snapshots.value = []; restores.value = []; operations.value = []; checkpoints.value = []; error.value = ''; loading.value = true; busy.value = false
    pendingState.value = ''; notReceived.value = false; rejected.value = false; nextCursor.value = ''
    if (!owner) { loading.value = false; return }
    try {
      const record = await readWorkspaceRequest(owner)
      if (!current(owner, version)) return
      pending.value = record
      await refresh(true)
      await observe()
    } catch { if (current(owner, version)) error.value = 'storage' }
    finally { if (current(owner, version)) loading.value = false }
  }, { immediate: true })
  const timer = setInterval(() => { if (pending.value) void observe(); else if (!busy.value) void refresh() }, 3000)
  onBeforeUnmount(() => { disposed = true; generation++; clearInterval(timer) })
  return { snapshots, restores, operations, checkpoints, binding, pending, pendingState, notReceived, rejected, loading, busy, error, nextCursor, canMutate, canCapture, canRestore, refresh, earlier, capture, restore, switchWorkspace, observe, postOriginal, checkReceipt, dismissRejected, checkCheckpoint }
}
