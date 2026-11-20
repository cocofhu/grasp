// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const sandboxDesktop = vi.fn()
vi.mock('@/lib/api/api', () => ({
  api: { sandboxDesktop: (...a: unknown[]) => sandboxDesktop(...a) },
}))

import { DESKTOP_IMAGE_MAX_TRIES, DESKTOP_IMAGE_RETRY_MS, useDesktopImage } from './useDesktopImage'

const status = (mode: string) => ({ mode, shellReady: false, agentActive: false, holder: 'agent', apps: [] })

function harness(id = ref<number | null>(7)) {
  const onBrowser = vi.fn()
  let out!: ReturnType<typeof useDesktopImage>
  const w = mount(
    defineComponent({
      setup() {
        out = useDesktopImage(() => id.value, { onBrowser })
        return () => h('div')
      },
    }),
  )
  return { w, id, onBrowser, desktopImage: () => out.desktopImage.value }
}

describe('useDesktopImage', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sandboxDesktop.mockReset()
  })
  afterEach(() => vi.useRealTimers())

  it('retries while the sandbox is starting, then shows the desktop', async () => {
    sandboxDesktop.mockRejectedValueOnce(new Error('502')).mockRejectedValueOnce(new Error('502')).mockResolvedValue(status('desktop'))
    const h = harness()
    await flushPromises()
    expect(h.desktopImage()).toBe(false)
    await vi.advanceTimersByTimeAsync(DESKTOP_IMAGE_RETRY_MS * 2)
    expect(h.desktopImage()).toBe(true)
    expect(sandboxDesktop).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(DESKTOP_IMAGE_RETRY_MS * 3)
    expect(sandboxDesktop).toHaveBeenCalledTimes(3)
  })

  it('reports the browser image once and stops', async () => {
    sandboxDesktop.mockResolvedValue(status('browser'))
    const h = harness()
    await flushPromises()
    expect(h.desktopImage()).toBe(false)
    expect(h.onBrowser).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(DESKTOP_IMAGE_RETRY_MS * 3)
    expect(sandboxDesktop).toHaveBeenCalledTimes(1)
  })

  it('gives up after the try limit and on unmount', async () => {
    sandboxDesktop.mockRejectedValue(new Error('down'))
    const h = harness()
    await vi.advanceTimersByTimeAsync(DESKTOP_IMAGE_RETRY_MS * (DESKTOP_IMAGE_MAX_TRIES + 5))
    expect(sandboxDesktop).toHaveBeenCalledTimes(DESKTOP_IMAGE_MAX_TRIES)
    h.w.unmount()
  })

  it('asks again when the refresh key changes, even after a browser answer', async () => {
    sandboxDesktop.mockResolvedValueOnce(status('browser')).mockResolvedValue(status('desktop'))
    const key = ref(0)
    let out!: ReturnType<typeof useDesktopImage>
    mount(
      defineComponent({
        setup() {
          out = useDesktopImage(() => 7, { refreshKey: () => key.value })
          return () => h('div')
        },
      }),
    )
    await flushPromises()
    expect(out.desktopImage.value).toBe(false)
    key.value = 1
    await nextTick()
    await flushPromises()
    expect(sandboxDesktop).toHaveBeenCalledTimes(2)
    expect(out.desktopImage.value).toBe(true)
  })

  it('restarts for a new sandbox id and drops the old retry', async () => {
    sandboxDesktop.mockRejectedValueOnce(new Error('502')).mockResolvedValue(status('desktop'))
    const h = harness()
    await flushPromises()
    h.id.value = 8
    await nextTick()
    await flushPromises()
    expect(sandboxDesktop).toHaveBeenLastCalledWith(8)
    expect(h.desktopImage()).toBe(true)
    await vi.advanceTimersByTimeAsync(DESKTOP_IMAGE_RETRY_MS * 2)
    expect(sandboxDesktop).toHaveBeenCalledTimes(2)
  })
})
