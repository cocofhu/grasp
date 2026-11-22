import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  reactQueueRemove: vi.fn(),
  reactQueueSteer: vi.fn(),
  reactQueueReorder: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    reactQueueRemove: mocks.reactQueueRemove,
    reactQueueSteer: mocks.reactQueueSteer,
    reactQueueReorder: mocks.reactQueueReorder,
  },
}))
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ error: mocks.toastError }),
}))

import pages from '@/locales/zh-CN/pages.json'
import { i18n } from '@/lib/shared/i18n'
import { reactQueueActions } from './reactQueueActions'

describe('reactQueueActions', () => {
  beforeEach(() => {
    i18n.global.setLocaleMessage('zh-CN', pages)
    for (const m of Object.values(mocks)) m.mockReset().mockResolvedValue({})
  })

  it('sends queue edits to the current target and skips empty ones', async () => {
    const q = reactQueueActions(() => ({ runId: 'r', nodeId: 'n' }))
    await q.queueRemove('a')
    await q.queueSteer('b')
    await q.queueReorder(['c', 'a'])
    await q.queueRemove(undefined)
    await q.queueReorder([])
    expect(mocks.reactQueueRemove).toHaveBeenCalledExactlyOnceWith('r', 'n', 'a')
    expect(mocks.reactQueueSteer).toHaveBeenCalledExactlyOnceWith('r', 'n', 'b')
    expect(mocks.reactQueueReorder).toHaveBeenCalledExactlyOnceWith('r', 'n', ['c', 'a'])
    expect(mocks.toastError).not.toHaveBeenCalled()
  })

  it('does nothing without a target', async () => {
    const q = reactQueueActions(() => null)
    await q.queueSteer('b')
    expect(mocks.reactQueueSteer).not.toHaveBeenCalled()
  })

  it('reports a failed request to the user and the console', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    mocks.reactQueueSteer.mockRejectedValueOnce(new Error('network down'))
    await reactQueueActions(() => ({ runId: 'r', nodeId: 'n' })).queueSteer('b')
    expect(warn).toHaveBeenCalledWith('reactQueueSteer failed', 'network down')
    expect(mocks.toastError).toHaveBeenCalledWith('排队操作失败：network down')
    warn.mockRestore()
  })
})
