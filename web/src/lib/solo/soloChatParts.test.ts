import { describe, expect, it, vi } from 'vitest'
import { computed, ref, shallowRef } from 'vue'
import type { SoloSession } from '@/lib/api/apiTypes'
import { createSoloModelSettings } from './soloModelSettings'
import { createSoloQueueOrder } from './soloQueueOrder'
import type { SoloQueueItem } from './useSoloChat'

const item = (id: string, local = false): SoloQueueItem => ({ id, text: id, images: [], ...(local ? { local } : {}) })

describe('createSoloQueueOrder', () => {
  it('shows the dragged order until the server queue state clears it, keeping local items last', async () => {
    const queued = ref([item('a'), item('b'), item('c'), item('l', true)])
    const reorderSoloQueue = vi.fn(async () => undefined)
    const order = createSoloQueueOrder({ sessionId: ref('s1'), queued, transport: { reorderSoloQueue } as never, isCurrent: () => true, epoch: () => 0 })
    await order.reorderQueued(2, 0)
    expect(reorderSoloQueue).toHaveBeenCalledWith('s1', ['c', 'a', 'b'])
    expect(order.displayQueued.value.map((row) => row.id)).toEqual(['c', 'a', 'b', 'l'])
    await order.reorderQueued(0, 3)
    expect(reorderSoloQueue).toHaveBeenCalledTimes(1)
    order.clear()
    expect(order.displayQueued.value.map((row) => row.id)).toEqual(['a', 'b', 'c', 'l'])
  })

  it('drops the overlay when the latest reorder fails', async () => {
    const queued = ref([item('a'), item('b')])
    const order = createSoloQueueOrder({ sessionId: ref('s1'), queued, transport: { reorderSoloQueue: vi.fn(async () => { throw new Error('x') }) } as never, isCurrent: () => true, epoch: () => 0 })
    await order.reorderQueued(1, 0)
    expect(order.displayQueued.value.map((row) => row.id)).toEqual(['a', 'b'])
  })
})

describe('createSoloModelSettings', () => {
  const row = (patch: Partial<SoloSession> = {}) => ({ id: 's1', status: 'running', currentModelId: 'm1', currentModelSet: true, reasoningEffort: 'low', ...patch }) as SoloSession

  function setup(setSoloSessionModel: (...args: unknown[]) => Promise<SoloSession>) {
    const session = shallowRef<SoloSession | null>(row())
    const actionError = ref('')
    const settings = createSoloModelSettings({
      sessionId: ref('s1'),
      session,
      sessionOpen: computed(() => true),
      actionError,
      transport: { setSoloSessionModel, setSoloSessionReasoningEffort: vi.fn(async () => row()) } as never,
      disposed: () => false,
      sessionGeneration: () => 0,
      patchSession: (patch) => { session.value = { ...session.value!, ...patch } },
      errorText: () => 'failed',
    })
    return { session, actionError, settings }
  }

  it('keeps a pending selection over fetched rows and confirms the server reply', async () => {
    let resolve!: (value: SoloSession) => void
    const { session, settings } = setup(() => new Promise((r) => { resolve = r }))
    const saving = settings.setCurrentModel('m2')
    expect(settings.pending()).toBe(true)
    expect(settings.modelChangePending.value).toBe(true)
    expect(settings.mergeFetched(row({ currentModelId: 'm1' })).currentModelId).toBe('m2')
    await Promise.resolve()
    resolve(row({ currentModelId: 'm2', reasoningEffort: 'high' }))
    expect(await saving).toBe(true)
    expect(session.value?.currentModelId).toBe('m2')
    expect(settings.pending()).toBe(false)
    expect(settings.modelChangePending.value).toBe(false)
  })

  it('restores the confirmed choice when the latest request fails', async () => {
    const { session, actionError, settings } = setup(async () => { throw new Error('x') })
    expect(await settings.setCurrentModel('m2')).toBe(false)
    expect(session.value?.currentModelId).toBe('m1')
    expect(actionError.value).toBe('failed')
  })
})
