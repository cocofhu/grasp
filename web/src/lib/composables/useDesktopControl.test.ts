// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const sandboxDesktop = vi.fn()
const setSandboxDesktopControl = vi.fn()
vi.mock('@/lib/api/api', () => ({
  api: {
    sandboxDesktop: (...a: unknown[]) => sandboxDesktop(...a),
    setSandboxDesktopControl: (...a: unknown[]) => setSandboxDesktopControl(...a),
  },
}))

import { DESKTOP_HEARTBEAT_MS, DESKTOP_POLL_MS, useDesktopControl } from './useDesktopControl'

const desktop = { mode: 'desktop', shellReady: true, agentActive: true, holder: 'agent', apps: [] }

function harness(id: number | undefined, on = false) {
  const control = ref(on)
  let api!: ReturnType<typeof useDesktopControl>
  const w = mount(
    defineComponent({
      setup() {
        api = useDesktopControl(() => id, control)
        return () => h('div')
      },
    }),
  )
  return { w, control, api: () => api }
}

describe('useDesktopControl', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sandboxDesktop.mockReset().mockResolvedValue(desktop)
    setSandboxDesktopControl
      .mockReset()
      .mockImplementation((_id: number, holder: string) => Promise.resolve({ ...desktop, holder }))
  })
  afterEach(() => vi.useRealTimers())

  it('polls status and takes, renews and returns control', async () => {
    const { w, control, api } = harness(7)
    await flushPromises()
    expect(api().isDesktop.value).toBe(true)
    expect(api().agentBusy.value).toBe(true)
    await vi.advanceTimersByTimeAsync(DESKTOP_POLL_MS)
    expect(sandboxDesktop).toHaveBeenCalledTimes(2)

    control.value = true
    await nextTick()
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenLastCalledWith(7, 'user', expect.any(String), undefined)
    expect(api().userHolds.value).toBe(true)
    await vi.advanceTimersByTimeAsync(DESKTOP_HEARTBEAT_MS)
    expect(setSandboxDesktopControl.mock.calls.filter((c) => c[1] === 'user').length).toBe(2)

    control.value = false
    await nextTick()
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenLastCalledWith(7, 'agent', expect.any(String), undefined)
    w.unmount()
    expect(setSandboxDesktopControl.mock.calls.filter((c) => c[1] === 'agent').length).toBe(1)
  })

  it('takes control once the desktop is detected and releases with keepalive on pagehide', async () => {
    harness(3, true)
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenCalledWith(3, 'user', expect.any(String), undefined)
    window.dispatchEvent(new Event('pagehide'))
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenLastCalledWith(3, 'agent', expect.any(String), { keepalive: true })
  })

  it('drops control when the tab is hidden and releases on unmount', async () => {
    const { w, control } = harness(4, true)
    await flushPromises()
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    await nextTick()
    expect(control.value).toBe(false)
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenLastCalledWith(4, 'agent', expect.any(String), undefined)
    control.value = true
    await nextTick()
    w.unmount()
    expect(setSandboxDesktopControl).toHaveBeenLastCalledWith(4, 'agent', expect.any(String), { keepalive: true })
  })

  it('stops polling on the browser image and ignores errors', async () => {
    sandboxDesktop.mockResolvedValueOnce({ mode: 'browser', shellReady: false, agentActive: false, holder: 'agent', apps: [] })
    const { control, api } = harness(5)
    await flushPromises()
    expect(api().isDesktop.value).toBe(false)
    control.value = true
    await nextTick()
    expect(setSandboxDesktopControl).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(DESKTOP_POLL_MS * 2)
    expect(sandboxDesktop).toHaveBeenCalledTimes(1)

    sandboxDesktop.mockRejectedValueOnce(new Error('down'))
    await api().refresh()
    expect(api().isDesktop.value).toBe(false)
  })

  it('swallows control errors and does nothing without a sandbox', async () => {
    setSandboxDesktopControl.mockRejectedValue(new Error('down'))
    const { control } = harness(6)
    await flushPromises()
    control.value = true
    await nextTick()
    await flushPromises()
    expect(setSandboxDesktopControl).toHaveBeenCalled()

    sandboxDesktop.mockClear()
    const none = harness(undefined)
    await flushPromises()
    await none.api().refresh()
    expect(sandboxDesktop).not.toHaveBeenCalled()
  })
})
